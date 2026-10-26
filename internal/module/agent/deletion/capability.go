package deletion

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const CapabilitySnapshotSchemaV1 = "agent-data-deletion-capabilities/v1"

type CapabilityDescriptor struct {
	ID      string `bson:"id" json:"id"`
	Adapter string `bson:"adapter" json:"adapter"`
	Version string `bson:"version" json:"version"`
}

type CapabilityProvider interface {
	DeletionCapability() CapabilityDescriptor
}

type CapabilitySnapshot struct {
	SchemaVersion string                 `bson:"schema_version" json:"schema_version"`
	Digest        string                 `bson:"digest" json:"digest"`
	Capabilities  []CapabilityDescriptor `bson:"capabilities" json:"capabilities"`
}

type CapabilityRegistry struct {
	snapshot CapabilitySnapshot
}

func NewCapabilityRegistry(providers ...CapabilityProvider) (*CapabilityRegistry, error) {
	descriptors := make([]CapabilityDescriptor, 0, len(providers))
	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		if provider == nil {
			return nil, errors.New("data deletion capability provider is nil")
		}
		descriptor := normalizeCapabilityDescriptor(provider.DeletionCapability())
		if err := validateCapabilityDescriptor(descriptor); err != nil {
			return nil, err
		}
		if _, exists := seen[descriptor.ID]; exists {
			return nil, fmt.Errorf("duplicate data deletion capability %q", descriptor.ID)
		}
		seen[descriptor.ID] = struct{}{}
		descriptors = append(descriptors, descriptor)
	}
	sort.Slice(descriptors, func(left, right int) bool { return descriptors[left].ID < descriptors[right].ID })
	snapshot := CapabilitySnapshot{SchemaVersion: CapabilitySnapshotSchemaV1, Capabilities: descriptors}
	snapshot.Digest = capabilitySnapshotDigest(snapshot)
	return &CapabilityRegistry{snapshot: snapshot}, nil
}

func EmptyCapabilityRegistry() *CapabilityRegistry {
	registry, _ := NewCapabilityRegistry()
	return registry
}

func (r *CapabilityRegistry) Snapshot() CapabilitySnapshot {
	if r == nil {
		return CapabilitySnapshot{}
	}
	result := r.snapshot
	result.Capabilities = append([]CapabilityDescriptor(nil), r.snapshot.Capabilities...)
	return result
}

func ValidateCapabilitySnapshot(snapshot CapabilitySnapshot) error {
	if snapshot.SchemaVersion != CapabilitySnapshotSchemaV1 || !isSHA256(snapshot.Digest) {
		return errors.New("data deletion capability snapshot identity is invalid")
	}
	previous := ""
	for _, descriptor := range snapshot.Capabilities {
		if err := validateCapabilityDescriptor(descriptor); err != nil {
			return err
		}
		if descriptor.ID <= previous {
			return errors.New("data deletion capabilities must be unique and sorted")
		}
		previous = descriptor.ID
	}
	if snapshot.Digest != capabilitySnapshotDigest(snapshot) {
		return errors.New("data deletion capability snapshot digest mismatch")
	}
	return nil
}

func CapabilityAvailable(snapshot CapabilitySnapshot, capability string) bool {
	index := sort.Search(len(snapshot.Capabilities), func(index int) bool {
		return snapshot.Capabilities[index].ID >= capability
	})
	return index < len(snapshot.Capabilities) && snapshot.Capabilities[index].ID == capability
}

func normalizeCapabilityDescriptor(descriptor CapabilityDescriptor) CapabilityDescriptor {
	descriptor.ID = strings.TrimSpace(descriptor.ID)
	descriptor.Adapter = strings.TrimSpace(descriptor.Adapter)
	descriptor.Version = strings.TrimSpace(descriptor.Version)
	return descriptor
}

func validateCapabilityDescriptor(descriptor CapabilityDescriptor) error {
	if _, ok := knownCapabilities()[descriptor.ID]; !ok {
		return fmt.Errorf("unknown data deletion capability %q", descriptor.ID)
	}
	if !isBoundedIdentity(descriptor.Adapter, 96) || !isBoundedIdentity(descriptor.Version, 64) {
		return errors.New("data deletion capability adapter identity is invalid")
	}
	return nil
}

func capabilitySnapshotDigest(snapshot CapabilitySnapshot) string {
	parts := []string{snapshot.SchemaVersion}
	for _, descriptor := range snapshot.Capabilities {
		parts = append(parts, descriptor.ID+"|"+descriptor.Adapter+"|"+descriptor.Version)
	}
	return sha256Hex(strings.Join(parts, "\x00"))
}

func knownCapabilities() map[string]struct{} {
	return map[string]struct{}{
		CapabilitySubjectWriteFence: {}, CapabilityRelationshipMutation: {},
		CapabilityMinIOSubjectDelete: {}, CapabilityQdrantSubjectDelete: {},
		CapabilityTweetServiceDelete: {}, CapabilityMongoOwnedDelete: {},
		CapabilityCrossStoreVerify: {}, CapabilityRequestFinalize: {},
	}
}

func isBoundedIdentity(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '-' || character == '.' || character == '/' {
			continue
		}
		return false
	}
	return true
}
