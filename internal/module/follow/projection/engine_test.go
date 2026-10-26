package projection

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

type storeFake struct {
	mu           sync.Mutex
	state        AuthorState
	count        int64
	fact         PairFact
	rows         []RelationRow
	tweets       []TweetRow
	active       map[uint64]bool
	registered   int
	failRegister bool
	failAdvance  bool
	stale        bool
	beforeAfter  func()
}

func (s *storeFake) Register(context.Context, events.FollowRelationEvent) (bool, error) {
	if s.failRegister {
		return false, errors.New("register failed")
	}
	s.registered++
	return true, nil
}
func (s *storeFake) WithAuthorLock(ctx context.Context, _ uint64, fn func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(ctx)
}
func (s *storeFake) Owns(context.Context, *domain.OutboxTask) (bool, error)     { return !s.stale, nil }
func (s *storeFake) ReadPair(context.Context, uint64, uint64) (PairFact, error) { return s.fact, nil }
func (s *storeFake) ReadAuthor(context.Context, uint64) (AuthorState, error)    { return s.state, nil }
func (s *storeFake) SaveAuthor(_ context.Context, state AuthorState) error {
	s.state = state
	return nil
}
func (s *storeFake) FollowerCount(context.Context, uint64) (int64, error) { return s.count, nil }
func (s *storeFake) HistoryFollowers(_ context.Context, _ uint64, cursor uint64) (RelationPage, error) {
	rows := []RelationRow{}
	for _, row := range s.rows {
		if cursor == 0 || row.ID < cursor {
			rows = append(rows, row)
			if len(rows) > PageSize {
				break
			}
		}
	}
	more := len(rows) > PageSize
	if more {
		rows = rows[:PageSize]
	}
	next := uint64(0)
	if more {
		next = rows[len(rows)-1].ID
	}
	return RelationPage{Rows: rows, More: more, Next: next}, nil
}
func (s *storeFake) ActiveFollowers(context.Context, uint64, []uint64) (map[uint64]bool, error) {
	return s.active, nil
}
func (s *storeFake) HistoryTweets(_ context.Context, _ uint64, cursor uint64) (TweetPage, error) {
	rows := []TweetRow{}
	for _, row := range s.tweets {
		if cursor == 0 || row.ID < cursor {
			rows = append(rows, row)
			if len(rows) > PageSize {
				break
			}
		}
	}
	more := len(rows) > PageSize
	if more {
		rows = rows[:PageSize]
	}
	next := uint64(0)
	if more {
		next = rows[len(rows)-1].ID
	}
	return TweetPage{Rows: rows, More: more, Next: next}, nil
}
func (s *storeFake) Advance(_ context.Context, t *domain.OutboxTask, p Checkpoint, done bool) (bool, error) {
	if s.failAdvance {
		return false, errors.New("checkpoint failed")
	}
	body, _ := json.Marshal(p)
	t.Payload = string(body)
	t.Status = domain.OutboxStatusPending
	if done {
		t.Status = domain.OutboxStatusSuccess
	}
	return true, nil
}

func (s *storeFake) Interrupt(context.Context, *domain.OutboxTask) (bool, error) {
	return !s.stale, nil
}

type cacheFake struct {
	members    map[uint64]bool
	timeline   map[uint64]bool
	calls      int
	failAt     int
	afterPair  func()
	beginCount int
	celebrity  bool
}

func (c *cacheFake) Celebrity(context.Context, uint64) (bool, error) { return c.celebrity, nil }

func (c *cacheFake) Begin(context.Context, uint64, AuthorState, Fence) error {
	c.beginCount++
	return nil
}
func (c *cacheFake) Follower(_ context.Context, _ uint64, follower uint64, active bool, _ Fence) error {
	c.calls++
	if c.calls == c.failAt {
		return errors.New("cache unavailable")
	}
	if c.members == nil {
		c.members = map[uint64]bool{}
	}
	c.members[follower] = active
	return nil
}
func (c *cacheFake) PairTweets(_ context.Context, _ uint64, _ uint64, following bool, rows []TweetRow, _ Fence) error {
	if c.timeline == nil {
		c.timeline = map[uint64]bool{}
	}
	for _, row := range rows {
		c.timeline[row.ID] = following && row.DeletedAt == 0 && (row.VisibleType == domain.VisiblePublic || row.VisibleType == domain.VisibleFollows)
	}
	if c.afterPair != nil {
		c.afterPair()
	}
	return nil
}
func eventFixture() events.FollowRelationEvent {
	return events.FollowRelationEvent{FollowerID: 17, FolloweeID: 42, RelationID: 61, EventKey: events.FollowRelationEventKey(61, false), SchemaVersion: events.FollowRelationSchemaVersion, OccurredAtUnixMS: 123}
}
func taskFixture(kind string) *domain.OutboxTask {
	body, _ := json.Marshal(Checkpoint{Event: eventFixture()})
	return &domain.OutboxTask{ID: 91, TaskType: kind, Payload: string(body), Status: 3, MaxRetries: 5, LeaseOwner: "fixture", LeaseToken: "fixture-token", LeaseUntil: time.Now().Add(time.Minute).UnixMilli()}
}
func runPages(t *testing.T, e Engine, task *domain.OutboxTask, max int) {
	t.Helper()
	for i := 0; i < max; i++ {
		task.Status = 3
		require.NoError(t, e.Execute(context.Background(), task))
		if task.Status == domain.OutboxStatusSuccess {
			return
		}
	}
	t.Fatal("projection failed to complete within bounded fixture pages")
}
func TestEnginePromotionCoversBeyondFiveThousandAndResumesPersistedAuthorCursor(t *testing.T) {
	s := &storeFake{state: AuthorState{AuthorID: 42}, count: 5012, active: map[uint64]bool{}}
	for i := 5012; i >= 1; i-- {
		s.rows = append(s.rows, RelationRow{ID: uint64(i + 10000), FollowerID: uint64(i)})
		s.active[uint64(i)] = true
	}
	c := &cacheFake{}
	e := Engine{Store: s, Cache: c}
	task := taskFixture(AuthorTask)
	for i := 0; i < 4; i++ {
		require.NoError(t, e.Execute(context.Background(), task))
	}
	cursor := s.state.Cursor
	require.Positive(t, cursor)
	require.False(t, s.state.Complete)
	require.Equal(t, 400, c.calls)
	e = Engine{Store: s, Cache: c}
	runPages(t, e, task, 60)
	require.True(t, s.state.Complete)
	require.Len(t, c.members, 5012)
	require.True(t, c.members[5012])
	require.Equal(t, 5012, c.calls)
	// Duplicate events share a complete author checkpoint instead of rebroadcast.
	runPages(t, e, taskFixture(AuthorTask), 1)
	require.Equal(t, 5012, c.calls)
	s.count = 4499
	s.active[1] = false
	runPages(t, e, taskFixture(AuthorTask), 60)
	require.False(t, s.state.Celebrity)
	for _, present := range c.members {
		require.False(t, present)
	}
}
func TestEngineFailureRetainsPageAndDuplicateWritesConverge(t *testing.T) {
	s := &storeFake{state: AuthorState{AuthorID: 42}, count: 5000, active: map[uint64]bool{1: true, 2: true}, rows: []RelationRow{{ID: 102, FollowerID: 1}, {ID: 101, FollowerID: 2}}}
	c := &cacheFake{failAt: 2}
	e := Engine{Store: s, Cache: c}
	task := taskFixture(AuthorTask)
	require.Error(t, e.Execute(context.Background(), task))
	require.Zero(t, s.state.Cursor)
	require.False(t, s.state.Complete)
	require.True(t, c.members[1])
	c.failAt = 0
	runPages(t, e, task, 2)
	require.True(t, c.members[2])
	require.True(t, s.state.Complete)
}
func TestEngineOldUnfollowCannotOverrideCurrentRefollowAndCleansMoreThanHundredTweets(t *testing.T) {
	s := &storeFake{state: AuthorState{AuthorID: 42, Revision: 1, Complete: true}, fact: PairFact{Generation: 99, Active: true}}
	for i := 250; i >= 1; i-- {
		s.tweets = append(s.tweets, TweetRow{ID: uint64(i), VisibleType: domain.VisibleFollows})
	}
	c := &cacheFake{}
	e := Engine{Store: s, Cache: c}
	task := taskFixture(PairTask) // event refers to old generation 61.
	runPages(t, e, task, 4)
	require.True(t, c.timeline[1])
	require.Len(t, c.timeline, 250)
	s.fact.Active = false
	runPages(t, e, taskFixture(PairTask), 4)
	for _, present := range c.timeline {
		require.False(t, present)
	}
}
func TestEnginePairGenerationChangesResetCheckpointAndHiddenTweetsAreRemoved(t *testing.T) {
	s := &storeFake{state: AuthorState{AuthorID: 42, Revision: 1, Complete: true}, fact: PairFact{Generation: 99, Active: true}, tweets: []TweetRow{{ID: 5}, {ID: 4, VisibleType: domain.VisiblePrivate}, {ID: 3, DeletedAt: 123}}}
	c := &cacheFake{timeline: map[uint64]bool{4: true, 3: true}}
	c.afterPair = func() { s.fact = PairFact{Generation: 100, Active: false} }
	task := taskFixture(PairTask)
	require.NoError(t, (Engine{Store: s, Cache: c}).Execute(context.Background(), task))
	require.Equal(t, domain.OutboxStatusPending, task.Status)
	var p Checkpoint
	require.NoError(t, json.Unmarshal([]byte(task.Payload), &p))
	require.False(t, p.Initialized)
	require.Zero(t, p.Cursor)
	c.afterPair = nil
	runPages(t, Engine{Store: s, Cache: c}, task, 2)
	require.False(t, c.timeline[5])
	require.False(t, c.timeline[4])
	require.False(t, c.timeline[3])
}
func TestEngineStaleClaimAndUnavailableCheckpointNeverComplete(t *testing.T) {
	s := &storeFake{state: AuthorState{AuthorID: 42, Revision: 1, Complete: true}, stale: true}
	c := &cacheFake{}
	e := Engine{Store: s, Cache: c}
	require.Error(t, e.Execute(context.Background(), taskFixture(PairTask)))
	require.Zero(t, c.beginCount)
	s.stale = false
	s.failAdvance = true
	task := taskFixture(PairTask)
	require.Error(t, e.Execute(context.Background(), task))
	require.Equal(t, 3, task.Status)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, e.Execute(ctx, task), context.Canceled)
}
func TestDecideCelebrityRetainsHysteresisAndCheckpointValidationRejectsNonadvancingPages(t *testing.T) {
	require.True(t, DecideCelebrity(false, 5000))
	require.False(t, DecideCelebrity(true, 4499))
	require.True(t, DecideCelebrity(true, 4800))
	require.False(t, DecideCelebrity(false, 4800))
	require.Error(t, validateRelationPage(RelationPage{Rows: []RelationRow{{ID: 100, FollowerID: 1}}, Next: 100, More: true}, 100))
	require.Error(t, validateTweetPage(TweetPage{Rows: []TweetRow{{ID: 5}, {ID: 6}}}, 0))
	for _, rows := range [][]Scope{nil, {{Author: 0}}, {{Author: 1, Follower: 1}}, {{Author: 1}, {Author: 1}}, make([]Scope, 101)} {
		_, err := NormalizeScope(rows)
		require.Error(t, err)
	}
}

func TestEngineFirstAuthorStatePreservesLegacyCelebrityWithinHysteresisBand(t *testing.T) {
	s := &storeFake{state: AuthorState{AuthorID: 42}, count: 4800}
	c := &cacheFake{celebrity: true}
	runPages(t, Engine{Store: s, Cache: c}, taskFixture(AuthorTask), 1)
	require.True(t, s.state.Celebrity)
	require.Positive(t, s.state.Revision)
}
