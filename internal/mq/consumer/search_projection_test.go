package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	tweetcache "twitter-clone/internal/module/tweet/cache"
)

type projectionFactFake struct {
	writer  sync.Mutex
	mu      sync.Mutex
	current *domain.Tweet
	readErr error
}

func (f *projectionFactFake) WithProjectionLock(ctx context.Context, _ uint64, fn func(context.Context) error) error {
	f.writer.Lock()
	defer f.writer.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(ctx)
}
func (f *projectionFactFake) ReadProjectionTweet(context.Context, uint64) (*domain.Tweet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	if f.current == nil {
		return nil, nil
	}
	copy := *f.current
	return &copy, nil
}
func (f *projectionFactFake) set(tweet *domain.Tweet) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = tweet
}

func TestSearchProjectionUsesCurrentBodyAndCleansIneligibleFacts(t *testing.T) {
	for _, kind := range []string{"public", "missing", "deleted", "private", "follows", "shadowban", "read_failure", "wrong_author"} {
		t.Run(kind, func(t *testing.T) {
			fact := &projectionFactFake{current: &domain.Tweet{ID: 9007199254740997, UserID: 42, Content: "current", CreatedAt: 123}}
			switch kind {
			case "missing":
				fact.current = nil
			case "deleted":
				fact.current.DeletedAt = 1234
			case "private":
				fact.current.VisibleType = domain.VisiblePrivate
			case "follows":
				fact.current.VisibleType = domain.VisibleFollows
			case "shadowban":
				fact.current.VisibleType = domain.VisibleShadowban
			case "read_failure":
				fact.readErr = errors.New("fixture read failure")
			case "wrong_author":
				fact.current.UserID = 99
			}
			indexed, cleaned := 0, 0
			c := &TimelineConsumer{projectionRepo: fact,
				projectionIndexExecutor: func(_ context.Context, event *events.TweetCreatedEvent) error {
					indexed++
					require.Equal(t, "current", event.Content)
					require.Equal(t, int64(123), event.CreatedAt)
					return nil
				},
				deleteESExecutor:     func(context.Context, uint64) error { cleaned++; return nil },
				deleteVectorExecutor: func(context.Context, uint64) error { cleaned++; return nil },
			}
			err := c.reconcileSearchProjection(context.Background(), &events.TweetCreatedEvent{TweetID: 9007199254740997, AuthorID: 42, Content: "old private event body"})
			if kind == "read_failure" || kind == "wrong_author" {
				require.Error(t, err)
				require.Zero(t, indexed)
				require.Zero(t, cleaned)
			} else {
				require.NoError(t, err)
				if kind == "public" {
					require.Equal(t, 1, indexed)
					require.Zero(t, cleaned)
				} else {
					require.Zero(t, indexed)
					require.Equal(t, 2, cleaned)
				}
			}
		})
	}
}

func TestProjectionDeletionAndLateCreationCannotReviveAcknowledgedWrites(t *testing.T) {
	fact := &projectionFactFake{current: &domain.Tweet{ID: 17, UserID: 42, Content: "current"}}
	started, release := make(chan struct{}), make(chan struct{})
	var esPresent, vectorPresent bool
	c := &TimelineConsumer{projectionRepo: fact,
		projectionIndexExecutor: func(context.Context, *events.TweetCreatedEvent) error {
			close(started)
			<-release
			esPresent, vectorPresent = true, true
			return nil
		},
		deleteESExecutor:     func(context.Context, uint64) error { esPresent = false; return nil },
		deleteVectorExecutor: func(context.Context, uint64) error { vectorPresent = false; return nil },
	}
	creationDone := make(chan error, 1)
	go func() {
		creationDone <- c.reconcileSearchProjection(context.Background(), &events.TweetCreatedEvent{TweetID: 17, AuthorID: 42, Content: "stale"})
	}()
	<-started
	fact.set(&domain.Tweet{ID: 17, UserID: 42, DeletedAt: 1})
	deleteDone := make(chan error, 1)
	go func() { deleteDone <- c.executeProjectionDeletion(context.Background(), 17, deleteESOutboxTaskType) }()
	close(release)
	require.NoError(t, <-creationDone)
	require.NoError(t, <-deleteDone)
	require.False(t, esPresent)
	require.False(t, vectorPresent)
	require.NoError(t, c.reconcileSearchProjection(context.Background(), &events.TweetCreatedEvent{TweetID: 17, AuthorID: 42}))
	require.False(t, esPresent)
	require.False(t, vectorPresent)
}

type partialDeletionEnqueueFake struct {
	tweetCreatedOutboxRepositoryFake
	failVector bool
}

func (f *partialDeletionEnqueueFake) CreateIdempotent(ctx context.Context, task *domain.OutboxTask) (bool, error) {
	if f.failVector && task.TaskType == deleteVectorOutboxTaskType {
		return false, errors.New("fixture vector enqueue failure")
	}
	return f.tweetCreatedOutboxRepositoryFake.CreateIdempotent(ctx, task)
}
func TestDeletionEnqueueReplaysPartialRegistrationWithoutDuplicatingES(t *testing.T) {
	repo := &partialDeletionEnqueueFake{failVector: true}
	id := uint64(100)
	c := &TimelineConsumer{outboxRepo: repo, newOutboxTaskID: func() (uint64, error) { id++; return id, nil }}
	event := events.TweetDeletedEvent{TweetID: 9007199254740997, AuthorID: 42}
	require.Error(t, c.enqueueProjectionDeletion(context.Background(), event))
	require.Len(t, repo.byDedupKey, 1)
	repo.failVector = false
	require.NoError(t, c.enqueueProjectionDeletion(context.Background(), event))
	require.NoError(t, c.enqueueProjectionDeletion(context.Background(), event))
	require.Len(t, repo.byDedupKey, 2)
	for _, task := range repo.byDedupKey {
		require.NotContains(t, task.Payload, "content")
		require.Equal(t, 5, task.MaxRetries)
	}
}

func TestDeletionWorkerRetriesVectorIndependentlyAndPreservesLostCompletion(t *testing.T) {
	esTask, vecTask := newSearchSyncOutboxTask(t, 101, 17), newSearchSyncOutboxTask(t, 102, 17)
	esTask.TaskType, vecTask.TaskType = deleteESOutboxTaskType, deleteVectorOutboxTaskType
	repo := &outboxWorkerRepositoryFake{claimable: []*domain.OutboxTask{esTask, vecTask}}
	c := newOutboxTestConsumer(repo, &recordingOutboxObserver{})
	c.projectionRepo = &projectionFactFake{current: &domain.Tweet{ID: 17, UserID: 44, DeletedAt: 1}}
	var mu sync.Mutex
	esCalls, vecCalls := 0, 0
	c.deleteESExecutor = func(context.Context, uint64) error { mu.Lock(); defer mu.Unlock(); esCalls++; return nil }
	c.deleteVectorExecutor = func(context.Context, uint64) error {
		mu.Lock()
		defer mu.Unlock()
		vecCalls++
		return errors.New("fixture provider body must not persist")
	}
	c.processOutboxTasks(context.Background())
	require.Len(t, repo.completions, 1)
	require.Equal(t, uint64(101), repo.completions[0].TaskID)
	require.Len(t, repo.failures, 1)
	require.Equal(t, uint64(102), repo.failures[0].TaskID)
	require.Equal(t, "projection execution failed", repo.failures[0].ErrorMsg)
	vecTask.Retries = 1
	repo.claimable = []*domain.OutboxTask{vecTask}
	repo.staleCompletion = true
	c.deleteVectorExecutor = func(context.Context, uint64) error { mu.Lock(); defer mu.Unlock(); vecCalls++; return nil }
	c.processOutboxTasks(context.Background())
	repo.claimable = []*domain.OutboxTask{vecTask}
	repo.staleCompletion = false
	c.processOutboxTasks(context.Background())
	require.Equal(t, 1, esCalls)
	require.Equal(t, 3, vecCalls)
	require.Len(t, repo.completions, 3)
}

func TestProjectionDependencyFailureDoesNotBecomeSuccess(t *testing.T) {
	c := &TimelineConsumer{}
	require.Error(t, c.reconcileSearchProjection(context.Background(), &events.TweetCreatedEvent{TweetID: 1, AuthorID: 2}))
	require.Error(t, c.executeESIndex(context.Background(), &events.TweetCreatedEvent{TweetID: 1, AuthorID: 2}))
	c.projectionRepo = &projectionFactFake{current: &domain.Tweet{ID: 1, UserID: 2}}
	require.ErrorContains(t, c.executeProjectionDeletion(context.Background(), 1, deleteESOutboxTaskType), "not current")
}

func TestDeletionMessageRegistersBothTasksBeforeSuccessfulAck(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	operations := []string{}
	repo := &partialDeletionEnqueueFake{}
	repo.operations = &operations
	ack := &timelineFailureAcknowledgerFake{operations: &operations}
	id := uint64(100)
	c := &TimelineConsumer{outboxRepo: repo, timelineCache: tweetcache.NewTimelineCache(client),
		followRepo: &tweetCreatedFollowRepositoryFake{}, newOutboxTaskID: func() (uint64, error) { id++; return id, nil }}
	body, err := json.Marshal(events.TweetDeletedEvent{TweetID: 17, AuthorID: 42})
	require.NoError(t, err)
	c.handleDeleteMessage(amqp.Delivery{Acknowledger: ack, Body: body})
	require.Equal(t, []string{"outbox", "outbox", "ack"}, operations)
	require.Len(t, repo.byDedupKey, 2)
	require.Equal(t, 1, ack.acked)
	require.Zero(t, ack.nacked)
}

func TestDeletionMessagePartialRegistrationUsesConfirmedRetryBeforeAck(t *testing.T) {
	operations := []string{}
	repo := &partialDeletionEnqueueFake{failVector: true}
	repo.operations = &operations
	ack := &timelineFailureAcknowledgerFake{operations: &operations}
	publisher := &timelineFailurePublisherFake{operations: &operations}
	router, err := newTimelineFailureRouter(publisher)
	require.NoError(t, err)
	operations = nil
	id := uint64(100)
	c := &TimelineConsumer{outboxRepo: repo, failureRouter: router,
		newOutboxTaskID: func() (uint64, error) { id++; return id, nil }}
	body, err := json.Marshal(events.TweetDeletedEvent{TweetID: 17, AuthorID: 42})
	require.NoError(t, err)
	c.handleDeleteMessage(amqp.Delivery{Acknowledger: ack, Body: body})
	// The original is acknowledged only after its retry publish is confirmed,
	// without executing Timeline cleanup or pretending registration completed.
	require.Equal(t, []string{"outbox", "publish", "ack"}, operations)
	require.Len(t, repo.byDedupKey, 1)
	require.Len(t, publisher.published, 1)
	require.Equal(t, ExchangeTimelineRetry, publisher.published[0].exchange)
}
