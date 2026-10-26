package rag

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc"
	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/module/agent/retrieval"
	"twitter-clone/pkg/es"
)

type knowledgeAuthorityStub struct {
	response *tweetv1.GetPublicTweetsResponse
	err      error
	ids      []uint64
	calls    int
}

func (s *knowledgeAuthorityStub) GetPublicTweets(_ context.Context, req *tweetv1.GetPublicTweetsRequest, _ ...grpc.CallOption) (*tweetv1.GetPublicTweetsResponse, error) {
	s.ids = req.TweetIds
	s.calls++
	return s.response, s.err
}

type knowledgeIndexTransport struct{}

func (knowledgeIndexTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{"hits":{"hits":[{"_source":{"id":"1","user_id":"7","content":"stale-public","created_at":1}},{"_source":{"id":"2","user_id":"7","content":"deleted-hidden-body","created_at":1}}]}}`
	if req.Method == http.MethodHead {
		body = ""
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"X-Elastic-Product": []string{"Elasticsearch"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func knowledgeVisibilityFixture(t *testing.T, authority *knowledgeAuthorityStub) *MemoryManager {
	t.Helper()
	index, err := es.NewClient(es.Config{Addresses: []string{"http://fixture.invalid"}, HTTPClient: &http.Client{Transport: knowledgeIndexTransport{}}})
	if err != nil {
		t.Fatal(err)
	}
	return NewMemoryManager(nil, index, nil, nil, "", "", WithPublicTweetReader(retrieval.NewGRPCPublicTweetReader(authority)))
}

func TestKnowledgeContextUsesCurrentPublicBodies(t *testing.T) {
	authority := &knowledgeAuthorityStub{response: &tweetv1.GetPublicTweetsResponse{Tweets: []*tweetv1.Tweet{{Id: 1, UserId: 7, Content: "current-public", CreatedAt: 1700000000000}}}}
	manager := knowledgeVisibilityFixture(t, authority)
	block, chunks, err := manager.BuildContextBlock(context.Background(), 9, "query", "", nil, 1200)
	if err != nil || len(chunks) != 1 || chunks[0].Timestamp != 1700000000 || !strings.Contains(block, "current-public") || strings.Contains(block, "stale") || strings.Contains(block, "hidden") {
		t.Fatalf("unsafe context: %q %+v %v", block, chunks, err)
	}
	if authority.calls != 1 {
		t.Fatalf("authority calls=%d", authority.calls)
	}
	seen := map[uint64]bool{}
	for _, id := range authority.ids {
		seen[id] = true
	}
	if !reflect.DeepEqual(seen, map[uint64]bool{1: true, 2: true}) {
		t.Fatalf("candidate batch: %v", authority.ids)
	}
}

func TestKnowledgeAuthorityFailureIsNotEmptySuccess(t *testing.T) {
	authority := &knowledgeAuthorityStub{err: errors.New("sensitive authority error")}
	manager := knowledgeVisibilityFixture(t, authority)
	block, chunks, err := manager.BuildContextBlock(context.Background(), 9, "query", "", nil, 1200)
	if !errors.Is(err, retrieval.ErrPublicTweetsUnavailable) || block != "" || chunks != nil {
		t.Fatalf("failure hidden: %q %+v %v", block, chunks, err)
	}
	manager.publicTweets = nil
	if _, err := manager.SearchHybridKnowledge(context.Background(), "query", 5); !errors.Is(err, retrieval.ErrPublicTweetsUnavailable) {
		t.Fatalf("unconfigured authority accepted: %v", err)
	}
}

func TestKnowledgeRemovedCandidatesProduceNoContext(t *testing.T) {
	authority := &knowledgeAuthorityStub{response: &tweetv1.GetPublicTweetsResponse{}}
	manager := knowledgeVisibilityFixture(t, authority)
	block, chunks, err := manager.BuildContextBlock(context.Background(), 9, "query", "", nil, 1200)
	if err != nil || block != "" || len(chunks) != 0 {
		t.Fatalf("removed candidates returned: %q %+v %v", block, chunks, err)
	}
}
