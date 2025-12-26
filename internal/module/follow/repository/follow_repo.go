package repository

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/pkg/pkg/snowflake"
)

type followRepo struct {
	db *gorm.DB
}

// NewFollowRepository 创建关注仓储
func NewFollowRepository(db *gorm.DB) domain.FollowRepository {
	return &followRepo{db: db}
}

// Follow 关注用户
func (r *followRepo) Follow(ctx context.Context, followerID, followeeID uint64) error {
	//不能关注自己
	if followerID == followeeID {
		return fmt.Errorf("cannot follow yourself")
	}

	//检查是否已经关注
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.Follow{}).Where("follower_id = ? AND followee_id = ? AND deleted_at = 0", followerID, followeeID).Count(&count).Error
	if err != nil {
		return fmt.Errorf("failed to check follow status: %w", err)
	}

	if count > 0 {
		return fmt.Errorf("already following")
	}

	//创建关注关系
	follow := &domain.Follow{
		ID:         snowflake.GenerateID(),
		FollowerID: followerID,
		FolloweeID: followeeID,
		CreatedAt:  time.Now().UnixMilli(),
		DeletedAt:  0,
	}

	if err := r.db.WithContext(ctx).Create(follow).Error; err != nil {
		return fmt.Errorf("failed to create follow: %w", err)
	}

	return nil
}

// unfollow 取消关注
func (r *followRepo) Unfollow(ctx context.Context, followerID, followeeID uint64) error {
	now := time.Now().UnixMilli()

	result := r.db.WithContext(ctx).Model(&domain.Follow{}).Where("follower_id = ? and followee_id = ? and deleted_at = 0", followerID, followeeID).Update("deleted_at", now)

	if result.Error != nil {
		return fmt.Errorf("failed to unfollow: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("not following this user")
	}

	return nil
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

	query := r.db.WithContext(ctx).Model(&domain.Follow{}).Where("followee_id = ? and deleted_id = 0", userID)

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
