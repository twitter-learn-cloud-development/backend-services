package websearch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
)

type memoryWebCacheStub struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (cache *memoryWebCacheStub) Get(_ context.Context, key string) ([]byte, bool, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	value, found := cache.values[key]
	return append([]byte(nil), value...), found, nil
}

func (cache *memoryWebCacheStub) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.values[key] = append([]byte(nil), value...)
	return nil
}

type countingSearchProvider struct {
	calls int
}

func (provider *countingSearchProvider) Name() string { return "counting" }

func (provider *countingSearchProvider) Search(
	_ context.Context,
	request Request,
) (agentEvidence.WebSearchResult, error) {
	provider.calls++
	return agentEvidence.WebSearchResult{
		Schema: agentEvidence.WebSearchSchema, Provider: provider.Name(), Query: request.Query,
		Items: []agentEvidence.WebSearchEvidence{{Rank: 1, URL: "https://example.com", Title: "Example"}},
	}, nil
}

type countingPageReader struct {
	calls int
}

func (reader *countingPageReader) Read(
	_ context.Context,
	request PageRequest,
) (agentEvidence.WebPageResult, error) {
	reader.calls++
	return agentEvidence.WebPageResult{
		Schema: agentEvidence.WebPageSchema, URL: request.URL,
		ContentType: "text/plain", Content: "content", Excerpt: "content",
	}, nil
}

type governorStub struct {
	requests []AdmissionRequest
	err      error
}

func (governor *governorStub) Admit(_ context.Context, request AdmissionRequest) error {
	governor.requests = append(governor.requests, request)
	return governor.err
}

func TestCachedProviderAndPageReaderReuseResults(t *testing.T) {
	t.Parallel()

	cache := &memoryWebCacheStub{values: make(map[string][]byte)}
	search := &countingSearchProvider{}
	cachedSearch := NewCachedProvider(search, cache, time.Minute)
	request := Request{Query: "Go release", Limit: 3}
	for range 2 {
		if _, err := cachedSearch.Search(context.Background(), request); err != nil {
			t.Fatalf("Search() error = %v", err)
		}
	}
	if search.calls != 1 {
		t.Fatalf("search calls = %d, want 1", search.calls)
	}

	page := &countingPageReader{}
	cachedPage := NewCachedPageReader(page, cache, time.Minute)
	pageRequest := PageRequest{URL: "https://example.com/article", MaxRunes: 1_000}
	for range 2 {
		if _, err := cachedPage.Read(context.Background(), pageRequest); err != nil {
			t.Fatalf("Read() error = %v", err)
		}
	}
	if page.calls != 1 {
		t.Fatalf("page calls = %d, want 1", page.calls)
	}
}

func TestGovernedWebAccessFailsClosed(t *testing.T) {
	t.Parallel()

	governor := &governorStub{err: ErrAccessBudgetExceeded}
	search := NewGovernedProvider(&countingSearchProvider{}, governor, 100)
	_, err := search.Search(context.Background(), Request{
		Query: "query",
		Subject: AccessSubject{
			UserID: 7,
			RunID:  "run-1",
		},
	})
	if !errors.Is(err, ErrAccessBudgetExceeded) {
		t.Fatalf("Search() error = %v", err)
	}
	if len(governor.requests) != 1 ||
		governor.requests[0].Operation != AccessOperationSearch ||
		governor.requests[0].CostMicros != 100 {
		t.Fatalf("requests = %+v", governor.requests)
	}
}
