package service

//
//import (
//	"context"
//	"fmt"
//	"log"
//	"net/url"
//	"strings"
//	"twitter-clone/internal/domain"
//	"twitter-clone/internal/module/tweet/cache"
//)
//
//const (
//	// MaxContentLength 推文最大长度（Twitter 标准）
//	MaxContentLength = 280
//
//	// MaxMediaCount 最大媒体数量
//	MaxMediaCount = 4
//
//	// MaxFanoutSize 最大扇出数量（推送给多少粉丝）
//	MaxFanoutSize = 1000
//)
//
//// TweetService 推文服务
//type TweetService struct {
//	repo          domain.TweetRepository
//	followRepo    domain.FollowRepository
//	timelineCache *cache.TimelineCache
//}
//
//// NewTweetService 创建推文方式
//func NewTweetService(repo domain.TweetRepository, followRepo domain.FollowRepository, timelineCache *cache.TimelineCache) *TweetService {
//	return &TweetService{
//		repo:          repo,
//		followRepo:    followRepo,
//		timelineCache: timelineCache,
//	}
//}
//
//// CreateTweet 发布推文
//func (s *TweetService) CreateTweet(ctx context.Context, userID uint64, content string, mediaURLs []string) (*domain.Tweet, error) {
//	//1. 参数验证
//	if err := s.validateContent(content); err != nil {
//		return nil, err
//	}
//
//	if err := s.validateMediaURLs(mediaURLs); err != nil {
//		return nil, err
//	}
//
//	//2.确定推文类型
//	tweetType := s.determineTweetType(mediaURLs)
//
//	//3.构建推文对象
//	tweet := &domain.Tweet{
//		UserID:      userID,
//		Content:     strings.TrimSpace(content),
//		MediaURLs:   mediaURLs,
//		Type:        tweetType,
//		VisibleType: domain.VisiblePublic, //默认公开
//	}
//
//	//4.保存到数据库
//	if err := s.repo.Create(ctx, tweet); err != nil {
//		return nil, fmt.Errorf("failed to create tweet: %w", err)
//	}
//
//	log.Printf("✅ Tweet created: ID=%d, UserID=%d", tweet.ID, tweet.UserID)
//
//	// 5. 异步推送到粉丝的 Timeline（推模式）
//	go s.fanoutToFollowers(context.Background(), userID, tweet.ID)
//
//	return tweet, nil
//
//}
//
//// fanoutToFollowers 扇出推文到粉丝的 Timeline
//func (s *TweetService) fanoutToFollowers(ctx context.Context, authorID uint64, tweetID uint64) {
//	// 获取活跃粉丝列表（限制数量，避免大 V 写放大）
//	followerIDs, err := s.followRepo.GetActiveFollowers(ctx, authorID, MaxFanoutSize)
//	if err != nil {
//		log.Printf("❌ Failed to get followers: %v", err)
//		return
//	}
//
//	if len(followerIDs) == 0 {
//		log.Printf("ℹ️  No followers to fanout for user %d", authorID)
//		return
//	}
//	log.Printf("📤 Fanout tweet %d to %d followers", tweetID, len(followerIDs))
//
//	// 批量推送到粉丝的 Timeline
//	if err := s.timelineCache.BatchAddToTimeline(ctx, followerIDs, tweetID); err != nil {
//		log.Printf("❌ Failed to fanout to timeline: %v", err)
//		return
//	}
//
//	log.Printf("✅ Fanout complete: tweet %d → %d followers", tweetID, len(followerIDs))
//
//}
//
//// DeleteTweet 删除推文
//func (s *TweetService) DeleteTweet(ctx context.Context, tweetID uint64, userID uint64) error {
//	//1.查询推文(验证是否存在)
//	tweet, err := s.repo.GetByID(ctx, tweetID)
//	if err != nil {
//		return ErrTweetNotFound
//	}
//	fmt.Println("测试3")
//
//	fmt.Println("tweet中的ID:", tweet.UserID, "jwt中解析的ID:", userID)
//
//	//2.权限检查(只能删除自己的推文)
//	if tweet.UserID != userID {
//		return ErrUnauthorized
//	}
//
//	//3.执行删除
//	if err := s.repo.Delete(ctx, tweetID); err != nil {
//		return fmt.Errorf("failed to delete tweet: %w", err)
//	}
//
//	// 4. 异步从粉丝的 Timeline 中删除
//	go s.removeTweetFromFollowersTimeline(context.Background(), userID, tweetID)
//
//	return nil
//}
//
//// removeTweetFromFollowersTimeline 从粉丝的 Timeline 中删除推文
//func (s *TweetService) removeTweetFromFollowersTimeline(ctx context.Context, authorID uint64, tweetID uint64) {
//	followerIDs, err := s.followRepo.GetActiveFollowers(ctx, authorID, MaxFanoutSize)
//	if err != nil {
//		log.Printf("❌ Failed to get followers for removal: %v", err)
//		return
//	}
//
//	if len(followerIDs) > 0 {
//		if err := s.timelineCache.BatchRemoveFromTimeline(ctx, followerIDs, tweetID); err != nil {
//			log.Printf("❌ Failed to remove from timeline: %v", err)
//		}
//	}
//}
//
//// GetTweet 获取推文详情
//func (s *TweetService) GetTweet(ctx context.Context, tweetID uint64) (*domain.Tweet, error) {
//	tweet, err := s.repo.GetByID(ctx, tweetID)
//	if err != nil {
//		return nil, ErrTweetNotFound
//	}
//
//	// TODO: 这里可以添加：
//	// 1. 从 Redis 或 tweet_stats 表获取点赞/评论/分享数
//	// 2. 检查当前用户是否已点赞
//	// 3. 获取作者信息（用户名、头像等）
//	return tweet, nil
//}
//
//// GetUserTimeline 获取用户时间线
//func (s *TweetService) GetUserTimeline(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
//	// 1. 参数验证
//	if limit <= 0 {
//		limit = 20
//	}
//	if limit > 100 {
//		limit = 100
//	}
//
//	// 2. 多查询一条，判断是否还有更多
//	tweets, err := s.repo.ListByUserID(ctx, userID, cursor, limit+1)
//	if err != nil {
//		return nil, 0, false, fmt.Errorf("failed to get user timeline: %w", err)
//	}
//
//	//3.判断是否还有更多
//	hasMore := len(tweets) > limit
//	if hasMore {
//		tweets = tweets[:limit]
//	}
//
//	//4.计算下一页游标
//	var nextCursor uint64
//	if hasMore && len(tweets) > 0 {
//		nextCursor = tweets[len(tweets)-1].ID
//	}
//
//	// TODO: 批量获取统计数据（点赞数、评论数等）
//
//	return tweets, nextCursor, hasMore, nil
//}
//
//// GetFeeds 获取关注流(Timeline)
//func (s *TweetService) GetFeeds(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
//	// 1. 参数验证
//	if limit <= 0 {
//		limit = 20
//	}
//	if limit > 100 {
//		limit = 100
//	}
//
//	// 2. 从 Redis 获取 Timeline（推文 ID 列表）
//	tweetIDs, err := s.timelineCache.GetTimeline(ctx, userID, cursor, limit+1)
//	if err != nil {
//		log.Printf("⚠️  Failed to get timeline from redis: %v", err)
//		// 降级：使用拉模式
//		return s.getFeedsByPull(ctx, userID, cursor, limit)
//	}
//
//	// 3. Redis 缓存为空，使用拉模式
//	if len(tweetIDs) == 0 {
//		log.Printf("ℹ️  Timeline cache empty for user %d, using pull mode", userID)
//		return s.getFeedsByPull(ctx, userID, cursor, limit)
//	}
//
//	// 4. 判断是否还有更多
//	hasMore := len(tweetIDs) > limit
//	if hasMore {
//		tweetIDs = tweetIDs[:limit]
//	}
//
//	// 5. 批量查询推文详情
//	tweets, err := s.repo.GetByIDs(ctx, tweetIDs)
//	if err != nil {
//		return nil, 0, false, fmt.Errorf("failed to get tweets by ids: %w", err)
//	}
//
//	// 6. 按照 tweetIDs 的顺序排序（保持时间顺序）
//	tweetMap := make(map[uint64]*domain.Tweet, len(tweets))
//	for _, tweet := range tweets {
//		tweetMap[tweet.ID] = tweet
//	}
//
//	sortedTweets := make([]*domain.Tweet, 0, len(tweetIDs))
//	for _, tweetID := range tweetIDs {
//		if tweet, ok := tweetMap[tweetID]; ok {
//			sortedTweets = append(sortedTweets, tweet)
//		}
//	}
//
//	// 7. 计算下一页游标
//	var nextCursor uint64
//	if hasMore && len(sortedTweets) > 0 {
//		nextCursor = sortedTweets[len(sortedTweets)-1].ID
//	}
//
//	log.Printf("✅ Feeds loaded from redis: user=%d, tweets=%d", userID, len(sortedTweets))
//
//	return sortedTweets, nextCursor, hasMore, nil
//}
//
//// getFeedsByPull 拉模式获取 Feeds（降级方案）
//func (s *TweetService) getFeedsByPull(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, uint64, bool, error) {
//	//1.获取关注列表
//	followeeIDs, err := s.followRepo.GetFollowees(ctx, userID, 9, 1000)
//	if err != nil {
//		return nil, 0, false, fmt.Errorf("failed to get followees: %w", err)
//	}
//
//	if len(followeeIDs) == 0 {
//		// 没有关注任何人，返回空
//		return []*domain.Tweet{}, 0, false, nil
//	}
//
//	// 2. 查询这些人的推文
//	tweets, err := s.repo.ListFeeds(ctx, userID, cursor, limit+1)
//	if err != nil {
//		return nil, 0, false, fmt.Errorf("failed to get feeds: %w", err)
//	}
//
//	// 3. 判断是否还有更多
//	hasMore := len(tweets) > limit
//	if hasMore {
//		tweets = tweets[:limit]
//	}
//
//	// 4. 计算下一页游标
//	var nextCursor uint64
//	if hasMore && len(tweets) > 0 {
//		nextCursor = tweets[len(tweets)-1].ID
//	}
//
//	log.Printf("⚠️  Feeds loaded by pull mode: user=%d, tweets=%d", userID, len(tweets))
//
//	return tweets, nextCursor, hasMore, nil
//}
//
//// ========== 私有辅助方法 ==========
//
//// validateContent 验证推文内容
//func (s *TweetService) validateContent(content string) error {
//	//去除首尾空白
//	content = strings.TrimSpace(content)
//
//	//内容不能为空
//	if content == "" {
//		return ErrInvalidContent
//	}
//
//	if len([]rune(content)) > MaxContentLength {
//		return ErrContentTooLong
//	}
//
//	return nil
//}
//
//// validateMediaURLs 验证媒体 URL
//func (s *TweetService) validateMediaURLs(mediaURLs []string) error {
//	//媒体数量限制
//	if len(mediaURLs) > MaxMediaCount {
//		return ErrTooManyMedia
//	}
//
//	//验证每个 URL 的格式
//	for _, mediaURL := range mediaURLs {
//		if mediaURL == "" {
//			continue
//		}
//
//		//验证是否是有效的 URL
//		parseURL, err := url.Parse(mediaURL)
//		if err != nil || parseURL.Scheme == "" || parseURL.Host == "" {
//			return ErrInvalidMediaURL
//		}
//
//		//只允许http和https
//		if parseURL.Scheme != "http" && parseURL.Scheme != "https" {
//			return ErrInvalidMediaURL
//		}
//	}
//
//	return nil
//}
//
//// determineTweetType 根据媒体 URL 确定推文类型
//func (s *TweetService) determineTweetType(mediaURLs []string) int {
//	if len(mediaURLs) == 0 {
//		return domain.TweetTypeText
//	}
//
//	// 简单判断：根据 URL 后缀判断类型
//	// 实际项目中可能需要更复杂的逻辑（比如上传时就确定类型）
//	for _, mediaURL := range mediaURLs {
//		lower := strings.ToLower(mediaURL)
//		if strings.HasSuffix(lower, ".mp4") || strings.HasSuffix(lower, ".mov") || strings.HasSuffix(lower, ".avi") {
//			return domain.TweetTypeVideo
//		}
//	}
//
//	return domain.TweetTypeImage
//}
