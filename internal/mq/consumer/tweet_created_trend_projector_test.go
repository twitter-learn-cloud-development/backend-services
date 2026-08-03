package consumer

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestTweetCreatedTrendProjectorSuppressesReplay(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	projector := newTweetCreatedTrendProjector(client)
	ctx := context.Background()

	applied, err := projector.Project(ctx, 101, 7, map[string]int64{
		"Golang":      30,
		"cloudnative": 10,
	})
	require.NoError(t, err)
	require.True(t, applied)

	applied, err = projector.Project(ctx, 101, 7, map[string]int64{
		"cloudnative": 10,
		"Golang":      30,
	})
	require.NoError(t, err)
	require.False(t, applied)

	golangScore, err := client.ZScore(ctx, "trends:global", "golang").Result()
	require.NoError(t, err)
	require.Equal(t, float64(30), golangScore)
	cloudScore, err := client.ZScore(ctx, "trends:global", "cloudnative").Result()
	require.NoError(t, err)
	require.Equal(t, float64(10), cloudScore)
	tags, err := client.Get(ctx, "tweet_tags:101").Result()
	require.NoError(t, err)
	require.Equal(t, "golang,cloudnative", tags)
	count, err := server.Get("lock:user_tag_count:7:golang")
	require.NoError(t, err)
	require.Equal(t, "1", count)
	require.True(t, server.Exists("idempotency:timeline:tweet_created:trends:v1:101"))
}

func TestTweetCreatedTrendProjectorAppliesAuthorRateLimit(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	projector := newTweetCreatedTrendProjector(client)
	ctx := context.Background()

	for tweetID := uint64(201); tweetID <= 204; tweetID++ {
		applied, err := projector.Project(ctx, tweetID, 8, map[string]int64{"golang": 30})
		require.NoError(t, err)
		require.True(t, applied)
	}

	score, err := client.ZScore(ctx, "trends:global", "golang").Result()
	require.NoError(t, err)
	require.Equal(t, float64(90), score)
	count, err := server.Get("lock:user_tag_count:8:golang")
	require.NoError(t, err)
	require.Equal(t, "4", count)
}

func TestTweetCreatedTrendProjectorRejectsWrongTypesBeforeWriting(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	projector := newTweetCreatedTrendProjector(client)
	ctx := context.Background()
	require.NoError(t, client.Set(ctx, "trends:global", "invalid", 0).Err())

	applied, err := projector.Project(ctx, 301, 9, map[string]int64{"golang": 30})
	require.Error(t, err)
	require.False(t, applied)
	require.False(t, server.Exists("tweet_tags:301"))
	require.False(t, server.Exists("lock:user_tag_count:9:golang"))
	require.False(t, server.Exists("idempotency:timeline:tweet_created:trends:v1:301"))
}

func TestCanonicalTrendTopicsIsDeterministicAndBounded(t *testing.T) {
	topics := make(map[string]int64, maxTweetCreatedTrendTopics+5)
	for index := 0; index < maxTweetCreatedTrendTopics+5; index++ {
		topics[fmt.Sprintf("topic-%02d", index)] = int64(index + 1)
	}
	topics[" GOLANG "] = 5
	topics["golang"] = 30

	ordered := canonicalTrendTopics(topics)
	require.Len(t, ordered, maxTweetCreatedTrendTopics)
	require.Equal(t, "topic-36", ordered[0].name)
	require.Equal(t, int64(37), ordered[0].score)
	for index := 1; index < len(ordered); index++ {
		require.GreaterOrEqual(t, ordered[index-1].score, ordered[index].score)
	}
}
