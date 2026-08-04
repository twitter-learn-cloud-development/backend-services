package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRetriever struct {
	name  string
	docs  map[string][]Document
	err   map[string]error
	calls int
}

func (f *fakeRetriever) Name() string { return f.name }

func (f *fakeRetriever) Retrieve(_ context.Context, query string, limit int) ([]Document, error) {
	f.calls++
	if err := f.err[query]; err != nil {
		return nil, err
	}
	docs := append([]Document(nil), f.docs[query]...)
	if len(docs) > limit {
		docs = docs[:limit]
	}
	return docs, nil
}

func TestRunPreservesDatasetAndStrategyOrder(t *testing.T) {
	retriever := &fakeRetriever{
		name: "bm25",
		docs: map[string][]Document{
			"go":  {{ID: "a"}, {ID: "noise"}},
			"rag": {{ID: "b"}},
		},
		err: make(map[string]error),
	}
	report, err := Run(context.Background(), []DatasetCase{
		{ID: "case-1", Query: "go", RelevantIDs: []string{"a"}},
		{ID: "case-2", Query: "rag", RelevantIDs: []string{"b"}},
	}, []Strategy{{Name: "bm25", Retriever: retriever}}, RunnerConfig{K: 2, Seed: 7})
	if err != nil {
		t.Fatalf("run evaluation: %v", err)
	}
	if got := report.Strategies[0].CaseResult[0].CaseID; got != "case-1" {
		t.Fatalf("unexpected case order: %q", got)
	}
	if report.Strategies[0].Succeeded != 2 || report.Strategies[0].Failed != 0 {
		t.Fatalf("unexpected strategy result: %#v", report.Strategies[0])
	}
	if report.Strategies[0].Metrics.RecallAtK != 1 {
		t.Fatalf("unexpected metrics: %#v", report.Strategies[0].Metrics)
	}
}

func TestRunKeepsPerCaseErrorsAndCalculatesMetrics(t *testing.T) {
	retriever := &fakeRetriever{
		name: "vector",
		docs: map[string][]Document{"ok": {{ID: "a"}}},
		err:  map[string]error{"broken": errors.New("provider unavailable")},
	}
	report, err := Run(context.Background(), []DatasetCase{
		{ID: "ok", Query: "ok", RelevantIDs: []string{"a"}},
		{ID: "broken", Query: "broken", RelevantIDs: []string{"b"}},
	}, []Strategy{{Retriever: retriever}}, RunnerConfig{K: 1})
	if err != nil {
		t.Fatalf("run evaluation: %v", err)
	}
	result := report.Strategies[0]
	if result.Name != "vector" || result.Succeeded != 1 || result.Failed != 1 {
		t.Fatalf("unexpected result summary: %#v", result)
	}
	if !strings.Contains(result.CaseResult[1].Error, "provider unavailable") {
		t.Fatalf("missing per-case error: %#v", result.CaseResult[1])
	}
}

func TestRunCopiesProviderMetadataAndAppliesCaseTimeout(t *testing.T) {
	retriever := &blockingRetriever{name: "vector"}
	report, err := Run(context.Background(), []DatasetCase{{ID: "slow", Query: "slow"}}, []Strategy{{Retriever: retriever}}, RunnerConfig{
		K:           1,
		CaseTimeout: 10 * time.Millisecond,
		Providers:   []ProviderReport{{Name: "embedding", Provider: "lmstudio", Endpoint: "http://localhost:1234/v1", Model: "text-embedding-bge-m3"}},
	})
	if err != nil {
		t.Fatalf("run evaluation: %v", err)
	}
	if len(report.Providers) != 1 || report.Providers[0].Endpoint != "http://localhost:1234/v1" {
		t.Fatalf("provider metadata was not preserved: %#v", report.Providers)
	}
	if report.CaseTimeoutMS != 10 || report.Strategies[0].Failed != 1 {
		t.Fatalf("timeout was not recorded: %#v", report)
	}
	if !strings.Contains(report.Strategies[0].CaseResult[0].Error, "context deadline exceeded") {
		t.Fatalf("missing timeout error: %#v", report.Strategies[0].CaseResult[0])
	}
}

type blockingRetriever struct{ name string }

func (r *blockingRetriever) Name() string { return r.name }

func (r *blockingRetriever) Retrieve(ctx context.Context, _ string, _ int) ([]Document, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestFuseRRFIsDeterministicAndDeduplicates(t *testing.T) {
	got := FuseRRF([][]Document{
		{{ID: "b", Text: "b"}, {ID: "a", Text: "a"}, {ID: "b", Text: "duplicate"}},
		{{ID: "a", Text: "a"}, {ID: "c", Text: "c"}},
	}, 3, 60)
	if len(got) != 3 {
		t.Fatalf("unexpected result count: %#v", got)
	}
	if got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "c" {
		t.Fatalf("unexpected RRF order: %#v", got)
	}
	if got[1].Text != "b" {
		t.Fatalf("expected first source document to win duplicate: %#v", got[1])
	}
}
