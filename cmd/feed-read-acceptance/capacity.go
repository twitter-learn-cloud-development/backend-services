package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"math"
	"sort"
	"sync"
	"time"

	"twitter-clone/internal/domain"
)

type options struct {
	allow, discover                 bool
	viewer, before                  uint64
	limit, samples, workers, pages  int
	minPages, minRows, minFollowees int
	dsnEnv                          string
	maxP95MS                        float64
}

func parseOptions(args []string) (options, error) {
	var o options
	f := flag.NewFlagSet("feed-read-acceptance", flag.ContinueOnError)
	f.SetOutput(io.Discard) // Invalid values must not echo arbitrary arguments.
	f.BoolVar(&o.allow, "allow-live", false, "permit a confirmed read-only target")
	f.BoolVar(&o.discover, "discover-viewer", false, "select an existing active follow owner")
	f.Uint64Var(&o.viewer, "viewer-id", 0, "existing fixture user")
	f.Uint64Var(&o.before, "cursor", 0, "deep-history bound")
	f.IntVar(&o.limit, "limit", 20, "page size, 1-100")
	f.IntVar(&o.samples, "samples", 20, "independent journeys, 1-100")
	f.IntVar(&o.workers, "workers", 1, "concurrent read transactions, 1-8")
	f.IntVar(&o.pages, "pages", 1, "page budget per journey, 1-20")
	f.IntVar(&o.minPages, "min-pages", 1, "minimum nonempty pages per journey")
	f.IntVar(&o.minRows, "min-rows", 1, "minimum returned rows per journey")
	f.IntVar(&o.minFollowees, "min-followees", 1, "required distinct active followees, 1-1000")
	f.StringVar(&o.dsnEnv, "dsn-env", "FEED_ACCEPTANCE_MYSQL_DSN", "read-only DSN environment variable")
	f.Float64Var(&o.maxP95MS, "max-p95-ms", 0, "optional successful-page p95 budget; zero disables")
	if err := f.Parse(args); err != nil || f.NArg() != 0 {
		return o, errors.New("invalid bounded acceptance options")
	}
	// No credential lookup, connector or dependency probe precedes this gate.
	if !o.allow {
		return o, errors.New("live acceptance requires --allow-live after dependency confirmation")
	}
	if (o.viewer == 0 && !o.discover) || (o.viewer != 0 && o.discover) ||
		o.limit < 1 || o.limit > 100 || o.samples < 1 || o.samples > 100 ||
		o.workers < 1 || o.workers > 8 || o.pages < 1 || o.pages > 20 ||
		o.minPages < 1 || o.minPages > o.pages || o.minRows < 1 || o.minRows > o.pages*o.limit ||
		o.minFollowees < 1 || o.minFollowees > 1000 || o.dsnEnv == "" ||
		math.IsNaN(o.maxP95MS) || math.IsInf(o.maxP95MS, 0) || o.maxP95MS < 0 || o.maxP95MS > 60000 {
		return o, errors.New("invalid bounded acceptance options")
	}
	return o, nil
}

type readSession interface {
	Candidates(context.Context, uint64, uint64, int) ([]*domain.Tweet, error)
	Following(context.Context, uint64, []uint64) (map[uint64]bool, error)
	Close() error // rollback only; never commit
}
type sessionFactory func(context.Context) (readSession, error)

type journeyResult struct {
	pages, rows int
	truncated   bool
	failure     string
	pageMS      []float64
	elapsedMS   float64
}

func readJourney(ctx context.Context, o options, open sessionFactory) (result journeyResult) {
	started := time.Now()
	defer func() { result.elapsedMS = float64(time.Since(started).Microseconds()) / 1000 }()
	if ctx.Err() != nil {
		result.failure = "cancelled"
		return
	}
	session, err := open(ctx)
	if err != nil {
		result.failure = "transaction_unavailable"
		return
	}
	defer func() {
		if err := session.Close(); err != nil && result.failure == "" {
			result.failure = "transaction_close_failed"
		}
	}()
	before := o.before
	for i := 0; i < o.pages; i++ {
		if ctx.Err() != nil {
			result.failure = "cancelled"
			return
		}
		pageStart := time.Now()
		rows, err := session.Candidates(ctx, o.viewer, before, o.limit+1)
		if err != nil {
			result.failure = "candidate_read_failed"
			return
		}
		if !validRows(rows, o.viewer, before, o.limit+1) {
			result.failure = "invalid_candidate_page"
			return
		}
		more := len(rows) > o.limit
		if more {
			rows = rows[:o.limit]
		}
		authors, seen := []uint64{}, map[uint64]bool{}
		for _, row := range rows {
			if row.UserID != o.viewer && !seen[row.UserID] {
				authors = append(authors, row.UserID)
				seen[row.UserID] = true
			}
		}
		if len(authors) > 0 {
			followed, err := session.Following(ctx, o.viewer, authors)
			if err != nil {
				result.failure = "follow_read_failed"
				return
			}
			for _, author := range authors {
				if !followed[author] {
					result.failure = "follow_validation_failed"
					return
				}
			}
		}
		if ctx.Err() != nil {
			result.failure = "cancelled"
			return
		}
		result.pageMS = append(result.pageMS, float64(time.Since(pageStart).Microseconds())/1000)
		if len(rows) > 0 {
			result.pages++
			result.rows += len(rows)
			before = rows[len(rows)-1].ID
		}
		result.truncated = more && i == o.pages-1
		if !more {
			break
		}
	}
	if result.pages < o.minPages || result.rows < o.minRows {
		result.failure = "insufficient_fixture"
	}
	return
}

func validRows(rows []*domain.Tweet, viewer, before uint64, maximum int) bool {
	if len(rows) > maximum {
		return false
	}
	previous := before
	for _, row := range rows {
		if row == nil || row.ID == 0 || row.UserID == 0 || row.DeletedAt != 0 ||
			(previous != 0 && row.ID >= previous) ||
			(row.UserID != viewer && row.VisibleType != domain.VisiblePublic && row.VisibleType != domain.VisibleFollows) {
			return false
		}
		previous = row.ID
	}
	return true
}

type latency struct {
	Count int     `json:"count"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	MaxMS float64 `json:"max_ms"`
}

func distribution(values []float64) latency {
	if len(values) == 0 {
		return latency{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return latency{len(sorted), sorted[(len(sorted)-1)/2], sorted[(len(sorted)*95+99)/100-1], sorted[len(sorted)-1]}
}

type capacityReport struct {
	Version            string         `json:"version"`
	Scope              string         `json:"scope"`
	ReadOnly           bool           `json:"read_only"`
	CapacityAccepted   bool           `json:"capacity_accepted"`
	SampleChecksPassed bool           `json:"sample_checks_passed"`
	LatencyExceeded    bool           `json:"latency_budget_exceeded"`
	ViewerSelection    string         `json:"viewer_selection"`
	StartMode          string         `json:"start_mode"`
	Workers            int            `json:"workers"`
	Samples            int            `json:"samples_requested"`
	Completed          int            `json:"samples_completed"`
	Failed             int            `json:"samples_failed"`
	PageBudget         int            `json:"page_budget_per_sample"`
	PageSize           int            `json:"page_size"`
	MinPages           int            `json:"minimum_nonempty_pages"`
	MinRows            int            `json:"minimum_rows_per_sample"`
	MinFollowees       int            `json:"minimum_followees"`
	NonemptyPages      int            `json:"nonempty_pages"`
	Returned           int            `json:"rows_returned"`
	Truncated          int            `json:"samples_reaching_page_budget"`
	FailureCounts      map[string]int `json:"failure_counts"`
	ElapsedMS          float64        `json:"load_elapsed_ms"`
	SuccessfulPageMS   latency        `json:"successful_sample_page_latency"`
	SuccessfulSampleMS latency        `json:"successful_sample_latency"`
	MaxP95MS           float64        `json:"max_p95_ms_budget_zero_disabled"`
	Dataset            datasetSummary `json:"preflight_dataset"`
	Plan               []planTable    `json:"explain_estimates"`
}

func measure(ctx context.Context, o options, open sessionFactory) capacityReport {
	r := capacityReport{Version: "feed-read-v2", Scope: "mysql_candidates_and_follow_check", ReadOnly: true,
		StartMode: "head", Workers: min(o.workers, o.samples), Samples: o.samples, PageBudget: o.pages,
		PageSize: o.limit, MinPages: o.minPages, MinRows: o.minRows, MinFollowees: o.minFollowees,
		FailureCounts: map[string]int{}, MaxP95MS: o.maxP95MS}
	if o.before != 0 {
		r.StartMode = "deep"
	}
	jobs := make(chan struct{}, o.samples)
	for i := 0; i < o.samples; i++ {
		jobs <- struct{}{}
	}
	close(jobs)
	results := make(chan journeyResult, r.Workers)
	var group sync.WaitGroup
	started := time.Now()
	for i := 0; i < r.Workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for range jobs {
				results <- readJourney(ctx, o, open)
			}
		}()
	}
	go func() { group.Wait(); close(results) }()
	var pageTimes, sampleTimes []float64
	for result := range results {
		r.NonemptyPages += result.pages
		r.Returned += result.rows
		if result.truncated {
			r.Truncated++
		}
		if result.failure != "" {
			r.Failed++
			r.FailureCounts[result.failure]++
			continue
		}
		r.Completed++
		pageTimes = append(pageTimes, result.pageMS...)
		sampleTimes = append(sampleTimes, result.elapsedMS)
	}
	r.ElapsedMS = float64(time.Since(started).Microseconds()) / 1000
	r.SuccessfulPageMS, r.SuccessfulSampleMS = distribution(pageTimes), distribution(sampleTimes)
	return r
}
