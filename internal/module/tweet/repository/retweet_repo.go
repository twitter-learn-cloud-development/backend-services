package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"twitter-clone/internal/domain"
	"twitter-clone/pkg/pkg/snowflake"
)

// retweetRepo 转发仓储实现
type retweetRepo struct {
	db *gorm.DB
}

// NewRetweetRepository 创建转发仓储
func NewRetweetRepository(db *gorm.DB) domain.RetweetRepository {
	return &retweetRepo{db: db}
}

// Create 创建转发（幂等）
func (r *retweetRepo) Create(ctx context.Context, userID, tweetID uint64) error {
	retweet := &domain.Retweet{
		ID:        snowflake.GenerateID(),
		UserID:    userID,
		TweetID:   tweetID,
		CreatedAt: time.Now().UnixMilli(),
	}

	result := r.db.WithContext(ctx).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		FirstOrCreate(retweet)

	if result.Error != nil {
		return fmt.Errorf("failed to retweet: %w", result.Error)
	}
	return nil
}

// Delete 取消转发
func (r *retweetRepo) Delete(ctx context.Context, userID, tweetID uint64) error {
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		Delete(&domain.Retweet{})

	if result.Error != nil {
		return fmt.Errorf("failed to unretweet: %w", result.Error)
	}
	return nil
}

// IsRetweeted 检查用户是否已转发
func (r *retweetRepo) IsRetweeted(ctx context.Context, userID, tweetID uint64) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.Retweet{}).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		Count(&count).Error

	if err != nil {
		return false, fmt.Errorf("failed to check retweet status: %w", err)
	}
	return count > 0, nil
}

// GetRetweetCount 获取推文转发数
func (r *retweetRepo) GetRetweetCount(ctx context.Context, tweetID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.Retweet{}).
		Where("tweet_id = ?", tweetID).
		Count(&count).Error

	if err != nil {
		return 0, fmt.Errorf("failed to get retweet count: %w", err)
	}
	return count, nil
}

// BatchGetRetweetCounts 批量获取推文转发数
func (r *retweetRepo) BatchGetRetweetCounts(ctx context.Context, tweetIDs []uint64) (map[uint64]int64, error) {
	if len(tweetIDs) == 0 {
		return map[uint64]int64{}, nil
	}

	type Result struct {
		TweetID uint64 `gorm:"column:tweet_id"`
		Count   int64  `gorm:"column:count"`
	}

	var results []Result
	err := r.db.WithContext(ctx).Model(&domain.Retweet{}).
		Select("tweet_id, COUNT(*) as count").
		Where("tweet_id IN ?", tweetIDs).
		Group("tweet_id").
		Find(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to batch get retweet counts: %w", err)
	}

	countMap := make(map[uint64]int64, len(tweetIDs))
	for _, r := range results {
		countMap[r.TweetID] = r.Count
	}
	return countMap, nil
}

// BatchIsRetweeted 批量检查用户是否已转发
func (r *retweetRepo) BatchIsRetweeted(ctx context.Context, userID uint64, tweetIDs []uint64) (map[uint64]bool, error) {
	if len(tweetIDs) == 0 {
		return map[uint64]bool{}, nil
	}

	var retweetedIDs []uint64
	err := r.db.WithContext(ctx).Model(&domain.Retweet{}).
		Select("tweet_id").
		Where("user_id = ? AND tweet_id IN ?", userID, tweetIDs).
		Pluck("tweet_id", &retweetedIDs).Error

	if err != nil {
		return nil, fmt.Errorf("failed to batch check retweet status: %w", err)
	}

	retweetedMap := make(map[uint64]bool, len(tweetIDs))
	for _, id := range retweetedIDs {
		retweetedMap[id] = true
	}
	return retweetedMap, nil
}
