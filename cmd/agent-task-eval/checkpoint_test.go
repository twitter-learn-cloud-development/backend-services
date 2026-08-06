package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

func TestAgentTaskCheckpointStorePersistsSignedReportSafeChain(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	root := t.TempDir()
	generatedAt := time.Date(2026, time.August, 1, 1, 2, 3, 0, time.UTC)
	identity := testAgentTaskCheckpointIdentity()
	store, err := openAgentTaskCheckpointStore(root, identity, []byte(key), "checkpoint-key-v1", generatedAt)
	if err != nil {
		t.Fatalf("open checkpoint store: %v", err)
	}
	for index, caseID := range []string{"case-one", "case-two"} {
		evidence := eval.AgentTaskCaseEvidence{
			Result: eval.AgentTaskCaseResult{
				CaseID: caseID, Category: "chat", Mode: "chat",
				ExpectedOutcome: eval.AgentTaskOutcomeCompleted, ActualOutcome: eval.AgentTaskOutcomeCompleted,
				OutcomeCorrect: true, ToolSelectionCorrect: true, Passed: true,
				OutputSHA256: strings.Repeat("a", 64), OutputCharacters: 12, DurationMS: int64(index + 1),
			},
		}
		if err := store.Append(eval.AgentTaskProgress{Completed: index + 1, Total: 2, Evidence: evidence}); err != nil {
			t.Fatalf("append checkpoint %d: %v", index+1, err)
		}
	}

	payload, err := os.ReadFile(filepath.Join(root, "candidate", "000001.json"))
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if bytes.Contains(payload, []byte("PRIVATE_OUTPUT")) || !bytes.Contains(payload, []byte(`"signature"`)) {
		t.Fatalf("checkpoint privacy/signature contract failed: %s", payload)
	}

	reopened, err := openAgentTaskCheckpointStore(root, identity, []byte(key), "checkpoint-key-v1", generatedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("reopen checkpoint store: %v", err)
	}
	if len(reopened.ResumeCases()) != 2 || !reopened.GeneratedAt().Equal(generatedAt) {
		t.Fatalf("checkpoint resume state = %d / %s", len(reopened.ResumeCases()), reopened.GeneratedAt())
	}
}

func TestAgentTaskCheckpointStoreRejectsTamperingAndIdentityDrift(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	root := t.TempDir()
	identity := testAgentTaskCheckpointIdentity()
	store, err := openAgentTaskCheckpointStore(root, identity, []byte(key), "checkpoint-key-v1", time.Now())
	if err != nil {
		t.Fatalf("open checkpoint store: %v", err)
	}
	evidence := eval.AgentTaskCaseEvidence{Result: eval.AgentTaskCaseResult{CaseID: "case-one"}}
	if err := store.Append(eval.AgentTaskProgress{Completed: 1, Total: 2, Evidence: evidence}); err != nil {
		t.Fatalf("append checkpoint: %v", err)
	}

	drifted := identity
	drifted.ExecutionConfigHash = strings.Repeat("c", 64)
	if _, err := openAgentTaskCheckpointStore(root, drifted, []byte(key), "checkpoint-key-v1", time.Now()); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("expected identity drift rejection, got %v", err)
	}

	path := filepath.Join(root, "candidate", "000001.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	payload = bytes.Replace(payload, []byte(`"case-one"`), []byte(`"case-evil"`), 1)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("tamper checkpoint: %v", err)
	}
	if _, err := openAgentTaskCheckpointStore(root, identity, []byte(key), "checkpoint-key-v1", time.Now()); err == nil || !strings.Contains(err.Error(), "payload hash mismatch") {
		t.Fatalf("expected checkpoint tamper rejection, got %v", err)
	}
}

func testAgentTaskCheckpointIdentity() agentTaskCheckpointIdentity {
	return agentTaskCheckpointIdentity{
		Side: "candidate", DatasetVersion: "dataset-v1",
		DatasetSHA256: strings.Repeat("a", 64), ExecutionConfigHash: strings.Repeat("b", 64),
		Environment: "test", CaseTimeoutMS: 1_000, TotalCases: 2,
		Execution: eval.AgentTaskExecutionDescriptor{
			Kind: "runtime_live", Version: "v1", Strategy: "single_agent",
			Provider: "lmstudio", Model: "fixed-model", ProfileID: "profile", ProfileVersion: "v1",
		},
	}
}
