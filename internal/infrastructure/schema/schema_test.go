package schema

import (
	"context"
	"database/sql"
	"errors"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"twitter-clone/internal/domain"
)

func compatibleMetadata(table Table) Metadata {
	m := Metadata{Engine: "InnoDB"}
	for _, column := range table.Columns {
		kind := "bigint"
		if column.Unsigned {
			kind += " unsigned"
		}
		nullable := "YES"
		if column.NotNull {
			nullable = "NO"
		}
		m.Columns = append(m.Columns, MetadataColumn{column.Name, kind, nullable})
	}
	for _, index := range table.Indexes {
		unique := 1
		if index.Unique {
			unique = 0
		}
		for i, column := range index.Columns {
			m.Indexes = append(m.Indexes, MetadataIndex{Name: index.Name, NonUnique: unique, Sequence: i + 1, Column: column})
		}
	}
	return m
}

type bootstrapFake struct {
	metadata  map[string]Metadata
	tables    map[string]Table
	created   []string
	fail      string
	readErr   bool
	locked    bool
	uncertain bool
}

func (f *bootstrapFake) Read(ctx context.Context, name string) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if f.readErr {
		return Metadata{}, errors.New("private metadata error")
	}
	return f.metadata[name], nil
}
func (f *bootstrapFake) WithLock(ctx context.Context, _ string, run func(context.Context) error) error {
	f.locked = true
	defer func() { f.locked = false }()
	return run(ctx)
}
func (f *bootstrapFake) Create(_ context.Context, statement Statement) error {
	if !f.locked {
		return errors.New("unlocked DDL")
	}
	f.created = append(f.created, statement.Table)
	if f.fail == statement.Table && !f.uncertain {
		return errors.New("create failed")
	}
	f.metadata[statement.Table] = compatibleMetadata(f.tables[statement.Table])
	if f.fail == statement.Table {
		return errors.New("response lost")
	}
	return nil
}
func fakeBootstrap(plan Plan) *bootstrapFake {
	f := &bootstrapFake{metadata: map[string]Metadata{}, tables: map[string]Table{}}
	for _, table := range plan.Tables {
		f.tables[table.Name] = table
	}
	return f
}
func TestOwnedBootstrapPlanIsStableAndScoped(t *testing.T) {
	for _, owner := range OwnerNames() {
		t.Run(owner, func(t *testing.T) {
			plan, err := BuildPlan(owner)
			require.NoError(t, err)
			require.NotEmpty(t, plan.Statements)
			for i := 0; i < 8; i++ {
				again, err := BuildPlan(owner)
				require.NoError(t, err)
				require.Equal(t, plan, again)
			}
			for _, statement := range plan.Statements {
				require.Equal(t, owner, TableOwner(statement.Table))
				require.True(t, strings.HasPrefix(statement.SQL, "CREATE TABLE `"))
				require.Contains(t, statement.SQL, "ENGINE=InnoDB")
				require.NotContains(t, statement.SQL, "FOREIGN KEY")
				require.NotContains(t, statement.SQL, ";")
			}
		})
	}
	_, err := BuildPlan("tweet", &domain.User{})
	require.Error(t, err)
	_, err = BuildPlan("unknown")
	require.Error(t, err)
}
func TestSchemaValidationRejectsIncompatibleIdentityAndIndexes(t *testing.T) {
	tables, err := Describe([]interface{}{&domain.Like{}})
	require.NoError(t, err)
	table := tables[0]
	require.NoError(t, Validate(table, compatibleMetadata(table)))
	for _, scenario := range []string{"engine", "missing", "signed", "nullable", "nonunique", "prefix", "reorder"} {
		t.Run(scenario, func(t *testing.T) {
			m := compatibleMetadata(table)
			switch scenario {
			case "engine":
				m.Engine = "MyISAM"
			case "missing":
				m.Columns = m.Columns[1:]
			case "signed":
				for i := range m.Columns {
					if m.Columns[i].Name == "id" {
						m.Columns[i].Type = "bigint"
					}
				}
			case "nullable":
				for i := range m.Columns {
					if m.Columns[i].Name == "id" {
						m.Columns[i].Nullable = "YES"
					}
				}
			case "nonunique":
				for i := range m.Indexes {
					if m.Indexes[i].Name == "uk_user_tweet" {
						m.Indexes[i].NonUnique = 1
					}
				}
			case "prefix":
				m.Indexes[0].Prefix = sql.NullInt64{Int64: 3, Valid: true}
			case "reorder":
				for i := range m.Indexes {
					if m.Indexes[i].Name == "uk_user_tweet" {
						m.Indexes[i].Column = "wrong"
					}
				}
			}
			require.Error(t, Validate(table, m))
		})
	}
}
func TestBootstrapPreflightRerunAndPartialDDL(t *testing.T) {
	plan, err := BuildPlan("follow")
	require.NoError(t, err)
	f := fakeBootstrap(plan)
	report, err := Apply(context.Background(), f, plan, plan.SHA256)
	require.NoError(t, err)
	require.Len(t, report.Results, len(plan.Tables))
	require.Len(t, f.created, len(plan.Tables))
	_, err = Apply(context.Background(), f, plan, plan.SHA256)
	require.NoError(t, err)
	require.Len(t, f.created, len(plan.Tables))
	f = fakeBootstrap(plan)
	m := compatibleMetadata(plan.Tables[len(plan.Tables)-1])
	m.Engine = "MyISAM"
	f.metadata[plan.Tables[len(plan.Tables)-1].Name] = m
	_, err = Apply(context.Background(), f, plan, plan.SHA256)
	require.Error(t, err)
	require.Empty(t, f.created)
	f = fakeBootstrap(plan)
	f.fail = plan.Statements[len(plan.Statements)-1].Table
	f.uncertain = true
	report, err = Apply(context.Background(), f, plan, plan.SHA256)
	require.Error(t, err)
	require.Equal(t, "partial_or_uncertain", report.Results[len(report.Results)-1].Outcome)
	count := len(f.created)
	f.fail = ""
	_, err = Apply(context.Background(), f, plan, plan.SHA256)
	require.NoError(t, err)
	require.Len(t, f.created, count)
}
func TestBootstrapApprovalAndCancellationPreventDDL(t *testing.T) {
	plan, err := BuildPlan("user")
	require.NoError(t, err)
	f := fakeBootstrap(plan)
	_, err = Apply(context.Background(), f, plan, "bad")
	require.Error(t, err)
	require.Empty(t, f.created)
	tampered := plan
	tampered.Statements = append([]Statement{}, plan.Statements...)
	tampered.Statements[0].SQL += ";DROP TABLE users"
	tampered.SHA256 = planDigest(tampered)
	_, err = Apply(context.Background(), f, tampered, tampered.SHA256)
	require.Error(t, err)
	require.Empty(t, f.created)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Apply(ctx, f, plan, plan.SHA256)
	require.Error(t, err)
	require.Empty(t, f.created)
	f.readErr = true
	err = Check(context.Background(), f, plan.Tables)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private")
}
func TestRuntimeScopesDenyCrossOwnerAndUnknownMetadata(t *testing.T) {
	models, err := RuntimeModels("tweet-service")
	require.NoError(t, err)
	tables, err := Describe(models)
	require.NoError(t, err)
	for _, table := range tables {
		require.NotEqual(t, "users", table.Name)
		require.NotEqual(t, "outbox_tasks", table.Name)
	}
	require.True(t, allowedRuntimeTable("tweet-service", "follows"))
	require.False(t, allowedRuntimeTable("agent-service", "tweets"))
	require.Error(t, CheckRuntime(context.Background(), nil, "user-service", &domain.Like{}))
	_, err = (SQLReader{}).Read(context.Background(), "untrusted")
	require.Error(t, err)
}
