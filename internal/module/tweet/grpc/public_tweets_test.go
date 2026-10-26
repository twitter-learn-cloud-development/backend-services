package grpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/tweet/service"
)

type publicTweetRepo struct {
	domain.TweetRepository
	tweets []*domain.Tweet
	err    error
	calls  int
}

func (r *publicTweetRepo) GetByIDs(context.Context, []uint64) ([]*domain.Tweet, error) {
	r.calls++
	return r.tweets, r.err
}

func publicTweetServer(repo *publicTweetRepo) *TweetServer {
	return NewTweetServer(service.NewTweetService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
}

func TestGetPublicTweetsGRPCContract(t *testing.T) {
	assertMessageFieldNumbers(t, "GetPublicTweetsRequest", map[protoreflect.Name]protoreflect.FieldNumber{"tweet_ids": 1})
	assertMessageFieldNumbers(t, "GetPublicTweetsResponse", map[protoreflect.Name]protoreflect.FieldNumber{"tweets": 1})
	if tweetv1.File_api_tweet_v1_tweet_proto.Services().ByName("TweetService").Methods().ByName("GetPublicTweets") == nil {
		t.Fatal("public retrieval RPC missing")
	}
	repo := &publicTweetRepo{tweets: []*domain.Tweet{{ID: 1, UserID: 7, Content: "current"}, {ID: 2, UserID: 7, VisibleType: domain.VisibleShadowban}}}
	got, err := publicTweetServer(repo).GetPublicTweets(context.Background(), &tweetv1.GetPublicTweetsRequest{TweetIds: []uint64{1, 2}})
	if err != nil || len(got.Tweets) != 1 || got.Tweets[0].Content != "current" || repo.calls != 1 {
		t.Fatalf("result: %+v %v", got, err)
	}
}

func TestGetPublicTweetsGRPCErrors(t *testing.T) {
	repo := &publicTweetRepo{err: errors.New("sensitive database detail")}
	srv := publicTweetServer(repo)
	for _, req := range []*tweetv1.GetPublicTweetsRequest{nil, {}, {TweetIds: []uint64{0}}, {TweetIds: make([]uint64, 101)}} {
		_, err := srv.GetPublicTweets(context.Background(), req)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("bad input code = %v", err)
		}
	}
	if repo.calls != 0 {
		t.Fatal("invalid input read authority")
	}
	_, err := srv.GetPublicTweets(context.Background(), &tweetv1.GetPublicTweetsRequest{TweetIds: []uint64{1}})
	if status.Code(err) != codes.Unavailable || status.Convert(err).Message() != "current public tweet content is unavailable" {
		t.Fatalf("database detail exposed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = srv.GetPublicTweets(ctx, &tweetv1.GetPublicTweetsRequest{TweetIds: []uint64{1}})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("cancel code = %v", err)
	}
}
