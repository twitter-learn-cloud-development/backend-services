package handler

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/gateway/middleware"
	"twitter-clone/pkg/pkg/snowflake"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TweetHandler 推文处理器
type TweetHandler struct {
	tweetClient tweetv1.TweetServiceClient
	userClient  userv1.UserServiceClient
	db          *gorm.DB
}

// NewTweetHandler 创建推文处理器
func NewTweetHandler(tweetClient tweetv1.TweetServiceClient, userClient userv1.UserServiceClient, db *gorm.DB) *TweetHandler {
	return &TweetHandler{
		tweetClient: tweetClient,
		userClient:  userClient,
		db:          db,
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	// 获取当前登录用户 ID (可选)
	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	// 1. 获取用户发布的推文 (来自 tweet-service)
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

	var tweets []gin.H

	// 2. 获取用户转发的推文 (来自 retweets 表)
	// 注意：分页比较复杂，这里采用简单策略：分别获取，然后内存合并。
	// 对于 MVP，如果 cursor 是时间戳(snowflake)，我们可以分别查询
	// tweet-service 的 cursor 是 tweet ID。
	// retweets 表的主键也是 snowflake ID。

	// 如果 h.db 存在 (Gateway 直连 DB 模式)
	if h.db != nil {
		var retweets []domain.Retweet
		rtQuery := h.db.Where("user_id = ?", userID)
		if cursor > 0 {
			rtQuery = rtQuery.Where("id < ?", cursor)
		}
		rtQuery.Order("id DESC").Limit(int(limit)).Find(&retweets)

		// 收集原推文 ID
		rtTweetIDs := make([]uint64, 0, len(retweets))
		for _, rt := range retweets {
			rtTweetIDs = append(rtTweetIDs, rt.TweetID)
		}

		// 批量获取原推文详情
		// 可以调用 tweetClient.GetTweets(ids) 如果有这个接口，或者直接查 DB (如果直连)
		// 这里假设直连 DB 查询 tweets 表
		// 也可以循环调用 GetTweet (性能差)
		// 我们假设 gateway 可以查 tweets 表
		var rtOriginalTweets []domain.Tweet
		if len(rtTweetIDs) > 0 {
			h.db.Model(&domain.Tweet{}).Where("id IN ?", rtTweetIDs).Find(&rtOriginalTweets)
		}
		rtObMap := make(map[uint64]*domain.Tweet)
		for i := range rtOriginalTweets {
			rtObMap[rtOriginalTweets[i].ID] = &rtOriginalTweets[i]
		}

		// 转换 tweet-service 返回的 tweets 为 map 或 list
		// tweet-service 返回的是 []*tweetv1.Tweet

		// 合并策略：
		// 将 retweets 转换为 tweetv1.Tweet 结构 (带有 is_retweeted=true, retweeted_at=?)
		// 然后和 resp.Tweets 合并，按 ID (时间) 排序

		// 为了简单，我们直接构造最终的 gin.H 列表

		// A. 处理原生推文 (resp.Tweets)
		// 先 enrich
		enrichedTweets := h.enrichTweetsWithUserInfo(ctx, resp.Tweets, requestingUserID)

		// B. 处理转发推文
		var enrichedRetweets []gin.H

		// 构造 Retweet 对应的 Tweet 对象
		// 注意：我们需要用户信息（原推文作者）
		// enrichTweetsWithUserInfo 需要 []*tweetv1.Tweet
		var tweetsToEnrich []*tweetv1.Tweet
		for _, rt := range retweets {
			if original, ok := rtObMap[rt.TweetID]; ok {
				// 转换为 RPC 对象
				t := &tweetv1.Tweet{
					Id:          original.ID,
					UserId:      original.UserID,
					Content:     original.Content,
					MediaUrls:   []string(original.MediaURLs),
					Type:        int32(original.Type),
					VisibleType: int32(original.VisibleType),
					CreatedAt:   original.CreatedAt,
					UpdatedAt:   original.UpdatedAt,
					// 计数可能不准，需要实时查? enrichTweetsWithUserInfo 会查
				}
				tweetsToEnrich = append(tweetsToEnrich, t)
			}
		}

		if len(tweetsToEnrich) > 0 {
			enrichedRetweets = h.enrichTweetsWithUserInfo(ctx, tweetsToEnrich, requestingUserID)
			// 标记为转发
			// 注意 enrichedRetweets 的顺序和 retweets 可能不一致 (因为 enrich 内部逻辑)
			// 但 tweetsToEnrich 是按 retweets 顺序加的
			// enrichTweetsWithUserInfo 返回顺序也是对应的吗？
			// h.enrichTweetsWithUserInfo 内部是遍历输入的 tweets，所以顺序一致。

			for i, rt := range retweets {
				// 找到对应的 enrichedTweet
				// tweetsToEnrich[i] 对应 retweets[i]
				// enrichedRetweets[i] 对应 tweetsToEnrich[i]
				if i < len(enrichedRetweets) {
					// 覆盖 created_at 为转发时间 (用于排序显示) ??
					// 不，通常显示 "Retweeted at xx"，内容还是原推文时间
					// 但 Timeline 排序要用 转发时间 (rt.ID / rt.CreatedAt)

					// 我们的 Timeline item 结构:
					item := enrichedRetweets[i]
					item["is_retweeted_display"] = true // 标记这是一条转发记录用于前端显示 "You Retweeted"
					item["retweeted_at"] = rt.CreatedAt
					item["sort_id"] = strconv.FormatUint(rt.ID, 10) // 排序用 ID (转发记录的 ID)

					// 还需要把 ID 改成 Retweet ID 吗？
					// 这里的 ID 是原推文 ID。
					// 前端 key 需要唯一。如果同一个推文被转发和原发 (不可能同时，除非不同时间)
					// 如果列表中既有 原推文 又有 转发推文 (比如我转发了自己?), ID 重复会报错。
					// 这种情况下，可以使用 sort_id 作为 key。

				}
			}
		}

		// C. 合并
		// 原生推文的 sort_id 就是其 ID
		for _, t := range enrichedTweets {
			t["sort_id"] = t["id"]
			t["is_retweeted_display"] = false
		}

		// Merge enrichedTweets and enrichedRetweets
		// Sort by sort_id desc
		// Limit to limit

		allItems := append(enrichedTweets, enrichedRetweets...)

		// 排序
		// 简单冒泡或 sort
		// 数量少 (2 * limit)，直接排
		// 需要定义排序函数

		// 简单起见，我们假设 limit 是总数。
		// 手动排序
		for i := 0; i < len(allItems); i++ {
			for j := i + 1; j < len(allItems); j++ {
				id1, _ := strconv.ParseUint(allItems[i]["sort_id"].(string), 10, 64)
				id2, _ := strconv.ParseUint(allItems[j]["sort_id"].(string), 10, 64)
				if id1 < id2 {
					allItems[i], allItems[j] = allItems[j], allItems[i]
				}
			}
		}

		if len(allItems) > int(limit) {
			allItems = allItems[:limit]
		}
		tweets = allItems

		// 更新 next_cursor
		if len(tweets) > 0 {
			lastItem := tweets[len(tweets)-1]
			nextCursorStr := lastItem["sort_id"].(string)
			resp.NextCursor, _ = strconv.ParseUint(nextCursorStr, 10, 64)
		}
		resp.HasMore = len(tweets) >= int(limit) // 粗略判断

	} else {
		// Fallback to original
		tweets = h.enrichTweetsWithUserInfo(ctx, resp.Tweets, requestingUserID)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.GetTweetReplies(ctx, &tweetv1.GetTweetRepliesRequest{
		TweetId: tweetID,
		Cursor:  cursor,
		Limit:   int32(limit),
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.SearchTweets(ctx, &tweetv1.SearchTweetsRequest{
		Query:  query,
		Cursor: cursor,
		Limit:  int32(limit),
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		"is_retweeted":  false,
		"is_bookmarked": false,
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

// enrichTweetsWithUserInfo 批量查询用户信息并注入到 tweets 中，同时注入 is_liked 和 is_bookmarked 状态
func (h *TweetHandler) enrichTweetsWithUserInfo(ctx context.Context, tweets []*tweetv1.Tweet, requestingUserID uint64) []gin.H {
	// 1. 收集所有 unique userIDs 和 tweetIDs
	userIDSet := make(map[uint64]bool)
	tweetIDs := make([]uint64, 0, len(tweets))
	for _, t := range tweets {
		userIDSet[t.UserId] = true
		tweetIDs = append(tweetIDs, t.Id)
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

	// 3. 批量查询 is_liked / is_bookmarked / is_retweeted 状态 + retweet_count
	likedSet := make(map[uint64]bool)
	bookmarkedSet := make(map[uint64]bool)
	retweetedSet := make(map[uint64]bool)
	retweetCountMap := make(map[uint64]int64)
	votedOptionMap := make(map[uint64]uint64)

	if h.db != nil && len(tweetIDs) > 0 {
		// 查询 retweet_count（对所有用户都展示）
		type RetweetCountResult struct {
			TweetID uint64 `gorm:"column:tweet_id"`
			Count   int64  `gorm:"column:count"`
		}
		var retweetCounts []RetweetCountResult
		h.db.Model(&domain.Retweet{}).Select("tweet_id, COUNT(*) as count").
			Where("tweet_id IN ?", tweetIDs).Group("tweet_id").Find(&retweetCounts)
		for _, rc := range retweetCounts {
			retweetCountMap[rc.TweetID] = rc.Count
		}

		if requestingUserID > 0 {
			// 查询 likes 表
			var likedIDs []uint64
			h.db.Model(&domain.Like{}).Where("user_id = ? AND tweet_id IN ?", requestingUserID, tweetIDs).Pluck("tweet_id", &likedIDs)
			for _, id := range likedIDs {
				likedSet[id] = true
			}
			// 查询 bookmarks 表
			var bookmarkedIDs []uint64
			h.db.Model(&domain.Bookmark{}).Where("user_id = ? AND tweet_id IN ?", requestingUserID, tweetIDs).Pluck("tweet_id", &bookmarkedIDs)
			for _, id := range bookmarkedIDs {
				bookmarkedSet[id] = true
			}
			// 查询 retweets 表
			var retweetedIDs []uint64
			h.db.Model(&domain.Retweet{}).Where("user_id = ? AND tweet_id IN ?", requestingUserID, tweetIDs).Pluck("tweet_id", &retweetedIDs)
			for _, id := range retweetedIDs {
				retweetedSet[id] = true
			}

			// 查询 poll votes 表
			var pollIDs []uint64
			pollToTweetMap := make(map[uint64]uint64)
			for _, t := range tweets {
				if t.Poll != nil && t.Poll.Id > 0 {
					pollIDs = append(pollIDs, t.Poll.Id)
					pollToTweetMap[t.Poll.Id] = t.Id
				}
			}

			if len(pollIDs) > 0 {
				type PollVoteResult struct {
					PollID   uint64 `gorm:"column:poll_id"`
					OptionID uint64 `gorm:"column:option_id"`
				}
				var votes []PollVoteResult
				h.db.Model(&domain.PollVote{}).Select("poll_id, option_id").Where("user_id = ? AND poll_id IN ?", requestingUserID, pollIDs).Find(&votes)

				for _, v := range votes {
					if tid, ok := pollToTweetMap[v.PollID]; ok {
						votedOptionMap[tid] = v.OptionID
					}
				}
			}
		}
	}

	// 4. 组装结果
	result := make([]gin.H, 0, len(tweets))
	for _, t := range tweets {
		tweetData := formatTweetWithUser(t, userInfoMap[t.UserId])
		// 注入交互状态 (覆盖 proto 中可能不准确的值)
		tweetData["is_liked"] = likedSet[t.Id]
		tweetData["is_bookmarked"] = bookmarkedSet[t.Id]
		tweetData["is_retweeted"] = retweetedSet[t.Id]
		if rc, ok := retweetCountMap[t.Id]; ok {
			tweetData["retweet_count"] = rc
		} else {
			tweetData["retweet_count"] = 0
		}

		if t.Poll != nil && t.Poll.Id > 0 {
			if optID, ok := votedOptionMap[t.Id]; ok {
				if pollData, ok := tweetData["poll"].(gin.H); ok && pollData != nil {
					pollData["is_voted"] = true
					pollData["voted_option_id"] = strconv.FormatUint(optID, 10)
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

	// 幂等创建
	retweet := &domain.Retweet{
		ID:        snowflake.GenerateID(),
		UserID:    userID,
		TweetID:   tweetID,
		CreatedAt: time.Now().UnixMilli(),
	}
	result := h.db.Where("user_id = ? AND tweet_id = ?", userID, tweetID).FirstOrCreate(retweet)
	if result.Error != nil {
		log.Printf("[RetweetTweet] Failed to FirstOrCreate retweet: %v", result.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retweet"})
		return
	}

	// 查询最新转发数
	var count int64
	h.db.Model(&domain.Retweet{}).Where("tweet_id = ?", tweetID).Count(&count)

	c.JSON(http.StatusOK, gin.H{
		"retweet_count": count,
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

	h.db.Where("user_id = ? AND tweet_id = ?", userID, tweetID).Delete(&domain.Retweet{})

	// 查询最新转发数
	var count int64
	h.db.Model(&domain.Retweet{}).Where("tweet_id = ?", tweetID).Count(&count)

	c.JSON(http.StatusOK, gin.H{
		"retweet_count": count,
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.tweetClient.ListTweets(ctx, &tweetv1.ListTweetsRequest{
		Cursor: cursor,
		Limit:  int32(limit),
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

	// 获取当前用户ID用于判断点赞/书签状态
	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	// 1. 从 likes 表查询该用户点赞的 tweet_ids（游标分页）
	var likes []domain.Like
	query := h.db.Where("user_id = ?", userID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	query.Order("id DESC").Limit(int(limit) + 1).Find(&likes)

	hasMore := len(likes) > int(limit)
	if hasMore {
		likes = likes[:limit]
	}

	var nextCursor uint64
	if len(likes) > 0 {
		nextCursor = likes[len(likes)-1].ID
	}

	// 2. 批量获取推文
	tweetIDs := make([]uint64, 0, len(likes))
	for _, l := range likes {
		tweetIDs = append(tweetIDs, l.TweetID)
	}

	if len(tweetIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"tweets":      []gin.H{},
			"next_cursor": "0",
			"has_more":    false,
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 批量获取推文
	var tweets []*tweetv1.Tweet
	for _, tid := range tweetIDs {
		tResp, err := h.tweetClient.GetTweet(ctx, &tweetv1.GetTweetRequest{
			TweetId:          tid,
			RequestingUserId: requestingUserID,
		})
		if err != nil {
			log.Printf("Failed to get tweet %d: %v", tid, err)
			continue
		}
		tweets = append(tweets, tResp.Tweet)
	}

	enriched := h.enrichTweetsWithUserInfo(ctx, tweets, requestingUserID)

	c.JSON(http.StatusOK, gin.H{
		"tweets":      enriched,
		"next_cursor": strconv.FormatUint(nextCursor, 10),
		"has_more":    hasMore,
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

	// 获取当前用户ID
	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	// 1. 从 comments 表查询该用户的评论
	var comments []domain.Comment
	query := h.db.Where("user_id = ? AND deleted_at = 0", userID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	query.Order("id DESC").Limit(int(limit) + 1).Find(&comments)

	hasMore := len(comments) > int(limit)
	if hasMore {
		comments = comments[:limit]
	}

	var nextCursor uint64
	if len(comments) > 0 {
		nextCursor = comments[len(comments)-1].ID
	}

	if len(comments) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"replies":     []gin.H{},
			"next_cursor": "0",
			"has_more":    false,
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 2. 查询评论者 + 原推文信息
	// 收集 unique user IDs 和 tweet IDs
	userIDSet := make(map[uint64]bool)
	tweetIDSet := make(map[uint64]bool)
	for _, c := range comments {
		userIDSet[c.UserID] = true
		tweetIDSet[c.TweetID] = true
	}

	// 批量查询用户
	userInfoMap := make(map[uint64]gin.H)
	for uid := range userIDSet {
		resp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: uid})
		if err != nil {
			userInfoMap[uid] = gin.H{"id": strconv.FormatUint(uid, 10), "username": "unknown", "avatar": ""}
			continue
		}
		userInfoMap[uid] = formatUser(resp.User)
	}

	// 批量查询原推文（仅获取简要信息）
	tweetSnippetMap := make(map[uint64]gin.H)
	for tid := range tweetIDSet {
		tResp, err := h.tweetClient.GetTweet(ctx, &tweetv1.GetTweetRequest{
			TweetId:          tid,
			RequestingUserId: requestingUserID,
		})
		if err != nil {
			continue
		}
		t := tResp.Tweet
		// 查询原推文作者
		var tweetUser gin.H
		uResp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: t.UserId})
		if err == nil {
			tweetUser = formatUser(uResp.User)
		} else {
			tweetUser = gin.H{"id": strconv.FormatUint(t.UserId, 10), "username": "unknown", "avatar": ""}
		}

		tweetSnippetMap[tid] = gin.H{
			"id":      strconv.FormatUint(t.Id, 10),
			"content": t.Content,
			"user":    tweetUser,
		}
	}

	// 3. 组装结果
	replies := make([]gin.H, 0, len(comments))
	for _, c := range comments {
		reply := gin.H{
			"id":         strconv.FormatUint(c.ID, 10),
			"user_id":    strconv.FormatUint(c.UserID, 10),
			"tweet_id":   strconv.FormatUint(c.TweetID, 10),
			"content":    c.Content,
			"created_at": c.CreatedAt,
			"user":       userInfoMap[c.UserID],
			"tweet":      tweetSnippetMap[c.TweetID],
		}
		replies = append(replies, reply)
	}

	c.JSON(http.StatusOK, gin.H{
		"replies":     replies,
		"next_cursor": strconv.FormatUint(nextCursor, 10),
		"has_more":    hasMore,
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

	// 获取当前用户ID
	var requestingUserID uint64
	if uid, exists := middleware.GetUserID(c); exists {
		requestingUserID = uid
	}

	// 1. 从 tweets 表查询含媒体的推文
	var tweets []domain.Tweet
	query := h.db.Where("user_id = ? AND deleted_at = 0 AND media_urls IS NOT NULL AND media_urls != '[]' AND media_urls != ''", userID)
	if cursor > 0 {
		query = query.Where("id < ?", cursor)
	}
	query.Order("id DESC").Limit(int(limit) + 1).Find(&tweets)

	hasMore := len(tweets) > int(limit)
	if hasMore {
		tweets = tweets[:limit]
	}

	var nextCursor uint64
	if len(tweets) > 0 {
		nextCursor = tweets[len(tweets)-1].ID
	}

	if len(tweets) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"tweets":      []gin.H{},
			"next_cursor": "0",
			"has_more":    false,
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 2. 转换为 proto 格式并 enrich
	protoTweets := make([]*tweetv1.Tweet, 0, len(tweets))
	for _, t := range tweets {
		mediaURLs := []string(t.MediaURLs)
		protoTweets = append(protoTweets, &tweetv1.Tweet{
			Id:          t.ID,
			UserId:      t.UserID,
			Content:     t.Content,
			MediaUrls:   mediaURLs,
			Type:        int32(t.Type),
			VisibleType: int32(t.VisibleType),
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
		})
	}

	enriched := h.enrichTweetsWithUserInfo(ctx, protoTweets, requestingUserID)

	c.JSON(http.StatusOK, gin.H{
		"tweets":      enriched,
		"next_cursor": strconv.FormatUint(nextCursor, 10),
		"has_more":    hasMore,
	})
}
