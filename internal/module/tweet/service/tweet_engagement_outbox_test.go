package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.opentelemetry.io/otel/trace"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

func TestPersistTweetLikedWritesBusinessMutationAndOutboxInUnitOfWork(t *testing.T) {
	likeRepo := new(MockLikeRepository)
	outboxRepo := new(MockOutboxEventRepository)
	uowManager := new(MockUOWManager)
	likeRepo.On("ApplyLike", mock.Anything, uint64(77), uint64(9001)).Return(&domain.Like{ID: 61, UserID: 77, TweetID: 9001, CreatedAt: 1700000000000}, true, nil).Once()
	outboxRepo.On("Create", mock.Anything, mock.MatchedBy(func(record *domain.OutboxEvent) bool {
		if record.EventType != "TWEET_LIKED" {
			return false
		}
		var event events.TweetLikedEvent
		return json.Unmarshal([]byte(record.Payload), &event) == nil && event.TweetID == 9001 &&
			event.UserID == 77 && event.TweetUser == 42 && event.OccurredAtUnixMS == 1700000000000 &&
			event.LikeID == 61 && event.EventKey == "like:61" && event.SchemaVersion == events.EngagementSchemaVersion && record.DedupKey != nil && *record.DedupKey == event.EventKey && record.CreatedAt == event.OccurredAtUnixMS && event.Traceparent == "00-01000000000000000000000000000000-0200000000000000-01"
	})).Return(nil).Once()
	service := &TweetService{
		likeRepo: likeRepo, outboxEventRepo: outboxRepo, uow: uowManager,
		subjectWriteFenceRepo: AllowTweetSubjectWrites(),
	}

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
	err := service.persistTweetLiked(trace.ContextWithSpanContext(context.Background(), spanContext), &events.TweetLikedEvent{
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
	likeRepo.On("ApplyLike", mock.Anything, uint64(77), uint64(9001)).Return(&domain.Like{ID: 61, UserID: 77, TweetID: 9001, CreatedAt: 1700000000000}, true, nil).Once()
	outboxRepo.On("Create", mock.Anything, mock.Anything).Return(errors.New("outbox unavailable")).Once()
	service := &TweetService{
		likeRepo: likeRepo, outboxEventRepo: outboxRepo, uow: uowManager,
		subjectWriteFenceRepo: AllowTweetSubjectWrites(),
	}

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
			event.OccurredAtUnixMS == 1700000000000 && event.EventKey == "comment:81" && event.SchemaVersion == events.EngagementSchemaVersion && record.DedupKey != nil && *record.DedupKey == event.EventKey
	})).Return(nil).Once()
	service := &TweetService{
		commentRepo: commentRepo, outboxEventRepo: outboxRepo, uow: uowManager,
		subjectWriteFenceRepo: AllowTweetSubjectWrites(),
	}

	err := service.persistCommentCreated(context.Background(), &domain.Comment{
		TweetID: 9001, UserID: 77, Content: "useful reply",
	}, 42)
	if err != nil {
		t.Fatal(err)
	}
	commentRepo.AssertExpectations(t)
	outboxRepo.AssertExpectations(t)
}

func TestPersistDuplicateLikeDoesNotProduceAnotherEvent(t *testing.T) {
	likeRepo := new(MockLikeRepository)
	outboxRepo := new(MockOutboxEventRepository)
	likeRepo.On("ApplyLike", mock.Anything, uint64(7), uint64(91)).Return(&domain.Like{ID: 12, UserID: 7, TweetID: 91, CreatedAt: 1}, false, nil).Once()
	service := &TweetService{likeRepo: likeRepo, outboxEventRepo: outboxRepo, uow: new(MockUOWManager), subjectWriteFenceRepo: AllowTweetSubjectWrites()}
	event := &events.TweetLikedEvent{TweetID: 91, UserID: 7, TweetUser: 42}
	if err := service.persistTweetLiked(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	outboxRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	likeRepo.AssertExpectations(t)
}
func TestPersistEngagementRejectsInvalidRepositoryIdentity(t *testing.T) {
	for _, created := range []bool{true, false} {
		likeRepo := new(MockLikeRepository)
		outboxRepo := new(MockOutboxEventRepository)
		likeRepo.On("ApplyLike", mock.Anything, uint64(7), uint64(91)).Return(&domain.Like{ID: 12, UserID: 8, TweetID: 91, CreatedAt: 1}, created, nil).Once()
		service := &TweetService{likeRepo: likeRepo, outboxEventRepo: outboxRepo, uow: new(MockUOWManager), subjectWriteFenceRepo: AllowTweetSubjectWrites()}
		if service.persistTweetLiked(context.Background(), &events.TweetLikedEvent{TweetID: 91, UserID: 7, TweetUser: 42}) == nil {
			t.Fatal("wrong persisted identity accepted")
		}
		outboxRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	}
	commentRepo := new(MockCommentRepository)
	outboxRepo := new(MockOutboxEventRepository)
	commentRepo.On("Create", mock.Anything, mock.Anything).Return(nil).Once()
	service := &TweetService{commentRepo: commentRepo, outboxEventRepo: outboxRepo, uow: new(MockUOWManager), subjectWriteFenceRepo: AllowTweetSubjectWrites()}
	if service.persistCommentCreated(context.Background(), &domain.Comment{UserID: 7, TweetID: 91}, 42) == nil {
		t.Fatal("missing persisted comment identity accepted")
	}
	outboxRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
}
