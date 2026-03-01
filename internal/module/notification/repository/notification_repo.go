package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"twitter-clone/internal/domain"
	"twitter-clone/pkg/pkg/snowflake"
)

// notificationRepo 通知仓储实现
type notificationRepo struct {
	db *gorm.DB
}

// NewNotificationRepository 创建通知仓储
func NewNotificationRepository(db *gorm.DB) domain.NotificationRepository {
	return &notificationRepo{db: db}
}

// Create 创建通知
func (r *notificationRepo) Create(ctx context.Context, n *domain.Notification) error {
	n.ID = snowflake.GenerateID()
	n.CreatedAt = time.Now().UnixMilli()

	if err := r.db.WithContext(ctx).Create(n).Error; err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}
	return nil
}

// List 获取通知列表 (按时间倒序)
func (r *notificationRepo) List(ctx context.Context, userID uint64, cursor uint64, limit int) ([]*domain.Notification, error) {
	var notifications []*domain.Notification
	query := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id DESC").
		Limit(limit)

	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}

	if err := query.Find(&notifications).Error; err != nil {
		return nil, fmt.Errorf("failed to list notifications: %w", err)
	}
	return notifications, nil
}

// MarkAsRead 标记为已读
func (r *notificationRepo) MarkAsRead(ctx context.Context, notificationIDs []uint64) error {
	if len(notificationIDs) == 0 {
		return nil
	}

	if err := r.db.WithContext(ctx).Model(&domain.Notification{}).
		Where("id IN ?", notificationIDs).
		Update("is_read", true).Error; err != nil {
		return fmt.Errorf("failed to mark notifications as read: %w", err)
	}
	return nil
}

// MarkAllAsRead 标记所有为已读
func (r *notificationRepo) MarkAllAsRead(ctx context.Context, userID uint64) error {
	if err := r.db.WithContext(ctx).Model(&domain.Notification{}).
		Where("user_id = ? AND is_read = ?", userID, false).
		Update("is_read", true).Error; err != nil {
		return fmt.Errorf("failed to mark all notifications as read: %w", err)
	}
	return nil
}

// UnreadCount 获取未读数量
func (r *notificationRepo) UnreadCount(ctx context.Context, userID uint64) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.Notification{}).
		Where("user_id = ? AND is_read = ?", userID, false).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to get unread count: %w", err)
	}
	return count, nil
}
