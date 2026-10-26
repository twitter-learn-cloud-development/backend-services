package projection

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io"
	"strings"
	"testing"
	"twitter-clone/internal/events"
)

type sqlFixture struct {
	receipts                     map[string]IngressReceipt
	tasks                        int
	failSecond, foundRows, stale bool
	queries                      []string
	releaseCancelled             bool
	lockValue                    driver.Value
}
type fixtureConnector struct{ f *sqlFixture }
type fixtureDriver struct{}
type fixtureConn struct {
	f  *sqlFixture
	tx *fixtureTx
}
type fixtureTx struct {
	c        *fixtureConn
	receipts map[string]IngressReceipt
	tasks    int
}

func (c fixtureConnector) Connect(context.Context) (driver.Conn, error) {
	return &fixtureConn{f: c.f}, nil
}
func (fixtureConnector) Driver() driver.Driver         { return fixtureDriver{} }
func (fixtureDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }
func (*fixtureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*fixtureConn) Close() error { return nil }
func (c *fixtureConn) Begin() (driver.Tx, error) {
	copy := map[string]IngressReceipt{}
	for key, value := range c.f.receipts {
		copy[key] = value
	}
	c.tx = &fixtureTx{c: c, receipts: copy, tasks: c.f.tasks}
	return c.tx, nil
}
func (t *fixtureTx) Commit() error {
	t.c.f.receipts = t.receipts
	t.c.f.tasks = t.tasks
	t.c.tx = nil
	return nil
}
func (t *fixtureTx) Rollback() error { t.c.tx = nil; return nil }

type fixtureRows struct {
	cols   []string
	values []driver.Value
	done   bool
}

func (r *fixtureRows) Columns() []string { return r.cols }
func (*fixtureRows) Close() error        { return nil }
func (r *fixtureRows) Next(out []driver.Value) error {
	if r.done || r.values == nil {
		return io.EOF
	}
	r.done = true
	copy(out, r.values)
	return nil
}

type fixtureResult struct{ affected int64 }

func (fixtureResult) LastInsertId() (int64, error)   { return 91, nil }
func (r fixtureResult) RowsAffected() (int64, error) { return r.affected, nil }
func (c *fixtureConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.f.queries = append(c.f.queries, query)
	if strings.Contains(query, "GET_LOCK") {
		requireKey := args[0].Value == "follow:projection:author:42"
		if !requireKey {
			return nil, errors.New("wrong author lock")
		}
		return &fixtureRows{cols: []string{"acquired"}, values: []driver.Value{c.f.lockValue}}, nil
	}
	if strings.Contains(query, "RELEASE_LOCK") {
		c.f.releaseCancelled = ctx.Err() != nil
		return &fixtureRows{cols: []string{"released"}, values: []driver.Value{int64(1)}}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(query, "follow_projection_receipts") {
		rows := c.f.receipts
		if c.tx != nil {
			rows = c.tx.receipts
		}
		receipt, ok := rows[args[0].Value.(string)]
		result := &fixtureRows{cols: []string{"event_key", "digest", "registration_token", "created_at"}}
		if ok {
			result.values = []driver.Value{receipt.EventKey, receipt.Digest, receipt.RegistrationToken, receipt.CreatedAt}
		}
		return result, nil
	}
	if strings.Contains(query, "COUNT") {
		value := int64(1)
		if c.f.stale {
			value = 0
		}
		return &fixtureRows{cols: []string{"count"}, values: []driver.Value{value}}, nil
	}
	return nil, errors.New("unexpected query")
}
func (c *fixtureConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.f.queries = append(c.f.queries, query)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.HasPrefix(query, "UPDATE `outbox_tasks`") {
		if c.f.stale {
			return fixtureResult{}, nil
		}
		return fixtureResult{1}, nil
	}
	if c.tx == nil {
		return nil, errors.New("registration escaped transaction")
	}
	if strings.Contains(query, "INSERT INTO `follow_projection_receipts`") {
		columns := strings.Split(strings.Split(strings.Split(query, "(")[1], ")")[0], ",")
		row := map[string]driver.Value{}
		for i, column := range columns {
			row[strings.Trim(column, " `")] = args[i].Value
		}
		key := row["event_key"].(string)
		if _, ok := c.tx.receipts[key]; ok {
			if c.f.foundRows {
				return fixtureResult{1}, nil
			}
			return fixtureResult{}, nil
		}
		c.tx.receipts[key] = IngressReceipt{EventKey: key, Digest: row["digest"].(string), RegistrationToken: row["registration_token"].(string), CreatedAt: row["created_at"].(int64)}
		return fixtureResult{1}, nil
	}
	if strings.Contains(query, "INSERT INTO `outbox_tasks`") {
		if c.f.failSecond && c.tx.tasks == 1 {
			return nil, errors.New("injected second registration failure")
		}
		c.tx.tasks++
		return fixtureResult{1}, nil
	}
	return nil, errors.New("unexpected write")
}
func sqlStoreFixture(t *testing.T, f *sqlFixture) *SQLStore {
	t.Helper()
	f.receipts = map[string]IngressReceipt{}
	f.lockValue = int64(1)
	pool := sql.OpenDB(fixtureConnector{f})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	id := uint64(100)
	return &SQLStore{DB: db, NewID: func() (uint64, error) { id++; return id, nil }}
}
func TestSQLFollowRegistrationIsAtomicAndDeduplicatesEvenWithClientFoundRows(t *testing.T) {
	f := &sqlFixture{failSecond: true}
	s := sqlStoreFixture(t, f)
	event := eventFixture()
	created, err := s.Register(context.Background(), event)
	require.Error(t, err)
	require.False(t, created)
	require.Zero(t, f.tasks)
	require.Empty(t, f.receipts)
	f.failSecond = false
	created, err = s.Register(context.Background(), event)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, 2, f.tasks)
	require.Len(t, f.receipts, 1)
	f.foundRows = true
	created, err = s.Register(context.Background(), event)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, 2, f.tasks)
	event.OccurredAtUnixMS++
	created, err = s.Register(context.Background(), event)
	require.ErrorContains(t, err, "identity conflict")
	require.False(t, created)
	require.Equal(t, 2, f.tasks)
}
func TestSQLFollowCheckpointFencesLeaseAndResetsOnlySuccessfulPageRetries(t *testing.T) {
	f := &sqlFixture{}
	s := sqlStoreFixture(t, f)
	task := taskFixture(PairTask)
	ok, err := s.Advance(context.Background(), task, Checkpoint{Event: eventFixture(), Cursor: 100}, false)
	require.NoError(t, err)
	require.True(t, ok)
	query := f.queries[len(f.queries)-1]
	for _, predicate := range []string{"lease_owner = ?", "lease_token = ?", "lease_until > ?", "status = ?", "id = ?"} {
		require.Contains(t, query, predicate)
	}
	require.Contains(t, query, "`retries`=?")
	require.Contains(t, query, "`payload`=?")
	f.stale = true
	ok, err = s.Advance(context.Background(), task, Checkpoint{}, true)
	require.NoError(t, err)
	require.False(t, ok)
}
func TestSQLFollowSessionLockIsBoundedAndReleasedAfterCancellation(t *testing.T) {
	f := &sqlFixture{}
	s := sqlStoreFixture(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	err := s.WithAuthorLock(ctx, 42, func(locked context.Context) error {
		conn, ok := locked.Value(sessionKey{}).(session)
		require.True(t, ok)
		require.Equal(t, uint64(42), conn.author)
		cancel()
		return locked.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, f.releaseCancelled)
	require.Contains(t, f.queries[0], ", 5)")
	require.Contains(t, f.queries[len(f.queries)-1], "RELEASE_LOCK")
	f.lockValue = nil
	require.Error(t, s.WithAuthorLock(context.Background(), 42, func(context.Context) error { t.Fatal("uncertain lock must not enter callback"); return nil }))
}

func TestSQLFollowInterruptionPreservesPayloadAndRestoresAttemptBudget(t *testing.T) {
	f := &sqlFixture{}
	s := sqlStoreFixture(t, f)
	ok, err := s.Interrupt(context.Background(), taskFixture(PairTask))
	require.NoError(t, err)
	require.True(t, ok)
	query := f.queries[len(f.queries)-1]
	require.Contains(t, query, "GREATEST(retries-1,0)")
	require.NotContains(t, query, "`payload`=")
	require.Contains(t, query, "lease_token = ?")
}
func TestSQLFollowMaintenanceRegistrationHasPersistentDedupAndCheckpointRepair(t *testing.T) {
	f := &sqlFixture{}
	s := sqlStoreFixture(t, f)
	count, err := s.RegisterRepair(context.Background(), Scope{Author: 42, Follower: 17}, "fixture-operation", strings.Repeat("a", 64), "fixture-operator", "fixture-reason")
	require.NoError(t, err)
	require.Equal(t, 2, count)
	f.foundRows = true
	count, err = s.RegisterRepair(context.Background(), Scope{Author: 42, Follower: 17}, "fixture-operation", strings.Repeat("a", 64), "fixture-operator", "fixture-reason")
	require.NoError(t, err)
	require.Zero(t, count)
	require.Equal(t, 2, f.tasks)
	for _, receipt := range f.receipts {
		require.NotContains(t, receipt.EventKey, "fixture-operator")
	}
	require.False(t, validCheckpoint(AuthorTask, Checkpoint{Repair: "forged", Event: eventFixture()}))
	require.True(t, validCheckpoint(AuthorTask, Checkpoint{Repair: strings.Repeat("a", 64), ApprovedPlan: strings.Repeat("b", 64), OperatorDigest: strings.Repeat("c", 64), ReasonDigest: strings.Repeat("d", 64), OperationDigest: strings.Repeat("e", 64), Event: events.FollowRelationEvent{FolloweeID: 42, SchemaVersion: "follow-projection-repair-v1"}}))
}
