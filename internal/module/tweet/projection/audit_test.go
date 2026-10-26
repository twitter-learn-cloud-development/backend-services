package projection

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

type factsFake struct {
	mu     sync.Mutex
	rows   map[uint64]*domain.Tweet
	locks  int
	onLock func(int)
}

func (f *factsFake) WithProjectionLock(ctx context.Context, _ uint64, fn func(context.Context) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.locks++
	if f.onLock != nil {
		f.onLock(f.locks)
	}
	return fn(ctx)
}
func (f *factsFake) ReadProjectionTweet(_ context.Context, id uint64) (*domain.Tweet, error) {
	if f.rows[id] == nil {
		return nil, nil
	}
	copy := *f.rows[id]
	return &copy, nil
}

type indexFake struct {
	rows    map[uint64]domain.IndexProjectionSnapshot
	failure error
}

func (f *indexFake) ReadTweetProjection(_ context.Context, id uint64) (domain.IndexProjectionSnapshot, error) {
	return f.rows[id], f.failure
}

type registrarFake struct {
	tasks     map[string]*domain.OutboxTask
	failureAt int
	calls     int
}

func (r *registrarFake) RegisterProjectionRepair(_ context.Context, tasks []*domain.OutboxTask) (int, error) {
	r.calls++
	if r.calls == r.failureAt {
		return 0, errors.New("fixture registration failure")
	}
	if r.tasks == nil {
		r.tasks = map[string]*domain.OutboxTask{}
	}
	count := 0
	for _, task := range tasks {
		if r.tasks[*task.DedupKey] == nil {
			r.tasks[*task.DedupKey] = task
			count++
		}
	}
	return count, nil
}
func fixtureAuditor() Auditor {
	facts := &factsFake{rows: map[uint64]*domain.Tweet{17: {ID: 17, UserID: 42, Content: "private fixture body", CreatedAt: 1}}}
	return Auditor{Facts: facts, Text: &indexFake{rows: map[uint64]domain.IndexProjectionSnapshot{}}, Vector: &indexFake{rows: map[uint64]domain.IndexProjectionSnapshot{}}, Profile: domain.EmbeddingProfile{Model: "fixture", Dimension: 2, Version: "v1"}, TargetDigest: Hash([]byte("target"))}
}
func fixtureAuthorization(digest string) RepairAuthorization {
	return RepairAuthorization{ApprovedPlan: digest, Operation: "repair-1", Operator: "fixture operator", Reason: "approved fixture repair"}
}

func TestAuditMetadataPlanIsDeterministicAndDoesNotExposeBodiesOrIDs(t *testing.T) {
	a := fixtureAuditor()
	fact := a.Facts.(*factsFake).rows[17]
	a.Text.(*indexFake).rows[17] = domain.IndexProjectionSnapshot{Present: true, Copies: 1, Digest: domain.ProjectionDigest(fact), Version: domain.ProjectionVersion}
	a.Vector.(*indexFake).rows[17] = domain.IndexProjectionSnapshot{Present: true, Copies: 2, Digest: domain.ProjectionDigest(fact), Version: domain.ProjectionVersion, EmbeddingProfile: a.Profile.Digest()}
	plan, err := a.Audit(context.Background(), []uint64{18, 17})
	require.NoError(t, err)
	again, err := a.Audit(context.Background(), []uint64{17, 18})
	require.NoError(t, err)
	require.Equal(t, plan, again)
	require.Equal(t, "consistent", plan.Rows[0].TextStatus)
	require.Equal(t, "aliases", plan.Rows[0].VectorStatus)
	require.Equal(t, "consistent", plan.Rows[1].VectorStatus)
	encoded, err := json.Marshal(plan)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private fixture body")
	require.NotContains(t, string(encoded), `"tweet_id"`)
	require.NotContains(t, string(encoded), `"author_id"`)
	changed := a
	changed.Profile.Version = "v2"
	other, err := changed.Audit(context.Background(), []uint64{17, 18})
	require.NoError(t, err)
	require.NotEqual(t, plan.Digest, other.Digest)
}

func TestAuditFailuresNeverBecomeAnEmptySuccess(t *testing.T) {
	a := fixtureAuditor()
	a.Text.(*indexFake).failure = errors.New("private backend error")
	plan, err := a.Audit(context.Background(), []uint64{17})
	require.Error(t, err)
	require.Empty(t, plan.Rows)
	require.NotContains(t, err.Error(), "private backend error")
	for _, ids := range [][]uint64{nil, {0}, {1, 1}, make([]uint64, 101)} {
		_, err := a.Audit(context.Background(), ids)
		require.Error(t, err)
	}
	require.Equal(t, "residue", Compare(false, "absent", "", domain.IndexProjectionSnapshot{Present: true, Copies: 1}))
	require.Equal(t, "profile_drift", Compare(true, "digest", "new", domain.IndexProjectionSnapshot{Present: true, Copies: 1, Digest: "digest", Version: domain.ProjectionVersion, EmbeddingProfile: "old"}))
}

func TestRepairBindsPlanAndRegistersOnlyDriftWithPersistentAuditMetadata(t *testing.T) {
	a := fixtureAuditor()
	plan, err := a.Audit(context.Background(), []uint64{17})
	require.NoError(t, err)
	repo := &registrarFake{}
	_, created, err := a.Repair(context.Background(), []uint64{17}, fixtureAuthorization(plan.Digest), repo)
	require.NoError(t, err)
	require.Equal(t, 2, created)
	_, created, err = a.Repair(context.Background(), []uint64{17}, fixtureAuthorization(plan.Digest), repo)
	require.NoError(t, err)
	require.Zero(t, created)
	require.Len(t, repo.tasks, 2)
	for _, task := range repo.tasks {
		var payload domain.ProjectionTaskPayload
		require.NoError(t, json.Unmarshal([]byte(task.Payload), &payload))
		require.Equal(t, plan.Digest, payload.ApprovedPlan)
		require.Equal(t, uint64(42), payload.AuthorID)
		require.NotEmpty(t, payload.OperatorDigest)
		require.NotContains(t, task.Payload, "fixture operator")
		require.NotContains(t, task.Payload, "private fixture body")
	}
	a.Facts.(*factsFake).rows[17].Content = "new"
	_, _, err = a.Repair(context.Background(), []uint64{17}, fixtureAuthorization(plan.Digest), repo)
	require.ErrorContains(t, err, "plan changed")
	require.Len(t, repo.tasks, 2)
}

func TestRepairReportsPartialRegistrationAndRejectsFactDriftAtWriteTime(t *testing.T) {
	a := fixtureAuditor()
	a.Text.(*indexFake).rows[18] = domain.IndexProjectionSnapshot{Present: true, Copies: 1}
	plan, err := a.Audit(context.Background(), []uint64{17, 18})
	require.NoError(t, err)
	repo := &registrarFake{failureAt: 2}
	_, created, err := a.Repair(context.Background(), []uint64{17, 18}, fixtureAuthorization(plan.Digest), repo)
	require.Error(t, err)
	require.Equal(t, 2, created)
	require.Len(t, repo.tasks, 2)
	a = fixtureAuditor()
	plan, err = a.Audit(context.Background(), []uint64{17})
	require.NoError(t, err)
	facts := a.Facts.(*factsFake)
	facts.onLock = func(n int) {
		if n == 3 {
			facts.rows[17].Content = "changed after audit"
		}
	}
	repo = &registrarFake{}
	_, created, err = a.Repair(context.Background(), []uint64{17}, fixtureAuthorization(plan.Digest), repo)
	require.ErrorContains(t, err, "fact changed")
	require.Zero(t, created)
	require.Empty(t, repo.tasks)
}

type publicationFake struct {
	events        []domain.OutboxEvent
	receipts      map[uint64]domain.OutboxPublication
	recordFailure bool
}

func (f *publicationFake) ReadEvents(context.Context, []uint64) ([]domain.OutboxEvent, error) {
	return append([]domain.OutboxEvent(nil), f.events...), nil
}
func (f *publicationFake) ReadPublication(_ context.Context, id uint64) (*domain.OutboxPublication, error) {
	receipt, ok := f.receipts[id]
	if !ok {
		return nil, nil
	}
	return &receipt, nil
}
func (f *publicationFake) RecordPublication(_ context.Context, receipt domain.OutboxPublication) error {
	if f.recordFailure {
		return errors.New("fixture receipt failure")
	}
	if f.receipts == nil {
		f.receipts = map[uint64]domain.OutboxPublication{}
	}
	f.receipts[receipt.EventID] = receipt
	return nil
}

type publicationPublisherFake struct {
	calls     int
	failureAt int
}

func (p *publicationPublisherFake) PublishRawJSON(context.Context, string, string, []byte) error {
	p.calls++
	if p.calls == p.failureAt {
		return errors.New("fixture confirmation failure")
	}
	return nil
}

func TestPublicationReceiptsNeverPrecedeConfirmationAndCannotAuthorizeGCWithoutMatch(t *testing.T) {
	old := time.Now().Add(-96 * time.Hour).UnixMilli()
	repo := &publicationFake{events: []domain.OutboxEvent{{ID: 17, EventType: "TWEET_DELETED", Payload: `{"tweet_id":9007199254740997,"author_id":42}`, CreatedAt: old}, {ID: 18, EventType: "TWEET_CREATED", Payload: `{"tweet_id":18,"author_id":42}`, CreatedAt: old}}}
	publisher := &publicationPublisherFake{failureAt: 2}
	maintenance := PublicationMaintenance{Repository: repo, Publisher: publisher, Target: Hash([]byte("target")), Now: time.Now}
	plan, err := maintenance.Inspect(context.Background(), []uint64{17, 18})
	require.NoError(t, err)
	require.False(t, plan.Rows[0].RetentionEligible)
	_, count, err := maintenance.Replay(context.Background(), []uint64{17, 18}, fixtureAuthorization(plan.Digest))
	require.Error(t, err)
	require.Equal(t, 1, count)
	require.Len(t, repo.receipts, 1)
	_, count, err = maintenance.Replay(context.Background(), []uint64{17, 18}, fixtureAuthorization(plan.Digest))
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Len(t, repo.receipts, 2)
	receipt := repo.receipts[17]
	receipt.ConfirmedAt = old
	repo.receipts[17] = receipt
	inspected, err := maintenance.Inspect(context.Background(), []uint64{17, 18})
	require.NoError(t, err)
	require.True(t, inspected.Rows[0].RetentionEligible)
	require.False(t, inspected.Rows[1].RetentionEligible)
	receipt.PayloadDigest = "different"
	repo.receipts[17] = receipt
	_, _, err = maintenance.Replay(context.Background(), []uint64{17, 18}, fixtureAuthorization(plan.Digest))
	require.ErrorContains(t, err, "receipt conflict")
}

func TestPublicationCanonicalDigestKeepsLargeIntegersAndRejectsTrailingJSON(t *testing.T) {
	a, err := domain.OutboxPayloadDigest(`{"tweet_id":9007199254740997,"author_id":42}`)
	require.NoError(t, err)
	b, err := domain.OutboxPayloadDigest(`{ "author_id":42, "tweet_id":9007199254740997 }`)
	require.NoError(t, err)
	require.Equal(t, a, b)
	c, err := domain.OutboxPayloadDigest(`{"tweet_id":9007199254740996,"author_id":42}`)
	require.NoError(t, err)
	require.NotEqual(t, a, c)
	_, err = domain.OutboxPayloadDigest(`{} {}`)
	require.Error(t, err)
}

func TestPublicationEngagementReplayRemainsClosedUntilIdempotencyIsImplemented(t *testing.T) {
	repo := &publicationFake{events: []domain.OutboxEvent{{ID: 17, EventType: "TWEET_LIKED", Payload: `{"tweet_id":17,"user_id":42}`}}}
	publisher := &publicationPublisherFake{}
	m := PublicationMaintenance{Repository: repo, Publisher: publisher, Target: Hash([]byte("target")), Now: time.Now}
	plan, err := m.Inspect(context.Background(), []uint64{17})
	require.NoError(t, err)
	_, count, err := m.Replay(context.Background(), []uint64{17}, fixtureAuthorization(plan.Digest))
	require.ErrorContains(t, err, "not yet idempotent")
	require.Zero(t, count)
	require.Zero(t, publisher.calls)
	require.Empty(t, repo.receipts)
}

func TestPublicationFollowEventsCanBeInspectedButNotReplayedBeforeConsumerMigration(t *testing.T) {
	body, _ := json.Marshal(events.FollowRelationEvent{FollowerID: 17, FolloweeID: 42, RelationID: 61, SchemaVersion: events.FollowRelationSchemaVersion, EventKey: events.FollowRelationEventKey(61, true), OccurredAtUnixMS: 123})
	repo := &publicationFake{events: []domain.OutboxEvent{{ID: 17, EventType: events.OutboxEventTypeUserFollowed, Payload: string(body)}}}
	publisher := &publicationPublisherFake{}
	m := PublicationMaintenance{Repository: repo, Publisher: publisher, Target: Hash([]byte("target")), Now: time.Now}
	plan, err := m.Inspect(context.Background(), []uint64{17})
	require.NoError(t, err)
	_, count, err := m.Replay(context.Background(), []uint64{17}, fixtureAuthorization(plan.Digest))
	require.Error(t, err)
	require.Zero(t, count)
	require.Zero(t, publisher.calls)
}
