package domain

import "context"

// Follow 关注关系实体
type Follow struct {
	ID         uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)" test_data:"id"`
	FollowerID uint64 `gorm:"index:idx_follower;not null;comment:关注者ID" test_data:"follower_id"`
	FolloweeID uint64 `gorm:"index:idx_followee;not null;comment:被关注者ID" test_data:"followee_id"`
	CreatedAt  int64  `gorm:"not null;comment:创建时间戳" test_data:"created_at"`
	DeletedAt  int64  `gorm:"default:0;comment:软删除时间戳" test_data:"-"`
}

// TableName 指定表名
func (Follow) TableName() string {
	return "follows"
}

// FollowRepository 关注仓储接口
type FollowRepository interface {
	// Follow 关注用户
	Follow(ctx context.Context, followerID, followeeID uint64) error

	// Unfollow 取消关注
	Unfollow(ctx context.Context, followerID, followeeID uint64) error

	// IsFollowing 检查是否关注
	IsFollowing(ctx context.Context, followerID, followeeID uint64) (bool, error)

	// GetFollowers 获取粉丝列表
	GetFollowers(ctx context.Context, userID uint64, cursor uint64, limit int) ([]uint64, error)

	// GetFollowees 获取关注列表
	GetFollowees(ctx context.Context, userID uint64, cursor uint64, limit int) ([]uint64, error)

	// GetFollowerCount 获取粉丝数
	GetFollowerCount(ctx context.Context, userID uint64) (int64, error)

	// GetFolloweeCount 获取关注数
	GetFolloweeCount(ctx context.Context, userID uint64) (int64, error)

	// GetActiveFollowers 获取活跃粉丝（用于推送 Timeline）
	GetActiveFollowers(ctx context.Context, userID uint64, limit int) ([]uint64, error)
}
