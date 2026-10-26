package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"testing"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

func engagementFixture(t *testing.T) (*miniredis.Miniredis, *redis.Client, *engagementProjector) {
	t.Helper()
	s := miniredis.RunT(t)
	r := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	return s, r, &engagementProjector{redis: r}
}
func engagementDelivery(t *testing.T, id uint64, kind string) amqp.Delivery {
	t.Helper()
	var record interface{}
	if kind == RoutingKeyTweetLiked {
		record = events.TweetLikedEvent{TweetID: 91, UserID: 7, TweetUser: 42, LikeID: id, EventKey: events.LikeEventKey(id), SchemaVersion: events.EngagementSchemaVersion, OccurredAtUnixMS: time.Now().UnixMilli()}
	} else {
		record = events.CommentCreatedEvent{TweetID: 91, UserID: 7, TweetUser: 42, CommentID: id, EventKey: events.CommentEventKey(id), SchemaVersion: events.EngagementSchemaVersion, OccurredAtUnixMS: time.Now().UnixMilli()}
	}
	body, err := json.Marshal(record)
	require.NoError(t, err)
	return amqp.Delivery{Body: body, DeliveryTag: 1}
}
func engagementFor(t *testing.T, id uint64, kind string) engagementEvent {
	t.Helper()
	e, err := decodeEngagement(engagementDelivery(t, id, kind), kind)
	require.NoError(t, err)
	return e
}

func TestEngagementConcurrentDuplicateAndAckLoss(t *testing.T) {
	s, r, p := engagementFixture(t)
	ctx := context.Background()
	require.NoError(t, r.Set(ctx, "tweet_tags:91", "golang", 48*time.Hour).Err())
	event := engagementFor(t, 1, RoutingKeyTweetLiked)
	var wg sync.WaitGroup
	results := make(chan string, 32)
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); result, err := p.Project(ctx, event, true); results <- result; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	applied := 0
	for result := range results {
		if result == "applied" {
			applied++
		} else {
			require.Equal(t, "duplicate", result)
		}
	}
	require.Equal(t, 1, applied)
	score, err := r.ZScore(ctx, "trends:global", "golang").Result()
	require.NoError(t, err)
	require.Equal(t, float64(2), score)
	require.Equal(t, time.Hour, s.TTL("lock:user_tag_count:7:golang"))
	count, err := s.Get("lock:user_tag_count:7:golang")
	require.NoError(t, err)
	require.Equal(t, "1", count)
	// Redis committed before the network ACK is lost. Redelivery cannot count twice.
	delivery := engagementDelivery(t, 1, RoutingKeyTweetLiked)
	body, _ := json.Marshal(events.TweetLikedEvent{TweetID: 91, UserID: 7, TweetUser: 42, LikeID: 1, EventKey: events.LikeEventKey(1), SchemaVersion: events.EngagementSchemaVersion, OccurredAtUnixMS: event.Occurred})
	delivery.Body = body
	ack := &timelineFailureAcknowledgerFake{ackErr: errors.New("closed")}
	delivery.Acknowledger = ack
	c := &TimelineConsumer{engagementExecutor: func(ctx context.Context, e engagementEvent) (string, error) { return p.Project(ctx, e, true) }}
	c.handleEngagementMessage(ctx, delivery, RoutingKeyTweetLiked)
	require.Equal(t, 1, ack.acked)
	ack.ackErr = nil
	c.handleEngagementMessage(ctx, delivery, RoutingKeyTweetLiked)
	score, err = r.ZScore(ctx, "trends:global", "golang").Result()
	require.NoError(t, err)
	require.Equal(t, float64(2), score)
}
func TestEngagementSharedRateLimitAndAtomicValidation(t *testing.T) {
	s, r, p := engagementFixture(t)
	ctx := context.Background()
	require.NoError(t, r.Set(ctx, "tweet_tags:91", "alpha,beta", 48*time.Hour).Err())
	// A later bad topic must not leave the first topic incremented.
	require.NoError(t, r.Set(ctx, "lock:user_tag_count:7:beta", "1.0", 0).Err())
	e := engagementFor(t, 1, RoutingKeyTweetLiked)
	_, err := p.Project(ctx, e, true)
	require.Error(t, err)
	require.False(t, s.Exists("lock:user_tag_count:7:alpha"))
	require.False(t, s.Exists("trends:global"))
	require.NoError(t, r.Del(ctx, "lock:user_tag_count:7:beta").Err())
	for i, kind := range []string{RoutingKeyTweetLiked, RoutingKeyCommentCreated, RoutingKeyTweetLiked, RoutingKeyCommentCreated} {
		result, err := p.Project(ctx, engagementFor(t, uint64(i+1), kind), true)
		require.NoError(t, err)
		if i == 3 {
			require.Equal(t, "limited", result)
		} else {
			require.Equal(t, "applied", result)
		}
	}
	for _, topic := range []string{"alpha", "beta"} {
		score, err := r.ZScore(ctx, "trends:global", topic).Result()
		require.NoError(t, err)
		require.Equal(t, float64(9), score)
		count, err := s.Get("lock:user_tag_count:7:" + topic)
		require.NoError(t, err)
		require.Equal(t, "3", count)
		require.Equal(t, time.Hour, s.TTL("lock:user_tag_count:7:"+topic))
	}
}
func TestEngagementOrderingVisibilityIdentityAndAge(t *testing.T) {
	_, r, p := engagementFixture(t)
	ctx := context.Background()
	e := engagementFor(t, 4, RoutingKeyTweetLiked)
	_, err := p.Project(ctx, e, true)
	require.Error(t, err) // Creation projection has not arrived.
	require.NoError(t, r.Set(ctx, "tweet_tags:91", "golang", 48*time.Hour).Err())
	result, err := p.Project(ctx, e, true)
	require.NoError(t, err)
	require.Equal(t, "applied", result)
	altered := e
	altered.UserID++
	altered.Digest = fmt.Sprintf("%064d", 9)
	_, err = p.Project(ctx, altered, true)
	require.ErrorIs(t, err, errEngagementIdentityConflict)
	hidden := engagementFor(t, 5, RoutingKeyTweetLiked)
	result, err = p.Project(ctx, hidden, false)
	require.NoError(t, err)
	require.Equal(t, "no_topics", result)
	result, err = p.Project(ctx, hidden, true)
	require.NoError(t, err)
	require.Equal(t, "duplicate", result)
	require.NoError(t, r.Del(ctx, "tweet_tags:91").Err())
	require.NoError(t, r.Set(ctx, "idempotency:timeline:tweet_created:trends:v1:91", "1", 72*time.Hour).Err())
	result, err = p.Project(ctx, engagementFor(t, 6, RoutingKeyTweetLiked), true)
	require.NoError(t, err)
	require.Equal(t, "no_topics", result)
	old := engagementFor(t, 7, RoutingKeyTweetLiked)
	old.Occurred = time.Now().Add(-49 * time.Hour).UnixMilli()
	result, err = p.Project(ctx, old, true)
	require.NoError(t, err)
	require.Equal(t, "expired", result)
	future := engagementFor(t, 8, RoutingKeyTweetLiked)
	future.Occurred = time.Now().Add(6 * time.Minute).UnixMilli()
	_, err = p.Project(ctx, future, true)
	require.ErrorIs(t, err, errEngagementInvalidTime)
	score, err := r.ZScore(ctx, "trends:global", "golang").Result()
	require.NoError(t, err)
	require.Equal(t, float64(2), score)
}
func TestEngagementRetiredMarkerCannotRescoreOldEvent(t *testing.T) {
	s, r, p := engagementFixture(t)
	ctx := context.Background()
	require.NoError(t, r.Set(ctx, "tweet_tags:91", "golang", 0).Err())
	e := engagementFor(t, 1, RoutingKeyTweetLiked)
	_, err := p.Project(ctx, e, true)
	require.NoError(t, err)
	s.FastForward(8 * 24 * time.Hour)
	s.SetTime(time.Now().Add(8 * 24 * time.Hour))
	result, err := p.Project(ctx, e, true)
	require.NoError(t, err)
	require.Equal(t, "expired", result)
	require.False(t, s.Exists("trends:global"))
}
func TestEngagementLegacyAndVersionedIdentity(t *testing.T) {
	legacy := amqp.Delivery{Body: []byte(`{"tweet_id":18446744073709551614,"user_id":7,"tweet_user_id":42,"occurred_at_unix_ms":1700000000000}`), MessageId: "first"}
	a, err := decodeEngagement(legacy, RoutingKeyTweetLiked)
	require.NoError(t, err)
	legacy.MessageId = "replayed"
	b, err := decodeEngagement(legacy, RoutingKeyTweetLiked)
	require.NoError(t, err)
	require.Equal(t, a.Key, b.Key)
	require.Equal(t, uint64(18446744073709551614), a.TweetID)
	for _, body := range []string{`null`, `{}`, `{"tweet_id":1,"user_id":7,"tweet_user_id":42}`, `{"tweet_id":1,"user_id":7,"tweet_user_id":42,"occurred_at_unix_ms":1,"schema_version":"engagement-v2"}`, `{"tweet_id":1,"user_id":7,"tweet_user_id":42,"occurred_at_unix_ms":1,"like_id":9}`} {
		_, err := decodeEngagement(amqp.Delivery{Body: []byte(body)}, RoutingKeyTweetLiked)
		require.Error(t, err)
	}
}

func TestEngagementFactGateAndTopicBound(t *testing.T) {
	for _, kind := range []string{"public", "hidden", "deleted", "missing", "old", "read_failure", "wrong_author"} {
		t.Run(kind, func(t *testing.T) {
			s, r, _ := engagementFixture(t)
			ctx := context.Background()
			require.NoError(t, r.Set(ctx, "tweet_tags:91", "golang", 48*time.Hour).Err())
			fact := &projectionFactFake{current: &domain.Tweet{ID: 91, UserID: 42, CreatedAt: time.Now().UnixMilli(), VisibleType: domain.VisiblePublic}}
			switch kind {
			case "hidden":
				fact.current.VisibleType = domain.VisibleShadowban
			case "deleted":
				fact.current.DeletedAt = 1
			case "missing":
				fact.current = nil
			case "old":
				fact.current.CreatedAt = time.Now().Add(-49 * time.Hour).UnixMilli()
			case "read_failure":
				fact.readErr = errors.New("unavailable")
			case "wrong_author":
				fact.current.UserID = 99
			}
			pub := &timelineFailurePublisherFake{}
			router, err := newTimelineFailureRouter(pub)
			require.NoError(t, err)
			ack := &timelineFailureAcknowledgerFake{}
			delivery := engagementDelivery(t, 1, RoutingKeyTweetLiked)
			delivery.Acknowledger = ack
			c := &TimelineConsumer{redisClient: r, projectionRepo: fact, failureRouter: router}
			c.handleEngagementMessage(ctx, delivery, RoutingKeyTweetLiked)
			require.Equal(t, 1, ack.acked)
			if kind == "public" {
				score, err := r.ZScore(ctx, "trends:global", "golang").Result()
				require.NoError(t, err)
				require.Equal(t, float64(2), score)
			} else {
				require.False(t, s.Exists("trends:global"))
			}
			if kind == "read_failure" || kind == "wrong_author" {
				require.Len(t, pub.published, 1)
			} else {
				require.Empty(t, pub.published)
			}
		})
	}
	s, r, p := engagementFixture(t)
	topics := []string{}
	for i := 0; i < 33; i++ {
		topics = append(topics, fmt.Sprintf("topic%d", i))
	}
	ctx := context.Background()
	require.NoError(t, r.Set(ctx, "tweet_tags:91", strings.Join(topics, ","), 0).Err())
	_, err := p.Project(ctx, engagementFor(t, 1, RoutingKeyTweetLiked), true)
	require.Error(t, err)
	require.False(t, s.Exists("trends:global"))
	require.NoError(t, r.Set(ctx, "tweet_tags:91", strings.Join(topics[:32], ","), 0).Err())
	result, err := p.Project(ctx, engagementFor(t, 1, RoutingKeyTweetLiked), true)
	require.NoError(t, err)
	require.Equal(t, "applied", result)
	require.Equal(t, int64(32), r.ZCard(ctx, "trends:global").Val())
}
func TestEngagementRecoveryConfirmsBeforeAckAndCancellation(t *testing.T) {
	for _, kind := range []string{RoutingKeyTweetLiked, RoutingKeyCommentCreated} {
		t.Run(kind, func(t *testing.T) {
			for _, permanent := range []bool{false, true} {
				ops := []string{}
				pub := &timelineFailurePublisherFake{operations: &ops}
				router, err := newTimelineFailureRouter(pub)
				require.NoError(t, err)
				router.wait = func(time.Duration) {}
				ack := &timelineFailureAcknowledgerFake{operations: &ops}
				delivery := engagementDelivery(t, 1, kind)
				delivery.Acknowledger = ack
				c := &TimelineConsumer{failureRouter: router, engagementExecutor: func(context.Context, engagementEvent) (string, error) {
					ops = append(ops, "project")
					if permanent {
						return "", errEngagementIdentityConflict
					}
					return "", errors.New("redis unavailable")
				}}
				c.handleEngagementMessage(context.Background(), delivery, kind)
				require.Equal(t, []string{"confirm", "project", "publish", "ack"}, ops)
				require.Len(t, pub.published, 1)
				route, _ := timelineRecoveryRouteForSource(kind)
				expected := route.retryRoutingKey
				if permanent {
					expected = kind + ".dlq"
				}
				require.Equal(t, expected, pub.published[0].routingKey)
				pub.publishErr = errors.New("confirm timeout")
				ack.acked = 0
				c.handleEngagementMessage(context.Background(), delivery, kind)
				require.Zero(t, ack.acked)
				require.True(t, ack.requeue)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			ack := &timelineFailureAcknowledgerFake{}
			delivery := engagementDelivery(t, 1, kind)
			delivery.Acknowledger = ack
			c := &TimelineConsumer{engagementExecutor: func(ctx context.Context, _ engagementEvent) (string, error) { return "", ctx.Err() }}
			c.handleEngagementMessage(ctx, delivery, kind)
			require.Zero(t, ack.acked)
			require.True(t, ack.requeue)
		})
	}
}
