package domain

import "context"

// Retweet 转发实体
type Retweet struct {
	ID        uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)"`
	UserID    uint64 `gorm:"uniqueIndex:uk_user_tweet;not null;comment:转发用户ID"`
	TweetID   uint64 `gorm:"uniqueIndex:uk_user_tweet;index:idx_tweet;not null;comment:推文ID"`
	CreatedAt int64  `gorm:"not null;comment:创建时间戳 (毫秒)"`
}

// TableName 指定表名
func (Retweet) TableName() string {
	return "retweets"
}

// RetweetRepository 转发仓储接口
type RetweetRepository interface {
	// Create 创建转发（幂等）
	Create(ctx context.Context, userID, tweetID uint64) error

	// Delete 取消转发
	Delete(ctx context.Context, userID, tweetID uint64) error

	// IsRetweeted 检查用户是否已转发
	IsRetweeted(ctx context.Context, userID, tweetID uint64) (bool, error)

	// GetRetweetCount 获取推文转发数
	GetRetweetCount(ctx context.Context, tweetID uint64) (int64, error)

	// BatchGetRetweetCounts 批量获取推文转发数
	BatchGetRetweetCounts(ctx context.Context, tweetIDs []uint64) (map[uint64]int64, error)

	// BatchIsRetweeted 批量检查用户是否已转发
	BatchIsRetweeted(ctx context.Context, userID uint64, tweetIDs []uint64) (map[uint64]bool, error)
}
