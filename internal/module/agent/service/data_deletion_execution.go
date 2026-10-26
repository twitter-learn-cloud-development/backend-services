package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
	"twitter-clone/internal/module/agent/repository"
)

var (
	ErrDataDeletionExecutionUnavailable = errors.New("data deletion execution is unavailable")
	ErrDataDeletionExecutionConflict    = errors.New("data deletion execution checkpoint conflict")
	ErrDataDeletionExecutionLegalHold   = errors.New("data deletion execution is blocked by legal hold")
	ErrDataDeletionExecutionSubjectBusy = errors.New("data deletion execution subject is busy")
)

// DataDeletionStageClaim is a control-plane lease. It does not execute or
// select a storage adapter.
type DataDeletionStageClaim struct {
	Request    *repository.DataDeletionRequest
	Definition agentdeletion.Stage
	Checkpoint agentdeletion.StageCheckpoint
	LeaseToken string `json:"-"`
}

func (s *DataDeletionRequestService) ClaimExecutionStage(
	ctx context.Context,
	requestID primitive.ObjectID,
	expectedRequestRevision int64,
	expectedCheckpointRevision int64,
	now time.Time,
) (*DataDeletionStageClaim, error) {
	if s == nil || requestID.IsZero() || expectedRequestRevision < 1 || expectedCheckpointRevision < 1 || now.IsZero() {
		return nil, errors.New("data deletion execution claim is invalid")
	}
	request, err := s.repository.GetDataDeletionRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	protocol, err := executableDataDeletionProtocol(
		request, requestID, expectedRequestRevision, expectedCheckpointRevision, s.capabilityRegistry.Snapshot(),
	)
	if err != nil {
		return nil, err
	}
	token, err := newDataRetentionClaimToken()
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(s.leaseDuration)
	subjectState, err := s.repository.AcquireDataRetentionSubjectLease(ctx, request.UserID, token, now, leaseUntil)
	if err != nil {
		return nil, fmt.Errorf("acquire data deletion execution subject lease: %w", err)
	}
	switch subjectState {
	case repository.DataRetentionSubjectHeld:
		return nil, ErrDataDeletionExecutionLegalHold
	case repository.DataRetentionSubjectBusy:
		return nil, ErrDataDeletionExecutionSubjectBusy
	case repository.DataRetentionSubjectLeaseAcquired:
	default:
		return nil, ErrDataDeletionExecutionSubjectBusy
	}
	releaseOnFailure := true
	defer func() {
		if releaseOnFailure {
			_ = s.repository.ReleaseDataRetentionSubjectLease(ctx, request.UserID, token)
		}
	}()

	checkpoint, err := agentdeletion.ClaimNextStage(
		&protocol.Checkpoint, protocol.Plan, expectedCheckpointRevision, token, now, leaseUntil,
	)
	if err != nil {
		return nil, err
	}
	auditEvent, err := agentdeletion.NewAuditEvent(agentdeletion.AuditEventInput{
		RequestID: requestID.Hex(), Sequence: request.ExecutionAuditCount + 1, Type: agentdeletion.AuditStageClaimed,
		Stage: *checkpoint, RequestRevision: expectedRequestRevision + 1,
		CheckpointRevision: protocol.Checkpoint.Revision, PlanDigest: protocol.Plan.Digest,
		CapabilityDigest: protocol.Plan.CapabilityDigest, LeaseToken: token,
		PreviousDigest: request.ExecutionAuditTail, OccurredAt: now,
	})
	if err != nil {
		return nil, err
	}
	updated, swapped, err := s.repository.CompareAndSwapDataDeletionCheckpoint(
		ctx, requestID, expectedRequestRevision, expectedCheckpointRevision,
		request.ExecutionAuditCount, request.ExecutionAuditTail, protocol, auditEvent, now,
	)
	if err != nil {
		return nil, err
	}
	if !swapped {
		return nil, ErrDataDeletionExecutionConflict
	}
	definition, ok := dataDeletionStageDefinition(protocol.Plan, checkpoint.StageID)
	if !ok {
		return nil, errors.New("claimed data deletion stage is absent from plan")
	}
	releaseOnFailure = false
	return &DataDeletionStageClaim{
		Request: updated, Definition: definition, Checkpoint: *checkpoint, LeaseToken: token,
	}, nil
}

func (s *DataDeletionRequestService) CompleteExecutionStage(
	ctx context.Context,
	requestID primitive.ObjectID,
	expectedRequestRevision int64,
	expectedCheckpointRevision int64,
	leaseToken string,
	status agentdeletion.StageStatus,
	receiptDigest string,
	failureCode string,
	now time.Time,
) (*repository.DataDeletionRequest, error) {
	if s == nil || requestID.IsZero() || expectedRequestRevision < 1 || expectedCheckpointRevision < 1 || leaseToken == "" || now.IsZero() {
		return nil, errors.New("data deletion execution completion is invalid")
	}
	request, err := s.repository.GetDataDeletionRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	protocol, err := executableDataDeletionProtocol(
		request, requestID, expectedRequestRevision, expectedCheckpointRevision, s.capabilityRegistry.Snapshot(),
	)
	if err != nil {
		return nil, err
	}
	runningStage, ok := dataDeletionRunningStage(protocol.Checkpoint, leaseToken)
	if !ok {
		return nil, agentdeletion.ErrStageNotRunning
	}
	if err := agentdeletion.CompleteStage(
		&protocol.Checkpoint, protocol.Plan, expectedCheckpointRevision, leaseToken,
		status, receiptDigest, failureCode, now,
	); err != nil {
		return nil, err
	}
	completedStage, ok := dataDeletionStageCheckpoint(protocol.Checkpoint, runningStage.StageID)
	if !ok {
		return nil, errors.New("completed data deletion stage is absent from checkpoint")
	}
	eventType := agentdeletion.AuditStageSucceeded
	if status == agentdeletion.StageFailed {
		eventType = agentdeletion.AuditStageFailed
	}
	auditEvent, err := agentdeletion.NewAuditEvent(agentdeletion.AuditEventInput{
		RequestID: requestID.Hex(), Sequence: request.ExecutionAuditCount + 1, Type: eventType,
		Stage: completedStage, RequestRevision: expectedRequestRevision + 1,
		CheckpointRevision: protocol.Checkpoint.Revision, PlanDigest: protocol.Plan.Digest,
		CapabilityDigest: protocol.Plan.CapabilityDigest, LeaseToken: leaseToken,
		PreviousDigest: request.ExecutionAuditTail, OccurredAt: now,
	})
	if err != nil {
		return nil, err
	}
	updated, swapped, err := s.repository.CompareAndSwapDataDeletionCheckpoint(
		ctx, requestID, expectedRequestRevision, expectedCheckpointRevision,
		request.ExecutionAuditCount, request.ExecutionAuditTail, protocol, auditEvent, now,
	)
	if err != nil {
		return nil, err
	}
	if !swapped {
		_ = s.repository.ReleaseDataRetentionSubjectLease(ctx, request.UserID, leaseToken)
		return nil, ErrDataDeletionExecutionConflict
	}
	if err := s.repository.ReleaseDataRetentionSubjectLease(ctx, request.UserID, leaseToken); err != nil {
		return updated, fmt.Errorf("release data deletion execution subject lease: %w", err)
	}
	return updated, nil
}

func executableDataDeletionProtocol(
	request *repository.DataDeletionRequest,
	expectedRequestID primitive.ObjectID,
	expectedRequestRevision int64,
	expectedCheckpointRevision int64,
	capabilities agentdeletion.CapabilitySnapshot,
) (agentdeletion.Protocol, error) {
	if request == nil || request.ID != expectedRequestID || request.Revision != expectedRequestRevision || request.ExecutionProtocol == nil ||
		request.ExecutionProtocol.Checkpoint.Revision != expectedCheckpointRevision {
		return agentdeletion.Protocol{}, ErrDataDeletionExecutionConflict
	}
	if !request.Active || request.Status != repository.DataDeletionPreviewReady || !request.ExecutionSupported ||
		!request.ExecutionProtocol.Plan.Ready {
		return agentdeletion.Protocol{}, ErrDataDeletionExecutionUnavailable
	}
	protocol := *request.ExecutionProtocol
	protocol.Plan.Stages = append([]agentdeletion.Stage(nil), request.ExecutionProtocol.Plan.Stages...)
	protocol.Plan.Blockers = append([]string(nil), request.ExecutionProtocol.Plan.Blockers...)
	protocol.Checkpoint.Stages = append([]agentdeletion.StageCheckpoint(nil), request.ExecutionProtocol.Checkpoint.Stages...)
	if err := agentdeletion.ValidateProtocol(protocol); err != nil {
		return agentdeletion.Protocol{}, fmt.Errorf("validate data deletion execution protocol: %w", err)
	}
	if err := agentdeletion.ValidatePlanCapabilities(protocol.Plan, capabilities); err != nil {
		return agentdeletion.Protocol{}, fmt.Errorf("%w: %v", ErrDataDeletionExecutionUnavailable, err)
	}
	if err := agentdeletion.ValidateAuditTrail(
		request.ExecutionAudit, request.ExecutionAuditCount, request.ExecutionAuditTail,
		protocol.Plan.Digest, protocol.Plan.CapabilityDigest,
	); err != nil {
		return agentdeletion.Protocol{}, fmt.Errorf("validate data deletion execution audit: %w", err)
	}
	if err := agentdeletion.ValidateAuditProtocolBinding(request.ExecutionAudit, request.Revision, protocol); err != nil {
		return agentdeletion.Protocol{}, fmt.Errorf("validate data deletion audit checkpoint binding: %w", err)
	}
	return protocol, nil
}

func dataDeletionStageDefinition(plan agentdeletion.Plan, stageID string) (agentdeletion.Stage, bool) {
	for _, stage := range plan.Stages {
		if stage.ID == stageID {
			return stage, true
		}
	}
	return agentdeletion.Stage{}, false
}

func dataDeletionRunningStage(checkpoint agentdeletion.Checkpoint, leaseToken string) (agentdeletion.StageCheckpoint, bool) {
	for _, stage := range checkpoint.Stages {
		if stage.StageID == checkpoint.CurrentStageID && stage.Status == agentdeletion.StageRunning && stage.LeaseToken == leaseToken {
			return stage, true
		}
	}
	return agentdeletion.StageCheckpoint{}, false
}

func dataDeletionStageCheckpoint(checkpoint agentdeletion.Checkpoint, stageID string) (agentdeletion.StageCheckpoint, bool) {
	for _, stage := range checkpoint.Stages {
		if stage.StageID == stageID {
			return stage, true
		}
	}
	return agentdeletion.StageCheckpoint{}, false
}
