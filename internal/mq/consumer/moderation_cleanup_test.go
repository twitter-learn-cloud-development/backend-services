package consumer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	tweetcache "twitter-clone/internal/module/tweet/cache"
)

type moderationFollowerPager struct {
	pages map[uint64]domain.FollowerPage
	calls []uint64
}

func (p *moderationFollowerPager) ListFollowerPage(_ context.Context, _ uint64, cursor uint64, _ int) (domain.FollowerPage, error) {
	p.calls = append(p.calls, cursor)
	page, ok := p.pages[cursor]
	if !ok {
		return domain.FollowerPage{}, fmt.Errorf("unexpected cursor %d", cursor)
	}
	return page, nil
}

func TestCleanupModeratedTweetPaginatesAndSuppressesCompletedReplay(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	defer server.Close()

	ctx := context.Background()
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer redisClient.Close()
	timelineCache := tweetcache.NewTimelineCache(redisClient)

	const tweetID = uint64(900)
	const authorID = uint64(42)
	followers := []uint64{101, 102, 103}
	for _, followerID := range followers {
		require.NoError(t, redisClient.ZAdd(ctx, fmt.Sprintf("timeline:%d", followerID), &redis.Z{
			Score: float64(tweetID), Member: tweetID,
		}).Err())
		require.NoError(t, redisClient.Set(ctx, fmt.Sprintf("unread:timeline:%d", followerID), "1", 0).Err())
	}

	pager := &moderationFollowerPager{pages: map[uint64]domain.FollowerPage{
		0:  {FollowerIDs: followers[:2], NextCursor: 90, HasMore: true},
		90: {FollowerIDs: followers[2:]},
	}}
	consumer := &TimelineConsumer{
		followerPager: pager,
		timelineCache: timelineCache,
		redisClient:   redisClient,
	}
	event := events.NewTweetModeratedEvent(tweetID, authorID, events.TweetModerationShadowban, time.Now().UnixMilli())

	stats, err := consumer.cleanupModeratedTweet(ctx, event)
	require.NoError(t, err)
	require.Equal(t, 2, stats.Pages)
	require.Equal(t, 3, stats.FollowersScanned)
	require.Equal(t, 3, stats.TimelinesRemoved)
	require.Equal(t, []uint64{0, 90}, pager.calls)

	for _, followerID := range followers {
		unread, getErr := redisClient.Get(ctx, fmt.Sprintf("unread:timeline:%d", followerID)).Result()
		require.NoError(t, getErr)
		require.Equal(t, "0", unread)
	}
	done, err := server.Get(moderationCompletionKeyPrefix + event.EventKey)
	require.NoError(t, err)
	require.Equal(t, "1", done)
	require.False(t, server.Exists(moderationProgressKeyPrefix+event.EventKey))

	replayed, err := consumer.cleanupModeratedTweet(ctx, event)
	require.NoError(t, err)
	require.True(t, replayed.AlreadyCompleted)
	require.Equal(t, []uint64{0, 90}, pager.calls)
}

func TestCleanupModeratedTweetResumesFromPersistedRelationCursor(t *testing.T) {
	server, err := miniredis.Run()
	require.NoError(t, err)
	defer server.Close()

	ctx := context.Background()
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer redisClient.Close()
	timelineCache := tweetcache.NewTimelineCache(redisClient)
	event := events.NewTweetModeratedEvent(901, 43, events.TweetModerationShadowban, time.Now().UnixMilli())
	require.NoError(t, redisClient.Set(ctx, moderationProgressKeyPrefix+event.EventKey, "90", moderationProgressTTL).Err())

	pager := &moderationFollowerPager{pages: map[uint64]domain.FollowerPage{
		90: {FollowerIDs: []uint64{104}},
	}}
	consumer := &TimelineConsumer{followerPager: pager, timelineCache: timelineCache, redisClient: redisClient}
	stats, err := consumer.cleanupModeratedTweet(ctx, event)
	require.NoError(t, err)
	require.Equal(t, []uint64{90}, pager.calls)
	require.Equal(t, 1, stats.FollowersScanned)
}
