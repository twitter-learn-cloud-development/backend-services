package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"twitter-clone/internal/domain"
	"twitter-clone/pkg/pkg/snowflake"
)

// commentRepo 评论仓储实现
type commentRepo struct {
	db *gorm.DB
}

// NewCommentRepository 创建评论仓储
func NewCommentRepository(db *gorm.DB) domain.CommentRepository {
	return &commentRepo{db: db}
}

// Create 创建评论
func (r *commentRepo) Create(ctx context.Context, comment *domain.Comment) error {
	comment.ID = snowflake.GenerateID()
	comment.CreatedAt = time.Now().UnixMilli()
	comment.DeletedAt = 0

	if err := r.db.WithContext(ctx).Create(comment).Error; err != nil {
		return fmt.Errorf("failed to create comment: %w", err)
	}
	return nil
}

// Delete 删除评论
func (r *commentRepo) Delete(ctx context.Context, id uint64) error {
	now := time.Now().UnixMilli()
	result := r.db.WithContext(ctx).Model(&domain.Comment{}).
		Where("id = ? AND deleted_at = 0", id).
		Update("deleted_at", now)

	if result.Error != nil {
		return fmt.Errorf("failed to delete comment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("comment not found or already deleted: id=%d", id)
	}
	return nil
}

// GetByID 获取评论详情
func (r *commentRepo) GetByID(ctx context.Context, id uint64) (*domain.Comment, error) {
	var comment domain.Comment
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at = 0", id).
		First(&comment).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("comment not found: id=%d", id)
		}
		return nil, fmt.Errorf("failed to get comment: %w", err)
	}
	return &comment, nil
}

// ListByTweetID 获取推文的评论列表 (按时间倒序)
func (r *commentRepo) ListByTweetID(ctx context.Context, tweetID uint64, cursor uint64, limit int) ([]*domain.Comment, error) {
	var comments []*domain.Comment
	query := r.db.WithContext(ctx).
		Where("tweet_id = ? AND deleted_at = 0", tweetID).
		Order("id DESC").
		Limit(limit)

	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}

	if err := query.Find(&comments).Error; err != nil {
		return nil, fmt.Errorf("failed to list comments: %w", err)
	}
	return comments, nil
}

// GetCommentCount 获取推文评论数
func (r *commentRepo) GetCommentCount(ctx context.Context, tweetID uint64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&domain.Comment{}).
		Where("tweet_id = ? AND deleted_at = 0", tweetID).
		Count(&count).Error

	if err != nil {
		return 0, fmt.Errorf("failed to get comment count: %w", err)
	}
	return count, nil
}

// BatchGetCommentCounts 批量获取推文评论数
func (r *commentRepo) BatchGetCommentCounts(ctx context.Context, tweetIDs []uint64) (map[uint64]int64, error) {
	if len(tweetIDs) == 0 {
		return map[uint64]int64{}, nil
	}

	type Result struct {
		TweetID uint64 `gorm:"column:tweet_id"`
		Count   int64  `gorm:"column:count"`
	}

	var results []Result
	err := r.db.WithContext(ctx).Model(&domain.Comment{}).
		Select("tweet_id, COUNT(*) as count").
		Where("tweet_id IN ? AND deleted_at = 0", tweetIDs).
		Group("tweet_id").
		Find(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to batch get comment counts: %w", err)
	}

	countMap := make(map[uint64]int64, len(tweetIDs))
	for _, r := range results {
		countMap[r.TweetID] = r.Count
	}
	return countMap, nil
}
