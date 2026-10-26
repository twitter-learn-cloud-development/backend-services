// Package retrieval provides authority-backed content ports shared by MCP and
// cognitive retrieval. It does not authorize through index payloads.
package retrieval

import (
	"context"
	"errors"
	"strconv"
	"time"

	tweetv1 "twitter-clone/api/tweet/v1"

	"google.golang.org/grpc"
)

const MaxPublicTweetCandidates = 100

var (
	ErrPublicTweetsUnavailable = errors.New("current public tweet content is unavailable")
	ErrInvalidPublicCandidates = errors.New("invalid public tweet candidates")
	ErrPublicTweetContract     = errors.New("invalid current public tweet response")
)

type PublicTweetReader interface {
	ReadPublicTweets(context.Context, []string) ([]*tweetv1.Tweet, error)
}

type PublicTweetClient interface {
	GetPublicTweets(context.Context, *tweetv1.GetPublicTweetsRequest, ...grpc.CallOption) (*tweetv1.GetPublicTweetsResponse, error)
}

type GRPCPublicTweetReader struct {
	client PublicTweetClient
}

func NewGRPCPublicTweetReader(client PublicTweetClient) *GRPCPublicTweetReader {
	return &GRPCPublicTweetReader{client: client}
}

func (r *GRPCPublicTweetReader) ReadPublicTweets(ctx context.Context, candidates []string) ([]*tweetv1.Tweet, error) {
	if len(candidates) > MaxPublicTweetCandidates {
		return nil, ErrInvalidPublicCandidates
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []*tweetv1.Tweet{}, nil
	}
	ids := make([]uint64, 0, len(candidates))
	wanted := make(map[uint64]struct{}, len(candidates))
	for _, candidate := range candidates {
		id, err := strconv.ParseUint(candidate, 10, 64)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != candidate {
			return nil, ErrInvalidPublicCandidates
		}
		if _, exists := wanted[id]; !exists {
			wanted[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if r == nil || r.client == nil {
		return nil, ErrPublicTweetsUnavailable
	}
	readCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	response, err := r.client.GetPublicTweets(readCtx, &tweetv1.GetPublicTweetsRequest{TweetIds: ids})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Do not propagate downstream text, IDs or cached content into logs/LLM.
		return nil, ErrPublicTweetsUnavailable
	}
	if response == nil || len(response.Tweets) > len(ids) {
		return nil, ErrPublicTweetContract
	}
	byID := make(map[uint64]*tweetv1.Tweet, len(response.Tweets))
	for _, tweet := range response.Tweets {
		if tweet == nil || tweet.UserId == 0 || tweet.VisibleType != 0 {
			return nil, ErrPublicTweetContract
		}
		if _, requested := wanted[tweet.Id]; !requested {
			return nil, ErrPublicTweetContract
		}
		if _, duplicate := byID[tweet.Id]; duplicate {
			return nil, ErrPublicTweetContract
		}
		byID[tweet.Id] = tweet
	}
	result := make([]*tweetv1.Tweet, 0, len(byID))
	for _, id := range ids {
		if tweet := byID[id]; tweet != nil {
			result = append(result, tweet)
		}
	}
	return result, nil
}
