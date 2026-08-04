package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/eval"
	"twitter-clone/internal/module/agent/profile"
)

type qualityEvidenceArchiveStub struct {
	payload []byte
	err     error
}

func (s *qualityEvidenceArchiveStub) Ensure(context.Context) error { return nil }
func (s *qualityEvidenceArchiveStub) PutImmutable(context.Context, eval.AgentTaskReportArchiveRequest) (eval.AgentTaskReportArchiveReceipt, error) {
	return eval.AgentTaskReportArchiveReceipt{}, errors.New("not implemented")
}
func (s *qualityEvidenceArchiveStub) Get(context.Context, eval.AgentTaskReportArchiveReceipt, int) ([]byte, error) {
	return append([]byte(nil), s.payload...), s.err
}

func TestAgentTaskQualityEvidenceVerifier(t *testing.T) {
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	payload, reference := signedQualityEvidenceReport(t, key, now)
	verifier, err := NewAgentTaskQualityEvidenceVerifier(&qualityEvidenceArchiveStub{payload: payload}, key, "eval-key-v1")
	if err != nil {
		t.Fatalf("NewAgentTaskQualityEvidenceVerifier() error = %v", err)
	}
	verifier.now = func() time.Time { return now }

	evidence, err := verifier.Verify(context.Background(), reference, "writer", "v2")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if evidence.GateStatus != profile.QualityEvidenceGatePassed || evidence.Cases != 50 || evidence.ProfileVersion != "v2" {
		t.Fatalf("Verify() evidence = %+v", evidence)
	}
}

func TestAgentTaskQualityEvidenceVerifierRejectsWrongProfile(t *testing.T) {
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	payload, reference := signedQualityEvidenceReport(t, key, now)
	verifier, _ := NewAgentTaskQualityEvidenceVerifier(&qualityEvidenceArchiveStub{payload: payload}, key, "eval-key-v1")
	verifier.now = func() time.Time { return now }

	_, err := verifier.Verify(context.Background(), reference, "researcher", "v2")
	if !errors.Is(err, profile.ErrQualityEvidenceInvalid) {
		t.Fatalf("Verify() error = %v, want ErrQualityEvidenceInvalid", err)
	}
}

func TestAgentTaskQualityEvidenceVerifierRequiresExternalHumanContentReview(t *testing.T) {
	now := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	reportKey := []byte("0123456789abcdef0123456789abcdef")
	signoffKey := []byte("content-signoff-key-material-0001")
	payload, reference := signedContentQualifiedQualityEvidence(t, reportKey, signoffKey, now)
	verifier, err := NewAgentTaskQualityEvidenceVerifier(
		&qualityEvidenceArchiveStub{payload: payload},
		reportKey,
		"eval-key-v1",
		WithRequiredExternalHumanContentReview(signoffKey, "content-signoff-v1"),
	)
	if err != nil {
		t.Fatalf("NewAgentTaskQualityEvidenceVerifier() error = %v", err)
	}
	verifier.now = func() time.Time { return now }

	evidence, err := verifier.Verify(context.Background(), reference, "writer", "v2")
	if err != nil {
		t.Fatalf("Verify() content-qualified error = %v", err)
	}
	if evidence.GateStatus != profile.QualityEvidenceGatePassed || evidence.Cases != 50 {
		t.Fatalf("Verify() content-qualified evidence = %+v", evidence)
	}
}

func TestAgentTaskQualityEvidenceVerifierStrictModeRejectsLegacyReport(t *testing.T) {
	now := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	reportKey := []byte("0123456789abcdef0123456789abcdef")
	signoffKey := []byte("content-signoff-key-material-0001")
	payload, reference := signedQualityEvidenceReport(t, reportKey, now)
	verifier, err := NewAgentTaskQualityEvidenceVerifier(
		&qualityEvidenceArchiveStub{payload: payload},
		reportKey,
		"eval-key-v1",
		WithRequiredExternalHumanContentReview(signoffKey, "content-signoff-v1"),
	)
	if err != nil {
		t.Fatalf("NewAgentTaskQualityEvidenceVerifier() error = %v", err)
	}
	verifier.now = func() time.Time { return now }

	_, err = verifier.Verify(context.Background(), reference, "writer", "v2")
	if !errors.Is(err, profile.ErrQualityEvidenceInvalid) {
		t.Fatalf("Verify() error = %v, want ErrQualityEvidenceInvalid", err)
	}
}

func TestAgentTaskQualityEvidenceVerifierRejectsContentSignoffKeyReuse(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	_, err := NewAgentTaskQualityEvidenceVerifier(
		&qualityEvidenceArchiveStub{},
		key,
		"shared-key-v1",
		WithRequiredExternalHumanContentReview(key, "shared-key-v1"),
	)
	if err == nil || !strings.Contains(err.Error(), "must be independent") {
		t.Fatalf("expected key reuse rejection, got %v", err)
	}
}

func signedQualityEvidenceReport(t *testing.T, key []byte, now time.Time) ([]byte, profile.QualityEvidenceReference) {
	t.Helper()
	datasetHash := strings.Repeat("a", 64)
	configHash := strings.Repeat("b", 64)
	metrics := eval.AgentTaskMetrics{
		Cases: 50, Passed: 50, TaskCompletionRate: 1, ReadToolSelectionAccuracy: 1,
		SemanticPassRate: 1, ApprovalCases: 4, ApprovalHandled: 4, ApprovalPassRate: 1,
	}
	candidate := eval.AgentTaskReport{
		GeneratedAt: now.Add(-2 * time.Minute), DatasetVersion: "dataset-v1", DatasetSHA256: datasetHash,
		ExecutionConfigHash: configHash,
		Execution:           eval.AgentTaskExecutionDescriptor{Kind: "runtime_live", ProfileID: "writer", ProfileVersion: "v2"},
		Metrics:             metrics,
	}
	stable := candidate
	stable.Execution.ProfileVersion = "v1"
	output := eval.AgentTaskEvaluationOutput{
		SchemaVersion: eval.AgentTaskEvaluationSchemaVersion, Candidate: candidate, Stable: &stable,
		Gate: &eval.AgentQualityGateDecision{Status: eval.AgentQualityGatePassed, Policy: eval.AgentQualityGatePolicy{MinCases: 50}},
	}
	if err := eval.SignAgentTaskEvaluationOutput(&output, key, "eval-key-v1", now.Add(-time.Minute)); err != nil {
		t.Fatalf("SignAgentTaskEvaluationOutput() error = %v", err)
	}
	payload, err := eval.MarshalAgentTaskEvaluationOutput(output)
	if err != nil {
		t.Fatalf("MarshalAgentTaskEvaluationOutput() error = %v", err)
	}
	digest := sha256.Sum256(payload)
	archivedAt := now.Add(-30 * time.Second)
	reference := profile.QualityEvidenceReference{
		Storage: profile.QualityEvidenceStorageMinIO, Bucket: "agent-eval", Key: "agent-task-eval/a/report.json",
		VersionID: "version-1", ReportSHA256: hex.EncodeToString(digest[:]), Length: len(payload),
		ContentType: profile.QualityEvidenceContentTypeJSON, RetentionMode: profile.QualityEvidenceRetentionCompliance,
		RetainUntil: now.Add(30 * 24 * time.Hour), ArchivedAt: archivedAt,
		DatasetVersion: "dataset-v1", DatasetSHA256: datasetHash, ExecutionConfigHash: configHash,
		IntegrityKeyID: "eval-key-v1",
	}
	return payload, reference
}

func signedContentQualifiedQualityEvidence(
	t *testing.T,
	reportKey []byte,
	signoffKey []byte,
	now time.Time,
) ([]byte, profile.QualityEvidenceReference) {
	t.Helper()
	datasetHash := strings.Repeat("a", 64)
	configHash := strings.Repeat("b", 64)
	stableConfigHash := strings.Repeat("c", 64)
	metrics := eval.AgentTaskMetrics{
		Cases: 50, Passed: 50, TaskCompletionRate: 1, ReadToolSelectionAccuracy: 1,
		SemanticPassRate: 1, ApprovalCases: 4, ApprovalHandled: 4, ApprovalPassRate: 1,
	}
	candidateResults := make([]eval.AgentTaskCaseResult, 50)
	stableResults := make([]eval.AgentTaskCaseResult, 50)
	decisions := make([]eval.AgentTaskContentReviewCaseDecision, 50)
	passed := eval.AgentTaskContentReviewAssessment{
		FactualCorrectness: eval.AgentTaskContentReviewPassed,
		Relevance:          eval.AgentTaskContentReviewPassed,
		EvidenceFidelity:   eval.AgentTaskContentReviewPassed,
		WritingQuality:     eval.AgentTaskContentReviewPassed,
	}
	for index := range candidateResults {
		caseID := fmt.Sprintf("case-%02d", index+1)
		candidateResults[index] = eval.AgentTaskCaseResult{CaseID: caseID, OutputSHA256: strings.Repeat("1", 64), Passed: true}
		stableResults[index] = eval.AgentTaskCaseResult{CaseID: caseID, OutputSHA256: strings.Repeat("2", 64), Passed: true}
		decisions[index] = eval.AgentTaskContentReviewCaseDecision{CaseID: caseID, Candidate: passed, Stable: passed}
	}
	candidate := eval.AgentTaskReport{
		GeneratedAt: now.Add(-3 * time.Minute), DatasetVersion: "dataset-v1", DatasetSHA256: datasetHash,
		ExecutionConfigHash: configHash,
		Execution:           eval.AgentTaskExecutionDescriptor{Kind: "runtime_live", ProfileID: "writer", ProfileVersion: "v2", Strategy: eval.AgentStrategyMulti},
		Metrics:             metrics,
		CaseResults:         candidateResults,
	}
	stable := candidate
	stable.ExecutionConfigHash = stableConfigHash
	stable.Execution.ProfileVersion = "v1"
	stable.Execution.Strategy = eval.AgentStrategySingle
	stable.CaseResults = stableResults
	output := eval.AgentTaskEvaluationOutput{
		SchemaVersion: eval.AgentTaskEvaluationSchemaVersion,
		Candidate:     candidate,
		Stable:        &stable,
		Gate:          &eval.AgentQualityGateDecision{Status: eval.AgentQualityGatePassed},
		StrategyGate:  &eval.AgentStrategyGateDecision{Status: eval.AgentQualityGatePassed},
	}
	if err := eval.SignAgentTaskEvaluationOutput(&output, reportKey, "eval-key-v1", now.Add(-2*time.Minute)); err != nil {
		t.Fatalf("SignAgentTaskEvaluationOutput() error = %v", err)
	}
	binding := eval.AgentTaskContentReviewBundleBinding{
		SchemaVersion: eval.AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-key-v1",
		FileSHA256:    strings.Repeat("d", 64),
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
			ExternalRecordSHA256: strings.Repeat("e", 64),
		},
		ReviewedAt:       now.Add(-90 * time.Second),
		CandidateVerdict: eval.AgentTaskContentReviewApproved,
		StableVerdict:    eval.AgentTaskContentReviewApproved,
		Cases:            decisions,
	}
	signoff, err := eval.BuildAndSignAgentTaskContentReviewSignoff(
		output, binding, decision, signoffKey, "content-signoff-v1", now.Add(-time.Minute),
	)
	if err != nil {
		t.Fatalf("BuildAndSignAgentTaskContentReviewSignoff() error = %v", err)
	}
	qualified, err := eval.BuildAgentTaskContentQualifiedEvidence(
		output, signoff, reportKey, "eval-key-v1", signoffKey, "content-signoff-v1",
	)
	if err != nil {
		t.Fatalf("BuildAgentTaskContentQualifiedEvidence() error = %v", err)
	}
	payload, err := eval.MarshalAgentTaskContentQualifiedEvidence(qualified)
	if err != nil {
		t.Fatalf("MarshalAgentTaskContentQualifiedEvidence() error = %v", err)
	}
	digest := sha256.Sum256(payload)
	archivedAt := now.Add(-30 * time.Second)
	reference := profile.QualityEvidenceReference{
		Storage: profile.QualityEvidenceStorageMinIO, Bucket: "agent-eval", Key: "agent-task-eval/a/qualified.json",
		VersionID: "version-1", ReportSHA256: hex.EncodeToString(digest[:]), Length: len(payload),
		ContentType: profile.QualityEvidenceContentTypeJSON, RetentionMode: profile.QualityEvidenceRetentionCompliance,
		RetainUntil: now.Add(30 * 24 * time.Hour), ArchivedAt: archivedAt,
		DatasetVersion: "dataset-v1", DatasetSHA256: datasetHash, ExecutionConfigHash: configHash,
		IntegrityKeyID: "eval-key-v1",
	}
	return payload, reference
}
