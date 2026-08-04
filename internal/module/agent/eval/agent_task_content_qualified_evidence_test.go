package eval

import (
	"strings"
	"testing"
	"time"
)

func TestAgentTaskContentQualifiedEvidenceRoundTrip(t *testing.T) {
	output, reportKey := testAgentTaskContentReviewReport(t)
	binding := AgentTaskContentReviewBundleBinding{
		SchemaVersion: AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-encryption-v1",
		FileSHA256:    strings.Repeat("b", 64),
	}
	decision := testAgentTaskContentReviewDecision(output, binding)
	signoffKey := []byte("content-review-signoff-key-material-v1")
	signoff, err := BuildAndSignAgentTaskContentReviewSignoff(
		output, binding, decision, signoffKey, "content-signoff-v1", decision.ReviewedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("BuildAndSignAgentTaskContentReviewSignoff() error = %v", err)
	}
	evidence, err := BuildAgentTaskContentQualifiedEvidence(
		output, signoff, reportKey, "report-key-v1", signoffKey, "content-signoff-v1",
	)
	if err != nil {
		t.Fatalf("BuildAgentTaskContentQualifiedEvidence() error = %v", err)
	}
	payload, err := MarshalAgentTaskContentQualifiedEvidence(evidence)
	if err != nil {
		t.Fatalf("MarshalAgentTaskContentQualifiedEvidence() error = %v", err)
	}
	verified, err := DecodeAndVerifyAgentTaskContentQualifiedEvidence(
		payload, reportKey, "report-key-v1", signoffKey, "content-signoff-v1",
	)
	if err != nil {
		t.Fatalf("DecodeAndVerifyAgentTaskContentQualifiedEvidence() error = %v", err)
	}
	if verified.Report.Integrity == nil || verified.ContentReviewSignoff.CandidateVerdict != AgentTaskContentReviewApproved {
		t.Fatalf("verified evidence is incomplete: %#v", verified)
	}
}

func TestAgentTaskContentQualifiedEvidenceRejectsAdvisoryJudgeAndKeyReuse(t *testing.T) {
	output, reportKey := testAgentTaskContentReviewReport(t)
	binding := AgentTaskContentReviewBundleBinding{
		SchemaVersion: AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-encryption-v1",
		FileSHA256:    strings.Repeat("b", 64),
	}
	decision := testAgentTaskContentReviewDecision(output, binding)
	decision.Reviewer = AgentTaskContentReviewer{
		Kind:              AgentTaskContentReviewerJudge,
		ID:                "judge.bound.v1",
		IdentityAssurance: AgentTaskContentReviewerModelConfigBound,
		Judge: &AgentTaskContentReviewJudgeIdentity{
			Provider: "dashscope", Model: "judge-model", PromptID: "review",
			PromptVersion: "v1", ConfigSHA256: strings.Repeat("e", 64),
		},
	}
	signoffKey := []byte("content-review-signoff-key-material-v1")
	signoff, err := BuildAndSignAgentTaskContentReviewSignoff(
		output, binding, decision, signoffKey, "content-signoff-v1", decision.ReviewedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("build judge signoff: %v", err)
	}
	_, err = BuildAgentTaskContentQualifiedEvidence(
		output, signoff, reportKey, "report-key-v1", signoffKey, "content-signoff-v1",
	)
	if err == nil || !strings.Contains(err.Error(), "external human") {
		t.Fatalf("expected advisory judge rejection, got %v", err)
	}

	_, err = BuildAgentTaskContentQualifiedEvidence(
		output, signoff, reportKey, "shared-key-v1", reportKey, "shared-key-v1",
	)
	if err == nil || !strings.Contains(err.Error(), "must be independent") {
		t.Fatalf("expected key reuse rejection, got %v", err)
	}
}

func TestDecodeAgentTaskContentQualifiedEvidenceRejectsDuplicateKeys(t *testing.T) {
	_, err := DecodeAgentTaskContentQualifiedEvidence([]byte(`{
		"schema_version":"agent-task-content-qualified-evidence/v1",
		"schema_version":"agent-task-content-qualified-evidence/v1"
	}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("expected duplicate key rejection, got %v", err)
	}
}
