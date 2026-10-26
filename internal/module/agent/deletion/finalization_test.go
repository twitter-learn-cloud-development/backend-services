package deletion

import (
	"testing"
	"time"
)

func TestBuildFinalizationReceiptBindsAllPriorStageReceipts(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	registry := finalizationTestRegistry(t)
	protocol, err := BuildProtocol(Inventory{}, registry.Snapshot(), now)
	if err != nil {
		t.Fatal(err)
	}
	for index := range protocol.Checkpoint.Stages {
		stage := &protocol.Checkpoint.Stages[index]
		stage.Attempts = 1
		stage.StartedAt = now.Add(time.Duration(index) * time.Second)
		if stage.StageID == StageFinalize {
			stage.Status = StageRunning
			stage.LeaseToken = "finalize-lease"
			stage.LeaseUntil = now.Add(time.Minute)
			protocol.Checkpoint.CurrentStageID = StageFinalize
			continue
		}
		stage.Status = StageSucceeded
		stage.ReceiptDigest = sha256Hex(stage.StageID)
		stage.CompletedAt = now.Add(time.Duration(index+1) * time.Second)
	}
	protocol.Checkpoint.Revision = 16
	protocol.Checkpoint.UpdatedAt = now.Add(8 * time.Second)
	receipt, err := BuildFinalizationReceipt("request-1", 42, *protocol, registry.Snapshot(), now.Add(9*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFinalizationReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	tampered := *protocol
	tampered.Checkpoint.Stages = append([]StageCheckpoint(nil), protocol.Checkpoint.Stages...)
	tampered.Checkpoint.Stages[2].ReceiptDigest = sha256Hex("tampered")
	other, err := BuildFinalizationReceipt("request-1", 42, tampered, registry.Snapshot(), now.Add(9*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if other.PriorReceiptsDigest == receipt.PriorReceiptsDigest || other.ReceiptDigest == receipt.ReceiptDigest {
		t.Fatal("finalization receipt did not bind prior stage receipts")
	}
}

func TestBuildFinalizationReceiptRejectsIncompleteProtocol(t *testing.T) {
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	registry := finalizationTestRegistry(t)
	protocol, err := BuildProtocol(Inventory{}, registry.Snapshot(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildFinalizationReceipt("request-1", 42, *protocol, registry.Snapshot(), now); err == nil {
		t.Fatal("incomplete protocol was accepted for finalization")
	}
}

func finalizationTestRegistry(t *testing.T) *CapabilityRegistry {
	t.Helper()
	ids := []string{
		CapabilitySubjectWriteFence, CapabilityRelationshipMutation, CapabilityMinIOSubjectDelete,
		CapabilityQdrantSubjectDelete, CapabilityTweetServiceDelete, CapabilityMongoOwnedDelete,
		CapabilityCrossStoreVerify, CapabilityRequestFinalize,
	}
	providers := make([]CapabilityProvider, 0, len(ids))
	for _, id := range ids {
		providers = append(providers, finalizationTestProvider{CapabilityDescriptor{ID: id, Adapter: "test/" + id, Version: "v1"}})
	}
	registry, err := NewCapabilityRegistry(providers...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

type finalizationTestProvider struct{ descriptor CapabilityDescriptor }

func (provider finalizationTestProvider) DeletionCapability() CapabilityDescriptor {
	return provider.descriptor
}
