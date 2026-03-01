package domain

import (
	"context"
)

// Message 消息实体
type Message struct {
	ID             uint64 `gorm:"primaryKey;column:id;comment:主键ID"`
	ConversationID string `gorm:"index:idx_conversation_time;type:varchar(64);not null;comment:会话ID (min_uid_max_uid)"`
	SenderID       uint64 `gorm:"index:idx_sender;not null;comment:发送者ID"`
	ReceiverID     uint64 `gorm:"index:idx_receiver;not null;comment:接收者ID"`
	Content        string `gorm:"type:text;comment:消息内容"`
	IsRead         bool   `gorm:"default:false;comment:是否已读"`
	CreatedAt      int64  `gorm:"index:idx_conversation_time;not null;comment:创建时间"`
}

// TableName 指定表名
func (Message) TableName() string {
	return "messages"
}

// Conversation 会话聚合 (非数据库表)
type Conversation struct {
	PeerID        uint64
	LatestMessage *Message
	UnreadCount   int64
}

// MessageRepository 消息仓储接口
type MessageRepository interface {
	// Create 创建消息
	Create(ctx context.Context, message *Message) error

	// GetMessages 获取会话历史消息
	GetMessages(ctx context.Context, conversationID string, cursor uint64, limit int) ([]*Message, error)

	// GetConversations 获取会话列表 (返回最近的一条消息)
	// 注意：基于单表实现的会话列表查询性能较差，生产环境建议使用独立 Conversations 表
	GetConversations(ctx context.Context, userID uint64, limit int, cursor int64) ([]*Conversation, error)

	// MarkAsRead 标记会话已读
	MarkAsRead(ctx context.Context, conversationID string, readerID uint64) error
}
