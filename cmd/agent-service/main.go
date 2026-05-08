package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	tweetv1 "twitter-clone/api/tweet/v1"
	agentGrpc "twitter-clone/internal/module/agent/grpc"
	agentMcp "twitter-clone/internal/module/agent/mcp"
	agentService "twitter-clone/internal/module/agent/service"
	"twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/registry"

	_ "github.com/mbobakov/grpc-consul-resolver"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	log.Println("========================================")
	log.Println("🤖 Agent Service (gRPC + MCP)")
	log.Println("========================================")

	// 0. 初始化 Logger
	logger.InitLogger()
	defer logger.Log.Sync()

	// 加载 .env
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using environment config")
	}

	// 1. 初始化 ES 客户端
	if err := es.Init(); err != nil {
		log.Fatalf("❌ Failed to init elasticsearch: %v", err)
	}
	log.Println("✅ Elasticsearch connected")
	esClient := es.GetClient()

	// 2. 初始化 AI Embedding 客户端
	aiClient := ai.NewClient(getEnv("LM_STUDIO_API_URL", "http://localhost:1234/v1"))
	log.Println("✅ AI Embedding client initialized")

	// 连接 tweet-service
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
	// 3. 启动 MCP Server（后台 goroutine）
	mcpAddr := getEnv("MCP_SERVER_ADDR", "0.0.0.0:9200")
	embeddingModel := getEnv("LM_STUDIO_MODEL_EMBEDDING", "text-embedding-bge-m3")
	mcpServer := agentMcp.NewMCPServer(esClient, aiClient, tweetClient, embeddingModel)
	go func() {
		log.Printf("🔧 MCP Server starting on %s", mcpAddr)
		if err := mcpServer.Start(mcpAddr); err != nil {
			log.Fatalf("❌ MCP Server failed: %v", err)
		}
	}()
	log.Println("✅ MCP Server started")

	// 4. 初始化 AgentService
	svc := agentService.NewAgentService(
		getEnv("DASHSCOPE_API_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
		getEnv("DASHSCOPE_API_KEY", ""),
		getEnv("LM_STUDIO_MODEL_CHAT", "qwen3.6-plus"),
		mcpAddr,
	)
	log.Println("✅ Agent Service initialized")

	// 5. 注册 Consul
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

	// 6. 启动 gRPC Server
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
	log.Println("   - CallApiOfAi")
	log.Println("   - ConsultContent")
	log.Println("   - AssistPublishTwitter")
	log.Println("========================================")

	// 7. 优雅关闭
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("❌ Failed to serve: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down server...")
	grpcServer.GracefulStop()
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
