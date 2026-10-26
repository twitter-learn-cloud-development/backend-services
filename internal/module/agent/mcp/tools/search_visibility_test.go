package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"google.golang.org/grpc"
	tweetv1 "twitter-clone/api/tweet/v1"
	agentEvidence "twitter-clone/internal/module/agent/evidence"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
)

type searchAuthorityStub struct {
	tweetv1.TweetServiceClient
	response *tweetv1.GetPublicTweetsResponse
	err      error
	calls    int
	ids      []uint64
}

func (s *searchAuthorityStub) GetPublicTweets(_ context.Context, req *tweetv1.GetPublicTweetsRequest, _ ...grpc.CallOption) (*tweetv1.GetPublicTweetsResponse, error) {
	s.calls++
	s.ids = req.TweetIds
	return s.response, s.err
}

type searchIndexTransport struct{ body string }

func (r searchIndexTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := r.body
	if req.Method == http.MethodHead {
		body = ""
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"X-Elastic-Product": []string{"Elasticsearch"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

type visibilityReranker struct{ docs []ai.Document }

func (r *visibilityReranker) Rerank(_ context.Context, _ string, docs []ai.Document) ([]ai.RerankResult, error) {
	r.docs = docs
	result := make([]ai.RerankResult, 0, len(docs))
	for _, doc := range docs {
		result = append(result, ai.RerankResult{Document: doc, Score: 1})
	}
	return result, nil
}

const staleSearchIndex = `{"hits":{"hits":[{"_source":{"id":"1","user_id":"7","content":"stale-one"}},{"_source":{"id":"2","user_id":"7","content":"hidden-body"}},{"_source":{"id":"3","user_id":"7","content":"stale-three"}}]}}`

func searchVisibilityFixture(t *testing.T, name string, authority *searchAuthorityStub, reranker ai.Reranker) server.ToolHandlerFunc {
	t.Helper()
	httpClient := &http.Client{Transport: searchIndexTransport{body: staleSearchIndex}}
	index, err := es.NewClient(es.Config{Addresses: []string{"http://fixture.invalid"}, HTTPClient: httpClient})
	if err != nil {
		t.Fatal(err)
	}
	model := ai.NewClientWithConfig("http://fixture.invalid/v1", "", httpClient)
	srv := server.NewMCPServer("fixture", "v1")
	if name == "hybrid_search_tweets" {
		RegisterHybridSearchTweets(srv, model, index, nil, reranker, authority, "fixture")
	} else {
		RegisterSearchTweets(srv, model, index, nil, reranker, authority, "fixture")
	}
	return srv.GetTool(name).Handler
}

func callVisibilitySearch(t *testing.T, handler server.ToolHandlerFunc, name string) *mcp.CallToolResult {
	t.Helper()
	// Deterministic embedding fixture; no Provider request or socket is used.
	ctx := context.WithValue(context.Background(), "MOCK_EMBEDDING", true)
	result, err := handler(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: map[string]any{"query": "query", "size": float64(1)}}})
	if err != nil || result == nil {
		t.Fatalf("search handler: %+v %v", result, err)
	}
	return result
}

func TestSearchToolsHydrateBeforeRerankingAndEvidence(t *testing.T) {
	for _, name := range []string{"hybrid_search_tweets", "search_tweets_by_semantic"} {
		t.Run(name, func(t *testing.T) {
			authority := &searchAuthorityStub{response: &tweetv1.GetPublicTweetsResponse{Tweets: []*tweetv1.Tweet{{Id: 1, UserId: 7, Content: "current-one"}, {Id: 3, UserId: 7, Content: "current-three"}}}}
			reranker := &visibilityReranker{}
			result := callVisibilitySearch(t, searchVisibilityFixture(t, name, authority, reranker), name)
			if result.IsError || authority.calls != 1 || len(reranker.docs) != 2 {
				t.Fatalf("search/rerank = %+v %+v", result, reranker.docs)
			}
			if !reflect.DeepEqual(authority.ids, []uint64{1, 2, 3}) {
				t.Fatalf("authority IDs: %v", authority.ids)
			}
			for _, doc := range reranker.docs {
				if doc.Text != "current-one" && doc.Text != "current-three" {
					t.Fatalf("unsafe text reached reranker: %+v", doc)
				}
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"stale-one", "stale-three", "hidden-body"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("index body leaked: %s", encoded)
				}
			}
			structured, _ := json.Marshal(result.StructuredContent)
			var evidence agentEvidence.PlatformTweetSearchResult
			if err := json.Unmarshal(structured, &evidence); err != nil || len(evidence.Items) != 1 || evidence.Items[0].Content != "current-one" {
				t.Fatalf("evidence: %s %v", structured, err)
			}
		})
	}
}

func TestSearchToolsAuthorityFailureNeverFallsBackToIndex(t *testing.T) {
	for _, name := range []string{"hybrid_search_tweets", "search_tweets_by_semantic"} {
		t.Run(name, func(t *testing.T) {
			authority := &searchAuthorityStub{err: errors.New("private downstream body")}
			reranker := &visibilityReranker{}
			result := callVisibilitySearch(t, searchVisibilityFixture(t, name, authority, reranker), name)
			encoded, _ := json.Marshal(result)
			if !result.IsError || result.StructuredContent != nil || reranker.docs != nil || strings.Contains(string(encoded), "stale") || strings.Contains(string(encoded), "private downstream") {
				t.Fatalf("unsafe failure: %s", encoded)
			}
		})
	}
}

func TestSearchToolsAllCandidatesRemovedRemainEmpty(t *testing.T) {
	for _, name := range []string{"hybrid_search_tweets", "search_tweets_by_semantic"} {
		t.Run(name, func(t *testing.T) {
			authority := &searchAuthorityStub{response: &tweetv1.GetPublicTweetsResponse{}}
			result := callVisibilitySearch(t, searchVisibilityFixture(t, name, authority, nil), name)
			encoded, _ := json.Marshal(result.StructuredContent)
			var evidence agentEvidence.PlatformTweetSearchResult
			if err := json.Unmarshal(encoded, &evidence); err != nil || result.IsError || len(evidence.Items) != 0 {
				t.Fatalf("removed candidates returned: %s %v", encoded, err)
			}
		})
	}
}
