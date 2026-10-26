package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"twitter-clone/internal/domain"
	followrepo "twitter-clone/internal/module/follow/repository"
	tweetrepo "twitter-clone/internal/module/tweet/repository"
)

type datasetSummary struct {
	ObservedAt             string `json:"observed_at"`
	FolloweesObserved      int    `json:"distinct_active_followees_observed"`
	FolloweeProbeLimit     int    `json:"followee_probe_limit"`
	FolloweeProbeSaturated bool   `json:"followee_probe_saturated"`
	// InnoDB TABLE_ROWS is approximate, not a COUNT or dataset qualification.
	TweetRowsEstimate      uint64 `json:"tweets_table_rows_estimate"`
	TweetEstimateAvailable bool   `json:"tweets_estimate_available"`
}

type mysqlSession struct {
	tx     *sql.Tx
	db     *gorm.DB
	pager  domain.FollowingFeedRepository
	reader domain.FollowReadRepository
}

func openMySQLSession(ctx context.Context, pool *sql.DB) (*mysqlSession, error) {
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, errors.New("read-only transaction unavailable")
	}
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: tx, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		_ = tx.Rollback()
		return nil, errors.New("read-only repository unavailable")
	}
	return &mysqlSession{tx: tx, db: db, pager: tweetrepo.NewTweetRepository(db).(domain.FollowingFeedRepository), reader: followrepo.NewFollowReadRepository(db)}, nil
}

func (s *mysqlSession) Candidates(ctx context.Context, viewer, before uint64, limit int) ([]*domain.Tweet, error) {
	return s.pager.ListFollowingFeed(ctx, viewer, before, limit)
}
func (s *mysqlSession) Following(ctx context.Context, viewer uint64, authors []uint64) (map[uint64]bool, error) {
	return s.reader.BatchIsFollowing(ctx, viewer, authors)
}
func (s *mysqlSession) Close() error { return s.tx.Rollback() }

func runMySQL(o options, out io.Writer) error {
	cfg, err := mysqlDriver.ParseDSN(os.Getenv(o.dsnEnv))
	if err != nil || cfg.User == "" || cfg.DBName == "" || cfg.MultiStatements {
		return errors.New("invalid read-only DSN configuration")
	}
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 5*time.Second, 5*time.Second
	// Driver diagnostics may contain endpoints or raw connection errors. The CLI
	// reports fixed stage errors and counters instead.
	cfg.Logger = log.New(io.Discard, "", 0)
	connector, err := mysqlDriver.NewConnector(cfg)
	if err != nil {
		return errors.New("MySQL connector unavailable")
	}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	pool.SetMaxOpenConns(min(o.workers, o.samples))
	pool.SetMaxIdleConns(min(o.workers, o.samples))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dataset, plan, selection, err := preflight(ctx, pool, &o)
	if err != nil {
		return err
	}
	r := measure(ctx, o, func(ctx context.Context) (readSession, error) { return openMySQLSession(ctx, pool) })
	r.Dataset, r.Plan, r.ViewerSelection = dataset, plan, selection
	return writeReport(out, r)
}

func writeReport(out io.Writer, r capacityReport) error {
	r.LatencyExceeded = r.MaxP95MS > 0 && r.SuccessfulPageMS.P95MS > r.MaxP95MS
	r.SampleChecksPassed = r.Completed > 0 && r.Completed == r.Samples && r.Failed == 0 && !r.LatencyExceeded
	if err := json.NewEncoder(out).Encode(r); err != nil {
		return errors.New("acceptance report write failed")
	}
	if r.Failed != 0 || r.Completed != r.Samples || r.Completed == 0 {
		return errors.New("feed read samples failed; see fixed failure counts")
	}
	if r.LatencyExceeded {
		return errors.New("successful-page latency budget exceeded")
	}
	return nil
}

func preflight(ctx context.Context, pool *sql.DB, o *options) (result datasetSummary, plans []planTable, selected string, resultErr error) {
	d := datasetSummary{ObservedAt: time.Now().UTC().Format(time.RFC3339), FolloweeProbeLimit: max(101, o.minFollowees+1)}
	s, err := openMySQLSession(ctx, pool)
	if err != nil {
		return d, nil, "", errors.New("read-only preflight unavailable")
	}
	defer func() {
		if err := s.Close(); err != nil && resultErr == nil {
			resultErr = errors.New("read-only preflight close failed")
		}
	}()
	selection := "explicit"
	if o.discover {
		if err := s.tx.QueryRowContext(ctx, "SELECT follower_id FROM follows WHERE deleted_at = 0 ORDER BY id DESC LIMIT 1").Scan(&o.viewer); err != nil || o.viewer == 0 {
			return d, nil, "", errors.New("existing active-follow fixture unavailable")
		}
		selection = "recent_active_follow"
	}
	rows, err := s.tx.QueryContext(ctx, "SELECT DISTINCT followee_id FROM follows WHERE follower_id = ? AND deleted_at = 0 AND followee_id <> 0 ORDER BY followee_id LIMIT ?", o.viewer, d.FolloweeProbeLimit)
	if err != nil {
		return d, nil, selection, errors.New("fixture follow probe failed")
	}
	for rows.Next() {
		var id uint64
		if rows.Scan(&id) != nil {
			_ = rows.Close()
			return d, nil, selection, errors.New("fixture follow probe failed")
		}
		d.FolloweesObserved++
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil || closeErr != nil {
		return d, nil, selection, errors.New("fixture follow probe failed")
	}
	d.FolloweeProbeSaturated = d.FolloweesObserved == d.FolloweeProbeLimit
	if d.FolloweesObserved < o.minFollowees {
		return d, nil, selection, errors.New("insufficient active-follow fixture")
	}
	var estimate sql.NullInt64
	if err := s.tx.QueryRowContext(ctx, "SELECT TABLE_ROWS FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'tweets'").Scan(&estimate); err != nil {
		return d, nil, selection, errors.New("fixture table estimate unavailable")
	}
	if estimate.Valid && estimate.Int64 >= 0 {
		d.TweetRowsEstimate, d.TweetEstimateAvailable = uint64(estimate.Int64), true
	}
	planner := tweetrepo.NewTweetRepository(s.db).(interface {
		ExplainFollowingFeed(context.Context, uint64, uint64, int) (string, error)
	})
	plan, err := planner.ExplainFollowingFeed(ctx, o.viewer, o.before, o.limit+1)
	if err != nil {
		return d, nil, selection, errors.New("following feed EXPLAIN failed")
	}
	tables, err := summarizePlan(plan)
	return d, tables, selection, err
}
