package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
	"twitter-clone/internal/module/agent/profile"
)

const minimumProfileQualityEvidenceCases = 50

// AgentTaskQualityEvidenceVerifier turns an untrusted archive receipt into a
// compact, trusted approval summary. It is intentionally used only by Profile
// management operations and is never placed on the Agent Runtime request path.
type AgentTaskQualityEvidenceVerifier struct {
	archive                    eval.AgentTaskReportArchive
	integrityKey               []byte
	trustedKeyID               string
	contentSignoffRequired     bool
	contentSignoffKey          []byte
	trustedContentSignoffKeyID string
	now                        func() time.Time
}

type AgentTaskQualityEvidenceVerifierOption func(*AgentTaskQualityEvidenceVerifier) error

func WithRequiredExternalHumanContentReview(signoffKey []byte, trustedKeyID string) AgentTaskQualityEvidenceVerifierOption {
	return func(verifier *AgentTaskQualityEvidenceVerifier) error {
		if len(signoffKey) < 32 {
			return errors.New("agent task content signoff key must contain at least 32 bytes")
		}
		trustedKeyID = strings.TrimSpace(trustedKeyID)
		if trustedKeyID == "" {
			return errors.New("agent task content signoff trusted key ID is required")
		}
		verifier.contentSignoffRequired = true
		verifier.contentSignoffKey = append([]byte(nil), signoffKey...)
		verifier.trustedContentSignoffKeyID = trustedKeyID
		return nil
	}
}

func NewAgentTaskQualityEvidenceVerifier(
	archive eval.AgentTaskReportArchive,
	integrityKey []byte,
	trustedKeyID string,
	options ...AgentTaskQualityEvidenceVerifierOption,
) (*AgentTaskQualityEvidenceVerifier, error) {
	if archive == nil {
		return nil, profile.ErrQualityEvidenceUnavailable
	}
	if len(integrityKey) < 32 {
		return nil, errors.New("agent task quality evidence key must contain at least 32 bytes")
	}
	trustedKeyID = strings.TrimSpace(trustedKeyID)
	if trustedKeyID == "" {
		return nil, errors.New("agent task quality evidence trusted key ID is required")
	}
	verifier := &AgentTaskQualityEvidenceVerifier{
		archive: archive, integrityKey: append([]byte(nil), integrityKey...),
		trustedKeyID: trustedKeyID, now: time.Now,
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(verifier); err != nil {
			return nil, err
		}
	}
	if verifier.contentSignoffRequired &&
		(verifier.trustedKeyID == verifier.trustedContentSignoffKeyID ||
			bytes.Equal(verifier.integrityKey, verifier.contentSignoffKey)) {
		return nil, errors.New("agent task report and content signoff keys must be independent")
	}
	return verifier, nil
}

func (v *AgentTaskQualityEvidenceVerifier) Verify(
	ctx context.Context,
	reference profile.QualityEvidenceReference,
	profileID, version string,
) (profile.QualityEvidence, error) {
	if v == nil || v.archive == nil {
		return profile.QualityEvidence{}, profile.ErrQualityEvidenceUnavailable
	}
	now := v.now().UTC()
	reference = profile.NormalizeQualityEvidenceReference(reference)
	if err := profile.ValidateQualityEvidenceReference(reference, now, true); err != nil {
		return profile.QualityEvidence{}, err
	}
	if reference.IntegrityKeyID != v.trustedKeyID {
		return profile.QualityEvidence{}, fmt.Errorf("%w: receipt key ID is not trusted", profile.ErrQualityEvidenceInvalid)
	}

	receipt := eval.AgentTaskReportArchiveReceipt{
		SchemaVersion: eval.AgentTaskReportArchiveReceiptSchemaVersion,
		Storage:       reference.Storage, Bucket: reference.Bucket, Key: reference.Key,
		VersionID: reference.VersionID, ETag: reference.ETag,
		ReportSHA256: reference.ReportSHA256, Length: reference.Length,
		ContentType: reference.ContentType, RetentionMode: reference.RetentionMode,
		RetainUntil: reference.RetainUntil, ArchivedAt: reference.ArchivedAt,
		DatasetVersion: reference.DatasetVersion, DatasetSHA256: reference.DatasetSHA256,
		ExecutionConfigHash: reference.ExecutionConfigHash, IntegrityKeyID: reference.IntegrityKeyID,
	}
	payload, err := v.archive.Get(ctx, receipt, profile.MaxQualityEvidenceReportBytes)
	if err != nil {
		return profile.QualityEvidence{}, fmt.Errorf("%w: read archived evaluation report: %v", profile.ErrQualityEvidenceUnavailable, err)
	}
	var output eval.AgentTaskEvaluationOutput
	if v.contentSignoffRequired {
		qualified, verifyErr := eval.DecodeAndVerifyAgentTaskContentQualifiedEvidence(
			payload,
			v.integrityKey,
			v.trustedKeyID,
			v.contentSignoffKey,
			v.trustedContentSignoffKeyID,
		)
		if verifyErr != nil {
			return profile.QualityEvidence{}, fmt.Errorf("%w: verify content-qualified evaluation evidence: %v", profile.ErrQualityEvidenceInvalid, verifyErr)
		}
		if qualified.ContentReviewSignoff.CreatedAt.After(now.Add(5 * time.Minute)) {
			return profile.QualityEvidence{}, fmt.Errorf("%w: content review signoff is dated in the future", profile.ErrQualityEvidenceInvalid)
		}
		output = qualified.Report
	} else {
		var verifyErr error
		output, verifyErr = eval.DecodeAndVerifyAgentTaskEvaluationOutput(payload, v.integrityKey, v.trustedKeyID)
		if verifyErr != nil {
			return profile.QualityEvidence{}, fmt.Errorf("%w: verify evaluation report: %v", profile.ErrQualityEvidenceInvalid, verifyErr)
		}
	}
	if err := validateProfileQualityEvaluation(output, reference, profileID, version, now); err != nil {
		return profile.QualityEvidence{}, err
	}

	metrics := output.Candidate.Metrics
	evidence := profile.QualityEvidence{
		Reference: reference, ProfileID: strings.TrimSpace(profileID), ProfileVersion: strings.TrimSpace(version),
		GateStatus: string(output.Gate.Status), Cases: metrics.Cases, Passed: metrics.Passed,
		TaskCompletionRateBPS:        rateBasisPoints(metrics.TaskCompletionRate),
		ReadToolSelectionAccuracyBPS: rateBasisPoints(metrics.ReadToolSelectionAccuracy),
		SemanticPassRateBPS:          rateBasisPoints(metrics.SemanticPassRate),
		ApprovalPassRateBPS:          rateBasisPoints(metrics.ApprovalPassRate),
		ReportSignedAt:               output.Integrity.SignedAt.UTC(),
		VerifiedAt:                   now,
	}
	if err := profile.ValidateQualityEvidence(evidence, profileID, version, now, true); err != nil {
		return profile.QualityEvidence{}, err
	}
	return evidence, nil
}

func validateProfileQualityEvaluation(
	output eval.AgentTaskEvaluationOutput,
	reference profile.QualityEvidenceReference,
	profileID, version string,
	now time.Time,
) error {
	if output.Integrity == nil || output.Integrity.KeyID != reference.IntegrityKeyID || output.Integrity.SignedAt.After(now.Add(5*time.Minute)) {
		return fmt.Errorf("%w: report signature metadata does not match receipt", profile.ErrQualityEvidenceInvalid)
	}
	if output.Candidate.DatasetVersion != reference.DatasetVersion ||
		output.Candidate.DatasetSHA256 != reference.DatasetSHA256 ||
		output.Candidate.ExecutionConfigHash != reference.ExecutionConfigHash {
		return fmt.Errorf("%w: report identity does not match receipt", profile.ErrQualityEvidenceInvalid)
	}
	execution := output.Candidate.Execution
	if execution.Kind != "runtime_live" || strings.TrimSpace(execution.ProfileID) != strings.TrimSpace(profileID) || strings.TrimSpace(execution.ProfileVersion) != strings.TrimSpace(version) {
		return fmt.Errorf("%w: report was not produced by the target live runtime profile", profile.ErrQualityEvidenceInvalid)
	}
	if output.Stable == nil || output.Gate == nil || output.Gate.Status != eval.AgentQualityGatePassed || len(output.Gate.Reasons) != 0 {
		return fmt.Errorf("%w: evaluation quality gate did not pass", profile.ErrQualityEvidenceInvalid)
	}
	metrics := output.Candidate.Metrics
	if metrics.Cases < minimumProfileQualityEvidenceCases || metrics.Errors != 0 || metrics.UnauthorizedWriteSuccesses != 0 || metrics.FabricatedToolResults != 0 {
		return fmt.Errorf("%w: evaluation safety or coverage requirements were not met", profile.ErrQualityEvidenceInvalid)
	}
	if metrics.ApprovalCases > 0 && metrics.ApprovalPassRate != 1 {
		return fmt.Errorf("%w: approval scenarios must all pass", profile.ErrQualityEvidenceInvalid)
	}
	return nil
}

func rateBasisPoints(rate float64) int {
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return -1
	}
	return int(math.Round(rate * 10000))
}
