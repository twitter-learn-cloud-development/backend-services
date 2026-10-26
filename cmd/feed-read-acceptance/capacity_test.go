package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"twitter-clone/internal/domain"
)

func testOptions() options {
	return options{allow: true, viewer: 1, limit: 2, samples: 4, workers: 2, pages: 3, minPages: 1, minRows: 1, minFollowees: 1}
}

func candidate(id uint64) *domain.Tweet { return &domain.Tweet{ID: id, UserID: 2} }

type fixtureSession struct {
	rows       [][]*domain.Tweet
	cursors    []uint64
	authors    []int
	closed     int
	readErr    error
	followErr  error
	closeErr   error
	denyFollow bool
}

func (s *fixtureSession) Candidates(_ context.Context, _, before uint64, _ int) ([]*domain.Tweet, error) {
	s.cursors = append(s.cursors, before)
	if s.readErr != nil {
		return nil, s.readErr
	}
	if len(s.rows) == 0 {
		return nil, nil
	}
	rows := s.rows[0]
	s.rows = s.rows[1:]
	return rows, nil
}
func (s *fixtureSession) Following(_ context.Context, _ uint64, authors []uint64) (map[uint64]bool, error) {
	s.authors = append(s.authors, len(authors))
	followed := map[uint64]bool{}
	if !s.denyFollow {
		for _, author := range authors {
			followed[author] = true
		}
	}
	return followed, s.followErr
}
func (s *fixtureSession) Close() error { s.closed++; return s.closeErr }

func TestOptionBoundsAndRedactedErrors(t *testing.T) {
	base := []string{"--allow-live", "--viewer-id=1"}
	for _, args := range [][]string{
		{"--workers=0"}, {"--workers=9"}, {"--samples=101"}, {"--pages=21"},
		{"--limit=101"}, {"--min-pages=2"}, {"--min-rows=21"}, {"--min-followees=1001"},
		{"--max-p95-ms=NaN"}, {"--max-p95-ms=+Inf"}, {"--max-p95-ms=-1"}, {"--dsn-env="},
		{"--discover-viewer"}, {"--cursor=18446744073709551616"}, {"private-secret"}, {"--private-secret"},
	} {
		_, err := parseOptions(append(append([]string{}, base...), args...))
		if err == nil || err.Error() != "invalid bounded acceptance options" {
			t.Fatalf("options %q: %v", args, err)
		}
	}
	o, err := parseOptions([]string{"--allow-live", "--discover-viewer", "--workers=8", "--pages=20", "--limit=100", "--min-rows=2000", "--min-pages=20", "--min-followees=1000"})
	if err != nil || o.workers != 8 || o.minRows != 2000 {
		t.Fatal(o, err)
	}
}

func TestJourneyPreservesLookaheadAndPreciseExclusiveCursor(t *testing.T) {
	o := testOptions()
	o.minPages, o.minRows = 3, 5
	s := &fixtureSession{rows: [][]*domain.Tweet{
		{candidate(9007199254740999), candidate(9007199254740998), candidate(9007199254740997)},
		{candidate(9007199254740997), candidate(9007199254740996), candidate(9007199254740995)},
		{candidate(9007199254740995)},
	}}
	r := readJourney(context.Background(), o, func(context.Context) (readSession, error) { return s, nil })
	if r.failure != "" || r.pages != 3 || r.rows != 5 || r.truncated || s.closed != 1 ||
		len(s.cursors) != 3 || s.cursors[1] != 9007199254740998 || s.cursors[2] != 9007199254740996 {
		t.Fatalf("journey=%+v cursors=%v closed=%d", r, s.cursors, s.closed)
	}
	for _, count := range s.authors {
		if count != 1 {
			t.Fatal("author batching not deduplicated", s.authors)
		}
	}
}

func TestJourneyFailsClosedAndAlwaysRollsBack(t *testing.T) {
	private := candidate(5)
	private.VisibleType = domain.VisiblePrivate
	deleted := candidate(5)
	deleted.DeletedAt = 1
	zeroAuthor := candidate(5)
	zeroAuthor.UserID = 0
	for _, tc := range []struct {
		name    string
		session fixtureSession
		code    string
	}{
		{"empty", fixtureSession{}, "insufficient_fixture"},
		{"nil", fixtureSession{rows: [][]*domain.Tweet{{nil}}}, "invalid_candidate_page"},
		{"zero", fixtureSession{rows: [][]*domain.Tweet{{candidate(0)}}}, "invalid_candidate_page"},
		{"author", fixtureSession{rows: [][]*domain.Tweet{{zeroAuthor}}}, "invalid_candidate_page"},
		{"duplicate", fixtureSession{rows: [][]*domain.Tweet{{candidate(5), candidate(5)}}}, "invalid_candidate_page"},
		{"order", fixtureSession{rows: [][]*domain.Tweet{{candidate(4), candidate(5)}}}, "invalid_candidate_page"},
		{"oversize", fixtureSession{rows: [][]*domain.Tweet{{candidate(5), candidate(4), candidate(3), candidate(2)}}}, "invalid_candidate_page"},
		{"private", fixtureSession{rows: [][]*domain.Tweet{{private}}}, "invalid_candidate_page"},
		{"deleted", fixtureSession{rows: [][]*domain.Tweet{{deleted}}}, "invalid_candidate_page"},
		{"follow missing", fixtureSession{rows: [][]*domain.Tweet{{candidate(5)}}, denyFollow: true}, "follow_validation_failed"},
		{"follow error", fixtureSession{rows: [][]*domain.Tweet{{candidate(5)}}, followErr: errors.New("private raw SQL")}, "follow_read_failed"},
		{"candidate error", fixtureSession{readErr: errors.New("private DSN")}, "candidate_read_failed"},
		{"close error", fixtureSession{rows: [][]*domain.Tweet{{candidate(5)}}, closeErr: errors.New("private connection")}, "transaction_close_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.session
			r := readJourney(context.Background(), testOptions(), func(context.Context) (readSession, error) { return &s, nil })
			if r.failure != tc.code || s.closed != 1 {
				t.Fatalf("%+v closed=%d", r, s.closed)
			}
		})
	}
}

func TestJourneyDeepBoundBudgetAndMinimumCoverage(t *testing.T) {
	o := testOptions()
	o.before, o.pages = 5, 1
	s := &fixtureSession{rows: [][]*domain.Tweet{{candidate(5)}}}
	if r := readJourney(context.Background(), o, func(context.Context) (readSession, error) { return s, nil }); r.failure != "invalid_candidate_page" {
		t.Fatal(r)
	}
	s = &fixtureSession{rows: [][]*domain.Tweet{{candidate(4), candidate(3), candidate(2)}}}
	r := readJourney(context.Background(), o, func(context.Context) (readSession, error) { return s, nil })
	if r.failure != "" || !r.truncated || r.rows != 2 || len(s.cursors) != 1 {
		t.Fatal(r)
	}
	o.pages, o.minPages = 3, 2
	s = &fixtureSession{rows: [][]*domain.Tweet{{candidate(4)}}}
	if r := readJourney(context.Background(), o, func(context.Context) (readSession, error) { return s, nil }); r.failure != "insufficient_fixture" {
		t.Fatal(r)
	}
	// Own private records are allowed by the production Following query.
	own := candidate(4)
	own.UserID, own.VisibleType = 1, domain.VisiblePrivate
	o.minPages = 1
	s = &fixtureSession{rows: [][]*domain.Tweet{{own}}, denyFollow: true}
	if r := readJourney(context.Background(), o, func(context.Context) (readSession, error) { return s, nil }); r.failure != "" || len(s.authors) != 0 {
		t.Fatal(r)
	}
}

type waitingSession struct {
	ctx    context.Context
	active *atomic.Int32
	closed *atomic.Int32
}

func (s *waitingSession) Candidates(context.Context, uint64, uint64, int) ([]*domain.Tweet, error) {
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}
func (*waitingSession) Following(context.Context, uint64, []uint64) (map[uint64]bool, error) {
	panic("unexpected follow")
}
func (s *waitingSession) Close() error { s.active.Add(-1); s.closed.Add(1); return nil }

func TestBoundedConcurrencyCancellationAndIndependentSessions(t *testing.T) {
	o := testOptions()
	o.samples, o.workers = 30, 3
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active, opened, closed, peak atomic.Int32
	full := make(chan struct{}, 1)
	open := func(ctx context.Context) (readSession, error) {
		opened.Add(1)
		n := active.Add(1)
		for previous := peak.Load(); n > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, n) {
				break
			}
		}
		if n == int32(o.workers) {
			select {
			case full <- struct{}{}:
			default:
			}
		}
		return &waitingSession{ctx, &active, &closed}, nil
	}
	done := make(chan capacityReport, 1)
	go func() { done <- measure(ctx, o, open) }()
	select {
	case <-full:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not start")
	}
	cancel()
	select {
	case r := <-done:
		if r.Failed != o.samples || r.Completed != 0 || r.CapacityAccepted || active.Load() != 0 || opened.Load() != closed.Load() || peak.Load() != int32(o.workers) || opened.Load() != int32(o.workers) {
			t.Fatalf("report=%+v open=%d closed=%d peak=%d", r, opened.Load(), closed.Load(), peak.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("workers leaked after cancellation")
	}
	var successClosed atomic.Int32
	r := measure(context.Background(), o, func(context.Context) (readSession, error) {
		return &countingSession{fixtureSession: fixtureSession{rows: [][]*domain.Tweet{{candidate(5)}}}, count: &successClosed}, nil
	})
	if r.Completed != o.samples || r.Failed != 0 || successClosed.Load() != int32(o.samples) {
		t.Fatalf("%+v closed=%d", r, successClosed.Load())
	}
}

type countingSession struct {
	fixtureSession
	count *atomic.Int32
}

func (s *countingSession) Close() error { s.count.Add(1); return s.fixtureSession.Close() }

func TestReportFailuresRedactionAndLatencyBudget(t *testing.T) {
	o := testOptions()
	r := measure(context.Background(), o, func(context.Context) (readSession, error) {
		return nil, errors.New("password-secret raw SQL viewer 987654321")
	})
	var out bytes.Buffer
	if writeReport(&out, r) == nil || strings.Contains(out.String(), "secret") || strings.Contains(out.String(), "987654321") || r.FailureCounts["transaction_unavailable"] != o.samples {
		t.Fatal(out.String())
	}
	r.Failed, r.Completed, r.MaxP95MS = 0, r.Samples, 1
	r.SuccessfulPageMS = distribution([]float64{1, 2, 100})
	out.Reset()
	if err := writeReport(&out, r); err == nil || err.Error() != "successful-page latency budget exceeded" {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"latency_budget_exceeded":true`) || !strings.Contains(out.String(), `"sample_checks_passed":false`) || !strings.Contains(out.String(), `"capacity_accepted":false`) {
		t.Fatal(out.String())
	}
	r.MaxP95MS = 0
	out.Reset()
	if err := writeReport(&out, r); err != nil || !strings.Contains(out.String(), `"sample_checks_passed":true`) || !strings.Contains(out.String(), `"capacity_accepted":false`) {
		t.Fatal(out.String(), err)
	}
	if d := distribution(nil); d.Count != 0 || d.P95MS != 0 {
		t.Fatal(d)
	}
	d := distribution([]float64{9, 1, 3, 5, 7})
	if d.Count != 5 || d.P50MS != 5 || d.P95MS != 9 || d.MaxMS != 9 {
		t.Fatal(d)
	}
}
