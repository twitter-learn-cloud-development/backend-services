package marketplace

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSignedReleaseVerifiesCanonicalManifest(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	publisher := testPublisher(publicKey, KeyActive)
	manifest := testManifest()
	manifest.CapabilityIDs = []string{"web.search", "conversation.reply", "web.search"}
	manifest.RequestedPermissions = []string{PermissionNetwork, PermissionReadUserData, PermissionNetwork}

	release, err := SignManifest(manifest, "key-2026", privateKey, time.Date(2026, 8, 3, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	require.NoError(t, err)
	listing, err := VerifyRelease(publisher, release)
	require.NoError(t, err)
	require.True(t, listing.SignatureVerified)
	require.Equal(t, "release_"+strings.TrimPrefix(release.ReleaseID, "release_"), listing.ReleaseID)
	require.Equal(t, []string{"conversation.reply", "web.search"}, listing.CapabilityIDs)
	require.Equal(t, []string{PermissionNetwork, PermissionReadUserData}, listing.RequestedPermissions)
	require.Equal(t, time.UTC, listing.PublishedAt.Location())
}

func TestSignedReleaseRejectsTamperSuspensionAndRevocation(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	release, err := SignManifest(testManifest(), "key-2026", privateKey, time.Now())
	require.NoError(t, err)

	tampered := release
	tampered.Manifest.Description = "tampered"
	_, err = VerifyRelease(testPublisher(publicKey, KeyActive), tampered)
	require.ErrorIs(t, err, ErrSignatureVerification)

	retired := testPublisher(publicKey, KeyRetired)
	_, err = VerifyRelease(retired, release)
	require.NoError(t, err)

	revoked := testPublisher(publicKey, KeyRevoked)
	_, err = VerifyRelease(revoked, release)
	require.ErrorIs(t, err, ErrSignatureVerification)

	suspended := testPublisher(publicKey, KeyActive)
	suspended.Verification = PublisherSuspended
	_, err = VerifyRelease(suspended, release)
	require.ErrorIs(t, err, ErrSignatureVerification)
}

func TestPrepareListBindsStableCursorToFilters(t *testing.T) {
	request, err := PrepareList(Query{Kind: KindSkill, Search: "Research", PageSize: 1}, 20)
	require.NoError(t, err)
	listing := testListing("release_a", time.Date(2026, 8, 3, 1, 2, 3, 4, time.UTC))
	page, err := BuildPage(request, []Listing{listing}, true)
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.NotEmpty(t, page.NextCursor)

	next, err := PrepareList(Query{
		Kind: KindSkill, Search: "research", PageSize: 1, AfterCursor: page.NextCursor,
	}, 20)
	require.NoError(t, err)
	require.NotNil(t, next.After)
	require.Equal(t, listing.ReleaseID, next.After.ReleaseID)
	require.Equal(t, listing.PublishedAt, next.After.PublishedAt)

	_, err = PrepareList(Query{
		Kind: KindMCPServer, Search: "research", PageSize: 1, AfterCursor: page.NextCursor,
	}, 20)
	require.ErrorIs(t, err, ErrInvalidCursor)
}

func TestBuildPageRejectsUnverifiedAndUnstableRows(t *testing.T) {
	request, err := PrepareList(Query{PageSize: 2}, 20)
	require.NoError(t, err)
	first := testListing("release_a", time.Date(2026, 8, 3, 1, 0, 0, 0, time.UTC))
	unverified := first
	unverified.SignatureVerified = false
	_, err = BuildPage(request, []Listing{unverified}, false)
	require.ErrorIs(t, err, ErrInvalidRelease)

	newer := testListing("release_b", first.PublishedAt.Add(time.Minute))
	_, err = BuildPage(request, []Listing{first, newer}, false)
	require.ErrorIs(t, err, ErrInvalidRelease)
}

func TestManifestAndQueryValidation(t *testing.T) {
	manifest := testManifest()
	manifest.Version = "latest"
	_, err := NormalizeManifest(manifest)
	require.ErrorIs(t, err, ErrInvalidManifest)

	manifest = testManifest()
	manifest.RequestedPermissions = []string{"root_access"}
	_, err = NormalizeManifest(manifest)
	require.ErrorIs(t, err, ErrInvalidManifest)

	_, err = PrepareList(Query{Kind: "unknown"}, 20)
	require.ErrorIs(t, err, ErrInvalidQuery)
	_, err = PrepareList(Query{AfterCursor: "not-a-cursor"}, 20)
	require.ErrorIs(t, err, ErrInvalidCursor)
}

func testPublisher(publicKey ed25519.PublicKey, keyStatus string) Publisher {
	return Publisher{
		ContractVersion: PublisherContractVersion,
		PublisherID:     "openai-labs", DisplayName: "OpenAI Labs", Verification: PublisherVerified,
		SigningKeys: []SigningKey{{
			KeyID: "key-2026", Algorithm: SignatureAlgorithmEd25519,
			PublicKeyBase64: base64PublicKey(publicKey), Status: keyStatus,
		}},
		VerifiedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
}

func testManifest() Manifest {
	return Manifest{
		ContractVersion: ManifestContractVersion,
		PackageID:       "openai-labs.research", Kind: KindSkill, Version: "1.2.3",
		PublisherID: "openai-labs", DisplayName: "Research assistant",
		Description:          "Builds source-grounded research drafts.",
		ArtifactDigestSHA256: strings.Repeat("a", 64),
		CapabilityIDs:        []string{"web.search"}, RequestedPermissions: []string{PermissionNetwork},
	}
}

func testListing(releaseID string, publishedAt time.Time) Listing {
	return Listing{
		ContractVersion: CatalogContractVersion,
		ReleaseID:       releaseID, PackageID: "publisher.package", Kind: KindSkill,
		Version: "1.0.0", DisplayName: "Package", Publisher: PublisherSummary{
			PublisherID: "publisher", DisplayName: "Publisher", Verification: PublisherVerified,
		},
		ArtifactDigestSHA256: strings.Repeat("b", 64), SignatureKeyID: "key-1",
		CapabilityIDs: []string{"conversation.reply"}, PublishedAt: publishedAt,
		SignatureVerified: true,
	}
}

func base64PublicKey(publicKey ed25519.PublicKey) string {
	return base64.RawStdEncoding.EncodeToString(publicKey)
}
