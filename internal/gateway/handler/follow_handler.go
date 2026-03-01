package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	followv1 "twitter-clone/api/follow/v1"
	"twitter-clone/internal/gateway/middleware"
)

// FollowHandler 关注处理器
type FollowHandler struct {
	followClient followv1.FollowServiceClient
}

// NewFollowHandler 创建关注处理器
func NewFollowHandler(followClient followv1.FollowServiceClient) *FollowHandler {
	return &FollowHandler{
		followClient: followClient,
	}
}

// FollowRequest 关注请求
// 使用 string 避免 JS 大整数精度丢失
type FollowRequest struct {
	FolloweeID string `json:"followee_id" binding:"required"`
}

// Follow 关注用户
func (h *FollowHandler) Follow(c *gin.Context) {
	followerID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}

	var req FollowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	followeeID, err := strconv.ParseUint(req.FolloweeID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid followee_id",
		})
		return
	}

	// 不能关注自己
	if followerID == followeeID {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "cannot follow yourself",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.followClient.Follow(ctx, &followv1.FollowRequest{
		FollowerId: followerID,
		FolloweeId: followeeID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": resp.Message,
	})
}

// Unfollow 取消关注
func (h *FollowHandler) Unfollow(c *gin.Context) {
	followerID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}

	followeeIDStr := c.Param("id")
	followeeID, err := strconv.ParseUint(followeeIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid user id",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.followClient.Unfollow(ctx, &followv1.UnfollowRequest{
		FollowerId: followerID,
		FolloweeId: followeeID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": resp.Message,
	})
}

// IsFollowing 检查是否关注
func (h *FollowHandler) IsFollowing(c *gin.Context) {
	followerID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}

	followeeIDStr := c.Param("id")
	followeeID, err := strconv.ParseUint(followeeIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid user id",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.followClient.IsFollowing(ctx, &followv1.IsFollowingRequest{
		FollowerId: followerID,
		FolloweeId: followeeID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"is_following": resp.IsFollowing,
	})
}

// GetFollowers 获取粉丝列表
func (h *FollowHandler) GetFollowers(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid user id",
		})
		return
	}

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.followClient.GetFollowers(ctx, &followv1.GetFollowersRequest{
		UserId: userID,
		Cursor: cursor,
		Limit:  int32(limit),
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	var strFollowerIDs []string
	for _, id := range resp.FollowerIds {
		strFollowerIDs = append(strFollowerIDs, strconv.FormatUint(id, 10))
	}

	c.JSON(http.StatusOK, gin.H{
		"follower_ids": strFollowerIDs,
		"next_cursor":  strconv.FormatUint(resp.NextCursor, 10),
		"has_more":     resp.HasMore,
	})
}

// GetFollowees 获取关注列表
func (h *FollowHandler) GetFollowees(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid user id",
		})
		return
	}

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.followClient.GetFollowees(ctx, &followv1.GetFolloweesRequest{
		UserId: userID,
		Cursor: cursor,
		Limit:  int32(limit),
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	var strFolloweeIDs []string
	for _, id := range resp.FolloweeIds {
		strFolloweeIDs = append(strFolloweeIDs, strconv.FormatUint(id, 10))
	}

	c.JSON(http.StatusOK, gin.H{
		"followee_ids": strFolloweeIDs,
		"next_cursor":  strconv.FormatUint(resp.NextCursor, 10),
		"has_more":     resp.HasMore,
	})
}

// GetFollowStats 获取关注统计
func (h *FollowHandler) GetFollowStats(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid user id",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.followClient.GetFollowStats(ctx, &followv1.GetFollowStatsRequest{
		UserId: userID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"follower_count": resp.FollowerCount,
		"followee_count": resp.FolloweeCount,
	})
}
