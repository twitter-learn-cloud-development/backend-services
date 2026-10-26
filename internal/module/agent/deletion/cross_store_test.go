package deletion

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCrossStoreVerificationRequiresEveryZeroResidualBoundary(t *testing.T) {
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	registry, err := NewCapabilityRegistry(crossStoreProviders()...)
	if err != nil {
		t.Fatal(err)
	}
	states := crossStoreStates(t, registry.Snapshot(), 42, now.Add(-time.Second))
	report, err := BuildCrossStoreVerificationReport("request-1", 42, sha256Hex("plan"), registry.Snapshot(), states, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCrossStoreVerificationReport(report); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), `"user_id"`) {
		t.Fatalf("report leaked raw user identity: %s", encoded)
	}
}

func TestCrossStoreVerificationRejectsMissingAndResidualState(t *testing.T) {
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	registry, err := NewCapabilityRegistry(crossStoreProviders()...)
	if err != nil {
		t.Fatal(err)
	}
	states := crossStoreStates(t, registry.Snapshot(), 42, now)
	if _, err := BuildCrossStoreVerificationReport("request-1", 42, sha256Hex("plan"), registry.Snapshot(), states[:len(states)-1], now); err == nil {
		t.Fatal("verification accepted a missing store state")
	}
	states[0], err = NewPostState(states[0].Capability, states[0].SubjectDigest, states[0].ScopeDigest, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildCrossStoreVerificationReport("request-1", 42, sha256Hex("plan"), registry.Snapshot(), states, now)
	if !errors.Is(err, ErrDeletionResidualsRemain) || report.ResidualCount != 1 {
		t.Fatalf("BuildCrossStoreVerificationReport() = %#v, %v", report, err)
	}
}

type crossStoreProvider CapabilityDescriptor

func (provider crossStoreProvider) DeletionCapability() CapabilityDescriptor {
	return CapabilityDescriptor(provider)
}

func crossStoreProviders() []CapabilityProvider {
	providers := make([]CapabilityProvider, 0, len(requiredCrossStoreCapabilities))
	for _, capabilityID := range requiredCrossStoreCapabilities {
		providers = append(providers, crossStoreProvider{ID: capabilityID, Adapter: capabilityID + "/adapter", Version: "v1"})
	}
	return providers
}

func crossStoreStates(t *testing.T, snapshot CapabilitySnapshot, userID uint64, observedAt time.Time) []PostState {
	t.Helper()
	states := make([]PostState, 0, len(snapshot.Capabilities))
	for _, capability := range snapshot.Capabilities {
		state, err := NewPostState(capability, DeletionSubjectDigest(userID), DeletionScopeDigest(capability.ID), 0, observedAt)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	return states
}
