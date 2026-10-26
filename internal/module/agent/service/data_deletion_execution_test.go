package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
	"twitter-clone/internal/module/agent/repository"
)

type dataDeletionCapabilityProvider struct {
	descriptor agentdeletion.CapabilityDescriptor
}

func (provider dataDeletionCapabilityProvider) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return provider.descriptor
}

func dataDeletionExecutionRegistry(t *testing.T) *agentdeletion.CapabilityRegistry {
	t.Helper()
	ids := []string{
		agentdeletion.CapabilitySubjectWriteFence, agentdeletion.CapabilityRelationshipMutation,
		agentdeletion.CapabilityMinIOSubjectDelete, agentdeletion.CapabilityQdrantSubjectDelete,
		agentdeletion.CapabilityTweetServiceDelete, agentdeletion.CapabilityMongoOwnedDelete,
		agentdeletion.CapabilityCrossStoreVerify, agentdeletion.CapabilityRequestFinalize,
	}
	providers := make([]agentdeletion.CapabilityProvider, 0, len(ids))
	for _, id := range ids {
		providers = append(providers, dataDeletionCapabilityProvider{agentdeletion.CapabilityDescriptor{
			ID: id, Adapter: "test/" + id, Version: "v1",
		}})
	}
	registry, err := agentdeletion.NewCapabilityRegistry(providers...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func newDataDeletionExecutionFixture(t *testing.T) (*DataDeletionRequestService, *dataDeletionRequestRepositoryFake, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 25, 15, 0, 0, 0, time.UTC)
	registry := dataDeletionExecutionRegistry(t)
	protocol, err := agentdeletion.BuildProtocol(agentdeletion.Inventory{
		Owned: map[string]int64{"dialogues": 2}, Related: map[string]int64{"project_memberships": 1},
		Retained:    map[string]int64{"product_events": 1},
		Unsupported: []string{"minio_tool_result_objects", "qdrant_episodic_memory", "tweet_service_published_content"},
	}, registry.Snapshot(), now)
	if err != nil {
		t.Fatal(err)
	}
	repo := &dataDeletionRequestRepositoryFake{
		request: &repository.DataDeletionRequest{
			ID: primitive.NewObjectID(), UserID: 42, Status: repository.DataDeletionPreviewReady,
			Active: true, Revision: 7, ExecutionSupported: true, ExecutionProtocol: protocol,
		},
		subjectState:   repository.DataRetentionSubjectLeaseAcquired,
		checkpointSwap: true,
	}
	service, err := NewDataDeletionRequestService(repo, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service.capabilityRegistry = registry
	return service, repo, now
}

func TestDataDeletionExecutionClaimAndCompleteOnlyMutateControlState(t *testing.T) {
	service, repo, now := newDataDeletionExecutionFixture(t)
	claim, err := service.ClaimExecutionStage(context.Background(), repo.request.ID, 7, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Definition.ID != agentdeletion.StageEnforceWriteFence || claim.Checkpoint.Status != agentdeletion.StageRunning ||
		claim.LeaseToken == "" || claim.Request.Revision != 8 || claim.Request.ExecutionProtocol.Checkpoint.Revision != 2 {
		t.Fatalf("unexpected execution claim: %+v", claim)
	}
	if repo.released {
		t.Fatal("subject lease was released before stage completion")
	}
	receipt := sha256.Sum256([]byte("write-fence-receipt"))
	completed, err := service.CompleteExecutionStage(
		context.Background(), repo.request.ID, 8, 2, claim.LeaseToken,
		agentdeletion.StageSucceeded, hex.EncodeToString(receipt[:]), "", now.Add(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Revision != 9 || completed.ExecutionProtocol.Checkpoint.Revision != 3 ||
		completed.ExecutionProtocol.Checkpoint.Stages[0].Status != agentdeletion.StageSucceeded || !repo.released || repo.checkpointCAS != 2 {
		t.Fatalf("unexpected completion state: request=%+v released=%v cas=%d", completed, repo.released, repo.checkpointCAS)
	}
	if completed.ExecutionAuditCount != 2 || len(completed.ExecutionAudit) != 2 ||
		completed.ExecutionAudit[0].Type != agentdeletion.AuditStageClaimed ||
		completed.ExecutionAudit[1].Type != agentdeletion.AuditStageSucceeded ||
		completed.ExecutionAuditTail != completed.ExecutionAudit[1].EventDigest {
		t.Fatalf("unexpected execution audit trail: %+v", completed.ExecutionAudit)
	}
	if err := agentdeletion.ValidateAuditTrail(
		completed.ExecutionAudit, completed.ExecutionAuditCount, completed.ExecutionAuditTail,
		completed.ExecutionProtocol.Plan.Digest, completed.ExecutionProtocol.Plan.CapabilityDigest,
	); err != nil {
		t.Fatal(err)
	}
	if err := agentdeletion.ValidateAuditProtocolBinding(completed.ExecutionAudit, completed.Revision, *completed.ExecutionProtocol); err != nil {
		t.Fatal(err)
	}
}

func TestDataDeletionExecutionRemainsDisabledForPreflightProtocol(t *testing.T) {
	service, repo, now := newDataDeletionServiceTestFixture(t)
	protocol, err := agentdeletion.BuildProtocol(agentdeletion.Inventory{}, agentdeletion.EmptyCapabilityRegistry().Snapshot(), now)
	if err != nil {
		t.Fatal(err)
	}
	repo.request.Status = repository.DataDeletionPreviewReady
	repo.request.Revision = 3
	repo.request.ExecutionProtocol = protocol
	repo.request.ExecutionSupported = false
	if _, err := service.ClaimExecutionStage(context.Background(), repo.request.ID, 3, 1, now); !errors.Is(err, ErrDataDeletionExecutionUnavailable) {
		t.Fatalf("ClaimExecutionStage() error = %v, want unavailable", err)
	}
	if repo.subjectClaims != 0 || repo.checkpointCAS != 0 {
		t.Fatalf("disabled execution touched control state: subject=%d cas=%d", repo.subjectClaims, repo.checkpointCAS)
	}
}

func TestDataDeletionExecutionRejectsCapabilityRegistryDriftBeforeClaim(t *testing.T) {
	service, repo, now := newDataDeletionExecutionFixture(t)
	service.capabilityRegistry = agentdeletion.EmptyCapabilityRegistry()
	if _, err := service.ClaimExecutionStage(context.Background(), repo.request.ID, 7, 1, now); !errors.Is(err, ErrDataDeletionExecutionUnavailable) {
		t.Fatalf("ClaimExecutionStage() error = %v, want unavailable", err)
	}
	if repo.subjectClaims != 0 || repo.checkpointCAS != 0 {
		t.Fatalf("capability drift touched control state: subject=%d cas=%d", repo.subjectClaims, repo.checkpointCAS)
	}
}

func TestDataDeletionExecutionRejectsTamperedAuditBeforeCheckpointMutation(t *testing.T) {
	service, repo, now := newDataDeletionExecutionFixture(t)
	claim, err := service.ClaimExecutionStage(context.Background(), repo.request.ID, 7, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	repo.request.ExecutionAudit[0].StageID = agentdeletion.StageDeleteMongo
	if _, err := service.CompleteExecutionStage(
		context.Background(), repo.request.ID, 8, 2, claim.LeaseToken,
		agentdeletion.StageSucceeded, hex.EncodeToString(make([]byte, sha256.Size)), "", now.Add(time.Second),
	); err == nil {
		t.Fatal("tampered execution audit was accepted")
	}
	if repo.checkpointCAS != 1 {
		t.Fatalf("tampered audit advanced checkpoint: cas=%d", repo.checkpointCAS)
	}
}

func TestDataDeletionExecutionLegalHoldBlocksCheckpointClaim(t *testing.T) {
	service, repo, now := newDataDeletionExecutionFixture(t)
	repo.subjectState = repository.DataRetentionSubjectHeld
	if _, err := service.ClaimExecutionStage(context.Background(), repo.request.ID, 7, 1, now); !errors.Is(err, ErrDataDeletionExecutionLegalHold) {
		t.Fatalf("ClaimExecutionStage() error = %v, want legal hold", err)
	}
	if repo.checkpointCAS != 0 || repo.released {
		t.Fatalf("legal hold changed checkpoint: cas=%d released=%v", repo.checkpointCAS, repo.released)
	}
}

func TestDataDeletionExecutionCASConflictReleasesSubjectLease(t *testing.T) {
	service, repo, now := newDataDeletionExecutionFixture(t)
	repo.checkpointSwap = false
	if _, err := service.ClaimExecutionStage(context.Background(), repo.request.ID, 7, 1, now); !errors.Is(err, ErrDataDeletionExecutionConflict) {
		t.Fatalf("ClaimExecutionStage() error = %v, want conflict", err)
	}
	if !repo.released || repo.checkpointCAS != 1 {
		t.Fatalf("conflict did not release subject lease: released=%v cas=%d", repo.released, repo.checkpointCAS)
	}
}
