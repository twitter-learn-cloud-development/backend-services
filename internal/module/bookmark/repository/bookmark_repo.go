package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"twitter-clone/internal/domain"
	"twitter-clone/pkg/pkg/snowflake"
)

// bookmarkRepo 书签仓储实现
type bookmarkRepo struct {
	db *gorm.DB
}

// NewBookmarkRepository 创建书签仓储
func NewBookmarkRepository(db *gorm.DB) domain.BookmarkRepository {
	return &bookmarkRepo{db: db}
}

// Create 添加书签
func (r *bookmarkRepo) Create(ctx context.Context, b *domain.Bookmark) error {
	b.ID = snowflake.GenerateID()
	b.CreatedAt = time.Now().UnixMilli()

	if err := r.db.WithContext(ctx).Create(b).Error; err != nil {
		return fmt.Errorf("failed to create bookmark: %w", err)
	}
	return nil
}

// Delete 取消书签
func (r *bookmarkRepo) Delete(ctx context.Context, userID, tweetID uint64) error {
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		Delete(&domain.Bookmark{}).Error; err != nil {
		return fmt.Errorf("failed to delete bookmark: %w", err)
	}
	return nil
}

// List 获取用户书签列表 (按 ID 倒序)
func (r *bookmarkRepo) List(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Bookmark, error) {
	var bookmarks []*domain.Bookmark
	query := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id DESC").
		Limit(limit)

	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}

	if err := query.Find(&bookmarks).Error; err != nil {
		return nil, fmt.Errorf("failed to list bookmarks: %w", err)
	}
	return bookmarks, nil
}

// IsBookmarked 检查是否已收藏
func (r *bookmarkRepo) IsBookmarked(ctx context.Context, userID, tweetID uint64) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.Bookmark{}).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check bookmark: %w", err)
	}
	return count > 0, nil
}
