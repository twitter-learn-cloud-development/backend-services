package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"twitter-clone/internal/domain"
)

// Exercises the real GORM transaction/SQL adapter without opening a socket.
// It does not simulate MySQL locking, commit uncertainty or execution plans.
type maintenanceStore struct {
	tasks, receipts    map[string]map[string]driver.Value
	failKey            string
	autoIncrement      bool
	commits, rollbacks int
}
type maintenanceConnector struct{ store *maintenanceStore }
type maintenanceDriver struct{}
type maintenanceConn struct {
	store           *maintenanceStore
	tasks, receipts map[string]map[string]driver.Value
}
type maintenanceTx struct{ conn *maintenanceConn }

func (c maintenanceConnector) Connect(context.Context) (driver.Conn, error) {
	return &maintenanceConn{store: c.store}, nil
}
func (maintenanceConnector) Driver() driver.Driver         { return maintenanceDriver{} }
func (maintenanceDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }
func (*maintenanceConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*maintenanceConn) Close() error { return nil }
func (c *maintenanceConn) Begin() (driver.Tx, error) {
	if c.tasks != nil {
		return nil, errors.New("nested transaction")
	}
	c.tasks = cloneMaintenanceRows(c.store.tasks)
	c.receipts = cloneMaintenanceRows(c.store.receipts)
	return maintenanceTx{c}, nil
}
func (t maintenanceTx) Commit() error {
	t.conn.store.tasks = t.conn.tasks
	t.conn.store.receipts = t.conn.receipts
	t.conn.tasks = nil
	t.conn.receipts = nil
	t.conn.store.commits++
	return nil
}
func (t maintenanceTx) Rollback() error {
	t.conn.tasks = nil
	t.conn.receipts = nil
	t.conn.store.rollbacks++
	return nil
}
func cloneMaintenanceRows(rows map[string]map[string]driver.Value) map[string]map[string]driver.Value {
	out := map[string]map[string]driver.Value{}
	for key, row := range rows {
		copyRow := map[string]driver.Value{}
		for column, value := range row {
			copyRow[column] = value
		}
		out[key] = copyRow
	}
	return out
}

type maintenanceResult struct{ id, affected int64 }

func (r maintenanceResult) LastInsertId() (int64, error) { return r.id, nil }
func (r maintenanceResult) RowsAffected() (int64, error) { return r.affected, nil }

func (c *maintenanceConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(query, "INSERT INTO ") || c.tasks == nil {
		return nil, errors.New("unexpected nontransactional write")
	}
	start, end := strings.Index(query, "("), strings.Index(query, ")")
	if start < 0 || end < start {
		return nil, errors.New("invalid insert")
	}
	columns := strings.Split(query[start+1:end], ",")
	if len(columns) != len(args) {
		return nil, errors.New("insert arguments mismatch")
	}
	row := map[string]driver.Value{}
	for i, column := range columns {
		row[strings.Trim(column, " `")] = args[i].Value
	}
	if strings.Contains(query, "`outbox_tasks`") {
		key, ok := row["dedup_key"].(string)
		if !ok {
			return nil, errors.New("missing task identity")
		}
		if key == c.store.failKey {
			return nil, errors.New("injected insert failure")
		}
		if _, ok := c.tasks[key]; ok {
			return maintenanceResult{}, nil
		}
		row["id"] = int64(len(c.tasks) + 1)
		c.tasks[key] = row
		return maintenanceResult{id: row["id"].(int64), affected: 1}, nil
	}
	if strings.Contains(query, "`outbox_publications`") {
		key := stringID(row["event_id"])
		if _, ok := c.receipts[key]; ok {
			return maintenanceResult{}, nil
		}
		c.receipts[key] = row
		return maintenanceResult{affected: 1}, nil
	}
	return nil, errors.New("unexpected table")
}
func stringID(value driver.Value) string {
	// Test identities fit within int64; real command IDs retain uint64 precision.
	if v, ok := value.(int64); ok {
		return strconv.FormatInt(v, 10)
	}
	return "unexpected"
}
func (c *maintenanceConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(query, "information_schema.COLUMNS") {
		extra := ""
		if c.store.autoIncrement {
			extra = "auto_increment"
		}
		return &projectionSQLRows{columns: []string{"EXTRA"}, values: []driver.Value{extra}}, nil
	}
	tasks, receipts := c.store.tasks, c.store.receipts
	if c.tasks != nil {
		tasks, receipts = c.tasks, c.receipts
	}
	var row map[string]driver.Value
	if strings.Contains(query, "`outbox_tasks`") && len(args) > 0 {
		row = tasks[args[0].Value.(string)]
	} else if strings.Contains(query, "`outbox_publications`") && len(args) > 0 {
		row = receipts[stringID(args[0].Value)]
	} else {
		return nil, errors.New("unexpected read")
	}
	if row == nil {
		return &projectionSQLRows{columns: []string{"id"}}, nil
	}
	columns := []string{}
	for column := range row {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	values := make([]driver.Value, len(columns))
	for i, column := range columns {
		values[i] = row[column]
	}
	return &projectionSQLRows{columns: columns, values: values}, nil
}
func newMaintenanceDB(t *testing.T, store *maintenanceStore) *gorm.DB {
	t.Helper()
	store.tasks = map[string]map[string]driver.Value{}
	store.receipts = map[string]map[string]driver.Value{}
	pool := sql.OpenDB(maintenanceConnector{store})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return db
}
func maintenanceTask(key, payload string) *domain.OutboxTask {
	return &domain.OutboxTask{TaskType: domain.ProjectionSyncText, DedupKey: &key, Payload: payload, MaxRetries: 5}
}

func TestProjectionRepairRepositoryAtomicPerSubjectAndPersistentDeduplication(t *testing.T) {
	store := &maintenanceStore{autoIncrement: true, failKey: "second"}
	repo := &outboxRepo{db: newMaintenanceDB(t, store)}
	count, err := repo.RegisterProjectionRepair(context.Background(), []*domain.OutboxTask{maintenanceTask("first", "one"), maintenanceTask("second", "two")})
	require.Error(t, err)
	require.Zero(t, count)
	require.Empty(t, store.tasks)
	require.Equal(t, 1, store.rollbacks)
	store.failKey = ""
	count, err = repo.RegisterProjectionRepair(context.Background(), []*domain.OutboxTask{maintenanceTask("first", "one"), maintenanceTask("second", "two")})
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Len(t, store.tasks, 2)
	count, err = repo.RegisterProjectionRepair(context.Background(), []*domain.OutboxTask{maintenanceTask("first", "one"), maintenanceTask("second", "two")})
	require.NoError(t, err)
	require.Zero(t, count)
	require.Len(t, store.tasks, 2)
	count, err = repo.RegisterProjectionRepair(context.Background(), []*domain.OutboxTask{maintenanceTask("third", "three"), maintenanceTask("first", "conflict")})
	require.ErrorContains(t, err, "identity conflict")
	require.Zero(t, count)
	require.Len(t, store.tasks, 2)
	store.autoIncrement = false
	_, err = repo.RegisterProjectionRepair(context.Background(), []*domain.OutboxTask{maintenanceTask("third", "three")})
	require.ErrorContains(t, err, "ID allocation")
	require.Len(t, store.tasks, 2)
}
func TestPublicationRepositoryRetainsFirstConfirmedReceiptAndRejectsIdentityConflict(t *testing.T) {
	store := &maintenanceStore{}
	repo := NewOutboxPublicationRepository(newMaintenanceDB(t, store))
	receipt := domain.OutboxPublication{EventID: 17, EventType: "TWEET_DELETED", PayloadDigest: strings.Repeat("a", 64), ConfirmedAt: 123, Operation: "first"}
	require.NoError(t, repo.RecordPublication(context.Background(), receipt))
	receipt.ConfirmedAt = 456
	receipt.Operation = "retry"
	require.NoError(t, repo.RecordPublication(context.Background(), receipt))
	persisted, err := repo.ReadPublication(context.Background(), 17)
	require.NoError(t, err)
	require.Equal(t, int64(123), persisted.ConfirmedAt)
	require.Equal(t, "first", persisted.Operation)
	receipt.PayloadDigest = strings.Repeat("b", 64)
	require.ErrorContains(t, repo.RecordPublication(context.Background(), receipt), "identity conflict")
	persisted, err = repo.ReadPublication(context.Background(), 17)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("a", 64), persisted.PayloadDigest)
	absent, err := repo.ReadPublication(context.Background(), 18)
	require.NoError(t, err)
	require.Nil(t, absent)
}
