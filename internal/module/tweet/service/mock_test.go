package service

import (
	"context"

	"github.com/stretchr/testify/mock"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

// MockTweetRepository 模拟 TweetRepository
type MockTweetRepository struct {
	mock.Mock
}

func (m *MockTweetRepository) Create(ctx context.Context, tweet *domain.Tweet) error {
	args := m.Called(ctx, tweet)
	return args.Error(0)
}

func (m *MockTweetRepository) Delete(ctx context.Context, id uint64) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockTweetRepository) GetByID(ctx context.Context, id uint64) (*domain.Tweet, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Tweet), args.Error(1)
}

func (m *MockTweetRepository) ListByUserID(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, error) {
	args := m.Called(ctx, userID, cursor, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Tweet), args.Error(1)
}

func (m *MockTweetRepository) GetFeeds(ctx context.Context, userIDs []uint64, cursor uint64, limit int) ([]*domain.Tweet, error) {
	args := m.Called(ctx, userIDs, cursor, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Tweet), args.Error(1)
}

func (m *MockTweetRepository) Search(ctx context.Context, query string, cursor uint64, limit int) ([]*domain.Tweet, error) {
	args := m.Called(ctx, query, cursor, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Tweet), args.Error(1)
}

func (m *MockTweetRepository) GetByIDs(ctx context.Context, ids []uint64) ([]*domain.Tweet, error) {
	args := m.Called(ctx, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Tweet), args.Error(1)
}

func (m *MockTweetRepository) ListAll(ctx context.Context, cursor uint64, limit int) ([]*domain.Tweet, error) {
	args := m.Called(ctx, cursor, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Tweet), args.Error(1)
}

func (m *MockTweetRepository) GetReplies(ctx context.Context, parentID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, error) {
	args := m.Called(ctx, parentID, cursor, limit)
	if args.Get(0) == nil {
		return nil, 0, args.Error(2)
	}
	return args.Get(0).([]*domain.Tweet), args.Get(1).(uint64), args.Error(2)
}

// MockEventProducer 模拟 EventProducer
type MockEventProducer struct {
	mock.Mock
}

func (m *MockEventProducer) PublishTweetCreated(ctx context.Context, event *events.TweetCreatedEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockEventProducer) PublishTweetDeleted(ctx context.Context, event *events.TweetDeletedEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockEventProducer) PublishTweetLiked(ctx context.Context, event *events.TweetLikedEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *MockEventProducer) PublishCommentCreated(ctx context.Context, event *events.CommentCreatedEvent) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}
