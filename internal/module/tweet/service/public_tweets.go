package service

import (
	"context"
	"errors"
	"fmt"

	"twitter-clone/internal/domain"
)

const MaxPublicTweetCandidates = 100

var ErrInvalidPublicTweetCandidates = errors.New("invalid public tweet candidates")

// GetPublicTweets reads the current authority once, without L1/L2 caches.
// Public retrieval never accepts caller-controlled identity as permission to
// read private/follower-only/hidden content.
func (s *TweetService) GetPublicTweets(ctx context.Context, ids []uint64) ([]*domain.Tweet, error) {
	if len(ids) == 0 || len(ids) > MaxPublicTweetCandidates {
		return nil, ErrInvalidPublicTweetCandidates
	}
	unique := make([]uint64, 0, len(ids))
	wanted := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			return nil, ErrInvalidPublicTweetCandidates
		}
		if _, exists := wanted[id]; !exists {
			wanted[id] = struct{}{}
			unique = append(unique, id)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tweets, err := s.repo.GetByIDs(ctx, unique)
	if err != nil {
		return nil, fmt.Errorf("read current public tweets: %w", err)
	}
	byID := make(map[uint64]*domain.Tweet, len(tweets))
	for _, tweet := range tweets {
		if tweet == nil || tweet.UserID == 0 || tweet.DeletedAt != 0 || tweet.VisibleType != domain.VisiblePublic {
			continue
		}
		if _, requested := wanted[tweet.ID]; requested {
			byID[tweet.ID] = tweet
		}
	}
	result := make([]*domain.Tweet, 0, len(byID))
	for _, id := range unique {
		if tweet := byID[id]; tweet != nil {
			result = append(result, tweet)
		}
	}
	return result, nil
}
