package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
	"twitter-clone/internal/module/agent/objectstore"
)

const (
	agentTaskArchiveConfigSchemaVersion = "agent-task-eval-archive-config/v1"
	maxArchivedEvaluationOutputBytes    = 8 << 20
)

type agentTaskArchiveConfig struct {
	SchemaVersion string `json:"schema_version"`
	Endpoint      string `json:"endpoint"`
	Secure        bool   `json:"secure"`
	Bucket        string `json:"bucket"`
	AccessKeyEnv  string `json:"access_key_env"`
	SecretKeyEnv  string `json:"secret_key_env"`
	RetentionDays int    `json:"retention_days"`
}

func loadAgentTaskArchiveConfig(path string) (agentTaskArchiveConfig, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return agentTaskArchiveConfig{}, err
	}
	var config agentTaskArchiveConfig
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return agentTaskArchiveConfig{}, fmt.Errorf("decode archive config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return agentTaskArchiveConfig{}, errors.New("archive config contains multiple JSON values")
		}
		return agentTaskArchiveConfig{}, fmt.Errorf("decode archive config trailer: %w", err)
	}
	config.Endpoint = strings.TrimSpace(config.Endpoint)
	config.Bucket = strings.TrimSpace(config.Bucket)
	config.AccessKeyEnv = strings.TrimSpace(config.AccessKeyEnv)
	config.SecretKeyEnv = strings.TrimSpace(config.SecretKeyEnv)
	if err := validateAgentTaskArchiveConfig(config); err != nil {
		return agentTaskArchiveConfig{}, err
	}
	return config, nil
}

func validateAgentTaskArchiveConfig(config agentTaskArchiveConfig) error {
	if config.SchemaVersion != agentTaskArchiveConfigSchemaVersion {
		return fmt.Errorf("unsupported archive config schema version %q", config.SchemaVersion)
	}
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" || strings.Contains(endpoint, "://") || strings.ContainsAny(endpoint, "/\\?#@\r\n\t ") {
		return errors.New("archive endpoint must be a credential-free MinIO host[:port]")
	}
	if strings.TrimSpace(config.Bucket) == "" {
		return errors.New("archive bucket is required")
	}
	if !validEnvironmentVariableName(config.AccessKeyEnv) || !validEnvironmentVariableName(config.SecretKeyEnv) {
		return errors.New("archive credential environment variable names are invalid")
	}
	if config.AccessKeyEnv == config.SecretKeyEnv {
		return errors.New("archive access and secret key environment variables must be different")
	}
	if config.RetentionDays < 1 || config.RetentionDays > 3650 {
		return errors.New("archive retention_days must be between 1 and 3650")
	}
	return nil
}

func validEnvironmentVariableName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for index, character := range value {
		if (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') || character == '_' || (index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return false
	}
	return true
}

func newAgentTaskReportArchive(config agentTaskArchiveConfig) (eval.AgentTaskReportArchive, error) {
	accessKey := os.Getenv(config.AccessKeyEnv)
	secretKey := os.Getenv(config.SecretKeyEnv)
	if strings.TrimSpace(accessKey) == "" || strings.TrimSpace(secretKey) == "" {
		return nil, fmt.Errorf("archive credential environment variables %q and %q must be configured", config.AccessKeyEnv, config.SecretKeyEnv)
	}
	return objectstore.NewMinIOAgentTaskReportArchive(objectstore.MinIOAgentTaskReportConfig{
		Endpoint: config.Endpoint, AccessKey: accessKey, SecretKey: secretKey,
		Bucket: config.Bucket, Secure: config.Secure,
	})
}

func archiveEvaluationOutput(
	ctx context.Context,
	archive eval.AgentTaskReportArchive,
	output agentTaskEvaluationOutput,
	key []byte,
	trustedKeyID string,
	retentionDays int,
	now time.Time,
) (eval.AgentTaskReportArchiveReceipt, error) {
	if archive == nil {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("agent task report archive is unavailable")
	}
	if err := verifyEvaluationOutput(output, key, trustedKeyID); err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("verify report before archive: %w", err)
	}
	if output.Integrity == nil {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("agent task report is unsigned")
	}
	payload, err := json.Marshal(output)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("encode canonical archived report: %w", err)
	}
	if len(payload) > maxArchivedEvaluationOutputBytes {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("canonical report size %d exceeds archive limit %d", len(payload), maxArchivedEvaluationOutputBytes)
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	request := eval.AgentTaskReportArchiveRequest{
		ReportSchemaVersion: output.SchemaVersion,
		DatasetVersion:      output.Candidate.DatasetVersion,
		DatasetSHA256:       output.Candidate.DatasetSHA256,
		ExecutionConfigHash: output.Candidate.ExecutionConfigHash,
		IntegrityKeyID:      output.Integrity.KeyID,
		SignedAt:            output.Integrity.SignedAt,
		RetainUntil:         now.Add(time.Duration(retentionDays) * 24 * time.Hour),
		ReportSHA256:        sha256Hex(payload),
		Payload:             payload,
	}
	if err := archive.Ensure(ctx); err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	receipt, err := archive.PutImmutable(ctx, request)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	archivedPayload, err := archive.Get(ctx, receipt, maxArchivedEvaluationOutputBytes)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("read archived report: %w", err)
	}
	archivedOutput, err := decodeVerifiedEvaluationOutput(archivedPayload, key, trustedKeyID)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("verify archived report: %w", err)
	}
	if !bytes.Equal(payload, archivedPayload) || archivedOutput.Candidate.DatasetSHA256 != receipt.DatasetSHA256 || archivedOutput.Candidate.ExecutionConfigHash != receipt.ExecutionConfigHash || archivedOutput.Integrity == nil || archivedOutput.Integrity.KeyID != receipt.IntegrityKeyID {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("archived report evidence does not match its receipt")
	}
	return receipt, nil
}

func archiveContentQualifiedEvaluationOutput(
	ctx context.Context,
	archive eval.AgentTaskReportArchive,
	output agentTaskEvaluationOutput,
	signoff eval.AgentTaskContentReviewSignoff,
	reportKey []byte,
	reportKeyID string,
	signoffKey []byte,
	signoffKeyID string,
	retentionDays int,
	now time.Time,
) (eval.AgentTaskReportArchiveReceipt, error) {
	if archive == nil {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("agent task report archive is unavailable")
	}
	evidence, err := eval.BuildAgentTaskContentQualifiedEvidence(
		output, signoff, reportKey, reportKeyID, signoffKey, signoffKeyID,
	)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("build content-qualified evidence: %w", err)
	}
	payload, err := eval.MarshalAgentTaskContentQualifiedEvidence(evidence)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	if len(payload) > maxArchivedEvaluationOutputBytes {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("content-qualified evidence size %d exceeds archive limit %d", len(payload), maxArchivedEvaluationOutputBytes)
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	request := eval.AgentTaskReportArchiveRequest{
		ReportSchemaVersion: eval.AgentTaskContentQualifiedEvidenceSchemaVersion,
		DatasetVersion:      output.Candidate.DatasetVersion,
		DatasetSHA256:       output.Candidate.DatasetSHA256,
		ExecutionConfigHash: output.Candidate.ExecutionConfigHash,
		IntegrityKeyID:      output.Integrity.KeyID,
		SignedAt:            signoff.Integrity.SignedAt,
		RetainUntil:         now.Add(time.Duration(retentionDays) * 24 * time.Hour),
		ReportSHA256:        sha256Hex(payload),
		Payload:             payload,
	}
	if err := archive.Ensure(ctx); err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	receipt, err := archive.PutImmutable(ctx, request)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	archivedPayload, err := archive.Get(ctx, receipt, maxArchivedEvaluationOutputBytes)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("read archived content-qualified evidence: %w", err)
	}
	archived, err := eval.DecodeAndVerifyAgentTaskContentQualifiedEvidence(
		archivedPayload, reportKey, reportKeyID, signoffKey, signoffKeyID,
	)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("verify archived content-qualified evidence: %w", err)
	}
	if !bytes.Equal(payload, archivedPayload) ||
		archived.Report.Candidate.DatasetSHA256 != receipt.DatasetSHA256 ||
		archived.Report.Candidate.ExecutionConfigHash != receipt.ExecutionConfigHash ||
		archived.Report.Integrity == nil || archived.Report.Integrity.KeyID != receipt.IntegrityKeyID {
		return eval.AgentTaskReportArchiveReceipt{}, errors.New("archived content-qualified evidence does not match its receipt")
	}
	return receipt, nil
}

func verifyArchivedEvaluationOutput(
	ctx context.Context,
	archive eval.AgentTaskReportArchive,
	receipt eval.AgentTaskReportArchiveReceipt,
	key []byte,
	trustedKeyID string,
) (agentTaskEvaluationOutput, error) {
	if archive == nil {
		return agentTaskEvaluationOutput{}, errors.New("agent task report archive is unavailable")
	}
	if err := archive.Ensure(ctx); err != nil {
		return agentTaskEvaluationOutput{}, err
	}
	payload, err := archive.Get(ctx, receipt, maxArchivedEvaluationOutputBytes)
	if err != nil {
		return agentTaskEvaluationOutput{}, err
	}
	if sha256Hex(payload) != receipt.ReportSHA256 {
		return agentTaskEvaluationOutput{}, errors.New("archived report payload hash does not match receipt")
	}
	output, err := decodeVerifiedEvaluationOutput(payload, key, trustedKeyID)
	if err != nil {
		return agentTaskEvaluationOutput{}, err
	}
	if output.Integrity == nil || output.Integrity.KeyID != receipt.IntegrityKeyID || output.Candidate.DatasetVersion != receipt.DatasetVersion || output.Candidate.DatasetSHA256 != receipt.DatasetSHA256 || output.Candidate.ExecutionConfigHash != receipt.ExecutionConfigHash {
		return agentTaskEvaluationOutput{}, errors.New("archived report identity does not match receipt")
	}
	return output, nil
}

func verifyArchivedContentQualifiedEvaluationOutput(
	ctx context.Context,
	archive eval.AgentTaskReportArchive,
	receipt eval.AgentTaskReportArchiveReceipt,
	reportKey []byte,
	reportKeyID string,
	signoffKey []byte,
	signoffKeyID string,
) (eval.AgentTaskContentQualifiedEvidence, error) {
	if archive == nil {
		return eval.AgentTaskContentQualifiedEvidence{}, errors.New("agent task report archive is unavailable")
	}
	if err := archive.Ensure(ctx); err != nil {
		return eval.AgentTaskContentQualifiedEvidence{}, err
	}
	payload, err := archive.Get(ctx, receipt, maxArchivedEvaluationOutputBytes)
	if err != nil {
		return eval.AgentTaskContentQualifiedEvidence{}, err
	}
	if sha256Hex(payload) != receipt.ReportSHA256 {
		return eval.AgentTaskContentQualifiedEvidence{}, errors.New("archived content-qualified payload hash does not match receipt")
	}
	evidence, err := eval.DecodeAndVerifyAgentTaskContentQualifiedEvidence(
		payload, reportKey, reportKeyID, signoffKey, signoffKeyID,
	)
	if err != nil {
		return eval.AgentTaskContentQualifiedEvidence{}, err
	}
	output := evidence.Report
	if output.Integrity == nil || output.Integrity.KeyID != receipt.IntegrityKeyID ||
		output.Candidate.DatasetVersion != receipt.DatasetVersion ||
		output.Candidate.DatasetSHA256 != receipt.DatasetSHA256 ||
		output.Candidate.ExecutionConfigHash != receipt.ExecutionConfigHash {
		return eval.AgentTaskContentQualifiedEvidence{}, errors.New("archived content-qualified identity does not match receipt")
	}
	return evidence, nil
}

func loadAgentTaskArchiveReceipt(path string) (eval.AgentTaskReportArchiveReceipt, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	var receipt eval.AgentTaskReportArchiveReceipt
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("decode archive receipt: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return eval.AgentTaskReportArchiveReceipt{}, errors.New("archive receipt contains multiple JSON values")
		}
		return eval.AgentTaskReportArchiveReceipt{}, fmt.Errorf("decode archive receipt trailer: %w", err)
	}
	if err := eval.ValidateAgentTaskReportArchiveReceipt(receipt); err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	return receipt, nil
}

func writeAgentTaskArchiveReceipt(path string, receipt eval.AgentTaskReportArchiveReceipt) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("archive receipt output path is required")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create archive receipt directory: %w", err)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create append-only archive receipt: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(receipt); err != nil {
		_ = file.Close()
		return fmt.Errorf("write archive receipt: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync archive receipt: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close archive receipt: %w", err)
	}
	return nil
}

func ensureArchiveReceiptPathAvailable(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("archive receipt output path is required")
	}
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("archive receipt path %q already exists", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check archive receipt path: %w", err)
	}
	return nil
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
