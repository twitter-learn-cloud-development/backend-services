package domain

import "context"

const (
	// OutboxStatusPending 待处理
	OutboxStatusPending = 0
	// OutboxStatusSuccess 成功处理
	OutboxStatusSuccess = 1
	// OutboxStatusFailed 处理失败
	OutboxStatusFailed = 2
)

// OutboxTask 事务发件箱任务实体
type OutboxTask struct {
	ID         uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)"`
	TaskType   string `gorm:"type:varchar(50);not null;column:task_type;comment:任务类型"` // "sync_es"
	Payload    string `gorm:"type:text;not null;column:payload;comment:任务载荷(JSON)"`
	Status     int    `gorm:"default:0;column:status;comment:任务状态(0Pending, 1Success, 2Failed)"`
	Retries    int    `gorm:"default:0;column:retries;comment:重试次数"`
	MaxRetries int    `gorm:"default:5;column:max_retries;comment:最大重试次数"`
	ErrorMsg   string `gorm:"type:text;column:error_msg;comment:错误信息"`
	CreatedAt  int64  `gorm:"not null;column:created_at;comment:创建时间戳"`
	UpdatedAt  int64  `gorm:"not null;column:updated_at;comment:更新时间戳"`
}

// TableName 指定表名
func (OutboxTask) TableName() string {
	return "outbox_tasks"
}

// OutboxRepository 事务发件箱仓储接口
type OutboxRepository interface {
	Create(ctx context.Context, task *OutboxTask) error
	GetPendingTasks(ctx context.Context, limit int) ([]*OutboxTask, error)
	Update(ctx context.Context, task *OutboxTask) error
	Delete(ctx context.Context, id uint64) error
}
