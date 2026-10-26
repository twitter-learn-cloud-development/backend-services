package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/go-redis/redis/v8"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"twitter-clone/internal/module/follow/projection"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func hashTarget(parts ...string) string {
	body, _ := json.Marshal(parts)
	return fmt.Sprintf("%x", sha256.Sum256(body))
}
func scopes(authors, pairs string) ([]projection.Scope, error) {
	if (authors == "") == (pairs == "") {
		return nil, errors.New("specify author-ids or follower:author pairs")
	}
	rows := []projection.Scope{}
	if authors != "" {
		for _, part := range strings.Split(authors, ",") {
			id, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				return nil, errors.New("invalid author scope")
			}
			rows = append(rows, projection.Scope{Author: id})
		}
	}
	if pairs != "" {
		for _, part := range strings.Split(pairs, ",") {
			ids := strings.Split(part, ":")
			if len(ids) != 2 {
				return nil, errors.New("invalid pair scope")
			}
			f, err := strconv.ParseUint(ids[0], 10, 64)
			if err != nil || f == 0 {
				return nil, errors.New("invalid follower")
			}
			a, err := strconv.ParseUint(ids[1], 10, 64)
			if err != nil {
				return nil, errors.New("invalid author")
			}
			rows = append(rows, projection.Scope{Author: a, Follower: f})
		}
	}
	return projection.NormalizeScope(rows)
}
func run(args []string, out io.Writer) error {
	f := flag.NewFlagSet("follow-projection-reconcile", flag.ContinueOnError)
	authors := f.String("author-ids", "", "1-100 explicit authors")
	pairs := f.String("pairs", "", "1-100 follower:author pairs")
	mode := f.String("mode", "audit", "audit or repair (enqueue only)")
	live := f.Bool("allow-live", false, "explicitly confirmed dependency access")
	write := f.Bool("allow-write", false, "reviewed task registration")
	approved := f.String("approve-plan", "", "reviewed digest")
	operation := f.String("operation-id", "", "stable operation")
	operator := f.String("operator", "", "operator attestation")
	reason := f.String("reason", "", "reason")
	if f.Parse(args) != nil {
		return errors.New("invalid follow maintenance options")
	}
	rows, err := scopes(*authors, *pairs)
	if err != nil {
		return err
	}
	if *mode != "audit" && *mode != "repair" {
		return errors.New("invalid follow maintenance mode")
	}
	if *live && *mode == "repair" && (!*write || len(*approved) != 64 || *operation == "" || len(*operation) > 64 || strings.TrimSpace(*operator) == "" || len(*operator) > 100 || strings.TrimSpace(*reason) == "" || len(*reason) > 500) {
		return errors.New("follow repair requires explicit write authorization and plan")
	}
	if !*live {
		return json.NewEncoder(out).Encode(map[string]interface{}{"dry_run": true, "scope_count": len(rows), "mode": *mode, "enqueue_only": true})
	}
	config, err := driver.ParseDSN(os.Getenv("FOLLOW_PROJECTION_MYSQL_DSN"))
	if err != nil || config.User == "" || config.DBName == "" || config.MultiStatements {
		return errors.New("explicit follow database target required")
	}
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 5 * time.Second
	config.WriteTimeout = 5 * time.Second
	connector, err := driver.NewConnector(config)
	if err != nil {
		return errors.New("follow connector unavailable")
	}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	pool.SetMaxOpenConns(2)
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("follow database unavailable")
	}
	addr := os.Getenv("FOLLOW_PROJECTION_REDIS_ADDR")
	if addr == "" {
		return errors.New("explicit follow Redis target required")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, Username: os.Getenv("FOLLOW_PROJECTION_REDIS_USER"), Password: os.Getenv("FOLLOW_PROJECTION_REDIS_PASSWORD"), DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, MaxRetries: -1})
	defer rdb.Close()
	// Maintenance uses the checked table allocator, not a second Snowflake owner.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var extra string
	if *mode == "repair" {
		if db.WithContext(ctx).Raw("SELECT EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='outbox_tasks' AND COLUMN_NAME='id'").Scan(&extra).Error != nil || extra != "auto_increment" {
			return errors.New("follow maintenance allocator unavailable")
		}
	}
	store := &projection.SQLStore{DB: db, NewID: func() (uint64, error) { return 0, nil }}
	// The repository's maintenance allocator below omits ID, while normal ingress
	// continues to use the configured Snowflake owner.
	store.MaintenanceAutoID = true
	maintenance := projection.Maintenance{Store: store, Cache: &projection.RedisCache{Client: rdb}, Target: hashTarget(config.Net, config.Addr, config.DBName, addr)}
	var plan projection.AuditPlan
	count := 0
	if *mode == "repair" {
		plan, count, err = maintenance.Repair(ctx, rows, *approved, *operation, *operator, *reason)
	} else {
		plan, err = maintenance.Audit(ctx, rows)
	}
	if err != nil {
		_ = json.NewEncoder(out).Encode(map[string]interface{}{"complete": false, "registered_tasks": count, "plan_digest": plan.Digest})
		return err
	}
	return json.NewEncoder(out).Encode(map[string]interface{}{"plan": plan, "registered_tasks": count, "enqueue_only": true})
}
