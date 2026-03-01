package domain

import "context"

// Comment 评论实体
type Comment struct {
	ID        uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)"`
	UserID    uint64 `gorm:"index:idx_user;not null;comment:评论者ID"`
	TweetID   uint64 `gorm:"index:idx_tweet;not null;comment:推文ID"`
	ParentID  uint64 `gorm:"index:idx_parent;default:0;comment:父评论ID (0表示一级评论)"`
	Content   string `gorm:"type:text;not null;comment:评论内容"`
	CreatedAt int64  `gorm:"not null;comment:创建时间戳 (毫秒)"`
	DeletedAt int64  `gorm:"default:0;comment:软删除时间戳"`

	// 聚合字段
	User *User `gorm:"-"` // 评论者信息
}

// TableName 指定表名
func (Comment) TableName() string {
	return "comments"
}

// CommentRepository 评论仓储接口
type CommentRepository interface {
	// Create 创建评论
	Create(ctx context.Context, comment *Comment) error

	// Delete 删除评论
	Delete(ctx context.Context, id uint64) error

	// GetByID 获取评论详情
	GetByID(ctx context.Context, id uint64) (*Comment, error)

	// ListByTweetID 获取推文的评论列表 (支持分页)
	ListByTweetID(ctx context.Context, tweetID uint64, cursor uint64, limit int) ([]*Comment, error)

	// GetCommentCount 获取推文评论数
	GetCommentCount(ctx context.Context, tweetID uint64) (int64, error)

	// BatchGetCommentCounts 批量获取推文评论数
	BatchGetCommentCounts(ctx context.Context, tweetIDs []uint64) (map[uint64]int64, error)
}
