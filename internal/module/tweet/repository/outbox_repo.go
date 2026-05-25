package repository

import (
	"context"
	"time"
	"twitter-clone/internal/domain"

	"gorm.io/gorm"
)

type outboxRepo struct {
	db *gorm.DB
}

// NewOutboxRepository 创建发件箱仓储
func NewOutboxRepository(db *gorm.DB) domain.OutboxRepository {
	return &outboxRepo{db: db}
}

// Create 创建 Outbox 任务
func (r *outboxRepo) Create(ctx context.Context, task *domain.OutboxTask) error {
	if task.CreatedAt == 0 {
		task.CreatedAt = time.Now().UnixMilli()
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = task.CreatedAt
	}
	return r.db.WithContext(ctx).Create(task).Error
}

// GetPendingTasks 差分获取待处理或已满足指数退避时间的失败重试任务
func (r *outboxRepo) GetPendingTasks(ctx context.Context, limit int) ([]*domain.OutboxTask, error) {
	var tasks []*domain.OutboxTask
	now := time.Now().UnixMilli()

	// 使用 100% ANSI SQL 兼容的 CASE WHEN 结构处理指数退避延迟时间（1s, 2s, 4s, 8s, 16s...）
	// 避免在不同数据库方言（MySQL / SQLite）中调用位移或乘幂函数导致的兼容性故障
	err := r.db.WithContext(ctx).
		Where("status = ? OR (status = ? AND retries < max_retries AND updated_at + (CASE retries WHEN 0 THEN 1000 WHEN 1 THEN 2000 WHEN 2 THEN 4000 WHEN 3 THEN 8000 WHEN 4 THEN 16000 ELSE 32000 END) <= ?)",
			domain.OutboxStatusPending, domain.OutboxStatusFailed, now).
		Order("id ASC").
		Limit(limit).
		Find(&tasks).Error

	return tasks, err
}

// Update 更新任务状态
func (r *outboxRepo) Update(ctx context.Context, task *domain.OutboxTask) error {
	task.UpdatedAt = time.Now().UnixMilli()
	return r.db.WithContext(ctx).Save(task).Error
}

// Delete 处理成功时物理删除记录，保护数据库表空间
func (r *outboxRepo) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Delete(&domain.OutboxTask{}, id).Error
}
