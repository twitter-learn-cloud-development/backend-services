package handler

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"

	"twitter-clone/internal/module/supportquery"
)

func TestRedisSupportQueryIdempotencyGuardRejectsChangedRequest(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	guard := NewRedisSupportQueryIdempotencyGuard(client)

	require.NoError(t, guard.Check(
		context.Background(), 42, "call-1", "fingerprint-a", time.Minute,
	))
	require.NoError(t, guard.Check(
		context.Background(), 42, "call-1", "fingerprint-a", time.Minute,
	))
	require.ErrorIs(t, guard.Check(
		context.Background(), 42, "call-1", "fingerprint-b", time.Minute,
	), supportquery.ErrToolCallConflict)
}
