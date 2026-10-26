package projection

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
	"twitter-clone/internal/domain"
)

func redisFixture(t *testing.T) (*RedisCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return &RedisCache{Client: client}, server
}
func TestFollowRedisEpochAndServerLeaseFenceRejectLateBeginAndWrites(t *testing.T) {
	c, server := redisFixture(t)
	ctx := context.Background()
	now := time.Now()
	server.SetTime(now)
	old := Fence{Epoch: 1, Token: "old", Until: now.Add(time.Minute).UnixMilli()}
	fresh := Fence{Epoch: 2, Token: "fresh", Until: old.Until}
	require.NoError(t, c.Begin(ctx, 42, AuthorState{AuthorID: 42, Celebrity: true}, old))
	require.NoError(t, c.Follower(ctx, 42, 17, true, old))
	require.True(t, c.Client.SIsMember(ctx, "user:celebrities:17", "42").Val())
	require.NoError(t, c.Begin(ctx, 42, AuthorState{AuthorID: 42, Celebrity: false}, fresh))
	require.NoError(t, c.Follower(ctx, 42, 17, false, fresh))
	require.Error(t, c.Begin(ctx, 42, AuthorState{AuthorID: 42, Celebrity: true}, old))
	require.Error(t, c.Follower(ctx, 42, 17, true, old))
	require.False(t, c.Client.SIsMember(ctx, "global:celebrities", "42").Val())
	require.False(t, c.Client.SIsMember(ctx, "user:celebrities:17", "42").Val())
	server.SetTime(now.Add(2 * time.Minute))
	require.Error(t, c.PairTweets(ctx, 42, 17, true, []TweetRow{{ID: 9007199254740997}}, fresh))
	require.Error(t, c.Begin(ctx, 42, AuthorState{AuthorID: 42}, fresh))
	require.False(t, c.Client.Exists(ctx, "timeline:17").Val() > 0)
}
func TestFollowRedisVisibilityRemovalUnreadAndRecoveryMarkersAreIdempotent(t *testing.T) {
	c, _ := redisFixture(t)
	ctx := context.Background()
	f := Fence{Epoch: 1, Token: "token", Until: time.Now().Add(time.Minute).UnixMilli()}
	require.NoError(t, c.Begin(ctx, 42, AuthorState{AuthorID: 42}, f))
	require.NoError(t, c.Client.Set(ctx, "unread:timeline:17", 10, time.Hour).Err())
	require.NoError(t, c.Client.Set(ctx, "timeline:17:initialized:v2", "filled", time.Minute).Err())
	require.NoError(t, c.Client.Set(ctx, "timeline:17:rebuild:v2", "old", time.Second).Err())
	rows := []TweetRow{{ID: 9007199254740997, VisibleType: domain.VisibleFollows}, {ID: 4, VisibleType: domain.VisiblePrivate}, {ID: 3, DeletedAt: 123}}
	require.NoError(t, c.PairTweets(ctx, 42, 17, true, rows, f))
	require.Equal(t, []string{"9007199254740997"}, c.Client.ZRange(ctx, "timeline:17", 0, -1).Val())
	require.Equal(t, int64(0), c.Client.Exists(ctx, "timeline:17:initialized:v2", "timeline:17:rebuild:v2").Val())
	require.NoError(t, c.PairTweets(ctx, 42, 17, false, rows, f))
	require.NoError(t, c.PairTweets(ctx, 42, 17, false, rows, f))
	require.Equal(t, "9", c.Client.Get(ctx, "unread:timeline:17").Val())
	require.Greater(t, c.Client.TTL(ctx, "unread:timeline:17").Val(), time.Minute)
}
func TestFollowRedisWrongTypeDoesNotAdvanceOrWriteTimeline(t *testing.T) {
	c, _ := redisFixture(t)
	ctx := context.Background()
	f := Fence{Epoch: 1, Token: "token", Until: time.Now().Add(time.Minute).UnixMilli()}
	require.NoError(t, c.Begin(ctx, 42, AuthorState{AuthorID: 42}, f))
	require.NoError(t, c.Client.Set(ctx, "unread:timeline:17", "broken", time.Hour).Err())
	require.Error(t, c.PairTweets(ctx, 42, 17, true, []TweetRow{{ID: 1}}, f))
	require.Zero(t, c.Client.Exists(ctx, "timeline:17").Val())
}
