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
func (r *bookmarkRepo) Create(ctx context.Context, bookmark *domain.Bookmark) error {
	if bookmark.ID == 0 {
		id, err := snowflake.GenerateID()
		if err != nil {
			return fmt.Errorf("failed to generate ID: %w", err)
		}
		bookmark.ID = id
	}
	if bookmark.CreatedAt == 0 {
		bookmark.CreatedAt = time.Now().UnixMilli()
	}

	result := r.db.WithContext(ctx).
		Where("user_id = ? AND tweet_id = ?", bookmark.UserID, bookmark.TweetID).
		FirstOrCreate(bookmark)

	if result.Error != nil {
		return fmt.Errorf("failed to create bookmark: %w", result.Error)
	}
	return nil
}

// Delete 取消书签
func (r *bookmarkRepo) Delete(ctx context.Context, userID, tweetID uint64) error {
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		Delete(&domain.Bookmark{})

	if result.Error != nil {
		return fmt.Errorf("failed to delete bookmark: %w", result.Error)
	}
	return nil
}

// List 获取用户书签列表 (游标分页)
func (r *bookmarkRepo) List(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Bookmark, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

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
	err := r.db.WithContext(ctx).Model(&domain.Bookmark{}).
		Where("user_id = ? AND tweet_id = ?", userID, tweetID).
		Count(&count).Error

	if err != nil {
		return false, fmt.Errorf("failed to check bookmark status: %w", err)
	}
	return count > 0, nil
}

// BatchIsBookmarked 批量检查用户是否已收藏
func (r *bookmarkRepo) BatchIsBookmarked(ctx context.Context, userID uint64, tweetIDs []uint64) (map[uint64]bool, error) {
	if len(tweetIDs) == 0 {
		return map[uint64]bool{}, nil
	}

	var bookmarkedIDs []uint64
	err := r.db.WithContext(ctx).Model(&domain.Bookmark{}).
		Select("tweet_id").
		Where("user_id = ? AND tweet_id IN ?", userID, tweetIDs).
		Pluck("tweet_id", &bookmarkedIDs).Error

	if err != nil {
		return nil, fmt.Errorf("failed to batch check bookmark status: %w", err)
	}

	bookmarkedMap := make(map[uint64]bool, len(tweetIDs))
	for _, id := range bookmarkedIDs {
		bookmarkedMap[id] = true
	}
	return bookmarkedMap, nil
}
