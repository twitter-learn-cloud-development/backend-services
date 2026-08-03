package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

const (
	defaultPostingStatsLookback = 10 * time.Minute
	maxPostingStatsLookback     = 24 * time.Hour
)

type AuthorPostingStats struct {
	SampleCount       int
	LatestCreatedAt   int64
	PreviousCreatedAt int64
}

type TweetModerationAction int

const (
	TweetModerationActionUnspecified TweetModerationAction = iota
	TweetModerationActionShadowban
)

type TweetModerationResult struct {
	Applied          bool
	TimelinesCleaned int
	CleanupQueued    bool
}

// GetAuthorPostingStats exposes only the minimum data needed by risk policy.
// Tweet content and persistence details remain inside the Tweet domain.
func (s *TweetService) GetAuthorPostingStats(ctx context.Context, authorID uint64, lookback time.Duration) (AuthorPostingStats, error) {
	if authorID == 0 {
		return AuthorPostingStats{}, ErrInvalidAuthorID
	}
	if lookback <= 0 {
		lookback = defaultPostingStatsLookback
	}
	if lookback > maxPostingStatsLookback {
		lookback = maxPostingStatsLookback
	}

	tweets, err := s.repo.ListByUserID(ctx, authorID, 0, 2)
	if err != nil {
		return AuthorPostingStats{}, fmt.Errorf("list recent author tweets: %w", err)
	}

	cutoff := time.Now().Add(-lookback).UnixMilli()
	stats := AuthorPostingStats{}
	for _, tweet := range tweets {
		if tweet == nil || tweet.CreatedAt < cutoff {
			continue
		}
		stats.SampleCount++
		switch stats.SampleCount {
		case 1:
			stats.LatestCreatedAt = tweet.CreatedAt
		case 2:
			stats.PreviousCreatedAt = tweet.CreatedAt
			return stats, nil
		}
	}
	return stats, nil
}

// ApplyTweetModeration atomically commits the visibility transition and an
// idempotent outbox command. Timeline cleanup is replayed asynchronously.
func (s *TweetService) ApplyTweetModeration(
	ctx context.Context,
	tweetID uint64,
	authorID uint64,
	action TweetModerationAction,
) (TweetModerationResult, error) {
	if tweetID == 0 {
		return TweetModerationResult{}, ErrInvalidTweetID
	}
	if authorID == 0 {
		return TweetModerationResult{}, ErrInvalidAuthorID
	}
	if action != TweetModerationActionShadowban {
		return TweetModerationResult{}, ErrInvalidModerationAction
	}
	if s.uow == nil || s.outboxEventRepo == nil {
		return TweetModerationResult{}, ErrModerationUnavailable
	}

	tweet, err := s.repo.GetByID(ctx, tweetID)
	if err != nil {
		return TweetModerationResult{}, ErrTweetNotFound
	}
	if tweet.UserID != authorID {
		return TweetModerationResult{}, ErrUnauthorized
	}

	now := time.Now().UnixMilli()
	event := events.NewTweetModeratedEvent(
		tweetID,
		authorID,
		events.TweetModerationShadowban,
		now,
	)
	payload, err := json.Marshal(event)
	if err != nil {
		return TweetModerationResult{}, fmt.Errorf("marshal tweet moderation event: %w", err)
	}

	var applied bool
	err = s.uow.Do(ctx, func(txCtx context.Context) error {
		var updateErr error
		applied, updateErr = s.repo.UpdateVisibleType(txCtx, tweetID, authorID, domain.VisibleShadowban)
		if updateErr != nil {
			return fmt.Errorf("apply tweet visibility moderation: %w", updateErr)
		}
		if _, createErr := s.outboxEventRepo.CreateIdempotent(txCtx, &domain.OutboxEvent{
			EventType: events.OutboxEventTypeTweetModerated,
			Payload:   string(payload),
			DedupKey:  &event.EventKey,
			CreatedAt: now,
		}); createErr != nil {
			return fmt.Errorf("enqueue tweet moderation cleanup: %w", createErr)
		}
		return nil
	})
	if err != nil {
		return TweetModerationResult{}, err
	}
	result := TweetModerationResult{Applied: applied, CleanupQueued: true}

	if s.l1Cache != nil {
		_ = s.l1Cache.Delete(fmt.Sprintf("tweet:base:%d", tweetID))
	}
	if s.timelineCache != nil {
		// The durable consumer repeats this invalidation. This eager attempt only
		// narrows the stale-cache window and must not roll back a committed command.
		_ = s.timelineCache.InvalidateBaseTweet(ctx, tweetID)
	}
	return result, nil
}
