package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io"
	"strings"
	"testing"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/pkg/database/uow"
)

// Executes the production GORM adapter without MySQL. A conflict reports an
// affected row, as ClientFoundRows can; creation must follow the persisted ID.
type likeSQLFixture struct {
	row                *domain.Like
	sql                []string
	commits, rollbacks int
	failInsert         bool
}
type likeSQLConnector struct{ f *likeSQLFixture }
type likeSQLDriver struct{}
type likeSQLConn struct{ f *likeSQLFixture }
type likeSQLTx struct {
	f      *likeSQLFixture
	before *domain.Like
}
type likeSQLRows struct {
	row  *domain.Like
	done bool
}
type likeSQLResult struct{}

func (likeSQLResult) LastInsertId() (int64, error) { return 0, nil }
func (likeSQLResult) RowsAffected() (int64, error) { return 1, nil }

func (c likeSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &likeSQLConn{c.f}, nil
}
func (likeSQLConnector) Driver() driver.Driver         { return likeSQLDriver{} }
func (likeSQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }
func (*likeSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*likeSQLConn) Close() error { return nil }
func (c *likeSQLConn) Begin() (driver.Tx, error) {
	var before *domain.Like
	if c.f.row != nil {
		copy := *c.f.row
		before = &copy
	}
	return &likeSQLTx{c.f, before}, nil
}
func (tx *likeSQLTx) Commit() error   { tx.f.commits++; return nil }
func (tx *likeSQLTx) Rollback() error { tx.f.rollbacks++; tx.f.row = tx.before; return nil }
func (c *likeSQLConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.f.sql = append(c.f.sql, q)
	if !strings.HasPrefix(q, "INSERT INTO `likes` (`user_id`,`tweet_id`,`created_at`,`id`)") || !strings.Contains(q, "ON DUPLICATE KEY UPDATE") || len(args) != 4 {
		return nil, fmt.Errorf("unexpected mutation SQL: %s %+v", q, args)
	}
	if c.f.failInsert {
		return nil, errors.New("insert unavailable")
	}
	if c.f.row == nil {
		c.f.row = &domain.Like{ID: uint64(args[3].Value.(int64)), UserID: uint64(args[0].Value.(int64)), TweetID: uint64(args[1].Value.(int64)), CreatedAt: args[2].Value.(int64)}
	}
	return likeSQLResult{}, nil
}
func (c *likeSQLConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.f.sql = append(c.f.sql, q)
	if !strings.Contains(q, "FOR UPDATE") || !strings.Contains(q, "user_id = ? AND tweet_id = ?") || len(args) != 3 || args[0].Value != int64(7) || args[1].Value != int64(91) || args[2].Value != int64(1) {
		return nil, fmt.Errorf("unbounded or nonlocking identity read: %s %+v", q, args)
	}
	return &likeSQLRows{row: c.f.row}, nil
}
func (*likeSQLRows) Columns() []string { return []string{"id", "user_id", "tweet_id", "created_at"} }
func (*likeSQLRows) Close() error      { return nil }
func (r *likeSQLRows) Next(values []driver.Value) error {
	if r.done || r.row == nil {
		return io.EOF
	}
	r.done = true
	values[0] = int64(r.row.ID)
	values[1] = int64(r.row.UserID)
	values[2] = int64(r.row.TweetID)
	values[3] = r.row.CreatedAt
	return nil
}
func likeSQLRepository(t *testing.T, f *likeSQLFixture) *likeRepo {
	t.Helper()
	raw := sql.OpenDB(likeSQLConnector{f})
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: raw, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	next := uint64(100)
	return &likeRepo{db: db, newLikeID: func() (uint64, error) { next++; return next, nil }}
}
func TestApplyLikeUsesPersistedIdentityAndRequiresTransaction(t *testing.T) {
	f := &likeSQLFixture{}
	r := likeSQLRepository(t, f)
	ctx := context.Background()
	_, _, err := r.ApplyLike(ctx, 7, 91)
	require.Error(t, err)
	require.Empty(t, f.sql)
	manager := uow.NewGormManager(r.db)
	var first, second *domain.Like
	require.NoError(t, manager.Do(ctx, func(tx context.Context) error {
		row, created, err := r.ApplyLike(tx, 7, 91)
		require.NoError(t, err)
		require.True(t, created)
		first = row
		return err
	}))
	require.Equal(t, uint64(101), first.ID)
	require.NoError(t, manager.Do(ctx, func(tx context.Context) error {
		row, created, err := r.ApplyLike(tx, 7, 91)
		require.False(t, created)
		second = row
		return err
	}))
	require.Equal(t, first, second)
	require.Equal(t, 2, f.commits)
	// A new relationship after physical unlike has a new event identity.
	f.row = nil
	require.NoError(t, manager.Do(ctx, func(tx context.Context) error {
		row, created, err := r.ApplyLike(tx, 7, 91)
		require.True(t, created)
		require.NotEqual(t, first.ID, row.ID)
		return err
	}))
}
func TestApplyLikeRollbackAndWrongPersistedIdentity(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		f := &likeSQLFixture{}
		r := likeSQLRepository(t, f)
		if wrong {
			f.row = &domain.Like{ID: 3, UserID: 8, TweetID: 91, CreatedAt: 1}
		}
		err := uow.NewGormManager(r.db).Do(context.Background(), func(tx context.Context) error {
			_, _, err := r.ApplyLike(tx, 7, 91)
			if err != nil {
				return err
			}
			return errors.New("outbox insert failed")
		})
		require.Error(t, err)
		require.Equal(t, 1, f.rollbacks)
		if !wrong {
			require.Nil(t, f.row)
		}
	}
}
