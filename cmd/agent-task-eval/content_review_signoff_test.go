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

func TestRunCreatesAndVerifiesContentReviewSignoffWithoutPlaintext(t *testing.T) {
	const (
		candidateOutput = "candidate private reviewed output"
		stableOutput    = "stable private reviewed output"
	)
	reportKey := []byte("content-review-report-key-material-v1")
	reviewKey := bytes.Repeat([]byte{0x31}, 32)
	signoffKey := []byte("content-review-signoff-key-material-v1")
	t.Setenv("TEST_CONTENT_REVIEW_REPORT_KEY", string(reportKey))
	t.Setenv("TEST_CONTENT_REVIEW_BUNDLE_KEY", base64.StdEncoding.EncodeToString(reviewKey))
	t.Setenv("TEST_CONTENT_REVIEW_SIGNOFF_KEY", string(signoffKey))

	signedAt := time.Now().UTC().Add(-2 * time.Minute)
	result := func(output string) eval.AgentTaskCaseResult {
		return eval.AgentTaskCaseResult{
			CaseID: "case-1", Category: "research", Mode: "assist",
			ActualOutcome: eval.AgentTaskOutcomeCompleted, OutcomeCorrect: true,
			ToolSelectionCorrect: true, SemanticEvaluated: true, SemanticPassed: true,
			OutputSHA256: outputSHA256(output), OutputCharacters: len(output), Passed: true,
		}
	}
	candidate := eval.AgentTaskReport{
		DatasetVersion: "agent-strategy-cases-v3", DatasetSHA256: strings.Repeat("a", 64),
		ExecutionConfigHash: strings.Repeat("b", 64),
		Execution:           eval.AgentTaskExecutionDescriptor{Kind: "runtime_live", Version: "v5", Strategy: eval.AgentStrategyMulti},
		Metrics:             eval.AgentTaskMetrics{Cases: 1, Passed: 1},
		CaseResults:         []eval.AgentTaskCaseResult{result(candidateOutput)},
	}
	stable := candidate
	stable.Execution.Strategy = eval.AgentStrategySingle
	stable.CaseResults = []eval.AgentTaskCaseResult{result(stableOutput)}
	output := agentTaskEvaluationOutput{
		SchemaVersion: agentTaskEvaluationSchemaVersion,
		Candidate:     candidate,
		Stable:        &stable,
		Gate:          &eval.AgentQualityGateDecision{Status: eval.AgentQualityGatePassed},
		StrategyGate:  &eval.AgentStrategyGateDecision{Status: eval.AgentQualityGatePassed},
	}
	if err := signEvaluationOutput(&output, reportKey, "report-key-v1", signedAt); err != nil {
		t.Fatalf("sign report: %v", err)
	}

	collector := newAgentTaskReviewCollector()
	collector.capture("candidate", eval.AgentTaskCase{ID: "case-1", Input: "private input"}, eval.AgentTaskExecution{Output: candidateOutput})
	collector.capture("stable", eval.AgentTaskCase{ID: "case-1", Input: "private input"}, eval.AgentTaskExecution{Output: stableOutput})
	reviewPayload, err := collector.Build(output, signedAt.Add(30*time.Second))
	if err != nil {
		t.Fatalf("build review payload: %v", err)
	}
	bundle, err := encryptAgentTaskReviewPayload(
		reviewPayload,
		reviewKey,
		"review-key-v1",
		bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)),
	)
	if err != nil {
		t.Fatalf("encrypt review payload: %v", err)
	}

	tempDir := t.TempDir()
	reportPath := filepath.Join(tempDir, "report.json")
	bundlePath := filepath.Join(tempDir, "review.enc.json")
	decisionPath := filepath.Join(tempDir, "decision.json")
	signoffPath := filepath.Join(tempDir, "signoff.json")
	writeJSONFixture(t, reportPath, output)
	writeJSONFixture(t, bundlePath, bundle)
	_, binding, err := loadAndOpenAgentTaskReviewBundleWithBinding(bundlePath, reviewKey, "review-key-v1", output)
	if err != nil {
		t.Fatalf("load review bundle binding: %v", err)
	}
	passed := eval.AgentTaskContentReviewAssessment{
		FactualCorrectness: eval.AgentTaskContentReviewPassed,
		Relevance:          eval.AgentTaskContentReviewPassed,
		EvidenceFidelity:   eval.AgentTaskContentReviewPassed,
		WritingQuality:     eval.AgentTaskContentReviewPassed,
	}
	decision := eval.AgentTaskContentReviewDecision{
		SchemaVersion:       eval.AgentTaskContentReviewDecisionSchemaVersion,
		ReportPayloadSHA256: output.Integrity.PayloadSHA256,
		ReviewBundleSHA256:  binding.FileSHA256,
		RuleVersion:         eval.AgentTaskContentReviewRuleVersion,
		Reviewer: eval.AgentTaskContentReviewer{
			Kind:                 eval.AgentTaskContentReviewerExternalHuman,
			ID:                   "reviewer.external-01",
			IdentityAssurance:    eval.AgentTaskContentReviewerAssertedExternal,
			ExternalRecordSHA256: strings.Repeat("c", 64),
		},
		ReviewedAt:       signedAt.Add(time.Minute),
		CandidateVerdict: eval.AgentTaskContentReviewApproved,
		StableVerdict:    eval.AgentTaskContentReviewApproved,
		Cases: []eval.AgentTaskContentReviewCaseDecision{
			{CaseID: "case-1", Candidate: passed, Stable: passed},
		},
	}
	writeJSONFixture(t, decisionPath, decision)

	common := []string{
		"--review-report", reportPath,
		"--review-bundle-input", bundlePath,
		"--allow-review-content",
		"--integrity-key-env", "TEST_CONTENT_REVIEW_REPORT_KEY",
		"--integrity-key-id", "report-key-v1",
		"--review-key-env", "TEST_CONTENT_REVIEW_BUNDLE_KEY",
		"--review-key-id", "review-key-v1",
		"--review-signoff-key-env", "TEST_CONTENT_REVIEW_SIGNOFF_KEY",
		"--review-signoff-key-id", "signoff-key-v1",
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	createArgs := append(append([]string(nil), common...),
		"--review-decision", decisionPath,
		"--create-review-signoff", signoffPath,
	)
	if exitCode := run(createArgs, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("create signoff: code=%d stderr=%q", exitCode, stderr.String())
	}
	signoffBytes, err := os.ReadFile(signoffPath)
	if err != nil {
		t.Fatalf("read signoff: %v", err)
	}
	for _, private := range []string{"private input", candidateOutput, stableOutput} {
		if bytes.Contains(signoffBytes, []byte(private)) {
			t.Fatalf("signoff leaked review plaintext %q: %s", private, signoffBytes)
		}
	}

	stdout.Reset()
	stderr.Reset()
	verifyArgs := append(append([]string(nil), common...), "--verify-review-signoff", signoffPath)
	if exitCode := run(verifyArgs, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("verify signoff: code=%d stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "external_human_approved=true") {
		t.Fatalf("verification summary = %q", stdout.String())
	}
}

func TestRunContentReviewSignoffRequiresExplicitContentConsent(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--verify-review-signoff", "signoff.json",
		"--review-report", "report.json",
		"--review-bundle-input", "review.json",
	}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "explicit --allow-review-content") {
		t.Fatalf("unexpected result: code=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestRunContentQualifiedArchiveRequiresConsentAndEncryptedBundle(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "consent",
			args: []string{
				"--archive-report", "report.json",
				"--archive-content-signoff", "signoff.json",
				"--review-bundle-input", "review.enc.json",
			},
			want: "explicit --allow-review-content",
		},
		{
			name: "bundle",
			args: []string{
				"--archive-report", "report.json",
				"--archive-content-signoff", "signoff.json",
				"--allow-review-content",
			},
			want: "requires --review-bundle-input",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := run(test.args, &stdout, &stderr)
			if exitCode != 2 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("unexpected result: code=%d stderr=%q", exitCode, stderr.String())
			}
		})
	}
}

func TestRunArchivedContentSignoffRequirementNeedsReceiptVerification(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"--require-archived-content-signoff"}, &stdout, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "requires --verify-archive-receipt") {
		t.Fatalf("unexpected result: code=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestContentReviewSignoffRejectsKeyReuseBeforeReadingInputs(t *testing.T) {
	key := []byte("shared-content-review-key-material-v1")
	_, err := runAgentTaskContentReviewSignoffCommand(agentTaskContentReviewSignoffCommand{
		CreatePath:       "signoff.json",
		DecisionPath:     "decision.json",
		ReportPath:       "report.json",
		ReviewBundlePath: "review.json",
		ReportKey:        key,
		ReportKeyID:      "report-v1",
		ReviewKey:        key,
		ReviewKeyID:      "review-v1",
		SignoffKey:       []byte("independent-signoff-key-material-v1"),
		SignoffKeyID:     "signoff-v1",
	})
	if err == nil || !strings.Contains(err.Error(), "must be independent") {
		t.Fatalf("expected key reuse rejection, got %v", err)
	}
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
