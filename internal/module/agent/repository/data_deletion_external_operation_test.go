package repository

import (
	"strings"
	"testing"
	"time"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

func TestExternalDeletionOperationBindsRequestBeforeStateAndReceipt(t *testing.T) {
	now := time.Date(2026, 9, 25, 19, 0, 0, 0, time.UTC)
	descriptor := agentdeletion.CapabilityDescriptor{
		ID: agentdeletion.CapabilityQdrantSubjectDelete, Adapter: "qdrant/episodic", Version: "v1",
	}
	decision, err := agentdeletion.NewGovernanceDecision(
		42, agentdeletion.GovernanceClear, 3, strings.Repeat("a", 64), now, now.Add(5*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := agentdeletion.NewAdapterRequest(agentdeletion.AdapterRequestInput{
		RequestID: "507f1f77bcf86cd799439011", StageID: agentdeletion.StageDeleteQdrant, UserID: 42,
		PlanDigest: strings.Repeat("b", 64), CapabilityDigest: strings.Repeat("c", 64),
		Capability: descriptor, ScopeDigest: agentdeletion.DeletionScopeDigest("qdrant", "user_id"),
		Governance: decision, Now: now, Deadline: now.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := agentdeletion.NewPostState(descriptor, request.SubjectDigest, request.ScopeDigest, 4, now)
	if err != nil {
		t.Fatal(err)
	}
	operation := newExternalDeletionOperation(request, before, "lease-1", now.Add(time.Minute), now)
	if err := validateExternalDeletionOperation(operation, request); err != nil {
		t.Fatal(err)
	}

	after, err := agentdeletion.NewPostState(descriptor, request.SubjectDigest, request.ScopeDigest, 0, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := agentdeletion.NewExecutionReceipt(request, before, after, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	operation.Status = externalDeletionStatusCompleted
	operation.LeaseToken = ""
	operation.LeaseUntil = time.Time{}
	operation.Revision++
	operation.Receipt = &receipt
	operation.CompletedAt = receipt.CompletedAt
	operation.UpdatedAt = receipt.CompletedAt
	operation.RecordDigest = externalDeletionOperationDigest(operation)
	if err := validateExternalDeletionOperation(operation, request); err != nil {
		t.Fatal(err)
	}

	operation.BeforeState.ResidualCount++
	if err := validateExternalDeletionOperation(operation, request); err == nil {
		t.Fatal("tampered persisted before-state was accepted")
	}
}
