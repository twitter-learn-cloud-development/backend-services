package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
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
	if err != nil || userID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid user id",
		})
		return
	}

	cursor, limit, ok := parseFollowPageQuery(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.followClient.GetFollowers(ctx, &followv1.GetFollowersRequest{
		UserId: userID,
		Cursor: cursor,
		Limit:  int32(limit),
	})

	if err != nil {
		writeFollowPageError(c, err)
		return
	}

	strFollowerIDs := make([]string, 0, len(resp.FollowerIds))
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
	if err != nil || userID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid user id",
		})
		return
	}

	cursor, limit, ok := parseFollowPageQuery(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.followClient.GetFollowees(ctx, &followv1.GetFolloweesRequest{
		UserId: userID,
		Cursor: cursor,
		Limit:  int32(limit),
	})

	if err != nil {
		writeFollowPageError(c, err)
		return
	}

	strFolloweeIDs := make([]string, 0, len(resp.FolloweeIds))
	for _, id := range resp.FolloweeIds {
		strFolloweeIDs = append(strFolloweeIDs, strconv.FormatUint(id, 10))
	}

	c.JSON(http.StatusOK, gin.H{
		"followee_ids": strFolloweeIDs,
		"next_cursor":  strconv.FormatUint(resp.NextCursor, 10),
		"has_more":     resp.HasMore,
	})
}

func parseFollowPageQuery(c *gin.Context) (uint64, int64, bool) {
	query := c.Request.URL.Query()
	cursorText := "0"
	if values, present := query["cursor"]; present {
		if len(values) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor"})
			return 0, 0, false
		}
		cursorText = values[0]
	}
	cursor, err := strconv.ParseUint(cursorText, 10, 64)
	if err != nil || strconv.FormatUint(cursor, 10) != cursorText {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor"})
		return 0, 0, false
	}
	limitText := "20"
	if values, present := query["limit"]; present {
		if len(values) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return 0, 0, false
		}
		limitText = values[0]
	}
	limit, err := strconv.ParseInt(limitText, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
		return 0, 0, false
	}
	return cursor, limit, true
}

func writeFollowPageError(c *gin.Context, err error) {
	code := http.StatusInternalServerError
	switch status.Code(err) {
	case codes.InvalidArgument:
		code = http.StatusBadRequest
	case codes.FailedPrecondition, codes.Unavailable:
		code = http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		code = http.StatusGatewayTimeout
	}
	c.JSON(code, gin.H{"error": "failed to read follow relation page"})
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
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
