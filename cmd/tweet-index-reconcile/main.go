package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/tweet/projection"
	tweetrepo "twitter-clone/internal/module/tweet/repository"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/qdrant"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func parseIDs(value string) ([]uint64, error) {
	ids := []uint64{}
	for _, part := range strings.Split(value, ",") {
		id, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil, errors.New("invalid explicit Tweet IDs")
		}
		ids = append(ids, id)
	}
	return projection.NormalizeIDs(ids)
}
func endpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("invalid explicit index endpoint")
	}
	return nil
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("tweet-index-reconcile", flag.ContinueOnError)
	mode := flags.String("mode", "audit", "audit or repair (enqueue only)")
	allow := flags.Bool("allow-live", false, "explicitly permit confirmed dependencies")
	write := flags.Bool("allow-write", false, "explicitly permit reviewed task registration")
	idsFlag := flags.String("tweet-ids", "", "1-100 explicit decimal IDs")
	dsnEnv := flags.String("dsn-env", "PROJECTION_MYSQL_DSN", "local credential environment reference")
	esEnv := flags.String("es-env", "PROJECTION_ES_URL", "explicit ES endpoint reference")
	vectorEnv := flags.String("vector-env", "PROJECTION_QDRANT_URL", "explicit vector endpoint reference")
	model := flags.String("embedding-model", "", "match deployed Worker profile")
	dimension := flags.Int("embedding-dimension", 1024, "match deployed dimension")
	version := flags.String("embedding-version", "v1", "match deployed profile version")
	approved := flags.String("approve-plan", "", "reviewed audit plan digest")
	operation := flags.String("operation-id", "", "stable repair operation")
	operator := flags.String("operator", "", "operator attestation, stored as digest")
	reason := flags.String("reason", "", "reviewed purpose, stored as digest")
	if flags.Parse(args) != nil {
		return errors.New("invalid reconciliation options")
	}
	ids, err := parseIDs(*idsFlag)
	if err != nil {
		return err
	}
	if *mode != "audit" && *mode != "repair" {
		return errors.New("invalid reconciliation mode")
	}
	profile := domain.EmbeddingProfile{Model: *model, Dimension: *dimension, Version: *version}
	if !profile.Valid() {
		return errors.New("explicit deployed embedding profile required")
	}
	auth := projection.RepairAuthorization{ApprovedPlan: *approved, Operation: *operation, Operator: *operator, Reason: *reason}
	if *allow && *mode == "repair" && (!*write || !auth.Valid()) {
		return errors.New("repair requires explicit write authorization and reviewed plan")
	}
	// Default dry run never reads credentials or constructs a remote client.
	if !*allow {
		return json.NewEncoder(out).Encode(map[string]interface{}{"version": "projection-reconcile-v1", "dry_run": true, "mode": *mode, "scope_count": len(ids), "embedding_profile": profile.Digest()})
	}
	if *dsnEnv == "" || *esEnv == "" || *vectorEnv == "" {
		return errors.New("explicit dependency references required")
	}
	esURL, vectorURL := os.Getenv(*esEnv), os.Getenv(*vectorEnv)
	if endpoint(esURL) != nil || endpoint(vectorURL) != nil {
		return errors.New("invalid explicit index endpoints")
	}
	config, err := mysqlDriver.ParseDSN(os.Getenv(*dsnEnv))
	if err != nil || config.User == "" || config.DBName == "" || config.MultiStatements {
		return errors.New("invalid explicit MySQL target")
	}
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 5 * time.Second
	config.WriteTimeout = 5 * time.Second
	connector, err := mysqlDriver.NewConnector(config)
	if err != nil {
		return errors.New("MySQL connector unavailable")
	}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	pool.SetMaxOpenConns(2)
	pool.SetMaxIdleConns(2)
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("projection repository unavailable")
	}
	typed, err := elasticsearch.NewTypedClient(elasticsearch.Config{Addresses: []string{esURL}, APIKey: os.Getenv("PROJECTION_ES_API_KEY"), DisableRetry: true})
	if err != nil {
		return errors.New("text metadata client unavailable")
	}
	// Explicit metadata reads only: no model client, auto-migration or Redis.
	repo := tweetrepo.NewTweetRepository(db).(domain.TweetProjectionRepository)
	target := projection.Hash([]byte(config.Net + "|" + config.Addr + "|" + config.DBName + "|" + esURL + "|" + vectorURL))
	auditor := projection.Auditor{Facts: repo, Text: &es.Client{TypedClient: typed}, Vector: qdrant.NewClient(vectorURL), Profile: profile, TargetDigest: target}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var plan projection.Plan
	created := 0
	if *mode == "repair" {
		plan, created, err = auditor.Repair(ctx, ids, auth, tweetrepo.NewOutboxRepository(db).(domain.ProjectionRepairRepository))
	} else {
		plan, err = auditor.Audit(ctx, ids)
	}
	if err != nil {
		// Partial repair is explicit; keep receipts, return non-zero. It cannot
		// be described as atomic across the entire multi-ID batch.
		if *mode == "repair" {
			_ = json.NewEncoder(out).Encode(map[string]interface{}{"version": "projection-reconcile-v1", "registered_tasks": created, "complete": false, "plan_digest": plan.Digest})
		}
		return err
	}
	return json.NewEncoder(out).Encode(struct {
		Plan       projection.Plan `json:"plan"`
		Registered int             `json:"registered_tasks"`
		ReadOnly   bool            `json:"read_only"`
	}{plan, created, *mode == "audit"})
}
