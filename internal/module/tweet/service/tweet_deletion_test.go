package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/module/tweet/cache"
	tweetrepo "twitter-clone/internal/module/tweet/repository"
	"twitter-clone/internal/pkg/database/uow"
)

// This in-memory SQL driver exercises the real GORM UoW and repositories.
// It stages both mutations until COMMIT and rejects queries outside the UoW.
// Its mutex models transaction serialization; it is not a MySQL lock test.
type deleteSQLFixture struct {
	mu                 sync.Mutex
	deleted, missing   bool
	fail               string
	events             []domain.OutboxEvent
	queries            []string
	commits, rollbacks int
	afterCommit        func()
	transactionBudget  time.Duration
}
type deleteSQLConnector struct{ fixture *deleteSQLFixture }
type deleteSQLDriver struct{}
type deleteSQLConn struct {
	fixture *deleteSQLFixture
	tx      *deleteSQLTx
}
type deleteSQLTx struct {
	conn    *deleteSQLConn
	deleted bool
	events  []domain.OutboxEvent
}

func (c deleteSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &deleteSQLConn{fixture: c.fixture}, nil
}
func (deleteSQLConnector) Driver() driver.Driver         { return deleteSQLDriver{} }
func (deleteSQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }
func (*deleteSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*deleteSQLConn) Close() error { return nil }
func (c *deleteSQLConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *deleteSQLConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.fixture.mu.Lock()
	if deadline, ok := ctx.Deadline(); ok {
		c.fixture.transactionBudget = time.Until(deadline)
	}
	if c.fixture.fail == "begin" {
		c.fixture.mu.Unlock()
		return nil, errors.New("fixture begin failure")
	}
	c.tx = &deleteSQLTx{conn: c, deleted: c.fixture.deleted}
	return c.tx, nil
}
func (tx *deleteSQLTx) Commit() error {
	f := tx.conn.fixture
	defer f.mu.Unlock()
	tx.conn.tx = nil
	if f.fail == "commit" {
		return errors.New("fixture commit failure")
	}
	f.deleted = tx.deleted
	f.events = append(f.events, tx.events...)
	f.commits++
	if f.afterCommit != nil {
		f.afterCommit()
	}
	return nil
}
func (tx *deleteSQLTx) Rollback() error {
	f := tx.conn.fixture
	tx.conn.tx = nil
	f.rollbacks++
	f.mu.Unlock()
	return nil
}
func (c *deleteSQLConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.tx == nil {
		return nil, errors.New("identity read escaped transaction")
	}
	c.fixture.queries = append(c.fixture.queries, query)
	if c.fixture.fail == "read" {
		return nil, errors.New("fixture read failure")
	}
	if !strings.Contains(query, "FOR UPDATE") || !strings.Contains(query, "deleted_at = 0") {
		return nil, errors.New("missing active-row lock")
	}
	return &deleteSQLRows{present: !c.fixture.missing && !c.tx.deleted}, nil
}
func (c *deleteSQLConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.tx == nil {
		return nil, errors.New("mutation escaped transaction")
	}
	c.fixture.queries = append(c.fixture.queries, query)
	switch {
	case strings.HasPrefix(query, "UPDATE `tweets`"):
		if c.fixture.fail == "delete" {
			return nil, errors.New("fixture delete failure")
		}
		if !strings.Contains(query, "deleted_at = 0") {
			return nil, errors.New("missing active-row predicate")
		}
		c.tx.deleted = true
		return deleteSQLResult{}, nil
	case strings.HasPrefix(query, "INSERT INTO `outbox_events`"):
		if c.fixture.fail == "outbox" {
			return nil, errors.New("fixture outbox failure")
		}
		if !c.tx.deleted {
			return nil, errors.New("event precedes delete")
		}
		var event domain.OutboxEvent
		// Match GORM's bound columns instead of assuming argument positions.
		columns := strings.Split(strings.Split(strings.Split(query, "(")[1], ")")[0], ",")
		for i, column := range columns {
			switch strings.Trim(column, "` ") {
			case "event_type":
				event.EventType = args[i].Value.(string)
			case "payload":
				event.Payload = args[i].Value.(string)
			case "dedup_key":
				key := args[i].Value.(string)
				event.DedupKey = &key
			case "created_at":
				event.CreatedAt = args[i].Value.(int64)
			}
		}
		c.tx.events = append(c.tx.events, event)
		return deleteSQLResult{}, nil
	default:
		return nil, errors.New("unexpected SQL mutation")
	}
}

type deleteSQLRows struct{ present bool }
type deleteSQLResult struct{}

func (deleteSQLResult) LastInsertId() (int64, error) { return 1, nil }
func (deleteSQLResult) RowsAffected() (int64, error) { return 1, nil }
func (*deleteSQLRows) Columns() []string             { return []string{"id", "user_id", "deleted_at"} }
func (*deleteSQLRows) Close() error                  { return nil }
func (r *deleteSQLRows) Next(row []driver.Value) error {
	if !r.present {
		return io.EOF
	}
	r.present = false
	row[0], row[1], row[2] = int64(9007199254740997), int64(42), int64(0)
	return nil
}

func newDeleteSQLService(t *testing.T, f *deleteSQLFixture) *TweetService {
	t.Helper()
	conn := sql.OpenDB(deleteSQLConnector{fixture: f})
	conn.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return &TweetService{repo: tweetrepo.NewTweetRepository(db), outboxEventRepo: tweetrepo.NewMysqlOutboxEventRepo(db), uow: uow.NewGormManager(db)}
}

func TestDeleteTweetCommitsRealRepositoriesTogetherWithoutProducer(t *testing.T) {
	f := &deleteSQLFixture{}
	s := newDeleteSQLService(t, f)
	require.NoError(t, s.DeleteTweet(context.Background(), 9007199254740997, 42))
	require.True(t, f.deleted)
	require.Equal(t, 1, f.commits)
	require.Positive(t, f.transactionBudget)
	require.LessOrEqual(t, f.transactionBudget, tweetDeleteTransactionTimeout)
	require.Len(t, f.events, 1)
	event := f.events[0]
	require.Equal(t, events.OutboxEventTypeTweetDeleted, event.EventType)
	require.Equal(t, "tweet-deleted:9007199254740997", *event.DedupKey)
	require.Positive(t, event.CreatedAt)
	var payload events.TweetDeletedEvent
	require.NoError(t, json.Unmarshal([]byte(event.Payload), &payload))
	require.Equal(t, uint64(9007199254740997), payload.TweetID)
	require.Equal(t, uint64(42), payload.AuthorID)
	require.NotContains(t, event.Payload, "content")
	require.Len(t, f.queries, 3)
	require.ErrorIs(t, s.DeleteTweet(context.Background(), 9007199254740997, 42), ErrTweetNotFound)
	require.Len(t, f.events, 1)
}

func TestDeleteTweetSQLFailuresDoNotReturnSuccessOrCommitPartialState(t *testing.T) {
	for _, stage := range []string{"begin", "read", "delete", "outbox", "commit"} {
		t.Run(stage, func(t *testing.T) {
			f := &deleteSQLFixture{fail: stage}
			s := newDeleteSQLService(t, f)
			err := s.DeleteTweet(context.Background(), 9007199254740997, 42)
			require.ErrorContains(t, err, "fixture "+stage+" failure")
			require.False(t, f.deleted)
			require.Empty(t, f.events)
			require.Zero(t, f.commits)
			if stage != "begin" && stage != "commit" {
				require.Equal(t, 1, f.rollbacks)
			}
		})
	}
}

func TestDeleteTweetRejectsUnauthorizedMissingAndCancelled(t *testing.T) {
	f := &deleteSQLFixture{}
	s := newDeleteSQLService(t, f)
	require.ErrorIs(t, s.DeleteTweet(context.Background(), 9007199254740997, 43), ErrUnauthorized)
	require.False(t, f.deleted)
	require.Empty(t, f.events)
	f.missing = true
	require.ErrorIs(t, s.DeleteTweet(context.Background(), 9007199254740997, 42), ErrTweetNotFound)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, s.DeleteTweet(ctx, 9007199254740997, 42), context.Canceled)
	require.Empty(t, f.events)
}

func TestDeleteTweetConcurrentRequestsCommitOneEvent(t *testing.T) {
	f := &deleteSQLFixture{}
	s := newDeleteSQLService(t, f)
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- s.DeleteTweet(context.Background(), 9007199254740997, 42) }()
	}
	first, second := <-results, <-results
	if first != nil {
		first, second = second, first
	}
	require.NoError(t, first)
	require.ErrorIs(t, second, ErrTweetNotFound)
	require.Len(t, f.events, 1)
	require.Equal(t, 1, f.commits)
}

func TestDeleteTweetRequiresTransactionPortsAndValidIDs(t *testing.T) {
	s := &TweetService{}
	require.ErrorIs(t, s.DeleteTweet(context.Background(), 0, 42), ErrInvalidTweetID)
	require.ErrorIs(t, s.DeleteTweet(context.Background(), 1, 0), ErrInvalidAuthorID)
	require.ErrorIs(t, s.DeleteTweet(context.Background(), 1, 42), ErrTweetDeletionUnavailable)
	f := &deleteSQLFixture{}
	s = newDeleteSQLService(t, f)
	reader := s.repo.(domain.TweetDeletionRepository)
	_, err := reader.GetForDeletion(context.Background(), 1)
	require.ErrorContains(t, err, "unit of work")
	s.outboxEventRepo = nil
	require.ErrorIs(t, s.DeleteTweet(context.Background(), 1, 42), ErrTweetDeletionUnavailable)
	require.Empty(t, f.queries)
}

type deletionCacheHook struct {
	fixture      *deleteSQLFixture
	beforeCommit bool
	cancelled    bool
	fail         bool
	calls        int
}

func (h *deletionCacheHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	h.calls++
	h.beforeCommit = h.beforeCommit || h.fixture.commits == 0
	h.cancelled = h.cancelled || ctx.Err() != nil
	if h.fail {
		return ctx, errors.New("fixture cache unavailable")
	}
	return ctx, nil
}
func (*deletionCacheHook) AfterProcess(context.Context, redis.Cmder) error { return nil }
func (*deletionCacheHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (*deletionCacheHook) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func TestDeleteTweetCacheRunsAfterCommitDespiteCallerCancellation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "cache_failure"}[fail], func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &deleteSQLFixture{afterCommit: cancel}
			s := newDeleteSQLService(t, f)
			hook := &deletionCacheHook{fixture: f, fail: fail}
			client.AddHook(hook)
			s.timelineCache = cache.NewTimelineCache(client)
			require.NoError(t, s.DeleteTweet(ctx, 9007199254740997, 42))
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.True(t, f.deleted)
			require.Len(t, f.events, 1)
			require.Equal(t, 2, hook.calls)
			require.False(t, hook.beforeCommit)
			require.False(t, hook.cancelled)
		})
	}
}
