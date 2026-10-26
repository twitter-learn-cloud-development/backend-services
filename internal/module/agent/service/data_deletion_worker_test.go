package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
	"twitter-clone/internal/module/agent/repository"
)

type dataDeletionWorkerClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *dataDeletionWorkerClock) Next() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(time.Second)
	return clock.now
}

type dataDeletionWorkerGovernance struct{}

func (dataDeletionWorkerGovernance) IssueGovernanceDecision(
	_ context.Context,
	userID uint64,
	now time.Time,
	validUntil time.Time,
) (agentdeletion.GovernanceDecision, error) {
	digest := sha256.Sum256([]byte("worker-governance"))
	return agentdeletion.NewGovernanceDecision(
		userID, agentdeletion.GovernanceClear, 1, hex.EncodeToString(digest[:]), now, validUntil,
	)
}

type dataDeletionWorkerFence struct {
	calls int
}

func (fence *dataDeletionWorkerFence) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return agentdeletion.CapabilityDescriptor{
		ID: agentdeletion.CapabilitySubjectWriteFence, Adapter: "test.subject-write-fence", Version: "v1",
	}
}

func (fence *dataDeletionWorkerFence) EnforceDataDeletionFence(
	_ context.Context,
	request agentdeletion.WriteFenceRequest,
) (agentdeletion.WriteFenceReceipt, error) {
	if err := agentdeletion.ValidateWriteFenceRequest(request, request.Governance.ObservedAt); err != nil {
		return agentdeletion.WriteFenceReceipt{}, err
	}
	fence.calls++
	return agentdeletion.NewWriteFenceReceipt(request, int64(fence.calls), request.Governance.ObservedAt.Add(time.Second))
}

type dataDeletionWorkerAdapter struct {
	descriptor              agentdeletion.CapabilityDescriptor
	scope                   string
	clock                   *dataDeletionWorkerClock
	remaining               int64
	deleteCalls             int
	observeCalls            int
	failDeletes             int
	residualFromObservation int
}

func (adapter *dataDeletionWorkerAdapter) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return adapter.descriptor
}

func (adapter *dataDeletionWorkerAdapter) Observe(
	_ context.Context,
	request agentdeletion.ObservationRequest,
) (agentdeletion.PostState, error) {
	if err := agentdeletion.ValidateObservationRequest(request); err != nil {
		return agentdeletion.PostState{}, err
	}
	adapter.observeCalls++
	remaining := adapter.remaining
	if adapter.residualFromObservation > 0 && adapter.observeCalls >= adapter.residualFromObservation {
		remaining = 1
	}
	return agentdeletion.NewPostState(
		adapter.descriptor, request.SubjectDigest, adapter.scope, remaining, adapter.clock.Next(),
	)
}

func (adapter *dataDeletionWorkerAdapter) Delete(
	_ context.Context,
	request agentdeletion.AdapterRequest,
) (agentdeletion.AdapterResult, error) {
	if err := agentdeletion.ValidateAdapterBinding(adapter, request); err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	adapter.deleteCalls++
	if adapter.failDeletes > 0 {
		adapter.failDeletes--
		return agentdeletion.AdapterResult{}, errors.New("injected adapter failure")
	}
	before, err := agentdeletion.NewPostState(
		adapter.descriptor, request.SubjectDigest, adapter.scope, adapter.remaining, adapter.clock.Next(),
	)
	if err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	adapter.remaining = 0
	after, err := agentdeletion.NewPostState(
		adapter.descriptor, request.SubjectDigest, adapter.scope, 0, adapter.clock.Next(),
	)
	if err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	receipt, err := agentdeletion.NewExecutionReceipt(request, before, after, adapter.clock.Next())
	if err != nil {
		return agentdeletion.AdapterResult{}, err
	}
	return agentdeletion.AdapterResult{
		Receipt: receipt, AuthorizationDigest: request.Governance.Digest,
	}, nil
}

func TestDataDeletionExecutionWorkerCompletesEightStageProtocol(t *testing.T) {
	worker, repo, fence, adapters := newDataDeletionWorkerFixture(t)
	completed, err := worker.RunRequest(context.Background(), repo.request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != repository.DataDeletionCompleted || completed.Active || completed.ExecutionSupported ||
		completed.CompletedAt.IsZero() || completed.ExecutionAuditCount != 16 || repo.checkpointCAS != 16 {
		t.Fatalf("unexpected completed request: %+v", completed)
	}
	if fence.calls != 1 {
		t.Fatalf("write fence calls = %d, want 1", fence.calls)
	}
	for capability, adapter := range adapters {
		if adapter.deleteCalls != 1 || adapter.observeCalls != 2 {
			t.Fatalf("%s calls: delete=%d observe=%d", capability, adapter.deleteCalls, adapter.observeCalls)
		}
	}
	if final := completed.ExecutionProtocol.Checkpoint.Stages[len(completed.ExecutionProtocol.Checkpoint.Stages)-1]; final.StageID != agentdeletion.StageFinalize || final.Status != agentdeletion.StageSucceeded {
		t.Fatalf("final stage was not committed: %+v", final)
	}
}

func TestDataDeletionExecutionWorkerRetriesOnlyFailedStage(t *testing.T) {
	worker, repo, fence, adapters := newDataDeletionWorkerFixture(t)
	failedAdapter := adapters[agentdeletion.CapabilityMinIOSubjectDelete]
	failedAdapter.failDeletes = 1
	failed, err := worker.RunRequest(context.Background(), repo.request.ID)
	if err == nil || failed == nil {
		t.Fatalf("first run = (%+v, %v), want persisted failure", failed, err)
	}
	checkpoint := failed.ExecutionProtocol.Checkpoint
	if checkpoint.Stages[2].Status != agentdeletion.StageFailed ||
		checkpoint.Stages[2].FailureCode != "minio_delete_failed" {
		t.Fatalf("failed stage was not persisted: %+v", checkpoint.Stages[2])
	}
	completed, err := worker.RunRequest(context.Background(), repo.request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != repository.DataDeletionCompleted || fence.calls != 1 ||
		failedAdapter.deleteCalls != 2 || checkpoint.Stages[0].Attempts != 1 || checkpoint.Stages[1].Attempts != 1 {
		t.Fatalf("retry replayed completed stages: completed=%+v fence=%d deletes=%d", completed, fence.calls, failedAdapter.deleteCalls)
	}
}

func TestDataDeletionExecutionWorkerFailsCrossStoreVerificationOnRegressedState(t *testing.T) {
	worker, repo, _, adapters := newDataDeletionWorkerFixture(t)
	adapters[agentdeletion.CapabilityQdrantSubjectDelete].residualFromObservation = 2
	failed, err := worker.RunRequest(context.Background(), repo.request.ID)
	if !errors.Is(err, agentdeletion.ErrDeletionResidualsRemain) || failed == nil {
		t.Fatalf("RunRequest() = (%+v, %v), want residual failure", failed, err)
	}
	verify := failed.ExecutionProtocol.Checkpoint.Stages[6]
	if verify.StageID != agentdeletion.StageVerify || verify.Status != agentdeletion.StageFailed ||
		verify.FailureCode != "cross_store_verification_failed" || failed.Status != repository.DataDeletionPreviewReady {
		t.Fatalf("unexpected verification failure state: %+v request=%+v", verify, failed)
	}
}

func TestDataDeletionExecutionWorkerRequiresCompleteAdapterSet(t *testing.T) {
	service, _, _ := newDataDeletionServiceTestFixture(t)
	clock := &dataDeletionWorkerClock{now: time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)}
	if _, err := NewDataDeletionExecutionWorker(
		service, dataDeletionWorkerGovernance{}, &dataDeletionWorkerFence{}, nil, clock.Next,
	); err == nil {
		t.Fatal("worker accepted an incomplete adapter set")
	}
}

func newDataDeletionWorkerFixture(
	t *testing.T,
) (*DataDeletionExecutionWorker, *dataDeletionRequestRepositoryFake, *dataDeletionWorkerFence, map[string]*dataDeletionWorkerAdapter) {
	t.Helper()
	base := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	clock := &dataDeletionWorkerClock{now: base}
	repo := &dataDeletionRequestRepositoryFake{
		request: &repository.DataDeletionRequest{
			ID: primitive.NewObjectID(), UserID: 42, Status: repository.DataDeletionPreviewReady,
			Active: true, Revision: 7, ExecutionSupported: true,
		},
		subjectState: repository.DataRetentionSubjectLeaseAcquired, checkpointSwap: true,
	}
	service, err := NewDataDeletionRequestService(repo, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fence := &dataDeletionWorkerFence{}
	adapters := make(map[string]*dataDeletionWorkerAdapter, len(destructiveDeletionStages))
	bindings := make([]DataDeletionAdapterBinding, 0, len(destructiveDeletionStages))
	for _, definition := range destructiveDeletionStages {
		adapter := &dataDeletionWorkerAdapter{
			descriptor: agentdeletion.CapabilityDescriptor{
				ID: definition.capability, Adapter: "test." + definition.capability, Version: "v1",
			},
			scope: agentdeletion.DeletionScopeDigest(definition.capability), clock: clock, remaining: 2,
		}
		adapters[definition.capability] = adapter
		bindings = append(bindings, DataDeletionAdapterBinding{Adapter: adapter, ScopeDigest: adapter.scope})
	}
	worker, err := NewDataDeletionExecutionWorker(
		service, dataDeletionWorkerGovernance{}, fence, bindings, clock.Next,
	)
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := agentdeletion.BuildProtocol(agentdeletion.Inventory{
		Owned: map[string]int64{"dialogues": 2}, Related: map[string]int64{"project_memberships": 1},
		Retained: map[string]int64{"product_events": 1}, Unsupported: []string{
			"minio_tool_result_objects", "qdrant_episodic_memory", "tweet_service_published_content",
		},
	}, worker.CapabilitySnapshot(), base)
	if err != nil {
		t.Fatal(err)
	}
	if !protocol.Plan.Ready {
		t.Fatalf("worker protocol is unexpectedly blocked: %v", protocol.Plan.Blockers)
	}
	repo.request.ExecutionProtocol = protocol
	return worker, repo, fence, adapters
}
