package repository

import (
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/profile"
)

func TestPrepareProfilePublishApprovalBindsQualityEvidenceIdentity(t *testing.T) {
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	withoutEvidence := &ProfilePublishApprovalRecord{
		ProfileID: "writer", Version: "v2", SnapshotHash: strings.Repeat("d", 64),
		ExpectedVersionRevision: 3, RequestedBy: 42,
	}
	if err := prepareProfilePublishApproval(withoutEvidence, now); err != nil {
		t.Fatalf("prepareProfilePublishApproval() without evidence error = %v", err)
	}

	withEvidence := &ProfilePublishApprovalRecord{
		ProfileID: "writer", Version: "v2", SnapshotHash: strings.Repeat("d", 64),
		ExpectedVersionRevision: 3, RequestedBy: 42,
		QualityEvidence: &profile.QualityEvidence{
			Reference: profile.QualityEvidenceReference{
				Storage: "minio", Bucket: "agent-eval", Key: "agent-task-eval/a/report.json",
				VersionID: "version-1", ReportSHA256: strings.Repeat("a", 64), Length: 1024,
				ContentType: "application/json", RetentionMode: "COMPLIANCE",
				ArchivedAt: now.Add(-time.Hour), RetainUntil: now.Add(30 * 24 * time.Hour),
				DatasetVersion: "dataset-v1", DatasetSHA256: strings.Repeat("b", 64),
				ExecutionConfigHash: strings.Repeat("c", 64), IntegrityKeyID: "eval-key-v1",
			},
			ProfileID: "writer", ProfileVersion: "v2", GateStatus: "passed", Cases: 50, Passed: 50,
			TaskCompletionRateBPS: 10000, ReadToolSelectionAccuracyBPS: 10000,
			SemanticPassRateBPS: 10000, ApprovalPassRateBPS: 10000,
			ReportSignedAt: now.Add(-2 * time.Hour), VerifiedAt: now,
		},
	}
	if err := prepareProfilePublishApproval(withEvidence, now); err != nil {
		t.Fatalf("prepareProfilePublishApproval() with evidence error = %v", err)
	}
	if withoutEvidence.RequestKey == withEvidence.RequestKey {
		t.Fatal("request key must bind quality evidence object identity")
	}
}
