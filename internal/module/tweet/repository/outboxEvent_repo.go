package repository

import (
	"context"
	"errors"
	"strings"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/pkg/database/uow" // 替换为你刚才新建的 uow 路径

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// mysqlOutboxRepo 是 domain.OutboxRepository 接口的具体实现
type mysqlOutboxEventRepo struct {
	db *gorm.DB // 全局的基础 db 连接
}

// NewMysqlOutboxRepo 构造函数，用于依赖注入 (Wire)
func NewMysqlOutboxEventRepo(db *gorm.DB) domain.OutboxEventRepository {
	return &mysqlOutboxEventRepo{db: db}
}

// Create 核心创建逻辑 (支持事务)
func (r *mysqlOutboxEventRepo) Create(ctx context.Context, event *domain.OutboxEvent) error {
	// 🌟 魔法发生在这里：
	// uow.ExtractTx 会检查 ctx 里面有没有开启好的事务句柄。
	// 如果有，它返回事务 tx；如果没有，它就返回普通的 r.db。
	// 这样这个 Repo 既可以参与 Service 层的跨表大事务，也可以独立测试调用。
	db := uow.ExtractTx(ctx, r.db)

	// 执行插入
	return db.WithContext(ctx).Create(event).Error
}

func (r *mysqlOutboxEventRepo) CreateIdempotent(ctx context.Context, event *domain.OutboxEvent) (bool, error) {
	if event == nil || event.DedupKey == nil || strings.TrimSpace(*event.DedupKey) == "" {
		return false, errors.New("outbox dedup key is required")
	}
	db := uow.ExtractTx(ctx, r.db)
	result := db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "dedup_key"}},
			DoNothing: true,
		}).
		Create(event)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// DeleteExpired 用于给将来的定时清理脚本调用
func (r *mysqlOutboxEventRepo) DeleteExpired(ctx context.Context, beforeTimestamp int64) error {
	// 清理操作一般不需要参与业务强事务，所以直接用普通的 r.db
	return r.db.WithContext(ctx).
		Where("created_at < ?", beforeTimestamp).
		Delete(&domain.OutboxEvent{}).Error
}
