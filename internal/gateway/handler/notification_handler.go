package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	notificationv1 "twitter-clone/api/notification/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/gateway/middleware"
)

// NotificationHandler 通知处理器
type NotificationHandler struct {
	client     notificationv1.NotificationServiceClient
	userClient userv1.UserServiceClient
}

// NewNotificationHandler 创建通知处理器
func NewNotificationHandler(client notificationv1.NotificationServiceClient, userClient userv1.UserServiceClient) *NotificationHandler {
	return &NotificationHandler{
		client:     client,
		userClient: userClient,
	}
}

// GetNotifications 获取通知列表
// GET /api/v1/notifications
func (h *NotificationHandler) GetNotifications(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	cursor, _ := strconv.ParseUint(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.client.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{
		UserId: userID,
		Cursor: cursor,
		Limit:  int32(limit),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get notifications"})
		return
	}

	// 批量查询用户信息 (actor)
	actorIDs := make(map[uint64]bool)
	for _, n := range resp.Notifications {
		actorIDs[n.ActorId] = true
	}

	actorMap := make(map[uint64]gin.H)
	for uid := range actorIDs {
		profileResp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: uid})
		if err != nil {
			actorMap[uid] = gin.H{"id": strconv.FormatUint(uid, 10), "username": "unknown", "avatar": ""}
			continue
		}
		actorMap[uid] = formatUser(profileResp.User)
	}

	// 格式化结果
	result := make([]gin.H, 0, len(resp.Notifications))
	for _, n := range resp.Notifications {
		result = append(result, gin.H{
			"id":         strconv.FormatUint(n.Id, 10),
			"type":       n.Type,
			"target_id":  strconv.FormatUint(n.TargetId, 10),
			"content":    n.Content,
			"is_read":    n.IsRead,
			"created_at": n.CreatedAt,
			"actor":      actorMap[n.ActorId],
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"notifications": result,
		"next_cursor":   strconv.FormatUint(resp.NextCursor, 10),
		"has_more":      resp.HasMore,
	})
}

// MarkAsRead 标记通知为已读
// PUT /api/v1/notifications/read
func (h *NotificationHandler) MarkAsRead(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		IDs []uint64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.client.MarkAsRead(ctx, &notificationv1.MarkAsReadRequest{
		UserId: userID,
		Ids:    req.IDs,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark as read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// GetUnreadCount 获取未读通知数量
// GET /api/v1/notifications/unread-count
func (h *NotificationHandler) GetUnreadCount(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.client.GetUnreadCount(ctx, &notificationv1.GetUnreadCountRequest{
		UserId: userID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get unread count"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": resp.Count})
}

// MarkAllAsRead 标记所有通知为已读
// PUT /api/v1/notifications/read-all
func (h *NotificationHandler) MarkAllAsRead(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err := h.client.MarkAllAsRead(ctx, &notificationv1.MarkAllAsReadRequest{
		UserId: userID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark all as read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
