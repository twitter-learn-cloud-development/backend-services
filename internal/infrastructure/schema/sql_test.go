package schema

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
)

type schemaSQLFixture struct {
	meta               map[string]Metadata
	table              Table
	queries            []string
	acquired, released int64
	releaseCancelled   bool
	closed             int
	executed           []string
}
type schemaSQLConnector struct{ f *schemaSQLFixture }
type schemaSQLDriver struct{}
type schemaSQLConn struct{ f *schemaSQLFixture }
type schemaSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (c schemaSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &schemaSQLConn{c.f}, nil
}
func (schemaSQLConnector) Driver() driver.Driver         { return schemaSQLDriver{} }
func (schemaSQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }
func (*schemaSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unexpected")
}
func (*schemaSQLConn) Begin() (driver.Tx, error) {
	return nil, errors.New("DDL/metadata must not open transaction")
}
func (c *schemaSQLConn) Close() error      { c.f.closed++; return nil }
func (r *schemaSQLRows) Columns() []string { return r.columns }
func (*schemaSQLRows) Close() error        { return nil }
func (r *schemaSQLRows) Next(values []driver.Value) error {
	if r.index == len(r.values) {
		return io.EOF
	}
	copy(values, r.values[r.index])
	r.index++
	return nil
}
func (c *schemaSQLConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.f.queries = append(c.f.queries, q)
	scalar := func(name string, value driver.Value) driver.Rows {
		return &schemaSQLRows{columns: []string{name}, values: [][]driver.Value{{value}}}
	}
	if q == "SELECT DATABASE()" {
		return scalar("database", "twitter"), nil
	}
	if strings.Contains(q, "GET_LOCK") {
		if !strings.Contains(q, ", 2)") || len(args) != 1 || !strings.HasPrefix(args[0].Value.(string), "schema:bootstrap:") {
			return nil, errors.New("unbounded lock")
		}
		return scalar("acquired", c.f.acquired), nil
	}
	if strings.Contains(q, "RELEASE_LOCK") {
		c.f.releaseCancelled = ctx.Err() != nil
		return scalar("released", c.f.released), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) != 1 || !strings.Contains(q, "TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?") || !strings.Contains(q, "LIMIT ") {
		return nil, errors.New("unbounded/unparameterized metadata query")
	}
	name := args[0].Value.(string)
	meta := c.f.meta[name]
	switch {
	case strings.Contains(q, "information_schema.TABLES"):
		rows := &schemaSQLRows{columns: []string{"ENGINE"}}
		if meta.Engine != "" {
			rows.values = [][]driver.Value{{meta.Engine}}
		}
		return rows, nil
	case strings.Contains(q, "information_schema.COLUMNS"):
		rows := &schemaSQLRows{columns: []string{"COLUMN_NAME", "COLUMN_TYPE", "IS_NULLABLE"}}
		for _, column := range meta.Columns {
			rows.values = append(rows.values, []driver.Value{column.Name, column.Type, column.Nullable})
		}
		return rows, nil
	case strings.Contains(q, "information_schema.STATISTICS"):
		rows := &schemaSQLRows{columns: []string{"INDEX_NAME", "NON_UNIQUE", "SEQ_IN_INDEX", "COLUMN_NAME", "SUB_PART"}}
		for _, index := range meta.Indexes {
			var prefix driver.Value
			if index.Prefix.Valid {
				prefix = index.Prefix.Int64
			}
			rows.values = append(rows.values, []driver.Value{index.Name, int64(index.NonUnique), int64(index.Sequence), index.Column, prefix})
		}
		return rows, nil
	}
	return nil, errors.New("unexpected query")
}
func (c *schemaSQLConn) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(q, "CREATE TABLE `users`") {
		return nil, errors.New("cross-owner DDL")
	}
	c.f.executed = append(c.f.executed, q)
	c.f.meta["users"] = compatibleMetadata(c.f.table)
	return driver.RowsAffected(0), nil
}
func schemaSQLStore(t *testing.T, f *schemaSQLFixture) SQLBootstrapStore {
	t.Helper()
	raw := sql.OpenDB(schemaSQLConnector{f})
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: raw, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	require.NoError(t, err)
	return SQLBootstrapStore{DB: db}
}
func TestSQLBootstrapUsesSameConnectionAndVerifiedCreate(t *testing.T) {
	plan, err := BuildPlan("user")
	require.NoError(t, err)
	f := &schemaSQLFixture{meta: map[string]Metadata{}, table: plan.Tables[0], acquired: 1, released: 1}
	store := schemaSQLStore(t, f)
	report, err := Apply(context.Background(), store, plan, plan.SHA256)
	require.NoError(t, err)
	require.Equal(t, "created_verified", report.Results[0].Outcome)
	require.Len(t, f.executed, 1)
	require.False(t, f.releaseCancelled)
	require.Error(t, store.Create(context.Background(), plan.Statements[0]))
	require.Len(t, f.executed, 1)
	for _, q := range f.queries {
		require.True(t, strings.HasPrefix(q, "SELECT"))
	}
	require.Contains(t, strings.Join(f.queries, "|"), "RELEASE_LOCK")
}
func TestSQLBootstrapLockFailureAndReleaseAfterCancel(t *testing.T) {
	f := &schemaSQLFixture{meta: map[string]Metadata{}, acquired: 0, released: 1}
	store := schemaSQLStore(t, f)
	ran := false
	err := store.WithLock(context.Background(), "user", func(context.Context) error { ran = true; return nil })
	require.Error(t, err)
	require.False(t, ran)
	f.acquired = 1
	ctx, cancel := context.WithCancel(context.Background())
	err = store.WithLock(ctx, "user", func(context.Context) error { cancel(); return context.Canceled })
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, f.releaseCancelled)
	f.released = 0
	err = store.WithLock(context.Background(), "user", func(context.Context) error { return nil })
	require.ErrorContains(t, err, "release uncertain")
}
