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

const DefaultDataDeletionPreflightLease = 2 * time.Minute

var ErrDataDeletionPreflightBusy = errors.New("data deletion request is not available for preflight")

var agentDataDeletionUnsupportedResources = []string{
	"qdrant_episodic_memory",
	"minio_tool_result_objects",
	"tweet_service_published_content",
}

type DataDeletionRequestRepository interface {
	CreateDataDeletionRequest(context.Context, uint64, string, string, time.Time) (*repository.DataDeletionRequest, error)
	GetDataDeletionRequest(context.Context, primitive.ObjectID) (*repository.DataDeletionRequest, error)
	ClaimDataDeletionPreflight(context.Context, primitive.ObjectID, string, time.Time, time.Time) (*repository.DataDeletionRequest, bool, error)
	CompleteDataDeletionPreflight(context.Context, primitive.ObjectID, string, repository.DataDeletionRequestStatus, repository.DataDeletionInventory, *agentdeletion.Protocol, []string, string, time.Time) (*repository.DataDeletionRequest, error)
	CompareAndSwapDataDeletionCheckpoint(context.Context, primitive.ObjectID, int64, int64, int64, string, agentdeletion.Protocol, agentdeletion.AuditEvent, time.Time) (*repository.DataDeletionRequest, bool, error)
	CancelDataDeletionRequest(context.Context, primitive.ObjectID, time.Time) (*repository.DataDeletionRequest, error)
	CountAgentDataDeletionInventory(context.Context, uint64) (repository.DataDeletionInventory, error)
	AcquireDataRetentionSubjectLease(context.Context, uint64, string, time.Time, time.Time) (repository.DataRetentionSubjectLeaseState, error)
	ReleaseDataRetentionSubjectLease(context.Context, uint64, string) error
}

type DataDeletionRequestService struct {
	repository         DataDeletionRequestRepository
	leaseDuration      time.Duration
	capabilityRegistry *agentdeletion.CapabilityRegistry
}

func NewDataDeletionRequestService(repo DataDeletionRequestRepository, leaseDuration time.Duration) (*DataDeletionRequestService, error) {
	if repo == nil {
		return nil, errors.New("data deletion request repository is required")
	}
	if leaseDuration <= 0 || leaseDuration > 15*time.Minute {
		return nil, errors.New("data deletion preflight lease must be positive and at most 15m")
	}
	return &DataDeletionRequestService{
		repository: repo, leaseDuration: leaseDuration,
		capabilityRegistry: agentdeletion.EmptyCapabilityRegistry(),
	}, nil
}

func (s *DataDeletionRequestService) Request(ctx context.Context, userID uint64, reasonCode, actor string, now time.Time) (*repository.DataDeletionRequest, error) {
	if s == nil {
		return nil, errors.New("data deletion request service is unavailable")
	}
	return s.repository.CreateDataDeletionRequest(ctx, userID, reasonCode, actor, now)
}

func (s *DataDeletionRequestService) Get(ctx context.Context, requestID primitive.ObjectID) (*repository.DataDeletionRequest, error) {
	if s == nil {
		return nil, errors.New("data deletion request service is unavailable")
	}
	return s.repository.GetDataDeletionRequest(ctx, requestID)
}

func (s *DataDeletionRequestService) Cancel(ctx context.Context, requestID primitive.ObjectID, now time.Time) (*repository.DataDeletionRequest, error) {
	if s == nil {
		return nil, errors.New("data deletion request service is unavailable")
	}
	return s.repository.CancelDataDeletionRequest(ctx, requestID, now)
}

func (s *DataDeletionRequestService) Preflight(ctx context.Context, requestID primitive.ObjectID, now time.Time) (*repository.DataDeletionRequest, error) {
	if s == nil || now.IsZero() {
		return nil, errors.New("data deletion preflight service or time is invalid")
	}
	token, err := newDataRetentionClaimToken()
	if err != nil {
		return nil, err
	}
	leaseUntil := now.Add(s.leaseDuration)
	request, claimed, err := s.repository.ClaimDataDeletionPreflight(ctx, requestID, token, now, leaseUntil)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, ErrDataDeletionPreflightBusy
	}
	subjectState, err := s.repository.AcquireDataRetentionSubjectLease(ctx, request.UserID, token, now, leaseUntil)
	if err != nil {
		return s.failPreflight(ctx, requestID, token, now, "subject_lease_unavailable", err)
	}
	if subjectState == repository.DataRetentionSubjectHeld {
		return s.repository.CompleteDataDeletionPreflight(
			ctx, requestID, token, repository.DataDeletionBlockedLegalHold,
			emptyDataDeletionInventory(), nil, dataDeletionUnsupportedResources(), "legal_hold", now,
		)
	}
	if subjectState != repository.DataRetentionSubjectLeaseAcquired {
		return s.repository.CompleteDataDeletionPreflight(
			ctx, requestID, token, repository.DataDeletionBlockedSubjectBusy,
			emptyDataDeletionInventory(), nil, dataDeletionUnsupportedResources(), "subject_busy", now,
		)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.repository.ReleaseDataRetentionSubjectLease(releaseCtx, request.UserID, token)
	}()
	inventory, err := s.repository.CountAgentDataDeletionInventory(ctx, request.UserID)
	if err != nil {
		return s.failPreflight(ctx, requestID, token, now, "inventory_unavailable", err)
	}
	unsupported := dataDeletionUnsupportedResources()
	protocol, err := agentdeletion.BuildProtocol(agentdeletion.Inventory{
		Owned: inventory.Owned, Related: inventory.Related, Retained: inventory.Retained,
		Unsupported: unsupported,
	}, s.capabilityRegistry.Snapshot(), now)
	if err != nil {
		return s.failPreflight(ctx, requestID, token, now, "protocol_unavailable", err)
	}
	return s.repository.CompleteDataDeletionPreflight(
		ctx, requestID, token, repository.DataDeletionPreviewReady,
		inventory, protocol, unsupported, "", now,
	)
}

func (s *DataDeletionRequestService) failPreflight(
	ctx context.Context,
	requestID primitive.ObjectID,
	token string,
	now time.Time,
	code string,
	cause error,
) (*repository.DataDeletionRequest, error) {
	request, err := s.repository.CompleteDataDeletionPreflight(
		ctx, requestID, token, repository.DataDeletionPreflightFailed,
		emptyDataDeletionInventory(), nil, dataDeletionUnsupportedResources(), code, now,
	)
	if err != nil {
		return nil, errors.Join(cause, err)
	}
	return request, fmt.Errorf("data deletion preflight %s: %w", code, cause)
}

func dataDeletionUnsupportedResources() []string {
	return append([]string(nil), agentDataDeletionUnsupportedResources...)
}

func emptyDataDeletionInventory() repository.DataDeletionInventory {
	return repository.DataDeletionInventory{
		Owned: map[string]int64{}, Related: map[string]int64{}, Retained: map[string]int64{},
	}
}
