package tools

import (
	"context"
	"strconv"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/module/agent/retrieval"
	"twitter-clone/pkg/es"
)

// Only authority content can be sent to rerankers or emitted as Evidence.
func hydrateSearchCandidates(ctx context.Context, client retrieval.PublicTweetClient, candidates []es.TweetDocument) ([]es.TweetDocument, error) {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	tweets, err := retrieval.NewGRPCPublicTweetReader(client).ReadPublicTweets(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]es.TweetDocument, 0, len(tweets))
	for _, tweet := range tweets {
		result = append(result, es.TweetDocument{
			ID: strconv.FormatUint(tweet.Id, 10), UserID: strconv.FormatUint(tweet.UserId, 10),
			Content: tweet.Content, CreatedAt: tweet.CreatedAt, LikeCount: int(tweet.LikeCount),
		})
	}
	return result, nil
}

func publicSearchResults(documents []es.TweetDocument) []*tweetv1.Tweet {
	result := make([]*tweetv1.Tweet, 0, len(documents))
	for _, document := range documents {
		id, _ := strconv.ParseUint(document.ID, 10, 64)
		userID, _ := strconv.ParseUint(document.UserID, 10, 64)
		result = append(result, &tweetv1.Tweet{
			Id: id, UserId: userID, Content: document.Content,
			CreatedAt: document.CreatedAt, LikeCount: int32(document.LikeCount),
		})
	}
	return result
}
