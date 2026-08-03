package websearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	agentModel "twitter-clone/internal/module/agent/model"
)

func TestBraveProviderSearchNormalizesAndBoundsResults(t *testing.T) {
	t.Parallel()

	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get(braveSubscriptionHeader) != "test-secret" {
			t.Errorf("subscription token = %q", request.Header.Get(braveSubscriptionHeader))
		}
		receivedQuery = request.URL.Query().Get("q")
		if request.URL.Query().Get("count") != "2" ||
			request.URL.Query().Get("result_filter") != "web" {
			t.Errorf("query params = %s", request.URL.RawQuery)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"web":{"results":[
				{"title":"<strong>First</strong> result","url":"https://example.com/a#fragment","description":"Useful <strong>snippet</strong>"},
				{"title":"Unsafe","url":"javascript:alert(1)","description":"drop me"},
				{"title":"Second","url":"https://example.org/b?q=1","description":"Second snippet"}
			]}
		}`))
	}))
	defer server.Close()

	provider, err := NewBraveProvider(BraveConfig{
		BaseURL:        server.URL,
		APIKey:         "test-secret",
		MaxResults:     2,
		EndpointPolicy: agentModel.NewEndpointPolicy("127.0.0.1"),
	})
	if err != nil {
		t.Fatalf("NewBraveProvider() error = %v", err)
	}
	result, err := provider.Search(context.Background(), Request{
		Query: "  Go   agent search  ",
		Limit: 20,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if receivedQuery != "Go agent search" {
		t.Fatalf("received query = %q", receivedQuery)
	}
	if result.Schema != "web.search.v1" ||
		result.Provider != BraveProviderName ||
		result.Query != "Go agent search" ||
		len(result.Items) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Items[0].Title != "First result" ||
		result.Items[0].Snippet != "Useful snippet" ||
		result.Items[0].URL != "https://example.com/a" ||
		result.Items[0].Rank != 1 {
		t.Fatalf("first item = %+v", result.Items[0])
	}
	if result.Items[1].URL != "https://example.org/b?q=1" ||
		result.Items[1].Rank != 2 {
		t.Fatalf("second item = %+v", result.Items[1])
	}
}

func TestBraveProviderFailsClosedForMissingKeyAndUnsafeEndpoint(t *testing.T) {
	t.Parallel()

	if _, err := NewBraveProvider(BraveConfig{
		BaseURL: DefaultBraveBaseURL,
	}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing key error = %v", err)
	}
	if _, err := NewBraveProvider(BraveConfig{
		BaseURL: "http://169.254.169.254/latest/meta-data",
		APIKey:  "secret",
	}); !errors.Is(err, agentModel.ErrEndpointNotAllowed) {
		t.Fatalf("unsafe endpoint error = %v", err)
	}
}

func TestBraveProviderDoesNotExposeProviderBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":"private-provider-detail"}`))
	}))
	defer server.Close()
	provider := newTestBraveProvider(t, server.URL, 1024)

	_, err := provider.Search(context.Background(), Request{Query: "query"})
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("Search() error = %v", err)
	}
	if strings.Contains(err.Error(), "private-provider-detail") {
		t.Fatalf("provider response body leaked in error: %v", err)
	}
}

func TestBraveProviderRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat("x", 129)))
	}))
	defer server.Close()
	provider := newTestBraveProvider(t, server.URL, 128)

	_, err := provider.Search(context.Background(), Request{Query: "query"})
	if !errors.Is(err, ErrProvider) || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestBraveProviderBlocksRedirectAndDoesNotForwardCredentials(t *testing.T) {
	t.Parallel()

	var targetCalls atomic.Int32
	var forwardedToken atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		targetCalls.Add(1)
		forwardedToken.Store(request.Header.Get(braveSubscriptionHeader) != "")
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()
	provider := newTestBraveProvider(t, source.URL, 1024)

	_, err := provider.Search(context.Background(), Request{Query: "query"})
	if !errors.Is(err, ErrProvider) ||
		!errors.Is(err, agentModel.ErrEndpointNotAllowed) {
		t.Fatalf("Search() error = %v", err)
	}
	if targetCalls.Load() != 0 || forwardedToken.Load() {
		t.Fatalf("redirect target calls/token = %d/%t", targetCalls.Load(), forwardedToken.Load())
	}
}

func TestBraveProviderConcurrencyAdmissionHonorsContext(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = writer.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer server.Close()
	provider, err := NewBraveProvider(BraveConfig{
		BaseURL:        server.URL,
		APIKey:         "test-secret",
		MaxConcurrent:  1,
		EndpointPolicy: agentModel.NewEndpointPolicy("127.0.0.1"),
	})
	if err != nil {
		t.Fatalf("NewBraveProvider() error = %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, searchErr := provider.Search(context.Background(), Request{Query: "first"})
		firstDone <- searchErr
	}()
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = provider.Search(ctx, Request{Query: "second"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Search() error = %v", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Search() error = %v", err)
	}
}

func TestNormalizeRequestRejectsProviderLimitViolations(t *testing.T) {
	t.Parallel()

	_, err := NormalizeRequest(Request{Query: strings.Repeat("a", MaxQueryRunes+1)}, 10)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("long query error = %v", err)
	}
	_, err = NormalizeRequest(Request{Query: strings.Repeat("word ", MaxQueryWords+1)}, 10)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("many words error = %v", err)
	}
}

func TestBraveProviderClampsDeploymentLimitsToHardCaps(t *testing.T) {
	t.Parallel()

	provider, err := NewBraveProvider(BraveConfig{
		BaseURL:          DefaultBraveBaseURL,
		APIKey:           "test-secret",
		Timeout:          time.Hour,
		MaxResults:       1000,
		MaxResponseBytes: 1 << 30,
		MaxConcurrent:    1000,
	})
	if err != nil {
		t.Fatalf("NewBraveProvider() error = %v", err)
	}
	if provider.client.Timeout != HardMaxSearchTimeout ||
		provider.maxResults != HardMaxSearchResults ||
		provider.maxResponseBytes != HardMaxResponseBytes ||
		cap(provider.admission) != HardMaxConcurrent {
		t.Fatalf(
			"limits = timeout %s results %d bytes %d concurrency %d",
			provider.client.Timeout,
			provider.maxResults,
			provider.maxResponseBytes,
			cap(provider.admission),
		)
	}
}

func newTestBraveProvider(t *testing.T, endpoint string, maxResponseBytes int64) *BraveProvider {
	t.Helper()
	provider, err := NewBraveProvider(BraveConfig{
		BaseURL:          endpoint,
		APIKey:           "test-secret",
		MaxResponseBytes: maxResponseBytes,
		EndpointPolicy:   agentModel.NewEndpointPolicy("127.0.0.1"),
	})
	if err != nil {
		t.Fatalf("NewBraveProvider() error = %v", err)
	}
	return provider
}
