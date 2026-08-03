package repository

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"twitter-clone/internal/domain"
)

func TestClaimableOutboxTasksQueryUsesSkipLockedAndBackoff(t *testing.T) {
	db := newOutboxDryRunDB(t)
	request := domain.OutboxClaimRequest{
		LeaseOwner:          "worker-a",
		LeaseToken:          "token-a",
		ClaimedAtUnixMilli:  1_800_000_000_000,
		LeaseUntilUnixMilli: 1_800_000_090_000,
		Limit:               10,
	}
	var tasks []*domain.OutboxTask
	statement := claimableOutboxTasksQuery(db, request).Find(&tasks).Statement
	sql := statement.SQL.String()

	require.Contains(t, sql, "FOR UPDATE SKIP LOCKED")
	require.Contains(t, sql, "retries < max_retries")
	require.Contains(t, sql, "CASE retries")
	require.Contains(t, sql, "ORDER BY id ASC LIMIT ?")
}

func TestActiveOutboxClaimQueryFencesOwnerTokenAndExpiry(t *testing.T) {
	db := newOutboxDryRunDB(t)
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return activeOutboxClaimQuery(tx, 91, "worker-a", "token-a", 1_800_000_000_000).
			Updates(map[string]interface{}{"status": domain.OutboxStatusSuccess})
	})

	require.Contains(t, sql, "status")
	require.Contains(t, sql, "lease_owner")
	require.Contains(t, sql, "lease_token")
	require.Contains(t, sql, "lease_until")
	require.Contains(t, sql, "worker-a")
	require.Contains(t, sql, "token-a")
	require.Contains(t, sql, "1800000000000")
}

func TestExpiredOutboxClaimsQueryIsBoundedAndSkipLocked(t *testing.T) {
	db := newOutboxDryRunDB(t)
	var tasks []*domain.OutboxTask
	statement := expiredOutboxClaimsQuery(db, 1_800_000_000_000, 25).Find(&tasks).Statement
	sql := statement.SQL.String()

	require.Contains(t, sql, "status = ? AND lease_until > 0 AND lease_until <= ?")
	require.Contains(t, sql, "ORDER BY lease_until ASC, id ASC LIMIT ?")
	require.Contains(t, sql, "FOR UPDATE SKIP LOCKED")
}

func TestOutboxClaimValidationRejectsUnsafeIdentityAndWindow(t *testing.T) {
	repository := &outboxRepo{}

	_, err := repository.Claim(context.Background(), domain.OutboxClaimRequest{
		LeaseOwner:          strings.Repeat("a", 192),
		LeaseToken:          "token-a",
		ClaimedAtUnixMilli:  100,
		LeaseUntilUnixMilli: 200,
		Limit:               1,
	})
	require.ErrorContains(t, err, "lease owner")

	_, err = repository.Claim(context.Background(), domain.OutboxClaimRequest{
		LeaseOwner:          "worker-a",
		LeaseToken:          "token-a",
		ClaimedAtUnixMilli:  200,
		LeaseUntilUnixMilli: 200,
		Limit:               1,
	})
	require.ErrorContains(t, err, "lease window")
}

func TestPartitionExpiredOutboxClaimsSeparatesRetryableAndExhausted(t *testing.T) {
	retryable, exhausted := partitionExpiredOutboxClaims([]*domain.OutboxTask{
		{ID: 1, Retries: 1, MaxRetries: 3},
		{ID: 2, Retries: 3, MaxRetries: 3},
		{ID: 3, Retries: 4, MaxRetries: 3},
	})

	require.Equal(t, []uint64{1}, retryable)
	require.Equal(t, []uint64{2, 3}, exhausted)
}

func TestBoundedOutboxErrorPreservesUTF8Boundary(t *testing.T) {
	bounded := boundedOutboxError(strings.Repeat("错", maxOutboxErrorBytes))

	require.LessOrEqual(t, len(bounded), maxOutboxErrorBytes)
	require.True(t, utf8.ValidString(bounded))
}

func newOutboxDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(127.0.0.1:3306)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	require.NoError(t, err)
	return db
}
