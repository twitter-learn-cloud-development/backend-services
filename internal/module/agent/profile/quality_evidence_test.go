package profile

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateQualityEvidence(t *testing.T) {
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	evidence := validQualityEvidence(now)
	if err := ValidateQualityEvidence(evidence, "writer", "v2", now, true); err != nil {
		t.Fatalf("ValidateQualityEvidence() error = %v", err)
	}

	evidence.Reference.RetainUntil = now.Add(-time.Second)
	if err := ValidateQualityEvidence(evidence, "writer", "v2", now, true); !errors.Is(err, ErrQualityEvidenceInvalid) {
		t.Fatalf("expired evidence error = %v, want ErrQualityEvidenceInvalid", err)
	}
}

func TestValidateQualityEvidenceRejectsImpossibleTimestampOrder(t *testing.T) {
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*QualityEvidence)
	}{
		{
			name: "verified before archive",
			mutate: func(evidence *QualityEvidence) {
				evidence.VerifiedAt = evidence.Reference.ArchivedAt.Add(-time.Second)
			},
		},
		{
			name: "verified in future",
			mutate: func(evidence *QualityEvidence) {
				evidence.VerifiedAt = now.Add(6 * time.Minute)
			},
		},
		{
			name: "archive in future",
			mutate: func(evidence *QualityEvidence) {
				evidence.Reference.ArchivedAt = now.Add(6 * time.Minute)
				evidence.Reference.RetainUntil = now.Add(24 * time.Hour)
				evidence.ReportSignedAt = now
				evidence.VerifiedAt = now
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := validQualityEvidence(now)
			test.mutate(&evidence)
			if err := ValidateQualityEvidence(evidence, "writer", "v2", now, true); !errors.Is(err, ErrQualityEvidenceInvalid) {
				t.Fatalf("ValidateQualityEvidence() error = %v, want ErrQualityEvidenceInvalid", err)
			}
		})
	}
}

func TestQualityEvidenceIdentityBindsObjectVersionAndDigest(t *testing.T) {
	reference := validQualityEvidence(time.Now()).Reference
	base := QualityEvidenceIdentity(reference)
	reference.VersionID = "different-version"
	if QualityEvidenceIdentity(reference) == base {
		t.Fatal("identity must change with object version")
	}
	reference = validQualityEvidence(time.Now()).Reference
	reference.ReportSHA256 = strings.Repeat("f", 64)
	if QualityEvidenceIdentity(reference) == base {
		t.Fatal("identity must change with report digest")
	}
}

func validQualityEvidence(now time.Time) QualityEvidence {
	archivedAt := now.Add(-time.Hour)
	return QualityEvidence{
		Reference: QualityEvidenceReference{
			Storage: QualityEvidenceStorageMinIO, Bucket: "agent-eval", Key: "agent-task-eval/a/report.json",
			VersionID: "v1", ReportSHA256: strings.Repeat("a", 64), Length: 1024,
			ContentType: QualityEvidenceContentTypeJSON, RetentionMode: QualityEvidenceRetentionCompliance,
			ArchivedAt: archivedAt, RetainUntil: now.Add(30 * 24 * time.Hour), DatasetVersion: "dataset-v1",
			DatasetSHA256: strings.Repeat("b", 64), ExecutionConfigHash: strings.Repeat("c", 64), IntegrityKeyID: "eval-key-v1",
		},
		ProfileID: "writer", ProfileVersion: "v2", GateStatus: QualityEvidenceGatePassed,
		Cases: 50, Passed: 50, TaskCompletionRateBPS: 10000, ReadToolSelectionAccuracyBPS: 10000,
		SemanticPassRateBPS: 10000, ApprovalPassRateBPS: 10000,
		ReportSignedAt: archivedAt.Add(-time.Minute), VerifiedAt: now,
	}
}
