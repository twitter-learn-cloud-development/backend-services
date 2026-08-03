package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

func TestGetAuthorPostingStatsReturnsBoundedSignal(t *testing.T) {
	now := time.Now().UnixMilli()
	repo := new(MockTweetRepository)
	repo.On("ListByUserID", mock.Anything, uint64(42), uint64(0), 2).Return([]*domain.Tweet{
		{ID: 2, UserID: 42, CreatedAt: now - 1_000},
		{ID: 1, UserID: 42, CreatedAt: now - 4_000},
	}, nil).Once()
	service := &TweetService{repo: repo}

	stats, err := service.GetAuthorPostingStats(context.Background(), 42, 10*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, stats.SampleCount)
	require.Equal(t, now-1_000, stats.LatestCreatedAt)
	require.Equal(t, now-4_000, stats.PreviousCreatedAt)
	repo.AssertExpectations(t)
}

func TestApplyTweetModerationCommitsIdempotentCleanupCommand(t *testing.T) {
	ctx := context.Background()
	const tweetID = uint64(900)
	const authorID = uint64(42)

	repo := new(MockTweetRepository)
	outboxRepo := new(MockOutboxEventRepository)
	repo.On("GetByID", mock.Anything, tweetID).Return(&domain.Tweet{ID: tweetID, UserID: authorID}, nil).Twice()
	repo.On("UpdateVisibleType", mock.Anything, tweetID, authorID, domain.VisibleShadowban).
		Return(true, nil).Once()
	repo.On("UpdateVisibleType", mock.Anything, tweetID, authorID, domain.VisibleShadowban).
		Return(false, nil).Once()
	outboxRepo.On("CreateIdempotent", mock.Anything, mock.MatchedBy(func(record *domain.OutboxEvent) bool {
		if record == nil || record.DedupKey == nil || record.EventType != events.OutboxEventTypeTweetModerated {
			return false
		}
		var event events.TweetModeratedEvent
		if err := json.Unmarshal([]byte(record.Payload), &event); err != nil {
			return false
		}
		return event.Validate() == nil && event.TweetID == tweetID && event.AuthorID == authorID &&
			*record.DedupKey == event.EventKey && record.CreatedAt > 0
	})).Return(true, nil).Once()
	outboxRepo.On("CreateIdempotent", mock.Anything, mock.AnythingOfType("*domain.OutboxEvent")).Return(false, nil).Once()

	service := &TweetService{
		repo:            repo,
		outboxEventRepo: outboxRepo,
		uow:             new(MockUOWManager),
	}

	first, err := service.ApplyTweetModeration(ctx, tweetID, authorID, TweetModerationActionShadowban)
	require.NoError(t, err)
	require.True(t, first.Applied)
	require.True(t, first.CleanupQueued)
	require.Zero(t, first.TimelinesCleaned)

	second, err := service.ApplyTweetModeration(ctx, tweetID, authorID, TweetModerationActionShadowban)
	require.NoError(t, err)
	require.False(t, second.Applied)
	require.True(t, second.CleanupQueued)
	require.Zero(t, second.TimelinesCleaned)
	repo.AssertExpectations(t)
	outboxRepo.AssertExpectations(t)
}

func TestApplyTweetModerationFailsWhenCleanupCommandCannotCommit(t *testing.T) {
	const tweetID = uint64(901)
	const authorID = uint64(43)
	repo := new(MockTweetRepository)
	outboxRepo := new(MockOutboxEventRepository)
	repo.On("GetByID", mock.Anything, tweetID).Return(&domain.Tweet{ID: tweetID, UserID: authorID}, nil).Once()
	repo.On("UpdateVisibleType", mock.Anything, tweetID, authorID, domain.VisibleShadowban).Return(true, nil).Once()
	outboxRepo.On("CreateIdempotent", mock.Anything, mock.AnythingOfType("*domain.OutboxEvent")).
		Return(false, errors.New("outbox unavailable")).Once()

	service := &TweetService{repo: repo, outboxEventRepo: outboxRepo, uow: new(MockUOWManager)}
	result, err := service.ApplyTweetModeration(context.Background(), tweetID, authorID, TweetModerationActionShadowban)
	require.ErrorContains(t, err, "enqueue tweet moderation cleanup")
	require.False(t, result.Applied)
	require.False(t, result.CleanupQueued)
	repo.AssertExpectations(t)
	outboxRepo.AssertExpectations(t)
}
