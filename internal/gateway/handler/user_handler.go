package handler

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	followv1 "twitter-clone/api/follow/v1"
	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/gateway/middleware"
)

// UserHandler 用户处理器
type UserHandler struct {
	userClient   userv1.UserServiceClient
	followClient followv1.FollowServiceClient
	tweetClient  tweetv1.TweetServiceClient
}

// NewUserHandler 创建用户处理器
func NewUserHandler(userClient userv1.UserServiceClient, followClient followv1.FollowServiceClient, tweetClient tweetv1.TweetServiceClient) *UserHandler {
	return &UserHandler{
		userClient:   userClient,
		followClient: followClient,
		tweetClient:  tweetClient,
	}
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// Register 用户注册
// POST /api/v1/auth/register
func (h *UserHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.Register(ctx, &userv1.RegisterRequest{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": formatUser(resp.User),
	})
}

// LoginRequest 登录请求
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// Login 用户登录
// POST /api/v1/auth/login
func (h *UserHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.Login(ctx, &userv1.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	})

	if err != nil {
		// 这里可以根据错误类型返回 401
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": resp.Token,
		"user":  formatUser(resp.User),
	})
}

// GetProfile 获取用户资料
// GET /api/v1/users/:id
func (h *UserHandler) GetProfile(c *gin.Context) {
	idStr := c.Param("id")
	userID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{
		UserId: userID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	uDict := formatUser(resp.User)
	uDict["is_following"] = false

	if currentUserID, exists := middleware.GetUserID(c); exists && currentUserID != userID {
		fCtx, fCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer fCancel()
		followResp, err := h.followClient.IsFollowing(fCtx, &followv1.IsFollowingRequest{
			FollowerId: currentUserID,
			FolloweeId: userID,
		})
		if err == nil {
			uDict["is_following"] = followResp.IsFollowing
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"user": uDict,
	})
}

// UpdateProfileRequest 更新资料请求
type UpdateProfileRequest struct {
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
	CoverUrl string `json:"cover_url"`
	Website  string `json:"website"`
	Location string `json:"location"`
}

// UpdateProfile 更新用户资料
// PUT /api/v1/users/profile
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.UpdateProfile(ctx, &userv1.UpdateProfileRequest{
		UserId:   userID,
		Avatar:   req.Avatar,
		Bio:      req.Bio,
		CoverUrl: req.CoverUrl,
		Website:  req.Website,
		Location: req.Location,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": formatUser(resp.User),
	})
}

// GetMe 获取当前用户信息
// GET /api/v1/users/me
func (h *UserHandler) GetMe(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{
		UserId: userID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": formatUser(resp.User),
	})
}

// GetBatchUsersRequest 批量获取用户请求
type GetBatchUsersRequest struct {
	UserIDs []string `json:"user_ids" binding:"required"`
}

// GetBatchUsers 批量获取用户信息
// POST /api/v1/users/batch
func (h *UserHandler) GetBatchUsers(c *gin.Context) {
	var req GetBatchUsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userIDs := make([]uint64, 0, len(req.UserIDs))
	for _, idStr := range req.UserIDs {
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err == nil {
			userIDs = append(userIDs, id)
		}
	}

	if len(userIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"users": []gin.H{}})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.GetBatchUsers(ctx, &userv1.GetBatchUsersRequest{
		UserIds: userIDs,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	users := make([]gin.H, len(resp.Users))
	for i, user := range resp.Users {
		uDict := formatUser(user)
		uDict["is_following"] = false
		users[i] = uDict
	}

	currentUserID, exists := middleware.GetUserID(c)
	if exists {
		var wg sync.WaitGroup
		var mu sync.Mutex

		for i, user := range resp.Users {
			if user.Id == currentUserID {
				continue
			}
			wg.Add(1)
			go func(idx int, targetID uint64) {
				defer wg.Done()
				fCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				followResp, err := h.followClient.IsFollowing(fCtx, &followv1.IsFollowingRequest{
					FollowerId: currentUserID,
					FolloweeId: targetID,
				})
				if err == nil {
					mu.Lock()
					users[idx]["is_following"] = followResp.IsFollowing
					mu.Unlock()
				}
			}(i, user.Id)
		}
		wg.Wait()
	}

	c.JSON(http.StatusOK, gin.H{
		"users": users,
	})
}

// SearchUsers 搜索用户
// GET /api/v1/users/search?q=keyword&cursor=0&limit=20
func (h *UserHandler) SearchUsers(c *gin.Context) {
	query := c.Query("q")
	cursorStr := c.DefaultQuery("cursor", "0")
	limitStr := c.DefaultQuery("limit", "20")

	cursor, _ := strconv.ParseUint(cursorStr, 10, 64)
	limit, _ := strconv.Atoi(limitStr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.SearchUsers(ctx, &userv1.SearchUsersRequest{
		Keyword: query,
		Cursor:  cursor,
		Limit:   int32(limit),
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	users := make([]gin.H, len(resp.Users))
	for i, user := range resp.Users {
		uDict := formatUser(user)
		uDict["is_following"] = false
		users[i] = uDict
	}

	currentUserID, exists := middleware.GetUserID(c)
	if exists {
		var wg sync.WaitGroup
		var mu sync.Mutex

		for i, user := range resp.Users {
			if user.Id == currentUserID {
				continue
			}
			wg.Add(1)
			go func(idx int, targetID uint64) {
				defer wg.Done()
				fCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				followResp, err := h.followClient.IsFollowing(fCtx, &followv1.IsFollowingRequest{
					FollowerId: currentUserID,
					FolloweeId: targetID,
				})
				if err == nil {
					mu.Lock()
					users[idx]["is_following"] = followResp.IsFollowing
					mu.Unlock()
				}
			}(i, user.Id)
		}
		wg.Wait()
	}

	c.JSON(http.StatusOK, gin.H{
		"users":       users,
		"next_cursor": strconv.FormatUint(resp.NextCursor, 10),
		"has_more":    resp.HasMore,
	})
}

func formatUser(user *userv1.User) gin.H {
	return gin.H{
		"id":         strconv.FormatUint(user.Id, 10),
		"username":   user.Username,
		"email":      user.Email, // 注意隐私，视情况是否返回
		"avatar":     user.Avatar,
		"bio":        user.Bio,
		"cover_url":  user.CoverUrl,
		"website":    user.Website,
		"location":   user.Location,
		"created_at": user.CreatedAt,
	}
}

// GetFullProfile 获取聚合的用户资料 (BFF)
func (h *UserHandler) GetFullProfile(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid user id",
		})
		return
	}

	// 使用 errgroup 并发请求所有服务
	g, ctx := errgroup.WithContext(c.Request.Context())

	// Create a derived context with timeout
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var (
		userProfile  *userv1.User
		followStats  *followv1.GetFollowStatsResponse
		recentTweets *tweetv1.GetUserTimelineResponse
	)

	// 1. Get User Profile
	g.Go(func() error {
		resp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{UserId: userID})
		if err != nil {
			return err
		}
		userProfile = resp.User
		return nil
	})

	// 2. Get Follow Stats
	g.Go(func() error {
		resp, err := h.followClient.GetFollowStats(ctx, &followv1.GetFollowStatsRequest{UserId: userID})
		if err != nil {
			// 如果统计挂了，我们可以返回空数据，而不是让整个请求失败（容错）
			followStats = &followv1.GetFollowStatsResponse{FollowerCount: 0, FolloweeCount: 0}
			return nil
		}
		followStats = resp
		return nil
	})

	// 3. Get Recent Tweets
	g.Go(func() error {
		resp, err := h.tweetClient.GetUserTimeline(ctx, &tweetv1.GetUserTimelineRequest{UserId: userID, Limit: 5})
		if err != nil {
			recentTweets = &tweetv1.GetUserTimelineResponse{Tweets: []*tweetv1.Tweet{}}
			return nil
		}
		recentTweets = resp
		return nil
	})

	if err := g.Wait(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch full profile: " + err.Error()})
		return
	}

	// 组装聚合响应
	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":         userProfile.Id,
			"username":   userProfile.Username,
			"bio":        userProfile.Bio,
			"avatar":     userProfile.Avatar,
			"cover_url":  userProfile.CoverUrl,
			"website":    userProfile.Website,
			"location":   userProfile.Location,
			"created_at": userProfile.CreatedAt,
		},
		"stats": gin.H{
			"followers": followStats.FollowerCount,
			"following": followStats.FolloweeCount,
		},
		"recent_tweets": recentTweets.Tweets,
	})
}
