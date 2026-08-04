package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// Document is the smallest retrieval result contract required by the runner.
// Storage-specific scores stay outside the evaluation package.
type Document struct {
	ID   string `json:"id"`
	Text string `json:"text,omitempty"`
}

// Retriever represents one retrieval strategy, such as BM25 or vector search.
type Retriever interface {
	Name() string
	Retrieve(context.Context, string, int) ([]Document, error)
}

type Strategy struct {
	Name      string
	Retriever Retriever
}

type RunnerConfig struct {
	K                int
	DatasetVersion   string
	EmbeddingModel   string
	EmbeddingVersion string
	Environment      string
	Seed             int64
	CaseTimeout      time.Duration
	Providers        []ProviderReport
	Now              func() time.Time
}

// ProviderReport records the live dependency configuration and observed
// request outcome for one evaluation dependency. It deliberately contains no
// credentials or prompt/document content.
type ProviderReport struct {
	Name           string  `json:"name"`
	Provider       string  `json:"provider,omitempty"`
	Endpoint       string  `json:"endpoint,omitempty"`
	Model          string  `json:"model,omitempty"`
	Requests       int     `json:"requests"`
	FailedRequests int     `json:"failed_requests"`
	FailureRate    float64 `json:"failure_rate"`
}

type CaseResult struct {
	CaseID       string   `json:"case_id"`
	Query        string   `json:"query"`
	Category     string   `json:"category,omitempty"`
	RelevantIDs  []string `json:"relevant_ids"`
	RetrievedIDs []string `json:"retrieved_ids"`
	DurationMS   int64    `json:"duration_ms"`
	Error        string   `json:"error,omitempty"`
}

type StrategyReport struct {
	Name       string           `json:"name"`
	Cases      int              `json:"cases"`
	Succeeded  int              `json:"succeeded"`
	Failed     int              `json:"failed"`
	P50MS      int64            `json:"p50_ms"`
	P95MS      int64            `json:"p95_ms"`
	Metrics    RetrievalMetrics `json:"metrics"`
	CaseResult []CaseResult     `json:"case_results"`
}

type Report struct {
	GeneratedAt      time.Time        `json:"generated_at"`
	K                int              `json:"k"`
	DatasetVersion   string           `json:"dataset_version,omitempty"`
	EmbeddingModel   string           `json:"embedding_model,omitempty"`
	EmbeddingVersion string           `json:"embedding_version,omitempty"`
	Environment      string           `json:"environment,omitempty"`
	Seed             int64            `json:"seed"`
	CaseTimeoutMS    int64            `json:"case_timeout_ms,omitempty"`
	Providers        []ProviderReport `json:"providers,omitempty"`
	Strategies       []StrategyReport `json:"strategies"`
}

// Run executes strategies in the supplied order and cases in dataset order.
// Sequential execution is intentional: it produces a comparable baseline and
// avoids turning an evaluation command into an uncontrolled load generator.
func Run(ctx context.Context, dataset []DatasetCase, strategies []Strategy, cfg RunnerConfig) (Report, error) {
	if ctx == nil {
		return Report{}, fmt.Errorf("evaluation context is nil")
	}
	if cfg.K <= 0 {
		return Report{}, fmt.Errorf("evaluation k must be positive")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	report := Report{
		GeneratedAt:      now().UTC(),
		K:                cfg.K,
		DatasetVersion:   cfg.DatasetVersion,
		EmbeddingModel:   cfg.EmbeddingModel,
		EmbeddingVersion: cfg.EmbeddingVersion,
		Environment:      cfg.Environment,
		Seed:             cfg.Seed,
		CaseTimeoutMS:    cfg.CaseTimeout.Milliseconds(),
		Providers:        append([]ProviderReport(nil), cfg.Providers...),
		Strategies:       make([]StrategyReport, 0, len(strategies)),
	}
	for index, strategy := range strategies {
		if strategy.Retriever == nil {
			return Report{}, fmt.Errorf("strategy %d has nil retriever", index)
		}
		name := strategy.Name
		if name == "" {
			name = strategy.Retriever.Name()
		}
		if name == "" {
			return Report{}, fmt.Errorf("strategy %d has empty name", index)
		}

		strategyReport := StrategyReport{
			Name:       name,
			Cases:      len(dataset),
			CaseResult: make([]CaseResult, 0, len(dataset)),
		}
		durations := make([]int64, 0, len(dataset))
		metricsCases := make([]RetrievalCase, 0, len(dataset))
		for _, sample := range dataset {
			started := time.Now()
			caseCtx := ctx
			cancel := func() {}
			if cfg.CaseTimeout > 0 {
				caseCtx, cancel = context.WithTimeout(ctx, cfg.CaseTimeout)
			}
			docs, err := strategy.Retriever.Retrieve(caseCtx, sample.Query, cfg.K)
			cancel()
			elapsed := time.Since(started).Milliseconds()
			if elapsed < 0 {
				elapsed = 0
			}
			durations = append(durations, elapsed)

			result := CaseResult{
				CaseID:       sample.ID,
				Query:        sample.Query,
				Category:     sample.Category,
				RelevantIDs:  append([]string(nil), sample.RelevantIDs...),
				RetrievedIDs: documentIDs(docs, cfg.K),
				DurationMS:   elapsed,
			}
			if err != nil {
				result.Error = err.Error()
				strategyReport.Failed++
			} else {
				strategyReport.Succeeded++
			}
			strategyReport.CaseResult = append(strategyReport.CaseResult, result)
			metricsCases = append(metricsCases, RetrievalCase{
				ID:           sample.ID,
				Query:        sample.Query,
				Category:     sample.Category,
				RelevantIDs:  sample.RelevantIDs,
				RetrievedIDs: result.RetrievedIDs,
			})
		}
		strategyReport.Metrics = EvaluateRetrieval(metricsCases, cfg.K)
		strategyReport.P50MS = percentile(durations, 0.50)
		strategyReport.P95MS = percentile(durations, 0.95)
		report.Strategies = append(report.Strategies, strategyReport)
	}
	return report, nil
}

func WriteReport(writer io.Writer, report Report) error {
	if writer == nil {
		return fmt.Errorf("evaluation report writer is nil")
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode evaluation report: %w", err)
	}
	return nil
}

func documentIDs(docs []Document, limit int) []string {
	if limit > 0 && len(docs) > limit {
		docs = docs[:limit]
	}
	ids := make([]string, 0, len(docs))
	seen := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		if doc.ID == "" {
			continue
		}
		if _, ok := seen[doc.ID]; ok {
			continue
		}
		seen[doc.ID] = struct{}{}
		ids = append(ids, doc.ID)
	}
	return ids
}

func percentile(values []int64, quantile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(float64(len(ordered))*quantile + 0.999999)
	if index < 1 {
		index = 1
	}
	if index > len(ordered) {
		index = len(ordered)
	}
	return ordered[index-1]
}

// RRFRetriever composes independent ranked sources without knowing their
// storage. A source failure fails the fused query instead of silently
// presenting a partial ranking as a complete result.
type RRFRetriever struct {
	RetrieverName string
	Constant      float64
	Sources       []Retriever
}

func NewRRFRetriever(name string, sources ...Retriever) *RRFRetriever {
	return &RRFRetriever{RetrieverName: name, Constant: 60, Sources: sources}
}

func (r *RRFRetriever) Name() string { return r.RetrieverName }

func (r *RRFRetriever) Retrieve(ctx context.Context, query string, limit int) ([]Document, error) {
	if r == nil || len(r.Sources) == 0 {
		return nil, fmt.Errorf("rrf retriever has no sources")
	}
	constant := r.Constant
	if constant <= 0 {
		constant = 60
	}
	rankings := make([][]Document, len(r.Sources))
	for index, source := range r.Sources {
		if source == nil {
			return nil, fmt.Errorf("rrf source %d is nil", index)
		}
		docs, err := source.Retrieve(ctx, query, limit)
		if err != nil {
			return nil, fmt.Errorf("rrf source %q: %w", source.Name(), err)
		}
		rankings[index] = docs
	}
	return FuseRRF(rankings, limit, constant), nil
}

// FuseRRF merges ranked documents deterministically. Ties are resolved by ID
// so two runs over identical source rankings produce identical reports.
func FuseRRF(rankings [][]Document, limit int, constant float64) []Document {
	if limit <= 0 {
		return nil
	}
	if constant <= 0 {
		constant = 60
	}
	type scored struct {
		doc   Document
		score float64
		first int
	}
	scores := make(map[string]*scored)
	first := 0
	for _, ranking := range rankings {
		seen := make(map[string]struct{}, len(ranking))
		for rank, doc := range ranking {
			if doc.ID == "" {
				continue
			}
			if _, ok := seen[doc.ID]; ok {
				continue
			}
			seen[doc.ID] = struct{}{}
			item, ok := scores[doc.ID]
			if !ok {
				item = &scored{doc: doc, first: first}
				scores[doc.ID] = item
				first++
			}
			item.score += 1 / (constant + float64(rank+1))
		}
	}
	ordered := make([]scored, 0, len(scores))
	for _, item := range scores {
		ordered = append(ordered, *item)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score > ordered[j].score
		}
		if ordered[i].doc.ID != ordered[j].doc.ID {
			return ordered[i].doc.ID < ordered[j].doc.ID
		}
		return ordered[i].first < ordered[j].first
	})
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	result := make([]Document, len(ordered))
	for index, item := range ordered {
		result[index] = item.doc
	}
	return result
}
