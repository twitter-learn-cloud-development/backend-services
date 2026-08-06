package repository

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/stretchr/testify/require"

	"twitter-clone/internal/module/agent/marketplace"
)

func TestMarketplaceReleaseFilterUsesStablePositionAndText(t *testing.T) {
	position := time.Date(2026, 8, 3, 3, 2, 1, 0, time.UTC)
	filter := marketplaceReleaseFilter(marketplace.ListRequest{
		Kind: marketplace.KindSkill, PublisherID: "publisher", Search: "research",
		After:    &marketplace.CursorPosition{PublishedAt: position, ReleaseID: "release_a"},
		PageSize: 20,
	})
	encoded, err := bson.MarshalExtJSON(filter, true, false)
	require.NoError(t, err)
	value := string(encoded)
	require.Contains(t, value, "manifest.kind")
	require.Contains(t, value, "manifest.publisher_id")
	require.Contains(t, value, "$text")
	require.Contains(t, value, "$lt")
	require.Contains(t, value, "$gt")
}

func TestMarketplaceReleaseRecordRoundTripCopiesDeclarations(t *testing.T) {
	release := marketplace.SignedRelease{
		ContractVersion: marketplace.ReleaseContractVersion, ReleaseID: "release_a",
		Manifest: marketplace.Manifest{
			ContractVersion: marketplace.ManifestContractVersion,
			PackageID:       "publisher.package", Kind: marketplace.KindMCPServer, Version: "1.0.0",
			PublisherID: "publisher", DisplayName: "Package", Description: "Description",
			ArtifactDigestSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			CapabilityIDs:        []string{"connector.mcp"},
			RequestedPermissions: []string{marketplace.PermissionNetwork},
		},
		SignatureKeyID: "key-1", SignatureBase64: "signature",
		Status: marketplace.ReleasePublished, PublishedAt: time.Now().UTC(),
	}
	record := marketplaceReleaseRecordFromDomain(release)
	result := marketplaceReleaseRecordToDomain(record)
	require.Equal(t, release, result)

	record.Manifest.CapabilityIDs[0] = "changed"
	require.Equal(t, "connector.mcp", release.Manifest.CapabilityIDs[0])
}
