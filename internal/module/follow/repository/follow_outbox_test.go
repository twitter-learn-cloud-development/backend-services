package repository

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

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	followservice "twitter-clone/internal/module/follow/service"
	tweetrepo "twitter-clone/internal/module/tweet/repository"
	"twitter-clone/internal/pkg/database/uow"
)

// Runs production Service/UoW/Follow/Outbox repositories. Mutex serialization is
// a deterministic SQL fixture, not evidence of live MySQL locking or durability.
type followSQLFixture struct {
	mu      sync.Mutex
	follows []domain.Follow
	events  []domain.OutboxEvent
	fail    string
	queries []string
	nextID  uint64
	budget  time.Duration
}
type followSQLConnector struct{ f *followSQLFixture }
type followSQLDriver struct{}
type followSQLConn struct {
	f  *followSQLFixture
	tx *followSQLTx
}
type followSQLTx struct {
	c       *followSQLConn
	follows []domain.Follow
	events  []domain.OutboxEvent
	locked  bool
}

func (c followSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &followSQLConn{f: c.f}, nil
}
func (followSQLConnector) Driver() driver.Driver         { return followSQLDriver{} }
func (followSQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }
func (*followSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*followSQLConn) Close() error { return nil }
func (c *followSQLConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *followSQLConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.f.mu.Lock()
	if c.f.fail == "begin" {
		c.f.mu.Unlock()
		return nil, errors.New("begin failed")
	}
	if deadline, ok := ctx.Deadline(); ok {
		c.f.budget = time.Until(deadline)
	}
	c.tx = &followSQLTx{c: c, follows: append([]domain.Follow(nil), c.f.follows...), events: append([]domain.OutboxEvent(nil), c.f.events...)}
	return c.tx, nil
}
func (t *followSQLTx) Commit() error {
	defer t.c.f.mu.Unlock()
	t.c.tx = nil
	if t.c.f.fail == "commit" {
		return errors.New("commit failed")
	}
	t.c.f.follows = t.follows
	t.c.f.events = t.events
	if t.c.f.fail == "commit_unknown" {
		return errors.New("commit receipt lost")
	}
	return nil
}
func (t *followSQLTx) Rollback() error { t.c.tx = nil; t.c.f.mu.Unlock(); return nil }

type followSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *followSQLRows) Columns() []string { return r.columns }
func (*followSQLRows) Close() error        { return nil }
func (r *followSQLRows) Next(dst []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dst, r.values[r.index])
	r.index++
	return nil
}
func (c *followSQLConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.tx == nil {
		return nil, errors.New("query escaped UoW")
	}
	c.f.queries = append(c.f.queries, query)
	if !strings.Contains(query, "FOR UPDATE") {
		return nil, errors.New("missing row lock")
	}
	if strings.Contains(query, "`follow_pair_locks`") {
		if args[0].Value != int64(17) || args[1].Value != int64(42) {
			return nil, errors.New("wrong pair lock")
		}
		c.tx.locked = true
		return &followSQLRows{columns: []string{"follower_id", "followee_id"}, values: [][]driver.Value{{int64(17), int64(42)}}}, nil
	}
	if !c.tx.locked || !strings.Contains(query, "deleted_at = 0") || !strings.Contains(query, "LIMIT ?") || args[len(args)-1].Value != int64(2) {
		return nil, errors.New("missing bounded active read")
	}
	if c.f.fail == "read" {
		return nil, errors.New("read failed")
	}
	rows := &followSQLRows{columns: []string{"id", "follower_id", "followee_id", "created_at", "deleted_at"}}
	for _, f := range c.tx.follows {
		if f.DeletedAt == 0 {
			rows.values = append(rows.values, []driver.Value{int64(f.ID), int64(f.FollowerID), int64(f.FolloweeID), f.CreatedAt, f.DeletedAt})
			if len(rows.values) == 2 {
				break
			}
		}
	}
	return rows, nil
}
func insertFollowColumns(query string, args []driver.NamedValue) map[string]driver.Value {
	columns := strings.Split(strings.Split(strings.Split(query, "(")[1], ")")[0], ",")
	row := map[string]driver.Value{}
	for i, column := range columns {
		row[strings.Trim(column, " `")] = args[i].Value
	}
	return row
}

type followSQLResult struct{ id int64 }

type followSQLZeroResult struct{ followSQLResult }

func (followSQLZeroResult) RowsAffected() (int64, error) { return 0, nil }

func (r followSQLResult) LastInsertId() (int64, error) { return r.id, nil }
func (followSQLResult) RowsAffected() (int64, error)   { return 1, nil }
func (c *followSQLConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.tx == nil {
		return nil, errors.New("write escaped UoW")
	}
	c.f.queries = append(c.f.queries, query)
	switch {
	case strings.Contains(query, "INSERT INTO `follow_pair_locks`"):
		if c.f.fail == "lock" {
			return nil, errors.New("lock failed")
		}
		return followSQLResult{}, nil
	case strings.Contains(query, "INSERT INTO `follows`"):
		if !c.tx.locked || c.f.fail == "insert" {
			return nil, errors.New("insert failed")
		}
		row := insertFollowColumns(query, args)
		f := domain.Follow{ID: uint64(row["id"].(int64)), FollowerID: uint64(row["follower_id"].(int64)), FolloweeID: uint64(row["followee_id"].(int64)), CreatedAt: row["created_at"].(int64)}
		c.tx.follows = append(c.tx.follows, f)
		return followSQLResult{id: int64(f.ID)}, nil
	case strings.HasPrefix(query, "UPDATE `follows`"):
		if !c.tx.locked || c.f.fail == "delete" {
			return nil, errors.New("delete failed")
		}
		if !strings.Contains(query, "id = ? AND follower_id = ? AND followee_id = ? AND deleted_at = 0") {
			return nil, errors.New("missing update identity predicate")
		}
		for i := range c.tx.follows {
			if c.tx.follows[i].ID == uint64(args[1].Value.(int64)) {
				c.tx.follows[i].DeletedAt = args[0].Value.(int64)
			}
		}
		if c.f.fail == "rowcount" {
			return followSQLZeroResult{}, nil
		}
		return followSQLResult{}, nil
	case strings.Contains(query, "INSERT INTO `outbox_events`"):
		if !c.tx.locked || c.f.fail == "outbox" {
			return nil, errors.New("outbox failed")
		}
		row := insertFollowColumns(query, args)
		key := row["dedup_key"].(string)
		for _, event := range c.tx.events {
			if *event.DedupKey == key {
				return nil, errors.New("duplicate event key")
			}
		}
		c.tx.events = append(c.tx.events, domain.OutboxEvent{ID: uint64(len(c.tx.events) + 1), EventType: row["event_type"].(string), Payload: row["payload"].(string), DedupKey: &key, CreatedAt: row["created_at"].(int64)})
		return followSQLResult{id: int64(len(c.tx.events))}, nil
	default:
		return nil, errors.New("unexpected SQL")
	}
}
func newAtomicFollowFixture(t *testing.T, f *followSQLFixture) (*followservice.FollowService, *followRepo) {
	t.Helper()
	if f.nextID == 0 {
		f.nextID = 9007199254740997
	}
	pool := sql.OpenDB(followSQLConnector{f})
	pool.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	repo := &followRepo{db: db, newRelationID: func() (uint64, error) {
		if f.fail == "identity" {
			return 0, errors.New("identity unavailable")
		}
		f.nextID++
		return f.nextID, nil
	}}
	svc := followservice.NewFollowService(repo, nil, nil, nil)
	svc.SetOutboxDependencies(uow.NewGormManager(db), tweetrepo.NewMysqlOutboxEventRepo(db))
	return svc, repo
}
func TestFollowOutboxAtomicLifecycleRepeatedRequestsAndRefollowGeneration(t *testing.T) {
	f := &followSQLFixture{}
	svc, _ := newAtomicFollowFixture(t, f)
	traceID, _ := trace.TraceIDFromHex("00112233445566778899aabbccddeeff")
	spanID, _ := trace.SpanIDFromHex("0011223344556677")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled}))
	require.NoError(t, svc.Follow(ctx, 17, 42))
	require.ErrorIs(t, svc.Follow(ctx, 17, 42), followservice.ErrAlreadyFollowing)
	require.Len(t, f.follows, 1)
	require.Len(t, f.events, 1)
	require.LessOrEqual(t, f.budget, 3*time.Second)
	var event events.FollowRelationEvent
	require.NoError(t, json.Unmarshal([]byte(f.events[0].Payload), &event))
	require.NoError(t, event.Validate(true))
	require.NotEmpty(t, event.Traceparent)
	require.Equal(t, f.follows[0].ID, event.RelationID)
	var legacy events.UserFollowedEvent
	require.NoError(t, json.Unmarshal([]byte(f.events[0].Payload), &legacy))
	require.Equal(t, uint64(17), legacy.FollowerID)
	require.NoError(t, svc.Unfollow(ctx, 17, 42))
	require.ErrorIs(t, svc.Unfollow(ctx, 17, 42), followservice.ErrNotFollowing)
	require.Len(t, f.events, 2)
	require.Positive(t, f.follows[0].DeletedAt)
	require.NoError(t, svc.Follow(ctx, 17, 42))
	require.Len(t, f.events, 3)
	require.Len(t, f.follows, 2)
	require.NotEqual(t, f.follows[0].ID, f.follows[1].ID)
	require.NotEqual(t, *f.events[0].DedupKey, *f.events[2].DedupKey)
}
func TestFollowOutboxFailuresRollbackFactAndEvent(t *testing.T) {
	for _, following := range []bool{true, false} {
		for _, phase := range []string{"begin", "lock", "read", "identity", "insert", "delete", "rowcount", "outbox", "commit"} {
			if following && (phase == "delete" || phase == "rowcount") || !following && (phase == "insert" || phase == "identity") {
				continue
			}
			t.Run(phase+map[bool]string{true: "_follow", false: "_unfollow"}[following], func(t *testing.T) {
				f := &followSQLFixture{fail: phase}
				if !following {
					f.follows = []domain.Follow{{ID: 61, FollowerID: 17, FolloweeID: 42, CreatedAt: 1}}
				}
				svc, _ := newAtomicFollowFixture(t, f)
				var err error
				if following {
					err = svc.Follow(context.Background(), 17, 42)
				} else {
					err = svc.Unfollow(context.Background(), 17, 42)
				}
				require.Error(t, err)
				require.Empty(t, f.events)
				if following {
					require.Empty(t, f.follows)
				} else {
					require.Zero(t, f.follows[0].DeletedAt)
				}
			})
		}
	}
}
func TestFollowOutboxConcurrentDuplicateAndExistingDuplicateFailClosed(t *testing.T) {
	f := &followSQLFixture{}
	svc, repo := newAtomicFollowFixture(t, f)
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- svc.Follow(context.Background(), 17, 42) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, followservice.ErrAlreadyFollowing)
		}
	}
	require.Equal(t, 1, success)
	require.Len(t, f.follows, 1)
	require.Len(t, f.events, 1)
	_, err := repo.ApplyFollowTransition(context.Background(), 17, 42, true)
	require.ErrorIs(t, err, domain.ErrFollowMutationUnavailable)
	require.ErrorIs(t, repo.Follow(context.Background(), 17, 42), domain.ErrFollowMutationUnavailable)
	require.ErrorIs(t, repo.Unfollow(context.Background(), 17, 42), domain.ErrFollowMutationUnavailable)
	f.follows = append(f.follows, domain.Follow{ID: 99, FollowerID: 17, FolloweeID: 42})
	require.ErrorIs(t, svc.Unfollow(context.Background(), 17, 42), domain.ErrFollowDuplicateActive)
	require.Len(t, f.events, 1)
}
func TestFollowOutboxValidationCancellationMissingDependenciesAndUncertainCommit(t *testing.T) {
	f := &followSQLFixture{}
	svc, _ := newAtomicFollowFixture(t, f)
	require.ErrorIs(t, svc.Follow(context.Background(), 0, 42), followservice.ErrInvalidFollowIdentity)
	require.ErrorIs(t, svc.Follow(context.Background(), 17, 17), followservice.ErrCannotFollowSelf)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, svc.Follow(ctx, 17, 42), context.Canceled)
	require.Empty(t, f.events)
	missing := followservice.NewFollowService(nil, nil, nil, nil)
	require.ErrorIs(t, missing.Follow(context.Background(), 17, 42), domain.ErrFollowMutationUnavailable)
	f.fail = "commit_unknown"
	require.Error(t, svc.Follow(context.Background(), 17, 42))
	require.Len(t, f.follows, 1)
	require.Len(t, f.events, 1)
	f.fail = ""
	require.ErrorIs(t, svc.Follow(context.Background(), 17, 42), followservice.ErrAlreadyFollowing)
	require.Len(t, f.events, 1)
}

func TestFollowOutboxEventKeyConflictRollsBackNewFact(t *testing.T) {
	for _, following := range []bool{true, false} {
		f := &followSQLFixture{nextID: 60}
		if !following {
			f.follows = []domain.Follow{{ID: 61, FollowerID: 17, FolloweeID: 42, CreatedAt: 1}}
		}
		key := events.FollowRelationEventKey(61, following)
		f.events = []domain.OutboxEvent{{ID: 91, DedupKey: &key, Payload: "historical conflicting payload"}}
		svc, _ := newAtomicFollowFixture(t, f)
		var err error
		if following {
			err = svc.Follow(context.Background(), 17, 42)
		} else {
			err = svc.Unfollow(context.Background(), 17, 42)
		}
		require.ErrorContains(t, err, "duplicate event key")
		require.Len(t, f.events, 1)
		require.Equal(t, "historical conflicting payload", f.events[0].Payload)
		if following {
			require.Empty(t, f.follows)
		} else {
			require.Zero(t, f.follows[0].DeletedAt)
		}
	}
}
