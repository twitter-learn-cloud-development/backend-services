package grpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/module/tweet/service"
)

type feedFailureFollowRepo struct{}

func (*feedFailureFollowRepo) BatchIsFollowing(context.Context, uint64, []uint64) (map[uint64]bool, error) {
	return nil, errors.New("private follow SQL")
}
func TestFeedGRPCInputCancellationAndLowSensitiveFailure(t *testing.T) {
	srv := NewTweetServer(service.NewTweetService(nil, &feedFailureFollowRepo{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
	for _, req := range []*tweetv1.GetFeedsRequest{nil, {}} {
		_, err := srv.GetFeeds(context.Background(), req)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := srv.GetFeeds(ctx, &tweetv1.GetFeedsRequest{UserId: 1})
	if status.Code(err) != codes.Canceled {
		t.Fatal(err)
	}
	_, err = srv.GetFeeds(context.Background(), &tweetv1.GetFeedsRequest{UserId: 1})
	if status.Code(err) != codes.Unavailable || status.Convert(err).Message() != "feed is unavailable" {
		t.Fatal(err)
	}
}
