package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type projectionSQLFixture struct {
	acquired, released      driver.Value
	absent                  bool
	closed                  int
	queries                 []string
	releaseContextCancelled bool
}
type projectionSQLConnector struct{ fixture *projectionSQLFixture }
type projectionSQLDriver struct{}
type projectionSQLConn struct{ fixture *projectionSQLFixture }

func (c projectionSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &projectionSQLConn{fixture: c.fixture}, nil
}
func (projectionSQLConnector) Driver() driver.Driver { return projectionSQLDriver{} }
func (projectionSQLDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("connector only")
}
func (*projectionSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *projectionSQLConn) Close() error { c.fixture.closed++; return nil }
func (*projectionSQLConn) Begin() (driver.Tx, error) {
	return nil, errors.New("projection must not open a SQL transaction")
}
func (c *projectionSQLConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.fixture.queries = append(c.fixture.queries, query)
	if strings.Contains(query, "RELEASE_LOCK") {
		c.fixture.releaseContextCancelled = ctx.Err() != nil
		if len(args) != 1 || args[0].Value != "tweet:projection:17" {
			return nil, errors.New("wrong release identity")
		}
		return &projectionSQLRows{columns: []string{"released"}, values: []driver.Value{c.fixture.released}}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(query, "GET_LOCK") {
		if len(args) != 1 || args[0].Value != "tweet:projection:17" || !strings.Contains(query, ", 5)") {
			return nil, errors.New("wrong bounded lock identity")
		}
		return &projectionSQLRows{columns: []string{"acquired"}, values: []driver.Value{c.fixture.acquired}}, nil
	}
	if strings.HasPrefix(query, "SELECT") && strings.Contains(query, "`tweets`") {
		rows := &projectionSQLRows{columns: []string{"id", "user_id", "content", "deleted_at"}, values: []driver.Value{int64(17), int64(42), "current", int64(123)}}
		if c.fixture.absent {
			rows.values = nil
		}
		return rows, nil
	}
	return nil, errors.New("unexpected projection query")
}

type projectionSQLRows struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (r *projectionSQLRows) Columns() []string { return r.columns }
func (*projectionSQLRows) Close() error        { return nil }
func (r *projectionSQLRows) Next(values []driver.Value) error {
	if r.done || r.values == nil {
		return io.EOF
	}
	r.done = true
	copy(values, r.values)
	return nil
}

func newProjectionSQLRepo(t *testing.T, f *projectionSQLFixture) *tweetRepo {
	t.Helper()
	conn := sql.OpenDB(projectionSQLConnector{fixture: f})
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return &tweetRepo{db: db}
}

func TestProjectionRepositoryPinsSessionAndReadsDeletedFactWithoutTransaction(t *testing.T) {
	f := &projectionSQLFixture{acquired: int64(1), released: int64(1)}
	r := newProjectionSQLRepo(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, r.WithProjectionLock(ctx, 17, func(locked context.Context) error {
		tweet, err := r.ReadProjectionTweet(locked, 17)
		require.NoError(t, err)
		require.Equal(t, uint64(17), tweet.ID)
		require.Equal(t, int64(123), tweet.DeletedAt)
		_, err = r.ReadProjectionTweet(locked, 18)
		require.Error(t, err)
		return nil
	}))
	require.Len(t, f.queries, 3)
	require.Contains(t, f.queries[0], "GET_LOCK")
	require.Contains(t, f.queries[2], "RELEASE_LOCK")
	require.NotContains(t, f.queries[1], "deleted_at = 0")
	require.Zero(t, f.closed)
	_, err := r.ReadProjectionTweet(context.Background(), 17)
	require.Error(t, err)
}

func TestProjectionRepositoryDiscardsUncertainLockSessions(t *testing.T) {
	for _, phase := range []string{"acquire_timeout", "acquire_null", "release_null"} {
		t.Run(phase, func(t *testing.T) {
			f := &projectionSQLFixture{acquired: int64(1), released: int64(1)}
			if phase == "acquire_timeout" {
				f.acquired = int64(0)
			}
			if phase == "acquire_null" {
				f.acquired = nil
			}
			if phase == "release_null" {
				f.released = nil
			}
			r := newProjectionSQLRepo(t, f)
			called := false
			require.Error(t, r.WithProjectionLock(context.Background(), 17, func(context.Context) error { called = true; return nil }))
			require.Equal(t, phase == "release_null", called)
			require.Equal(t, 1, f.closed)
		})
	}
}

func TestProjectionRepositoryReleasesAfterCancellationAndReturnsAbsentFact(t *testing.T) {
	f := &projectionSQLFixture{acquired: int64(1), released: int64(1), absent: true}
	r := newProjectionSQLRepo(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := r.WithProjectionLock(ctx, 17, func(locked context.Context) error {
		tweet, err := r.ReadProjectionTweet(locked, 17)
		require.NoError(t, err)
		require.Nil(t, tweet)
		cancel()
		return ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, f.releaseContextCancelled)
	require.Contains(t, f.queries[len(f.queries)-1], "RELEASE_LOCK")
}
