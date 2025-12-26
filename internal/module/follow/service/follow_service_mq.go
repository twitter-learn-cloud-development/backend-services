package service

import (
	"context"
	"errors"
	"fmt"
	"log"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/module/tweet/cache"
	"twitter-clone/internal/mq/producer"
)

var (
	// ErrCannotFollowSelf 不能关注自己
	ErrCannotFollowSelf = errors.New("cannot follow yourself")

	// ErrAlreadyFollowing 已经关注
	ErrAlreadyFollowing = errors.New("already following")

	// ErrNotFollowing 没有关注
	ErrNotFollowing = errors.New("not following")
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
		log.Printf("⚠️  Failed to publish user followed event: %v", err)
	}

	// 4. 🔥 立即拉取被关注者最近的推文（不阻塞主流程）
	go s.pullRecentTweetsToTimeline(context.Background(), followerID, followeeID)

	return nil
}

// pullRecentTweetsToTimeline 拉取最新推文到 Timeline
func (s *FollowService) pullRecentTweetsToTimeline(ctx context.Context, followerID, followeeID uint64) {
	// 获取被关注者最近的 50 条推文
	tweets, err := s.tweetRepo.ListByUserID(ctx, followeeID, 0, 50)
	if err != nil {
		log.Printf("⚠️  Failed to get recent tweets: %v", err)
		return
	}

	if len(tweets) == 0 {
		return
	}

	log.Printf("📥 Pulling %d recent tweets to timeline for user %d", len(tweets), followerID)

	// 批量添加到关注者的 Timeline
	for _, tweet := range tweets {
		if err := s.timelineCache.AddToTimeline(ctx, followerID, tweet.ID); err != nil {
			log.Printf("⚠️  Failed to add tweet %d to timeline: %v", tweet.ID, err)
		}
	}

	log.Printf("✅ Pulled %d tweets to timeline for user %d", len(tweets), followerID)
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
		log.Printf("⚠️  Failed to publish user unfollowed event: %v", err)
	}

	// 3. 立即从 Timeline 中删除被取关者的推文（不阻塞主流程）
	go s.removeTweetsFromTimeline(context.Background(), followerID, followeeID)

	return nil
}

// removeTweetsFromTimeline 从 Timeline 中删除推文
func (s *FollowService) removeTweetsFromTimeline(ctx context.Context, followerID, followeeID uint64) {
	// 获取被取关者的推文
	tweets, err := s.tweetRepo.ListByUserID(ctx, followeeID, 0, 100)
	if err != nil {
		log.Printf("⚠️  Failed to get tweets for removal: %v", err)
		return
	}

	log.Printf("🗑️  Removing %d tweets from timeline for user %d", len(tweets), followerID)

	// 批量删除
	for _, tweet := range tweets {
		if err := s.timelineCache.RemoveFromTimeline(ctx, followerID, tweet.ID); err != nil {
			log.Printf("⚠️  Failed to remove tweet %d from timeline: %v", tweet.ID, err)
		}
	}

	log.Printf("✅ Removed %d tweets from timeline for user %d", len(tweets), followerID)
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
