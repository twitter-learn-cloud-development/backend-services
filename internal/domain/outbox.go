package domain

import "context"

const (
	// OutboxStatusPending 待处理
	OutboxStatusPending = 0
	// OutboxStatusSuccess 成功处理
	OutboxStatusSuccess = 1
	// OutboxStatusFailed 处理失败
	OutboxStatusFailed = 2
	// OutboxStatusProcessing 已由一个带租约的 Worker Attempt 领取
	OutboxStatusProcessing = 3
)

// OutboxTask 事务发件箱任务实体
type OutboxTask struct {
	DedupKey   *string `gorm:"type:varchar(191);column:dedup_key;uniqueIndex:uk_outbox_tasks_dedup_key;comment:bounded idempotency key"`
	ID         uint64  `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)"`
	TaskType   string  `gorm:"type:varchar(50);not null;column:task_type;comment:任务类型"` // "sync_es"
	Payload    string  `gorm:"type:text;not null;column:payload;comment:任务载荷(JSON)"`
	Status     int     `gorm:"default:0;not null;column:status;index:idx_outbox_claim_retry,priority:1;index:idx_outbox_lease_expiry,priority:1;comment:任务状态(0Pending, 1Success, 2Failed, 3Processing)"`
	Retries    int     `gorm:"default:0;not null;column:retries;comment:已领取执行次数"`
	MaxRetries int     `gorm:"default:5;column:max_retries;comment:最大重试次数"`
	ErrorMsg   string  `gorm:"type:text;column:error_msg;comment:错误信息"`
	CreatedAt  int64   `gorm:"not null;column:created_at;comment:创建时间戳"`
	UpdatedAt  int64   `gorm:"not null;column:updated_at;index:idx_outbox_claim_retry,priority:2;comment:更新时间戳"`
	LeaseOwner string  `gorm:"type:varchar(191);not null;default:'';column:lease_owner;comment:当前租约持有者"`
	LeaseToken string  `gorm:"type:varchar(64);not null;default:'';column:lease_token;comment:当前 Attempt 防护令牌"`
	LeaseUntil int64   `gorm:"not null;default:0;column:lease_until;index:idx_outbox_lease_expiry,priority:2;comment:租约到期时间戳"`
}

// OutboxClaimRequest 描述一次有界批量领取。LeaseToken 在每次领取时必须重新生成。
type OutboxClaimRequest struct {
	LeaseOwner          string
	LeaseToken          string
	ClaimedAtUnixMilli  int64
	LeaseUntilUnixMilli int64
	Limit               int
}

// OutboxClaimCompletion 只允许仍持有有效租约的 Attempt 提交成功结果。
type OutboxClaimCompletion struct {
	TaskID               uint64
	LeaseOwner           string
	LeaseToken           string
	CompletedAtUnixMilli int64
}

// OutboxClaimFailure 释放当前租约；Terminal 会耗尽该任务的剩余执行次数。
type OutboxClaimFailure struct {
	TaskID            uint64
	LeaseOwner        string
	LeaseToken        string
	FailedAtUnixMilli int64
	ErrorMsg          string
	Terminal          bool
}

// OutboxLeaseRecovery 汇总一次过期租约恢复，不暴露任务 ID 作为指标标签。
type OutboxLeaseRecovery struct {
	Retryable int64
	Exhausted int64
}

// TableName 指定表名
func (OutboxTask) TableName() string {
	return "outbox_tasks"
}

// OutboxRepository 事务发件箱仓储接口
type OutboxRepository interface {
	Create(ctx context.Context, task *OutboxTask) error
	CreateIdempotent(ctx context.Context, task *OutboxTask) (bool, error)
	Claim(ctx context.Context, request OutboxClaimRequest) ([]*OutboxTask, error)
	CompleteClaim(ctx context.Context, completion OutboxClaimCompletion) (bool, error)
	FailClaim(ctx context.Context, failure OutboxClaimFailure) (bool, error)
	RecoverExpiredClaims(ctx context.Context, nowUnixMilli int64, limit int) (OutboxLeaseRecovery, error)
	Delete(ctx context.Context, id uint64) error
	DeleteCompletedBefore(ctx context.Context, beforeUnixMilli int64, limit int) (int64, error)
}
