package domain

import (
	"context"
)

// NotificationType 通知类型
type NotificationType string

const (
	NotificationTypeLike    NotificationType = "like"    // 点赞
	NotificationTypeComment NotificationType = "comment" // 评论
	NotificationTypeFollow  NotificationType = "follow"  // 关注
)

// Notification 通知实体
type Notification struct {
	ID        uint64           `gorm:"primaryKey;column:id;comment:主键ID"`
	UserID    uint64           `gorm:"index:idx_user;not null;comment:接收通知的用户ID"`
	ActorID   uint64           `gorm:"not null;comment:触发通知的用户ID"`
	Type      NotificationType `gorm:"type:varchar(20);not null;comment:通知类型"`
	TargetID  uint64           `gorm:"not null;comment:关联目标ID (TweetID 或 UserID)"`
	Content   string           `gorm:"type:text;comment:通知内容 (可选，如评论摘要)"`
	IsRead    bool             `gorm:"default:false;comment:是否已读"`
	CreatedAt int64            `gorm:"not null;comment:创建时间"`

	// 聚合字段 (非数据库字段)
	Actor *User `gorm:"-"` // 触发者信息
}

// TableName 指定表名
func (Notification) TableName() string {
	return "notifications"
}

// NotificationRepository 通知仓储接口
type NotificationRepository interface {
	// Create 创建通知
	Create(ctx context.Context, notification *Notification) error

	// 列表获取 (分页)
	List(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*Notification, error)

	// MarkAsRead 标记为已读
	MarkAsRead(ctx context.Context, notificationIDs []uint64) error

	// MarkAllAsRead 标记所有为已读
	MarkAllAsRead(ctx context.Context, userID uint64) error

	// UnreadCount 获取未读数量
	UnreadCount(ctx context.Context, userID uint64) (int64, error)
}
