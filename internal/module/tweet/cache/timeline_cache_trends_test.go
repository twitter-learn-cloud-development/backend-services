package cache

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestGetTrendingTopicsSkipsDecayedScores(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	require.NoError(t, client.ZAdd(ctx, "trends:global",
		&redis.Z{Member: "stale", Score: 0.00001},
		&redis.Z{Member: "active", Score: 1.4},
		&redis.Z{Member: "hot", Score: 2.6},
	).Err())

	topics, err := NewTimelineCache(client).GetTrendingTopics(ctx, 2)
	require.NoError(t, err)
	require.Len(t, topics, 2)
	require.Equal(t, "hot", topics[0].Topic)
	require.EqualValues(t, 3, topics[0].Score)
	require.Equal(t, "active", topics[1].Topic)
	require.EqualValues(t, 1, topics[1].Score)
}

func TestGetTrendingTopicsEmptyForStaleOnly(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	require.NoError(t, client.ZAdd(ctx, "trends:global", &redis.Z{Member: "old", Score: 0.95}).Err())

	topics, err := NewTimelineCache(client).GetTrendingTopics(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, topics)
}
