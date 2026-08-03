package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/pkg/database/uow"
	"twitter-clone/pkg/pkg/snowflake"
)

// likeRepo 点赞仓储实现
type likeRepo struct {
	db *gorm.DB
}

// NewLikeRepository 创建点赞仓储
func NewLikeRepository(db *gorm.DB) domain.LikeRepository {
	return &likeRepo{db: db}
}

// Like 点赞（幂等）
func (r *likeRepo) Like(ctx context.Context, userID, tweetID uint64) error {
	db := uow.ExtractTx(ctx, r.db)
	var existing domain.Like
	err := db.WithContext(ctx).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		First(&existing).Error
	if err == nil {
		// Already liked, return nil for idempotency
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check existing like: %w", err)
	}

	id, err := snowflake.GenerateID()
	if err != nil {
		return fmt.Errorf("failed to generate ID: %w", err)
	}

	like := &domain.Like{
		ID:        id,
		UserID:    userID,
		TweetID:   tweetID,
		CreatedAt: time.Now().UnixMilli(),
	}

	if err := db.WithContext(ctx).Create(like).Error; err != nil {
		// If another concurrent insert succeeded in the meantime, ignore the duplicate entry error
		// to maintain idempotency
		if strings.Contains(err.Error(), "1062") || strings.Contains(err.Error(), "Duplicate entry") || strings.Contains(err.Error(), "duplicate key") {
			return nil
		}
		return fmt.Errorf("failed to create like: %w", err)
	}
	return nil
}

// Unlike 取消点赞
func (r *likeRepo) Unlike(ctx context.Context, userID, tweetID uint64) error {
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		Delete(&domain.Like{})

	if result.Error != nil {
		return fmt.Errorf("failed to unlike tweet: %w", result.Error)
	}
	return nil
}

// IsLiked 检查用户是否已点赞
func (r *likeRepo) IsLiked(ctx context.Context, userID, tweetID uint64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.Like{}).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		Count(&count).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check like status: %w", err)
	}
	return count > 0, nil
}

// GetLikeCount 获取推文的点赞数
func (r *likeRepo) GetLikeCount(ctx context.Context, tweetID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.Like{}).
		Where("tweet_id = ?", tweetID).
		Count(&count).Error

	if err != nil {
		return 0, fmt.Errorf("failed to get like count: %w", err)
	}
	return count, nil
}

// BatchGetLikeCounts 批量获取推文的点赞数
func (r *likeRepo) BatchGetLikeCounts(ctx context.Context, tweetIDs []uint64) (map[uint64]int64, error) {
	if len(tweetIDs) == 0 {
		return map[uint64]int64{}, nil
	}

	type Result struct {
		TweetID uint64 `gorm:"column:tweet_id"`
		Count   int64  `gorm:"column:count"`
	}

	var results []Result
	err := r.db.WithContext(ctx).Model(&domain.Like{}).
		Select("tweet_id, COUNT(*) as count").
		Where("tweet_id IN ?", tweetIDs).
		Group("tweet_id").
		Find(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to batch get like counts: %w", err)
	}

	countMap := make(map[uint64]int64, len(tweetIDs))
	for _, r := range results {
		countMap[r.TweetID] = r.Count
	}
	return countMap, nil
}

// BatchIsLiked 批量检查用户是否已点赞
func (r *likeRepo) BatchIsLiked(ctx context.Context, userID uint64, tweetIDs []uint64) (map[uint64]bool, error) {
	if len(tweetIDs) == 0 {
		return map[uint64]bool{}, nil
	}

	var likedTweetIDs []uint64
	err := r.db.WithContext(ctx).Model(&domain.Like{}).
		Select("tweet_id").
		Where("user_id = ? AND tweet_id IN ?", userID, tweetIDs).
		Pluck("tweet_id", &likedTweetIDs).Error

	if err != nil {
		return nil, fmt.Errorf("failed to batch check like status: %w", err)
	}

	likedMap := make(map[uint64]bool, len(tweetIDs))
	for _, id := range likedTweetIDs {
		likedMap[id] = true
	}
	return likedMap, nil
}

// ListByUserID 获取用户点赞的记录列表 (分页)
func (r *likeRepo) ListByUserID(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Like, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var likes []*domain.Like
	query := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id DESC").
		Limit(limit)

	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}

	if err := query.Find(&likes).Error; err != nil {
		return nil, fmt.Errorf("failed to list user likes: %w", err)
	}
	return likes, nil
}
