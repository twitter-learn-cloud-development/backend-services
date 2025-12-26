package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/gateway/middleware"
)

// TweetHandler 推文处理器
type TweetHandler struct {
	tweetClient tweetv1.TweetServiceClient
}

// NewTweetHandler 创建推文处理器
func NewTweetHandler(tweetClient tweetv1.TweetServiceClient) *TweetHandler {
	return &TweetHandler{
		tweetClient: tweetClient,
	}
}

// CreateTweetRequest 创建推文请求
type CreateTweetRequest struct {
	Content   string   `json:"content" binding:"required,min=1,max=280"`
	MediaURLs []string `json:"media_urls"`
}

// CreateTweet 发推文
func (h *TweetHandler) CreateTweet(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}

	var req CreateTweetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.CreateTweet(ctx, &tweetv1.CreateTweetRequest{
		UserId:    userID,
		Content:   req.Content,
		MediaUrls: req.MediaURLs,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"tweet": formatTweet(resp.Tweet),
	})
}

// GetTweet 获取推文
func (h *TweetHandler) GetTweet(c *gin.Context) {
	tweetIDStr := c.Param("id")
	tweetID, err := strconv.ParseUint(tweetIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid tweet id",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetTweet(ctx, &tweetv1.GetTweetRequest{
		TweetId: tweetID,
	})

	if err != nil {
		// Identify if it's a 404 or System Error
		// Grmpc doesn't return sql.ErrNoRows directly, it returns gRPC status.
		// For now, if the error contains "not found", return 404.
		// Otherwise, return 500 so Circuit Breaker errors are visible.
		// Note: Circuit Breaker usually returns "service overloaded".
		// TODO: Better error code inspection using status package.
		if err.Error() == "tweet not found" { // Assuming service returns this string
			c.JSON(http.StatusNotFound, gin.H{
				"error": "tweet not found",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tweet": formatTweet(resp.Tweet),
	})
}

// DeleteTweet 删除推文
func (h *TweetHandler) DeleteTweet(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}

	tweetIDStr := c.Param("id")
	tweetID, err := strconv.ParseUint(tweetIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid tweet id",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = h.tweetClient.DeleteTweet(ctx, &tweetv1.DeleteTweetRequest{
		TweetId: tweetID,
		UserId:  userID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "tweet deleted successfully",
	})
}

// GetUserTimeline 获取用户时间线
func (h *TweetHandler) GetUserTimeline(c *gin.Context) {
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

	resp, err := h.tweetClient.GetUserTimeline(ctx, &tweetv1.GetUserTimelineRequest{
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

	tweets := make([]gin.H, 0, len(resp.Tweets))
	for _, tweet := range resp.Tweets {
		tweets = append(tweets, formatTweet(tweet))
	}

	c.JSON(http.StatusOK, gin.H{
		"tweets":      tweets,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
	})
}

// GetFeeds 获取关注流
func (h *TweetHandler) GetFeeds(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetFeeds(ctx, &tweetv1.GetFeedsRequest{
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

	tweets := make([]gin.H, 0, len(resp.Tweets))
	for _, tweet := range resp.Tweets {
		tweets = append(tweets, formatTweet(tweet))
	}

	c.JSON(http.StatusOK, gin.H{
		"tweets":      tweets,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
	})
}

// formatTweet 格式化推文
func formatTweet(tweet *tweetv1.Tweet) gin.H {
	return gin.H{
		"id":            tweet.Id,
		"user_id":       tweet.UserId,
		"content":       tweet.Content,
		"media_urls":    tweet.MediaUrls,
		"type":          tweet.Type,
		"visible_type":  tweet.VisibleType,
		"like_count":    tweet.LikeCount,
		"comment_count": tweet.CommentCount,
		"share_count":   tweet.ShareCount,
		"is_liked":      tweet.IsLiked,
		"created_at":    tweet.CreatedAt,
		"updated_at":    tweet.UpdatedAt,
	}
}
