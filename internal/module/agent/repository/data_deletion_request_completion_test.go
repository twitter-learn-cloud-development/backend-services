package repository

import (
	"testing"
	"time"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

func TestDataDeletionProtocolCompletedRequiresEveryStageReceipt(t *testing.T) {
	now := time.Date(2026, 9, 26, 11, 0, 0, 0, time.UTC)
	providers := make([]agentdeletion.CapabilityProvider, 0, 8)
	for _, id := range []string{
		agentdeletion.CapabilitySubjectWriteFence, agentdeletion.CapabilityRelationshipMutation,
		agentdeletion.CapabilityMinIOSubjectDelete, agentdeletion.CapabilityQdrantSubjectDelete,
		agentdeletion.CapabilityTweetServiceDelete, agentdeletion.CapabilityMongoOwnedDelete,
		agentdeletion.CapabilityCrossStoreVerify, agentdeletion.CapabilityRequestFinalize,
	} {
		providers = append(providers, dataDeletionCompletionProvider{agentdeletion.CapabilityDescriptor{
			ID: id, Adapter: "test." + id, Version: "v1",
		}})
	}
	registry, err := agentdeletion.NewCapabilityRegistry(providers...)
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := agentdeletion.BuildProtocol(agentdeletion.Inventory{}, registry.Snapshot(), now)
	if err != nil {
		t.Fatal(err)
	}
	if dataDeletionProtocolCompleted(*protocol) {
		t.Fatal("pending protocol was treated as complete")
	}
	for index := range protocol.Checkpoint.Stages {
		stage := &protocol.Checkpoint.Stages[index]
		stage.Status = agentdeletion.StageSucceeded
		stage.Attempts = 1
		stage.ReceiptDigest = DataDeletionActorDigest(stage.StageID)
		stage.StartedAt = now
		stage.CompletedAt = now.Add(time.Second)
	}
	protocol.Checkpoint.CurrentStageID = ""
	protocol.Checkpoint.Revision = 17
	protocol.Checkpoint.UpdatedAt = now.Add(time.Second)
	if !dataDeletionProtocolCompleted(*protocol) {
		t.Fatal("completed protocol was not recognized")
	}
	protocol.Checkpoint.Stages[3].Status = agentdeletion.StageFailed
	protocol.Checkpoint.Stages[3].ReceiptDigest = ""
	protocol.Checkpoint.Stages[3].FailureCode = "failed"
	if dataDeletionProtocolCompleted(*protocol) {
		t.Fatal("failed protocol was treated as complete")
	}
}

type dataDeletionCompletionProvider struct {
	descriptor agentdeletion.CapabilityDescriptor
}

func (provider dataDeletionCompletionProvider) DeletionCapability() agentdeletion.CapabilityDescriptor {
	return provider.descriptor
}
