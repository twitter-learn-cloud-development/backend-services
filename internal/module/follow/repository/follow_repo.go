package repository

import (
	"context"
	"fmt"
	"twitter-clone/internal/domain"

	"gorm.io/gorm"
)

type followRepo struct {
	db            *gorm.DB
	newRelationID func() (uint64, error)
}

// NewFollowRepository 创建关注仓储
func NewFollowRepository(db *gorm.DB) domain.FollowRepository {
	return &followRepo{db: db}
}

// Follow 关注用户
func (r *followRepo) Follow(ctx context.Context, followerID, followeeID uint64) error {
	_, err := r.ApplyFollowTransition(ctx, followerID, followeeID, true)
	return err
}

// unfollow 取消关注
func (r *followRepo) Unfollow(ctx context.Context, followerID, followeeID uint64) error {
	_, err := r.ApplyFollowTransition(ctx, followerID, followeeID, false)
	return err
}

// IsFollowing 检查是否关注
func (r *followRepo) IsFollowing(ctx context.Context, followerID, followeeID uint64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&domain.Follow{}).
		Where("follower_id = ? AND followee_id = ? AND deleted_at = 0", followerID, followeeID).
		Count(&count).Error

	if err != nil {
		return false, fmt.Errorf("failed to check follow status: %w", err)
	}

	return count > 0, nil
}

// GetFollowers 获取粉丝列表
func (r *followRepo) GetFollowers(ctx context.Context, userID uint64, cursor uint64, limit int) ([]uint64, error) {
	if limit <= 0 {
		limit = 20
	}

	if limit > 100 {
		limit = 100
	}

	query := r.db.WithContext(ctx).Model(&domain.Follow{}).Where("followee_id = ? and deleted_at = 0", userID)

	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}

	// 步骤3：排序 + 限制数量
	query = query.
		Order("id DESC").
		Limit(limit)

	var follows []domain.Follow
	if err := query.Find(&follows).Error; err != nil {
		return nil, fmt.Errorf("failed to get followers: %w", err)
	}

	followerIDs := make([]uint64, len(follows))
	for i, follow := range follows {
		followerIDs[i] = follow.FollowerID
	}

	return followerIDs, nil
}

// ListFollowerPage exposes the follow relation cursor for public lists and
// internal replayable jobs. Legacy IDs-only methods remain for other callers.
func (r *followRepo) ListFollowerPage(ctx context.Context, userID uint64, cursor uint64, limit int) (domain.FollowerPage, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}

	var follows []domain.Follow
	if err := r.followerPageQuery(ctx, userID, cursor, limit).Find(&follows).Error; err != nil {
		return domain.FollowerPage{}, fmt.Errorf("failed to list follower page: %w", err)
	}

	return buildFollowerPage(follows, limit), nil
}

func buildFollowerPage(follows []domain.Follow, limit int) domain.FollowerPage {
	hasMore := limit > 0 && len(follows) > limit
	if hasMore {
		follows = follows[:limit]
	}
	followerIDs := make([]uint64, len(follows))
	for index, follow := range follows {
		followerIDs[index] = follow.FollowerID
	}

	var nextCursor uint64
	if hasMore && len(follows) > 0 {
		nextCursor = follows[len(follows)-1].ID
	}
	return domain.FollowerPage{
		FollowerIDs: followerIDs,
		NextCursor:  nextCursor,
		HasMore:     hasMore,
	}
}

// GetFollowees 获取关注列表
func (r *followRepo) GetFollowees(ctx context.Context, userID uint64, cursor uint64, limit int) ([]uint64, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	query := r.db.WithContext(ctx).
		Model(&domain.Follow{}).
		Where("follower_id = ? AND deleted_at = 0", userID)

	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}

	// 步骤3：排序 + 限制数量
	query = query.
		Order("id DESC").
		Limit(limit)

	var follows []domain.Follow
	if err := query.Find(&follows).Error; err != nil {
		return nil, fmt.Errorf("failed to get followees: %w", err)
	}

	followeeIDs := make([]uint64, len(follows))
	for i, follow := range follows {
		followeeIDs[i] = follow.FolloweeID
	}

	return followeeIDs, nil
}

// GetFollowerCount 获取粉丝数
func (r *followRepo) GetFollowerCount(ctx context.Context, userID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&domain.Follow{}).
		Where("followee_id = ? AND deleted_at = 0", userID).
		Count(&count).Error

	if err != nil {
		return 0, fmt.Errorf("failed to get follower count: %w", err)
	}

	return count, nil
}

// GetFolloweeCount 获取关注数
func (r *followRepo) GetFolloweeCount(ctx context.Context, userID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&domain.Follow{}).
		Where("follower_id = ? AND deleted_at = 0", userID).
		Count(&count).Error

	if err != nil {
		return 0, fmt.Errorf("failed to get followee count: %w", err)
	}

	return count, nil
}

// GetActiveFollowers 获取活跃粉丝（用于推送 Timeline）
func (r *followRepo) GetActiveFollowers(ctx context.Context, userID uint64, limit int) ([]uint64, error) {
	if limit <= 0 {
		limit = 1000
	}

	// 获取最近关注的粉丝（最活跃）
	var follows []domain.Follow
	err := r.db.WithContext(ctx).
		Where("followee_id = ? AND deleted_at = 0", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&follows).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get active followers: %w", err)
	}

	followerIDs := make([]uint64, len(follows))
	for i, follow := range follows {
		followerIDs[i] = follow.FollowerID
	}

	return followerIDs, nil
}

// GetCelebrities 查询数据库中粉丝数 >= threshold 的大V ID 列表 (用于对账任务)
func (r *followRepo) GetCelebrities(ctx context.Context, threshold int64) ([]uint64, error) {
	var celebrityIDs []uint64
	err := r.db.WithContext(ctx).
		Model(&domain.Follow{}).
		Select("followee_id").
		Where("deleted_at = 0").
		Group("followee_id").
		Having("COUNT(*) >= ?", threshold).
		Find(&celebrityIDs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get celebrities from DB: %w", err)
	}
	return celebrityIDs, nil
}
