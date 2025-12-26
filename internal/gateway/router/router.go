package router

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"time"
	"twitter-clone/internal/gateway/handler"
	"twitter-clone/internal/gateway/middleware"

	"github.com/go-redis/redis/v8"
)

// SetupRouter 设置路由
func SetupRouter(
	userHandler *handler.UserHandler,
	tweetHandler *handler.TweetHandler,
	followHandler *handler.FollowHandler,
	jwtMW *middleware.JWTMiddleware,
	redisClient *redis.Client,
) *gin.Engine {
	// 设置为 Release 模式
	// gin.SetMode(gin.ReleaseMode)

	r := gin.New()

	// 🔍 分布式链路追踪 (OpenTelemetry Middleware)
	// 🔍 分布式链路追踪 (OpenTelemetry Middleware)
	r.Use(otelgin.Middleware("gateway"))

	// 📊 Prometheus 指标收集
	r.Use(middleware.MetricsMiddleware())

	// 🚦 Rate Limiting (Global: 1000 req/minute per IP)
	// 🚦 Rate Limiting (Global: 1000 req/minute per IP)
	if redisClient != nil {
		r.Use(middleware.NewRateLimitMiddleware(redisClient, 1000, 60*time.Second))
	}

	// 全局中间件
	r.Use(middleware.Logger())
	r.Use(middleware.CORS())
	r.Use(gin.Recovery())
	r.Use(middleware.ErrorHandler())

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	// 📊 Prometheus Metrics Endpoint
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API v1
	v1 := r.Group("/api/v1")
	{
		// 认证相关（不需要 JWT）
		auth := v1.Group("/auth")
		{
			auth.POST("/register", userHandler.Register)
			auth.POST("/login", userHandler.Login)
		}

		// 用户相关
		users := v1.Group("/users")
		{
			// 公开接口
			users.GET("/:id", userHandler.GetProfile)
			users.GET("/:id/timeline", tweetHandler.GetUserTimeline)
			users.GET("/:id/followers", followHandler.GetFollowers)
			users.GET("/:id/followees", followHandler.GetFollowees)
			users.GET("/:id/stats", followHandler.GetFollowStats)
			users.GET("/:id/full_profile", userHandler.GetFullProfile)

			// 需要认证的接口
			users.Use(jwtMW.AuthRequired())
			{
				users.GET("/me", userHandler.GetMe)
				users.PUT("/me", userHandler.UpdateProfile)
			}
		}

		// 推文相关
		tweets := v1.Group("/tweets")
		{
			// 公开接口
			tweets.GET("/:id", tweetHandler.GetTweet)

			// 需要认证的接口
			tweets.Use(jwtMW.AuthRequired())
			{
				tweets.POST("", tweetHandler.CreateTweet)
				tweets.DELETE("/:id", tweetHandler.DeleteTweet)
			}
		}

		// Feeds（需要认证）
		feeds := v1.Group("/feeds")
		feeds.Use(jwtMW.AuthRequired())
		{
			feeds.GET("", tweetHandler.GetFeeds)
		}

		// 关注相关（需要认证）
		follows := v1.Group("/follows")
		follows.Use(jwtMW.AuthRequired())
		{
			follows.POST("", followHandler.Follow)
			follows.DELETE("/:id", followHandler.Unfollow)
			follows.GET("/:id/status", followHandler.IsFollowing)
		}
	}

	return r
}
