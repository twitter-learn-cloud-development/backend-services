package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestFeedRecoveryLeaseExpiryAndTokenFencing(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	c := NewTimelineCache(client)
	ctx := context.Background()
	first, ok, err := c.TryBeginFeedRecovery(ctx, 1)
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = c.TryBeginFeedRecovery(ctx, 1)
	require.NoError(t, err)
	require.False(t, ok)
	server.FastForward(4 * time.Second)
	second, ok, err := c.TryBeginFeedRecovery(ctx, 1)
	require.NoError(t, err)
	require.True(t, ok)
	committed, err := c.CommitFeedRecovery(ctx, 1, first, []uint64{100})
	require.NoError(t, err)
	require.False(t, committed)
	require.NoError(t, c.ReleaseFeedRecovery(ctx, 1, first))
	require.Equal(t, second, client.Get(ctx, "timeline:1:rebuild:v2").Val())
	require.NoError(t, c.AddToTimeline(ctx, 1, 102))
	committed, err = c.CommitFeedRecovery(ctx, 1, second, []uint64{101, 100})
	require.NoError(t, err)
	require.True(t, committed)
	ids, err := c.GetTimeline(ctx, 1, 0, 20)
	require.NoError(t, err)
	require.Equal(t, []uint64{102, 101, 100}, ids)
	require.False(t, server.Exists("timeline:1:rebuild:v2"))
	require.Greater(t, server.TTL("timeline:1:initialized:v2"), time.Duration(0))
	_, ok, err = c.TryBeginFeedRecovery(ctx, 1)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, c.ClearTimeline(ctx, 1))
	_, ok, err = c.TryBeginFeedRecovery(ctx, 1)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestFeedRecoveryEmptyStateAndInvalidCommit(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	c := NewTimelineCache(client)
	ctx := context.Background()
	token, ok, err := c.TryBeginFeedRecovery(ctx, 1)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = c.CommitFeedRecovery(ctx, 1, token, []uint64{0})
	require.Error(t, err)
	require.False(t, server.Exists("timeline:1:initialized:v2"))
	_, err = c.CommitFeedRecovery(ctx, 1, token, []uint64{100, 100})
	require.Error(t, err)
	committed, err := c.CommitFeedRecovery(ctx, 1, token, nil)
	require.NoError(t, err)
	require.True(t, committed)
	require.Equal(t, "empty", client.Get(ctx, "timeline:1:initialized:v2").Val())
	_, ok, err = c.TryBeginFeedRecovery(ctx, 1)
	require.NoError(t, err)
	require.False(t, ok)
	server.FastForward(301 * time.Second)
	_, ok, err = c.TryBeginFeedRecovery(ctx, 1)
	require.NoError(t, err)
	require.True(t, ok)
}
