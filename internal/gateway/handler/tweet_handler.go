package handler

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/gateway/middleware"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sentinel "github.com/alibaba/sentinel-golang/api"
)

// TweetHandler 推文处理器
type TweetHandler struct {
	tweetClient tweetv1.TweetServiceClient
	userClient  userv1.UserServiceClient
}

// NewTweetHandler 创建推文处理器
func NewTweetHandler(tweetClient tweetv1.TweetServiceClient, userClient userv1.UserServiceClient) *TweetHandler {
	return &TweetHandler{
		tweetClient: tweetClient,
		userClient:  userClient,
	}
}

// CreateTweetRequest 创建推文请求
type CreateTweetRequest struct {
	Content             string   `json:"content" binding:"required,min=1,max=280"`
	MediaURLs           []string `json:"media_urls"`
	ParentID            string   `json:"parent_id"` // 可选，回复的推文ID (接收字符串以避免精度丢失)
	PollOptions         []string `json:"poll_options"`
	PollDurationMinutes int32    `json:"poll_duration_minutes"`
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	var parentID uint64
	if req.ParentID != "" {
		var err error
		parentID, err = strconv.ParseUint(req.ParentID, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parent_id"})
			return
		}
	}

	log.Printf("CreateTweet: userID=%d, parentID=%d, content=%s", userID, parentID, req.Content)

	resp, err := h.tweetClient.CreateTweet(ctx, &tweetv1.CreateTweetRequest{
		UserId:              userID,
		Content:             req.Content,
		MediaUrls:           req.MediaURLs,
		ParentId:            parentID,
		PollOptions:         req.PollOptions,
		PollDurationMinutes: req.PollDurationMinutes,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	// 获取用户信息以返回完整数据
	userResp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: userID})
	var userInfo gin.H
	if err != nil {
		log.Printf("Failed to get user profile for created tweet: %v", err)
		userInfo = gin.H{"id": strconv.FormatUint(userID, 10), "username": "unknown", "avatar": ""}
	} else {
		userInfo = formatUser(userResp.User)
	}

	c.JSON(http.StatusCreated, gin.H{
		"tweet": formatTweetWithUser(resp.Tweet, userInfo),
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// 获取当前登录用户 ID (可选)
	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	resp, err := h.tweetClient.GetTweet(ctx, &tweetv1.GetTweetRequest{
		TweetId:          tweetID,
		RequestingUserId: requestingUserID,
	})

	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == codes.NotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "tweet not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	enrichedTweets := h.enrichTweetsWithUserInfo(ctx, []*tweetv1.Tweet{resp.Tweet}, requestingUserID)
	if len(enrichedTweets) > 0 {
		c.JSON(http.StatusOK, gin.H{"tweet": enrichedTweets[0]})
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process tweet data"})
	}
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
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

	c.JSON(http.StatusOK, gin.H{"message": "tweet deleted successfully"})
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	// 获取当前登录用户 ID (可选)
	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	resp, err := h.tweetClient.GetUserTimeline(ctx, &tweetv1.GetUserTimelineRequest{
		UserId:           userID,
		Cursor:           cursor,
		Limit:            int32(limit),
		RequestingUserId: requestingUserID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	tweets := h.enrichTweetsWithUserInfo(ctx, resp.Tweets, requestingUserID)

	c.JSON(http.StatusOK, gin.H{
		"tweets":      tweets,
		"next_cursor": strconv.FormatUint(resp.NextCursor, 10),
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

	// 🚦 Sentinel Entry 保护，提供自愈动态熔断拦截
	entry, blockErr := sentinel.Entry("GET:/api/v1/feeds")
	if blockErr != nil {
		log.Printf("🔥 [Sentinel CB] GET:/api/v1/feeds blocked: %v", blockErr)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "service temporary unavailable (circuit broken)",
		})
		return
	}
	defer entry.Exit()

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetFeeds(ctx, &tweetv1.GetFeedsRequest{
		UserId: userID,
		Cursor: cursor,
		Limit:  int32(limit),
	})

	if err != nil {
		sentinel.TraceError(entry, err) // 🎯 追踪错误以使得 Sentinel 统计错误比率触发自适应熔断
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	tweets := h.enrichTweetsWithUserInfo(ctx, resp.Tweets, userID)

	c.JSON(http.StatusOK, gin.H{
		"tweets":      tweets,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
	})
}

// LikeTweet 点赞推文
func (h *TweetHandler) LikeTweet(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	tweetIDStr := c.Param("id")
	tweetID, err := strconv.ParseUint(tweetIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.LikeTweet(ctx, &tweetv1.LikeTweetRequest{
		UserId:  userID,
		TweetId: tweetID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"like_count": resp.LikeCount,
		"is_liked":   true,
	})
}

// UnlikeTweet 取消点赞
func (h *TweetHandler) UnlikeTweet(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	tweetIDStr := c.Param("id")
	tweetID, err := strconv.ParseUint(tweetIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.UnlikeTweet(ctx, &tweetv1.UnlikeTweetRequest{
		UserId:  userID,
		TweetId: tweetID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"like_count": resp.LikeCount,
		"is_liked":   false,
	})
}

// VotePollRequest 投票请求
type VotePollRequest struct {
	PollID   string `json:"poll_id" binding:"required"`
	OptionID string `json:"option_id" binding:"required"`
}

// VotePoll 投票
func (h *TweetHandler) VotePoll(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req VotePollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	pollID, _ := strconv.ParseUint(req.PollID, 10, 64)
	optionID, _ := strconv.ParseUint(req.OptionID, 10, 64)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.VotePoll(ctx, &tweetv1.VotePollRequest{
		UserId:   userID,
		PollId:   pollID,
		OptionId: optionID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"poll": formatPoll(resp.Poll),
	})
}

// CreateCommentRequest 创建评论请求
type CreateCommentRequest struct {
	Content  string `json:"content" binding:"required,min=1,max=280"`
	ParentID string `json:"parent_id"` // 可选
}

// CreateComment 发布评论
func (h *TweetHandler) CreateComment(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	tweetIDStr := c.Param("id")
	tweetID, err := strconv.ParseUint(tweetIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet id"})
		return
	}

	var req CreateCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var parentID uint64
	if req.ParentID != "" {
		pid, err := strconv.ParseUint(req.ParentID, 10, 64)
		if err == nil {
			parentID = pid
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.CreateComment(ctx, &tweetv1.CreateCommentRequest{
		UserId:   userID,
		TweetId:  tweetID,
		Content:  req.Content,
		ParentId: parentID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Enrich with User info
	userResp, err := h.userClient.GetBatchUsers(ctx, &userv1.GetBatchUsersRequest{
		UserIds: []uint64{resp.Comment.UserId},
	})
	if err == nil && len(userResp.Users) > 0 {
		u := userResp.Users[0]
		resp.Comment.Username = u.Username
		resp.Comment.AvatarUrl = u.Avatar
	}

	c.JSON(http.StatusCreated, gin.H{
		"comment": formatComment(resp.Comment),
	})
}

// DeleteComment 删除评论
func (h *TweetHandler) DeleteComment(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	commentIDStr := c.Param("id")
	commentID, err := strconv.ParseUint(commentIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid comment id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	_, err = h.tweetClient.DeleteComment(ctx, &tweetv1.DeleteCommentRequest{
		CommentId: commentID,
		UserId:    userID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "comment deleted successfully"})
}

// GetTweetComments 获取推文评论
func (h *TweetHandler) GetTweetComments(c *gin.Context) {
	tweetIDStr := c.Param("id")
	tweetID, err := strconv.ParseUint(tweetIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet id"})
		return
	}

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetTweetComments(ctx, &tweetv1.GetTweetCommentsRequest{
		TweetId: tweetID,
		Cursor:  cursor,
		Limit:   int32(limit),
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	comments := make([]gin.H, 0, len(resp.Comments))
	// 收集所有评论的 userID
	commentUserIDs := make(map[uint64]bool)
	for _, comment := range resp.Comments {
		commentUserIDs[comment.UserId] = true
	}
	// 批量查询用户信息
	commentUserMap := make(map[uint64]gin.H)
	for uid := range commentUserIDs {
		userResp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: uid})
		if err != nil {
			log.Printf("Failed to get user %d for comment: %v", uid, err)
			commentUserMap[uid] = gin.H{"username": "unknown", "nickname": "", "avatar_url": ""}
			continue
		}
		u := userResp.User
		commentUserMap[uid] = gin.H{
			"username":   u.Username,
			"nickname":   u.Username,
			"avatar_url": u.Avatar,
		}
	}
	for _, comment := range resp.Comments {
		c := formatComment(comment)
		if userInfo, ok := commentUserMap[comment.UserId]; ok {
			c["user"] = userInfo
		}
		comments = append(comments, c)
	}

	c.JSON(http.StatusOK, gin.H{
		"comments":    comments,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
	})
}

// GetTweetReplies 获取推文回复
func (h *TweetHandler) GetTweetReplies(c *gin.Context) {
	tweetIDStr := c.Param("id")
	tweetID, err := strconv.ParseUint(tweetIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet id"})
		return
	}

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)
	// 获取当前登录用户 ID (可选)
	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetTweetReplies(ctx, &tweetv1.GetTweetRepliesRequest{
		TweetId:          tweetID,
		Cursor:           cursor,
		Limit:            int32(limit),
		RequestingUserId: requestingUserID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 丰富数据
	tweets := h.enrichTweetsWithUserInfo(ctx, resp.Replies, requestingUserID)

	c.JSON(http.StatusOK, gin.H{
		"replies":     tweets,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
	})
}

// SearchTweets 搜索推文
func (h *TweetHandler) SearchTweets(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'q' is required"})
		return
	}

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)

	// 获取当前登录用户 ID (可选)
	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.SearchTweets(ctx, &tweetv1.SearchTweetsRequest{
		Query:            query,
		Cursor:           cursor,
		Limit:            int32(limit),
		RequestingUserId: requestingUserID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tweets := h.enrichTweetsWithUserInfo(ctx, resp.Tweets, requestingUserID)

	c.JSON(http.StatusOK, gin.H{
		"tweets":      tweets,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
	})
}

// GetTrendingTopics 获取热门话题
func (h *TweetHandler) GetTrendingTopics(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetTrendingTopics(ctx, &tweetv1.GetTrendingTopicsRequest{
		Limit: int32(limit),
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	topics := make([]gin.H, 0, len(resp.Topics))
	for _, topic := range resp.Topics {
		topics = append(topics, gin.H{
			"topic": topic.Topic,
			"score": topic.Score,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"topics": topics,
	})
}

// formatComment 格式化评论
func formatComment(comment *tweetv1.Comment) gin.H {
	return gin.H{
		"id":         strconv.FormatUint(comment.Id, 10),
		"user_id":    strconv.FormatUint(comment.UserId, 10),
		"tweet_id":   strconv.FormatUint(comment.TweetId, 10),
		"content":    comment.Content,
		"created_at": comment.CreatedAt,
		"user": gin.H{
			"username":   comment.Username,
			"nickname":   comment.Nickname,
			"avatar_url": comment.AvatarUrl,
		},
	}
}

// formatTweet 格式化推文 (不含用户信息)
func formatTweet(tweet *tweetv1.Tweet) gin.H {
	return gin.H{
		"id":            strconv.FormatUint(tweet.Id, 10),
		"user_id":       strconv.FormatUint(tweet.UserId, 10),
		"content":       tweet.Content,
		"media_urls":    tweet.MediaUrls,
		"type":          tweet.Type,
		"visible_type":  tweet.VisibleType,
		"like_count":    tweet.LikeCount,
		"comment_count": tweet.CommentCount,
		"share_count":   tweet.ShareCount,
		"retweet_count": tweet.ShareCount,
		"is_liked":      tweet.IsLiked,
		"is_retweeted":  tweet.IsRetweeted,
		"is_bookmarked": tweet.IsBookmarked,
		"created_at":    tweet.CreatedAt,
		"updated_at":    tweet.UpdatedAt,
		"poll":          formatPoll(tweet.Poll),
	}
}

// formatPoll 格式化投票
func formatPoll(poll *tweetv1.Poll) gin.H {
	if poll == nil {
		return nil
	}
	options := make([]gin.H, len(poll.Options))
	for i, opt := range poll.Options {
		options[i] = gin.H{
			"id":         strconv.FormatUint(opt.Id, 10),
			"poll_id":    strconv.FormatUint(opt.PollId, 10),
			"text":       opt.Text,
			"vote_count": opt.VoteCount,
			"percentage": opt.Percentage,
		}
	}
	return gin.H{
		"id":              strconv.FormatUint(poll.Id, 10),
		"tweet_id":        strconv.FormatUint(poll.TweetId, 10),
		"question":        poll.Question,
		"options":         options,
		"end_time":        poll.EndTime,
		"is_expired":      poll.IsExpired,
		"is_voted":        poll.IsVoted,
		"voted_option_id": strconv.FormatUint(poll.VotedOptionId, 10),
		"total_votes":     poll.TotalVotes,
	}
}

// formatTweetWithUser 格式化推文 (含用户信息)
func formatTweetWithUser(tweet *tweetv1.Tweet, userInfo gin.H) gin.H {
	result := formatTweet(tweet)
	result["user"] = userInfo
	return result
}

// enrichTweetsWithUserInfo 批量查询用户信息并注入到 tweets 中
func (h *TweetHandler) enrichTweetsWithUserInfo(ctx context.Context, tweets []*tweetv1.Tweet, requestingUserID uint64) []gin.H {
	// 1. 收集所有 unique userIDs
	userIDSet := make(map[uint64]bool)
	for _, t := range tweets {
		userIDSet[t.UserId] = true
	}

	// 2. 查询每个用户信息
	userInfoMap := make(map[uint64]gin.H)
	for uid := range userIDSet {
		resp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: uid})
		if err != nil {
			log.Printf("Failed to get user %d: %v", uid, err)
			userInfoMap[uid] = gin.H{"id": strconv.FormatUint(uid, 10), "username": "unknown", "avatar": ""}
			continue
		}
		userInfoMap[uid] = formatUser(resp.User)
	}

	// 3. 组装结果
	result := make([]gin.H, 0, len(tweets))
	for _, t := range tweets {
		tweetData := formatTweetWithUser(t, userInfoMap[t.UserId])
		
		// 注入交互状态
		tweetData["is_liked"] = t.IsLiked
		tweetData["is_bookmarked"] = t.IsBookmarked
		tweetData["is_retweeted"] = t.IsRetweeted
		tweetData["retweet_count"] = int64(t.ShareCount)

		// 注入转发排序与显示所需的字段
		tweetData["is_retweeted_display"] = t.IsRetweetedDisplay
		tweetData["retweeted_at"] = t.RetweetedAt
		if t.SortId > 0 {
			tweetData["sort_id"] = strconv.FormatUint(t.SortId, 10)
		} else {
			tweetData["sort_id"] = strconv.FormatUint(t.Id, 10)
		}

		if t.Poll != nil && t.Poll.Id > 0 {
			if t.Poll.IsVoted {
				if pollData, ok := tweetData["poll"].(gin.H); ok && pollData != nil {
					pollData["is_voted"] = true
					pollData["voted_option_id"] = strconv.FormatUint(t.Poll.VotedOptionId, 10)
					tweetData["poll"] = pollData
				}
			}
		}

		result = append(result, tweetData)
	}
	return result
}

// RetweetTweet 转发推文
func (h *TweetHandler) RetweetTweet(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	tweetIDStr := c.Param("id")
	tweetID, err := strconv.ParseUint(tweetIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.RetweetTweet(ctx, &tweetv1.RetweetTweetRequest{
		UserId:  userID,
		TweetId: tweetID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"retweet_count": resp.RetweetCount,
		"is_retweeted":  true,
	})
}

// UnretweetTweet 取消转发
func (h *TweetHandler) UnretweetTweet(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	tweetIDStr := c.Param("id")
	tweetID, err := strconv.ParseUint(tweetIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tweet id"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.UnretweetTweet(ctx, &tweetv1.UnretweetTweetRequest{
		UserId:  userID,
		TweetId: tweetID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"retweet_count": resp.RetweetCount,
		"is_retweeted":  false,
	})
}

// ListTweets 获取全站最新推文
func (h *TweetHandler) ListTweets(c *gin.Context) {
	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)

	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)

	// 获取当前用户ID用于判断点赞/书签状态
	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.ListTweets(ctx, &tweetv1.ListTweetsRequest{
		Cursor:           cursor,
		Limit:            int32(limit),
		RequestingUserId: requestingUserID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tweets := h.enrichTweetsWithUserInfo(ctx, resp.Tweets, requestingUserID)

	c.JSON(http.StatusOK, gin.H{
		"tweets":      tweets,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
	})
}

// ==================== 用户个人资料 Tabs ====================

// GetUserLikes 获取用户喜欢的推文
func (h *TweetHandler) GetUserLikes(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)
	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetUserLikes(ctx, &tweetv1.GetUserLikesRequest{
		UserId:           userID,
		Cursor:           cursor,
		Limit:            int32(limit),
		RequestingUserId: requestingUserID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	enriched := h.enrichTweetsWithUserInfo(ctx, resp.Tweets, requestingUserID)

	c.JSON(http.StatusOK, gin.H{
		"tweets":      enriched,
		"next_cursor": strconv.FormatUint(resp.NextCursor, 10),
		"has_more":    resp.HasMore,
	})
}

// GetUserReplies 获取用户的回复（评论）
func (h *TweetHandler) GetUserReplies(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)
	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetUserReplies(ctx, &tweetv1.GetUserRepliesRequest{
		UserId:           userID,
		Cursor:           cursor,
		Limit:            int32(limit),
		RequestingUserId: requestingUserID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	enriched := h.enrichTweetsWithUserInfo(ctx, resp.Replies, requestingUserID)

	c.JSON(http.StatusOK, gin.H{
		"replies":     enriched,
		"next_cursor": strconv.FormatUint(resp.NextCursor, 10),
		"has_more":    resp.HasMore,
	})
}

// GetUserMedia 获取用户的媒体推文
func (h *TweetHandler) GetUserMedia(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	cursorStr := c.DefaultQuery("cursor", "0")
	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)
	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.ParseInt(limitStr, 10, 32)
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetUserMedia(ctx, &tweetv1.GetUserMediaRequest{
		UserId:           userID,
		Cursor:           cursor,
		Limit:            int32(limit),
		RequestingUserId: requestingUserID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	enriched := h.enrichTweetsWithUserInfo(ctx, resp.Tweets, requestingUserID)

	c.JSON(http.StatusOK, gin.H{
		"tweets":      enriched,
		"next_cursor": strconv.FormatUint(resp.NextCursor, 10),
		"has_more":    resp.HasMore,
	})
}
