package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	messengerv1 "twitter-clone/api/messenger/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/pkg/snowflake"
)

type MessengerService struct {
	messengerv1.UnimplementedMessengerServiceServer
	repo        domain.MessageRepository
	redisClient *redis.Client
	generateID  func() (uint64, error)
}

func NewMessengerService(repo domain.MessageRepository, redisClient *redis.Client) *MessengerService {
	return &MessengerService{
		repo:        repo,
		redisClient: redisClient,
		generateID:  snowflake.GenerateID,
	}
}

// SendMessage 发送消息
func (s *MessengerService) SendMessage(ctx context.Context, req *messengerv1.SendMessageRequest) (*messengerv1.SendMessageResponse, error) {
	if req == nil || req.SenderId == 0 || req.ReceiverId == 0 || strings.TrimSpace(req.Content) == "" || len(req.Content) > 16384 {
		return nil, status.Error(codes.InvalidArgument, "invalid message; maximum content is 16384 UTF-8 bytes")
	}
	// 1. 生成 ID 和 ConversationID
	id, err := s.generateID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate ID: %w", err)
	}
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
	pushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	s.publishMessage(pushCtx, req.ReceiverId, msg)
	// 推送给发送者 (多端同步)
	if req.SenderId != req.ReceiverId {
		s.publishMessage(pushCtx, req.SenderId, msg)
	}

	return &messengerv1.SendMessageResponse{
		Message: convertToProto(msg),
	}, nil
}

// GetConversations 获取会话列表
func (s *MessengerService) GetConversations(ctx context.Context, req *messengerv1.GetConversationsRequest) (*messengerv1.GetConversationsResponse, error) {
	if req == nil || req.UserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	snapshot, before, err := parseConversationCursor(req.Cursor, req.UserId)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	pager, ok := s.repo.(domain.ConversationPageRepository)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "conversation pagination is unavailable")
	}
	limit := messengerPageLimit(req.Limit)
	page, err := pager.ListConversationPage(ctx, req.UserId, snapshot, before, limit+1)
	if err != nil {
		return nil, messengerReadError(ctx, err)
	}
	if err := validateConversationPage(page, req.UserId, snapshot, before, limit+1); err != nil {
		return nil, err
	}
	conversations := page.Conversations
	hasMore := len(conversations) > limit
	if hasMore {
		conversations = conversations[:limit]
	}
	protoConvs := make([]*messengerv1.Conversation, 0, len(conversations))
	for _, c := range conversations {
		unread := c.UnreadCount
		if unread > 1<<31-1 {
			unread = 1<<31 - 1
		}
		protoConvs = append(protoConvs, &messengerv1.Conversation{
			PeerId:        c.PeerID,
			LatestMessage: convertToProto(c.LatestMessage),
			UnreadCount:   int32(unread),
		})
	}
	var next string
	if hasMore {
		next = encodeConversationCursor(req.UserId, page.SnapshotID, conversations[len(conversations)-1].LatestMessage.ID)
	}

	return &messengerv1.GetConversationsResponse{
		Conversations: protoConvs,
		HasMore:       hasMore,
		NextCursor:    next,
	}, nil
}

// GetMessages 获取消息历史
func (s *MessengerService) GetMessages(ctx context.Context, req *messengerv1.GetMessagesRequest) (*messengerv1.GetMessagesResponse, error) {
	if req == nil || req.UserId == 0 || req.PeerId == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id and peer_id are required")
	}
	cursor, err := parseMessageCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	reader, ok := s.repo.(domain.MessagePageReadRepository)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, "message read pagination is unavailable")
	}
	limit := messengerPageLimit(req.Limit)

	conversationID := getConversationID(req.UserId, req.PeerId)
	messages, err := s.repo.GetMessages(ctx, conversationID, cursor, limit+1)
	if err != nil {
		return nil, messengerReadError(ctx, err)
	}
	if err := validateMessagePage(messages, req.UserId, req.PeerId, cursor, limit+1); err != nil {
		return nil, err
	}
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}
	ids := make([]uint64, 0, len(messages))
	for _, m := range messages {
		if m.ReceiverID == req.UserId && !m.IsRead {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) > 0 {
		if err := reader.MarkMessagePageRead(ctx, conversationID, req.UserId, ids); err != nil {
			return nil, messengerReadError(ctx, err)
		}
	}
	protoMsgs := make([]*messengerv1.Message, 0, len(messages))
	for _, m := range messages {
		message := convertToProto(m)
		if m.ReceiverID == req.UserId {
			message.IsRead = true
		}
		protoMsgs = append(protoMsgs, message)
	}
	var next string
	if hasMore {
		next = strconv.FormatUint(messages[len(messages)-1].ID, 10)
	}

	return &messengerv1.GetMessagesResponse{
		Messages:   protoMsgs,
		HasMore:    hasMore,
		NextCursor: next,
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
	bytes, err := json.Marshal(payload)
	if err != nil {
		messagePushResults.WithLabelValues("encode_error").Inc()
		return
	}

	// 使用 default context 避免 request context cancel
	if s.redisClient == nil {
		messagePushResults.WithLabelValues("unavailable").Inc()
		return
	}
	count, err := s.redisClient.Publish(ctx, channel, string(bytes)).Result()
	if err != nil {
		messagePushResults.WithLabelValues("error").Inc()
		logger.Warn(ctx, "message stored; realtime hint unavailable")
	} else {
		outcome := "published"
		if count == 0 {
			outcome = "no_subscriber"
		}
		messagePushResults.WithLabelValues(outcome).Inc()
	}
}
