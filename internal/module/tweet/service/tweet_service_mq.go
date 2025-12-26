package service

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/module/tweet/cache"
	"twitter-clone/internal/mq/producer"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	// MaxContentLength 推文最大长度
	MaxContentLength = 280

	// MaxMediaCount 最大媒体数量
	MaxMediaCount = 4
)

// TweetService 推文服务（带消息队列）
type TweetService struct {
	repo          domain.TweetRepository
	followRepo    domain.FollowRepository
	timelineCache *cache.TimelineCache
	eventProducer *producer.EventProducer // 🆕 消息生产者
}

// NewTweetService 创建推文服务
func NewTweetService(
	repo domain.TweetRepository,
	followRepo domain.FollowRepository,
	timelineCache *cache.TimelineCache,
	eventProducer *producer.EventProducer, // 🆕 注入消息生产者
) *TweetService {
	return &TweetService{
		repo:          repo,
		followRepo:    followRepo,
		timelineCache: timelineCache,
		eventProducer: eventProducer,
	}
}

// CreateTweet 发布推文（使用消息队列）
func (s *TweetService) CreateTweet(ctx context.Context, userID uint64, content string, mediaURLs []string) (*domain.Tweet, error) {
	// 🔍 启动 Span
	tr := otel.Tracer("tweet-service")
	ctx, span := tr.Start(ctx, "TweetService.CreateTweet")
	defer span.End()

	span.SetAttributes(attribute.Int64("user.id", int64(userID)))

	// 1. 参数验证
	if err := s.validateContent(content); err != nil {
		span.RecordError(err)
		return nil, err
	}

	if err := s.validateMediaURLs(mediaURLs); err != nil {
		span.RecordError(err)
		return nil, err
	}

	// 2. 确定推文类型
	tweetType := s.determineTweetType(mediaURLs)

	// 3. 构建推文对象
	tweet := &domain.Tweet{
		UserID:      userID,
		Content:     strings.TrimSpace(content),
		MediaURLs:   mediaURLs,
		Type:        tweetType,
		VisibleType: domain.VisiblePublic,
	}

	// 4. 保存到数据库
	// 🔍 DB Span
	dbCtx, dbSpan := tr.Start(ctx, "TweetRepo.Create")
	if err := s.repo.Create(dbCtx, tweet); err != nil {
		dbSpan.RecordError(err)
		dbSpan.End()
		return nil, fmt.Errorf("failed to create tweet: %w", err)
	}
	dbSpan.End()

	log.Printf("✅ Tweet created: ID=%d, UserID=%d", tweet.ID, tweet.UserID)

	// 5. 🆕 发送消息到 MQ（异步扇出）
	event := &events.TweetCreatedEvent{
		TweetID:  tweet.ID,
		AuthorID: tweet.UserID,
		Content:  tweet.Content,
		Type:     tweet.Type,
	}

	// 🔥 关键改进：不再使用 Goroutine，而是发送到消息队列
	// 🔍 MQ Span
	mqCtx, mqSpan := tr.Start(ctx, "MQProducer.Publish")
	if err := s.eventProducer.PublishTweetCreated(mqCtx, event); err != nil {
		// ⚠️ 即使发送失败，推文也已保存，记录错误即可
		mqSpan.RecordError(err)
		log.Printf("⚠️  Failed to publish tweet created event: %v", err)
	}
	mqSpan.End()

	return tweet, nil
}

// DeleteTweet 删除推文（使用消息队列）
func (s *TweetService) DeleteTweet(ctx context.Context, tweetID uint64, userID uint64) error {
	// 1. 查询推文
	tweet, err := s.repo.GetByID(ctx, tweetID)
	if err != nil {
		return ErrTweetNotFound
	}

	// 2. 权限检查
	if tweet.UserID != userID {
		return ErrUnauthorized
	}

	// 3. 执行删除
	if err := s.repo.Delete(ctx, tweetID); err != nil {
		return fmt.Errorf("failed to delete tweet: %w", err)
	}

	// 4. 🆕 发送删除事件到 MQ
	event := &events.TweetDeletedEvent{
		TweetID:  tweetID,
		AuthorID: userID,
	}

	if err := s.eventProducer.PublishTweetDeleted(ctx, event); err != nil {
		log.Printf("⚠️  Failed to publish tweet deleted event: %v", err)
	}

	return nil
}

// GetTweet 获取推文详情
func (s *TweetService) GetTweet(ctx context.Context, tweetID uint64) (*domain.Tweet, error) {
	tweet, err := s.repo.GetByID(ctx, tweetID)
	if err != nil {
		return nil, ErrTweetNotFound
	}

	// TODO: 从 Redis 或 tweet_stats 表获取统计数据

	return tweet, nil
}

// GetUserTimeline 获取用户时间线（拉模式）
func (s *TweetService) GetUserTimeline(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
	// 1. 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 2. 从数据库拉取
	tweets, err := s.repo.ListByUserID(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get user timeline: %w", err)
	}

	// 3. 判断是否还有更多
	hasMore := len(tweets) > limit
	if hasMore {
		tweets = tweets[:limit]
	}

	// 4. 计算下一页游标
	var nextCursor uint64
	if hasMore && len(tweets) > 0 {
		nextCursor = tweets[len(tweets)-1].ID
	}

	return tweets, nextCursor, hasMore, nil
}

// GetFeeds 获取关注流（推拉结合 + 消息队列）
func (s *TweetService) GetFeeds(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
	// 1. 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// 2. 从 Redis 获取 Timeline（推文 ID 列表）
	tweetIDs, err := s.timelineCache.GetTimeline(ctx, userID, cursor, limit+1)
	if err != nil {
		log.Printf("⚠️  Failed to get timeline from redis: %v", err)
		// 降级：使用拉模式
		return s.getFeedsByPull(ctx, userID, cursor, limit)
	}

	// 3. Redis 缓存为空，使用拉模式
	if len(tweetIDs) == 0 {
		log.Printf("ℹ️  Timeline cache empty for user %d, using pull mode", userID)
		return s.getFeedsByPull(ctx, userID, cursor, limit)
	}

	// 4. 判断是否还有更多
	hasMore := len(tweetIDs) > limit
	if hasMore {
		tweetIDs = tweetIDs[:limit]
	}

	// 5. 批量查询推文详情
	tweets, err := s.repo.GetByIDs(ctx, tweetIDs)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get tweets by ids: %w", err)
	}

	// 6. 按照 tweetIDs 的顺序排序
	tweetMap := make(map[uint64]*domain.Tweet, len(tweets))
	for _, tweet := range tweets {
		tweetMap[tweet.ID] = tweet
	}

	sortedTweets := make([]*domain.Tweet, 0, len(tweetIDs))
	for _, tweetID := range tweetIDs {
		if tweet, ok := tweetMap[tweetID]; ok {
			sortedTweets = append(sortedTweets, tweet)
		}
	}

	// 7. 计算下一页游标
	var nextCursor uint64
	if hasMore && len(sortedTweets) > 0 {
		nextCursor = sortedTweets[len(sortedTweets)-1].ID
	}

	log.Printf("✅ Feeds loaded from redis: user=%d, tweets=%d", userID, len(sortedTweets))

	return sortedTweets, nextCursor, hasMore, nil
}

// getFeedsByPull 拉模式获取 Feeds（降级方案）
func (s *TweetService) getFeedsByPull(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
	// 1. 获取关注列表
	followeeIDs, err := s.followRepo.GetFollowees(ctx, userID, 0, 1000)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get followees: %w", err)
	}

	if len(followeeIDs) == 0 {
		return []*domain.Tweet{}, 0, false, nil
	}

	// 2. 查询这些人的推文
	tweets, err := s.repo.ListFeeds(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get feeds: %w", err)
	}

	// 3. 判断是否还有更多
	hasMore := len(tweets) > limit
	if hasMore {
		tweets = tweets[:limit]
	}

	// 4. 计算下一页游标
	var nextCursor uint64
	if hasMore && len(tweets) > 0 {
		nextCursor = tweets[len(tweets)-1].ID
	}

	log.Printf("⚠️  Feeds loaded by pull mode: user=%d, tweets=%d", userID, len(tweets))

	return tweets, nextCursor, hasMore, nil
}

// ========== 私有辅助方法 ==========

func (s *TweetService) validateContent(content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return ErrInvalidContent
	}

	// 使用 rune 计数（支持中文等 Unicode 字符）
	if len([]rune(content)) > MaxContentLength {
		return ErrContentTooLong
	}

	return nil
}

func (s *TweetService) validateMediaURLs(mediaURLs []string) error {
	if len(mediaURLs) > MaxMediaCount {
		return ErrTooManyMedia
	}

	for _, mediaURL := range mediaURLs {
		if mediaURL == "" {
			continue
		}

		parsedURL, err := url.Parse(mediaURL)
		if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
			return ErrInvalidMediaURL
		}

		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return ErrInvalidMediaURL
		}
	}

	return nil
}

func (s *TweetService) determineTweetType(mediaURLs []string) int {
	if len(mediaURLs) == 0 {
		return domain.TweetTypeText
	}

	for _, mediaURL := range mediaURLs {
		lower := strings.ToLower(mediaURL)
		if strings.HasSuffix(lower, ".mp4") ||
			strings.HasSuffix(lower, ".mov") ||
			strings.HasSuffix(lower, ".avi") {
			return domain.TweetTypeVideo
		}
	}

	return domain.TweetTypeImage
}
