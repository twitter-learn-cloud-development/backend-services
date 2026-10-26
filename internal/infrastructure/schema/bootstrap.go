package schema

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"regexp"
	"strings"
	"time"
)

type Statement struct {
	Table string `json:"table"`
	SQL   string `json:"sql"`
}
type Plan struct {
	Version    string      `json:"version"`
	Owner      string      `json:"owner"`
	Tables     []Table     `json:"tables"`
	Statements []Statement `json:"statements"`
	SHA256     string      `json:"sha256"`
}
type ddlRecorder struct {
	logger.Interface
	statements []string
}

func (r *ddlRecorder) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	r.statements = append(r.statements, sql)
}

var createTablePattern = regexp.MustCompile("^CREATE TABLE `([a-z_]+)` ")

func BuildPlan(owner string, extra ...interface{}) (Plan, error) {
	models, err := OwnerModels(owner)
	if err != nil {
		return Plan{}, err
	}
	models = append(models, extra...)
	tables, err := Describe(models)
	if err != nil {
		return Plan{}, err
	}
	for _, table := range tables {
		if table.Owner != owner {
			return Plan{}, errors.New("cross-owner migration denied")
		}
	}
	recorder := &ddlRecorder{Interface: logger.Discard}
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "offline:offline@tcp(127.0.0.1:1)/offline", SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, DisableForeignKeyConstraintWhenMigrating: true, Logger: recorder})
	if err != nil {
		return Plan{}, errors.New("offline schema builder unavailable")
	}
	raw, err := db.DB()
	if err != nil {
		return Plan{}, err
	}
	defer raw.Close()
	if err := db.Set("gorm:table_options", "ENGINE=InnoDB").Migrator().CreateTable(models...); err != nil {
		return Plan{}, errors.New("offline schema build failed")
	}
	statements := []Statement{}
	seen := map[string]bool{}
	for _, sql := range recorder.statements {
		match := createTablePattern.FindStringSubmatch(sql)
		if len(match) != 2 || TableOwner(match[1]) != owner || seen[match[1]] {
			return Plan{}, errors.New("bootstrap generated unexpected DDL")
		}
		seen[match[1]] = true
		statements = append(statements, Statement{Table: match[1], SQL: sql})
	}
	// Add reviewed indexes inside CREATE TABLE, so a cold bootstrap has no
	// interval between table creation and separate index statements.
	for i := range statements {
		var required Table
		for _, table := range tables {
			if table.Name == statements[i].Table {
				required = table
				break
			}
		}
		sql := statements[i].SQL
		end := strings.LastIndex(sql, ")")
		if end < 0 || strings.TrimSpace(sql[end+1:]) != "ENGINE=InnoDB" {
			return Plan{}, errors.New("bootstrap engine clause missing")
		}
		for _, index := range required.Indexes {
			if index.Name == "PRIMARY" || strings.Contains(sql, "`"+index.Name+"`") {
				continue
			}
			columns := []string{}
			for _, name := range index.Columns {
				columns = append(columns, "`"+name+"`")
			}
			kind := "INDEX"
			if index.Unique {
				kind = "UNIQUE INDEX"
			}
			sql = sql[:end] + ", " + kind + " `" + index.Name + "` (" + strings.Join(columns, ",") + ")" + sql[end:]
			end = strings.LastIndex(sql, ")")
		}
		statements[i].SQL = sql
	}
	if len(seen) != len(tables) {
		return Plan{}, errors.New("bootstrap scope is incomplete")
	}
	plan := Plan{Version: "owned-bootstrap-v1", Owner: owner, Tables: tables, Statements: statements}
	plan.SHA256 = planDigest(plan)
	return plan, nil
}
func planDigest(plan Plan) string {
	plan.SHA256 = ""
	body, _ := json.Marshal(plan)
	return fmt.Sprintf("%x", sha256.Sum256(body))
}

type BootstrapStore interface {
	Reader
	WithLock(context.Context, string, func(context.Context) error) error
	Create(context.Context, Statement) error
}
type Result struct {
	Table   string `json:"table"`
	Outcome string `json:"outcome"`
}
type ApplyReport struct {
	PlanSHA256 string   `json:"plan_sha256"`
	Results    []Result `json:"results"`
}

func Apply(ctx context.Context, store BootstrapStore, plan Plan, approvedSHA string) (ApplyReport, error) {
	report := ApplyReport{PlanSHA256: plan.SHA256, Results: []Result{}}
	if store == nil || plan.Version != "owned-bootstrap-v1" || plan.SHA256 != approvedSHA || plan.SHA256 != planDigest(plan) || len(plan.Tables) == 0 || len(plan.Tables) > 32 || len(plan.Statements) != len(plan.Tables) {
		return report, errors.New("bootstrap plan approval mismatch")
	}
	for _, table := range plan.Tables {
		if TableOwner(table.Name) != plan.Owner {
			return report, errors.New("bootstrap owner mismatch")
		}
	}
	seenTables := map[string]bool{}
	for _, table := range plan.Tables {
		if seenTables[table.Name] {
			return report, errors.New("duplicate bootstrap table")
		}
		seenTables[table.Name] = true
	}
	seenStatements := map[string]bool{}
	for _, statement := range plan.Statements {
		match := createTablePattern.FindStringSubmatch(statement.SQL)
		if len(match) != 2 || match[1] != statement.Table || !seenTables[statement.Table] || seenStatements[statement.Table] || strings.Contains(statement.SQL, ";") {
			return report, errors.New("bootstrap statement scope denied")
		}
		seenStatements[statement.Table] = true
	}
	err := store.WithLock(ctx, plan.Owner, func(locked context.Context) error {
		present := map[string]bool{}
		// Inspect all existing tables before the first DDL. Existing drift must use
		// a separately reviewed migration; this job never silently ALTERs tables.
		for _, table := range plan.Tables {
			m, err := store.Read(locked, table.Name)
			if err != nil {
				return errors.New("bootstrap preflight unavailable")
			}
			if m.Engine != "" {
				if err := Validate(table, m); err != nil {
					return err
				}
				present[table.Name] = true
			}
		}
		for _, statement := range plan.Statements {
			if err := locked.Err(); err != nil {
				return err
			}
			match := createTablePattern.FindStringSubmatch(statement.SQL)
			if len(match) != 2 || match[1] != statement.Table || TableOwner(statement.Table) != plan.Owner {
				return errors.New("bootstrap statement scope denied")
			}
			if present[statement.Table] {
				report.Results = append(report.Results, Result{statement.Table, "already_present"})
				continue
			}
			if err := store.Create(locked, statement); err != nil {
				report.Results = append(report.Results, Result{statement.Table, "partial_or_uncertain"})
				return errors.New("bootstrap DDL outcome uncertain")
			}
			var table Table
			for _, candidate := range plan.Tables {
				if candidate.Name == statement.Table {
					table = candidate
					break
				}
			}
			m, err := store.Read(locked, table.Name)
			if err != nil || Validate(table, m) != nil {
				report.Results = append(report.Results, Result{statement.Table, "unverified"})
				return errors.New("bootstrap verification failed")
			}
			report.Results = append(report.Results, Result{statement.Table, "created_verified"})
		}
		return nil
	})
	return report, err
}
