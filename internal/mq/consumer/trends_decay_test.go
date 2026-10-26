package consumer

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestTrendsDecayReplicaBucketAndTTL(t *testing.T) {
	s, r, _ := engagementFixture(t)
	ctx := context.Background()
	now := time.Unix(1800000000, 0)
	s.SetTime(now)
	require.NoError(t, r.ZAdd(ctx, "trends:global", &redis.Z{Score: 100, Member: "golang"}).Err())
	require.NoError(t, r.Expire(ctx, "trends:global", 2*time.Hour).Err())
	var wg sync.WaitGroup
	results := make(chan string, 32)
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); outcome, err := decayTrendBucket(ctx, r); results <- outcome; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	applied := 0
	for err := range errs {
		require.NoError(t, err)
	}
	for result := range results {
		if result == "applied" {
			applied++
		} else {
			require.Equal(t, "duplicate", result)
		}
	}
	require.Equal(t, 1, applied)
	require.InDelta(t, 95, r.ZScore(ctx, "trends:global", "golang").Val(), 0.0001)
	require.Equal(t, 2*time.Hour, s.TTL("trends:global"))
	// A replica restart after an uncertain response observes the committed bucket.
	outcome, err := decayTrendBucket(ctx, r)
	require.NoError(t, err)
	require.Equal(t, "duplicate", outcome)
	s.SetTime(now.Add(51 * time.Second))
	outcome, err = decayTrendBucket(ctx, r)
	require.NoError(t, err)
	require.Equal(t, "duplicate", outcome) // old 50s lease would expire.
	s.SetTime(now.Add(time.Minute))
	outcome, err = decayTrendBucket(ctx, r)
	require.NoError(t, err)
	require.Equal(t, "applied", outcome)
	require.InDelta(t, 90.25, r.ZScore(ctx, "trends:global", "golang").Val(), 0.0001)
	s.SetTime(now.Add(10 * time.Minute))
	outcome, err = decayTrendBucket(ctx, r)
	require.NoError(t, err)
	require.Equal(t, "applied", outcome)
	require.InDelta(t, 85.7375, r.ZScore(ctx, "trends:global", "golang").Val(), 0.0001) // missed buckets are skipped.
	s.SetTime(now)
	outcome, err = decayTrendBucket(ctx, r)
	require.Error(t, err)
	require.Equal(t, "clock_regression", outcome)
}
func TestTrendsDecayKeepsOriginalTopAndRepairsMissingTTL(t *testing.T) {
	s, r, _ := engagementFixture(t)
	ctx := context.Background()
	for i := 0; i < 101; i++ {
		require.NoError(t, r.ZAdd(ctx, "trends:global", &redis.Z{Score: float64(1000 + i), Member: fmt.Sprintf("topic%d", i)}).Err())
	}
	result, err := decayTrendBucket(ctx, r)
	require.NoError(t, err)
	require.Equal(t, "applied", result)
	require.Equal(t, int64(100), r.ZCard(ctx, "trends:global").Val())
	require.ErrorIs(t, r.ZScore(ctx, "trends:global", "topic0").Err(), redis.Nil)
	require.InDelta(t, 1001*0.95, r.ZScore(ctx, "trends:global", "topic1").Val(), 0.0001)
	require.Equal(t, 24*time.Hour, s.TTL("trends:global"))
}
func TestTrendsDecayRejectsInvalidOrOversizedStateBeforeWrites(t *testing.T) {
	for _, kind := range []string{"wrong_global", "wrong_marker", "bad_marker", "bad_score", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			s, r, _ := engagementFixture(t)
			ctx := context.Background()
			require.NoError(t, r.ZAdd(ctx, "trends:global", &redis.Z{Score: 100, Member: "valid"}).Err())
			require.NoError(t, r.Expire(ctx, "trends:global", time.Hour).Err())
			switch kind {
			case "wrong_global":
				require.NoError(t, r.Del(ctx, "trends:global").Err())
				require.NoError(t, r.Set(ctx, "trends:global", "invalid", time.Hour).Err())
			case "wrong_marker":
				require.NoError(t, r.LPush(ctx, "trends:decay:bucket:v1", "bad").Err())
			case "bad_marker":
				require.NoError(t, r.Set(ctx, "trends:decay:bucket:v1", "1.2", 0).Err())
			case "bad_score":
				require.NoError(t, r.ZAdd(ctx, "trends:global", &redis.Z{Score: 1e14, Member: "bad"}).Err())
			case "oversized":
				members := []*redis.Z{}
				for i := 0; i < trendsDecayMaxMembers; i++ {
					members = append(members, &redis.Z{Score: 1, Member: fmt.Sprintf("t%d", i)})
				}
				require.NoError(t, r.ZAdd(ctx, "trends:global", members...).Err())
			}
			_, err := decayTrendBucket(ctx, r)
			require.Error(t, err)
			if kind != "wrong_global" {
				require.Equal(t, float64(100), r.ZScore(ctx, "trends:global", "valid").Val())
			}
			if kind != "wrong_marker" && kind != "bad_marker" {
				require.False(t, s.Exists("trends:decay:bucket:v1"))
			}
			require.Equal(t, time.Hour, s.TTL("trends:global"))
		})
	}
}
func TestTrendsDecayEmptyCancellationAndMetrics(t *testing.T) {
	s, r, _ := engagementFixture(t)
	result, err := decayTrendBucket(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, "empty", result)
	require.Equal(t, 7*24*time.Hour, s.TTL("trends:decay:bucket:v1"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = decayTrendBucket(ctx, r)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "cancelled", result)
	reg := prometheus.NewPedanticRegistry()
	m, err := NewTrendsDecayMetrics(reg)
	require.NoError(t, err)
	m.Observe("private ID", time.Millisecond)
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				require.Equal(t, "result", label.GetName())
				require.Equal(t, "unknown", label.GetValue())
			}
		}
	}
}
