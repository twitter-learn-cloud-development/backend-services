package domain

import (
	"context"
)

// Bookmark 书签/收藏实体
type Bookmark struct {
	ID        uint64 `gorm:"primaryKey;column:id;comment:主键ID"`
	UserID    uint64 `gorm:"index:idx_user_bookmark;not null;comment:用户ID"`
	TweetID   uint64 `gorm:"index:idx_user_bookmark;not null;comment:推文ID"`
	CreatedAt int64  `gorm:"not null;comment:收藏时间"`
}

// TableName 指定表名
func (Bookmark) TableName() string {
	return "bookmarks"
}

// BookmarkRepository 书签仓储接口
type BookmarkRepository interface {
	// Create 添加书签
	Create(ctx context.Context, bookmark *Bookmark) error

	// Delete 取消书签
	Delete(ctx context.Context, userID, tweetID uint64) error

	// List 获取用户书签列表 (分页)
	List(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*Bookmark, error)

	// IsBookmarked 检查是否已收藏
	IsBookmarked(ctx context.Context, userID, tweetID uint64) (bool, error)
}
