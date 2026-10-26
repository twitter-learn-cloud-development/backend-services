package repository

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
	"time"
)

type personaSQLFixture struct {
	t             *testing.T
	owner         int64
	value         string
	missing       bool
	fail          bool
	reads, writes int
	writeArgs     []driver.NamedValue
}
type personaConnector struct{ f *personaSQLFixture }
type personaDriver struct{}
type personaResult struct{}

func (personaResult) LastInsertId() (int64, error) { return 7, nil }
func (personaResult) RowsAffected() (int64, error) { return 1, nil }

type personaConn struct{ f *personaSQLFixture }
type personaRows struct {
	f         *personaSQLFixture
	delivered bool
}

func (c personaConnector) Connect(context.Context) (driver.Conn, error) {
	return &personaConn{c.f}, nil
}
func (personaConnector) Driver() driver.Driver         { return personaDriver{} }
func (personaDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector required") }
func (*personaConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unexpected")
}
func (*personaConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (*personaConn) Close() error              { return nil }
func (*personaRows) Columns() []string         { return []string{"user_id", "persona"} }
func (*personaRows) Close() error              { return nil }
func (r *personaRows) Next(values []driver.Value) error {
	if r.delivered || r.f.missing {
		return io.EOF
	}
	r.delivered = true
	values[0] = r.f.owner
	values[1] = r.f.value
	return nil
}
func (c *personaConn) bounded(ctx context.Context) {
	deadline, ok := ctx.Deadline()
	require.True(c.f.t, ok)
	require.LessOrEqual(c.f.t, time.Until(deadline), 3*time.Second)
}
func (c *personaConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.bounded(ctx)
	c.f.reads++
	require.Contains(c.f.t, q, "SELECT `user_id`,`persona` FROM `agent_personas` WHERE user_id = ? LIMIT ?")
	require.Equal(c.f.t, int64(7), args[0].Value)
	require.Equal(c.f.t, int64(1), args[1].Value)
	if c.f.fail {
		return nil, errors.New("private SQL failure")
	}
	return &personaRows{f: c.f}, nil
}
func (c *personaConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.bounded(ctx)
	c.f.writes++
	c.f.writeArgs = args
	require.True(c.f.t, strings.HasPrefix(q, "INSERT INTO `agent_personas`"))
	require.Contains(c.f.t, q, "ON DUPLICATE KEY UPDATE `persona`=VALUES(`persona`),`updated_at`=VALUES(`updated_at`)")
	if c.f.fail {
		return nil, errors.New("private SQL failure")
	}
	return personaResult{}, nil
}
func TestPersonaSQLBoundedOwnerReadAndUpsertWithoutDDL(t *testing.T) {
	fixture := &personaSQLFixture{t: t, owner: 7, value: "Go"}
	sqlDB := sql.OpenDB(personaConnector{fixture})
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	store := SQLPersonaStore{DB: db}
	ctx := context.Background()
	value, err := store.Get(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, "Go", value)
	fixture.missing = true
	value, err = store.Get(ctx, 7)
	require.NoError(t, err)
	require.Empty(t, value)
	fixture.missing = false
	fixture.owner = 8
	_, err = store.Get(ctx, 7)
	require.Error(t, err)
	fixture.owner = 7
	fixture.fail = true
	_, err = store.Get(ctx, 7)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private")
	fixture.fail = false
	require.NoError(t, store.Put(ctx, 7, "  value  "))
	// GORM places the auto-increment primary field after the mutable columns.
	require.Equal(t, "value", fixture.writeArgs[0].Value)
	require.Equal(t, int64(7), fixture.writeArgs[len(fixture.writeArgs)-1].Value)
	reads, writes := fixture.reads, fixture.writes
	_, err = store.Get(ctx, 0)
	require.Error(t, err)
	require.Error(t, store.Put(ctx, 7, strings.Repeat("x", 65537)))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.Error(t, store.Put(canceled, 7, "discard"))
	require.Equal(t, reads, fixture.reads)
	require.Equal(t, writes, fixture.writes)
}
