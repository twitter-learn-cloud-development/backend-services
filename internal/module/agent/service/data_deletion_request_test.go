package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
	"twitter-clone/internal/module/agent/repository"
)

type dataDeletionRequestRepositoryFake struct {
	request        *repository.DataDeletionRequest
	claimed        bool
	claimErr       error
	subjectState   repository.DataRetentionSubjectLeaseState
	subjectErr     error
	inventory      repository.DataDeletionInventory
	inventoryErr   error
	completedState repository.DataDeletionRequestStatus
	failureCode    string
	released       bool
	protocol       *agentdeletion.Protocol
	checkpointSwap bool
	checkpointCAS  int
	subjectClaims  int
}

func (f *dataDeletionRequestRepositoryFake) CreateDataDeletionRequest(_ context.Context, userID uint64, reason, actor string, now time.Time) (*repository.DataDeletionRequest, error) {
	f.request = &repository.DataDeletionRequest{
		ID: primitive.NewObjectID(), UserID: userID, Status: repository.DataDeletionRequested,
		ReasonCode: reason, ActorDigest: repository.DataDeletionActorDigest(actor), RequestedAt: now, Active: true,
	}
	return f.request, nil
}

func (f *dataDeletionRequestRepositoryFake) GetDataDeletionRequest(context.Context, primitive.ObjectID) (*repository.DataDeletionRequest, error) {
	return f.request, nil
}

func (f *dataDeletionRequestRepositoryFake) ClaimDataDeletionPreflight(context.Context, primitive.ObjectID, string, time.Time, time.Time) (*repository.DataDeletionRequest, bool, error) {
	return f.request, f.claimed, f.claimErr
}

func (f *dataDeletionRequestRepositoryFake) CompleteDataDeletionPreflight(
	_ context.Context,
	_ primitive.ObjectID,
	_ string,
	status repository.DataDeletionRequestStatus,
	inventory repository.DataDeletionInventory,
	protocol *agentdeletion.Protocol,
	unsupported []string,
	failureCode string,
	now time.Time,
) (*repository.DataDeletionRequest, error) {
	f.completedState = status
	f.failureCode = failureCode
	f.protocol = protocol
	f.request.Status = status
	f.request.Inventory = inventory.Owned
	f.request.RelatedInventory = inventory.Related
	f.request.RetainedInventory = inventory.Retained
	f.request.ExecutionProtocol = protocol
	f.request.Unsupported = unsupported
	f.request.PreflightedAt = now
	f.request.ExecutionSupported = false
	return f.request, nil
}

func (f *dataDeletionRequestRepositoryFake) CompareAndSwapDataDeletionCheckpoint(
	_ context.Context,
	_ primitive.ObjectID,
	expectedRequestRevision int64,
	expectedCheckpointRevision int64,
	expectedAuditCount int64,
	expectedAuditTail string,
	protocol agentdeletion.Protocol,
	auditEvent agentdeletion.AuditEvent,
	now time.Time,
) (*repository.DataDeletionRequest, bool, error) {
	f.checkpointCAS++
	if !f.checkpointSwap || f.request.Revision != expectedRequestRevision ||
		f.request.ExecutionProtocol == nil || f.request.ExecutionProtocol.Checkpoint.Revision != expectedCheckpointRevision ||
		f.request.ExecutionAuditCount != expectedAuditCount || f.request.ExecutionAuditTail != expectedAuditTail {
		return nil, false, nil
	}
	if err := agentdeletion.ValidateAuditEvent(auditEvent); err != nil || !auditEvent.OccurredAt.Equal(now.UTC()) {
		return nil, false, err
	}
	f.request.ExecutionProtocol = &protocol
	f.request.ExecutionAudit = append(f.request.ExecutionAudit, auditEvent)
	f.request.ExecutionAuditCount++
	f.request.ExecutionAuditTail = auditEvent.EventDigest
	f.request.Revision++
	if auditEvent.Type == agentdeletion.AuditStageSucceeded && auditEvent.StageID == agentdeletion.StageFinalize {
		completed := protocol.Checkpoint.CurrentStageID == ""
		for _, stage := range protocol.Checkpoint.Stages {
			completed = completed && stage.Status == agentdeletion.StageSucceeded
		}
		if completed {
			f.request.Status = repository.DataDeletionCompleted
			f.request.Active = false
			f.request.ExecutionSupported = false
			f.request.CompletedAt = now.UTC()
		}
	}
	return f.request, true, nil
}

func (f *dataDeletionRequestRepositoryFake) CancelDataDeletionRequest(context.Context, primitive.ObjectID, time.Time) (*repository.DataDeletionRequest, error) {
	f.request.Status = repository.DataDeletionCancelled
	f.request.Active = false
	return f.request, nil
}

func (f *dataDeletionRequestRepositoryFake) CountAgentDataDeletionInventory(context.Context, uint64) (repository.DataDeletionInventory, error) {
	return f.inventory, f.inventoryErr
}

func (f *dataDeletionRequestRepositoryFake) AcquireDataRetentionSubjectLease(context.Context, uint64, string, time.Time, time.Time) (repository.DataRetentionSubjectLeaseState, error) {
	f.subjectClaims++
	return f.subjectState, f.subjectErr
}

func (f *dataDeletionRequestRepositoryFake) ReleaseDataRetentionSubjectLease(context.Context, uint64, string) error {
	f.released = true
	return nil
}

func newDataDeletionServiceTestFixture(t *testing.T) (*DataDeletionRequestService, *dataDeletionRequestRepositoryFake, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	repo := &dataDeletionRequestRepositoryFake{
		request: &repository.DataDeletionRequest{
			ID: primitive.NewObjectID(), UserID: 42, Status: repository.DataDeletionRequested, Active: true,
		},
		claimed: true, subjectState: repository.DataRetentionSubjectLeaseAcquired,
		inventory: repository.DataDeletionInventory{
			Owned:    map[string]int64{"dialogues": 2, "tool_executions": 3},
			Related:  map[string]int64{"project_memberships": 1},
			Retained: map[string]int64{"product_events": 4},
		},
	}
	service, err := NewDataDeletionRequestService(repo, DefaultDataDeletionPreflightLease)
	if err != nil {
		t.Fatal(err)
	}
	return service, repo, now
}

func TestDataDeletionPreflightBuildsReadOnlyPreview(t *testing.T) {
	service, repo, now := newDataDeletionServiceTestFixture(t)
	request, err := service.Preflight(context.Background(), repo.request.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != repository.DataDeletionPreviewReady || request.ExecutionSupported {
		t.Fatalf("unexpected request state: %+v", request)
	}
	if !repo.released || !reflect.DeepEqual(request.Inventory, repo.inventory.Owned) ||
		!reflect.DeepEqual(request.RelatedInventory, repo.inventory.Related) ||
		!reflect.DeepEqual(request.RetainedInventory, repo.inventory.Retained) || len(request.Unsupported) == 0 ||
		request.ExecutionProtocol == nil || request.ExecutionProtocol.Plan.Ready || request.ExecutionProtocol.Plan.Digest == "" {
		t.Fatalf("preview did not preserve inventory boundaries: %+v", request)
	}
}

func TestDataDeletionPreflightStopsAtLegalHold(t *testing.T) {
	service, repo, now := newDataDeletionServiceTestFixture(t)
	repo.subjectState = repository.DataRetentionSubjectHeld
	request, err := service.Preflight(context.Background(), repo.request.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != repository.DataDeletionBlockedLegalHold || repo.failureCode != "legal_hold" || repo.released {
		t.Fatalf("unexpected legal hold result: request=%+v released=%v", request, repo.released)
	}
}

func TestDataDeletionPreflightFailsClosedWhenInventoryIsUnavailable(t *testing.T) {
	service, repo, now := newDataDeletionServiceTestFixture(t)
	repo.inventoryErr = errors.New("mongo unavailable")
	request, err := service.Preflight(context.Background(), repo.request.ID, now)
	if err == nil || request.Status != repository.DataDeletionPreflightFailed || repo.failureCode != "inventory_unavailable" {
		t.Fatalf("unexpected failed preflight result: request=%+v err=%v", request, err)
	}
	if !repo.released {
		t.Fatal("subject lease was not released after inventory failure")
	}
}

func TestDataDeletionPreflightRejectsConcurrentClaim(t *testing.T) {
	service, repo, now := newDataDeletionServiceTestFixture(t)
	repo.claimed = false
	if _, err := service.Preflight(context.Background(), repo.request.ID, now); !errors.Is(err, ErrDataDeletionPreflightBusy) {
		t.Fatalf("Preflight() error = %v, want busy", err)
	}
}

func TestDataDeletionRequestStoresActorDigest(t *testing.T) {
	service, _, now := newDataDeletionServiceTestFixture(t)
	request, err := service.Request(context.Background(), 42, "user_request", " Privacy@Example.com ", now)
	if err != nil {
		t.Fatal(err)
	}
	if request.ActorDigest == "" || request.ActorDigest == " Privacy@Example.com " ||
		request.ActorDigest != repository.DataDeletionActorDigest("privacy@example.com") {
		t.Fatalf("actor identity was not normalized and digested: %q", request.ActorDigest)
	}
}
