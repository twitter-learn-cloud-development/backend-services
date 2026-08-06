package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"twitter-clone/internal/module/agent/marketplace"
)

func TestExtensionMarketplaceManagerEnforcesOwnershipAndAuditsKeyRotation(t *testing.T) {
	store := newExtensionMarketplaceStoreFake()
	clock := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	manager := newTestExtensionMarketplaceManager(store, clock)
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	publisher, err := manager.RegisterPublisher(context.Background(), 1, RegisterExtensionPublisherRequest{
		PublisherID: "publisher", DisplayName: "Publisher", OwnerUserIDs: []uint64{42},
		InitialKeyID: "key-1", PublicKeyBase64: base64.RawStdEncoding.EncodeToString(publicKey),
	})
	require.NoError(t, err)

	newPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, err = manager.RotatePublisherKey(context.Background(), 99, "publisher", marketplace.SigningKey{
		KeyID: "key-2", Algorithm: marketplace.SignatureAlgorithmEd25519,
		PublicKeyBase64: base64.RawStdEncoding.EncodeToString(newPublicKey),
	}, publisher.Revision)
	require.ErrorIs(t, err, marketplace.ErrControlForbidden)

	rotated, err := manager.RotatePublisherKey(context.Background(), 42, "publisher", marketplace.SigningKey{
		KeyID: "key-2", Algorithm: marketplace.SignatureAlgorithmEd25519,
		PublicKeyBase64: base64.RawStdEncoding.EncodeToString(newPublicKey),
	}, publisher.Revision)
	require.NoError(t, err)
	require.Equal(t, int64(2), rotated.Revision)
	require.Len(t, store.audits, 4, "registration and rotation each write requested+succeeded")
	require.Equal(t, marketplace.AuditOutcomeRequested, store.audits[2].Outcome)
	require.Equal(t, marketplace.AuditOutcomeSucceeded, store.audits[3].Outcome)
	require.Equal(t, store.audits[2].OperationID, store.audits[3].OperationID)
}

func TestExtensionMarketplaceManagerRejectsRetiredKeyForNewReleaseAndWithdrawsByCAS(t *testing.T) {
	store := newExtensionMarketplaceStoreFake()
	clock := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	manager := newTestExtensionMarketplaceManager(store, clock)
	oldPublic, oldPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	publisher, err := manager.RegisterPublisher(context.Background(), 1, RegisterExtensionPublisherRequest{
		PublisherID: "publisher", DisplayName: "Publisher", OwnerUserIDs: []uint64{42},
		InitialKeyID: "key-1", PublicKeyBase64: base64.RawStdEncoding.EncodeToString(oldPublic),
	})
	require.NoError(t, err)
	newPublic, newPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	publisher, err = manager.RotatePublisherKey(context.Background(), 42, "publisher", marketplace.SigningKey{
		KeyID: "key-2", Algorithm: marketplace.SignatureAlgorithmEd25519,
		PublicKeyBase64: base64.RawStdEncoding.EncodeToString(newPublic),
	}, publisher.Revision)
	require.NoError(t, err)
	manifest := extensionMarketplaceTestManifest()
	oldRelease, err := marketplace.SignManifest(manifest, "key-1", oldPrivate, clock)
	require.NoError(t, err)
	_, err = manager.PublishRelease(context.Background(), 42, PublishExtensionReleaseRequest{
		Manifest: oldRelease.Manifest, SignatureKeyID: oldRelease.SignatureKeyID,
		SignatureBase64: oldRelease.SignatureBase64, ExpectedPublisherRevision: publisher.Revision,
	})
	require.ErrorIs(t, err, marketplace.ErrSignatureVerification)

	newRelease, err := marketplace.SignManifest(manifest, "key-2", newPrivate, clock)
	require.NoError(t, err)
	published, err := manager.PublishRelease(context.Background(), 42, PublishExtensionReleaseRequest{
		Manifest: newRelease.Manifest, SignatureKeyID: newRelease.SignatureKeyID,
		SignatureBase64: newRelease.SignatureBase64, ExpectedPublisherRevision: publisher.Revision,
	})
	require.NoError(t, err)
	withdrawn, err := manager.WithdrawRelease(
		context.Background(), 42, published.Release.ReleaseID,
		marketplace.WithdrawalReasonPublisher, published.Revision,
	)
	require.NoError(t, err)
	require.Equal(t, marketplace.ReleaseWithdrawn, withdrawn.Release.Status)
	_, err = manager.WithdrawRelease(context.Background(), 42, published.Release.ReleaseID,
		marketplace.WithdrawalReasonPublisher, published.Revision)
	require.ErrorIs(t, err, marketplace.ErrRevisionConflict)
}

func TestExtensionMarketplaceManagerScopesListingsToOwnedPublishers(t *testing.T) {
	store := newExtensionMarketplaceStoreFake()
	manager := newTestExtensionMarketplaceManager(store, time.Now().UTC())
	for index, owner := range []uint64{42, 99} {
		publicKey, _, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		_, err = manager.RegisterPublisher(context.Background(), 1, RegisterExtensionPublisherRequest{
			PublisherID: fmt.Sprintf("publisher-%d", index), DisplayName: "Publisher",
			OwnerUserIDs: []uint64{owner}, InitialKeyID: "key-1",
			PublicKeyBase64: base64.RawStdEncoding.EncodeToString(publicKey),
		})
		require.NoError(t, err)
	}
	access, err := manager.ResolveAccess(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, access.PlatformAdmin)
	require.Equal(t, []string{"publisher-0"}, access.OwnedPublisherIDs)
	publishers, total, err := manager.ListPublishers(context.Background(), 42, marketplace.ManagementPage{})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "publisher-0", publishers[0].Publisher.PublisherID)
}

func newTestExtensionMarketplaceManager(store marketplace.ControlStore, clock time.Time) *ExtensionMarketplaceManager {
	sequence := 0
	return NewExtensionMarketplaceManager(store, true, []uint64{1},
		WithExtensionMarketplaceClock(func() time.Time { return clock }),
		WithExtensionMarketplaceIDGenerator(func(prefix string) (string, error) {
			sequence++
			return fmt.Sprintf("%s_%d", prefix, sequence), nil
		}),
	)
}

func extensionMarketplaceTestManifest() marketplace.Manifest {
	return marketplace.Manifest{
		ContractVersion: marketplace.ManifestContractVersion,
		PackageID:       "publisher.package", Kind: marketplace.KindSkill, Version: "1.0.0",
		PublisherID: "publisher", DisplayName: "Package", Description: "Description",
		ArtifactDigestSHA256: strings.Repeat("a", 64), CapabilityIDs: []string{"web.search"},
		RequestedPermissions: []string{marketplace.PermissionNetwork},
	}
}

type extensionMarketplaceStoreFake struct {
	publishers map[string]marketplace.PublisherControl
	releases   map[string]marketplace.ReleaseControl
	audits     []marketplace.AuditEvent
}

func newExtensionMarketplaceStoreFake() *extensionMarketplaceStoreFake {
	return &extensionMarketplaceStoreFake{
		publishers: make(map[string]marketplace.PublisherControl),
		releases:   make(map[string]marketplace.ReleaseControl),
	}
}

func (s *extensionMarketplaceStoreFake) CreatePublisher(_ context.Context, record marketplace.PublisherControl) error {
	id := record.Publisher.PublisherID
	if _, exists := s.publishers[id]; exists {
		return marketplace.ErrPublisherConflict
	}
	s.publishers[id] = record
	return nil
}

func (s *extensionMarketplaceStoreFake) GetPublisherControl(_ context.Context, publisherID string) (marketplace.PublisherControl, error) {
	record, exists := s.publishers[strings.ToLower(strings.TrimSpace(publisherID))]
	if !exists {
		return marketplace.PublisherControl{}, marketplace.ErrPublisherNotFound
	}
	return record, nil
}

func (s *extensionMarketplaceStoreFake) ListPublisherControls(_ context.Context, owner uint64, all bool, page marketplace.ManagementPage) ([]marketplace.PublisherControl, int64, error) {
	_, err := marketplace.NormalizeManagementPage(page)
	if err != nil {
		return nil, 0, err
	}
	result := make([]marketplace.PublisherControl, 0)
	for _, record := range s.publishers {
		if all || marketplace.PublisherOwnedBy(record, owner) {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Publisher.PublisherID < result[j].Publisher.PublisherID })
	return result, int64(len(result)), nil
}

func (s *extensionMarketplaceStoreFake) ListOwnedPublisherIDs(_ context.Context, owner uint64) ([]string, error) {
	result := make([]string, 0)
	for id, record := range s.publishers {
		if marketplace.PublisherOwnedBy(record, owner) {
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result, nil
}

func (s *extensionMarketplaceStoreFake) UpdatePublisherControl(_ context.Context, record marketplace.PublisherControl, expected int64) error {
	current, exists := s.publishers[record.Publisher.PublisherID]
	if !exists {
		return marketplace.ErrPublisherNotFound
	}
	if current.Revision != expected || record.Revision != expected+1 {
		return marketplace.ErrRevisionConflict
	}
	s.publishers[record.Publisher.PublisherID] = record
	return nil
}

func (s *extensionMarketplaceStoreFake) CreateRelease(_ context.Context, record marketplace.ReleaseControl) error {
	if _, exists := s.releases[record.Release.ReleaseID]; exists {
		return marketplace.ErrReleaseConflict
	}
	s.releases[record.Release.ReleaseID] = record
	return nil
}

func (s *extensionMarketplaceStoreFake) GetReleaseControl(_ context.Context, releaseID string) (marketplace.ReleaseControl, error) {
	record, exists := s.releases[releaseID]
	if !exists {
		return marketplace.ReleaseControl{}, marketplace.ErrReleaseNotFound
	}
	return record, nil
}

func (s *extensionMarketplaceStoreFake) ListReleaseControls(_ context.Context, ids []string, status string, _ marketplace.ManagementPage) ([]marketplace.ReleaseControl, int64, error) {
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	result := make([]marketplace.ReleaseControl, 0)
	for _, record := range s.releases {
		_, matched := allowed[record.Release.Manifest.PublisherID]
		if ids != nil && !matched {
			continue
		}
		if status == "" || status == record.Release.Status {
			result = append(result, record)
		}
	}
	return result, int64(len(result)), nil
}

func (s *extensionMarketplaceStoreFake) UpdateReleaseControl(_ context.Context, record marketplace.ReleaseControl, expected int64) error {
	current, exists := s.releases[record.Release.ReleaseID]
	if !exists {
		return marketplace.ErrReleaseNotFound
	}
	if current.Revision != expected || record.Revision != expected+1 {
		return marketplace.ErrRevisionConflict
	}
	s.releases[record.Release.ReleaseID] = record
	return nil
}

func (s *extensionMarketplaceStoreFake) AppendAuditEvent(_ context.Context, event marketplace.AuditEvent) error {
	if _, err := marketplace.NormalizeAuditEvent(event); err != nil {
		return err
	}
	s.audits = append(s.audits, event)
	return nil
}

func (s *extensionMarketplaceStoreFake) ListAuditEvents(_ context.Context, ids []string, action, outcome string, _ marketplace.ManagementPage) ([]marketplace.AuditEvent, int64, error) {
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	result := make([]marketplace.AuditEvent, 0)
	for _, event := range s.audits {
		_, matched := allowed[event.PublisherID]
		if ids != nil && !matched {
			continue
		}
		if action != "" && action != event.Action {
			continue
		}
		if outcome != "" && outcome != event.Outcome {
			continue
		}
		result = append(result, event)
	}
	return result, int64(len(result)), nil
}

var _ marketplace.ControlStore = (*extensionMarketplaceStoreFake)(nil)
