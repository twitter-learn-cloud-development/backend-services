package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	followv1 "twitter-clone/api/follow/v1"
	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/gateway/middleware"

	"golang.org/x/sync/errgroup"
)

// UserHandler 用户处理器
type UserHandler struct {
	userClient   userv1.UserServiceClient
	tweetClient  tweetv1.TweetServiceClient
	followClient followv1.FollowServiceClient
	jwtMW        *middleware.JWTMiddleware
}

// NewUserHandler 创建用户处理器
func NewUserHandler(
	userClient userv1.UserServiceClient,
	tweetClient tweetv1.TweetServiceClient,
	followClient followv1.FollowServiceClient,
	jwtMW *middleware.JWTMiddleware,
) *UserHandler {
	return &UserHandler{
		userClient:   userClient,
		tweetClient:  tweetClient,
		followClient: followClient,
		jwtMW:        jwtMW,
	}
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=20"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// UpdateProfileRequest 更新资料请求
type UpdateProfileRequest struct {
	Avatar string `json:"avatar"`
	Bio    string `json:"bio"`
}

// Register 注册用户
func (h *UserHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 调用 gRPC
	resp, err := h.userClient.Register(ctx, &userv1.RegisterRequest{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user": gin.H{
			"id":         resp.User.Id,
			"username":   resp.User.Username,
			"email":      resp.User.Email,
			"avatar":     resp.User.Avatar,
			"bio":        resp.User.Bio,
			"created_at": resp.User.CreatedAt,
		},
	})
}

// Login 登录
func (h *UserHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 调用 gRPC
	resp, err := h.userClient.Login(ctx, &userv1.LoginRequest{
		Email:    req.Email,
		Password: req.Password,
	})

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "invalid email or password",
		})
		return
	}

	// 解析 user_id
	userID, err := strconv.ParseUint(strconv.FormatUint(resp.User.Id, 10), 10, 64)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to parse user id",
		})
		return
	}

	// 生成 JWT Token
	token, err := h.jwtMW.GenerateToken(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to generate token",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":       resp.User.Id,
			"username": resp.User.Username,
			"email":    resp.User.Email,
			"avatar":   resp.User.Avatar,
			"bio":      resp.User.Bio,
		},
	})
}

// GetProfile 获取用户资料
func (h *UserHandler) GetProfile(c *gin.Context) {
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

	resp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{
		UserId: userID,
	})

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "user not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":         resp.User.Id,
			"username":   resp.User.Username,
			"email":      resp.User.Email,
			"avatar":     resp.User.Avatar,
			"bio":        resp.User.Bio,
			"created_at": resp.User.CreatedAt,
		},
	})
}

// UpdateProfile 更新资料
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	// 获取当前用户 ID
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.UpdateProfile(ctx, &userv1.UpdateProfileRequest{
		UserId: userID,
		Avatar: req.Avatar,
		Bio:    req.Bio,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":         resp.User.Id,
			"username":   resp.User.Username,
			"email":      resp.User.Email,
			"avatar":     resp.User.Avatar,
			"bio":        resp.User.Bio,
			"updated_at": resp.User.UpdatedAt,
		},
	})
}

// GetMe 获取当前用户信息
func (h *UserHandler) GetMe(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := h.userClient.GetProfile(ctx, &userv1.GetProfileRequest{
		UserId: userID,
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":         resp.User.Id,
			"username":   resp.User.Username,
			"email":      resp.User.Email,
			"avatar":     resp.User.Avatar,
			"bio":        resp.User.Bio,
			"created_at": resp.User.CreatedAt,
		},
	})
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
			// 这里演示简单做法：返回空
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
			"created_at": userProfile.CreatedAt,
		},
		"stats": gin.H{
			"followers": followStats.FollowerCount,
			"following": followStats.FolloweeCount,
		},
		"recent_tweets": recentTweets.Tweets,
	})
}
