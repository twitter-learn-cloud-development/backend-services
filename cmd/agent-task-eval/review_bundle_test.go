package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

func TestAgentTaskReviewBundleRoundTripBindsSignedReport(t *testing.T) {
	const candidateOutput = "candidate exact private output"
	const stableOutput = "stable exact private output"
	result := func(output string) eval.AgentTaskCaseResult {
		return eval.AgentTaskCaseResult{
			CaseID: "case-1", Category: "research", Mode: "assist",
			StrategyTemplateID: "platform.research_draft.v1",
			ExpectedOutcome:    eval.AgentTaskOutcomeCompleted,
			ActualOutcome:      eval.AgentTaskOutcomeCompleted,
			ExpectedTools:      []string{"hybrid_search_tweets"},
			AllowedTools:       []string{"hybrid_search_tweets"},
			SelectedTools:      []string{"hybrid_search_tweets"},
			OutcomeCorrect:     true, ToolSelectionCorrect: true,
			SemanticEvaluated: true, SemanticPassed: true,
			OutputSHA256: outputSHA256(output), OutputCharacters: len(output), Passed: true,
		}
	}
	candidate := eval.AgentTaskReport{
		DatasetVersion: "dataset-v1", DatasetSHA256: strings.Repeat("a", 64),
		ExecutionConfigHash: strings.Repeat("b", 64),
		Execution: eval.AgentTaskExecutionDescriptor{
			Kind: "runtime_live", Strategy: eval.AgentStrategyMulti, Provider: "test", Model: "fixed",
		},
		CaseResults: []eval.AgentTaskCaseResult{result(candidateOutput)},
	}
	stable := candidate
	stable.Execution.Strategy = eval.AgentStrategySingle
	stable.CaseResults = []eval.AgentTaskCaseResult{result(stableOutput)}
	output := agentTaskEvaluationOutput{
		SchemaVersion: agentTaskEvaluationSchemaVersion, Candidate: candidate, Stable: &stable,
		Gate:         &eval.AgentQualityGateDecision{Status: eval.AgentQualityGatePassed},
		StrategyGate: &eval.AgentStrategyGateDecision{Status: eval.AgentQualityGatePassed},
	}
	if err := signEvaluationOutput(&output, []byte("0123456789abcdef0123456789abcdef"), "report-key-v1", time.Unix(10, 0)); err != nil {
		t.Fatalf("sign output: %v", err)
	}

	collector := newAgentTaskReviewCollector()
	collector.capture("candidate", eval.AgentTaskCase{ID: "case-1", Input: "sensitive input"}, eval.AgentTaskExecution{Output: candidateOutput})
	collector.capture("stable", eval.AgentTaskCase{ID: "case-1", Input: "sensitive input"}, eval.AgentTaskExecution{Output: stableOutput})
	payload, err := collector.Build(output, time.Unix(20, 0))
	if err != nil {
		t.Fatalf("build review payload: %v", err)
	}
	reviewKey := bytes.Repeat([]byte{0x42}, 32)
	bundle, err := encryptAgentTaskReviewPayload(payload, reviewKey, "review-key-v1", bytes.NewReader(bytes.Repeat([]byte{0x24}, 32)))
	if err != nil {
		t.Fatalf("encrypt review payload: %v", err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	if bytes.Contains(encoded, []byte("sensitive input")) || bytes.Contains(encoded, []byte(candidateOutput)) || bytes.Contains(encoded, []byte(stableOutput)) {
		t.Fatalf("encrypted bundle leaked plaintext: %s", encoded)
	}
	opened, err := decryptAgentTaskReviewBundle(bundle, reviewKey, "review-key-v1")
	if err != nil {
		t.Fatalf("decrypt review bundle: %v", err)
	}
	if err := validateAgentTaskReviewPayload(opened, output); err != nil {
		t.Fatalf("validate opened payload: %v", err)
	}
	if opened.Candidate[0].Output != candidateOutput || opened.Stable[0].Output != stableOutput {
		t.Fatalf("opened outputs = %#v / %#v", opened.Candidate, opened.Stable)
	}

	tampered := bundle
	tampered.ReportPayloadSHA256 = strings.Repeat("c", 64)
	if _, err := decryptAgentTaskReviewBundle(tampered, reviewKey, "review-key-v1"); err == nil || !strings.Contains(err.Error(), "decrypt") {
		t.Fatalf("tampered report binding was not rejected: %v", err)
	}
}

func TestAgentTaskReviewBundleAllowsFailedGateForDiagnostics(t *testing.T) {
	const candidateOutput = "candidate diagnostic output"
	const stableOutput = "stable diagnostic output"
	result := func(output string) eval.AgentTaskCaseResult {
		return eval.AgentTaskCaseResult{
			CaseID: "case-1", Category: "research", Mode: "assist",
			StrategyTemplateID: "platform.research_draft.v1",
			ExpectedOutcome:    eval.AgentTaskOutcomeCompleted,
			ActualOutcome:      eval.AgentTaskOutcomeCompleted,
			OutputSHA256:       outputSHA256(output),
			OutputCharacters:   len(output),
			SemanticEvaluated:  true,
		}
	}
	candidate := eval.AgentTaskReport{
		DatasetVersion: "dataset-v1", DatasetSHA256: strings.Repeat("a", 64),
		ExecutionConfigHash: strings.Repeat("b", 64),
		Execution: eval.AgentTaskExecutionDescriptor{
			Kind: "runtime_live", Strategy: eval.AgentStrategyMulti, Provider: "test", Model: "fixed",
		},
		CaseResults: []eval.AgentTaskCaseResult{result(candidateOutput)},
	}
	stable := candidate
	stable.Execution.Strategy = eval.AgentStrategySingle
	stable.CaseResults = []eval.AgentTaskCaseResult{result(stableOutput)}
	output := agentTaskEvaluationOutput{
		SchemaVersion: agentTaskEvaluationSchemaVersion, Candidate: candidate, Stable: &stable,
		Gate:         &eval.AgentQualityGateDecision{Status: eval.AgentQualityGatePassed},
		StrategyGate: &eval.AgentStrategyGateDecision{Status: eval.AgentQualityGateFailed},
	}
	if err := signEvaluationOutput(&output, []byte("0123456789abcdef0123456789abcdef"), "report-key-v1", time.Unix(10, 0)); err != nil {
		t.Fatalf("sign output: %v", err)
	}

	collector := newAgentTaskReviewCollector()
	collector.capture("candidate", eval.AgentTaskCase{ID: "case-1", Input: "sensitive input"}, eval.AgentTaskExecution{Output: candidateOutput})
	collector.capture("stable", eval.AgentTaskCase{ID: "case-1", Input: "sensitive input"}, eval.AgentTaskExecution{Output: stableOutput})
	payload, err := collector.Build(output, time.Unix(20, 0))
	if err != nil {
		t.Fatalf("build diagnostic review payload: %v", err)
	}
	if payload.ReportPayloadSHA256 != output.Integrity.PayloadSHA256 {
		t.Fatalf("diagnostic payload report digest = %q", payload.ReportPayloadSHA256)
	}
}

func TestReadAgentTaskReviewKeyRequiresBase64Encoded32Bytes(t *testing.T) {
	t.Setenv("TEST_AGENT_TASK_REVIEW_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, 32)))
	key, err := readAgentTaskReviewKey("TEST_AGENT_TASK_REVIEW_KEY", "review-key-v1")
	if err != nil || len(key) != 32 {
		t.Fatalf("read review key: len=%d err=%v", len(key), err)
	}
	t.Setenv("TEST_AGENT_TASK_REVIEW_KEY", "short")
	if _, err := readAgentTaskReviewKey("TEST_AGENT_TASK_REVIEW_KEY", "review-key-v1"); err == nil {
		t.Fatal("invalid review key was accepted")
	}
}

func TestWriteAgentTaskReviewBundleIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.json")
	bundle := agentTaskReviewBundle{SchemaVersion: agentTaskReviewBundleSchemaVersion}
	if err := writeAgentTaskReviewBundle(path, bundle); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	if err := writeAgentTaskReviewBundle(path, bundle); err == nil {
		t.Fatal("existing review bundle was overwritten")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("review bundle stat: info=%v err=%v", info, err)
	}
}

func TestRunReviewBundleRequiresExplicitContentConsent(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--review-bundle", filepath.Join(t.TempDir(), "review.json")}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "explicit --allow-review-content") {
		t.Fatalf("unexpected result: code=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestRunCaptureFailedReviewBundleRequiresReviewBundle(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--capture-failed-review-bundle"}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "requires --review-bundle") {
		t.Fatalf("unexpected result: code=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestRunReviewBundleRejectsCheckpointResumeBeforeLoadingRuntimeConfig(t *testing.T) {
	t.Setenv("TEST_AGENT_TASK_REVIEW_REPORT_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("TEST_AGENT_TASK_REVIEW_CONTENT_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x19}, 32)))
	tempDir := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--strategy-runtime-config", filepath.Join(tempDir, "missing-runtime.json"),
		"--allow-live",
		"--checkpoint-dir", filepath.Join(tempDir, "checkpoint"),
		"--review-bundle", filepath.Join(tempDir, "review.json"),
		"--allow-review-content",
		"--review-key-env", "TEST_AGENT_TASK_REVIEW_CONTENT_KEY",
		"--review-key-id", "review-key-v1",
		"--integrity-key-env", "TEST_AGENT_TASK_REVIEW_REPORT_KEY",
		"--integrity-key-id", "report-key-v1",
		"--out", filepath.Join(tempDir, "report.json"),
		"--enforce-gate",
		"--enforce-strategy-gate",
	}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "cannot be combined with --checkpoint-dir") ||
		strings.Contains(stderr.String(), "missing-runtime") {
		t.Fatalf("unexpected result: code=%d stderr=%q", exitCode, stderr.String())
	}
}
