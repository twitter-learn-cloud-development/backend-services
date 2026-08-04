package eval

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestAgentTaskContentReviewSignoffRoundTrip(t *testing.T) {
	output, reportKey := testAgentTaskContentReviewReport(t)
	binding := AgentTaskContentReviewBundleBinding{
		SchemaVersion: AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-encryption-v1",
		FileSHA256:    strings.Repeat("b", 64),
	}
	decision := testAgentTaskContentReviewDecision(output, binding)
	signoffKey := []byte("content-review-signoff-key-material-v1")
	createdAt := decision.ReviewedAt.Add(time.Minute)
	signoff, err := BuildAndSignAgentTaskContentReviewSignoff(
		output, binding, decision, signoffKey, "content-signoff-v1", createdAt,
	)
	if err != nil {
		t.Fatalf("BuildAndSignAgentTaskContentReviewSignoff() error = %v", err)
	}
	if signoff.Integrity == nil || signoff.DecisionSHA256 == "" || len(signoff.Cases) != 2 {
		t.Fatalf("signoff identity is incomplete: %#v", signoff)
	}
	if signoff.Cases[0].CandidateOutputSHA256 != output.Candidate.CaseResults[0].OutputSHA256 {
		t.Fatalf("candidate output digest was not bound: %#v", signoff.Cases[0])
	}
	if err := VerifyAgentTaskContentReviewSignoff(signoff, output, binding, signoffKey, "content-signoff-v1"); err != nil {
		t.Fatalf("VerifyAgentTaskContentReviewSignoff() error = %v", err)
	}
	if !AgentTaskContentReviewHasApprovedExternalHumanSignoff(signoff) {
		t.Fatal("approved external human signoff should be recognized after verification")
	}
	if err := VerifyAgentTaskEvaluationOutput(output, reportKey, "report-key-v1"); err != nil {
		t.Fatalf("fixture report signature is invalid: %v", err)
	}

	encoded, err := MarshalAgentTaskContentReviewSignoff(signoff)
	if err != nil {
		t.Fatalf("MarshalAgentTaskContentReviewSignoff() error = %v", err)
	}
	decoded, err := DecodeAgentTaskContentReviewSignoff(encoded)
	if err != nil {
		t.Fatalf("DecodeAgentTaskContentReviewSignoff() error = %v", err)
	}
	if err := VerifyAgentTaskContentReviewSignoff(decoded, output, binding, signoffKey, "content-signoff-v1"); err != nil {
		t.Fatalf("decoded signoff verification error = %v", err)
	}
}

func TestAgentTaskContentReviewSignoffRejectsFailedAutomaticGate(t *testing.T) {
	output, reportKey := testAgentTaskContentReviewReport(t)
	output.StrategyGate.Status = AgentQualityGateFailed
	output.Integrity = nil
	if err := SignAgentTaskEvaluationOutput(
		&output, reportKey, "report-key-v1", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("SignAgentTaskEvaluationOutput() error = %v", err)
	}
	binding := AgentTaskContentReviewBundleBinding{
		SchemaVersion: AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-encryption-v1",
		FileSHA256:    strings.Repeat("b", 64),
	}
	decision := testAgentTaskContentReviewDecision(output, binding)
	_, err := BuildAndSignAgentTaskContentReviewSignoff(
		output, binding, decision,
		[]byte("content-review-signoff-key-material-v1"), "content-signoff-v1",
		decision.ReviewedAt.Add(time.Minute),
	)
	if err == nil || !strings.Contains(err.Error(), "automatic quality gates") {
		t.Fatalf("failed automatic gate was accepted for signoff: %v", err)
	}
}

func TestAgentTaskContentReviewSignoffRejectsPartialCoverageAndVerdictMismatch(t *testing.T) {
	output, _ := testAgentTaskContentReviewReport(t)
	binding := AgentTaskContentReviewBundleBinding{
		SchemaVersion: AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-encryption-v1",
		FileSHA256:    strings.Repeat("b", 64),
	}
	decision := testAgentTaskContentReviewDecision(output, binding)
	createdAt := decision.ReviewedAt.Add(time.Minute)
	key := []byte("content-review-signoff-key-material-v1")

	partial := decision
	partial.Cases = append([]AgentTaskContentReviewCaseDecision(nil), decision.Cases[:1]...)
	if _, err := BuildAndSignAgentTaskContentReviewSignoff(output, binding, partial, key, "content-signoff-v1", createdAt); err == nil || !strings.Contains(err.Error(), "contains 1 cases") {
		t.Fatalf("expected partial coverage rejection, got %v", err)
	}

	mismatch := decision
	mismatch.Cases = append([]AgentTaskContentReviewCaseDecision(nil), decision.Cases...)
	mismatch.Cases[0].Candidate.WritingQuality = AgentTaskContentReviewFailed
	if _, err := BuildAndSignAgentTaskContentReviewSignoff(output, binding, mismatch, key, "content-signoff-v1", createdAt); err == nil || !strings.Contains(err.Error(), "verdict does not match") {
		t.Fatalf("expected verdict mismatch rejection, got %v", err)
	}

	placeholder := decision
	placeholder.Reviewer.ExternalRecordSHA256 = strings.Repeat("0", 64)
	if _, err := BuildAndSignAgentTaskContentReviewSignoff(output, binding, placeholder, key, "content-signoff-v1", createdAt); err == nil || !strings.Contains(err.Error(), "external record digest") {
		t.Fatalf("expected placeholder external record rejection, got %v", err)
	}
}

func TestAgentTaskContentReviewJudgeIsAdvisoryAndConfigBound(t *testing.T) {
	output, _ := testAgentTaskContentReviewReport(t)
	binding := AgentTaskContentReviewBundleBinding{
		SchemaVersion: AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-encryption-v1",
		FileSHA256:    strings.Repeat("b", 64),
	}
	decision := testAgentTaskContentReviewDecision(output, binding)
	decision.Reviewer = AgentTaskContentReviewer{
		Kind:              AgentTaskContentReviewerJudge,
		ID:                "judge.qwen37.v1",
		IdentityAssurance: AgentTaskContentReviewerModelConfigBound,
		Judge: &AgentTaskContentReviewJudgeIdentity{
			Provider:      "dashscope",
			Model:         "qwen3.7-plus-2026-05-26",
			PromptID:      "content-judge",
			PromptVersion: "v1",
			ConfigSHA256:  strings.Repeat("e", 64),
		},
	}
	signoff, err := BuildAndSignAgentTaskContentReviewSignoff(
		output,
		binding,
		decision,
		[]byte("content-review-signoff-key-material-v1"),
		"content-signoff-v1",
		decision.ReviewedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("BuildAndSignAgentTaskContentReviewSignoff() error = %v", err)
	}
	if AgentTaskContentReviewHasApprovedExternalHumanSignoff(signoff) {
		t.Fatal("judge signoff must remain advisory")
	}

	decision.Reviewer.Judge.ConfigSHA256 = ""
	if _, err := BuildAndSignAgentTaskContentReviewSignoff(
		output,
		binding,
		decision,
		[]byte("content-review-signoff-key-material-v1"),
		"content-signoff-v1",
		decision.ReviewedAt.Add(time.Minute),
	); err == nil || !strings.Contains(err.Error(), "judge reviewer identity is incomplete") {
		t.Fatalf("expected incomplete judge identity rejection, got %v", err)
	}
}

func TestAgentTaskContentReviewSignoffDetectsTamperAndWrongBundle(t *testing.T) {
	output, _ := testAgentTaskContentReviewReport(t)
	binding := AgentTaskContentReviewBundleBinding{
		SchemaVersion: AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-encryption-v1",
		FileSHA256:    strings.Repeat("b", 64),
	}
	decision := testAgentTaskContentReviewDecision(output, binding)
	key := []byte("content-review-signoff-key-material-v1")
	signoff, err := BuildAndSignAgentTaskContentReviewSignoff(
		output, binding, decision, key, "content-signoff-v1", decision.ReviewedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("BuildAndSignAgentTaskContentReviewSignoff() error = %v", err)
	}

	tampered := signoff
	tampered.CandidateVerdict = AgentTaskContentReviewRejected
	if err := VerifyAgentTaskContentReviewSignoff(tampered, output, binding, key, "content-signoff-v1"); err == nil || !strings.Contains(err.Error(), "payload hash mismatch") {
		t.Fatalf("expected signoff tamper rejection, got %v", err)
	}

	wrongBinding := binding
	wrongBinding.FileSHA256 = strings.Repeat("c", 64)
	if err := VerifyAgentTaskContentReviewSignoff(signoff, output, wrongBinding, key, "content-signoff-v1"); err == nil || !strings.Contains(err.Error(), "does not match report or review bundle") {
		t.Fatalf("expected bundle substitution rejection, got %v", err)
	}
}

func TestDecodeAgentTaskContentReviewDecisionRejectsUnknownFields(t *testing.T) {
	_, err := DecodeAgentTaskContentReviewDecision([]byte(`{"schema_version":"agent-task-content-review-decision/v1","unknown":true}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected strict decision decode failure, got %v", err)
	}
	_, err = DecodeAgentTaskContentReviewDecision([]byte(`{"schema_version":"first","schema_version":"second"}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("expected duplicate decision key failure, got %v", err)
	}
}

func TestAgentTaskContentReviewDecisionTemplateCoversV3Dataset(t *testing.T) {
	datasetFile, err := os.Open("testdata/agent_strategy_cases_v3.json")
	if err != nil {
		t.Fatalf("open v3 dataset: %v", err)
	}
	defer datasetFile.Close()
	dataset, err := LoadAgentTaskDataset(datasetFile)
	if err != nil {
		t.Fatalf("load v3 dataset: %v", err)
	}
	payload, err := os.ReadFile("testdata/agent_task_content_review_decision_v1.example.json")
	if err != nil {
		t.Fatalf("read decision template: %v", err)
	}
	decision, err := DecodeAgentTaskContentReviewDecision(payload)
	if err != nil {
		t.Fatalf("decode decision template: %v", err)
	}
	if len(decision.Cases) != len(dataset) {
		t.Fatalf("decision template cases = %d, dataset cases = %d", len(decision.Cases), len(dataset))
	}
	for index, sample := range dataset {
		reviewed := decision.Cases[index]
		if reviewed.CaseID != sample.ID {
			t.Fatalf("decision template case %d = %q, want %q", index, reviewed.CaseID, sample.ID)
		}
		if reviewed.Candidate.FactualCorrectness != AgentTaskContentReviewFailed ||
			reviewed.Candidate.Relevance != AgentTaskContentReviewFailed ||
			reviewed.Candidate.EvidenceFidelity != AgentTaskContentReviewFailed ||
			reviewed.Candidate.WritingQuality != AgentTaskContentReviewFailed ||
			reviewed.Stable.FactualCorrectness != AgentTaskContentReviewFailed ||
			reviewed.Stable.Relevance != AgentTaskContentReviewFailed ||
			reviewed.Stable.EvidenceFidelity != AgentTaskContentReviewFailed ||
			reviewed.Stable.WritingQuality != AgentTaskContentReviewFailed {
			t.Fatalf("decision template case %q is not fail-closed", reviewed.CaseID)
		}
	}
	if decision.CandidateVerdict != AgentTaskContentReviewRejected || decision.StableVerdict != AgentTaskContentReviewRejected {
		t.Fatalf("decision template verdicts are not rejected: %q/%q", decision.CandidateVerdict, decision.StableVerdict)
	}
}

func testAgentTaskContentReviewReport(t *testing.T) (AgentTaskEvaluationOutput, []byte) {
	t.Helper()
	signedAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	report := func(strategy string, outputDigests []string) AgentTaskReport {
		return AgentTaskReport{
			DatasetVersion:      "agent-strategy-cases-v3",
			DatasetSHA256:       strings.Repeat("d", 64),
			ExecutionConfigHash: strings.Repeat("c", 64),
			Execution:           AgentTaskExecutionDescriptor{Kind: "runtime", Version: "v5", Strategy: strategy},
			Metrics:             AgentTaskMetrics{Cases: 2, Passed: 2},
			CaseResults: []AgentTaskCaseResult{
				{CaseID: "case-001", OutputSHA256: outputDigests[0], Passed: true},
				{CaseID: "case-002", OutputSHA256: outputDigests[1], Passed: true},
			},
		}
	}
	stable := report("single", []string{strings.Repeat("3", 64), strings.Repeat("4", 64)})
	output := AgentTaskEvaluationOutput{
		SchemaVersion: AgentTaskEvaluationSchemaVersion,
		Candidate:     report("multi", []string{strings.Repeat("1", 64), strings.Repeat("2", 64)}),
		Stable:        &stable,
		Gate:          &AgentQualityGateDecision{Status: AgentQualityGatePassed},
		StrategyGate:  &AgentStrategyGateDecision{Status: AgentQualityGatePassed},
	}
	reportKey := []byte("agent-task-report-signing-key-material-v1")
	if err := SignAgentTaskEvaluationOutput(&output, reportKey, "report-key-v1", signedAt); err != nil {
		t.Fatalf("SignAgentTaskEvaluationOutput() error = %v", err)
	}
	return output, reportKey
}

func testAgentTaskContentReviewDecision(
	output AgentTaskEvaluationOutput,
	binding AgentTaskContentReviewBundleBinding,
) AgentTaskContentReviewDecision {
	passed := AgentTaskContentReviewAssessment{
		FactualCorrectness: AgentTaskContentReviewPassed,
		Relevance:          AgentTaskContentReviewPassed,
		EvidenceFidelity:   AgentTaskContentReviewPassed,
		WritingQuality:     AgentTaskContentReviewPassed,
	}
	return AgentTaskContentReviewDecision{
		SchemaVersion:       AgentTaskContentReviewDecisionSchemaVersion,
		ReportPayloadSHA256: output.Integrity.PayloadSHA256,
		ReviewBundleSHA256:  binding.FileSHA256,
		RuleVersion:         AgentTaskContentReviewRuleVersion,
		Reviewer: AgentTaskContentReviewer{
			Kind:                 AgentTaskContentReviewerExternalHuman,
			ID:                   "reviewer.external-01",
			IdentityAssurance:    AgentTaskContentReviewerAssertedExternal,
			ExternalRecordSHA256: strings.Repeat("a", 64),
		},
		ReviewedAt:       output.Integrity.SignedAt.Add(time.Minute),
		CandidateVerdict: AgentTaskContentReviewApproved,
		StableVerdict:    AgentTaskContentReviewApproved,
		Cases: []AgentTaskContentReviewCaseDecision{
			{CaseID: "case-001", Candidate: passed, Stable: passed},
			{CaseID: "case-002", Candidate: passed, Stable: passed},
		},
	}
}
