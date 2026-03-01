package handler

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	messengerv1 "twitter-clone/api/messenger/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/gateway/middleware"
)

type MessengerHandler struct {
	client     messengerv1.MessengerServiceClient
	userClient userv1.UserServiceClient
}

func NewMessengerHandler(client messengerv1.MessengerServiceClient, userClient userv1.UserServiceClient) *MessengerHandler {
	return &MessengerHandler{
		client:     client,
		userClient: userClient,
	}
}

// SendMessage 发送消息
// POST /api/v1/messages
func (h *MessengerHandler) SendMessage(c *gin.Context) {
	userId, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		ReceiverID string `json:"receiver_id" binding:"required"`
		Content    string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	receiverID, err := strconv.ParseUint(req.ReceiverID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid receiver_id"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.client.SendMessage(ctx, &messengerv1.SendMessageRequest{
		SenderId:   userId,
		ReceiverId: receiverID,
		Content:    req.Content,
	})

	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.InvalidArgument {
			c.JSON(http.StatusBadRequest, gin.H{"error": st.Message()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send message"})
		return
	}

	c.JSON(http.StatusOK, h.formatMessageWithUser(ctx, resp.Message))
}

// GetConversations 获取会话列表
// GET /api/v1/conversations
func (h *MessengerHandler) GetConversations(c *gin.Context) {
	userId, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	cursor := c.DefaultQuery("cursor", "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.client.GetConversations(ctx, &messengerv1.GetConversationsRequest{
		UserId: userId,
		Limit:  int32(limit),
		Cursor: cursor,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get conversations"})
		return
	}

	var conversations []gin.H
	for _, conv := range resp.Conversations {
		// 获取 Peer 用户信息
		peerUser := gin.H{
			"id":       strconv.FormatUint(conv.PeerId, 10),
			"username": "unknown",
			"avatar":   "",
		}
		userResp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: conv.PeerId})
		if err == nil {
			peerUser = gin.H{
				"id":       strconv.FormatUint(userResp.User.Id, 10),
				"username": userResp.User.Username,
				"nickname": userResp.User.Username, // Use Username as Nickname
				"avatar":   userResp.User.Avatar,
			}
		} else {
			log.Printf("Failed to get peer profile %d: %v", conv.PeerId, err)
		}

		conversations = append(conversations, gin.H{
			"peer_id":        strconv.FormatUint(conv.PeerId, 10),
			"peer":           peerUser,
			"latest_message": h.formatMessageWithUser(ctx, conv.LatestMessage),
			"unread_count":   conv.UnreadCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"conversations": conversations,
		"next_cursor":   resp.NextCursor,
		"has_more":      resp.HasMore,
	})
}

// GetMessages 获取消息历史
// GET /api/v1/conversations/:peer_id/messages
func (h *MessengerHandler) GetMessages(c *gin.Context) {
	userId, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	peerIDStr := c.Param("peer_id")
	peerID, err := strconv.ParseUint(peerIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid peer_id"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	cursor := c.DefaultQuery("cursor", "")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.client.GetMessages(ctx, &messengerv1.GetMessagesRequest{
		UserId: userId,
		PeerId: peerID,
		Limit:  int32(limit),
		Cursor: cursor,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get messages"})
		return
	}

	var messages []gin.H
	for _, msg := range resp.Messages {
		messages = append(messages, h.formatMessageWithUser(ctx, msg))
	}

	c.JSON(http.StatusOK, gin.H{
		"messages":    messages,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
	})
}

func formatMessage(msg *messengerv1.Message) gin.H {
	if msg == nil {
		return nil
	}
	return gin.H{
		"id":          strconv.FormatUint(msg.Id, 10),
		"sender_id":   strconv.FormatUint(msg.SenderId, 10),
		"receiver_id": strconv.FormatUint(msg.ReceiverId, 10),
		"content":     msg.Content,
		"created_at":  msg.CreatedAt,
		"is_read":     msg.IsRead,
	}
}

func (h *MessengerHandler) formatMessageWithUser(ctx context.Context, msg *messengerv1.Message) gin.H {
	if msg == nil {
		return nil
	}
	res := formatMessage(msg)

	// 获取发送者信息
	senderUser := gin.H{
		"id":       strconv.FormatUint(msg.SenderId, 10),
		"username": "unknown",
		"avatar":   "",
	}
	userResp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: msg.SenderId})
	if err == nil {
		senderUser = gin.H{
			"id":       strconv.FormatUint(userResp.User.Id, 10),
			"username": userResp.User.Username,
			"nickname": userResp.User.Username, // Use Username as Nickname
			"avatar":   userResp.User.Avatar,
		}
	}
	res["sender"] = senderUser
	return res
}
