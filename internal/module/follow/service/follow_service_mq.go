package service

import (
	"context"
	"errors"
	"fmt"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/tweet/cache"
	"twitter-clone/internal/mq/producer"
	"twitter-clone/internal/pkg/database/uow"
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
	repo   domain.FollowRepository
	uow    uow.Manager
	outbox domain.OutboxEventRepository
}

// NewFollowService 创建关注服务
func NewFollowService(
	repo domain.FollowRepository,
	_ domain.TweetRepository,
	_ *cache.TimelineCache,
	_ *producer.EventProducer, // Legacy constructor compatibility; writes use Outbox.
) *FollowService {
	return &FollowService{
		repo: repo,
	}
}

// Follow 关注用户
func (s *FollowService) Follow(ctx context.Context, followerID, followeeID uint64) error {
	return s.changeFollowWithOutbox(ctx, followerID, followeeID, true)
}

// Unfollow only commits facts and an event; the durable Worker owns projections.
func (s *FollowService) Unfollow(ctx context.Context, followerID, followeeID uint64) error {
	return s.changeFollowWithOutbox(ctx, followerID, followeeID, false)
}

// IsFollowing reads the authoritative current relation.
func (s *FollowService) IsFollowing(ctx context.Context, followerID, followeeID uint64) (bool, error) {
	return s.repo.IsFollowing(ctx, followerID, followeeID)
}

// GetFollowers 获取粉丝列表
func (s *FollowService) GetFollowers(ctx context.Context, userID uint64, cursor uint64, limit int) ([]uint64, uint64, bool, error) {
	if err := validateFollowPageRequest(ctx, userID); err != nil {
		return nil, 0, false, err
	}
	limit = publicFollowPageLimit(limit)
	pager, ok := s.repo.(domain.FollowerPageRepository)
	if !ok {
		return nil, 0, false, ErrFollowPaginationUnavailable
	}
	page, err := pager.ListFollowerPage(ctx, userID, cursor, limit)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get followers: %w", err)
	}
	if err := validateFollowPage(page.FollowerIDs, page.NextCursor, page.HasMore, cursor, limit); err != nil {
		return nil, 0, false, err
	}
	return nonNilFollowIDs(page.FollowerIDs), page.NextCursor, page.HasMore, nil
}

// GetFollowees 获取关注列表
func (s *FollowService) GetFollowees(ctx context.Context, userID uint64, cursor uint64, limit int) ([]uint64, uint64, bool, error) {
	if err := validateFollowPageRequest(ctx, userID); err != nil {
		return nil, 0, false, err
	}
	limit = publicFollowPageLimit(limit)
	pager, ok := s.repo.(domain.FolloweePageRepository)
	if !ok {
		return nil, 0, false, ErrFollowPaginationUnavailable
	}
	page, err := pager.ListFolloweePage(ctx, userID, cursor, limit)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get followees: %w", err)
	}
	if err := validateFollowPage(page.FolloweeIDs, page.NextCursor, page.HasMore, cursor, limit); err != nil {
		return nil, 0, false, err
	}
	return nonNilFollowIDs(page.FolloweeIDs), page.NextCursor, page.HasMore, nil
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
