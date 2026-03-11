package delivery

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/module/tweet/service"

	"github.com/gin-gonic/gin"
)

// TweetHandler 推文HTTP处理器
type TweetHandler struct {
	svc *service.TweetService
}

// NewTweetHandler 创建推文处理器
func NewTweetHandler(svc *service.TweetService) *TweetHandler {
	return &TweetHandler{
		svc: svc,
	}
}

func (h *TweetHandler) CreateTweet(c *gin.Context) {
	//1.获取当前用户 ID (由认证中间件设置)
	userID, exist := c.Get("user_id")
	if !exist {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	//2.绑定请求
	var req CreateTweetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	//3.调用 Service
	tweet, err := h.svc.CreateTweet(c.Request.Context(), userID.(uint64), req.Content, req.MediaURLs, 0, nil, 0)
	if err != nil {
		h.handleError(c, err)
		return
	}

	// 4. 返回响应
	c.JSON(http.StatusCreated, h.toTweetResponse(tweet))
}

// DeleteTweet 删除推文
func (h *TweetHandler) DeleteTweet(c *gin.Context) {
	// 1. 获取当前用户 ID
	userID, exists := c.Get("user_id")
	fmt.Println("获取用户的ID:", userID, "exists:", exists)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	fmt.Println("测试1")
	//2. 获取推文 ID
	tweetID, err := h.parseTweetID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet id"})
		return
	}
	fmt.Println("测试2")

	//3. 调用Service
	if err := h.svc.DeleteTweet(c.Request.Context(), tweetID, userID.(uint64)); err != nil {
		h.handleError(c, err)
		return
	}

	//4.返回成功
	c.JSON(http.StatusOK, gin.H{"message": "tweet deleted"})
}

// GetTweet 获取推文详情
func (h *TweetHandler) GetTweet(c *gin.Context) {
	// 1. 获取推文 ID
	tweetID, err := h.parseTweetID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet id"})
		return
	}

	// 2. 调用 Service (获取当前用户用于判断 is_liked)
	var requestingUserID uint64
	if val, exists := c.Get("user_id"); exists {
		requestingUserID = val.(uint64)
	}

	tweet, err := h.svc.GetTweet(c.Request.Context(), tweetID, requestingUserID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	// 3. 返回响应
	c.JSON(http.StatusOK, h.toTweetResponse(tweet))
}

// GetUserTimeline 获取用户时间线
func (h *TweetHandler) GetUserTimeline(c *gin.Context) {
	// 1. 获取用户 ID
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	// 2. 获取分页参数
	cursor, _ := strconv.ParseUint(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	// 3. 调用 Service
	var requestingUserID uint64
	if val, exists := c.Get("user_id"); exists {
		requestingUserID = val.(uint64)
	}

	tweets, nextCursor, hasMore, err := h.svc.GetUserTimeline(c.Request.Context(), userID, cursor, limit, requestingUserID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	// 4. 返回响应
	c.JSON(http.StatusOK, h.toTimelineResponse(tweets, nextCursor, hasMore))
}

// GetFeeds 获取关注流
func (h *TweetHandler) GetFeeds(c *gin.Context) {
	// 1. 获取当前用户 ID
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// 2. 获取分页参数
	cursor, _ := strconv.ParseUint(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	// 3. 调用 Service
	tweets, nextCursor, hasMore, err := h.svc.GetFeeds(c.Request.Context(), userID.(uint64), cursor, limit)
	if err != nil {
		h.handleError(c, err)
		return
	}

	// 4. 返回响应
	c.JSON(http.StatusOK, h.toTimelineResponse(tweets, nextCursor, hasMore))
}

// ===== 辅助方法 =====

// handleError 处理业务错误
func (h *TweetHandler) handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrTweetNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "tweet not found"})
	case errors.Is(err, service.ErrInvalidContent):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid content"})
	case errors.Is(err, service.ErrContentTooLong):
		c.JSON(http.StatusBadRequest, gin.H{"error": "content too long (max 280 characters)"})
	case errors.Is(err, service.ErrUnauthorized):
		c.JSON(http.StatusForbidden, gin.H{"error": "unauthorized"})
	case errors.Is(err, service.ErrInvalidMediaURL):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid media url"})
	case errors.Is(err, service.ErrTooManyMedia):
		c.JSON(http.StatusBadRequest, gin.H{"error": "too many media (max 4)"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

// toTweetResponse 转换为响应 DTO
func (h *TweetHandler) toTweetResponse(tweet *domain.Tweet) TweetResponse {
	return TweetResponse{
		ID:           tweet.ID,
		UserID:       tweet.UserID,
		Content:      tweet.Content,
		MediaURLs:    tweet.MediaURLs,
		Type:         tweet.Type,
		VisibleType:  tweet.VisibleType,
		LikeCount:    tweet.LikeCount,
		CommentCount: tweet.CommentCount,
		ShareCount:   tweet.ShareCount,
		IsLiked:      tweet.IsLiked,
		CreatedAt:    tweet.CreatedAt,
	}
}

// toTimelineResponse 转换为时间线响应
func (h *TweetHandler) toTimelineResponse(tweets []*domain.Tweet, nextCursor uint64, hasMore bool) TimelineResponse {
	tweetResponses := make([]*TweetResponse, len(tweets))
	for i, tweet := range tweets {
		resp := h.toTweetResponse(tweet)
		tweetResponses[i] = &resp
	}

	return TimelineResponse{
		Tweets:     tweetResponses,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}
}

// parseTweetID 解析推文 ID
func (h *TweetHandler) parseTweetID(c *gin.Context) (uint64, error) {
	return strconv.ParseUint(c.Param("id"), 10, 64)
}
