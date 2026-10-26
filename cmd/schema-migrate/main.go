package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"gorm.io/gorm/logger"
	"io"
	"os"
	"strings"
	"time"
	"twitter-clone/internal/infrastructure/persistence"
	"twitter-clone/internal/infrastructure/schema"
	followProjection "twitter-clone/internal/module/follow/projection"
)

type storeFactory func(context.Context) (schema.BootstrapStore, func(), error)

func main() {
	if err := run(os.Args[1:], os.Stdout, connect); err != nil {
		fmt.Fprintln(os.Stderr, "schema migration stopped:", err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer, factory storeFactory) error {
	flags := flag.NewFlagSet("schema-migrate", flag.ContinueOnError)
	flags.SetOutput(out)
	owner := flags.String("owner", "", "user/tweet/follow/messenger/notification/outbox/agent")
	accessRole := flags.String("access-role", "", "offline least-privilege SQL plan for a runtime role")
	accessDB := flags.String("database", "", "explicit database for access plan")
	accessAccount := flags.String("account", "", "existing runtime account; no credentials")
	accessHost := flags.String("account-host", "", "explicit MySQL account host pattern")
	live := flags.Bool("allow-live", false, "inspect existing metadata using dedicated migrator credentials")
	execute := flags.Bool("execute", false, "create only missing owned tables")
	approval := flags.String("approved-plan-sha256", "", "exact reviewed offline plan digest")
	operator := flags.String("operator", "", "bounded operator identity")
	reason := flags.String("reason", "", "bounded reason")
	operation := flags.String("operation", "", "bounded change identity")
	timeout := flags.Duration("timeout", 60*time.Second, "1-60s including initialization")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *timeout < time.Second || *timeout > time.Minute {
		return errors.New("invalid migration arguments")
	}
	if *accessRole != "" {
		if *owner != "" || *live || *execute {
			return errors.New("access plan is offline and separate from schema execution")
		}
		plan, err := schema.BuildAccessPlan(*accessRole, *accessDB, *accessAccount, *accessHost)
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(plan)
	}
	extras := []interface{}{}
	if *owner == "follow" {
		extras = []interface{}{&followProjection.AuthorState{}, &followProjection.IngressReceipt{}}
	}
	plan, err := schema.BuildPlan(*owner, extras...)
	if err != nil {
		return err
	}
	if *execute && (!*live || *approval != plan.SHA256 || strings.TrimSpace(*operator) == "" || strings.TrimSpace(*reason) == "" || strings.TrimSpace(*operation) == "" || len(*operator) > 128 || len(*reason) > 512 || len(*operation) > 128) {
		return errors.New("execute requires explicit live access and matching reviewed plan/operator/reason/operation")
	}
	if !*live {
		return json.NewEncoder(out).Encode(plan)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	store, close, err := factory(ctx)
	if err != nil {
		return errors.New("migration connection unavailable")
	}
	defer close()
	if !*execute {
		if err := schema.Check(ctx, store, plan.Tables); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(struct{ Mode, PlanSHA256 string }{"checked", plan.SHA256})
	}
	report, applyErr := schema.Apply(ctx, store, plan, *approval)
	audit := struct {
		schema.ApplyReport
		OperatorSHA256, ReasonSHA256, OperationSHA256 string
	}{report, digest(*operator), digest(*reason), digest(*operation)}
	if err := json.NewEncoder(out).Encode(audit); err != nil {
		return errors.New("migration report unavailable")
	}
	return applyErr
}
func connect(ctx context.Context) (schema.BootstrapStore, func(), error) {
	user, userSet := os.LookupEnv("DB_MIGRATOR_USER")
	password, passwordSet := os.LookupEnv("DB_MIGRATOR_PASSWORD")
	if !userSet || !passwordSet || user == "" || password == "" || strings.EqualFold(user, "root") {
		return nil, nil, errors.New("dedicated migrator credentials required")
	}
	cfg := persistence.DefaultDBConfig()
	cfg.User = user
	cfg.Password = password
	cfg.LogLevel = logger.Silent
	cfg.MaxIdleConns = 1
	cfg.MaxOpenConns = 2
	db, err := persistence.NewDBWithContext(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	raw, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	return schema.SQLBootstrapStore{DB: db}, func() { _ = raw.Close() }, nil
}
func digest(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }
