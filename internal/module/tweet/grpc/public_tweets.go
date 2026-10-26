package grpc

import (
	"context"
	"errors"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/module/tweet/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *TweetServer) GetPublicTweets(ctx context.Context, req *tweetv1.GetPublicTweetsRequest) (*tweetv1.GetPublicTweetsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "tweet_ids are required")
	}
	tweets, err := s.svc.GetPublicTweets(ctx, req.TweetIds)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidPublicTweetCandidates):
			return nil, status.Error(codes.InvalidArgument, "tweet_ids must contain 1 to 100 nonzero IDs")
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, status.FromContextError(err).Err()
		default:
			return nil, status.Error(codes.Unavailable, "current public tweet content is unavailable")
		}
	}
	response := &tweetv1.GetPublicTweetsResponse{Tweets: make([]*tweetv1.Tweet, 0, len(tweets))}
	for _, tweet := range tweets {
		response.Tweets = append(response.Tweets, domainTweetToProto(tweet))
	}
	return response, nil
}
