package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/pkg/logger"
)

const (
	tweetDeleteCacheTimeout       = 800 * time.Millisecond
	tweetDeleteTransactionTimeout = 3 * time.Second
)

func (s *TweetService) deleteTweetWithOutbox(ctx context.Context, tweetID, userID uint64) error {
	ctx, span := otel.Tracer("tweet-service").Start(ctx, "TweetService.DeleteTweet")
	defer span.End()
	ctx, cancelTransaction := context.WithTimeout(ctx, tweetDeleteTransactionTimeout)
	defer cancelTransaction()
	if tweetID == 0 {
		return ErrInvalidTweetID
	}
	if userID == 0 {
		return ErrInvalidAuthorID
	}
	reader, ok := s.repo.(domain.TweetDeletionRepository)
	if !ok || s.uow == nil || s.outboxEventRepo == nil {
		return ErrTweetDeletionUnavailable
	}
	stage := "begin"
	err := s.uow.Do(ctx, func(txCtx context.Context) error {
		stage = "read_identity"
		tweet, err := reader.GetForDeletion(txCtx, tweetID)
		if errors.Is(err, domain.ErrTweetDeletionNotFound) {
			return ErrTweetNotFound
		}
		if err != nil {
			return fmt.Errorf("read tweet for deletion: %w", err)
		}
		if tweet == nil || tweet.ID != tweetID || tweet.DeletedAt != 0 {
			return errors.New("invalid tweet deletion identity")
		}
		if tweet.UserID != userID {
			return ErrUnauthorized
		}
		stage = "delete"
		if err := s.repo.Delete(txCtx, tweetID); err != nil {
			return fmt.Errorf("delete tweet in transaction: %w", err)
		}
		// Legacy payload/routing remains readable by existing Canal/consumers.
		payload, err := json.Marshal(events.TweetDeletedEvent{TweetID: tweetID, AuthorID: userID})
		if err != nil {
			return fmt.Errorf("marshal tweet deletion: %w", err)
		}
		key := fmt.Sprintf("tweet-deleted:%d", tweetID)
		stage = "outbox"
		// A duplicate key is an error, so inconsistent historical state cannot
		// silently commit a new delete without its event.
		if err := s.outboxEventRepo.Create(txCtx, &domain.OutboxEvent{
			EventType: events.OutboxEventTypeTweetDeleted, Payload: string(payload),
			DedupKey: &key, CreatedAt: time.Now().UnixMilli(),
		}); err != nil {
			return fmt.Errorf("enqueue tweet deletion: %w", err)
		}
		stage = "commit"
		return nil
	})
	if err != nil {
		span.SetStatus(codes.Error, "tweet_deletion_failed")
		span.SetAttributes(attribute.String("deletion.stage", stage))
		logger.Error(ctx, "tweet deletion transaction failed", zap.String("stage", stage))
		return err
	}
	span.SetAttributes(attribute.Bool("deletion.committed", true))
	logger.Info(ctx, "tweet deletion and outbox committed")
	// A caller cancellation after commit must not skip this eager attempt;
	// the durable consumer can repeat it. Never report post-commit cache
	// failure as rollback of the authoritative transaction.
	if s.l1Cache != nil {
		_ = s.l1Cache.Delete(fmt.Sprintf("tweet:base:%d", tweetID))
	}
	if s.timelineCache != nil {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tweetDeleteCacheTimeout)
		defer cancel()
		if err := s.timelineCache.InvalidateBaseTweet(cacheCtx, tweetID); err != nil {
			logger.Error(ctx, "committed tweet deletion cache invalidation failed")
		}
	}
	return nil
}
