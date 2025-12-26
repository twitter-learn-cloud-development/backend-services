package repository

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/pkg/pkg/snowflake"
)

// tweetRepo 推文仓储实现
type tweetRepo struct {
	db *gorm.DB
}

// NewTweetRepository 创建推文仓储
func NewTweetRepository(db *gorm.DB) domain.TweetRepository {
	return &tweetRepo{db: db}
}

func (r *tweetRepo) Create(ctx context.Context, tweet *domain.Tweet) error {
	// 1. 生成 Snowflake ID
	if tweet.ID == 0 {
		tweet.ID = snowflake.GenerateID()
	}

	//2. 设置时间戳
	now := time.Now().UnixMilli()
	if tweet.CreatedAt == 0 {
		tweet.CreatedAt = now
	}
	if tweet.UpdatedAt == 0 {
		tweet.UpdatedAt = now
	}
	tweet.DeletedAt = 0

	// 3. 默认值处理
	if tweet.Type == 0 && len(tweet.MediaURLs) > 0 {
		// 如果有媒体但没设置类型，默认为图片
		tweet.Type = domain.TweetTypeImage
	}
	if tweet.VisibleType == 0 {
		tweet.VisibleType = domain.VisiblePublic
	}

	// 4. 存入数据库
	if err := r.db.WithContext(ctx).Create(tweet).Error; err != nil {
		return fmt.Errorf("failed to create tweet: %w", err)
	}

	return nil
}

// Delete 删除(软删除)
func (r *tweetRepo) Delete(ctx context.Context, id uint64) error {
	now := time.Now().UnixMilli()
	result := r.db.WithContext(ctx).Model(&domain.Tweet{}).Where("id = ? and deleted_at = 0", id).Updates(map[string]interface{}{
		"deleted_at": now,
		"updated_at": now,
	})
	if result.Error != nil {
		return fmt.Errorf("failed to delete tweet: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("tweet not found or already deleted: id=%d", id)
	}
	return nil
}

// GetByID 根据 ID 查询推文
func (r *tweetRepo) GetByID(ctx context.Context, id uint64) (*domain.Tweet, error) {
	var tweet domain.Tweet
	err := r.db.WithContext(ctx).Where("id = ? and deleted_at = 0", id).First(&tweet).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("tweet not found: id=%d", id)
		}
		return nil, fmt.Errorf("failed to get tweet by id: %w", err)
	}
	return &tweet, nil
}

// ListByUserID 查询某个用户的推文（游标分页）
func (r *tweetRepo) ListByUserID(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, error) {
	//参数验证
	if limit <= 0 {
		limit = 20 //默认值
	}
	if limit > 100 {
		limit = 100 //最大值
	}

	var tweets []*domain.Tweet
	// 构建查询
	query := r.db.WithContext(ctx).
		Where("user_id = ? AND deleted_at = 0", userID).
		Order("id DESC"). // ID 降序 = 时间降序（Snowflake ID 特性）
		Limit(limit)

	// 如果有游标，从游标之后开始
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}

	// 执行查询
	if err := query.Find(&tweets).Error; err != nil {
		return nil, fmt.Errorf("failed to list tweets by user: %w", err)
	}

	return tweets, nil
}

// ListFeeds 查关注流 (拉模式：核心 SQL 实现)
func (r *tweetRepo) ListFeeds(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Tweet, error) {
	// 参数验证
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var tweets []*domain.Tweet

	// 子查询：查出我关注的人的 ID
	// SQL: SELECT followee_id FROM follows WHERE follower_id = ? AND deleted_at = 0
	subQuery := r.db.Table("follows").
		Select("followee_id").
		Where("follower_id = ? AND deleted_at = 0", userID)

	// 主查询：查这些人的推文
	// SQL: SELECT * FROM tweets WHERE user_id IN (subQuery) AND deleted_at = 0 ...
	query := r.db.WithContext(ctx).
		Where("user_id IN (?)", subQuery).
		Where("deleted_at = 0 AND visible_type = ?", domain.VisiblePublic).
		Order("id DESC").
		Limit(limit)

	// 游标分页
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}

	if err := query.Find(&tweets).Error; err != nil {
		return nil, fmt.Errorf("failed to list feeds: %w", err)
	}

	return tweets, nil
}

// GetByIDs 批量查询推文
func (r *tweetRepo) GetByIDs(ctx context.Context, ids []uint64) ([]*domain.Tweet, error) {
	if len(ids) == 0 {
		return []*domain.Tweet{}, nil
	}

	var tweets []*domain.Tweet

	err := r.db.WithContext(ctx).
		Where("id IN ? AND deleted_at = 0", ids).
		Find(&tweets).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get tweets by ids: %w", err)
	}

	return tweets, nil
}
