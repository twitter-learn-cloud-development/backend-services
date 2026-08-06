package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

const agentTaskCheckpointSchemaVersion = "agent-task-eval-checkpoint/v1"

var agentTaskCheckpointFilePattern = regexp.MustCompile(`^[0-9]{6}\.json$`)

type agentTaskCheckpointIdentity struct {
	Side                string                            `json:"side"`
	DatasetVersion      string                            `json:"dataset_version"`
	DatasetSHA256       string                            `json:"dataset_sha256"`
	ExecutionConfigHash string                            `json:"execution_config_sha256"`
	Environment         string                            `json:"environment,omitempty"`
	Seed                int64                             `json:"seed"`
	CaseTimeoutMS       int64                             `json:"case_timeout_ms"`
	Execution           eval.AgentTaskExecutionDescriptor `json:"execution"`
	TotalCases          int                               `json:"total_cases"`
	GeneratedAt         time.Time                         `json:"generated_at"`
}

type agentTaskCheckpointRecord struct {
	SchemaVersion         string                         `json:"schema_version"`
	Identity              agentTaskCheckpointIdentity    `json:"identity"`
	Completed             int                            `json:"completed"`
	PreviousPayloadSHA256 string                         `json:"previous_payload_sha256,omitempty"`
	Evidence              eval.AgentTaskCaseEvidence     `json:"evidence"`
	Integrity             *eval.AgentTaskReportIntegrity `json:"integrity,omitempty"`
}

type agentTaskCheckpointStore struct {
	directory           string
	identity            agentTaskCheckpointIdentity
	key                 []byte
	keyID               string
	evidence            []eval.AgentTaskCaseEvidence
	previousPayloadHash string
}

func openAgentTaskCheckpointStore(
	root string,
	expected agentTaskCheckpointIdentity,
	key []byte,
	keyID string,
	now time.Time,
) (*agentTaskCheckpointStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("agent task checkpoint directory is required")
	}
	expected.Side = strings.TrimSpace(expected.Side)
	expected.DatasetVersion = strings.TrimSpace(expected.DatasetVersion)
	expected.DatasetSHA256 = strings.ToLower(strings.TrimSpace(expected.DatasetSHA256))
	expected.ExecutionConfigHash = strings.ToLower(strings.TrimSpace(expected.ExecutionConfigHash))
	expected.Environment = strings.TrimSpace(expected.Environment)
	keyID = strings.TrimSpace(keyID)
	if expected.Side != "candidate" && expected.Side != "stable" {
		return nil, fmt.Errorf("agent task checkpoint side %q is unsupported", expected.Side)
	}
	if expected.DatasetVersion == "" || !validAgentTaskCheckpointSHA256(expected.DatasetSHA256) ||
		!validAgentTaskCheckpointSHA256(expected.ExecutionConfigHash) || expected.TotalCases < 1 || expected.TotalCases > 999_999 || expected.CaseTimeoutMS <= 0 {
		return nil, errors.New("agent task checkpoint identity is incomplete")
	}
	if len(key) < 32 || keyID == "" {
		return nil, errors.New("agent task checkpoint requires an HMAC key and key ID")
	}
	if now.IsZero() {
		now = time.Now()
	}
	expected.GeneratedAt = now.UTC()
	store := &agentTaskCheckpointStore{
		directory: filepath.Join(root, expected.Side),
		identity:  expected,
		key:       append([]byte(nil), key...),
		keyID:     keyID,
		evidence:  make([]eval.AgentTaskCaseEvidence, 0, expected.TotalCases),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *agentTaskCheckpointStore) load() error {
	entries, err := os.ReadDir(store.directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read checkpoint directory: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			return fmt.Errorf("checkpoint directory contains unexpected subdirectory %q", entry.Name())
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".tmp") {
			return fmt.Errorf("checkpoint directory contains interrupted write %q", name)
		}
		if !agentTaskCheckpointFilePattern.MatchString(name) {
			return fmt.Errorf("checkpoint directory contains unexpected file %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > store.identity.TotalCases {
		return fmt.Errorf("checkpoint contains %d records for %d cases", len(names), store.identity.TotalCases)
	}

	var persistedIdentity *agentTaskCheckpointIdentity
	previousHash := ""
	for index, name := range names {
		expectedName := fmt.Sprintf("%06d.json", index+1)
		if name != expectedName {
			return fmt.Errorf("checkpoint sequence has a gap: expected %q, found %q", expectedName, name)
		}
		payload, readErr := os.ReadFile(filepath.Join(store.directory, name))
		if readErr != nil {
			return fmt.Errorf("read checkpoint record %s: %w", name, readErr)
		}
		record, decodeErr := decodeAgentTaskCheckpointRecord(payload)
		if decodeErr != nil {
			return fmt.Errorf("decode checkpoint record %s: %w", name, decodeErr)
		}
		if verifyErr := verifyAgentTaskCheckpointRecord(record, store.key, store.keyID); verifyErr != nil {
			return fmt.Errorf("verify checkpoint record %s: %w", name, verifyErr)
		}
		if record.Completed != index+1 || record.Identity.TotalCases != store.identity.TotalCases {
			return fmt.Errorf("checkpoint record %s has invalid progress %d/%d", name, record.Completed, record.Identity.TotalCases)
		}
		if record.PreviousPayloadSHA256 != previousHash {
			return fmt.Errorf("checkpoint record %s does not continue the signed hash chain", name)
		}
		if record.Identity.GeneratedAt.IsZero() {
			return fmt.Errorf("checkpoint record %s has no generation time", name)
		}
		if persistedIdentity == nil {
			identity := record.Identity
			persistedIdentity = &identity
			store.identity.GeneratedAt = identity.GeneratedAt
			if err := equalAgentTaskCheckpointIdentity(store.identity, identity); err != nil {
				return fmt.Errorf("checkpoint identity mismatch: %w", err)
			}
		} else if err := equalAgentTaskCheckpointIdentity(*persistedIdentity, record.Identity); err != nil {
			return fmt.Errorf("checkpoint record %s identity mismatch: %w", name, err)
		}
		store.evidence = append(store.evidence, copyAgentTaskCaseEvidence(record.Evidence))
		previousHash = record.Integrity.PayloadSHA256
	}
	store.previousPayloadHash = previousHash
	return nil
}

func (store *agentTaskCheckpointStore) Append(progress eval.AgentTaskProgress) error {
	if store == nil {
		return errors.New("agent task checkpoint store is nil")
	}
	expectedCompleted := len(store.evidence) + 1
	if progress.Completed != expectedCompleted || progress.Total != store.identity.TotalCases {
		return fmt.Errorf("checkpoint progress must be the next case: got %d/%d, want %d/%d", progress.Completed, progress.Total, expectedCompleted, store.identity.TotalCases)
	}
	record := agentTaskCheckpointRecord{
		SchemaVersion:         agentTaskCheckpointSchemaVersion,
		Identity:              store.identity,
		Completed:             progress.Completed,
		PreviousPayloadSHA256: store.previousPayloadHash,
		Evidence:              copyAgentTaskCaseEvidence(progress.Evidence),
	}
	payload, err := unsignedAgentTaskCheckpointPayload(record)
	if err != nil {
		return err
	}
	integrity, err := eval.SignAgentTaskPayload(payload, store.key, store.keyID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("sign checkpoint record: %w", err)
	}
	record.Integrity = &integrity
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode checkpoint record: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(store.directory, 0o750); err != nil {
		return fmt.Errorf("create checkpoint directory: %w", err)
	}
	name := fmt.Sprintf("%06d.json", progress.Completed)
	target := filepath.Join(store.directory, name)
	temporary := target + ".tmp"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create checkpoint record %s: %w", name, err)
	}
	cleanupTemporary := true
	defer func() {
		if cleanupTemporary {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return fmt.Errorf("write checkpoint record %s: %w", name, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync checkpoint record %s: %w", name, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close checkpoint record %s: %w", name, err)
	}
	if err := os.Link(temporary, target); err != nil {
		return fmt.Errorf("commit checkpoint record %s: %w", name, err)
	}
	if err := os.Remove(temporary); err != nil {
		return fmt.Errorf("remove committed checkpoint temporary file %s: %w", name, err)
	}
	cleanupTemporary = false
	store.evidence = append(store.evidence, copyAgentTaskCaseEvidence(progress.Evidence))
	store.previousPayloadHash = integrity.PayloadSHA256
	return nil
}

func (store *agentTaskCheckpointStore) ResumeCases() []eval.AgentTaskCaseEvidence {
	if store == nil {
		return nil
	}
	result := make([]eval.AgentTaskCaseEvidence, len(store.evidence))
	for index := range store.evidence {
		result[index] = copyAgentTaskCaseEvidence(store.evidence[index])
	}
	return result
}

func (store *agentTaskCheckpointStore) GeneratedAt() time.Time {
	if store == nil {
		return time.Time{}
	}
	return store.identity.GeneratedAt
}

func decodeAgentTaskCheckpointRecord(payload []byte) (agentTaskCheckpointRecord, error) {
	var record agentTaskCheckpointRecord
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return agentTaskCheckpointRecord{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return agentTaskCheckpointRecord{}, errors.New("checkpoint record contains multiple JSON values")
		}
		return agentTaskCheckpointRecord{}, fmt.Errorf("decode checkpoint record trailer: %w", err)
	}
	return record, nil
}

func verifyAgentTaskCheckpointRecord(record agentTaskCheckpointRecord, key []byte, trustedKeyID string) error {
	if record.SchemaVersion != agentTaskCheckpointSchemaVersion {
		return fmt.Errorf("unsupported checkpoint schema version %q", record.SchemaVersion)
	}
	if record.Integrity == nil {
		return errors.New("checkpoint record is unsigned")
	}
	if trustedKeyID = strings.TrimSpace(trustedKeyID); trustedKeyID != "" && record.Integrity.KeyID != trustedKeyID {
		return fmt.Errorf("checkpoint key ID %q does not match trusted key ID %q", record.Integrity.KeyID, trustedKeyID)
	}
	payload, err := unsignedAgentTaskCheckpointPayload(record)
	if err != nil {
		return err
	}
	return eval.VerifyAgentTaskPayload(payload, key, *record.Integrity)
}

func unsignedAgentTaskCheckpointPayload(record agentTaskCheckpointRecord) ([]byte, error) {
	record.Integrity = nil
	payload, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode unsigned checkpoint record: %w", err)
	}
	return payload, nil
}

func equalAgentTaskCheckpointIdentity(left, right agentTaskCheckpointIdentity) error {
	leftHash, err := eval.HashCanonicalJSON(left)
	if err != nil {
		return err
	}
	rightHash, err := eval.HashCanonicalJSON(right)
	if err != nil {
		return err
	}
	if leftHash != rightHash {
		return fmt.Errorf("expected identity %s, found %s", leftHash, rightHash)
	}
	return nil
}

func copyAgentTaskCaseEvidence(evidence eval.AgentTaskCaseEvidence) eval.AgentTaskCaseEvidence {
	result := evidence.Result
	result.ExpectedTools = append([]string(nil), result.ExpectedTools...)
	result.AllowedTools = append([]string(nil), result.AllowedTools...)
	result.SelectedTools = append([]string(nil), result.SelectedTools...)
	result.SemanticFailureCodes = append([]string(nil), result.SemanticFailureCodes...)
	evidence.Result = result
	return evidence
}

func validAgentTaskCheckpointSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
