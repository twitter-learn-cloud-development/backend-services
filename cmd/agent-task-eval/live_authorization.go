package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"twitter-clone/internal/module/agent/eval"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	agentTaskLiveAuthorizationSchemaVersion = eval.AgentTaskLiveAuthorizationSchemaVersion
	agentTaskLiveLedgerRecordSchemaVersion  = "agent-task-live-authorization-ledger/v1"
	maxAgentTaskLiveAuthorizationLifetime   = 7 * 24 * time.Hour
	maxAgentTaskLiveAuthorizationCostMicros = int64(1_000_000_000_000)
	maxAgentTaskLiveAuthorizationEvents     = 100_000
)

var (
	agentTaskLiveAuthorizationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	agentTaskLiveLedgerFilePattern      = regexp.MustCompile(`^[0-9]{6}\.json$`)
)

type agentTaskLiveAuthorizationLimits struct {
	MaxRuns                int   `json:"max_runs"`
	MaxProviderCalls       int   `json:"max_provider_calls"`
	MaxCapturedOutputs     int   `json:"max_captured_outputs"`
	MaxEstimatedCostMicros int64 `json:"max_estimated_cost_micros"`
}

type agentTaskLiveAuthorization struct {
	SchemaVersion         string                           `json:"schema_version"`
	AuthorizationID       string                           `json:"authorization_id"`
	IssuedAt              time.Time                        `json:"issued_at"`
	ExpiresAt             time.Time                        `json:"expires_at"`
	Provider              string                           `json:"provider"`
	Model                 string                           `json:"model"`
	DatasetVersion        string                           `json:"dataset_version"`
	DatasetSHA256         string                           `json:"dataset_sha256"`
	ExecutionConfigSHA256 string                           `json:"execution_config_sha256"`
	Limits                agentTaskLiveAuthorizationLimits `json:"limits"`
	Integrity             *eval.AgentTaskReportIntegrity   `json:"integrity,omitempty"`
}

type agentTaskLiveAuthorizationBinding struct {
	Provider              string
	Model                 string
	DatasetVersion        string
	DatasetSHA256         string
	ExecutionConfigSHA256 string
}

type agentTaskLiveLedgerRecord struct {
	SchemaVersion              string                         `json:"schema_version"`
	AuthorizationID            string                         `json:"authorization_id"`
	AuthorizationPayloadSHA256 string                         `json:"authorization_payload_sha256"`
	Sequence                   int                            `json:"sequence"`
	PreviousPayloadSHA256      string                         `json:"previous_payload_sha256,omitempty"`
	InvocationID               string                         `json:"invocation_id"`
	EventType                  string                         `json:"event_type"`
	SubjectSHA256              string                         `json:"subject_sha256,omitempty"`
	Runs                       int                            `json:"runs,omitempty"`
	ProviderCalls              int                            `json:"provider_calls,omitempty"`
	CapturedOutputs            int                            `json:"captured_outputs,omitempty"`
	EstimatedCostMicros        int64                          `json:"estimated_cost_micros,omitempty"`
	CreatedAt                  time.Time                      `json:"created_at"`
	Integrity                  *eval.AgentTaskReportIntegrity `json:"integrity,omitempty"`
}

type agentTaskLiveAuthorizationUsage struct {
	Runs                int
	ProviderCalls       int
	CapturedOutputs     int
	EstimatedCostMicros int64
}

type agentTaskLiveAuthorizationBudget interface {
	Evidence() eval.AgentTaskLiveAuthorizationEvidence
	ReserveProviderCallContext(context.Context, string, int64, time.Time) error
}

type agentTaskLiveAuthorizationLedger struct {
	mu                       sync.Mutex
	directory                string
	authorization            agentTaskLiveAuthorization
	authorizationPayloadHash string
	key                      []byte
	keyID                    string
	invocationID             string
}

type authorizedLiveModelClient struct {
	delegate        agentRuntime.ModelClient
	ledger          agentTaskLiveAuthorizationBudget
	costEstimator   agentRuntime.CostEstimator
	tokenCounter    agentRuntime.TokenCounter
	model           string
	maxOutputTokens int
	allowZeroCost   bool
}

type createAgentTaskLiveAuthorizationCommand struct {
	OutputPath         string
	AuthorizationID    string
	TTL                time.Duration
	DatasetPath        string
	DatasetVersion     string
	RuntimeConfigPath  string
	StrategyConfigPath string
	Limits             agentTaskLiveAuthorizationLimits
	Key                []byte
	KeyID              string
	Now                time.Time
}

func runCreateAgentTaskLiveAuthorization(command createAgentTaskLiveAuthorizationCommand) (agentTaskLiveAuthorization, error) {
	command.OutputPath = strings.TrimSpace(command.OutputPath)
	command.RuntimeConfigPath = strings.TrimSpace(command.RuntimeConfigPath)
	command.StrategyConfigPath = strings.TrimSpace(command.StrategyConfigPath)
	if command.OutputPath == "" || (command.RuntimeConfigPath == "") == (command.StrategyConfigPath == "") {
		return agentTaskLiveAuthorization{}, errors.New("live authorization creation requires an output and exactly one runtime config")
	}
	if command.TTL <= 0 || command.TTL > maxAgentTaskLiveAuthorizationLifetime {
		return agentTaskLiveAuthorization{}, fmt.Errorf("live authorization TTL must be between 1ns and %s", maxAgentTaskLiveAuthorizationLifetime)
	}
	if err := ensureReviewPathAvailable(command.OutputPath, "live authorization"); err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	dataset, err := loadAgentTaskDataset(command.DatasetPath)
	if err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	datasetHash, err := eval.HashAgentTaskDataset(dataset)
	if err != nil {
		return agentTaskLiveAuthorization{}, fmt.Errorf("hash live authorization dataset: %w", err)
	}
	provider := ""
	model := ""
	configHash := ""
	var plan agentTaskLivePlan
	if command.RuntimeConfigPath != "" {
		config, err := loadRuntimeEvalConfig(command.RuntimeConfigPath)
		if err != nil {
			return agentTaskLiveAuthorization{}, fmt.Errorf("load runtime evaluation config: %w", err)
		}
		plan, err = buildRuntimeAgentTaskLivePlan(dataset, command.DatasetVersion, config)
		if err != nil {
			return agentTaskLiveAuthorization{}, err
		}
		provider, model, configHash = plan.Provider, plan.Model, plan.ExecutionConfigSHA256
	} else {
		config, err := loadStrategyRuntimeEvalConfig(command.StrategyConfigPath)
		if err != nil {
			return agentTaskLiveAuthorization{}, fmt.Errorf("load strategy runtime evaluation config: %w", err)
		}
		plan, err = buildStrategyAgentTaskLivePlan(dataset, command.DatasetVersion, config)
		if err != nil {
			return agentTaskLiveAuthorization{}, err
		}
		provider, model, configHash = plan.Provider, plan.Model, plan.ExecutionConfigSHA256
	}
	if err := validateAgentTaskLiveAuthorizationPlanCoverage(command.Limits, plan); err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	if command.Now.IsZero() {
		command.Now = time.Now().UTC()
	}
	authorization, err := buildAndSignAgentTaskLiveAuthorization(agentTaskLiveAuthorization{
		AuthorizationID: command.AuthorizationID,
		ExpiresAt:       command.Now.UTC().Add(command.TTL),
		Provider:        provider, Model: model,
		DatasetVersion: strings.TrimSpace(command.DatasetVersion), DatasetSHA256: datasetHash,
		ExecutionConfigSHA256: configHash, Limits: command.Limits,
	}, command.Key, command.KeyID, command.Now)
	if err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	if err := writeAgentTaskLiveAuthorization(command.OutputPath, authorization); err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	return authorization, nil
}

func buildAndSignAgentTaskLiveAuthorization(
	authorization agentTaskLiveAuthorization,
	key []byte,
	keyID string,
	now time.Time,
) (agentTaskLiveAuthorization, error) {
	if now.IsZero() {
		now = time.Now()
	}
	authorization.SchemaVersion = agentTaskLiveAuthorizationSchemaVersion
	authorization.IssuedAt = now.UTC()
	authorization.Integrity = nil
	authorization = normalizeAgentTaskLiveAuthorization(authorization)
	if err := validateAgentTaskLiveAuthorization(authorization, now.UTC(), false); err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	keyID = strings.TrimSpace(keyID)
	if len(key) < 32 || keyID == "" {
		return agentTaskLiveAuthorization{}, errors.New("live authorization requires an HMAC key and key ID")
	}
	payload, err := unsignedAgentTaskLiveAuthorizationPayload(authorization)
	if err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	integrity, err := eval.SignAgentTaskPayload(payload, key, keyID, authorization.IssuedAt)
	if err != nil {
		return agentTaskLiveAuthorization{}, fmt.Errorf("sign live authorization: %w", err)
	}
	authorization.Integrity = &integrity
	return authorization, nil
}

func loadAndVerifyAgentTaskLiveAuthorization(
	path string,
	key []byte,
	trustedKeyID string,
	binding agentTaskLiveAuthorizationBinding,
	now time.Time,
) (agentTaskLiveAuthorization, error) {
	authorization, err := loadAndVerifyAgentTaskLiveAuthorizationDocument(path, key, trustedKeyID, now)
	if err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	if err := authorization.matches(binding); err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	return authorization, nil
}

func loadAndVerifyAgentTaskLiveAuthorizationDocument(
	path string,
	key []byte,
	trustedKeyID string,
	now time.Time,
) (agentTaskLiveAuthorization, error) {
	payload, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return agentTaskLiveAuthorization{}, fmt.Errorf("read live authorization: %w", err)
	}
	var authorization agentTaskLiveAuthorization
	if err := decodeStrictJSON(payload, &authorization, "live authorization"); err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	authorization = normalizeAgentTaskLiveAuthorization(authorization)
	if err := validateAgentTaskLiveAuthorization(authorization, now.UTC(), true); err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	if authorization.Integrity == nil {
		return agentTaskLiveAuthorization{}, errors.New("live authorization is unsigned")
	}
	trustedKeyID = strings.TrimSpace(trustedKeyID)
	if len(key) < 32 || trustedKeyID == "" {
		return agentTaskLiveAuthorization{}, errors.New("live authorization verification requires an HMAC key and key ID")
	}
	if authorization.Integrity.KeyID != trustedKeyID {
		return agentTaskLiveAuthorization{}, fmt.Errorf("live authorization key ID %q does not match trusted key ID %q", authorization.Integrity.KeyID, trustedKeyID)
	}
	if !authorization.Integrity.SignedAt.Equal(authorization.IssuedAt) {
		return agentTaskLiveAuthorization{}, errors.New("live authorization issuance and signature times differ")
	}
	unsigned, err := unsignedAgentTaskLiveAuthorizationPayload(authorization)
	if err != nil {
		return agentTaskLiveAuthorization{}, err
	}
	if err := eval.VerifyAgentTaskPayload(unsigned, key, *authorization.Integrity); err != nil {
		return agentTaskLiveAuthorization{}, fmt.Errorf("verify live authorization integrity: %w", err)
	}
	return authorization, nil
}

func normalizeAgentTaskLiveAuthorization(authorization agentTaskLiveAuthorization) agentTaskLiveAuthorization {
	authorization.SchemaVersion = strings.TrimSpace(authorization.SchemaVersion)
	authorization.AuthorizationID = strings.TrimSpace(authorization.AuthorizationID)
	authorization.Provider = strings.TrimSpace(authorization.Provider)
	authorization.Model = strings.TrimSpace(authorization.Model)
	authorization.DatasetVersion = strings.TrimSpace(authorization.DatasetVersion)
	authorization.DatasetSHA256 = strings.ToLower(strings.TrimSpace(authorization.DatasetSHA256))
	authorization.ExecutionConfigSHA256 = strings.ToLower(strings.TrimSpace(authorization.ExecutionConfigSHA256))
	authorization.IssuedAt = authorization.IssuedAt.UTC()
	authorization.ExpiresAt = authorization.ExpiresAt.UTC()
	return authorization
}

func validateAgentTaskLiveAuthorization(authorization agentTaskLiveAuthorization, now time.Time, enforceWindow bool) error {
	if authorization.SchemaVersion != agentTaskLiveAuthorizationSchemaVersion {
		return fmt.Errorf("unsupported live authorization schema version %q", authorization.SchemaVersion)
	}
	if !agentTaskLiveAuthorizationIDPattern.MatchString(authorization.AuthorizationID) {
		return errors.New("live authorization ID must be a 1-128 character portable identifier")
	}
	if !validAgentTaskLiveAuthorizationLabel(authorization.Provider) || !validAgentTaskLiveAuthorizationLabel(authorization.Model) ||
		!validAgentTaskLiveAuthorizationLabel(authorization.DatasetVersion) {
		return errors.New("live authorization provider, model and dataset version are required bounded labels")
	}
	if !validAgentTaskLiveAuthorizationSHA256(authorization.DatasetSHA256) ||
		!validAgentTaskLiveAuthorizationSHA256(authorization.ExecutionConfigSHA256) {
		return errors.New("live authorization dataset and execution config digests are invalid")
	}
	if authorization.IssuedAt.IsZero() || authorization.ExpiresAt.IsZero() || !authorization.ExpiresAt.After(authorization.IssuedAt) {
		return errors.New("live authorization validity interval is invalid")
	}
	if authorization.ExpiresAt.Sub(authorization.IssuedAt) > maxAgentTaskLiveAuthorizationLifetime {
		return fmt.Errorf("live authorization lifetime exceeds %s", maxAgentTaskLiveAuthorizationLifetime)
	}
	limits := authorization.Limits
	if limits.MaxRuns < 1 || limits.MaxRuns > 100 || limits.MaxProviderCalls < 1 || limits.MaxProviderCalls > maxAgentTaskLiveAuthorizationEvents ||
		limits.MaxCapturedOutputs < 0 || limits.MaxCapturedOutputs > maxAgentTaskLiveAuthorizationEvents ||
		limits.MaxEstimatedCostMicros < 0 || limits.MaxEstimatedCostMicros > maxAgentTaskLiveAuthorizationCostMicros {
		return errors.New("live authorization limits are outside supported bounds")
	}
	if enforceWindow {
		if now.IsZero() {
			now = time.Now().UTC()
		}
		if now.Before(authorization.IssuedAt.Add(-time.Minute)) {
			return errors.New("live authorization is not active yet")
		}
		if !now.Before(authorization.ExpiresAt) {
			return errors.New("live authorization has expired")
		}
	}
	return nil
}

func (authorization agentTaskLiveAuthorization) matches(binding agentTaskLiveAuthorizationBinding) error {
	binding.Provider = strings.TrimSpace(binding.Provider)
	binding.Model = strings.TrimSpace(binding.Model)
	binding.DatasetVersion = strings.TrimSpace(binding.DatasetVersion)
	binding.DatasetSHA256 = strings.ToLower(strings.TrimSpace(binding.DatasetSHA256))
	binding.ExecutionConfigSHA256 = strings.ToLower(strings.TrimSpace(binding.ExecutionConfigSHA256))
	if authorization.Provider != binding.Provider || authorization.Model != binding.Model ||
		authorization.DatasetVersion != binding.DatasetVersion || authorization.DatasetSHA256 != binding.DatasetSHA256 ||
		authorization.ExecutionConfigSHA256 != binding.ExecutionConfigSHA256 {
		return errors.New("live authorization does not match provider, model, dataset or execution config identity")
	}
	return nil
}

func unsignedAgentTaskLiveAuthorizationPayload(authorization agentTaskLiveAuthorization) ([]byte, error) {
	authorization.Integrity = nil
	payload, err := json.Marshal(authorization)
	if err != nil {
		return nil, fmt.Errorf("encode unsigned live authorization: %w", err)
	}
	return payload, nil
}

func writeAgentTaskLiveAuthorization(path string, authorization agentTaskLiveAuthorization) error {
	return writeExclusiveReviewJSON(path, authorization, "live authorization")
}

func readAgentTaskLiveAuthorizationKey(envName, keyID string) ([]byte, error) {
	envName = strings.TrimSpace(envName)
	keyID = strings.TrimSpace(keyID)
	if envName == "" || keyID == "" {
		return nil, errors.New("live authorization key environment variable and key ID are required")
	}
	key := []byte(os.Getenv(envName))
	if len(key) < 32 {
		return nil, fmt.Errorf("live authorization key environment variable %q must contain at least 32 bytes", envName)
	}
	return key, nil
}

func newAgentTaskLiveAuthorizationLedger(
	root string,
	authorization agentTaskLiveAuthorization,
	key []byte,
	keyID string,
) (*agentTaskLiveAuthorizationLedger, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("live authorization state directory is required")
	}
	if authorization.Integrity == nil {
		return nil, errors.New("live authorization ledger requires a signed authorization")
	}
	payload, err := unsignedAgentTaskLiveAuthorizationPayload(authorization)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(payload)
	invocationID, err := randomAgentTaskLiveAuthorizationID()
	if err != nil {
		return nil, err
	}
	ledger := &agentTaskLiveAuthorizationLedger{
		directory:     filepath.Join(root, authorization.AuthorizationID),
		authorization: authorization, authorizationPayloadHash: hex.EncodeToString(digest[:]),
		key: append([]byte(nil), key...), keyID: strings.TrimSpace(keyID), invocationID: invocationID,
	}
	if len(ledger.key) < 32 || ledger.keyID == "" || ledger.keyID != authorization.Integrity.KeyID {
		return nil, errors.New("live authorization ledger key does not match authorization integrity identity")
	}
	if _, _, err := ledger.load(); err != nil {
		return nil, err
	}
	return ledger, nil
}

func openAndReserveAgentTaskLiveAuthorization(
	authorizationPath string,
	stateRoot string,
	key []byte,
	keyID string,
	binding agentTaskLiveAuthorizationBinding,
	capturedOutputs int,
	now time.Time,
) (*agentTaskLiveAuthorizationLedger, error) {
	authorization, err := loadAndVerifyAgentTaskLiveAuthorization(authorizationPath, key, keyID, binding, now)
	if err != nil {
		return nil, err
	}
	ledger, err := newAgentTaskLiveAuthorizationLedger(stateRoot, authorization, key, keyID)
	if err != nil {
		return nil, err
	}
	if err := ledger.ReserveRun(capturedOutputs, now); err != nil {
		return nil, err
	}
	return ledger, nil
}

func (ledger *agentTaskLiveAuthorizationLedger) Evidence() eval.AgentTaskLiveAuthorizationEvidence {
	if ledger == nil {
		return eval.AgentTaskLiveAuthorizationEvidence{}
	}
	limits := ledger.authorization.Limits
	return eval.AgentTaskLiveAuthorizationEvidence{
		SchemaVersion:              agentTaskLiveAuthorizationSchemaVersion,
		AuthorizationID:            ledger.authorization.AuthorizationID,
		AuthorizationPayloadSHA256: ledger.authorizationPayloadHash,
		AuthorizationKeyID:         ledger.keyID,
		InvocationSHA256:           hashAgentTaskLiveAuthorizationSubject(ledger.invocationID),
		Limits: eval.AgentTaskLiveAuthorizationLimits{
			MaxRuns: limits.MaxRuns, MaxProviderCalls: limits.MaxProviderCalls,
			MaxCapturedOutputs:     limits.MaxCapturedOutputs,
			MaxEstimatedCostMicros: limits.MaxEstimatedCostMicros,
		},
	}
}

func (ledger *agentTaskLiveAuthorizationLedger) ReserveRun(capturedOutputs int, now time.Time) error {
	if capturedOutputs < 0 {
		return errors.New("captured output reservation cannot be negative")
	}
	return ledger.appendReservation(agentTaskLiveLedgerRecord{
		EventType: "run_reserved", Runs: 1, CapturedOutputs: capturedOutputs,
		SubjectSHA256: hashAgentTaskLiveAuthorizationSubject(ledger.invocationID), CreatedAt: now.UTC(),
	})
}

func (ledger *agentTaskLiveAuthorizationLedger) ReserveProviderCall(subject string, estimatedCostMicros int64, now time.Time) error {
	if estimatedCostMicros < 0 {
		return errors.New("provider call cost reservation cannot be negative")
	}
	return ledger.appendReservation(agentTaskLiveLedgerRecord{
		EventType: "provider_call_reserved", ProviderCalls: 1, EstimatedCostMicros: estimatedCostMicros,
		SubjectSHA256: hashAgentTaskLiveAuthorizationSubject(subject), CreatedAt: now.UTC(),
	})
}

func (ledger *agentTaskLiveAuthorizationLedger) ReserveProviderCallContext(
	ctx context.Context,
	subject string,
	estimatedCostMicros int64,
	now time.Time,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ledger.ReserveProviderCall(subject, estimatedCostMicros, now)
}

func (ledger *agentTaskLiveAuthorizationLedger) appendReservation(record agentTaskLiveLedgerRecord) error {
	if ledger == nil {
		return errors.New("live authorization ledger is nil")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	if err := ledger.validateEventTime(record.CreatedAt); err != nil {
		return err
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	for attempt := 0; attempt < 32; attempt++ {
		records, usage, err := ledger.load()
		if err != nil {
			return err
		}
		if err := ledger.admit(usage, record); err != nil {
			return err
		}
		record.SchemaVersion = agentTaskLiveLedgerRecordSchemaVersion
		record.AuthorizationID = ledger.authorization.AuthorizationID
		record.AuthorizationPayloadSHA256 = ledger.authorizationPayloadHash
		record.Sequence = len(records) + 1
		record.InvocationID = ledger.invocationID
		if len(records) > 0 {
			record.PreviousPayloadSHA256 = records[len(records)-1].Integrity.PayloadSHA256
		}
		payload, err := unsignedAgentTaskLiveLedgerRecordPayload(record)
		if err != nil {
			return err
		}
		integrity, err := eval.SignAgentTaskPayload(payload, ledger.key, ledger.keyID, record.CreatedAt)
		if err != nil {
			return fmt.Errorf("sign live authorization ledger record: %w", err)
		}
		record.Integrity = &integrity
		committed, err := ledger.commit(record)
		if err != nil {
			return err
		}
		if committed {
			return nil
		}
	}
	return errors.New("live authorization ledger remained contended after 32 attempts")
}

func (ledger *agentTaskLiveAuthorizationLedger) validateEventTime(createdAt time.Time) error {
	if createdAt.Before(ledger.authorization.IssuedAt) || !createdAt.Before(ledger.authorization.ExpiresAt) {
		return fmt.Errorf(
			"live authorization event time %s is outside validity window [%s, %s)",
			createdAt.Format(time.RFC3339Nano),
			ledger.authorization.IssuedAt.Format(time.RFC3339Nano),
			ledger.authorization.ExpiresAt.Format(time.RFC3339Nano),
		)
	}
	return nil
}

func (ledger *agentTaskLiveAuthorizationLedger) admit(current agentTaskLiveAuthorizationUsage, incoming agentTaskLiveLedgerRecord) error {
	limits := ledger.authorization.Limits
	if incoming.Runs < 0 || incoming.ProviderCalls < 0 || incoming.CapturedOutputs < 0 || incoming.EstimatedCostMicros < 0 {
		return errors.New("live authorization reservation cannot decrement usage")
	}
	if current.Runs+incoming.Runs > limits.MaxRuns {
		return fmt.Errorf("live authorization run budget exhausted: %d > %d", current.Runs+incoming.Runs, limits.MaxRuns)
	}
	if current.ProviderCalls+incoming.ProviderCalls > limits.MaxProviderCalls {
		return fmt.Errorf("live authorization provider call budget exhausted: %d > %d", current.ProviderCalls+incoming.ProviderCalls, limits.MaxProviderCalls)
	}
	if current.CapturedOutputs+incoming.CapturedOutputs > limits.MaxCapturedOutputs {
		return fmt.Errorf("live authorization captured output budget exhausted: %d > %d", current.CapturedOutputs+incoming.CapturedOutputs, limits.MaxCapturedOutputs)
	}
	if incoming.EstimatedCostMicros > math.MaxInt64-current.EstimatedCostMicros ||
		current.EstimatedCostMicros+incoming.EstimatedCostMicros > limits.MaxEstimatedCostMicros {
		return fmt.Errorf("live authorization estimated cost budget exhausted: %d + %d > %d", current.EstimatedCostMicros, incoming.EstimatedCostMicros, limits.MaxEstimatedCostMicros)
	}
	return nil
}

func (ledger *agentTaskLiveAuthorizationLedger) load() ([]agentTaskLiveLedgerRecord, agentTaskLiveAuthorizationUsage, error) {
	entries, err := os.ReadDir(ledger.directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, agentTaskLiveAuthorizationUsage{}, nil
	}
	if err != nil {
		return nil, agentTaskLiveAuthorizationUsage{}, fmt.Errorf("read live authorization ledger: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !agentTaskLiveLedgerFilePattern.MatchString(entry.Name()) {
			return nil, agentTaskLiveAuthorizationUsage{}, fmt.Errorf("live authorization ledger contains unexpected entry %q", entry.Name())
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) > maxAgentTaskLiveAuthorizationEvents {
		return nil, agentTaskLiveAuthorizationUsage{}, errors.New("live authorization ledger exceeds the supported event limit")
	}
	records := make([]agentTaskLiveLedgerRecord, 0, len(names))
	usage := agentTaskLiveAuthorizationUsage{}
	previousHash := ""
	for index, name := range names {
		expectedName := fmt.Sprintf("%06d.json", index+1)
		if name != expectedName {
			return nil, agentTaskLiveAuthorizationUsage{}, fmt.Errorf("live authorization ledger sequence has a gap: expected %q, found %q", expectedName, name)
		}
		payload, err := os.ReadFile(filepath.Join(ledger.directory, name))
		if err != nil {
			return nil, agentTaskLiveAuthorizationUsage{}, fmt.Errorf("read live authorization ledger record %s: %w", name, err)
		}
		var record agentTaskLiveLedgerRecord
		if err := decodeStrictJSON(payload, &record, "live authorization ledger record"); err != nil {
			return nil, agentTaskLiveAuthorizationUsage{}, fmt.Errorf("decode record %s: %w", name, err)
		}
		if err := ledger.verifyRecord(record, index+1, previousHash); err != nil {
			return nil, agentTaskLiveAuthorizationUsage{}, fmt.Errorf("verify record %s: %w", name, err)
		}
		usage.Runs += record.Runs
		usage.ProviderCalls += record.ProviderCalls
		usage.CapturedOutputs += record.CapturedOutputs
		if record.EstimatedCostMicros > math.MaxInt64-usage.EstimatedCostMicros {
			return nil, agentTaskLiveAuthorizationUsage{}, errors.New("live authorization ledger cost usage overflow")
		}
		usage.EstimatedCostMicros += record.EstimatedCostMicros
		if err := ledger.admit(agentTaskLiveAuthorizationUsage{}, agentTaskLiveLedgerRecord{
			Runs: usage.Runs, ProviderCalls: usage.ProviderCalls, CapturedOutputs: usage.CapturedOutputs,
			EstimatedCostMicros: usage.EstimatedCostMicros,
		}); err != nil {
			return nil, agentTaskLiveAuthorizationUsage{}, fmt.Errorf("persisted live authorization usage exceeds limits: %w", err)
		}
		records = append(records, record)
		previousHash = record.Integrity.PayloadSHA256
	}
	return records, usage, nil
}

func (ledger *agentTaskLiveAuthorizationLedger) verifyRecord(record agentTaskLiveLedgerRecord, sequence int, previousHash string) error {
	if record.SchemaVersion != agentTaskLiveLedgerRecordSchemaVersion || record.AuthorizationID != ledger.authorization.AuthorizationID ||
		record.AuthorizationPayloadSHA256 != ledger.authorizationPayloadHash || record.Sequence != sequence ||
		record.PreviousPayloadSHA256 != previousHash || record.Integrity == nil {
		return errors.New("live authorization ledger identity is invalid")
	}
	if record.EventType != "run_reserved" && record.EventType != "provider_call_reserved" {
		return fmt.Errorf("unsupported live authorization event type %q", record.EventType)
	}
	if record.InvocationID == "" || record.CreatedAt.IsZero() || !record.CreatedAt.Equal(record.Integrity.SignedAt) {
		return errors.New("live authorization ledger event time or invocation identity is invalid")
	}
	if err := ledger.validateEventTime(record.CreatedAt); err != nil {
		return err
	}
	if record.Integrity.KeyID != ledger.keyID {
		return errors.New("live authorization ledger key ID does not match the trusted authorization key")
	}
	payload, err := unsignedAgentTaskLiveLedgerRecordPayload(record)
	if err != nil {
		return err
	}
	return eval.VerifyAgentTaskPayload(payload, ledger.key, *record.Integrity)
}

func (ledger *agentTaskLiveAuthorizationLedger) commit(record agentTaskLiveLedgerRecord) (bool, error) {
	if err := os.MkdirAll(ledger.directory, 0o750); err != nil {
		return false, fmt.Errorf("create live authorization ledger directory: %w", err)
	}
	pendingDirectory := ledger.directory + ".pending"
	if err := os.MkdirAll(pendingDirectory, 0o700); err != nil {
		return false, fmt.Errorf("create live authorization pending directory: %w", err)
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode live authorization ledger record: %w", err)
	}
	encoded = append(encoded, '\n')
	target := filepath.Join(ledger.directory, fmt.Sprintf("%06d.json", record.Sequence))
	temporary := filepath.Join(pendingDirectory, fmt.Sprintf("%06d.%s.tmp", record.Sequence, ledger.invocationID))
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, fmt.Errorf("create live authorization temporary record: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("write live authorization ledger record: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("sync live authorization ledger record: %w", err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("close live authorization ledger record: %w", err)
	}
	if err := os.Link(temporary, target); err != nil {
		if _, statErr := os.Stat(target); statErr == nil {
			return false, nil
		}
		return false, fmt.Errorf("commit live authorization ledger record: %w", err)
	}
	if err := os.Remove(temporary); err != nil {
		return false, fmt.Errorf("remove live authorization temporary record: %w", err)
	}
	cleanup = false
	return true, nil
}

func unsignedAgentTaskLiveLedgerRecordPayload(record agentTaskLiveLedgerRecord) ([]byte, error) {
	record.Integrity = nil
	payload, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode unsigned live authorization ledger record: %w", err)
	}
	return payload, nil
}

func newAuthorizedLiveModelClient(
	delegate agentRuntime.ModelClient,
	ledger agentTaskLiveAuthorizationBudget,
	estimator agentRuntime.CostEstimator,
	model string,
	maxOutputTokens int,
	allowZeroCost bool,
) (agentRuntime.ModelClient, error) {
	if delegate == nil || ledger == nil || estimator == nil || strings.TrimSpace(model) == "" || maxOutputTokens <= 0 {
		return nil, errors.New("authorized live model client configuration is incomplete")
	}
	return &authorizedLiveModelClient{
		delegate: delegate, ledger: ledger, costEstimator: estimator,
		tokenCounter: agentRuntime.NewHeuristicTokenCounter(), model: strings.TrimSpace(model),
		maxOutputTokens: maxOutputTokens, allowZeroCost: allowZeroCost,
	}, nil
}

func (client *authorizedLiveModelClient) Complete(ctx context.Context, request agentRuntime.ModelRequest) (agentRuntime.ModelResponse, error) {
	if client == nil || client.delegate == nil || client.ledger == nil || client.costEstimator == nil || client.tokenCounter == nil {
		return agentRuntime.ModelResponse{}, errors.New("authorized live model client is not configured")
	}
	usage := client.tokenCounter.EstimateRequest(request)
	if upper := conservativeAgentTaskLiveRequestTokens(request); upper > usage.InputTokens {
		usage.InputTokens = upper
		usage.TotalTokens = upper
	}
	outputTokens := request.MaxOutputTokens
	if outputTokens <= 0 || outputTokens > client.maxOutputTokens {
		outputTokens = client.maxOutputTokens
	}
	usage.OutputTokens = outputTokens
	usage.TotalTokens += outputTokens
	estimate, err := client.costEstimator.EstimateCost(client.model, usage)
	if err != nil {
		return agentRuntime.ModelResponse{}, fmt.Errorf("estimate authorized live provider call cost: %w", err)
	}
	if estimate.Micros < 0 || (estimate.Micros == 0 && !client.allowZeroCost) {
		return agentRuntime.ModelResponse{}, errors.New("authorized live provider call has no positive configured cost estimate")
	}
	if err := client.ledger.ReserveProviderCallContext(ctx, request.Context.RunID, estimate.Micros, time.Now().UTC()); err != nil {
		return agentRuntime.ModelResponse{}, fmt.Errorf("reserve authorized live provider call: %w", err)
	}
	return client.delegate.Complete(ctx, request)
}

func conservativeAgentTaskLiveRequestTokens(request agentRuntime.ModelRequest) int {
	total := 64
	add := func(value string) {
		if len(value) > math.MaxInt-total {
			total = math.MaxInt
			return
		}
		total += len(value)
	}
	for _, message := range request.Messages {
		add(string(message.Role))
		add(message.Content)
		add(message.Name)
		add(message.ToolCallID)
		for _, action := range message.Actions {
			add(action.ID)
			add(string(action.Type))
			add(action.Name)
			add(action.Content)
			add(string(action.Arguments))
		}
	}
	for _, tool := range request.Tools {
		add(tool.Name)
		add(tool.Description)
		add(string(tool.InputSchema))
	}
	return total
}

func validAgentTaskLiveAuthorizationLabel(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func validAgentTaskLiveAuthorizationSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func hashAgentTaskLiveAuthorizationSubject(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(digest[:])
}

func randomAgentTaskLiveAuthorizationID() (string, error) {
	payload := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, payload); err != nil {
		return "", fmt.Errorf("generate live authorization invocation ID: %w", err)
	}
	return hex.EncodeToString(payload), nil
}

func sameAgentTaskLiveAuthorizationKey(left []byte, leftID string, right []byte, rightID string) bool {
	return strings.TrimSpace(leftID) != "" && strings.TrimSpace(leftID) == strings.TrimSpace(rightID) ||
		len(left) > 0 && len(right) > 0 && bytes.Equal(left, right)
}
