package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"twitter-clone/internal/domain"
	"twitter-clone/pkg/logger"
)

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
