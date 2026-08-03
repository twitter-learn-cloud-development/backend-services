package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	tweetcache "twitter-clone/internal/module/tweet/cache"
)

type tweetCreatedFollowRepositoryFake struct {
	followers []uint64
}

func (*tweetCreatedFollowRepositoryFake) Follow(context.Context, uint64, uint64) error { return nil }
func (*tweetCreatedFollowRepositoryFake) Unfollow(context.Context, uint64, uint64) error {
	return nil
}
func (*tweetCreatedFollowRepositoryFake) IsFollowing(context.Context, uint64, uint64) (bool, error) {
	return false, nil
}
func (*tweetCreatedFollowRepositoryFake) GetFollowers(context.Context, uint64, uint64, int) ([]uint64, error) {
	return nil, nil
}
func (*tweetCreatedFollowRepositoryFake) GetFollowees(context.Context, uint64, uint64, int) ([]uint64, error) {
	return nil, nil
}
func (*tweetCreatedFollowRepositoryFake) GetFollowerCount(context.Context, uint64) (int64, error) {
	return 0, nil
}
func (*tweetCreatedFollowRepositoryFake) GetFolloweeCount(context.Context, uint64) (int64, error) {
	return 0, nil
}
func (r *tweetCreatedFollowRepositoryFake) GetActiveFollowers(context.Context, uint64, int) ([]uint64, error) {
	return append([]uint64(nil), r.followers...), nil
}
func (*tweetCreatedFollowRepositoryFake) GetCelebrities(context.Context, int64) ([]uint64, error) {
	return nil, nil
}

type tweetCreatedOutboxRepositoryFake struct {
	byDedupKey map[string]*domain.OutboxTask
	operations *[]string
}

func (r *tweetCreatedOutboxRepositoryFake) Create(ctx context.Context, task *domain.OutboxTask) error {
	_, err := r.CreateIdempotent(ctx, task)
	return err
}

func (r *tweetCreatedOutboxRepositoryFake) CreateIdempotent(_ context.Context, task *domain.OutboxTask) (bool, error) {
	if r.operations != nil {
		*r.operations = append(*r.operations, "outbox")
	}
	if task == nil || task.DedupKey == nil {
		return false, errors.New("dedup key required")
	}
	if r.byDedupKey == nil {
		r.byDedupKey = make(map[string]*domain.OutboxTask)
	}
	if _, exists := r.byDedupKey[*task.DedupKey]; exists {
		return false, nil
	}
	copyTask := *task
	r.byDedupKey[*task.DedupKey] = &copyTask
	return true, nil
}

func (*tweetCreatedOutboxRepositoryFake) Claim(context.Context, domain.OutboxClaimRequest) ([]*domain.OutboxTask, error) {
	return nil, nil
}
func (*tweetCreatedOutboxRepositoryFake) CompleteClaim(context.Context, domain.OutboxClaimCompletion) (bool, error) {
	return false, nil
}
func (*tweetCreatedOutboxRepositoryFake) FailClaim(context.Context, domain.OutboxClaimFailure) (bool, error) {
	return false, nil
}
func (*tweetCreatedOutboxRepositoryFake) RecoverExpiredClaims(context.Context, int64, int) (domain.OutboxLeaseRecovery, error) {
	return domain.OutboxLeaseRecovery{}, nil
}
func (*tweetCreatedOutboxRepositoryFake) Delete(context.Context, uint64) error { return nil }
func (*tweetCreatedOutboxRepositoryFake) DeleteCompletedBefore(context.Context, int64, int) (int64, error) {
	return 0, nil
}

type tweetCreatedObserverRecording struct {
	results []string
}

func (o *tweetCreatedObserverRecording) ObserveStage(stage, result string) {
	o.results = append(o.results, stage+":"+result)
}

func TestHandleFanoutMessageReplaysWithoutDuplicatingDerivedState(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })

	operations := make([]string, 0, 4)
	outboxRepo := &tweetCreatedOutboxRepositoryFake{operations: &operations}
	observer := &tweetCreatedObserverRecording{}
	nextID := uint64(500)
	consumer := &TimelineConsumer{
		followRepo:           &tweetCreatedFollowRepositoryFake{followers: []uint64{11}},
		timelineCache:        tweetcache.NewTimelineCache(redisClient),
		redisClient:          redisClient,
		outboxRepo:           outboxRepo,
		trendProjector:       newTweetCreatedTrendProjector(redisClient),
		tweetCreatedObserver: observer,
		newOutboxTaskID: func() (uint64, error) {
			nextID++
			return nextID, nil
		},
	}
	event := events.TweetCreatedEvent{TweetID: 400, AuthorID: 10, Content: "hello #Golang"}
	body, err := json.Marshal(event)
	require.NoError(t, err)

	for deliveryTag := uint64(1); deliveryTag <= 2; deliveryTag++ {
		acknowledger := &timelineFailureAcknowledgerFake{operations: &operations}
		consumer.handleFanoutMessage(amqp.Delivery{
			Acknowledger: acknowledger,
			DeliveryTag:  deliveryTag,
			Body:         body,
		})
		require.Equal(t, 1, acknowledger.acked)
		require.Zero(t, acknowledger.nacked)
	}

	require.Equal(t, []string{"outbox", "ack", "outbox", "ack"}, operations)
	require.Len(t, outboxRepo.byDedupKey, 1)
	require.Contains(t, outboxRepo.byDedupKey, "timeline:sync_es:tweet:400:v1")
	score, err := redisClient.ZScore(context.Background(), "trends:global", "golang").Result()
	require.NoError(t, err)
	require.Equal(t, float64(30), score)
	cardinality, err := redisClient.ZCard(context.Background(), fmt.Sprintf("timeline:%d", 11)).Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), cardinality)
	require.Contains(t, observer.results, "trends:duplicate")
	require.Contains(t, observer.results, "sync_es_outbox:duplicate")
}
