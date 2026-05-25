package service

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/module/tweet/cache"
	"twitter-clone/internal/mq/producer"
	"twitter-clone/pkg/logger"
)

var (
	// ErrCannotFollowSelf 不能关注自己
	ErrCannotFollowSelf = errors.New("cannot follow yourself")

	// ErrAlreadyFollowing 已经关注
	ErrAlreadyFollowing = errors.New("already following")

	// ErrNotFollowing 没有关注
	ErrNotFollowing = errors.New("not following")
)

const (
	// CelebrityPromoThreshold 晋升大V粉丝数阈值
	CelebrityPromoThreshold = 5000

	// CelebrityDemoteThreshold 降级为普通博主粉丝数阈值
	CelebrityDemoteThreshold = 4500
)

// FollowService 关注服务（带消息队列）
type FollowService struct {
	repo          domain.FollowRepository
	tweetRepo     domain.TweetRepository
	timelineCache *cache.TimelineCache
	eventProducer *producer.EventProducer // 🆕 消息生产者
}

// NewFollowService 创建关注服务
func NewFollowService(
	repo domain.FollowRepository,
	tweetRepo domain.TweetRepository,
	timelineCache *cache.TimelineCache,
	eventProducer *producer.EventProducer, // 🆕 注入消息生产者
) *FollowService {
	return &FollowService{
		repo:          repo,
		tweetRepo:     tweetRepo,
		timelineCache: timelineCache,
		eventProducer: eventProducer,
	}
}

// Follow 关注用户
func (s *FollowService) Follow(ctx context.Context, followerID, followeeID uint64) error {
	// 1. 不能关注自己
	if followerID == followeeID {
		return ErrCannotFollowSelf
	}

	// 2. 创建关注关系
	if err := s.repo.Follow(ctx, followerID, followeeID); err != nil {
		if err.Error() == "already following" {
			return ErrAlreadyFollowing
		}
		return fmt.Errorf("failed to follow: %w", err)
	}

	// 3. 🆕 发送关注事件到 MQ（由 Consumer 处理拉取推文到 Timeline）
	event := &events.UserFollowedEvent{
		FollowerID: followerID,
		FolloweeID: followeeID,
	}

	if err := s.eventProducer.PublishUserFollowed(ctx, event); err != nil {
		logger.Warn(ctx, "⚠️ Failed to publish user followed event", zap.Error(err))
	}

	// 4. 维护大V状态与双阈值防抖晋升
	go s.handleFollowCelebrityStatus(context.Background(), followerID, followeeID)

	// 5. 🔥 立即拉取被关注者最近的推文（不阻塞主流程），传递带有 Span 的 async 上下文
	span := trace.SpanFromContext(ctx)
	asyncCtx := trace.ContextWithSpan(context.Background(), span)
	go s.pullRecentTweetsToTimeline(asyncCtx, followerID, followeeID)

	return nil
}

// pullRecentTweetsToTimeline 拉取最新推文到 Timeline
func (s *FollowService) pullRecentTweetsToTimeline(ctx context.Context, followerID, followeeID uint64) {
	// 获取被关注者最近的 50 条推文
	tweets, err := s.tweetRepo.ListByUserID(ctx, followeeID, 0, 50)
	if err != nil {
		logger.Warn(ctx, "⚠️ Failed to get recent tweets", zap.Error(err), zap.Uint64("followee_id", followeeID))
		return
	}

	if len(tweets) == 0 {
		return
	}

	logger.Info(ctx, "📥 Pulling recent tweets to timeline", zap.Int("count", len(tweets)), zap.Uint64("follower_id", followerID), zap.Uint64("followee_id", followeeID))

	// 批量添加到关注者的 Timeline
	for _, tweet := range tweets {
		if err := s.timelineCache.AddToTimeline(ctx, followerID, tweet.ID); err != nil {
			logger.Warn(ctx, "⚠️ Failed to add tweet to timeline", zap.Uint64("tweet_id", tweet.ID), zap.Error(err))
		}
	}

	logger.Info(ctx, "✅ Pulled tweets to timeline", zap.Int("count", len(tweets)), zap.Uint64("follower_id", followerID))
}

// Unfollow 取消关注
func (s *FollowService) Unfollow(ctx context.Context, followerID, followeeID uint64) error {
	// 1. 取消关注
	if err := s.repo.Unfollow(ctx, followerID, followeeID); err != nil {
		if err.Error() == "not following this user" {
			return ErrNotFollowing
		}
		return fmt.Errorf("failed to unfollow: %w", err)
	}

	// 2. 🆕 发送取关事件到 MQ
	event := &events.UserUnfollowedEvent{
		FollowerID: followerID,
		FolloweeID: followeeID,
	}

	if err := s.eventProducer.PublishUserUnfollowed(ctx, event); err != nil {
		logger.Warn(ctx, "⚠️ Failed to publish user unfollowed event", zap.Error(err))
	}

	// 3. 维护大V状态与双阈值防抖降级
	go s.handleUnfollowCelebrityStatus(context.Background(), followerID, followeeID)

	// 4. 立即从 Timeline 中删除被取关者的推文（不阻塞主流程），传递带有 Span 的 async 上下文
	span := trace.SpanFromContext(ctx)
	asyncCtx := trace.ContextWithSpan(context.Background(), span)
	go s.removeTweetsFromTimeline(asyncCtx, followerID, followeeID)

	return nil
}

// removeTweetsFromTimeline 从 Timeline 中删除推文
func (s *FollowService) removeTweetsFromTimeline(ctx context.Context, followerID, followeeID uint64) {
	// 获取被取关者的推文
	tweets, err := s.tweetRepo.ListByUserID(ctx, followeeID, 0, 100)
	if err != nil {
		logger.Warn(ctx, "⚠️ Failed to get tweets for removal", zap.Error(err), zap.Uint64("followee_id", followeeID))
		return
	}

	logger.Info(ctx, "🗑️ Removing tweets from timeline", zap.Int("count", len(tweets)), zap.Uint64("follower_id", followerID), zap.Uint64("followee_id", followeeID))

	// 批量删除
	for _, tweet := range tweets {
		if err := s.timelineCache.RemoveFromTimeline(ctx, followerID, tweet.ID); err != nil {
			logger.Warn(ctx, "⚠️ Failed to remove tweet from timeline", zap.Uint64("tweet_id", tweet.ID), zap.Error(err))
		}
	}

	logger.Info(ctx, "✅ Removed tweets from timeline", zap.Int("count", len(tweets)), zap.Uint64("follower_id", followerID))
}

// IsFollowing 检查是否关注
func (s *FollowService) IsFollowing(ctx context.Context, followerID, followeeID uint64) (bool, error) {
	return s.repo.IsFollowing(ctx, followerID, followeeID)
}

// GetFollowers 获取粉丝列表
func (s *FollowService) GetFollowers(ctx context.Context, userID uint64, cursor uint64, limit int) ([]uint64, uint64, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	followerIDs, err := s.repo.GetFollowers(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get followers: %w", err)
	}

	hasMore := len(followerIDs) > limit
	if hasMore {
		followerIDs = followerIDs[:limit]
	}

	var nextCursor uint64
	if hasMore && len(followerIDs) > 0 {
		nextCursor = followerIDs[len(followerIDs)-1]
	}

	return followerIDs, nextCursor, hasMore, nil
}

// GetFollowees 获取关注列表
func (s *FollowService) GetFollowees(ctx context.Context, userID uint64, cursor uint64, limit int) ([]uint64, uint64, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	followeeIDs, err := s.repo.GetFollowees(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get followees: %w", err)
	}

	hasMore := len(followeeIDs) > limit
	if hasMore {
		followeeIDs = followeeIDs[:limit]
	}

	var nextCursor uint64
	if hasMore && len(followeeIDs) > 0 {
		nextCursor = followeeIDs[len(followeeIDs)-1]
	}

	return followeeIDs, nextCursor, hasMore, nil
}

// GetFollowStats 获取关注统计
func (s *FollowService) GetFollowStats(ctx context.Context, userID uint64) (followerCount, followeeCount int64, err error) {
	followerCount, err = s.repo.GetFollowerCount(ctx, userID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get follower count: %w", err)
	}

	followeeCount, err = s.repo.GetFolloweeCount(ctx, userID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get followee count: %w", err)
	}

	return followerCount, followeeCount, nil
}

// handleFollowCelebrityStatus 处理关注后的大V状态及晋升
func (s *FollowService) handleFollowCelebrityStatus(ctx context.Context, followerID, followeeID uint64) {
	followersCount, err := s.repo.GetFollowerCount(ctx, followeeID)
	if err != nil {
		logger.Warn(ctx, "⚠️ Failed to get follower count on follow", zap.Error(err), zap.Uint64("user_id", followeeID))
		return
	}

	isCelebrity, err := s.timelineCache.IsCelebrity(ctx, followeeID)
	if err != nil {
		logger.Warn(ctx, "⚠️ Failed to check celebrity status on follow", zap.Error(err), zap.Uint64("user_id", followeeID))
		return
	}

	if !isCelebrity && followersCount >= CelebrityPromoThreshold {
		// 🚀 晋升为大V
		logger.Info(ctx, "👑 User promoted to celebrity!", zap.Uint64("user_id", followeeID), zap.Int64("followers", followersCount))
		if err := s.timelineCache.AddCelebrity(ctx, followeeID); err != nil {
			logger.Warn(ctx, "⚠️ Failed to add celebrity to global set", zap.Error(err))
		}
		// 广播晋升给老粉丝
		go s.broadcastCelebrityPromotion(context.Background(), followeeID)
	} else if isCelebrity {
		// 已是大V，将被关注者加入当前关注者的大V列表
		if err := s.timelineCache.AddCelebrityFollowee(ctx, followerID, followeeID); err != nil {
			logger.Warn(ctx, "⚠️ Failed to add celebrity followee cache", zap.Error(err), zap.Uint64("follower", followerID))
		}
	}
}

// broadcastCelebrityPromotion 异步广播大V晋升，拉取其粉丝并批量写入其粉丝的 user:celebrities 缓存
func (s *FollowService) broadcastCelebrityPromotion(ctx context.Context, celebrityID uint64) {
	// 获取该大V的全部粉丝 (设置较大的 limit 以拉取全量)
	followerIDs, err := s.repo.GetFollowers(ctx, celebrityID, 0, CelebrityPromoThreshold+1000)
	if err != nil {
		logger.Warn(ctx, "⚠️ Failed to get followers for promotion broadcast", zap.Error(err))
		return
	}

	logger.Info(ctx, "📢 Broadcasting celebrity promotion to followers...", zap.Uint64("celebrity_id", celebrityID), zap.Int("followers_count", len(followerIDs)))

	// 批量写入粉丝的关注大V列表缓存
	if err := s.timelineCache.BatchAddCelebrityFollowees(ctx, followerIDs, celebrityID); err != nil {
		logger.Warn(ctx, "⚠️ Failed to batch add celebrity followees cache", zap.Error(err), zap.Uint64("celebrity_id", celebrityID))
	}
	logger.Info(ctx, "✅ Broadcast celebrity promotion completed", zap.Uint64("celebrity_id", celebrityID))
}

// handleUnfollowCelebrityStatus 处理取消关注后的大V状态及降级
func (s *FollowService) handleUnfollowCelebrityStatus(ctx context.Context, followerID, followeeID uint64) {
	// 无论是否触发降级，当前关注者取消关注此人，需要从其关注大V缓存中移除
	if err := s.timelineCache.RemoveCelebrityFollowee(ctx, followerID, followeeID); err != nil {
		logger.Warn(ctx, "⚠️ Failed to remove celebrity followee cache", zap.Error(err), zap.Uint64("follower", followerID))
	}

	followersCount, err := s.repo.GetFollowerCount(ctx, followeeID)
	if err != nil {
		logger.Warn(ctx, "⚠️ Failed to get follower count on unfollow", zap.Error(err), zap.Uint64("user_id", followeeID))
		return
	}

	isCelebrity, err := s.timelineCache.IsCelebrity(ctx, followeeID)
	if err != nil {
		logger.Warn(ctx, "⚠️ Failed to check celebrity status on unfollow", zap.Error(err), zap.Uint64("user_id", followeeID))
		return
	}

	if isCelebrity && followersCount < CelebrityDemoteThreshold {
		// 📉 降级为普通博主
		logger.Info(ctx, "📉 User demoted to normal blogger!", zap.Uint64("user_id", followeeID), zap.Int64("followers", followersCount))
		if err := s.timelineCache.RemoveCelebrity(ctx, followeeID); err != nil {
			logger.Warn(ctx, "⚠️ Failed to remove celebrity from global set", zap.Error(err))
		}
		// 广播降级给粉丝，清理粉丝的 user:celebrities 缓存
		go s.broadcastCelebrityDemotion(context.Background(), followeeID)
	}
}

// broadcastCelebrityDemotion 异步广播大V降级，批量从粉丝的 user:celebrities 缓存中移除
func (s *FollowService) broadcastCelebrityDemotion(ctx context.Context, celebrityID uint64) {
	// 获取被降级博主的所有粉丝 (设置较大的 limit 以拉取全量)
	followerIDs, err := s.repo.GetFollowers(ctx, celebrityID, 0, CelebrityDemoteThreshold+1000)
	if err != nil {
		logger.Warn(ctx, "⚠️ Failed to get followers for demotion broadcast", zap.Error(err))
		return
	}

	logger.Info(ctx, "📢 Broadcasting celebrity demotion to followers...", zap.Uint64("celebrity_id", celebrityID), zap.Int("followers_count", len(followerIDs)))

	// 批量从粉丝的关注大V缓存中移除
	if err := s.timelineCache.BatchRemoveCelebrityFollowees(ctx, followerIDs, celebrityID); err != nil {
		logger.Warn(ctx, "⚠️ Failed to batch remove celebrity followees cache", zap.Error(err), zap.Uint64("celebrity_id", celebrityID))
	}
	logger.Info(ctx, "✅ Broadcast celebrity demotion completed", zap.Uint64("celebrity_id", celebrityID))
}

