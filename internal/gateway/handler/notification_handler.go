package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/gateway/middleware"
	notificationRepo "twitter-clone/internal/module/notification/repository"
)

// NotificationHandler 通知处理器
type NotificationHandler struct {
	repo       domain.NotificationRepository
	userClient userv1.UserServiceClient
}

// NewNotificationHandler 创建通知处理器
func NewNotificationHandler(db *gorm.DB, userClient userv1.UserServiceClient) *NotificationHandler {
	return &NotificationHandler{
		repo:       notificationRepo.NewNotificationRepository(db),
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	notifications, err := h.repo.List(ctx, userID, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get notifications"})
		return
	}

	// 批量查询用户信息 (actor)
	actorIDs := make(map[uint64]bool)
	for _, n := range notifications {
		actorIDs[n.ActorID] = true
	}

	actorMap := make(map[uint64]gin.H)
	for uid := range actorIDs {
		resp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: uid})
		if err != nil {
			actorMap[uid] = gin.H{"id": strconv.FormatUint(uid, 10), "username": "unknown", "avatar": ""}
			continue
		}
		actorMap[uid] = formatUser(resp.User)
	}

	// 格式化结果
	result := make([]gin.H, 0, len(notifications))
	for _, n := range notifications {
		result = append(result, gin.H{
			"id":         strconv.FormatUint(n.ID, 10),
			"type":       n.Type,
			"target_id":  strconv.FormatUint(n.TargetID, 10),
			"content":    n.Content,
			"is_read":    n.IsRead,
			"created_at": n.CreatedAt,
			"actor":      actorMap[n.ActorID],
		})
	}

	// 计算 next_cursor
	var nextCursor string = "0"
	hasMore := false
	if len(notifications) >= limit {
		nextCursor = strconv.FormatUint(notifications[len(notifications)-1].ID, 10)
		hasMore = true
	}

	c.JSON(http.StatusOK, gin.H{
		"notifications": result,
		"next_cursor":   nextCursor,
		"has_more":      hasMore,
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
	_ = userID // 可以加验证通知归属

	var req struct {
		IDs []uint64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.repo.MarkAsRead(ctx, req.IDs); err != nil {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := h.repo.UnreadCount(ctx, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get unread count"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

// MarkAllAsRead 标记所有通知为已读
// PUT /api/v1/notifications/read-all
func (h *NotificationHandler) MarkAllAsRead(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.repo.MarkAllAsRead(ctx, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to mark all as read"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
