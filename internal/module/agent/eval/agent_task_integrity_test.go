package eval

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestAgentTaskPayloadIntegrityRoundTripAndTamperDetection(t *testing.T) {
	payload := []byte(`{"candidate":{"dataset_version":"v1"}}`)
	key := bytes.Repeat([]byte("k"), 32)
	signedAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	integrity, err := SignAgentTaskPayload(payload, key, "eval-key-v1", signedAt)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	if integrity.PayloadSHA256 == "" || integrity.Signature == "" {
		t.Fatalf("missing integrity evidence: %#v", integrity)
	}
	if err := VerifyAgentTaskPayload(payload, key, integrity); err != nil {
		t.Fatalf("verify payload: %v", err)
	}
	if err := VerifyAgentTaskPayload(append(payload, 'x'), key, integrity); err == nil || !strings.Contains(err.Error(), "payload hash mismatch") {
		t.Fatalf("expected payload tamper failure, got %v", err)
	}
	integrity.Signature = strings.Repeat("0", 64)
	if err := VerifyAgentTaskPayload(payload, key, integrity); err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected signature tamper failure, got %v", err)
	}
}

func TestAgentTaskPayloadIntegrityRejectsWeakKey(t *testing.T) {
	_, err := SignAgentTaskPayload([]byte("payload"), []byte("short"), "key", time.Now())
	if err == nil || !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("expected weak key error, got %v", err)
	}
}

func TestRunAgentTasksBindsDatasetAndExecutionHashes(t *testing.T) {
	dataset := []AgentTaskCase{{
		ID: "case", Category: "chat", Mode: "chat", Input: "hello", ExpectedOutcome: AgentTaskOutcomeCompleted,
	}}
	report, err := RunAgentTasks(t.Context(), dataset, agentTaskExecutorFunc(func(_ context.Context, _ AgentTaskCase) (AgentTaskExecution, error) {
		return AgentTaskExecution{Outcome: AgentTaskOutcomeCompleted, Output: "hello"}, nil
	}), AgentTaskRunnerConfig{DatasetVersion: "v1", Execution: AgentTaskExecutionDescriptor{Kind: "fake", Version: "v1"}})
	if err != nil {
		t.Fatalf("run agent tasks: %v", err)
	}
	if !validSHA256(report.DatasetSHA256) || !validSHA256(report.ExecutionConfigHash) {
		t.Fatalf("missing report hashes: %#v", report)
	}
}

func TestAgentTaskEvaluationOutputKeepsLegacyV2SignatureShapeWithoutResourceEvidence(t *testing.T) {
	output := AgentTaskEvaluationOutput{
		SchemaVersion: AgentTaskEvaluationSchemaVersion,
		Candidate: AgentTaskReport{
			DatasetVersion: "v1", DatasetSHA256: strings.Repeat("a", 64),
			ExecutionConfigHash: strings.Repeat("b", 64),
			CaseResults:         []AgentTaskCaseResult{{CaseID: "case-1"}},
		},
	}
	payload, err := unsignedAgentTaskEvaluationPayload(output)
	if err != nil {
		t.Fatalf("unsignedAgentTaskEvaluationPayload() error = %v", err)
	}
	for _, field := range []string{"strategy_gate", "live_authorization", "cost_evidence_cases", "estimated_cost_micros", "pricing_versions", "strategy_template_id"} {
		if bytes.Contains(payload, []byte(field)) {
			t.Fatalf("legacy-shaped payload unexpectedly contains %q: %s", field, payload)
		}
	}
	key := bytes.Repeat([]byte("k"), 32)
	if err := SignAgentTaskEvaluationOutput(&output, key, "eval-key-v1", time.Now()); err != nil {
		t.Fatalf("SignAgentTaskEvaluationOutput() error = %v", err)
	}
	encoded, err := MarshalAgentTaskEvaluationOutput(output)
	if err != nil {
		t.Fatalf("MarshalAgentTaskEvaluationOutput() error = %v", err)
	}
	if _, err := DecodeAndVerifyAgentTaskEvaluationOutput(encoded, key, "eval-key-v1"); err != nil {
		t.Fatalf("DecodeAndVerifyAgentTaskEvaluationOutput() error = %v", err)
	}
}

func TestAgentTaskEvaluationOutputBindsLiveAuthorizationEvidence(t *testing.T) {
	key := bytes.Repeat([]byte("k"), 32)
	output := AgentTaskEvaluationOutput{
		SchemaVersion: AgentTaskEvaluationSchemaVersion,
		Candidate: AgentTaskReport{
			DatasetVersion: "v1", DatasetSHA256: strings.Repeat("a", 64),
			ExecutionConfigHash: strings.Repeat("b", 64),
			CaseResults:         []AgentTaskCaseResult{{CaseID: "case-1"}},
		},
		LiveAuthorization: &AgentTaskLiveAuthorizationEvidence{
			SchemaVersion:              AgentTaskLiveAuthorizationSchemaVersion,
			AuthorizationID:            "authorization-001",
			AuthorizationPayloadSHA256: strings.Repeat("c", 64),
			AuthorizationKeyID:         "authorization-key-v1",
			InvocationSHA256:           strings.Repeat("d", 64),
			StateBackend:               "redis",
			StateNamespaceSHA256:       strings.Repeat("e", 64),
			Limits: AgentTaskLiveAuthorizationLimits{
				MaxRuns: 1, MaxProviderCalls: 4, MaxCapturedOutputs: 2, MaxEstimatedCostMicros: 100,
			},
		},
	}
	if err := SignAgentTaskEvaluationOutput(&output, key, "eval-key-v1", time.Now()); err != nil {
		t.Fatalf("SignAgentTaskEvaluationOutput() error = %v", err)
	}
	if err := VerifyAgentTaskEvaluationOutput(output, key, "eval-key-v1"); err != nil {
		t.Fatalf("VerifyAgentTaskEvaluationOutput() error = %v", err)
	}

	tampered := output
	tamperedEvidence := *output.LiveAuthorization
	tamperedEvidence.AuthorizationID = "authorization-002"
	tampered.LiveAuthorization = &tamperedEvidence
	if err := VerifyAgentTaskEvaluationOutput(tampered, key, "eval-key-v1"); err == nil || !strings.Contains(err.Error(), "payload hash mismatch") {
		t.Fatalf("tampered live authorization evidence was accepted: %v", err)
	}

	invalid := output
	invalid.Integrity = nil
	invalidEvidence := *output.LiveAuthorization
	invalidEvidence.InvocationSHA256 = strings.Repeat("D", 64)
	invalid.LiveAuthorization = &invalidEvidence
	if err := SignAgentTaskEvaluationOutput(&invalid, key, "eval-key-v1", time.Now()); err == nil || !strings.Contains(err.Error(), "identity is invalid") {
		t.Fatalf("non-canonical live authorization evidence was accepted: %v", err)
	}

	invalidState := output
	invalidState.Integrity = nil
	invalidStateEvidence := *output.LiveAuthorization
	invalidStateEvidence.StateNamespaceSHA256 = ""
	invalidState.LiveAuthorization = &invalidStateEvidence
	if err := SignAgentTaskEvaluationOutput(&invalidState, key, "eval-key-v1", time.Now()); err == nil ||
		!strings.Contains(err.Error(), "must provide both") {
		t.Fatalf("partial live authorization state evidence was accepted: %v", err)
	}
}
