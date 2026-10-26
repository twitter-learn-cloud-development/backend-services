package retrieval

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	tweetv1 "twitter-clone/api/tweet/v1"
)

type publicClientStub struct {
	response *tweetv1.GetPublicTweetsResponse
	err      error
	request  *tweetv1.GetPublicTweetsRequest
	calls    int
}

func (s *publicClientStub) GetPublicTweets(ctx context.Context, req *tweetv1.GetPublicTweetsRequest, _ ...grpc.CallOption) (*tweetv1.GetPublicTweetsResponse, error) {
	s.calls++
	s.request = req
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 1500*time.Millisecond {
		panic("authority read is not bounded")
	}
	return s.response, s.err
}

func TestPublicReaderBatchOrderAndOmissions(t *testing.T) {
	stub := &publicClientStub{response: &tweetv1.GetPublicTweetsResponse{Tweets: []*tweetv1.Tweet{{Id: 1, UserId: 7, Content: "new"}, {Id: 3, UserId: 7, Content: "first"}}}}
	got, err := NewGRPCPublicTweetReader(stub).ReadPublicTweets(context.Background(), []string{"3", "2", "1", "3"})
	if err != nil || len(got) != 2 || got[0].Id != 3 || got[1].Id != 1 || got[1].Content != "new" || stub.calls != 1 || !reflect.DeepEqual(stub.request.TweetIds, []uint64{3, 2, 1}) {
		t.Fatalf("batch: %+v %v request=%+v", got, err, stub.request)
	}
}

func TestPublicReaderRejectsMalformedAuthority(t *testing.T) {
	for name, tweets := range map[string][]*tweetv1.Tweet{
		"nil": {nil}, "unknown": {{Id: 9, UserId: 7}}, "zero owner": {{Id: 1}},
		"private": {{Id: 1, UserId: 7, VisibleType: 2}}, "shadowban": {{Id: 1, UserId: 7, VisibleType: 4}},
		"duplicate": {{Id: 1, UserId: 7}, {Id: 1, UserId: 7}},
	} {
		t.Run(name, func(t *testing.T) {
			stub := &publicClientStub{response: &tweetv1.GetPublicTweetsResponse{Tweets: tweets}}
			got, err := NewGRPCPublicTweetReader(stub).ReadPublicTweets(context.Background(), []string{"1", "2"})
			if got != nil || !errors.Is(err, ErrPublicTweetContract) {
				t.Fatalf("malformed authority accepted: %+v %v", got, err)
			}
		})
	}
	got, err := NewGRPCPublicTweetReader(&publicClientStub{}).ReadPublicTweets(context.Background(), []string{"1"})
	if got != nil || !errors.Is(err, ErrPublicTweetContract) {
		t.Fatalf("nil response accepted: %+v %v", got, err)
	}
}

func TestPublicReaderFailureAndInputBoundaries(t *testing.T) {
	stub := &publicClientStub{err: errors.New("secret downstream body")}
	reader := NewGRPCPublicTweetReader(stub)
	for _, ids := range [][]string{{"0"}, {"01"}, {"+1"}, {"x"}, {"18446744073709551616"}, make([]string, 101)} {
		if _, err := reader.ReadPublicTweets(context.Background(), ids); !errors.Is(err, ErrInvalidPublicCandidates) {
			t.Fatalf("bad candidates accepted: %v", err)
		}
	}
	if stub.calls != 0 {
		t.Fatal("bad input invoked authority")
	}
	_, err := reader.ReadPublicTweets(context.Background(), []string{"1"})
	if !errors.Is(err, ErrPublicTweetsUnavailable) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe failure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reader.ReadPublicTweets(ctx, []string{"1"})
	if !errors.Is(err, context.Canceled) || stub.calls != 1 {
		t.Fatalf("cancel ignored: %v", err)
	}
	if _, err = NewGRPCPublicTweetReader(nil).ReadPublicTweets(context.Background(), []string{"1"}); !errors.Is(err, ErrPublicTweetsUnavailable) {
		t.Fatalf("missing port: %v", err)
	}
	got, err := reader.ReadPublicTweets(context.Background(), nil)
	if err != nil || len(got) != 0 || stub.calls != 1 {
		t.Fatalf("empty candidates: %+v %v", got, err)
	}
}
