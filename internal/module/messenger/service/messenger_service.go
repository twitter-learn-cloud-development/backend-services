package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"

	messengerv1 "twitter-clone/api/messenger/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/pkg/snowflake"
)

type MessengerService struct {
	messengerv1.UnimplementedMessengerServiceServer
	repo        domain.MessageRepository
	redisClient *redis.Client
}

func NewMessengerService(repo domain.MessageRepository, redisClient *redis.Client) *MessengerService {
	return &MessengerService{
		repo:        repo,
		redisClient: redisClient,
	}
}

// SendMessage 发送消息
func (s *MessengerService) SendMessage(ctx context.Context, req *messengerv1.SendMessageRequest) (*messengerv1.SendMessageResponse, error) {
	// 1. 生成 ID 和 ConversationID
	id := snowflake.GenerateID()
	conversationID := getConversationID(req.SenderId, req.ReceiverId)

	now := time.Now().UnixMilli()
	msg := &domain.Message{
		ID:             id,
		ConversationID: conversationID,
		SenderID:       req.SenderId,
		ReceiverID:     req.ReceiverId,
		Content:        req.Content,
		IsRead:         false,
		CreatedAt:      now,
	}

	// 2. 保存到数据库
	if err := s.repo.Create(ctx, msg); err != nil {
		logger.Error(ctx, "failed to create message", zap.Error(err))
		return nil, err
	}

	// 3. 实时推送 (Redis PubSub)
	// 推送给接收者
	go s.publishMessage(ctx, req.ReceiverId, msg)
	// 推送给发送者 (多端同步)
	go s.publishMessage(ctx, req.SenderId, msg)

	return &messengerv1.SendMessageResponse{
		Message: convertToProto(msg),
	}, nil
}

// GetConversations 获取会话列表
func (s *MessengerService) GetConversations(ctx context.Context, req *messengerv1.GetConversationsRequest) (*messengerv1.GetConversationsResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}
	// cursor 这里我们复用 create_at 时间戳
	// 如果 req.Cursor 是空字符串，则用当前时间
	// 如果不是空，尝试解析为 int64
	// 这里简化处理：req.Cursor 传入 timestamp string

	// TODO: 解析 cursorString 到 int64

	conversations, err := s.repo.GetConversations(ctx, req.UserId, limit, 0) // cursor logic pending
	if err != nil {
		return nil, err
	}

	var protoConvs []*messengerv1.Conversation
	for _, c := range conversations {
		protoConvs = append(protoConvs, &messengerv1.Conversation{
			PeerId:        c.PeerID,
			LatestMessage: convertToProto(c.LatestMessage),
			UnreadCount:   int32(c.UnreadCount),
		})
	}

	return &messengerv1.GetConversationsResponse{
		Conversations: protoConvs,
		HasMore:       false, // TODO: Implement cursor properly
	}, nil
}

// GetMessages 获取消息历史
func (s *MessengerService) GetMessages(ctx context.Context, req *messengerv1.GetMessagesRequest) (*messengerv1.GetMessagesResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}
	// Cursor logic
	// TODO: parse cursor

	conversationID := getConversationID(req.UserId, req.PeerId)
	messages, err := s.repo.GetMessages(ctx, conversationID, 0, limit) // cursor logic pending
	if err != nil {
		return nil, err
	}

	// 标记已读 (异步)
	go s.repo.MarkAsRead(context.Background(), conversationID, req.UserId)

	var protoMsgs []*messengerv1.Message
	for _, m := range messages {
		protoMsgs = append(protoMsgs, convertToProto(m))
	}

	return &messengerv1.GetMessagesResponse{
		Messages: protoMsgs,
		HasMore:  len(messages) >= limit,
	}, nil
}

// Helper functions

func getConversationID(uid1, uid2 uint64) string {
	if uid1 < uid2 {
		return fmt.Sprintf("%d_%d", uid1, uid2)
	}
	return fmt.Sprintf("%d_%d", uid2, uid1)
}

func convertToProto(m *domain.Message) *messengerv1.Message {
	return &messengerv1.Message{
		Id:         m.ID,
		SenderId:   m.SenderID,
		ReceiverId: m.ReceiverID,
		Content:    m.Content,
		CreatedAt:  m.CreatedAt,
		IsRead:     m.IsRead,
	}
}

// RedisMessage 用于 Redis 发布，将 ID 转换为字符串以避免前端精度丢失
type RedisMessage struct {
	Id         string `json:"id"`
	SenderId   string `json:"sender_id"`
	ReceiverId string `json:"receiver_id"`
	Content    string `json:"content"`
	CreatedAt  int64  `json:"created_at"`
	IsRead     bool   `json:"is_read"`
}

func (s *MessengerService) publishMessage(ctx context.Context, userID uint64, msg *domain.Message) {
	channel := fmt.Sprintf("notifications:user:%d", userID)

	// 使用自定义结构体，确保 ID 为字符串
	redisMsg := &RedisMessage{
		Id:         fmt.Sprintf("%d", msg.ID),
		SenderId:   fmt.Sprintf("%d", msg.SenderID),
		ReceiverId: fmt.Sprintf("%d", msg.ReceiverID),
		Content:    msg.Content,
		CreatedAt:  msg.CreatedAt,
		IsRead:     msg.IsRead,
	}

	payload := map[string]interface{}{
		"type": "message",
		"data": redisMsg,
	}
	bytes, _ := json.Marshal(payload)

	// 使用 default context 避免 request context cancel
	err := s.redisClient.Publish(context.Background(), channel, string(bytes)).Err()
	if err != nil {
		logger.Error(ctx, "Failed to publish message to Redis", zap.String("channel", channel), zap.Error(err))
	} else {
		logger.Info(ctx, "📤 Published message to Redis", zap.String("channel", channel))
	}
}
