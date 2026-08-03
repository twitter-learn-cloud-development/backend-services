package profile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	QualityEvidenceStorageMinIO              = "minio"
	QualityEvidenceContentTypeJSON           = "application/json"
	QualityEvidenceRetentionCompliance       = "COMPLIANCE"
	QualityEvidenceGatePassed                = "passed"
	MaxQualityEvidenceReportBytes            = 8 << 20
	QualityEvidenceRequiredEnv               = "AGENT_PROFILE_EVAL_EVIDENCE_REQUIRED"
	QualityEvidenceArchiveEndpointEnv        = "AGENT_TASK_EVAL_ARCHIVE_ENDPOINT"
	QualityEvidenceArchiveAccessKeyEnv       = "AGENT_TASK_EVAL_ARCHIVE_ACCESS_KEY"
	QualityEvidenceArchiveSecretKeyEnv       = "AGENT_TASK_EVAL_ARCHIVE_SECRET_KEY"
	QualityEvidenceArchiveBucketEnv          = "AGENT_TASK_EVAL_ARCHIVE_BUCKET"
	QualityEvidenceArchiveSecureEnv          = "AGENT_TASK_EVAL_ARCHIVE_SECURE"
	QualityEvidenceIntegrityKeyEnv           = "AGENT_TASK_EVAL_INTEGRITY_KEY"
	QualityEvidenceIntegrityKeyIDEnv         = "AGENT_TASK_EVAL_INTEGRITY_KEY_ID"
	QualityEvidenceContentSignoffRequiredEnv = "AGENT_PROFILE_EVAL_CONTENT_SIGNOFF_REQUIRED"
	QualityEvidenceContentSignoffKeyEnv      = "AGENT_TASK_EVAL_REVIEW_SIGNOFF_KEY"
	QualityEvidenceContentSignoffKeyIDEnv    = "AGENT_TASK_EVAL_REVIEW_SIGNOFF_KEY_ID"
)

var (
	ErrQualityEvidenceRequired    = errors.New("agent profile quality evidence is required")
	ErrQualityEvidenceInvalid     = errors.New("agent profile quality evidence is invalid")
	ErrQualityEvidenceUnavailable = errors.New("agent profile quality evidence verifier is unavailable")
)

// QualityEvidenceReference is the untrusted, user-supplied locator for one
// immutable evaluation report. It deliberately contains no model credential,
// prompt, task input, or generated output.
type QualityEvidenceReference struct {
	Storage             string    `bson:"storage" json:"storage"`
	Bucket              string    `bson:"bucket" json:"bucket"`
	Key                 string    `bson:"key" json:"key"`
	VersionID           string    `bson:"version_id" json:"version_id"`
	ETag                string    `bson:"etag,omitempty" json:"etag,omitempty"`
	ReportSHA256        string    `bson:"report_sha256" json:"report_sha256"`
	Length              int       `bson:"length" json:"length"`
	ContentType         string    `bson:"content_type" json:"content_type"`
	RetentionMode       string    `bson:"retention_mode" json:"retention_mode"`
	RetainUntil         time.Time `bson:"retain_until" json:"retain_until"`
	ArchivedAt          time.Time `bson:"archived_at" json:"archived_at"`
	DatasetVersion      string    `bson:"dataset_version" json:"dataset_version"`
	DatasetSHA256       string    `bson:"dataset_sha256" json:"dataset_sha256"`
	ExecutionConfigHash string    `bson:"execution_config_sha256" json:"execution_config_sha256"`
	IntegrityKeyID      string    `bson:"integrity_key_id" json:"integrity_key_id"`
}

// QualityEvidence is the verified, compact approval record. The full report
// remains in immutable object storage and is fetched only on management paths.
type QualityEvidence struct {
	Reference                    QualityEvidenceReference `bson:"reference" json:"reference"`
	ProfileID                    string                   `bson:"profile_id" json:"profile_id"`
	ProfileVersion               string                   `bson:"profile_version" json:"profile_version"`
	GateStatus                   string                   `bson:"gate_status" json:"gate_status"`
	Cases                        int                      `bson:"cases" json:"cases"`
	Passed                       int                      `bson:"passed" json:"passed"`
	TaskCompletionRateBPS        int                      `bson:"task_completion_rate_bps" json:"task_completion_rate_bps"`
	ReadToolSelectionAccuracyBPS int                      `bson:"read_tool_selection_accuracy_bps" json:"read_tool_selection_accuracy_bps"`
	SemanticPassRateBPS          int                      `bson:"semantic_pass_rate_bps" json:"semantic_pass_rate_bps"`
	ApprovalPassRateBPS          int                      `bson:"approval_pass_rate_bps" json:"approval_pass_rate_bps"`
	ReportSignedAt               time.Time                `bson:"report_signed_at" json:"report_signed_at"`
	VerifiedAt                   time.Time                `bson:"verified_at" json:"verified_at"`
}

type QualityEvidenceVerifier interface {
	Verify(ctx context.Context, reference QualityEvidenceReference, profileID, version string) (QualityEvidence, error)
}

func NormalizeQualityEvidenceReference(reference QualityEvidenceReference) QualityEvidenceReference {
	reference.Storage = strings.ToLower(strings.TrimSpace(reference.Storage))
	reference.Bucket = strings.TrimSpace(reference.Bucket)
	reference.Key = strings.TrimSpace(reference.Key)
	reference.VersionID = strings.TrimSpace(reference.VersionID)
	reference.ETag = strings.Trim(strings.TrimSpace(reference.ETag), "\"")
	reference.ReportSHA256 = strings.ToLower(strings.TrimSpace(reference.ReportSHA256))
	reference.ContentType = strings.ToLower(strings.TrimSpace(reference.ContentType))
	reference.RetentionMode = strings.ToUpper(strings.TrimSpace(reference.RetentionMode))
	reference.RetainUntil = reference.RetainUntil.UTC()
	reference.ArchivedAt = reference.ArchivedAt.UTC()
	reference.DatasetVersion = strings.TrimSpace(reference.DatasetVersion)
	reference.DatasetSHA256 = strings.ToLower(strings.TrimSpace(reference.DatasetSHA256))
	reference.ExecutionConfigHash = strings.ToLower(strings.TrimSpace(reference.ExecutionConfigHash))
	reference.IntegrityKeyID = strings.TrimSpace(reference.IntegrityKeyID)
	return reference
}

func ValidateQualityEvidenceReference(reference QualityEvidenceReference, now time.Time, requireActiveRetention bool) error {
	reference = NormalizeQualityEvidenceReference(reference)
	if reference.Storage != QualityEvidenceStorageMinIO {
		return fmt.Errorf("%w: unsupported storage %q", ErrQualityEvidenceInvalid, reference.Storage)
	}
	if reference.Bucket == "" || reference.Key == "" || reference.VersionID == "" {
		return fmt.Errorf("%w: object identity is incomplete", ErrQualityEvidenceInvalid)
	}
	if !strings.HasPrefix(reference.Key, "agent-task-eval/") || strings.Contains(reference.Key, "..") || strings.ContainsAny(reference.Key, "\\\r\n") {
		return fmt.Errorf("%w: object key is invalid", ErrQualityEvidenceInvalid)
	}
	if !validQualityEvidenceSHA256(reference.ReportSHA256) || !validQualityEvidenceSHA256(reference.DatasetSHA256) || !validQualityEvidenceSHA256(reference.ExecutionConfigHash) {
		return fmt.Errorf("%w: digest is invalid", ErrQualityEvidenceInvalid)
	}
	if reference.Length < 1 || reference.Length > MaxQualityEvidenceReportBytes || reference.ContentType != QualityEvidenceContentTypeJSON {
		return fmt.Errorf("%w: content metadata is invalid", ErrQualityEvidenceInvalid)
	}
	if reference.RetentionMode != QualityEvidenceRetentionCompliance || reference.ArchivedAt.IsZero() || !reference.RetainUntil.After(reference.ArchivedAt) {
		return fmt.Errorf("%w: compliance retention metadata is invalid", ErrQualityEvidenceInvalid)
	}
	if requireActiveRetention {
		if now.IsZero() {
			now = time.Now()
		}
		now = now.UTC()
		if reference.ArchivedAt.After(now.Add(5 * time.Minute)) {
			return fmt.Errorf("%w: archive timestamp is in the future", ErrQualityEvidenceInvalid)
		}
		if !reference.RetainUntil.After(now) {
			return fmt.Errorf("%w: compliance retention has expired", ErrQualityEvidenceInvalid)
		}
	}
	if reference.DatasetVersion == "" || reference.IntegrityKeyID == "" {
		return fmt.Errorf("%w: report identity is incomplete", ErrQualityEvidenceInvalid)
	}
	return nil
}

func ValidateQualityEvidence(evidence QualityEvidence, profileID, version string, now time.Time, requireActiveRetention bool) error {
	if err := ValidateQualityEvidenceReference(evidence.Reference, now, requireActiveRetention); err != nil {
		return err
	}
	if strings.TrimSpace(evidence.ProfileID) == "" || strings.TrimSpace(evidence.ProfileVersion) == "" ||
		strings.TrimSpace(evidence.ProfileID) != strings.TrimSpace(profileID) || strings.TrimSpace(evidence.ProfileVersion) != strings.TrimSpace(version) {
		return fmt.Errorf("%w: profile identity does not match approval target", ErrQualityEvidenceInvalid)
	}
	if evidence.GateStatus != QualityEvidenceGatePassed || evidence.Cases < 1 || evidence.Passed < 0 || evidence.Passed > evidence.Cases {
		return fmt.Errorf("%w: quality gate summary is invalid", ErrQualityEvidenceInvalid)
	}
	for name, value := range map[string]int{
		"task completion rate":         evidence.TaskCompletionRateBPS,
		"read tool selection accuracy": evidence.ReadToolSelectionAccuracyBPS,
		"semantic pass rate":           evidence.SemanticPassRateBPS,
		"approval pass rate":           evidence.ApprovalPassRateBPS,
	} {
		if value < 0 || value > 10000 {
			return fmt.Errorf("%w: %s basis points are invalid", ErrQualityEvidenceInvalid, name)
		}
	}
	reportSignedAt := evidence.ReportSignedAt.UTC()
	verifiedAt := evidence.VerifiedAt.UTC()
	archivedAt := evidence.Reference.ArchivedAt.UTC()
	if reportSignedAt.IsZero() || verifiedAt.IsZero() || reportSignedAt.After(archivedAt.Add(5*time.Minute)) ||
		verifiedAt.Before(reportSignedAt) || verifiedAt.Before(archivedAt) {
		return fmt.Errorf("%w: verification timestamps are invalid", ErrQualityEvidenceInvalid)
	}
	if !now.IsZero() && verifiedAt.After(now.UTC().Add(5*time.Minute)) {
		return fmt.Errorf("%w: verification timestamp is in the future", ErrQualityEvidenceInvalid)
	}
	return nil
}

func QualityEvidenceIdentity(reference QualityEvidenceReference) string {
	reference = NormalizeQualityEvidenceReference(reference)
	payload := strings.Join([]string{reference.Storage, reference.Bucket, reference.Key, reference.VersionID, reference.ReportSHA256}, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

func validQualityEvidenceSHA256(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
