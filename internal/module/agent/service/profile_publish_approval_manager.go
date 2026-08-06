package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
)

const profilePublishApplyLease = time.Minute

var ErrProfilePublishApprovalUnavailable = errors.New("profile publish approval repository is not configured")

// RequestPublishApproval freezes the draft revision and content digest that a
// second operator must review. Immutable profile versions make the binding
// stable for the entire approval lifecycle.
func (m *ProfileCatalogManager) RequestPublishApproval(
	ctx context.Context,
	profileID, version string,
	expectedVersionRevision int64,
	requestedBy uint64,
) (*repository.ProfilePublishApprovalRecord, error) {
	return m.RequestPublishApprovalWithEvidence(ctx, profileID, version, expectedVersionRevision, requestedBy, nil)
}

func (m *ProfileCatalogManager) RequestPublishApprovalWithEvidence(
	ctx context.Context,
	profileID, version string,
	expectedVersionRevision int64,
	requestedBy uint64,
	reference *profile.QualityEvidenceReference,
) (*repository.ProfilePublishApprovalRecord, error) {
	if m.approvalRepository == nil {
		return nil, ErrProfilePublishApprovalUnavailable
	}
	if requestedBy == 0 || expectedVersionRevision < 1 {
		return nil, errors.New("profile publish requester and expected version revision are required")
	}
	record, err := m.repository.GetProfileVersion(ctx, profileID, version)
	if err != nil {
		return nil, err
	}
	if record.Status != repository.ProfileVersionStatusDraft || record.Revision != expectedVersionRevision {
		return nil, repository.ErrProfileVersionConflict
	}
	if _, err := decodeProfileVersion(record); err != nil {
		return nil, fmt.Errorf("profile version cannot be submitted for approval: %w", err)
	}
	qualityEvidence, err := m.verifyRequestedQualityEvidence(ctx, reference, record.ProfileID, record.Version)
	if err != nil {
		return nil, err
	}
	approval := &repository.ProfilePublishApprovalRecord{
		ProfileID:               record.ProfileID,
		Version:                 record.Version,
		SnapshotHash:            record.SnapshotHash,
		ExpectedVersionRevision: record.Revision,
		RequestedBy:             requestedBy,
		QualityEvidence:         qualityEvidence,
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return nil, err
	}
	audit := repository.ProfileAuditEvent{
		OperationID: operationID, Action: repository.ProfileAuditActionRequestPublish,
		ProfileID: record.ProfileID, Version: record.Version, ActorUserID: requestedBy,
		VersionRevision: record.Revision, SnapshotHash: record.SnapshotHash,
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return nil, fmt.Errorf("profile publish request audit failed before mutation: %w", err)
	}
	if err := m.approvalRepository.CreateProfilePublishApproval(ctx, approval); err != nil {
		return nil, m.finishFailedProfileMutation(ctx, audit, err)
	}
	audit.ApprovalID = approval.ID.Hex()
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
		return approval, fmt.Errorf("profile publish request was stored but final audit failed: %w", err)
	}
	return approval, nil
}

// DecidePublishApproval enforces two-person control. Approval claims a short
// application lease before invoking the existing CAS publication primitive;
// rejection never mutates the profile version.
func (m *ProfileCatalogManager) DecidePublishApproval(
	ctx context.Context,
	approvalID string,
	expectedApprovalRevision int64,
	actorUserID uint64,
	decision, reason string,
) (*repository.ProfilePublishApprovalRecord, error) {
	if m.approvalRepository == nil {
		return nil, ErrProfilePublishApprovalUnavailable
	}
	if actorUserID == 0 || expectedApprovalRevision < 1 {
		return nil, errors.New("profile publish approval actor and expected revision are required")
	}
	current, err := m.approvalRepository.GetProfilePublishApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}
	if current.RequestedBy == actorUserID {
		return nil, repository.ErrProfilePublishSelfApproval
	}
	decision = strings.TrimSpace(decision)
	if decision == repository.ProfilePublishDecisionApproved {
		if err := m.validateApprovalTarget(ctx, current); err != nil {
			return nil, err
		}
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return nil, err
	}
	audit := repository.ProfileAuditEvent{
		OperationID: operationID, Action: repository.ProfileAuditActionDecidePublish,
		ProfileID: current.ProfileID, Version: current.Version, ApprovalID: current.ID.Hex(),
		ActorUserID: actorUserID, VersionRevision: current.ExpectedVersionRevision,
		SnapshotHash: current.SnapshotHash,
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return nil, fmt.Errorf("profile publish decision audit failed before mutation: %w", err)
	}
	claimed, err := m.approvalRepository.DecideProfilePublishApproval(
		ctx, approvalID, expectedApprovalRevision, actorUserID, decision, reason, profilePublishApplyLease,
	)
	if err != nil {
		return nil, m.finishFailedProfileMutation(ctx, audit, err)
	}
	if decision == repository.ProfilePublishDecisionRejected {
		if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
			return claimed, fmt.Errorf("profile publish rejection was stored but final audit failed: %w", err)
		}
		return claimed, nil
	}
	completed, applyErr := m.applyClaimedPublishApproval(ctx, claimed, actorUserID)
	if applyErr != nil {
		if auditErr := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeFailed, profileMutationErrorCode(applyErr)); auditErr != nil {
			return completed, errors.Join(applyErr, fmt.Errorf("final profile approval audit failed: %w", auditErr))
		}
		return completed, applyErr
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
		return completed, fmt.Errorf("profile publish approval was applied but final audit failed: %w", err)
	}
	return completed, nil
}

// RetryPublishApproval reclaims a failed or lease-expired application. If the
// version was published before a crash, it only reconciles the approval record.
func (m *ProfileCatalogManager) RetryPublishApproval(
	ctx context.Context,
	approvalID string,
	expectedApprovalRevision int64,
	actorUserID uint64,
) (*repository.ProfilePublishApprovalRecord, error) {
	if m.approvalRepository == nil {
		return nil, ErrProfilePublishApprovalUnavailable
	}
	current, err := m.approvalRepository.GetProfilePublishApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}
	if current.RequestedBy == actorUserID {
		return nil, repository.ErrProfilePublishSelfApproval
	}
	if err := m.reverifyApprovalQualityEvidence(ctx, current); err != nil {
		return nil, err
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return nil, err
	}
	audit := repository.ProfileAuditEvent{
		OperationID: operationID, Action: repository.ProfileAuditActionRetryPublish,
		ProfileID: current.ProfileID, Version: current.Version, ApprovalID: current.ID.Hex(),
		ActorUserID: actorUserID, VersionRevision: current.ExpectedVersionRevision,
		SnapshotHash: current.SnapshotHash,
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeRequested, ""); err != nil {
		return nil, fmt.Errorf("profile publish retry audit failed before mutation: %w", err)
	}
	claimed, err := m.approvalRepository.ClaimProfilePublishApprovalRetry(
		ctx, approvalID, expectedApprovalRevision, actorUserID, profilePublishApplyLease,
	)
	if err != nil {
		return nil, m.finishFailedProfileMutation(ctx, audit, err)
	}
	completed, applyErr := m.applyClaimedPublishApproval(ctx, claimed, actorUserID)
	if applyErr != nil {
		if auditErr := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeFailed, profileMutationErrorCode(applyErr)); auditErr != nil {
			return completed, errors.Join(applyErr, fmt.Errorf("final profile approval retry audit failed: %w", auditErr))
		}
		return completed, applyErr
	}
	if err := m.appendProfileAudit(ctx, audit, repository.ProfileAuditOutcomeSucceeded, ""); err != nil {
		return completed, fmt.Errorf("profile publish retry was applied but final audit failed: %w", err)
	}
	return completed, nil
}

func (m *ProfileCatalogManager) GetPublishApproval(ctx context.Context, approvalID string) (*repository.ProfilePublishApprovalRecord, error) {
	if m.approvalRepository == nil {
		return nil, ErrProfilePublishApprovalUnavailable
	}
	return m.approvalRepository.GetProfilePublishApproval(ctx, approvalID)
}

func (m *ProfileCatalogManager) ListPublishApprovals(ctx context.Context, profileID, status string, page, pageSize int) ([]*repository.ProfilePublishApprovalRecord, int64, error) {
	if m.approvalRepository == nil {
		return nil, 0, ErrProfilePublishApprovalUnavailable
	}
	return m.approvalRepository.ListProfilePublishApprovals(ctx, profileID, status, page, pageSize)
}

func (m *ProfileCatalogManager) validateApprovalTarget(ctx context.Context, approval *repository.ProfilePublishApprovalRecord) error {
	if approval == nil {
		return repository.ErrProfilePublishApprovalNotFound
	}
	version, err := m.repository.GetProfileVersion(ctx, approval.ProfileID, approval.Version)
	if err != nil {
		return err
	}
	if version.Status != repository.ProfileVersionStatusDraft ||
		version.Revision != approval.ExpectedVersionRevision ||
		version.SnapshotHash != approval.SnapshotHash {
		return repository.ErrProfileVersionConflict
	}
	if _, err := decodeProfileVersion(version); err != nil {
		return fmt.Errorf("approved profile target is invalid: %w", err)
	}
	if err := m.reverifyApprovalQualityEvidence(ctx, approval); err != nil {
		return err
	}
	return nil
}

func (m *ProfileCatalogManager) verifyRequestedQualityEvidence(
	ctx context.Context,
	reference *profile.QualityEvidenceReference,
	profileID, version string,
) (*profile.QualityEvidence, error) {
	if reference == nil {
		if m.qualityEvidenceRequired {
			return nil, profile.ErrQualityEvidenceRequired
		}
		return nil, nil
	}
	if m.qualityEvidenceVerifier == nil {
		return nil, profile.ErrQualityEvidenceUnavailable
	}
	verified, err := m.qualityEvidenceVerifier.Verify(ctx, *reference, profileID, version)
	if err != nil {
		return nil, err
	}
	return &verified, nil
}

func (m *ProfileCatalogManager) reverifyApprovalQualityEvidence(ctx context.Context, approval *repository.ProfilePublishApprovalRecord) error {
	if approval == nil {
		return repository.ErrProfilePublishApprovalNotFound
	}
	if approval.QualityEvidence == nil {
		if m.qualityEvidenceRequired {
			return profile.ErrQualityEvidenceRequired
		}
		return nil
	}
	if m.qualityEvidenceVerifier == nil {
		if m.qualityEvidenceRequired {
			return profile.ErrQualityEvidenceUnavailable
		}
		return nil
	}
	verified, err := m.qualityEvidenceVerifier.Verify(
		ctx,
		approval.QualityEvidence.Reference,
		approval.ProfileID,
		approval.Version,
	)
	if err != nil {
		return err
	}
	if !qualityEvidenceSummaryMatches(*approval.QualityEvidence, verified) {
		return fmt.Errorf("%w: stored approval summary does not match archived report", profile.ErrQualityEvidenceInvalid)
	}
	return nil
}

func qualityEvidenceSummaryMatches(stored, verified profile.QualityEvidence) bool {
	return profile.QualityEvidenceIdentity(stored.Reference) == profile.QualityEvidenceIdentity(verified.Reference) &&
		stored.ProfileID == verified.ProfileID && stored.ProfileVersion == verified.ProfileVersion &&
		stored.GateStatus == verified.GateStatus && stored.Cases == verified.Cases && stored.Passed == verified.Passed &&
		stored.TaskCompletionRateBPS == verified.TaskCompletionRateBPS &&
		stored.ReadToolSelectionAccuracyBPS == verified.ReadToolSelectionAccuracyBPS &&
		stored.SemanticPassRateBPS == verified.SemanticPassRateBPS &&
		stored.ApprovalPassRateBPS == verified.ApprovalPassRateBPS &&
		stored.ReportSignedAt.Equal(verified.ReportSignedAt)
}

func (m *ProfileCatalogManager) applyClaimedPublishApproval(
	ctx context.Context,
	approval *repository.ProfilePublishApprovalRecord,
	actorUserID uint64,
) (*repository.ProfilePublishApprovalRecord, error) {
	version, err := m.repository.GetProfileVersion(ctx, approval.ProfileID, approval.Version)
	if err == nil && version.Status == repository.ProfileVersionStatusPublished && version.SnapshotHash == approval.SnapshotHash {
		if err := m.reconcilePublishedApproval(ctx, approval); err != nil {
			return m.completePublishApprovalFailure(ctx, approval, err)
		}
		return m.approvalRepository.CompleteProfilePublishApproval(ctx, approval.ID.Hex(), approval.Revision, true, "")
	}
	if err != nil || version.Status != repository.ProfileVersionStatusDraft ||
		version.Revision != approval.ExpectedVersionRevision || version.SnapshotHash != approval.SnapshotHash {
		if err == nil {
			err = repository.ErrProfileVersionConflict
		}
		return m.completePublishApprovalFailure(ctx, approval, err)
	}
	if err := m.PublishVersion(ctx, approval.ProfileID, approval.Version, approval.ExpectedVersionRevision, actorUserID); err != nil {
		return m.completePublishApprovalFailure(ctx, approval, err)
	}
	return m.approvalRepository.CompleteProfilePublishApproval(ctx, approval.ID.Hex(), approval.Revision, true, "")
}

func (m *ProfileCatalogManager) reconcilePublishedApproval(
	ctx context.Context,
	approval *repository.ProfilePublishApprovalRecord,
) error {
	if err := m.Reload(ctx); err != nil {
		return fmt.Errorf("reconcile published profile catalog: %w", err)
	}
	operationID, err := newProfileOperationID()
	if err != nil {
		return err
	}
	if err := m.publishProfileChange(ctx, repository.ProfileAuditEvent{
		OperationID: operationID, ProfileID: approval.ProfileID,
		VersionRevision: approval.ExpectedVersionRevision + 1,
	}); err != nil {
		return fmt.Errorf("reconcile published profile notification: %w", err)
	}
	return nil
}

func (m *ProfileCatalogManager) completePublishApprovalFailure(
	ctx context.Context,
	approval *repository.ProfilePublishApprovalRecord,
	cause error,
) (*repository.ProfilePublishApprovalRecord, error) {
	completed, completionErr := m.approvalRepository.CompleteProfilePublishApproval(
		ctx, approval.ID.Hex(), approval.Revision, false, profileMutationErrorCode(cause),
	)
	if completionErr != nil {
		return completed, errors.Join(cause, fmt.Errorf("mark profile publish approval failed: %w", completionErr))
	}
	return completed, cause
}
