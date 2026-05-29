package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	tweetv1 "twitter-clone/api/tweet/v1"
	userv1 "twitter-clone/api/user/v1"
	agentGrpc "twitter-clone/internal/module/agent/grpc"
	agentMcp "twitter-clone/internal/module/agent/mcp"
	"twitter-clone/internal/module/agent/repository"
	agentService "twitter-clone/internal/module/agent/service"
	mongoInfra "twitter-clone/internal/infrastructure/mongo"
	"twitter-clone/internal/infrastructure/persistence"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/infrastructure/mq"
	followRepository "twitter-clone/internal/module/follow/repository"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/qdrant"
	"twitter-clone/pkg/registry"
	"twitter-clone/pkg/profiler"

	_ "github.com/mbobakov/grpc-consul-resolver"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// 启动 Profiler 持续性能监控
	profiler.Init("agent-service")

	log.Println("========================================")
	log.Println("🤖 Agent Service (gRPC + MCP + MongoDB)")
	log.Println("========================================")

	// 0. 初始化 Logger
	logger.InitLogger()
	defer logger.Log.Sync()

	// 加载 .env
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using environment config")
	}

	// 1. 初始化 MongoDB
	mongoInfra.InitMongoDB()
	defer mongoInfra.Close()
	log.Println("✅ MongoDB connected")

	// 创建 AgentRepository 并初始化索引
	repo := repository.NewMongoAgentRepository(mongoInfra.GetDB())
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		log.Printf("⚠️ Failed to ensure MongoDB indexes: %v", err)
	} else {
		log.Println("✅ MongoDB indexes ensured")
	}

	// 2. 初始化 ES 客户端 (高可用降级：允许 ES 离线启动)
	var esClient *es.Client
	if err := es.Init(); err != nil {
		log.Printf("⚠️ Warning: Failed to init elasticsearch: %v. Search features might be limited.", err)
	} else {
		log.Println("✅ Elasticsearch connected")
		esClient = es.GetClient()
	}
	_ = esClient

	// 2.1 Qdrant 向量库初始化
	var qdrantClient *qdrant.Client
	qdrantURL := getEnv("QDRANT_URL", "http://localhost:6333")
	qdrantClient = qdrant.NewClient(qdrantURL)
	log.Println("✅ Qdrant client initialized")
	// 预建 collection (1024 维 cosine 相似度)
	if err := qdrantClient.CreateCollection(context.Background(), "tweets", 1024); err != nil {
		log.Printf("⚠️ Failed to create qdrant collection: %v. Search features might be limited.", err)
	}

	// 3. 初始化 AI Embedding 客户端
	aiClient := ai.NewClient(getEnv("LM_STUDIO_API_URL", "http://localhost:1234/v1"))
	log.Println("✅ AI Embedding client initialized")

	// 3.1 初始化 Reranker
	rerankerType := getEnv("RERANKER_TYPE", "local")
	rerankerApiKey := getEnv("RERANKER_API_KEY", "")
	if rerankerApiKey == "" && strings.ToLower(rerankerType) == "dashscope" {
		rerankerApiKey = getEnv("DASHSCOPE_API_KEY", "")
	}
	reranker := ai.NewReranker(
		rerankerType,
		rerankerApiKey,
		getEnv("RERANKER_API_URL", ""),
		getEnv("RERANKER_MODEL", ""),
	)
	log.Printf("✅ Reranker client initialized (Type: %s)", rerankerType)

	// 4. 连接 tweet-service（通过 Consul 服务发现）
	consulAddrTweetService := getEnv("CONSUL_HOST", "localhost") + ":" + getEnv("CONSUL_PORT", "8500")
	tweetTarget := fmt.Sprintf("consul://%s/tweet-service?healthy=true", consulAddrTweetService)
	tweetConn, err := grpc.NewClient(tweetTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("❌ Failed to connect tweet-service: %v", err)
	}
	defer tweetConn.Close()
	tweetClient := tweetv1.NewTweetServiceClient(tweetConn)
	log.Println("✅ Tweet Service client connected")

	// 4.1 连接 user-service
	userTarget := fmt.Sprintf("consul://%s/user-service?healthy=true", consulAddrTweetService)
	userConn, err := grpc.NewClient(userTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy": "round_robin"}`),
	)
	if err != nil {
		log.Fatalf("❌ Failed to connect user-service: %v", err)
	}
	defer userConn.Close()
	userClient := userv1.NewUserServiceClient(userConn)
	log.Println("✅ User Service client connected")

	// 5. 启动 MCP Server（后台 goroutine）
	mcpAddr := getEnv("MCP_SERVER_ADDR", "0.0.0.0:9200")
	embeddingModel := getEnv("LM_STUDIO_MODEL_EMBEDDING", "text-embedding-bge-m3")
	mcpServer := agentMcp.NewMCPServer(esClient, qdrantClient, aiClient, reranker, tweetClient, userClient, embeddingModel)
	go func() {
		log.Printf("🔧 MCP Server starting on %s", mcpAddr)
		if err := mcpServer.Start(mcpAddr); err != nil {
			log.Fatalf("❌ MCP Server failed: %v", err)
		}
	}()
	log.Println("✅ MCP Server started")

	// 🆕 初始化 MySQL/Redis/RabbitMQ (用于影子风控与舆情播报双轨并行)
	dbConfig := persistence.DefaultDBConfig()
	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect database: %v", err)
	}
	log.Println("✅ Database connected for Agent Service bypass")

	redisConfig := cache.DefaultRedisConfig()
	redisClient, err := cache.NewRedis(redisConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect redis: %v", err)
	}
	log.Println("✅ Redis connected for Agent Service bypass")

	// 6. 初始化 AgentService（注入 Repository, aiClient 和 redisClient）
	svc := agentService.NewAgentService(
		getEnv("DASHSCOPE_API_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		getEnv("DASHSCOPE_API_KEY", ""),
		getEnv("LM_STUDIO_MODEL_CHAT", "qwen3.6-plus"),
		mcpAddr,
		repo,
		aiClient,
		redisClient,
	)
	log.Println("✅ Agent Service initialized (with MongoDB persistence)")

	mqConfig := mq.DefaultRabbitMQConfig()
	mqClient, err := mq.NewRabbitMQ(mqConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect rabbitmq: %v", err)
	}
	log.Println("✅ RabbitMQ connected for Agent Service bypass")

	followRepo := followRepository.NewFollowRepository(db)

	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	_ = backgroundCancel // 保持引用

	// 🆕 初始化 Temporal 客户端 (高可用降级：支持 Temporal 离线启动)
	temporalHost := getEnv("TEMPORAL_HOST", "localhost:7233")
	temporalClient, err := client.Dial(client.Options{
		HostPort: temporalHost,
	})
	if err != nil {
		log.Printf("⚠️ Warning: Failed to connect Temporal Server at %s: %v. Temporal features will be disabled.", temporalHost, err)
	} else {
		log.Printf("✅ Connected to Temporal Server at %s", temporalHost)
		defer temporalClient.Close()

		// 🆕 初始化 Temporal Activities
		chatModelCheap := getEnv("LM_STUDIO_MODEL_CHAT", "qwen3.6-plus")
		chatModelPremium := getEnv("PREMIUM_AI_MODEL_CHAT", "qwen-max")
		botUserIDStr := getEnv("TRENDING_BOT_USER_ID", "100")
		botUserID, _ := strconv.ParseUint(botUserIDStr, 10, 64)
		if botUserID == 0 {
			botUserID = 100
		}
		embeddingModel = getEnv("LM_STUDIO_MODEL_EMBEDDING", "text-embedding-bge-m3")

		activities := agentService.NewAgentActivities(
			db,
			redisClient,
			esClient,
			qdrantClient,
			aiClient,
			tweetClient,
			followRepo,
			embeddingModel,
			chatModelCheap,
			chatModelPremium,
			botUserID,
		)

		// 🆕 注册 Worker 并运行
		temporalWorker := worker.New(temporalClient, "AGENT_TASK_QUEUE", worker.Options{})
		temporalWorker.RegisterWorkflow(agentService.TweetRiskControlWorkflow)
		temporalWorker.RegisterWorkflow(agentService.TrendingReporterWorkflow)
		temporalWorker.RegisterActivity(activities)

		go func() {
			log.Println("👷 Temporal Worker starting to process queues...")
			if err := temporalWorker.Run(worker.InterruptCh()); err != nil {
				log.Fatalf("❌ Temporal Worker failed: %v", err)
			}
		}()

		// 🆕 启动反作弊影子风控 MQ 监听器（其底层会向 Temporal 发起风控工作流）
		riskControl := agentService.NewRiskControl(mqClient, temporalClient)
		go riskControl.Start(backgroundCtx)

		// 🆕 启动常驻的周期性舆情监控自愈工作流
		reporterOptions := client.StartWorkflowOptions{
			ID:        "TrendingReporter-Sentinel",
			TaskQueue: "AGENT_TASK_QUEUE",
		}
		_, err = temporalClient.ExecuteWorkflow(backgroundCtx, reporterOptions, agentService.TrendingReporterWorkflow, 1*time.Minute)
		if err != nil {
			if !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
				log.Printf("⚠️ Failed to trigger TrendingReporter workflow: %v", err)
			} else {
				log.Println("ℹ️ TrendingReporter workflow is already running")
			}
		} else {
			log.Println("🚀 Handoff TrendingReporter to Temporal Workflow Engine successfully!")
		}
	}

	// 7. 注册 Consul
	consulAddr := getEnv("CONSUL_HOST", "localhost") + ":" + getEnv("CONSUL_PORT", "8500")
	svcRegistry, err := registry.NewConsulRegistry(consulAddr)
	if err != nil {
		log.Printf("⚠️ Failed to connect consul: %v", err)
	} else {
		serviceName := getEnv("SERVICE_NAME", "agent-service")
		serviceAddr := getLocalIP()
		if serviceAddr == "" {
			serviceAddr = getEnv("SERVICE_ADDR", "localhost")
		}
		servicePortStr := getEnv("SERVICE_PORT", "9100")
		servicePort, _ := strconv.Atoi(servicePortStr)
		hostname, _ := os.Hostname()
		serviceID := fmt.Sprintf("%s-%s-%s", serviceName, hostname, servicePortStr)

		if err := svcRegistry.RegisterService(serviceName, serviceID, serviceAddr, servicePort, []string{"agent", "grpc"}); err != nil {
			log.Printf("❌ Failed to register service: %v", err)
		} else {
			defer svcRegistry.DeregisterService(serviceID)
		}
	}

	// 🆕 实例化并启动热点播报姬后台定时任务 (发总结的 AI 助手)
	chatModelCheap := getEnv("LM_STUDIO_MODEL_CHAT", "qwen3.6-plus")
	botUserIDStr := getEnv("TRENDING_BOT_USER_ID", "100")
	botUserID, _ := strconv.ParseUint(botUserIDStr, 10, 64)
	if botUserID == 0 {
		botUserID = 100
	}
	embeddingModel = getEnv("LM_STUDIO_MODEL_EMBEDDING", "text-embedding-bge-m3")

	reporter := agentService.NewTrendingReporter(
		redisClient,
		esClient,
		qdrantClient,
		aiClient,
		tweetClient,
		embeddingModel,
		chatModelCheap,
		botUserID,
	)
	go reporter.Start(backgroundCtx, 5*time.Minute)
	log.Println("✅ Trending Reporter (AI summary assistant) background task spawned successfully")

	// 8. 启动 gRPC Server
	grpcServer := grpc.NewServer()
	aiAgentv1.RegisterAiAgentServiceServer(grpcServer, agentGrpc.NewAgentServer(svc))
	reflection.Register(grpcServer)

	grpcPort := getEnv("SERVICE_PORT", "9100")
	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("❌ Failed to listen: %v", err)
	}

	log.Println("========================================")
	log.Printf("🚀 Agent Service listening on :%s", grpcPort)
	log.Println("📡 gRPC endpoints:")
	log.Println("   - CallApiOfAi          (模式一：直接对话)")
	log.Println("   - ConsultContent        (模式二：RAG 搜索)")
	log.Println("   - AssistPublishTwitter  (模式三：AI 写推文)")
	log.Println("   - ConfirmPublishTwitter (模式三：确认发布)")
	log.Println("   - MultiAgentPublishTwitter (模式四：多Agent协作)")
	log.Println("   - GetRepositoryDialogue (对话历史列表)")
	log.Println("   - GetDialogueDetail     (对话消息详情)")
	log.Println("📦 MongoDB: dialogue persistence enabled")
	log.Println("========================================")

	// 9. 优雅关闭
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("❌ Failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down server...")
	backgroundCancel() // 停止影子风控与舆情播报姬后台 Worker
	grpcServer.GracefulStop()
	if svc != nil {
		svc.Close() // 🆕 关闭 AgentService 关联的 MCP 长连接及生命周期 Context
	}
	if mqClient != nil {
		mqClient.Close()
	}
	log.Println("✅ Server exited")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
