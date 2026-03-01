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
	tweetHandler *handler.TweetHandler,
	followHandler *handler.FollowHandler,
	userHandler *handler.UserHandler,
	uploadHandler *handler.UploadHandler,
	notificationHandler *handler.NotificationHandler,
	bookmarkHandler *handler.BookmarkHandler,
	messengerHandler *handler.MessengerHandler,
	wsHandler *handler.WebSocketHandler,
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
			// 公开接口 (允许可选认证以提取 currentUserID)
			users.Use(jwtMW.AuthOptional())
			users.GET("/search", userHandler.SearchUsers) // P3 新增
			users.POST("/batch", userHandler.GetBatchUsers)
			users.GET("/:id", userHandler.GetProfile)
			users.GET("/:id/timeline", tweetHandler.GetUserTimeline)
			users.GET("/:id/followers", followHandler.GetFollowers)
			users.GET("/:id/followees", followHandler.GetFollowees)
			users.GET("/:id/stats", followHandler.GetFollowStats)
			users.GET("/:id/full_profile", userHandler.GetFullProfile)
			users.GET("/:id/likes", tweetHandler.GetUserLikes)
			users.GET("/:id/replies", tweetHandler.GetUserReplies)
			users.GET("/:id/media", tweetHandler.GetUserMedia)

			// 需要认证的接口
			users.Use(jwtMW.AuthRequired())
			{
				users.GET("/me", userHandler.GetMe)
				users.PUT("/me", userHandler.UpdateProfile)
			}
		}

		// 公共搜索接口 (推文搜索)
		v1.GET("/search", jwtMW.AuthOptional(), tweetHandler.SearchTweets)
		v1.GET("/trends", tweetHandler.GetTrendingTopics)

		// 推文相关
		tweets := v1.Group("/tweets")
		{
			// 公开接口 (允许可选认证以提取 currentUserID)
			tweets.Use(jwtMW.AuthOptional())
			tweets.GET("/public", tweetHandler.ListTweets) // 映射到 ListTweets
			tweets.GET("/:id", tweetHandler.GetTweet)
			tweets.GET("/:id/comments", tweetHandler.GetTweetComments)
			tweets.GET("/:id/replies", tweetHandler.GetTweetReplies)

			// 需要认证的接口
			tweets.Use(jwtMW.AuthRequired())
			{
				tweets.POST("", tweetHandler.CreateTweet)
				tweets.DELETE("/:id", tweetHandler.DeleteTweet)
				tweets.POST("/:id/like", tweetHandler.LikeTweet)
				tweets.DELETE("/:id/like", tweetHandler.UnlikeTweet)
				tweets.POST("/:id/retweet", tweetHandler.RetweetTweet)
				tweets.DELETE("/:id/retweet", tweetHandler.UnretweetTweet)
				tweets.POST("/:id/comments", tweetHandler.CreateComment)
				tweets.POST("/:id/bookmark", bookmarkHandler.AddBookmark)
				tweets.DELETE("/:id/bookmark", bookmarkHandler.RemoveBookmark)
			}
		}

		// ---------- 其他服务 (已存在) ----------
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

		// ---------- 恢复之前被覆盖的路由 ----------

		// 媒体上传
		v1.POST("/upload", jwtMW.AuthRequired(), uploadHandler.UploadFile) // UploadFile, not UploadMedia

		// 收藏系统
		bookmarks := v1.Group("/bookmarks")
		bookmarks.Use(jwtMW.AuthRequired())
		{
			bookmarks.GET("", bookmarkHandler.ListBookmarks)
		}

		// 评论相关
		comments := v1.Group("/comments")
		comments.Use(jwtMW.AuthRequired())
		{
			comments.DELETE("/:id", tweetHandler.DeleteComment)
		}

		// 投票相关
		polls := v1.Group("/polls")
		polls.Use(jwtMW.AuthRequired())
		{
			polls.POST("/vote", tweetHandler.VotePoll)
		}

		// 通知系统
		notifications := v1.Group("/notifications")
		notifications.Use(jwtMW.AuthRequired())
		{
			notifications.GET("", notificationHandler.GetNotifications)
			notifications.GET("/unread-count", notificationHandler.GetUnreadCount)
			notifications.PUT("/read", notificationHandler.MarkAsRead)
			notifications.PUT("/read-all", notificationHandler.MarkAllAsRead) // Found this in handler
		}

		// 私信系统 (Messenger)
		messenger := v1.Group("/messenger")
		messenger.Use(jwtMW.AuthRequired())
		{
			messenger.POST("/messages", messengerHandler.SendMessage)
			messenger.GET("/conversations", messengerHandler.GetConversations)
			messenger.GET("/conversations/:peer_id/messages", messengerHandler.GetMessages) // /:peer_id/messages to match handler
		}

		// WebSocket
		v1.GET("/ws", wsHandler.HandleConnection)
	}

	return r
}
