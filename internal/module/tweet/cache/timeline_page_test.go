package cache

import (
	"context"
	"math"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestTimelineExactCursorAcrossScoreTies(t *testing.T) {
	for _, base := range []uint64{9007199254741010, 10000000000000000000, math.MaxUint64} {
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: server.Addr()})
		t.Cleanup(func() { _ = client.Close() })
		c := NewTimelineCache(client)
		for i := uint64(0); i < 12; i++ {
			require.NoError(t, c.AddToTimeline(context.Background(), 1, base-i))
		}
		cursor := uint64(0)
		for i := uint64(0); i < 12; i++ {
			ids, err := c.GetTimeline(context.Background(), 1, cursor, 1)
			require.NoError(t, err)
			require.Equal(t, []uint64{base - i}, ids)
			cursor = ids[0]
		}
		ids, err := c.GetTimeline(context.Background(), 1, cursor, 1)
		require.NoError(t, err)
		require.Empty(t, ids)
		// Cursor need not itself still exist in the ZSet.
		require.NoError(t, client.ZRem(context.Background(), "timeline:1", base-5).Err())
		ids, err = c.GetTimeline(context.Background(), 1, base-5, 2)
		require.NoError(t, err)
		require.Equal(t, []uint64{base - 6, base - 7}, ids)
	}
}

func TestCelebrityPipelineExactIDsAndEmptyInitialization(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	c := NewTimelineCache(client)
	ctx := context.Background()
	ids := []uint64{9007199254740997, 9007199254740996, 9007199254740995}
	require.NoError(t, c.RebuildUserTimeline(ctx, 2, ids))
	require.NoError(t, c.RebuildUserTimeline(ctx, 3, nil))
	pages, missing, err := c.PipelineGetCelebrityTweets(ctx, []uint64{2, 3, 4}, ids[0], 2)
	require.NoError(t, err)
	require.Equal(t, ids[1:], pages[2])
	require.Empty(t, pages[3])
	require.Equal(t, []uint64{4}, missing)
	require.NoError(t, client.ZAdd(ctx, "timeline:1", &redis.Z{Score: 1, Member: "bad"}).Err())
	_, err = c.GetTimeline(ctx, 1, 0, 20)
	require.Error(t, err)
}
