package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

func TestPersistTweetLikedWritesBusinessMutationAndOutboxInUnitOfWork(t *testing.T) {
	likeRepo := new(MockLikeRepository)
	outboxRepo := new(MockOutboxEventRepository)
	uowManager := new(MockUOWManager)
	likeRepo.On("Like", mock.Anything, uint64(77), uint64(9001)).Return(nil).Once()
	outboxRepo.On("Create", mock.Anything, mock.MatchedBy(func(record *domain.OutboxEvent) bool {
		if record.EventType != "TWEET_LIKED" {
			return false
		}
		var event events.TweetLikedEvent
		return json.Unmarshal([]byte(record.Payload), &event) == nil && event.TweetID == 9001 &&
			event.UserID == 77 && event.TweetUser == 42 && event.OccurredAtUnixMS == 1700000000000
	})).Return(nil).Once()
	service := &TweetService{likeRepo: likeRepo, outboxEventRepo: outboxRepo, uow: uowManager}

	err := service.persistTweetLiked(context.Background(), &events.TweetLikedEvent{
		TweetID: 9001, UserID: 77, TweetUser: 42, OccurredAtUnixMS: 1700000000000,
	})
	if err != nil {
		t.Fatal(err)
	}
	likeRepo.AssertExpectations(t)
	outboxRepo.AssertExpectations(t)
}

func TestPersistTweetLikedReturnsOutboxFailure(t *testing.T) {
	likeRepo := new(MockLikeRepository)
	outboxRepo := new(MockOutboxEventRepository)
	uowManager := new(MockUOWManager)
	likeRepo.On("Like", mock.Anything, uint64(77), uint64(9001)).Return(nil).Once()
	outboxRepo.On("Create", mock.Anything, mock.Anything).Return(errors.New("outbox unavailable")).Once()
	service := &TweetService{likeRepo: likeRepo, outboxEventRepo: outboxRepo, uow: uowManager}

	err := service.persistTweetLiked(context.Background(), &events.TweetLikedEvent{
		TweetID: 9001, UserID: 77, TweetUser: 42, OccurredAtUnixMS: 1700000000000,
	})
	if err == nil {
		t.Fatal("persistTweetLiked() error = nil")
	}
}

func TestPersistCommentCreatedWritesCommentAndOutboxInUnitOfWork(t *testing.T) {
	commentRepo := new(MockCommentRepository)
	outboxRepo := new(MockOutboxEventRepository)
	uowManager := new(MockUOWManager)
	commentRepo.On("Create", mock.Anything, mock.Anything).Run(func(arguments mock.Arguments) {
		comment := arguments.Get(1).(*domain.Comment)
		comment.ID = 81
		comment.CreatedAt = 1700000000000
	}).Return(nil).Once()
	outboxRepo.On("Create", mock.Anything, mock.MatchedBy(func(record *domain.OutboxEvent) bool {
		if record.EventType != "COMMENT_CREATED" {
			return false
		}
		var event events.CommentCreatedEvent
		return json.Unmarshal([]byte(record.Payload), &event) == nil && event.CommentID == 81 &&
			event.TweetID == 9001 && event.UserID == 77 && event.TweetUser == 42 &&
			event.OccurredAtUnixMS == 1700000000000
	})).Return(nil).Once()
	service := &TweetService{commentRepo: commentRepo, outboxEventRepo: outboxRepo, uow: uowManager}

	err := service.persistCommentCreated(context.Background(), &domain.Comment{
		TweetID: 9001, UserID: 77, Content: "useful reply",
	}, 42)
	if err != nil {
		t.Fatal(err)
	}
	commentRepo.AssertExpectations(t)
	outboxRepo.AssertExpectations(t)
}
