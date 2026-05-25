package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/gateway/middleware"
)

// BookmarkHandler 书签处理器
type BookmarkHandler struct {
	tweetClient tweetv1.TweetServiceClient
	userClient  userv1.UserServiceClient
}

// NewBookmarkHandler 创建书签处理器
func NewBookmarkHandler(tweetClient tweetv1.TweetServiceClient, userClient userv1.UserServiceClient) *BookmarkHandler {
	return &BookmarkHandler{
		tweetClient: tweetClient,
		userClient:  userClient,
	}
}

// AddBookmark 添加书签
// POST /api/v1/tweets/:id/bookmark
func (h *BookmarkHandler) AddBookmark(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	tweetID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet_id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.BookmarkTweet(ctx, &tweetv1.BookmarkTweetRequest{
		UserId:  userID,
		TweetId: tweetID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": resp.Message})
}

// RemoveBookmark 取消书签
// DELETE /api/v1/tweets/:id/bookmark
func (h *BookmarkHandler) RemoveBookmark(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	tweetID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet_id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.UnbookmarkTweet(ctx, &tweetv1.UnbookmarkTweetRequest{
		UserId:  userID,
		TweetId: tweetID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": resp.Message})
}

// ListBookmarks 获取书签列表
// GET /api/v1/bookmarks
func (h *BookmarkHandler) ListBookmarks(c *gin.Context) {
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

	resp, err := h.tweetClient.GetUserBookmarks(ctx, &tweetv1.GetUserBookmarksRequest{
		UserId: userID,
		Cursor: cursor,
		Limit:  int32(limit),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list bookmarks"})
		return
	}

	// 格式化推文并聚合作者信息
	formattedTweets := make([]gin.H, 0, len(resp.Tweets))
	for _, t := range resp.Tweets {
		tweetData := formatTweet(t)

		// 获取作者信息
		userResp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: t.UserId})
		if err == nil {
			tweetData["user"] = formatUser(userResp.User)
		} else {
			tweetData["user"] = gin.H{"id": strconv.FormatUint(t.UserId, 10), "username": "unknown", "avatar": ""}
		}

		formattedTweets = append(formattedTweets, tweetData)
	}

	c.JSON(http.StatusOK, gin.H{
		"tweets":      formattedTweets,
		"next_cursor": strconv.FormatUint(resp.NextCursor, 10),
		"has_more":    resp.HasMore,
	})
}
