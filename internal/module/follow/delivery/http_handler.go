package delivery

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"twitter-clone/internal/module/follow/service"
)

// FollowHandler 关注 HTTP 处理器
type FollowHandler struct {
	svc *service.FollowService
}

// NewFollowHandler 创建关注处理器
func NewFollowHandler(svc *service.FollowService) *FollowHandler {
	return &FollowHandler{svc: svc}
}

// Follow 关注用户
func (h *FollowHandler) Follow(c *gin.Context) {
	// 1. 获取当前用户 ID
	followerID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// 2. 获取被关注者 ID
	followeeID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	// 3. 调用 Service
	if err := h.svc.Follow(c.Request.Context(), followerID.(uint64), followeeID); err != nil {
		h.handleError(c, err)
		return
	}

	// 4. 返回成功
	c.JSON(http.StatusOK, gin.H{"message": "followed successfully"})
}

// Unfollow 取消关注
func (h *FollowHandler) Unfollow(c *gin.Context) {
	// 1. 获取当前用户 ID
	followerID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// 2. 获取被取关者 ID
	followeeID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	// 3. 调用 Service
	if err := h.svc.Unfollow(c.Request.Context(), followerID.(uint64), followeeID); err != nil {
		h.handleError(c, err)
		return
	}

	// 4. 返回成功
	c.JSON(http.StatusOK, gin.H{"message": "unfollowed successfully"})
}

// IsFollowing 检查是否关注
func (h *FollowHandler) IsFollowing(c *gin.Context) {
	// 1. 获取当前用户 ID
	followerID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// 2. 获取目标用户 ID
	followeeID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	// 3. 调用 Service
	isFollowing, err := h.svc.IsFollowing(c.Request.Context(), followerID.(uint64), followeeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// 4. 返回结果
	c.JSON(http.StatusOK, gin.H{"is_following": isFollowing})
}

// GetFollowers 获取粉丝列表
func (h *FollowHandler) GetFollowers(c *gin.Context) {
	// 1. 获取用户 ID
	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	// 2. 获取分页参数
	cursor, _ := strconv.ParseUint(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	// 3. 调用 Service
	followerIDs, nextCursor, hasMore, err := h.svc.GetFollowers(c.Request.Context(), userID, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// 4. 返回结果
	c.JSON(http.StatusOK, gin.H{
		"follower_ids": followerIDs,
		"next_cursor":  nextCursor,
		"has_more":     hasMore,
	})
}

// GetFollowees 获取关注列表
func (h *FollowHandler) GetFollowees(c *gin.Context) {
	// 1. 获取用户 ID
	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	// 2. 获取分页参数
	cursor, _ := strconv.ParseUint(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	// 3. 调用 Service
	followeeIDs, nextCursor, hasMore, err := h.svc.GetFollowees(c.Request.Context(), userID, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// 4. 返回结果
	c.JSON(http.StatusOK, gin.H{
		"followee_ids": followeeIDs,
		"next_cursor":  nextCursor,
		"has_more":     hasMore,
	})
}

// GetFollowStats 获取关注统计
func (h *FollowHandler) GetFollowStats(c *gin.Context) {
	// 1. 获取用户 ID
	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	// 2. 调用 Service
	followerCount, followeeCount, err := h.svc.GetFollowStats(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// 3. 返回结果
	c.JSON(http.StatusOK, gin.H{
		"follower_count": followerCount,
		"followee_count": followeeCount,
	})
}

// handleError 处理业务错误
func (h *FollowHandler) handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCannotFollowSelf):
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot follow yourself"})
	case errors.Is(err, service.ErrAlreadyFollowing):
		c.JSON(http.StatusConflict, gin.H{"error": "already following"})
	case errors.Is(err, service.ErrNotFollowing):
		c.JSON(http.StatusNotFound, gin.H{"error": "not following"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
