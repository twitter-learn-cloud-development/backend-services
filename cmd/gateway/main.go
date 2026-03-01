package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/gateway/client"
	"twitter-clone/internal/gateway/config"
	"twitter-clone/internal/gateway/handler"
	"twitter-clone/internal/gateway/middleware"
	"twitter-clone/internal/gateway/router"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/infrastructure/persistence"
	consulConfig "twitter-clone/pkg/config"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/metric"
	"twitter-clone/pkg/pkg/snowflake"
	"twitter-clone/pkg/trace"

	"github.com/gin-gonic/gin"
)

func main() {
	log.Println("========================================")
	log.Println("🚀 Twitter Clone - API Gateway")
	log.Println("========================================")

	// 0. 初始化 Logger (TraceID 支持)
	logger.InitLogger()

	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using default/environment config")
	}

	// 加载配置
	cfg := config.LoadGatewayConfig()
	log.Printf("✅ Configuration loaded")

	// 📊 初始化 Prometheus 指标
	metric.InitMetrics()
	log.Println("✅ Prometheus metrics initialized")

	// 🆕 初始化 Consul 配置客户端
	consulClient, err := consulConfig.NewConsulConfigClient(cfg.Consul.Address)
	if err != nil {
		log.Printf("⚠️ Failed to connect to Consul for config: %v", err)
	} else {
		// 尝试从 Consul 获取 JWT Secret
		secret, err := consulClient.GetConfig("config/global/jwt_secret")
		if err == nil && secret != "" {
			log.Println("✅ Loaded JWT Secret from Consul")
			cfg.JWTSecret = secret
		} else {
			log.Println("⚠️ Failed to load JWT Secret from Consul, using default/env")
		}
	}

	// 🔍 初始化链路追踪
	jaegerEndpoint := getEnv("JAEGER_COLLECTOR_ENDPOINT", "http://localhost:14268/api/traces")
	trace.InitTracer("gateway", jaegerEndpoint)

	// 4. 初始化 Redis (用于限流)
	redisConfig := cache.DefaultRedisConfig()
	// 尝试从 Consul 获取 Redis 配置 (可选)
	if consulClient != nil {
		if host, err := consulClient.GetConfig("config/global/redis_host"); err == nil {
			redisConfig.Host = host
		}
	}
	redisClient, err := cache.NewRedis(redisConfig)
	if err != nil {
		log.Printf("⚠️ Failed to connect to Redis for Rate Limiting: %v", err)
	} else {
		log.Println("✅ Redis connected (Rate Limiting)")
	}

	// 🛡️ 初始化 Sentinel (熔断降级)
	config.InitSentinel()

	// 连接 gRPC 服务
	log.Println("📡 Connecting to gRPC services via Consul...")
	grpcClients, err := client.NewGRPCClients(cfg.Consul.Address)
	if err != nil {
		log.Fatalf("❌ Failed to connect to gRPC services: %v", err)
	}
	defer grpcClients.Close()

	// 创建 JWT 中间件
	jwtMW := middleware.NewJWTMiddleware(cfg.JWTSecret, cfg.JWTExpire)
	log.Println("✅ JWT middleware initialized")

	// 📊 注册 Prometheus 中间件
	r := gin.New()
	r.Use(middleware.MetricsMiddleware())
	// 注意：router.SetupRouter 内部创建了 gin.New()，我们需要调整方式。
	// 这里最好传入 r 给 SetupRouter，或者修改 SetupRouter。
	// 鉴于 router.SetupRouter 是现成的，我们修改 SetupRouter 参数或者在 main 中包装。
	// 简单起见，修改 router.go 更好，但为了不改 router.go 签名，我们可以把 SetupRouter 返回的 r 再包装？
	// 最好是先把 SetupRouter 也是用 r.Use。
	// 哎呀，router.SetupRouter 内部 new 了一个 gin。
	// 让我们看看 router.go。

	// 🆕 启动配置监听 (Hot Reload)
	if consulClient != nil {
		consulClient.WatchConfig("config/global/jwt_secret", func(newSecret string) {
			log.Println("🔄 Hot Reload: JWT Secret updated")
			jwtMW.SetSecret(newSecret)
		})
	}

	// 初始化 Snowflake ID 生成器 (书签等功能需要)
	snowflake.MustInit(1)
	log.Println("✅ Snowflake ID generator initialized")

	// 初始化数据库 (通知/书签等需要直接查询DB)
	dbConfig := persistence.DefaultDBConfig()
	if consulClient != nil {
		if host, err := consulClient.GetConfig("config/gateway/db_host"); err == nil {
			dbConfig.Host = host
		}
	}
	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Printf("⚠️ Failed to connect to DB for notifications: %v", err)
	} else {
		// 自动迁移通知/书签表
		db.AutoMigrate(&domain.Notification{}, &domain.Bookmark{}, &domain.Like{}, &domain.Retweet{})
		log.Println("✅ Database connected (Notifications/Bookmarks)")
	}

	// 创建处理器
	// The instruction provided a snippet that seems to redefine clients,
	// but we already have grpcClients. Let's adapt it to use existing clients.
	// The instruction also had a typo in uploadHandler, fixing it.
	userHandler := handler.NewUserHandler(grpcClients.UserClient, grpcClients.FollowClient, grpcClients.TweetClient)
	tweetHandler := handler.NewTweetHandler(grpcClients.TweetClient, grpcClients.UserClient, db)
	followHandler := handler.NewFollowHandler(grpcClients.FollowClient)
	uploadHandler := handler.NewUploadHandler("./uploads", "http://localhost:"+cfg.Port) // MVP: Local upload

	// 通知/书签处理器 (需要 DB)
	var notificationHandler *handler.NotificationHandler
	var bookmarkHandler *handler.BookmarkHandler
	if db != nil {
		notificationHandler = handler.NewNotificationHandler(db, grpcClients.UserClient)
		bookmarkHandler = handler.NewBookmarkHandler(db, grpcClients.TweetClient, grpcClients.UserClient)
	}

	// 私信处理器 (gRPC)
	messengerHandler := handler.NewMessengerHandler(grpcClients.MessengerClient, grpcClients.UserClient)

	// 创建 WebSocket 处理器
	wsHandler := handler.NewWebSocketHandler(redisClient, jwtMW)

	log.Println("✅ Handlers initialized")

	// 设置路由
	// 传入 Redis Client 用于限流
	r = router.SetupRouter(tweetHandler, followHandler, userHandler, uploadHandler, notificationHandler, bookmarkHandler, messengerHandler, wsHandler, jwtMW, redisClient)

	log.Println("✅ Router configured")

	// 创建 HTTP 服务器
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	// 在 goroutine 中启动服务器
	go func() {
		log.Println("========================================")
		log.Printf("🌐 API Gateway listening on :%s", cfg.Port)
		log.Println("========================================")
		log.Println("📋 Available endpoints:")
		log.Println("   Health:")
		log.Println("     GET  /health")
		log.Println("")
		log.Println("   Authentication:")
		log.Println("     POST /api/v1/auth/register")
		log.Println("     POST /api/v1/auth/login")
		log.Println("")
		log.Println("   Users:")
		log.Println("     GET  /api/v1/users/:id")
		log.Println("     GET  /api/v1/users/me          (auth required)")
		log.Println("     PUT  /api/v1/users/me          (auth required)")
		log.Println("")
		log.Println("   Tweets:")
		log.Println("     POST /api/v1/tweets             (auth required)")
		log.Println("     GET  /api/v1/tweets/:id")
		log.Println("     DELETE /api/v1/tweets/:id       (auth required)")
		log.Println("     GET  /api/v1/users/:id/timeline")
		log.Println("")
		log.Println("   Feeds:")
		log.Println("     GET  /api/v1/feeds              (auth required)")
		log.Println("")
		log.Println("   Follows:")
		log.Println("     POST /api/v1/follows            (auth required)")
		log.Println("     DELETE /api/v1/follows/:id      (auth required)")
		log.Println("     GET  /api/v1/follows/:id/status (auth required)")
		log.Println("     GET  /api/v1/users/:id/followers")
		log.Println("     GET  /api/v1/users/:id/followees")
		log.Println("     GET  /api/v1/users/:id/stats")
		log.Println("========================================")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Failed to start server: %v", err)
		}
	}()

	// 等待中断信号以优雅地关闭服务器
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("\n🛑 Shutting down server...")

	// 5 秒超时关闭
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("❌ Server forced to shutdown: %v", err)
	}

	log.Println("✅ Server exited")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
