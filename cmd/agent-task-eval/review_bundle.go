package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"twitter-clone/internal/module/agent/eval"
)

const (
	agentTaskReviewBundleSchemaVersion  = eval.AgentTaskReviewBundleSchemaVersion
	agentTaskReviewPayloadSchemaVersion = "agent-task-review-payload/v1"
	agentTaskReviewBundleAlgorithm      = "AES-256-GCM"
	maxAgentTaskReviewPayloadBytes      = 32 << 20
	maxAgentTaskReviewBundleBytes       = 48 << 20
)

type agentTaskReviewCase struct {
	Input  string                   `json:"input"`
	Output string                   `json:"output"`
	Result eval.AgentTaskCaseResult `json:"result"`
}

type agentTaskReviewPayload struct {
	SchemaVersion        string                            `json:"schema_version"`
	CreatedAt            time.Time                         `json:"created_at"`
	ReportPayloadSHA256  string                            `json:"report_payload_sha256"`
	ReportIntegrityKeyID string                            `json:"report_integrity_key_id"`
	DatasetVersion       string                            `json:"dataset_version"`
	DatasetSHA256        string                            `json:"dataset_sha256"`
	CandidateExecution   eval.AgentTaskExecutionDescriptor `json:"candidate_execution"`
	StableExecution      eval.AgentTaskExecutionDescriptor `json:"stable_execution"`
	Candidate            []agentTaskReviewCase             `json:"candidate"`
	Stable               []agentTaskReviewCase             `json:"stable"`
}

type agentTaskReviewBundle struct {
	SchemaVersion       string `json:"schema_version"`
	Algorithm           string `json:"algorithm"`
	KeyID               string `json:"key_id"`
	ReportPayloadSHA256 string `json:"report_payload_sha256"`
	Nonce               string `json:"nonce"`
	Ciphertext          string `json:"ciphertext"`
}

type agentTaskReviewBundleHeader struct {
	SchemaVersion       string `json:"schema_version"`
	Algorithm           string `json:"algorithm"`
	KeyID               string `json:"key_id"`
	ReportPayloadSHA256 string `json:"report_payload_sha256"`
}

type capturedAgentTaskReviewCase struct {
	CaseID string
	Input  string
	Output string
}

type agentTaskReviewCollector struct {
	mu      sync.Mutex
	samples map[string][]capturedAgentTaskReviewCase
}

type reviewCapturingAgentTaskExecutor struct {
	side      string
	delegate  eval.AgentTaskExecutor
	collector *agentTaskReviewCollector
}

func newAgentTaskReviewCollector() *agentTaskReviewCollector {
	return &agentTaskReviewCollector{samples: make(map[string][]capturedAgentTaskReviewCase, 2)}
}

func (collector *agentTaskReviewCollector) Wrap(side string, executor eval.AgentTaskExecutor) eval.AgentTaskExecutor {
	return &reviewCapturingAgentTaskExecutor{
		side: strings.TrimSpace(side), delegate: executor, collector: collector,
	}
}

func (executor *reviewCapturingAgentTaskExecutor) Execute(
	ctx context.Context,
	sample eval.AgentTaskCase,
) (eval.AgentTaskExecution, error) {
	execution, err := executor.delegate.Execute(ctx, sample)
	if err == nil {
		executor.collector.capture(executor.side, sample, execution)
	}
	return execution, err
}

func (executor *reviewCapturingAgentTaskExecutor) Preflight(ctx context.Context) error {
	preflighter, ok := executor.delegate.(agentTaskExecutorPreflighter)
	if !ok {
		return errors.New("review capture delegate does not implement model/tool preflight")
	}
	return preflighter.Preflight(ctx)
}

func (collector *agentTaskReviewCollector) capture(
	side string,
	sample eval.AgentTaskCase,
	execution eval.AgentTaskExecution,
) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.samples[side] = append(collector.samples[side], capturedAgentTaskReviewCase{
		CaseID: sample.ID,
		Input:  sample.Input,
		Output: execution.Output,
	})
}

func (collector *agentTaskReviewCollector) Build(
	output agentTaskEvaluationOutput,
	createdAt time.Time,
) (agentTaskReviewPayload, error) {
	if collector == nil {
		return agentTaskReviewPayload{}, errors.New("agent task review collector is nil")
	}
	if output.Integrity == nil {
		return agentTaskReviewPayload{}, errors.New("agent task review requires a signed report")
	}
	if output.Stable == nil {
		return agentTaskReviewPayload{}, errors.New("agent task review requires stable comparison evidence")
	}
	if createdAt.IsZero() {
		return agentTaskReviewPayload{}, errors.New("agent task review creation time is required")
	}

	collector.mu.Lock()
	candidateSamples := append([]capturedAgentTaskReviewCase(nil), collector.samples["candidate"]...)
	stableSamples := append([]capturedAgentTaskReviewCase(nil), collector.samples["stable"]...)
	collector.mu.Unlock()

	candidate, err := buildAgentTaskReviewCases(output.Candidate, candidateSamples)
	if err != nil {
		return agentTaskReviewPayload{}, fmt.Errorf("build candidate review evidence: %w", err)
	}
	stable, err := buildAgentTaskReviewCases(*output.Stable, stableSamples)
	if err != nil {
		return agentTaskReviewPayload{}, fmt.Errorf("build stable review evidence: %w", err)
	}
	payload := agentTaskReviewPayload{
		SchemaVersion:        agentTaskReviewPayloadSchemaVersion,
		CreatedAt:            createdAt.UTC(),
		ReportPayloadSHA256:  strings.ToLower(output.Integrity.PayloadSHA256),
		ReportIntegrityKeyID: output.Integrity.KeyID,
		DatasetVersion:       output.Candidate.DatasetVersion,
		DatasetSHA256:        strings.ToLower(output.Candidate.DatasetSHA256),
		CandidateExecution:   output.Candidate.Execution,
		StableExecution:      output.Stable.Execution,
		Candidate:            candidate,
		Stable:               stable,
	}
	if err := validateAgentTaskReviewPayload(payload, output); err != nil {
		return agentTaskReviewPayload{}, err
	}
	return payload, nil
}

func buildAgentTaskReviewCases(
	report eval.AgentTaskReport,
	samples []capturedAgentTaskReviewCase,
) ([]agentTaskReviewCase, error) {
	if len(samples) != len(report.CaseResults) {
		return nil, fmt.Errorf("captured %d outputs for %d report cases", len(samples), len(report.CaseResults))
	}
	reviewCases := make([]agentTaskReviewCase, 0, len(samples))
	for index, sample := range samples {
		result := report.CaseResults[index]
		if sample.CaseID != result.CaseID {
			return nil, fmt.Errorf("captured case %d id %q does not match report id %q", index, sample.CaseID, result.CaseID)
		}
		if outputSHA256(sample.Output) != result.OutputSHA256 || utf8.RuneCountInString(sample.Output) != result.OutputCharacters {
			return nil, fmt.Errorf("captured output for case %q does not match report digest", sample.CaseID)
		}
		reviewCases = append(reviewCases, agentTaskReviewCase{
			Input: sample.Input, Output: sample.Output, Result: cloneAgentTaskReviewResult(result),
		})
	}
	return reviewCases, nil
}

func validateAgentTaskReviewPayload(payload agentTaskReviewPayload, output agentTaskEvaluationOutput) error {
	if payload.SchemaVersion != agentTaskReviewPayloadSchemaVersion {
		return fmt.Errorf("unsupported agent task review payload schema %q", payload.SchemaVersion)
	}
	if payload.CreatedAt.IsZero() || output.Integrity == nil || output.Stable == nil {
		return errors.New("agent task review payload or report identity is incomplete")
	}
	if payload.ReportPayloadSHA256 != strings.ToLower(output.Integrity.PayloadSHA256) ||
		payload.ReportIntegrityKeyID != output.Integrity.KeyID ||
		payload.DatasetVersion != output.Candidate.DatasetVersion ||
		payload.DatasetSHA256 != strings.ToLower(output.Candidate.DatasetSHA256) ||
		payload.CandidateExecution != output.Candidate.Execution ||
		payload.StableExecution != output.Stable.Execution {
		return errors.New("agent task review payload does not match signed report identity")
	}
	if !validReviewSHA256(payload.ReportPayloadSHA256) || !validReviewSHA256(payload.DatasetSHA256) {
		return errors.New("agent task review payload contains an invalid digest")
	}
	if err := validateAgentTaskReviewCases(payload.Candidate, output.Candidate.CaseResults); err != nil {
		return fmt.Errorf("validate candidate review evidence: %w", err)
	}
	if err := validateAgentTaskReviewCases(payload.Stable, output.Stable.CaseResults); err != nil {
		return fmt.Errorf("validate stable review evidence: %w", err)
	}
	if len(payload.Candidate) != len(payload.Stable) {
		return errors.New("agent task review sides have different case counts")
	}
	for index := range payload.Candidate {
		if payload.Candidate[index].Result.CaseID != payload.Stable[index].Result.CaseID ||
			payload.Candidate[index].Input != payload.Stable[index].Input {
			return fmt.Errorf("agent task review side mismatch at case %d", index)
		}
	}
	return nil
}

func validateAgentTaskReviewCases(reviewCases []agentTaskReviewCase, reportCases []eval.AgentTaskCaseResult) error {
	if len(reviewCases) != len(reportCases) {
		return fmt.Errorf("review contains %d cases, report contains %d", len(reviewCases), len(reportCases))
	}
	seen := make(map[string]struct{}, len(reviewCases))
	for index, reviewCase := range reviewCases {
		result := reportCases[index]
		if !reflect.DeepEqual(reviewCase.Result, result) {
			return fmt.Errorf("review case %d result does not match signed report", index)
		}
		if _, exists := seen[result.CaseID]; exists {
			return fmt.Errorf("review contains duplicate case id %q", result.CaseID)
		}
		seen[result.CaseID] = struct{}{}
		if outputSHA256(reviewCase.Output) != result.OutputSHA256 ||
			utf8.RuneCountInString(reviewCase.Output) != result.OutputCharacters {
			return fmt.Errorf("review output for case %q does not match signed report digest", result.CaseID)
		}
	}
	return nil
}

func encryptAgentTaskReviewPayload(
	payload agentTaskReviewPayload,
	key []byte,
	keyID string,
	random io.Reader,
) (agentTaskReviewBundle, error) {
	keyID = strings.TrimSpace(keyID)
	if len(key) != 32 || keyID == "" {
		return agentTaskReviewBundle{}, errors.New("agent task review encryption requires a 32-byte key and key id")
	}
	if random == nil {
		random = rand.Reader
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return agentTaskReviewBundle{}, fmt.Errorf("encode agent task review payload: %w", err)
	}
	if len(plaintext) > maxAgentTaskReviewPayloadBytes {
		return agentTaskReviewBundle{}, fmt.Errorf("agent task review payload exceeds %d bytes", maxAgentTaskReviewPayloadBytes)
	}
	aead, err := newAgentTaskReviewAEAD(key)
	if err != nil {
		return agentTaskReviewBundle{}, err
	}
	bundle := agentTaskReviewBundle{
		SchemaVersion:       agentTaskReviewBundleSchemaVersion,
		Algorithm:           agentTaskReviewBundleAlgorithm,
		KeyID:               keyID,
		ReportPayloadSHA256: payload.ReportPayloadSHA256,
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(random, nonce); err != nil {
		return agentTaskReviewBundle{}, fmt.Errorf("generate agent task review nonce: %w", err)
	}
	aad, err := agentTaskReviewAdditionalData(bundle)
	if err != nil {
		return agentTaskReviewBundle{}, err
	}
	bundle.Nonce = base64.RawURLEncoding.EncodeToString(nonce)
	bundle.Ciphertext = base64.RawURLEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, aad))
	return bundle, nil
}

func decryptAgentTaskReviewBundle(bundle agentTaskReviewBundle, key []byte, trustedKeyID string) (agentTaskReviewPayload, error) {
	trustedKeyID = strings.TrimSpace(trustedKeyID)
	if len(key) != 32 || trustedKeyID == "" {
		return agentTaskReviewPayload{}, errors.New("agent task review decryption requires a 32-byte key and key id")
	}
	if bundle.SchemaVersion != agentTaskReviewBundleSchemaVersion || bundle.Algorithm != agentTaskReviewBundleAlgorithm {
		return agentTaskReviewPayload{}, errors.New("unsupported agent task review bundle format")
	}
	if bundle.KeyID != trustedKeyID {
		return agentTaskReviewPayload{}, fmt.Errorf("review bundle key id %q does not match trusted key id %q", bundle.KeyID, trustedKeyID)
	}
	if !validReviewSHA256(bundle.ReportPayloadSHA256) {
		return agentTaskReviewPayload{}, errors.New("agent task review bundle report digest is invalid")
	}
	aead, err := newAgentTaskReviewAEAD(key)
	if err != nil {
		return agentTaskReviewPayload{}, err
	}
	nonce, err := base64.RawURLEncoding.DecodeString(bundle.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return agentTaskReviewPayload{}, errors.New("agent task review bundle nonce is invalid")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(bundle.Ciphertext)
	if err != nil {
		return agentTaskReviewPayload{}, errors.New("agent task review bundle ciphertext is invalid")
	}
	aad, err := agentTaskReviewAdditionalData(bundle)
	if err != nil {
		return agentTaskReviewPayload{}, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return agentTaskReviewPayload{}, errors.New("decrypt agent task review bundle failed")
	}
	var payload agentTaskReviewPayload
	if err := decodeStrictJSON(plaintext, &payload, "agent task review payload"); err != nil {
		return agentTaskReviewPayload{}, err
	}
	if payload.ReportPayloadSHA256 != bundle.ReportPayloadSHA256 {
		return agentTaskReviewPayload{}, errors.New("agent task review bundle and payload report digests differ")
	}
	return payload, nil
}

func loadAndOpenAgentTaskReviewBundle(
	path string,
	key []byte,
	trustedKeyID string,
	output agentTaskEvaluationOutput,
) (agentTaskReviewPayload, error) {
	payload, _, err := loadAndOpenAgentTaskReviewBundleWithBinding(path, key, trustedKeyID, output)
	return payload, err
}

func loadAndOpenAgentTaskReviewBundleWithBinding(
	path string,
	key []byte,
	trustedKeyID string,
	output agentTaskEvaluationOutput,
) (agentTaskReviewPayload, eval.AgentTaskContentReviewBundleBinding, error) {
	encoded, err := readBoundedReviewFile(strings.TrimSpace(path), maxAgentTaskReviewBundleBytes)
	if err != nil {
		return agentTaskReviewPayload{}, eval.AgentTaskContentReviewBundleBinding{}, fmt.Errorf("read agent task review bundle: %w", err)
	}
	var bundle agentTaskReviewBundle
	if err := decodeStrictJSON(encoded, &bundle, "agent task review bundle"); err != nil {
		return agentTaskReviewPayload{}, eval.AgentTaskContentReviewBundleBinding{}, err
	}
	payload, err := decryptAgentTaskReviewBundle(bundle, key, trustedKeyID)
	if err != nil {
		return agentTaskReviewPayload{}, eval.AgentTaskContentReviewBundleBinding{}, err
	}
	if err := validateAgentTaskReviewPayload(payload, output); err != nil {
		return agentTaskReviewPayload{}, eval.AgentTaskContentReviewBundleBinding{}, err
	}
	digest := sha256.Sum256(encoded)
	binding := eval.AgentTaskContentReviewBundleBinding{
		SchemaVersion: bundle.SchemaVersion,
		KeyID:         bundle.KeyID,
		FileSHA256:    hex.EncodeToString(digest[:]),
	}
	return payload, binding, nil
}

func readBoundedReviewFile(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file size %d exceeds %d bytes", info.Size(), maxBytes)
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return payload, nil
}

func readAgentTaskReviewKey(envName, keyID string) ([]byte, error) {
	envName = strings.TrimSpace(envName)
	keyID = strings.TrimSpace(keyID)
	if envName == "" {
		return nil, errors.New("review key environment variable name is required")
	}
	if keyID == "" {
		return nil, errors.New("review key id is required")
	}
	encoded := strings.TrimSpace(os.Getenv(envName))
	if encoded == "" {
		return nil, fmt.Errorf("review key environment variable %q is empty", envName)
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(encoded)
		if err == nil {
			if len(decoded) != 32 {
				return nil, fmt.Errorf("review key environment variable %q decodes to %d bytes, want 32", envName, len(decoded))
			}
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("review key environment variable %q is not valid base64", envName)
}

func writeAgentTaskReviewBundle(path string, bundle agentTaskReviewBundle) error {
	return writeExclusiveReviewJSON(path, bundle, "encrypted agent task review bundle")
}

func writeAgentTaskReviewPayload(path string, payload agentTaskReviewPayload) error {
	return writeExclusiveReviewJSON(path, payload, "plaintext agent task review output")
}

func writeExclusiveReviewJSON(path string, value any, label string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s path is required", label)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s directory: %w", label, err)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", label, err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", label, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync %s: %w", label, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", label, err)
	}
	return nil
}

func ensureReviewPathAvailable(path, label string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s path is required", label)
	}
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("%s path %q already exists", label, path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check %s path: %w", label, err)
	}
	return nil
}

func sameReviewPath(left, right string) (bool, error) {
	leftPath, err := filepath.Abs(strings.TrimSpace(left))
	if err != nil {
		return false, err
	}
	rightPath, err := filepath.Abs(strings.TrimSpace(right))
	if err != nil {
		return false, err
	}
	return strings.EqualFold(filepath.Clean(leftPath), filepath.Clean(rightPath)), nil
}

func decodeStrictJSON(payload []byte, target any, label string) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%s contains multiple JSON values", label)
		}
		return fmt.Errorf("decode %s trailer: %w", label, err)
	}
	return nil
}

func agentTaskReviewAdditionalData(bundle agentTaskReviewBundle) ([]byte, error) {
	header := agentTaskReviewBundleHeader{
		SchemaVersion:       bundle.SchemaVersion,
		Algorithm:           bundle.Algorithm,
		KeyID:               bundle.KeyID,
		ReportPayloadSHA256: bundle.ReportPayloadSHA256,
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("encode agent task review bundle header: %w", err)
	}
	return encoded, nil
}

func newAgentTaskReviewAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create agent task review cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create agent task review AEAD: %w", err)
	}
	return aead, nil
}

func outputSHA256(output string) string {
	if output == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(output))
	return hex.EncodeToString(digest[:])
}

func validReviewSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func cloneAgentTaskReviewResult(result eval.AgentTaskCaseResult) eval.AgentTaskCaseResult {
	result.ExpectedTools = append([]string(nil), result.ExpectedTools...)
	result.AllowedTools = append([]string(nil), result.AllowedTools...)
	result.SelectedTools = append([]string(nil), result.SelectedTools...)
	result.SemanticFailureCodes = append([]string(nil), result.SemanticFailureCodes...)
	return result
}
