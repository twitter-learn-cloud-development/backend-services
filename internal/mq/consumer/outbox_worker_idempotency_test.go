package consumer

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

type outboxWorkerRepositoryFake struct {
	mu              sync.Mutex
	claimable       []*domain.OutboxTask
	claimRequests   []domain.OutboxClaimRequest
	completions     []domain.OutboxClaimCompletion
	completionErrs  []error
	failures        []domain.OutboxClaimFailure
	recovery        domain.OutboxLeaseRecovery
	recoveryCalls   int
	staleCompletion bool
	staleFailure    bool
	deleted         []uint64
	cleanupResults  []int64
	cleanupLimits   []int
	cleanupCutoffs  []int64
}

func (*outboxWorkerRepositoryFake) Create(context.Context, *domain.OutboxTask) error { return nil }
func (*outboxWorkerRepositoryFake) CreateIdempotent(context.Context, *domain.OutboxTask) (bool, error) {
	return true, nil
}
func (r *outboxWorkerRepositoryFake) Claim(_ context.Context, request domain.OutboxClaimRequest) ([]*domain.OutboxTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claimRequests = append(r.claimRequests, request)
	count := len(r.claimable)
	if count > request.Limit {
		count = request.Limit
	}
	tasks := make([]*domain.OutboxTask, 0, count)
	for _, task := range r.claimable[:count] {
		copyTask := *task
		copyTask.Status = domain.OutboxStatusProcessing
		copyTask.Retries++
		copyTask.LeaseOwner = request.LeaseOwner
		copyTask.LeaseToken = request.LeaseToken
		copyTask.LeaseUntil = request.LeaseUntilUnixMilli
		tasks = append(tasks, &copyTask)
	}
	r.claimable = r.claimable[count:]
	return tasks, nil
}
func (r *outboxWorkerRepositoryFake) CompleteClaim(ctx context.Context, completion domain.OutboxClaimCompletion) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completions = append(r.completions, completion)
	r.completionErrs = append(r.completionErrs, ctx.Err())
	return !r.staleCompletion, nil
}
func (r *outboxWorkerRepositoryFake) FailClaim(_ context.Context, failure domain.OutboxClaimFailure) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = append(r.failures, failure)
	return !r.staleFailure, nil
}
func (r *outboxWorkerRepositoryFake) RecoverExpiredClaims(context.Context, int64, int) (domain.OutboxLeaseRecovery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recoveryCalls++
	return r.recovery, nil
}
func (r *outboxWorkerRepositoryFake) Delete(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, id)
	return nil
}
func (r *outboxWorkerRepositoryFake) DeleteCompletedBefore(_ context.Context, cutoff int64, limit int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupCutoffs = append(r.cleanupCutoffs, cutoff)
	r.cleanupLimits = append(r.cleanupLimits, limit)
	if len(r.cleanupResults) == 0 {
		return 0, nil
	}
	result := r.cleanupResults[0]
	r.cleanupResults = r.cleanupResults[1:]
	return result, nil
}

type recordingOutboxObserver struct {
	mu     sync.Mutex
	counts map[string]int
}

func (o *recordingOutboxObserver) ObserveOutbox(operation, result string, count int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.counts == nil {
		o.counts = make(map[string]int)
	}
	o.counts[operation+":"+result] += count
}

func (o *recordingOutboxObserver) count(operation, result string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.counts[operation+":"+result]
}

func TestOutboxWorkerRetainsSuccessfulTaskAsDedupReceipt(t *testing.T) {
	task := newSearchSyncOutboxTask(t, 91, 801)
	repository := &outboxWorkerRepositoryFake{claimable: []*domain.OutboxTask{task}}
	observer := &recordingOutboxObserver{}
	executions := 0
	consumer := newOutboxTestConsumer(repository, observer)
	consumer.searchSyncExecutor = func(context.Context, *events.TweetCreatedEvent) error {
		executions++
		return nil
	}

	consumer.processOutboxTasks(context.Background())

	require.Equal(t, 1, executions)
	require.Empty(t, repository.deleted)
	require.Len(t, repository.claimRequests, 1)
	require.Len(t, repository.completions, 1)
	require.Empty(t, repository.failures)
	require.Equal(t, "worker-a", repository.completions[0].LeaseOwner)
	require.Equal(t, "claim-token-a", repository.completions[0].LeaseToken)
	require.Equal(t, 1, observer.count(outboxOperationFinalize, "succeeded"))
}

func TestOutboxWorkerRejectsStaleCompletion(t *testing.T) {
	repository := &outboxWorkerRepositoryFake{
		claimable:       []*domain.OutboxTask{newSearchSyncOutboxTask(t, 92, 802)},
		staleCompletion: true,
	}
	observer := &recordingOutboxObserver{}
	consumer := newOutboxTestConsumer(repository, observer)
	consumer.searchSyncExecutor = func(context.Context, *events.TweetCreatedEvent) error { return nil }

	consumer.processOutboxTasks(context.Background())

	require.Len(t, repository.completions, 1)
	require.Equal(t, 1, observer.count(outboxOperationFinalize, "stale"))
}

func TestOutboxWorkerMarksInvalidTaskTerminalThroughClaim(t *testing.T) {
	task := newSearchSyncOutboxTask(t, 93, 803)
	task.TaskType = "unknown"
	repository := &outboxWorkerRepositoryFake{claimable: []*domain.OutboxTask{task}}
	consumer := newOutboxTestConsumer(repository, &recordingOutboxObserver{})

	consumer.processOutboxTasks(context.Background())

	require.Len(t, repository.failures, 1)
	require.True(t, repository.failures[0].Terminal)
	require.Equal(t, "unknown outbox task type", repository.failures[0].ErrorMsg)
}

func TestOutboxWorkerMarksLastFailedAttemptTerminal(t *testing.T) {
	task := newSearchSyncOutboxTask(t, 96, 806)
	task.Retries = task.MaxRetries - 1
	repository := &outboxWorkerRepositoryFake{claimable: []*domain.OutboxTask{task}}
	consumer := newOutboxTestConsumer(repository, &recordingOutboxObserver{})
	consumer.searchSyncExecutor = func(context.Context, *events.TweetCreatedEvent) error {
		return context.DeadlineExceeded
	}

	consumer.processOutboxTasks(context.Background())

	require.Len(t, repository.failures, 1)
	require.True(t, repository.failures[0].Terminal)
}

func TestOutboxWorkerFinalizesWithBoundedContextAfterParentCancellation(t *testing.T) {
	task := newSearchSyncOutboxTask(t, 97, 807)
	task.Status = domain.OutboxStatusProcessing
	task.LeaseOwner = "worker-a"
	task.LeaseToken = "claim-token-a"
	repository := &outboxWorkerRepositoryFake{}
	consumer := newOutboxTestConsumer(repository, &recordingOutboxObserver{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	consumer.completeOutboxClaim(ctx, task)

	require.Len(t, repository.completions, 1)
	require.Len(t, repository.completionErrs, 1)
	require.NoError(t, repository.completionErrs[0])
}

func TestOutboxWorkerExecutesClaimedBatchConcurrently(t *testing.T) {
	repository := &outboxWorkerRepositoryFake{claimable: []*domain.OutboxTask{
		newSearchSyncOutboxTask(t, 94, 804),
		newSearchSyncOutboxTask(t, 95, 805),
	}}
	consumer := newOutboxTestConsumer(repository, &recordingOutboxObserver{})
	release := make(chan struct{})
	var started atomic.Int32
	consumer.searchSyncExecutor = func(context.Context, *events.TweetCreatedEvent) error {
		started.Add(1)
		<-release
		return nil
	}
	done := make(chan struct{})
	go func() {
		consumer.processOutboxTasks(context.Background())
		close(done)
	}()

	require.Eventually(t, func() bool { return started.Load() == 2 }, time.Second, 10*time.Millisecond)
	close(release)
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Len(t, repository.completions, 2)
}

func TestOutboxWorkerObservesExpiredLeaseRecovery(t *testing.T) {
	repository := &outboxWorkerRepositoryFake{recovery: domain.OutboxLeaseRecovery{Retryable: 2, Exhausted: 1}}
	observer := &recordingOutboxObserver{}
	consumer := newOutboxTestConsumer(repository, observer)

	consumer.processOutboxTasks(context.Background())

	require.Equal(t, 1, repository.recoveryCalls)
	require.Equal(t, 2, observer.count(outboxOperationRecover, "retryable"))
	require.Equal(t, 1, observer.count(outboxOperationRecover, "exhausted"))
	require.Equal(t, 1, observer.count(outboxOperationClaim, "empty"))
}

func TestOutboxLeaseCoversExecutionAndFinalizationBudgets(t *testing.T) {
	require.Greater(t, outboxLeaseDuration, outboxTaskTimeout+outboxFinalizeTimeout)
}

func TestOutboxWorkerCleanupIsBoundedAndBatched(t *testing.T) {
	repository := &outboxWorkerRepositoryFake{
		cleanupResults: []int64{outboxCleanupBatchSize, 3},
	}
	observer := &recordingOutboxObserver{}
	consumer := newOutboxTestConsumer(repository, observer)

	consumer.cleanupCompletedOutboxTasks(context.Background())

	require.Equal(t, []int{outboxCleanupBatchSize, outboxCleanupBatchSize}, repository.cleanupLimits)
	require.Len(t, repository.cleanupCutoffs, 2)
	require.Equal(t, repository.cleanupCutoffs[0], repository.cleanupCutoffs[1])
	require.Equal(t, outboxCleanupBatchSize+3, observer.count(outboxOperationCleanup, "deleted"))
}

func newOutboxTestConsumer(repository domain.OutboxRepository, observer OutboxWorkerObserver) *TimelineConsumer {
	fixedNow := time.UnixMilli(1_800_000_000_000)
	return &TimelineConsumer{
		outboxRepo:          repository,
		outboxObserver:      observer,
		outboxWorkerID:      "worker-a",
		newOutboxLeaseToken: func() string { return "claim-token-a" },
		outboxNow:           func() time.Time { return fixedNow },
	}
}

func newSearchSyncOutboxTask(t *testing.T, taskID, tweetID uint64) *domain.OutboxTask {
	t.Helper()
	event := events.TweetCreatedEvent{TweetID: tweetID, AuthorID: 44, Content: "hello"}
	payload, err := json.Marshal(event)
	require.NoError(t, err)
	dedupKey := "timeline:sync_es:tweet:test:v1"
	return &domain.OutboxTask{
		ID:         taskID,
		DedupKey:   &dedupKey,
		TaskType:   syncESOutboxTaskType,
		Payload:    string(payload),
		Status:     domain.OutboxStatusPending,
		MaxRetries: 5,
	}
}
