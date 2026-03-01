package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"twitter-clone/internal/domain"
)

type messageRepo struct {
	db *gorm.DB
}

// NewMessageRepository 创建消息仓储
func NewMessageRepository(db *gorm.DB) domain.MessageRepository {
	return &messageRepo{db: db}
}

func (r *messageRepo) Create(ctx context.Context, message *domain.Message) error {
	return r.db.WithContext(ctx).Create(message).Error
}

func (r *messageRepo) GetMessages(ctx context.Context, conversationID string, cursor uint64, limit int) ([]*domain.Message, error) {
	var messages []*domain.Message
	query := r.db.WithContext(ctx).Where("conversation_id = ?", conversationID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	err := query.Order("id DESC").Limit(limit).Find(&messages).Error
	return messages, err
}

// GetConversations 获取会话列表
// 实现逻辑：
// 1. 查询该用户参与的所有会话ID (conversation_id)
// 2. 对每个会话ID查询最新一条消息
// 3. 统计未读数
// 注意：这是一个简单实现，性能可能随着数据量增加而下降。优化方案是维护 UserConversation 表。
func (r *messageRepo) GetConversations(ctx context.Context, userID uint64, limit int, cursor int64) ([]*domain.Conversation, error) {
	// 1. 找出所有涉及该用户的 conversation_id，按最新时间排序
	// SELECT conversation_id, MAX(created_at) as last_msg_time FROM messages
	// WHERE sender_id = ? OR receiver_id = ?
	// GROUP BY conversation_id
	// HAVING last_msg_time < ?
	// ORDER BY last_msg_time DESC LIMIT ?

	type Result struct {
		ConversationID string
		LastMsgTime    int64
	}
	var results []Result

	// 构建子查询或者直接聚合
	// GORM 比较难写复杂的聚合+HAVING+Limit，用原生 SQL 或分开查
	// 为了简化，这里先全量查出 conversation_id (假设用户会话数不多)，然后在内存排序分页
	// 或者更好的方式：维护一个 separate table。但现在为了 MVP，我们尝试用 SQL 解决。

	// 优化：只查最近的 N 个会话
	// SELECT conversation_id, MAX(created_at) as last_msg_time FROM messages
	// WHERE (sender_id = ? OR receiver_id = ?)
	// GROUP BY conversation_id
	// ORDER BY last_msg_time DESC

	currentCursor := cursor
	if currentCursor == 0 {
		currentCursor = time.Now().UnixMilli()
	}

	err := r.db.WithContext(ctx).Raw(`
		SELECT conversation_id, MAX(created_at) as last_msg_time
		FROM messages
		WHERE sender_id = ? OR receiver_id = ?
		GROUP BY conversation_id
		HAVING last_msg_time < ?
		ORDER BY last_msg_time DESC
		LIMIT ?
	`, userID, userID, currentCursor, limit).Scan(&results).Error

	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return []*domain.Conversation{}, nil
	}

	var conversations []*domain.Conversation

	for _, res := range results {
		// 查询最新消息
		var msg domain.Message
		if err := r.db.Where("conversation_id = ? AND created_at = ?", res.ConversationID, res.LastMsgTime).First(&msg).Error; err != nil {
			continue
		}

		// 计算未读数 (别人发给我的，且未读)
		var unread int64
		r.db.Model(&domain.Message{}).
			Where("conversation_id = ? AND receiver_id = ? AND is_read = ?", res.ConversationID, userID, false).
			Count(&unread)

		// 解析 PeerID
		peerID := msg.SenderID
		if peerID == userID {
			peerID = msg.ReceiverID
		}

		conversations = append(conversations, &domain.Conversation{
			PeerID:        peerID,
			LatestMessage: &msg,
			UnreadCount:   unread,
		})
	}

	return conversations, nil
}

func (r *messageRepo) MarkAsRead(ctx context.Context, conversationID string, readerID uint64) error {
	// 将 conversation 中 receiver_id = readerID 的消息标记为已读
	return r.db.WithContext(ctx).Model(&domain.Message{}).
		Where("conversation_id = ? AND receiver_id = ? AND is_read = ?", conversationID, readerID, false).
		Update("is_read", true).Error
}
