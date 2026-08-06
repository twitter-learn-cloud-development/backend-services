package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"twitter-clone/internal/gateway/client"
	"twitter-clone/internal/gateway/config"
	"twitter-clone/internal/gateway/handler"
	"twitter-clone/internal/gateway/middleware"
	"twitter-clone/internal/gateway/router"
	"twitter-clone/internal/infrastructure/cache"
	agentMarketplace "twitter-clone/internal/module/agent/marketplace"
	agentProfile "twitter-clone/internal/module/agent/profile"
	consulConfig "twitter-clone/pkg/config"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/metric"
	"twitter-clone/pkg/pkg/snowflake"
	"twitter-clone/pkg/profiler"
	"twitter-clone/pkg/trace"
)

func main() {
	// 启动 Profiler 持续性能监控
	profiler.Init("api-gateway")

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
	metric.StartMetricsServer(2110)
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
	jaegerEndpoint := getEnv("JAEGER_COLLECTOR_ENDPOINT", "localhost:4317")
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
		// 🎯 [Hot Reload] 启动自举报拉取与 PubSub 动态监听器，解决配置脑裂
		cache.StartConfigListener(context.Background(), redisClient)
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

	//初始化auth认证中间件

	authServiceJWKSUrl := getEnv("AUTH_SERVICE_JWKS_URL", "http://localhost:8081/.well-known/jwks.json")
	authMW, err := middleware.NewGatewayAuthMiddleware(authServiceJWKSUrl)
	if err != nil {
		log.Fatalf("Init gateway jwks middleware failed: %v", err)
	}

	// // 创建 JWT 中间件
	// jwtMW := middleware.NewJWTMiddleware(cfg.JWTSecret, cfg.JWTExpire)
	// log.Println("✅ JWT middleware initialized")

	// // 🆕 启动配置监听 (Hot Reload)
	// if consulClient != nil {
	// 	consulClient.WatchConfig("config/global/jwt_secret", func(newSecret string) {
	// 		log.Println("🔄 Hot Reload: JWT Secret updated")
	// 		jwtMW.SetSecret(newSecret)
	// 	})
	// }

	// 初始化 Snowflake ID 生成器 (书签等功能需要)
	snowflake.MustInit(redisClient)
	log.Println("✅ Snowflake ID generator initialized")

	// 创建处理器
	// The instruction provided a snippet that seems to redefine clients,
	// but we already have grpcClients. Let's adapt it to use existing clients.
	// The instruction also had a typo in uploadHandler, fixing it.
	userHandler := handler.NewUserHandler(grpcClients.UserClient, grpcClients.FollowClient, grpcClients.TweetClient, grpcClients.AuthClient)
	tweetHandler := handler.NewTweetHandler(grpcClients.TweetClient, grpcClients.UserClient)
	followHandler := handler.NewFollowHandler(grpcClients.FollowClient)
	minioEndpoint := getEnv("MINIO_ENDPOINT", "localhost:9000")
	minioAccessKey := getEnv("MINIO_ACCESS_KEY", "admin")
	minioSecretKey := getEnv("MINIO_SECRET_KEY", "31415927")
	minioBucket := getEnv("MINIO_BUCKET", "twitter-media")
	minioPublicURL := getEnv("MINIO_PUBLIC_URL", "http://localhost:9000")
	uploadHandler := handler.NewUploadHandler(minioEndpoint, minioAccessKey, minioSecretKey, minioBucket, minioPublicURL)

	// 通知/书签处理器
	notificationHandler := handler.NewNotificationHandler(grpcClients.NotificationClient, grpcClients.UserClient)
	bookmarkHandler := handler.NewBookmarkHandler(grpcClients.TweetClient, grpcClients.UserClient)

	// 私信处理器 (gRPC)
	messengerHandler := handler.NewMessengerHandler(grpcClients.MessengerClient, grpcClients.UserClient)

	profileAdminToken := strings.TrimSpace(getEnv(agentProfile.AdminTokenEnv, ""))
	profileViewerIDs, profileAdminErr := parseProfileUserIDs(getEnv(agentProfile.ViewerUserIDsEnv, ""), "viewer")
	if profileAdminErr != nil {
		log.Fatalf("invalid %s: %v", agentProfile.ViewerUserIDsEnv, profileAdminErr)
	}
	profileEditorIDs, profileAdminErr := parseProfileUserIDs(getEnv(agentProfile.EditorUserIDsEnv, ""), "editor")
	if profileAdminErr != nil {
		log.Fatalf("invalid %s: %v", agentProfile.EditorUserIDsEnv, profileAdminErr)
	}
	profileApproverIDs, profileAdminErr := parseProfileUserIDs(getEnv(agentProfile.ApproverUserIDsEnv, ""), "approver")
	if profileAdminErr != nil {
		log.Fatalf("invalid %s: %v", agentProfile.ApproverUserIDsEnv, profileAdminErr)
	}
	profileAdministratorIDs, profileAdminErr := parseProfileUserIDs(getEnv(agentProfile.AdminUserIDsEnv, ""), "administrator")
	if profileAdminErr != nil {
		log.Fatalf("invalid %s: %v", agentProfile.AdminUserIDsEnv, profileAdminErr)
	}
	profileRoleCount := len(profileViewerIDs) + len(profileEditorIDs) + len(profileApproverIDs) + len(profileAdministratorIDs)
	if (profileAdminToken == "") != (profileRoleCount == 0) {
		log.Fatalf("%s and at least one Agent Profile role user list must be configured together", agentProfile.AdminTokenEnv)
	}
	if profileAdminToken != "" && len(profileAdminToken) < 32 {
		log.Fatalf("%s must contain at least 32 characters", agentProfile.AdminTokenEnv)
	}
	profileDirectPublishEnabled, profileAdminErr := strconv.ParseBool(getEnv(agentProfile.DirectPublishEnabledEnv, "false"))
	if profileAdminErr != nil {
		log.Fatalf("invalid %s: %v", agentProfile.DirectPublishEnabledEnv, profileAdminErr)
	}
	if profileDirectPublishEnabled && len(profileAdministratorIDs) == 0 {
		log.Fatalf("%s requires at least one %s", agentProfile.DirectPublishEnabledEnv, agentProfile.AdminUserIDsEnv)
	}
	agentHandlerOptions := []handler.AgentHandlerOption{}
	if profileAdminToken != "" {
		agentHandlerOptions = append(agentHandlerOptions, handler.WithProfileManagementRoles(
			profileAdminToken, profileViewerIDs, profileEditorIDs, profileApproverIDs,
			profileAdministratorIDs, profileDirectPublishEnabled,
		))
	}
	extensionMarketplaceAdminEnabled, marketplaceAdminErr := strconv.ParseBool(
		getEnv(agentMarketplace.AdministrationEnabledEnv, "false"),
	)
	if marketplaceAdminErr != nil {
		log.Fatalf("invalid %s: %v", agentMarketplace.AdministrationEnabledEnv, marketplaceAdminErr)
	}
	extensionMarketplaceAdminToken := strings.TrimSpace(getEnv(agentMarketplace.AdministrationTokenEnv, ""))
	if extensionMarketplaceAdminEnabled && len(extensionMarketplaceAdminToken) < 32 {
		log.Fatalf("%s must contain at least 32 characters when administration is enabled", agentMarketplace.AdministrationTokenEnv)
	}
	if !extensionMarketplaceAdminEnabled && extensionMarketplaceAdminToken != "" {
		log.Fatalf("%s requires %s=true", agentMarketplace.AdministrationTokenEnv, agentMarketplace.AdministrationEnabledEnv)
	}
	if extensionMarketplaceAdminEnabled {
		agentHandlerOptions = append(agentHandlerOptions, handler.WithExtensionMarketplaceAdministration(extensionMarketplaceAdminToken))
	}
	agentHandler := handler.NewAgentHandler(grpcClients.AgentClient, agentHandlerOptions...)

	// 创建 WebSocket 处理器
	wsHandler := handler.NewWebSocketHandler(redisClient, authMW)

	log.Println("✅ Handlers initialized")

	// 设置路由
	// 传入 Redis Client 用于限流
	r := router.SetupRouter(tweetHandler, followHandler, userHandler, uploadHandler, notificationHandler, bookmarkHandler, messengerHandler, wsHandler, agentHandler, authMW, redisClient)

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

func parseProfileUserIDs(raw, role string) ([]uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	seen := make(map[uint64]struct{})
	result := make([]uint64, 0)
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		userID, err := strconv.ParseUint(value, 10, 64)
		if err != nil || userID == 0 {
			return nil, fmt.Errorf("invalid %s user id %q", role, value)
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		result = append(result, userID)
	}
	return result, nil
}
