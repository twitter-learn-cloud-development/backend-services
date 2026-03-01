package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/gateway/middleware"
	bookmarkRepo "twitter-clone/internal/module/bookmark/repository"
)

// BookmarkHandler 书签处理器
type BookmarkHandler struct {
	repo        domain.BookmarkRepository
	tweetClient tweetv1.TweetServiceClient
	userClient  userv1.UserServiceClient
}

// NewBookmarkHandler 创建书签处理器
func NewBookmarkHandler(db *gorm.DB, tweetClient tweetv1.TweetServiceClient, userClient userv1.UserServiceClient) *BookmarkHandler {
	return &BookmarkHandler{
		repo:        bookmarkRepo.NewBookmarkRepository(db),
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 检查是否已收藏
	exists2, err := h.repo.IsBookmarked(ctx, userID, tweetID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check bookmark"})
		return
	}
	if exists2 {
		c.JSON(http.StatusOK, gin.H{"message": "already bookmarked"})
		return
	}

	bookmark := &domain.Bookmark{
		UserID:  userID,
		TweetID: tweetID,
	}

	if err := h.repo.Create(ctx, bookmark); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add bookmark"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "bookmarked"})
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.repo.Delete(ctx, userID, tweetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove bookmark"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "bookmark removed"})
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bookmarks, err := h.repo.List(ctx, userID, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list bookmarks"})
		return
	}

	// 获取推文详情
	tweets := make([]gin.H, 0, len(bookmarks))
	for _, b := range bookmarks {
		resp, err := h.tweetClient.GetTweet(ctx, &tweetv1.GetTweetRequest{
			TweetId:          b.TweetID,
			RequestingUserId: userID,
		})
		if err != nil {
			continue // 推文可能已删除
		}

		tweetData := formatTweet(resp.Tweet)

		// 获取推文作者信息
		userResp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: resp.Tweet.UserId})
		if err == nil {
			tweetData["user"] = formatUser(userResp.User)
		}

		tweetData["bookmarked_at"] = b.CreatedAt
		tweets = append(tweets, tweetData)
	}

	var nextCursor string = "0"
	hasMore := false
	if len(bookmarks) >= limit {
		nextCursor = strconv.FormatUint(bookmarks[len(bookmarks)-1].ID, 10)
		hasMore = true
	}

	c.JSON(http.StatusOK, gin.H{
		"tweets":      tweets,
		"next_cursor": nextCursor,
		"has_more":    hasMore,
	})
}
