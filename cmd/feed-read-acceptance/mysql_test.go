package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// A small database/sql fixture exercises actual GORM queries and read-only
// BeginTx/Rollback wiring, without adding a driver or opening a network socket.
type sqlFixture struct {
	queriesMu                    sync.Mutex
	queries                      []string
	begun, rolledBack, committed atomic.Int32
	badOptions                   atomic.Bool
	followees                    int
}
type fixtureConnector struct{ fixture *sqlFixture }

func (c fixtureConnector) Connect(context.Context) (driver.Conn, error) {
	return &fixtureConn{fixture: c.fixture}, nil
}
func (fixtureConnector) Driver() driver.Driver { return fixtureDriver{} }

type fixtureDriver struct{}

func (fixtureDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type fixtureConn struct{ fixture *sqlFixture }

func (*fixtureConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*fixtureConn) Close() error              { return nil }
func (*fixtureConn) Begin() (driver.Tx, error) { return nil, errors.New("read-only BeginTx required") }
func (c *fixtureConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if !opts.ReadOnly || opts.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
		c.fixture.badOptions.Store(true)
	}
	c.fixture.begun.Add(1)
	return fixtureTx{c.fixture}, nil
}

type fixtureTx struct{ fixture *sqlFixture }

func (t fixtureTx) Commit() error   { t.fixture.committed.Add(1); return nil }
func (t fixtureTx) Rollback() error { t.fixture.rolledBack.Add(1); return nil }

type fixtureRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *fixtureRows) Columns() []string { return r.columns }
func (*fixtureRows) Close() error        { return nil }
func (r *fixtureRows) Next(dest []driver.Value) error {
	if r.index == len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}
func (c *fixtureConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	f := c.fixture
	f.queriesMu.Lock()
	f.queries = append(f.queries, query)
	f.queriesMu.Unlock()
	switch {
	case strings.HasPrefix(query, "SELECT follower_id FROM follows"):
		return &fixtureRows{columns: []string{"follower_id"}, rows: [][]driver.Value{{int64(1)}}}, nil
	case strings.HasPrefix(query, "SELECT DISTINCT followee_id FROM follows"):
		limit := args[1].Value.(int64)
		r := &fixtureRows{columns: []string{"followee_id"}}
		for i := 0; i < min(f.followees, int(limit)); i++ {
			r.rows = append(r.rows, []driver.Value{int64(i + 2)})
		}
		return r, nil
	case strings.Contains(query, "information_schema.TABLES"):
		return &fixtureRows{columns: []string{"TABLE_ROWS"}, rows: [][]driver.Value{{int64(1000000)}}}, nil
	case strings.HasPrefix(query, "EXPLAIN FORMAT=JSON"):
		return &fixtureRows{columns: []string{"EXPLAIN"}, rows: [][]driver.Value{{`{"query_block":{"table":{"table_name":"tweets","access_type":"range","key":"PRIMARY","rows_examined_per_scan":101,"attached_condition":"private raw predicate"}}}`}}}, nil
	case strings.Contains(query, "FROM `tweets`"):
		before := int64(0)
		if strings.Contains(query, "tweets.id < ?") {
			before = args[len(args)-2].Value.(int64)
		}
		limit := int(args[len(args)-1].Value.(int64))
		r := &fixtureRows{columns: []string{"id", "user_id", "visible_type", "deleted_at"}}
		for id := int64(9007199254740999); id >= 9007199254740995; id-- {
			if before != 0 && id >= before {
				continue
			}
			if len(r.rows) == limit {
				break
			}
			r.rows = append(r.rows, []driver.Value{id, int64(2), int64(0), int64(0)})
		}
		return r, nil
	case strings.Contains(query, "FROM `follows`"):
		return &fixtureRows{columns: []string{"followee_id"}, rows: [][]driver.Value{{int64(2)}}}, nil
	default:
		return nil, errors.New("unexpected or non-read SQL")
	}
}

func TestSQLPreflightAndIndependentReadOnlyJourneyTransactions(t *testing.T) {
	f := &sqlFixture{followees: 200}
	pool := sql.OpenDB(fixtureConnector{f})
	defer pool.Close()
	pool.SetMaxOpenConns(2)
	o := testOptions()
	o.viewer = 0
	o.discover = true
	o.minFollowees = 105
	o.minPages = 3
	o.minRows = 5
	d, plan, selection, err := preflight(context.Background(), pool, &o)
	if err != nil || selection != "recent_active_follow" || o.viewer != 1 || d.FolloweesObserved != 106 || !d.FolloweeProbeSaturated || !d.TweetEstimateAvailable || d.TweetRowsEstimate != 1000000 || len(plan) != 1 {
		t.Fatalf("%+v %v %s %v", d, plan, selection, err)
	}
	r := measure(context.Background(), o, func(ctx context.Context) (readSession, error) { return openMySQLSession(ctx, pool) })
	if r.Completed != 4 || r.Failed != 0 || r.Returned != 20 || r.NonemptyPages != 12 || r.SuccessfulPageMS.Count != 12 {
		t.Fatalf("%+v", r)
	}
	if f.badOptions.Load() || f.committed.Load() != 0 || f.begun.Load() != 5 || f.rolledBack.Load() != 5 {
		t.Fatalf("tx began=%d rollback=%d commit=%d bad=%v", f.begun.Load(), f.rolledBack.Load(), f.committed.Load(), f.badOptions.Load())
	}
	f.queriesMu.Lock()
	defer f.queriesMu.Unlock()
	for _, query := range f.queries {
		if !strings.HasPrefix(query, "SELECT ") && !strings.HasPrefix(query, "EXPLAIN ") {
			t.Fatal("unexpected SQL", query)
		}
	}
}

func TestSQLPreflightRejectsSmallFollowFixture(t *testing.T) {
	f := &sqlFixture{followees: 1}
	pool := sql.OpenDB(fixtureConnector{f})
	defer pool.Close()
	o := testOptions()
	o.minFollowees = 101
	_, _, _, err := preflight(context.Background(), pool, &o)
	if err == nil || err.Error() != "insufficient active-follow fixture" || f.begun.Load() != 1 || f.rolledBack.Load() != 1 {
		t.Fatal(err)
	}
}

func TestExplainInputIsBounded(t *testing.T) {
	if _, err := summarizePlan(strings.Repeat(" ", (1<<20)+1)); err == nil {
		t.Fatal("unbounded EXPLAIN")
	}
}
