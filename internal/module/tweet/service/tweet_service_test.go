package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"twitter-clone/internal/domain"
	"twitter-clone/pkg/logger"
)

type fakeTweetCreateIdempotencyRepository struct {
	mu      sync.Mutex
	records map[string]*domain.TweetCreateIdempotency
}

func newFakeTweetCreateIdempotencyRepository() *fakeTweetCreateIdempotencyRepository {
	return &fakeTweetCreateIdempotencyRepository{records: make(map[string]*domain.TweetCreateIdempotency)}
}

func (r *fakeTweetCreateIdempotencyRepository) Create(_ context.Context, record *domain.TweetCreateIdempotency) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := idempotencyRecordKey(record.UserID, record.IdempotencyKey)
	if _, exists := r.records[key]; exists {
		return domain.ErrTweetCreateIdempotencyExists
	}
	copyRecord := *record
	r.records[key] = &copyRecord
	return nil
}

func (r *fakeTweetCreateIdempotencyRepository) Get(_ context.Context, userID uint64, idempotencyKey string) (*domain.TweetCreateIdempotency, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, exists := r.records[idempotencyRecordKey(userID, idempotencyKey)]
	if !exists {
		return nil, domain.ErrTweetCreateIdempotencyNotFound
	}
	copyRecord := *record
	return &copyRecord, nil
}

func idempotencyRecordKey(userID uint64, idempotencyKey string) string {
	return strconv.FormatUint(userID, 10) + ":" + idempotencyKey
}

func init() {
	logger.InitLogger()
}

func TestCreateTweet_Success(t *testing.T) {
	// 1. Setup
	mockRepo := new(MockTweetRepository)
	mockOutbox := new(MockOutboxEventRepository)
	mockUOW := new(MockUOWManager)

	// 其他依赖暂时传 nil，因为 CreateTweet 只用了 repo, outbox, uow
	svc := NewTweetService(mockRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, mockOutbox, mockUOW)

	ctx := context.Background()
	userID := uint64(123)
	content := "Hello World"
	mediaURLs := []string{}

	// 2. Expectations
	// 预期 repo.Create 会被调用，参数是任意 context 和非空 Tweet
	mockRepo.On("Create", mock.Anything, mock.MatchedBy(func(tweet *domain.Tweet) bool {
		return tweet.UserID == userID && tweet.Content == content
	})).Return(nil)

	// 预期 mockOutbox.Create 会被调用，保存发件箱事件
	mockOutbox.On("Create", mock.Anything, mock.MatchedBy(func(event *domain.OutboxEvent) bool {
		return event.EventType == "TWEET_CREATED" && event.Payload != ""
	})).Return(nil)

	// 3. Execution
	tweet, err := svc.CreateTweet(ctx, userID, content, mediaURLs, 0, nil, 0)

	// 4. Assertions
	assert.NoError(t, err)
	assert.NotNil(t, tweet)
	assert.Equal(t, userID, tweet.UserID)
	assert.Equal(t, content, tweet.Content)

	// 验证所有 Mock 期望是否被满足
	mockRepo.AssertExpectations(t)
	mockOutbox.AssertExpectations(t)
}

func TestCreateTweet_ContentTooLong(t *testing.T) {
	t.Setenv("TWEET_MAX_CONTENT_LENGTH", "10000")
	t.Setenv("TWEET_HARD_MAX_CONTENT_LENGTH", "20000")
	svc := NewTweetService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// 构造超长字符串 (281 字符)
	longContent := ""
	for i := 0; i < DefaultMaxContentLength+1; i++ {
		longContent += "a"
	}

	tweet, err := svc.CreateTweet(context.Background(), 1, longContent, nil, 0, nil, 0)

	assert.Error(t, err)
	assert.Nil(t, tweet)
	assert.ErrorIs(t, err, ErrContentTooLong)
}

func TestCreateTweetReturnsTransactionFailure(t *testing.T) {
	mockRepo := new(MockTweetRepository)
	mockUOW := new(MockUOWManager)
	mockRepo.On("Create", mock.Anything, mock.Anything).Return(errors.New("database unavailable")).Once()
	svc := NewTweetService(mockRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, mockUOW)

	tweet, err := svc.CreateTweet(context.Background(), 7, "must rollback", nil, 0, nil, 0)

	assert.Error(t, err)
	assert.Nil(t, tweet)
	mockRepo.AssertExpectations(t)
}

func TestCreateTweetIdempotentReplaysCommittedTweet(t *testing.T) {
	mockRepo := new(MockTweetRepository)
	mockOutbox := new(MockOutboxEventRepository)
	mockUOW := new(MockUOWManager)
	idempotencyRepo := newFakeTweetCreateIdempotencyRepository()
	committed := &domain.Tweet{ID: 9001, UserID: 42, Content: "stable content"}

	mockRepo.On("Create", mock.Anything, mock.Anything).Run(func(arguments mock.Arguments) {
		tweet := arguments.Get(1).(*domain.Tweet)
		tweet.ID = committed.ID
		tweet.CreatedAt = 100
		tweet.UpdatedAt = 100
	}).Return(nil).Once()
	mockRepo.On("GetByID", mock.Anything, committed.ID).Return(committed, nil).Once()
	mockOutbox.On("Create", mock.Anything, mock.Anything).Return(nil).Once()
	svc := NewTweetService(
		mockRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, mockOutbox, mockUOW,
		WithTweetCreateIdempotencyRepository(idempotencyRepo),
	)

	created, err := svc.CreateTweetIdempotent(context.Background(), 42, "stable content", nil, 0, nil, 0, "run:step:publish")
	assert.NoError(t, err)
	assert.Equal(t, committed.ID, created.ID)

	replayed, err := svc.CreateTweetIdempotent(context.Background(), 42, "stable content", []string{}, 0, []string{}, 0, "run:step:publish")
	assert.NoError(t, err)
	assert.Equal(t, committed.ID, replayed.ID)
	mockRepo.AssertExpectations(t)
	mockOutbox.AssertExpectations(t)
}

func TestCreateTweetIdempotentRejectsDifferentInput(t *testing.T) {
	idempotencyRepo := newFakeTweetCreateIdempotencyRepository()
	idempotencyRepo.records[idempotencyRecordKey(42, "same-key")] = &domain.TweetCreateIdempotency{
		UserID: 42, IdempotencyKey: "same-key", TweetID: 9001,
		InputDigest: tweetCreateInputDigest("original", nil, 0, nil, 0),
	}
	svc := NewTweetService(
		new(MockTweetRepository), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, new(MockUOWManager),
		WithTweetCreateIdempotencyRepository(idempotencyRepo),
	)

	tweet, err := svc.CreateTweetIdempotent(context.Background(), 42, "different", nil, 0, nil, 0, "same-key")

	assert.ErrorIs(t, err, ErrIdempotencyConflict)
	assert.Nil(t, tweet)
}
