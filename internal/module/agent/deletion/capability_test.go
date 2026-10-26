package deletion

import "testing"

type testCapabilityProvider struct{ descriptor CapabilityDescriptor }

func (provider testCapabilityProvider) DeletionCapability() CapabilityDescriptor {
	return provider.descriptor
}

func testCapabilitySnapshot(t *testing.T, ids ...string) CapabilitySnapshot {
	t.Helper()
	providers := make([]CapabilityProvider, 0, len(ids))
	for _, id := range ids {
		providers = append(providers, testCapabilityProvider{descriptor: CapabilityDescriptor{
			ID: id, Adapter: "test/" + id, Version: "v1",
		}})
	}
	registry, err := NewCapabilityRegistry(providers...)
	if err != nil {
		t.Fatal(err)
	}
	return registry.Snapshot()
}

func allCapabilitySnapshot(t *testing.T) CapabilitySnapshot {
	return testCapabilitySnapshot(t,
		CapabilitySubjectWriteFence, CapabilityRelationshipMutation,
		CapabilityMinIOSubjectDelete, CapabilityQdrantSubjectDelete,
		CapabilityTweetServiceDelete, CapabilityMongoOwnedDelete,
		CapabilityCrossStoreVerify, CapabilityRequestFinalize,
	)
}

func TestCapabilityRegistrySnapshotIsDeterministicAndImmutable(t *testing.T) {
	left := testCapabilitySnapshot(t, CapabilityMongoOwnedDelete, CapabilitySubjectWriteFence)
	right := testCapabilitySnapshot(t, CapabilitySubjectWriteFence, CapabilityMongoOwnedDelete)
	if left.Digest != right.Digest {
		t.Fatalf("capability digest depends on registration order: %s != %s", left.Digest, right.Digest)
	}
	left.Capabilities[0].Adapter = "tampered"
	registry, err := NewCapabilityRegistry(testCapabilityProvider{CapabilityDescriptor{
		ID: CapabilityMongoOwnedDelete, Adapter: "mongo/delete", Version: "v1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := registry.Snapshot()
	snapshot.Capabilities[0].Adapter = "tampered"
	if registry.Snapshot().Capabilities[0].Adapter != "mongo/delete" {
		t.Fatal("registry snapshot was mutated by its caller")
	}
}

func TestCapabilityRegistryRejectsDuplicateAndUnknownCapabilities(t *testing.T) {
	provider := testCapabilityProvider{CapabilityDescriptor{ID: CapabilityMongoOwnedDelete, Adapter: "mongo/delete", Version: "v1"}}
	if _, err := NewCapabilityRegistry(provider, provider); err == nil {
		t.Fatal("duplicate capability was accepted")
	}
	if _, err := NewCapabilityRegistry(testCapabilityProvider{CapabilityDescriptor{ID: "unknown", Adapter: "unknown", Version: "v1"}}); err == nil {
		t.Fatal("unknown capability was accepted")
	}
}
