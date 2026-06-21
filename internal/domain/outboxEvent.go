package domain

import "context"

// OutboxEvent 领域事件发件箱实体 (纯粹的事件记录，无状态)
type OutboxEvent struct {
	ID        uint64 `gorm:"primaryKey;column:id;comment:主键ID (Snowflake)"`
	EventType string `gorm:"type:varchar(100);not null;column:event_type;comment:事件类型(例如: TWEET_CREATED)"`
	Payload   string `gorm:"type:json;not null;column:payload;comment:事件全景载荷(JSON)"` // 建议使用 json 类型
	CreatedAt int64  `gorm:"index;not null;column:created_at;comment:创建时间戳(用于定期清理)"`
}

func (OutboxEvent) TableName() string {
	return "outbox_events"
}

// OutboxRepository 事务发件箱仓储接口
type OutboxEventRepository interface {
	// 只有一个核心方法：创建！在 UoW 事务中调用
	Create(ctx context.Context, event *OutboxEvent) error

	// 提供一个供后台清理脚本调用的方法即可
	DeleteExpired(ctx context.Context, beforeTimestamp int64) error
}

// OutboxTweetCreatedPayload 发推事件载荷
type OutboxTweetCreatedPayload struct {
	TweetID     uint64   `json:"tweet_id"`
	AuthorID    uint64   `json:"author_id"`
	ParentID    uint64   `json:"parent_id"`
	Content     string   `json:"content"`
	MediaURLs   []string `json:"media_urls"`
	Type        int      `json:"type"`
	VisibleType int      `json:"visible_type"`
	CreatedAt   int64    `json:"created_at"`
	HasPoll     bool     `json:"has_poll"`
	Poll        *Poll    `json:"poll,omitempty"`
}

