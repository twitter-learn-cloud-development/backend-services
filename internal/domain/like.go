package domain

import "context"

// Like 点赞实体
type Like struct {
	ID        uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)"`
	UserID    uint64 `gorm:"uniqueIndex:uk_user_tweet;not null;comment:点赞用户ID"`
	TweetID   uint64 `gorm:"uniqueIndex:uk_user_tweet;index:idx_tweet;not null;comment:推文ID"`
	CreatedAt int64  `gorm:"not null;comment:创建时间戳 (毫秒)"`
}

// TableName 指定表名
func (Like) TableName() string {
	return "likes"
}

// LikeRepository 点赞仓储接口
type LikeRepository interface {
	// Like 点赞（幂等，重复点赞不报错）
	Like(ctx context.Context, userID, tweetID uint64) error

	// Unlike 取消点赞
	Unlike(ctx context.Context, userID, tweetID uint64) error

	// IsLiked 检查用户是否已点赞
	IsLiked(ctx context.Context, userID, tweetID uint64) (bool, error)

	// GetLikeCount 获取推文的点赞数
	GetLikeCount(ctx context.Context, tweetID uint64) (int64, error)

	// BatchGetLikeCounts 批量获取推文的点赞数
	BatchGetLikeCounts(ctx context.Context, tweetIDs []uint64) (map[uint64]int64, error)

	// BatchIsLiked 批量检查用户是否已点赞
	BatchIsLiked(ctx context.Context, userID uint64, tweetIDs []uint64) (map[uint64]bool, error)
}
