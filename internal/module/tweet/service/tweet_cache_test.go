package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/tweet/cache"
)

func TestGetTweet_MultiLevelCacheFlow(t *testing.T) {
	// 1. 开启 miniredis
	mr, err := miniredis.Run()
	assert.NoError(t, err)
	defer mr.Close()

	rClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer rClient.Close()

	// 2. 初始化一级/二级缓存
	timelineCache := cache.NewTimelineCache(rClient)
	l1Cache, err := cache.NewL1Cache(rClient, 10) // 10MB
	assert.NoError(t, err)
	defer l1Cache.Close()

	// 3. Mock Repositories
	mockRepo := new(MockTweetRepository)
	mockLike := new(MockLikeRepository)
	mockComment := new(MockCommentRepository)
	mockPoll := new(MockPollRepository)
	mockBookmark := new(MockBookmarkRepository)
	mockRetweet := new(MockRetweetRepository)
	mockProducer := new(MockEventProducer)

	// 4. 创建 TweetService
	svc := NewTweetService(
		mockRepo,
		nil, // followRepo
		mockLike,
		mockComment,
		mockPoll,
		mockBookmark,
		mockRetweet,
		timelineCache,
		mockProducer,
		l1Cache,
		nil, // outboxEventRepo
		nil, // uowManager
	)

	tweetID := uint64(1001)
	userID := uint64(2001)
	baseTweet := &domain.Tweet{
		ID:          tweetID,
		UserID:      userID,
		Content:     "Cache test tweet content",
		VisibleType: domain.VisiblePublic,
	}

	key := fmt.Sprintf("tweet:base:%d", tweetID)

	// Mock stats/relations responses
	mockLike.On("GetLikeCount", mock.Anything, tweetID).Return(int64(10), nil)
	mockLike.On("IsLiked", mock.Anything, userID, tweetID).Return(true, nil)
	mockComment.On("GetCommentCount", mock.Anything, tweetID).Return(int64(5), nil)
	mockLike.On("BatchGetLikeCounts", mock.Anything, []uint64{tweetID}).Return(map[uint64]int64{tweetID: 10}, nil)
	mockRetweet.On("BatchGetRetweetCounts", mock.Anything, []uint64{tweetID}).Return(map[uint64]int64{tweetID: 2}, nil)
	mockLike.On("BatchIsLiked", mock.Anything, userID, []uint64{tweetID}).Return(map[uint64]bool{tweetID: true}, nil)
	mockRetweet.On("BatchIsRetweeted", mock.Anything, userID, []uint64{tweetID}).Return(map[uint64]bool{tweetID: false}, nil)
	mockBookmark.On("BatchIsBookmarked", mock.Anything, userID, []uint64{tweetID}).Return(map[uint64]bool{tweetID: false}, nil)
	mockComment.On("BatchGetCommentCounts", mock.Anything, []uint64{tweetID}).Return(map[uint64]int64{tweetID: 5}, nil)
	mockPoll.On("GetByTweetIDs", mock.Anything, []uint64{tweetID}).Return(nil, nil)

	// ==========================================
	// Test Case 1: Cache Miss -> DB Hit -> Fill Cache
	// ==========================================
	mockRepo.On("GetByID", mock.Anything, tweetID).Return(baseTweet, nil).Once()

	ctx := context.Background()
	resTweet, err := svc.GetTweet(ctx, tweetID, userID)
	assert.NoError(t, err)
	assert.Equal(t, "Cache test tweet content", resTweet.Content)
	assert.Equal(t, 10, resTweet.LikeCount)

	// 此时 L2 Redis 中应该已被回写，验证之
	l2Val, err := rClient.Get(ctx, key).Result()
	assert.NoError(t, err)
	var l2Tweet domain.Tweet
	err = json.Unmarshal([]byte(l2Val), &l2Tweet)
	assert.NoError(t, err)
	assert.Equal(t, tweetID, l2Tweet.ID)

	// ==========================================
	// Test Case 2: L1 Cache Hit (微秒级，不打 DB，不打 Redis)
	// ==========================================
	// 我们可以清除 Redis 中的值，如果依旧成功获取，说明命中 L1
	err = rClient.Del(ctx, key).Err()
	assert.NoError(t, err)

	resTweet2, err := svc.GetTweet(ctx, tweetID, userID)
	assert.NoError(t, err)
	assert.Equal(t, "Cache test tweet content", resTweet2.Content)

	// ==========================================
	// Test Case 3: L1 Miss -> L2 Hit
	// ==========================================
	// 清空 L1 本地缓存
	err = l1Cache.Delete(key)
	assert.NoError(t, err)

	// 重新写入 Redis L2
	err = timelineCache.SetBaseTweet(ctx, baseTweet)
	assert.NoError(t, err)

	// 读取，此时应命中 L2 并自动填充 L1
	resTweet3, err := svc.GetTweet(ctx, tweetID, userID)
	assert.NoError(t, err)
	assert.Equal(t, "Cache test tweet content", resTweet3.Content)

	// 再次验证 L1 是否被重新填充
	_, err = l1Cache.Get(key)
	assert.NoError(t, err)

	mockRepo.AssertExpectations(t)
}

func TestGetTweet_Singleflight(t *testing.T) {
	mr, err := miniredis.Run()
	assert.NoError(t, err)
	defer mr.Close()

	rClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer rClient.Close()

	timelineCache := cache.NewTimelineCache(rClient)
	l1Cache, err := cache.NewL1Cache(rClient, 10)
	assert.NoError(t, err)
	defer l1Cache.Close()

	mockRepo := new(MockTweetRepository)
	mockLike := new(MockLikeRepository)
	mockComment := new(MockCommentRepository)
	mockPoll := new(MockPollRepository)
	mockBookmark := new(MockBookmarkRepository)
	mockRetweet := new(MockRetweetRepository)

	svc := NewTweetService(
		mockRepo,
		nil,
		mockLike,
		mockComment,
		mockPoll,
		mockBookmark,
		mockRetweet,
		timelineCache,
		nil,
		l1Cache,
		nil,
		nil,
	)

	tweetID := uint64(9999)
	userID := uint64(101)
	baseTweet := &domain.Tweet{
		ID:          tweetID,
		UserID:      userID,
		Content:     "Hot tweet",
		VisibleType: domain.VisiblePublic,
	}

	// 设定 DB 在被调用时带延迟，以容易触发并发竞争
	mockRepo.On("GetByID", mock.Anything, tweetID).Run(func(args mock.Arguments) {
		time.Sleep(100 * time.Millisecond)
	}).Return(baseTweet, nil).Once() // 注意：这里指定 .Once()！如果多次调用会报错。

	mockLike.On("GetLikeCount", mock.Anything, tweetID).Return(int64(0), nil)
	mockLike.On("IsLiked", mock.Anything, userID, tweetID).Return(false, nil)
	mockComment.On("GetCommentCount", mock.Anything, tweetID).Return(int64(0), nil)
	mockLike.On("BatchGetLikeCounts", mock.Anything, []uint64{tweetID}).Return(map[uint64]int64{tweetID: 0}, nil)
	mockRetweet.On("BatchGetRetweetCounts", mock.Anything, []uint64{tweetID}).Return(map[uint64]int64{tweetID: 0}, nil)
	mockLike.On("BatchIsLiked", mock.Anything, userID, []uint64{tweetID}).Return(map[uint64]bool{tweetID: false}, nil)
	mockRetweet.On("BatchIsRetweeted", mock.Anything, userID, []uint64{tweetID}).Return(map[uint64]bool{tweetID: false}, nil)
	mockBookmark.On("BatchIsBookmarked", mock.Anything, userID, []uint64{tweetID}).Return(map[uint64]bool{tweetID: false}, nil)
	mockComment.On("BatchGetCommentCounts", mock.Anything, []uint64{tweetID}).Return(map[uint64]int64{tweetID: 0}, nil)
	mockPoll.On("GetByTweetIDs", mock.Anything, []uint64{tweetID}).Return(nil, nil)

	// 并发请求 20 次
	concurrentCount := 20
	var wg sync.WaitGroup
	wg.Add(concurrentCount)

	results := make([]*domain.Tweet, concurrentCount)
	errors := make([]error, concurrentCount)

	for i := 0; i < concurrentCount; i++ {
		go func(idx int) {
			defer wg.Done()
			tweet, err := svc.GetTweet(context.Background(), tweetID, userID)
			results[idx] = tweet
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// 断言：所有请求均应成功返回，且 DB 只被调用了一次
	for i := 0; i < concurrentCount; i++ {
		assert.NoError(t, errors[i])
		assert.NotNil(t, results[i])
		assert.Equal(t, "Hot tweet", results[i].Content)
	}

	mockRepo.AssertExpectations(t)
}

func TestGetFeeds_HybridCacheFlow(t *testing.T) {
	mr, err := miniredis.Run()
	assert.NoError(t, err)
	defer mr.Close()

	rClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer rClient.Close()

	timelineCache := cache.NewTimelineCache(rClient)
	l1Cache, err := cache.NewL1Cache(rClient, 10)
	assert.NoError(t, err)
	defer l1Cache.Close()

	mockRepo := new(MockTweetRepository)
	mockLike := new(MockLikeRepository)
	mockComment := new(MockCommentRepository)
	mockPoll := new(MockPollRepository)
	mockBookmark := new(MockBookmarkRepository)
	mockRetweet := new(MockRetweetRepository)

	svc := NewTweetService(
		mockRepo,
		nil,
		mockLike,
		mockComment,
		mockPoll,
		mockBookmark,
		mockRetweet,
		timelineCache,
		nil,
		l1Cache,
		nil,
		nil,
	)

	userID := uint64(100)
	celebID := uint64(200)
	tweetID := uint64(12345)

	// 1. 设置关注关系：userID 关注了大V celebID
	ctx := context.Background()
	err = timelineCache.AddCelebrity(ctx, celebID)
	assert.NoError(t, err)
	err = timelineCache.AddCelebrityFollowee(ctx, userID, celebID)
	assert.NoError(t, err)

	// Mock stats/relations responses
	mockLike.On("BatchGetLikeCounts", mock.Anything, mock.Anything).Return(map[uint64]int64{tweetID: 0}, nil)
	mockRetweet.On("BatchGetRetweetCounts", mock.Anything, mock.Anything).Return(map[uint64]int64{tweetID: 0}, nil)
	mockLike.On("BatchIsLiked", mock.Anything, userID, mock.Anything).Return(map[uint64]bool{tweetID: false}, nil)
	mockRetweet.On("BatchIsRetweeted", mock.Anything, userID, mock.Anything).Return(map[uint64]bool{tweetID: false}, nil)
	mockBookmark.On("BatchIsBookmarked", mock.Anything, userID, mock.Anything).Return(map[uint64]bool{tweetID: false}, nil)
	mockComment.On("BatchGetCommentCounts", mock.Anything, mock.Anything).Return(map[uint64]int64{tweetID: 0}, nil)
	mockPoll.On("GetByTweetIDs", mock.Anything, mock.Anything).Return(nil, nil)

	// ==========================================
	// Test Case 1: L2 Celebrity Timeline Cache Hit (No DB)
	// ==========================================
	// 写入大V时间线缓存
	err = timelineCache.AddToUserTimeline(ctx, celebID, tweetID)
	assert.NoError(t, err)

	// 写入推文详情到 L2
	baseTweet := &domain.Tweet{
		ID:          tweetID,
		UserID:      celebID,
		Content:     "Hello from VIP",
		VisibleType: domain.VisiblePublic,
	}
	err = timelineCache.SetBaseTweet(ctx, baseTweet)
	assert.NoError(t, err)

	// 运行 GetFeeds，预期完全从缓存读取，GORM 不会收到任何 SQL 查询
	feeds, nextCursor, hasMore, err := svc.GetFeeds(ctx, userID, 0, 10)
	assert.NoError(t, err)
	assert.Len(t, feeds, 1)
	assert.Equal(t, "Hello from VIP", feeds[0].Content)
	assert.Equal(t, tweetID, nextCursor)
	assert.False(t, hasMore)

	// ==========================================
	// Test Case 2: L2 Cache Miss -> Trigger Rebuild from DB (Singleflight)
	// ==========================================
	// 彻底删除该大V的初始化标志，模拟缓存缺失状态
	initKey := fmt.Sprintf("user_timeline:%d:initialized", celebID)
	err = rClient.Del(ctx, initKey).Err()
	assert.NoError(t, err)

	// Mock DB：预期在重建时会查询数据库中该大V的帖子列表
	mockRepo.On("ListByUserID", mock.Anything, celebID, uint64(0), 1000).Return([]*domain.Tweet{baseTweet}, nil).Once()

	feeds2, nextCursor2, hasMore2, err := svc.GetFeeds(ctx, userID, 0, 10)
	assert.NoError(t, err)
	assert.Len(t, feeds2, 1)
	assert.Equal(t, "Hello from VIP", feeds2[0].Content)
	assert.Equal(t, tweetID, nextCursor2)
	assert.False(t, hasMore2)

	// 重建后，初始化标志应该已被重新写入 Redis
	exists, err := rClient.Exists(ctx, initKey).Result()
	assert.NoError(t, err)
	assert.Equal(t, int64(1), exists)

	mockRepo.AssertExpectations(t)
}

// -------------------------------------------------------------
// 以下为 Mock 的 Like, Comment, Poll, Bookmark, Retweet 仓储定义，以支持多维度 Stats 的并发填充
// -------------------------------------------------------------

type MockLikeRepository struct {
	mock.Mock
}

func (m *MockLikeRepository) Like(ctx context.Context, userID, tweetID uint64) error {
	return m.Called(ctx, userID, tweetID).Error(0)
}
func (m *MockLikeRepository) Unlike(ctx context.Context, userID, tweetID uint64) error {
	return m.Called(ctx, userID, tweetID).Error(0)
}
func (m *MockLikeRepository) GetLikeCount(ctx context.Context, tweetID uint64) (int64, error) {
	args := m.Called(ctx, tweetID)
	return args.Get(0).(int64), args.Error(1)
}
func (m *MockLikeRepository) IsLiked(ctx context.Context, userID, tweetID uint64) (bool, error) {
	args := m.Called(ctx, userID, tweetID)
	return args.Bool(0), args.Error(1)
}
func (m *MockLikeRepository) BatchGetLikeCounts(ctx context.Context, tweetIDs []uint64) (map[uint64]int64, error) {
	args := m.Called(ctx, tweetIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[uint64]int64), args.Error(1)
}
func (m *MockLikeRepository) BatchIsLiked(ctx context.Context, userID uint64, tweetIDs []uint64) (map[uint64]bool, error) {
	args := m.Called(ctx, userID, tweetIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[uint64]bool), args.Error(1)
}
func (m *MockLikeRepository) ListByUserID(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Like, error) {
	args := m.Called(ctx, userID, cursor, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Like), args.Error(1)
}

type MockCommentRepository struct {
	mock.Mock
}

func (m *MockCommentRepository) Create(ctx context.Context, comment *domain.Comment) error {
	return m.Called(ctx, comment).Error(0)
}
func (m *MockCommentRepository) Delete(ctx context.Context, commentID uint64) error {
	return m.Called(ctx, commentID).Error(0)
}
func (m *MockCommentRepository) GetByID(ctx context.Context, commentID uint64) (*domain.Comment, error) {
	args := m.Called(ctx, commentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Comment), args.Error(1)
}
func (m *MockCommentRepository) ListByTweetID(ctx context.Context, tweetID uint64, cursor uint64, limit int) ([]*domain.Comment, error) {
	args := m.Called(ctx, tweetID, cursor, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Comment), args.Error(1)
}
func (m *MockCommentRepository) GetCommentCount(ctx context.Context, tweetID uint64) (int64, error) {
	args := m.Called(ctx, tweetID)
	return args.Get(0).(int64), args.Error(1)
}
func (m *MockCommentRepository) BatchGetCommentCounts(ctx context.Context, tweetIDs []uint64) (map[uint64]int64, error) {
	args := m.Called(ctx, tweetIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[uint64]int64), args.Error(1)
}

type MockPollRepository struct {
	mock.Mock
}

func (m *MockPollRepository) Create(ctx context.Context, poll *domain.Poll) error {
	args := m.Called(ctx, poll)
	return args.Error(0)
}
func (m *MockPollRepository) GetByTweetID(ctx context.Context, tweetID uint64) (*domain.Poll, error) {
	args := m.Called(ctx, tweetID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Poll), args.Error(1)
}
func (m *MockPollRepository) GetByID(ctx context.Context, pollID uint64) (*domain.Poll, error) {
	args := m.Called(ctx, pollID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Poll), args.Error(1)
}
func (m *MockPollRepository) GetByTweetIDs(ctx context.Context, tweetIDs []uint64) (map[uint64]*domain.Poll, error) {
	args := m.Called(ctx, tweetIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[uint64]*domain.Poll), args.Error(1)
}
func (m *MockPollRepository) Vote(ctx context.Context, vote *domain.PollVote) error {
	return m.Called(ctx, vote).Error(0)
}
func (m *MockPollRepository) GetVote(ctx context.Context, pollID, userID uint64) (*domain.PollVote, error) {
	args := m.Called(ctx, pollID, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.PollVote), args.Error(1)
}
func (m *MockPollRepository) GetVotesByTweetIDs(ctx context.Context, tweetIDs []uint64, userID uint64) (map[uint64]uint64, error) {
	args := m.Called(ctx, tweetIDs, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[uint64]uint64), args.Error(1)
}

type MockBookmarkRepository struct {
	mock.Mock
}

func (m *MockBookmarkRepository) Create(ctx context.Context, b *domain.Bookmark) error {
	return m.Called(ctx, b).Error(0)
}
func (m *MockBookmarkRepository) Delete(ctx context.Context, userID, tweetID uint64) error {
	return m.Called(ctx, userID, tweetID).Error(0)
}
func (m *MockBookmarkRepository) List(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Bookmark, error) {
	args := m.Called(ctx, userID, cursor, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Bookmark), args.Error(1)
}
func (m *MockBookmarkRepository) IsBookmarked(ctx context.Context, userID, tweetID uint64) (bool, error) {
	args := m.Called(ctx, userID, tweetID)
	return args.Bool(0), args.Error(1)
}
func (m *MockBookmarkRepository) BatchIsBookmarked(ctx context.Context, userID uint64, tweetIDs []uint64) (map[uint64]bool, error) {
	args := m.Called(ctx, userID, tweetIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[uint64]bool), args.Error(1)
}

type MockRetweetRepository struct {
	mock.Mock
}

func (m *MockRetweetRepository) Create(ctx context.Context, userID, tweetID uint64) error {
	return m.Called(ctx, userID, tweetID).Error(0)
}
func (m *MockRetweetRepository) Delete(ctx context.Context, userID, tweetID uint64) error {
	return m.Called(ctx, userID, tweetID).Error(0)
}
func (m *MockRetweetRepository) IsRetweeted(ctx context.Context, userID, tweetID uint64) (bool, error) {
	args := m.Called(ctx, userID, tweetID)
	return args.Bool(0), args.Error(1)
}
func (m *MockRetweetRepository) ListByUserID(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Retweet, error) {
	args := m.Called(ctx, userID, cursor, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.Retweet), args.Error(1)
}
func (m *MockRetweetRepository) BatchGetRetweetCounts(ctx context.Context, tweetIDs []uint64) (map[uint64]int64, error) {
	args := m.Called(ctx, tweetIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[uint64]int64), args.Error(1)
}
func (m *MockRetweetRepository) BatchIsRetweeted(ctx context.Context, userID uint64, tweetIDs []uint64) (map[uint64]bool, error) {
	args := m.Called(ctx, userID, tweetIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[uint64]bool), args.Error(1)
}
func (m *MockRetweetRepository) GetRetweetCount(ctx context.Context, tweetID uint64) (int64, error) {
	args := m.Called(ctx, tweetID)
	return args.Get(0).(int64), args.Error(1)
}
