package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

type memoryAgentTaskReportArchive struct {
	payload      []byte
	receipt      eval.AgentTaskReportArchiveReceipt
	ensureCalls  int
	tamperOnRead bool
}

func (a *memoryAgentTaskReportArchive) Ensure(context.Context) error {
	a.ensureCalls++
	return nil
}

func (a *memoryAgentTaskReportArchive) PutImmutable(_ context.Context, request eval.AgentTaskReportArchiveRequest) (eval.AgentTaskReportArchiveReceipt, error) {
	if err := eval.ValidateAgentTaskReportArchiveRequest(request, time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)); err != nil {
		return eval.AgentTaskReportArchiveReceipt{}, err
	}
	a.payload = append([]byte(nil), request.Payload...)
	a.receipt = eval.AgentTaskReportArchiveReceipt{
		SchemaVersion:       eval.AgentTaskReportArchiveReceiptSchemaVersion,
		Storage:             "minio",
		Bucket:              "agent-task-eval",
		Key:                 "agent-task-eval/dataset/hash/config/2026/07/22/" + request.ReportSHA256 + ".json",
		VersionID:           "version-1",
		ETag:                "etag",
		ReportSHA256:        request.ReportSHA256,
		Length:              len(request.Payload),
		ContentType:         eval.AgentTaskReportArchiveContentType,
		RetentionMode:       eval.AgentTaskReportRetentionCompliance,
		RetainUntil:         request.RetainUntil,
		ArchivedAt:          time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC),
		Created:             true,
		DatasetVersion:      request.DatasetVersion,
		DatasetSHA256:       request.DatasetSHA256,
		ExecutionConfigHash: request.ExecutionConfigHash,
		IntegrityKeyID:      request.IntegrityKeyID,
	}
	return a.receipt, nil
}

func (a *memoryAgentTaskReportArchive) Get(context.Context, eval.AgentTaskReportArchiveReceipt, int) ([]byte, error) {
	if len(a.payload) == 0 {
		return nil, errors.New("missing archived report")
	}
	payload := append([]byte(nil), a.payload...)
	if a.tamperOnRead {
		payload[len(payload)-1] ^= 1
	}
	return payload, nil
}

func TestArchiveEvaluationOutputRoundTripVerifiesSignatureAndReceipt(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	output := signedArchiveTestOutput(t, []byte(key), now)
	archive := &memoryAgentTaskReportArchive{}
	receipt, err := archiveEvaluationOutput(context.Background(), archive, output, []byte(key), "test-key-v1", 365, now)
	if err != nil {
		t.Fatalf("archive evaluation output: %v", err)
	}
	if archive.ensureCalls != 1 || receipt.ReportSHA256 != sha256Hex(archive.payload) || receipt.ExecutionConfigHash != output.Candidate.ExecutionConfigHash {
		t.Fatalf("unexpected archive evidence: calls=%d receipt=%#v", archive.ensureCalls, receipt)
	}
	verified, err := verifyArchivedEvaluationOutput(context.Background(), archive, receipt, []byte(key), "test-key-v1")
	if err != nil {
		t.Fatalf("verify archived output: %v", err)
	}
	if verified.Candidate.DatasetSHA256 != output.Candidate.DatasetSHA256 || verified.Integrity == nil {
		t.Fatalf("unexpected verified output: %#v", verified)
	}
}

func TestArchiveEvaluationOutputRejectsTamperedRemotePayload(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	archive := &memoryAgentTaskReportArchive{tamperOnRead: true}
	_, err := archiveEvaluationOutput(context.Background(), archive, signedArchiveTestOutput(t, []byte(key), now), []byte(key), "test-key-v1", 365, now)
	if err == nil || !strings.Contains(err.Error(), "verify archived report") {
		t.Fatalf("expected archived payload verification failure, got %v", err)
	}
}

func TestArchiveContentQualifiedEvaluationOutputRoundTrip(t *testing.T) {
	now := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	output, signoff, reportKey, signoffKey := contentQualifiedArchiveFixture(t, now)
	archive := &memoryAgentTaskReportArchive{}
	receipt, err := archiveContentQualifiedEvaluationOutput(
		context.Background(), archive, output, signoff,
		reportKey, "report-key-v1", signoffKey, "content-signoff-v1", 365, now,
	)
	if err != nil {
		t.Fatalf("archive content-qualified evidence: %v", err)
	}
	if receipt.ReportSHA256 != sha256Hex(archive.payload) || receipt.IntegrityKeyID != "report-key-v1" {
		t.Fatalf("unexpected content-qualified receipt: %#v", receipt)
	}
	verified, err := verifyArchivedContentQualifiedEvaluationOutput(
		context.Background(), archive, receipt,
		reportKey, "report-key-v1", signoffKey, "content-signoff-v1",
	)
	if err != nil {
		t.Fatalf("verify archived content-qualified evidence: %v", err)
	}
	if !eval.AgentTaskContentReviewHasApprovedExternalHumanSignoff(verified.ContentReviewSignoff) {
		t.Fatalf("archived evidence lost external human approval: %#v", verified.ContentReviewSignoff)
	}
}

func TestArchiveContentQualifiedEvaluationOutputRejectsTamperedRemotePayload(t *testing.T) {
	now := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	output, signoff, reportKey, signoffKey := contentQualifiedArchiveFixture(t, now)
	archive := &memoryAgentTaskReportArchive{tamperOnRead: true}
	_, err := archiveContentQualifiedEvaluationOutput(
		context.Background(), archive, output, signoff,
		reportKey, "report-key-v1", signoffKey, "content-signoff-v1", 365, now,
	)
	if err == nil || !strings.Contains(err.Error(), "verify archived content-qualified evidence") {
		t.Fatalf("expected archived content-qualified tamper rejection, got %v", err)
	}
}

func TestLoadAgentTaskArchiveConfigRejectsInlineCredentialFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.json")
	payload := []byte(`{
		"schema_version":"agent-task-eval-archive-config/v1",
		"endpoint":"localhost:9000",
		"secure":false,
		"bucket":"agent-task-eval",
		"access_key_env":"MINIO_ACCESS_KEY",
		"secret_key_env":"MINIO_SECRET_KEY",
		"retention_days":365,
		"secret_key":"must-not-be-accepted"
	}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write archive config: %v", err)
	}
	_, err := loadAgentTaskArchiveConfig(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected inline credential rejection, got %v", err)
	}
}

func TestWriteAgentTaskArchiveReceiptIsAppendOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.json")
	receipt := eval.AgentTaskReportArchiveReceipt{SchemaVersion: eval.AgentTaskReportArchiveReceiptSchemaVersion}
	if err := writeAgentTaskArchiveReceipt(path, receipt); err != nil {
		t.Fatalf("write first receipt: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read first receipt: %v", err)
	}
	if err := writeAgentTaskArchiveReceipt(path, receipt); err == nil {
		t.Fatal("expected existing receipt path to be rejected")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preserved receipt: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("existing receipt was modified")
	}
}

func signedArchiveTestOutput(t *testing.T, key []byte, signedAt time.Time) agentTaskEvaluationOutput {
	t.Helper()
	output := agentTaskEvaluationOutput{
		SchemaVersion: agentTaskEvaluationSchemaVersion,
		Candidate: eval.AgentTaskReport{
			DatasetVersion:      "agent-task-cases-v1",
			DatasetSHA256:       strings.Repeat("a", 64),
			ExecutionConfigHash: strings.Repeat("b", 64),
		},
	}
	if err := signEvaluationOutput(&output, key, "test-key-v1", signedAt); err != nil {
		t.Fatalf("sign archive test output: %v", err)
	}
	encoded, err := json.Marshal(output)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("encode signed archive test output: %v", err)
	}
	return output
}

func contentQualifiedArchiveFixture(
	t *testing.T,
	now time.Time,
) (agentTaskEvaluationOutput, eval.AgentTaskContentReviewSignoff, []byte, []byte) {
	t.Helper()
	reportKey := []byte("content-qualified-report-key-material-v1")
	signoffKey := []byte("content-qualified-signoff-key-material-v1")
	result := func(caseID, digest string) eval.AgentTaskCaseResult {
		return eval.AgentTaskCaseResult{CaseID: caseID, OutputSHA256: digest, Passed: true}
	}
	candidate := eval.AgentTaskReport{
		DatasetVersion: "agent-strategy-cases-v3", DatasetSHA256: strings.Repeat("a", 64),
		ExecutionConfigHash: strings.Repeat("b", 64),
		Execution:           eval.AgentTaskExecutionDescriptor{Kind: "runtime_live", Strategy: eval.AgentStrategyMulti},
		Metrics:             eval.AgentTaskMetrics{Cases: 2, Passed: 2},
		CaseResults: []eval.AgentTaskCaseResult{
			result("case-1", strings.Repeat("1", 64)),
			result("case-2", strings.Repeat("2", 64)),
		},
	}
	stable := candidate
	stable.ExecutionConfigHash = strings.Repeat("c", 64)
	stable.Execution.Strategy = eval.AgentStrategySingle
	stable.CaseResults = []eval.AgentTaskCaseResult{
		result("case-1", strings.Repeat("3", 64)),
		result("case-2", strings.Repeat("4", 64)),
	}
	output := agentTaskEvaluationOutput{
		SchemaVersion: agentTaskEvaluationSchemaVersion,
		Candidate:     candidate,
		Stable:        &stable,
		Gate:          &eval.AgentQualityGateDecision{Status: eval.AgentQualityGatePassed},
		StrategyGate:  &eval.AgentStrategyGateDecision{Status: eval.AgentQualityGatePassed},
	}
	if err := signEvaluationOutput(&output, reportKey, "report-key-v1", now.Add(-3*time.Minute)); err != nil {
		t.Fatalf("sign content-qualified report: %v", err)
	}
	binding := eval.AgentTaskContentReviewBundleBinding{
		SchemaVersion: eval.AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-key-v1",
		FileSHA256:    strings.Repeat("d", 64),
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
			ExternalRecordSHA256: strings.Repeat("e", 64),
		},
		ReviewedAt:       now.Add(-2 * time.Minute),
		CandidateVerdict: eval.AgentTaskContentReviewApproved,
		StableVerdict:    eval.AgentTaskContentReviewApproved,
		Cases: []eval.AgentTaskContentReviewCaseDecision{
			{CaseID: "case-1", Candidate: passed, Stable: passed},
			{CaseID: "case-2", Candidate: passed, Stable: passed},
		},
	}
	signoff, err := eval.BuildAndSignAgentTaskContentReviewSignoff(
		output, binding, decision, signoffKey, "content-signoff-v1", now.Add(-time.Minute),
	)
	if err != nil {
		t.Fatalf("build content-qualified signoff: %v", err)
	}
	return output, signoff, reportKey, signoffKey
}
