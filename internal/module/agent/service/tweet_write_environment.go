package service

import (
	"context"
	"fmt"

	tweetv1 "twitter-clone/api/tweet/v1"
	agentEnvironment "twitter-clone/internal/module/agent/environment"

	"google.golang.org/grpc"
)

type tweetWriteStateSource interface {
	ListRecentTweetWriteState(
		ctx context.Context,
		userID uint64,
		limit int,
	) (agentEnvironment.TweetWriteStatePage, error)
}

type tweetTimelineClient interface {
	GetUserTimeline(
		ctx context.Context,
		request *tweetv1.GetUserTimelineRequest,
		options ...grpc.CallOption,
	) (*tweetv1.GetUserTimelineResponse, error)
}

type tweetServiceWriteStateReader struct {
	client tweetTimelineClient
}

func (reader tweetServiceWriteStateReader) ListRecentTweetWriteState(
	ctx context.Context,
	userID uint64,
	limit int,
) (agentEnvironment.TweetWriteStatePage, error) {
	if reader.client == nil {
		return agentEnvironment.TweetWriteStatePage{}, fmt.Errorf("tweet timeline client is required")
	}
	if ctx == nil {
		return agentEnvironment.TweetWriteStatePage{}, fmt.Errorf("tweet timeline context is required")
	}
	if userID == 0 || limit <= 0 || limit > 100 {
		return agentEnvironment.TweetWriteStatePage{}, fmt.Errorf("tweet timeline state request is invalid")
	}
	response, err := reader.client.GetUserTimeline(ctx, &tweetv1.GetUserTimelineRequest{
		UserId: userID, Limit: int32(limit), RequestingUserId: userID,
	})
	if err != nil {
		return agentEnvironment.TweetWriteStatePage{}, fmt.Errorf("get user timeline: %w", err)
	}
	if response == nil {
		return agentEnvironment.TweetWriteStatePage{}, fmt.Errorf("get user timeline returned no response")
	}
	states := make([]agentEnvironment.TweetWriteState, 0, len(response.Tweets))
	for _, tweet := range response.Tweets {
		if tweet == nil || tweet.Id == 0 || tweet.UserId == 0 {
			return agentEnvironment.TweetWriteStatePage{}, fmt.Errorf("user timeline contains an invalid tweet")
		}
		// User timelines also contain retweets. Only author-owned tweets are
		// authoritative evidence for create_tweet.
		if tweet.UserId != userID {
			continue
		}
		states = append(states, agentEnvironment.TweetWriteState{
			TweetID: tweet.Id, AuthorID: tweet.UserId,
		})
	}
	return agentEnvironment.TweetWriteStatePage{Tweets: states, HasMore: response.HasMore}, nil
}

// WithTweetWriteStateClient supplies the authoritative read side used by Goal
// verification. It never exposes TweetService mutation methods to Environment.
func WithTweetWriteStateClient(client tweetv1.TweetServiceClient) Option {
	return func(service *AgentService) {
		if client != nil {
			service.tweetWriteStateSource = tweetServiceWriteStateReader{client: client}
		}
	}
}

func (service *AgentService) newTweetWriteEnvironment(
	userID uint64,
) (*agentEnvironment.TweetWriteEnvironment, error) {
	if service == nil || service.runtimeTools == nil {
		return nil, fmt.Errorf("tweet write tool catalog is not configured")
	}
	if service.tweetWriteStateSource == nil {
		return nil, fmt.Errorf("tweet write state source is not configured")
	}
	return agentEnvironment.NewTweetWriteEnvironment(
		service.runtimeTools,
		service.tweetWriteStateSource,
		userID,
	)
}

var _ agentEnvironment.TweetWriteStateReader = tweetServiceWriteStateReader{}
