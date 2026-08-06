package marketplace

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPublisherKeyRotationRetiresOldKeyAndNewPublicationRequiresActiveKey(t *testing.T) {
	oldPublic, oldPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	newPublic, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	record := testPublisherControl(oldPublic, now)

	rotated, err := RotatePublisherKey(record, SigningKey{
		KeyID: "key-2027", Algorithm: SignatureAlgorithmEd25519,
		PublicKeyBase64: base64.RawStdEncoding.EncodeToString(newPublic),
	}, 42, now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(2), rotated.Revision)
	require.Equal(t, KeyRetired, rotated.Publisher.SigningKeys[0].Status)
	require.Equal(t, KeyActive, rotated.Publisher.SigningKeys[1].Status)

	release, err := SignManifest(testManifest(), "key-2026", oldPrivate, now.Add(2*time.Hour))
	require.NoError(t, err)
	_, err = VerifyRelease(rotated.Publisher, release)
	require.NoError(t, err, "historical release stays verifiable after rotation")
	_, err = VerifyNewRelease(rotated.Publisher, release)
	require.ErrorIs(t, err, ErrSignatureVerification)
}

func TestPublisherKeyRevocationInvalidatesHistoricalRelease(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	now := time.Now().UTC()
	record := testPublisherControl(publicKey, now)
	release, err := SignManifest(testManifest(), "key-2026", privateKey, now)
	require.NoError(t, err)

	revoked, err := RevokePublisherKey(record, "key-2026", 42, now.Add(time.Minute))
	require.NoError(t, err)
	_, err = VerifyRelease(revoked.Publisher, release)
	require.ErrorIs(t, err, ErrSignatureVerification)
}

func TestReleaseWithdrawalIsTerminalAndReasonCoded(t *testing.T) {
	now := time.Now().UTC()
	record := ReleaseControl{
		Release: SignedRelease{
			ContractVersion: ReleaseContractVersion,
			ReleaseID:       StableReleaseID("openai-labs", "openai-labs.research", "1.2.3"),
			Manifest:        testManifest(), SignatureKeyID: "key-2026", SignatureBase64: "signed",
			Status: ReleasePublished, PublishedAt: now,
		},
		Revision: 1, PublishedBy: 42, CreatedAt: now, UpdatedAt: now,
	}
	withdrawn, err := WithdrawRelease(record, WithdrawalReasonSecurity, 42, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, ReleaseWithdrawn, withdrawn.Release.Status)
	require.Equal(t, int64(2), withdrawn.Revision)
	_, err = WithdrawRelease(withdrawn, WithdrawalReasonPublisher, 42, now.Add(2*time.Minute))
	require.ErrorIs(t, err, ErrRevisionConflict)
	_, err = WithdrawRelease(record, "free_form_reason", 42, now.Add(time.Minute))
	require.ErrorIs(t, err, ErrInvalidWithdrawal)
}

func TestPublisherOwnershipIsNormalizedAndImmutableProjectionIsSearchable(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	record := testPublisherControl(publicKey, time.Now().UTC())
	record.OwnerUserIDs = []uint64{9, 2, 9}
	normalized, err := NormalizePublisherControl(record)
	require.NoError(t, err)
	require.Equal(t, []uint64{2, 9}, normalized.OwnerUserIDs)
	require.True(t, PublisherOwnedBy(normalized, 9))
	require.False(t, PublisherOwnedBy(normalized, 3))
}

func testPublisherControl(publicKey ed25519.PublicKey, now time.Time) PublisherControl {
	return PublisherControl{
		Publisher: testPublisher(publicKey, KeyActive), OwnerUserIDs: []uint64{42},
		Revision: 1, CreatedBy: 1, UpdatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
}
