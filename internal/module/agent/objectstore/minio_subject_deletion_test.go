package objectstore

import (
	"context"
	"testing"
	"time"

	agentdeletion "twitter-clone/internal/module/agent/deletion"
)

type minioDeletionJournalStub struct{}

func (minioDeletionJournalStub) Claim(context.Context, agentdeletion.AdapterRequest, agentdeletion.PostState, time.Time) (agentdeletion.ExternalDeletionClaim, error) {
	return agentdeletion.ExternalDeletionClaim{}, nil
}

func (minioDeletionJournalStub) Complete(context.Context, agentdeletion.AdapterRequest, string, agentdeletion.ExecutionReceipt, time.Time) error {
	return nil
}

func TestMinIOSubjectDeletionAdapterUsesFixedBucketManifest(t *testing.T) {
	store := newMinIOToolResultStore(&toolResultBackendFake{}, "agent-tool-results")
	adapter, err := NewMinIOSubjectDeletionAdapter(store, minioDeletionJournalStub{})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := adapter.DeletionCapability()
	if descriptor.ID != agentdeletion.CapabilityMinIOSubjectDelete ||
		descriptor.Adapter != minioSubjectDeletionAdapterName || descriptor.Version != minioSubjectDeletionAdapterVersion {
		t.Fatalf("unexpected MinIO deletion capability: %+v", descriptor)
	}
	if MinIOSubjectDeletionScopeDigest("agent-tool-results") == MinIOSubjectDeletionScopeDigest("other-bucket") {
		t.Fatal("MinIO deletion scope ignored the bucket identity")
	}
}
