package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"twitter-clone/internal/module/agent/marketplace"
)

func TestAgentExtensionMarketplaceListsVerifiedReleases(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	publisher := marketplaceTestPublisher(publicKey)
	newer := marketplaceTestRelease(t, privateKey, "publisher.research", "1.1.0", time.Date(2026, 8, 3, 2, 0, 0, 0, time.UTC))
	older := marketplaceTestRelease(t, privateKey, "publisher.writer", "1.0.0", time.Date(2026, 8, 3, 1, 0, 0, 0, time.UTC))
	store := &marketplaceStoreFake{
		releases: []marketplace.SignedRelease{newer, older}, hasMore: true,
		publishers: map[string]marketplace.Publisher{publisher.PublisherID: publisher},
	}
	service := &AgentService{
		extensionMarketplaceEnabled: true,
		extensionMarketplaceLimit:   marketplace.DefaultPageSize,
		extensionMarketplaceStore:   store,
	}
	page, err := service.ListAgentMarketplaceExtensions(context.Background(), 42, marketplace.Query{
		Kind: marketplace.KindSkill, Search: "research", PageSize: 2,
	})
	require.NoError(t, err)
	require.Equal(t, marketplace.CatalogContractVersion, page.ContractVersion)
	require.Len(t, page.Releases, 2)
	require.True(t, page.HasMore)
	require.NotEmpty(t, page.NextCursor)
	require.True(t, page.Releases[0].SignatureVerified)
	require.Equal(t, "publisher", page.Releases[0].Publisher.PublisherID)
	require.Equal(t, marketplace.KindSkill, store.request.Kind)
	require.Equal(t, "research", store.request.Search)
	require.Equal(t, 2, store.request.PageSize)
	require.ElementsMatch(t, []string{"publisher"}, store.publisherIDs)
}

func TestAgentExtensionMarketplaceFailsClosedOnTamperAndMissingPublisher(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	publisher := marketplaceTestPublisher(publicKey)
	release := marketplaceTestRelease(t, privateKey, "publisher.research", "1.0.0", time.Now())
	release.Manifest.Description = "tampered"
	service := &AgentService{
		extensionMarketplaceEnabled: true, extensionMarketplaceLimit: 20,
		extensionMarketplaceStore: &marketplaceStoreFake{
			releases:   []marketplace.SignedRelease{release},
			publishers: map[string]marketplace.Publisher{"publisher": publisher},
		},
	}
	_, err = service.ListAgentMarketplaceExtensions(context.Background(), 42, marketplace.Query{})
	require.ErrorIs(t, err, marketplace.ErrSignatureVerification)

	service.extensionMarketplaceStore = &marketplaceStoreFake{releases: []marketplace.SignedRelease{release}}
	_, err = service.ListAgentMarketplaceExtensions(context.Background(), 42, marketplace.Query{})
	require.ErrorIs(t, err, marketplace.ErrSignatureVerification)
}

func TestAgentExtensionMarketplaceRejectsDisabledInvalidCancelledAndStoreErrors(t *testing.T) {
	service := &AgentService{}
	_, err := service.ListAgentMarketplaceExtensions(context.Background(), 42, marketplace.Query{})
	require.ErrorIs(t, err, marketplace.ErrCatalogDisabled)

	service.extensionMarketplaceEnabled = true
	service.extensionMarketplaceLimit = 20
	service.extensionMarketplaceStore = &marketplaceStoreFake{}
	_, err = service.ListAgentMarketplaceExtensions(context.Background(), 0, marketplace.Query{})
	require.ErrorIs(t, err, ErrInvalidUnifiedAgentRequest)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.ListAgentMarketplaceExtensions(cancelled, 42, marketplace.Query{})
	require.ErrorIs(t, err, context.Canceled)

	expected := errors.New("store unavailable")
	service.extensionMarketplaceStore = &marketplaceStoreFake{err: expected}
	_, err = service.ListAgentMarketplaceExtensions(context.Background(), 42, marketplace.Query{})
	require.ErrorIs(t, err, expected)
}

type marketplaceStoreFake struct {
	releases     []marketplace.SignedRelease
	publishers   map[string]marketplace.Publisher
	hasMore      bool
	err          error
	request      marketplace.ListRequest
	publisherIDs []string
}

func (store *marketplaceStoreFake) ListPublished(
	_ context.Context,
	request marketplace.ListRequest,
) ([]marketplace.SignedRelease, bool, error) {
	store.request = request
	return append([]marketplace.SignedRelease(nil), store.releases...), store.hasMore, store.err
}

func (store *marketplaceStoreFake) GetPublishers(
	_ context.Context,
	publisherIDs []string,
) (map[string]marketplace.Publisher, error) {
	store.publisherIDs = append([]string(nil), publisherIDs...)
	if store.err != nil {
		return nil, store.err
	}
	result := make(map[string]marketplace.Publisher, len(store.publishers))
	for key, publisher := range store.publishers {
		result[key] = publisher
	}
	return result, nil
}

func marketplaceTestPublisher(publicKey ed25519.PublicKey) marketplace.Publisher {
	return marketplace.Publisher{
		ContractVersion: marketplace.PublisherContractVersion,
		PublisherID:     "publisher", DisplayName: "Verified Publisher",
		Verification: marketplace.PublisherVerified,
		SigningKeys: []marketplace.SigningKey{{
			KeyID: "key-1", Algorithm: marketplace.SignatureAlgorithmEd25519,
			PublicKeyBase64: base64.RawStdEncoding.EncodeToString(publicKey), Status: marketplace.KeyActive,
		}},
		VerifiedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
}

func marketplaceTestRelease(
	t *testing.T,
	privateKey ed25519.PrivateKey,
	packageID string,
	version string,
	publishedAt time.Time,
) marketplace.SignedRelease {
	t.Helper()
	release, err := marketplace.SignManifest(marketplace.Manifest{
		ContractVersion: marketplace.ManifestContractVersion,
		PackageID:       packageID, Kind: marketplace.KindSkill, Version: version,
		PublisherID: "publisher", DisplayName: packageID, Description: "Verified package",
		ArtifactDigestSHA256: strings.Repeat("a", 64),
		CapabilityIDs:        []string{"conversation.reply"},
		RequestedPermissions: []string{marketplace.PermissionNetwork},
	}, "key-1", privateKey, publishedAt)
	require.NoError(t, err)
	return release
}
