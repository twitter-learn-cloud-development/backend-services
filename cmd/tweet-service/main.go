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
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/infrastructure/persistence"
	followRepository "twitter-clone/internal/module/follow/repository"
	tweetCache "twitter-clone/internal/module/tweet/cache"
	tweetGrpc "twitter-clone/internal/module/tweet/grpc"
	tweetRepository "twitter-clone/internal/module/tweet/repository"
	tweetService "twitter-clone/internal/module/tweet/service"
	"twitter-clone/internal/mq/producer"
	consulConfig "twitter-clone/pkg/config"
	"twitter-clone/pkg/pkg/snowflake"
	"twitter-clone/pkg/registry"
	"twitter-clone/pkg/trace"

	"twitter-clone/pkg/metric"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
)

func main() {
	log.Println("========================================")
	log.Println("🚀 Tweet Service (gRPC)")
	log.Println("========================================")

	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using default/environment config")
	}

	// 🔍 初始化链路追踪
	jaegerHost := getEnv("JAEGER_AGENT_HOST", "localhost")
	trace.InitTracer("tweet-service", jaegerHost)

	// 1. 初始化 Snowflake
	if err := snowflake.Init(1); err != nil {
		log.Fatalf("❌ Failed to init snowflake: %v", err)
	}
	log.Println("✅ Snowflake initialized (Node ID: 1)")

	// 📊 初始化 Prometheus 指标 (Tweet Service uses 2112)
	metric.InitMetrics()
	metric.StartMetricsServer(2112)

	// 4. 初始化 Consul 连接信息
	consulHost := getEnv("CONSUL_HOST", "localhost")
	consulPort := getEnv("CONSUL_PORT", "8500")
	registryAddr := consulHost + ":" + consulPort

	// 2. 初始化数据库
	dbConfig := persistence.DefaultDBConfig()

	// 🆕 Consul Config Client
	var consulConfigClient *consulConfig.ConsulConfigClient
	if client, err := consulConfig.NewConsulConfigClient(registryAddr); err == nil {
		consulConfigClient = client
		// DB Config
		if host, err := client.GetConfig("config/tweet-service/db_host"); err == nil {
			dbConfig.Host = host
			log.Printf("✅ Loaded DB Host from Consul: %s", host)
		}
	} else {
		log.Printf("⚠️ Failed to create Consul client for config: %v", err)
	}

	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect database: %v", err)
	}
	log.Println("✅ Database connected")

	// 3. 自动迁移
	if err := db.AutoMigrate(&domain.Tweet{}, &domain.Follow{}); err != nil {
		log.Fatalf("❌ Failed to migrate database: %v", err)
	}
	log.Println("✅ Database migrated")

	// 4. 初始化 Redis
	redisConfig := cache.DefaultRedisConfig()
	if consulConfigClient != nil {
		if host, err := consulConfigClient.GetConfig("config/tweet-service/redis_host"); err == nil {
			redisConfig.Host = host
			log.Printf("✅ Loaded Redis Host from Consul: %s", host)
		}
	}
	redisClient, err := cache.NewRedis(redisConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect redis: %v", err)
	}
	log.Println("✅ Redis connected")

	// 5. 初始化 RabbitMQ
	mqConfig := mq.DefaultRabbitMQConfig()
	if consulConfigClient != nil {
		if host, err := consulConfigClient.GetConfig("config/tweet-service/mq_host"); err == nil {
			mqConfig.Host = host
			log.Printf("✅ Loaded MQ Host from Consul: %s", host)
		}
	}
	mqClient, err := mq.NewRabbitMQ(mqConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect rabbitmq: %v", err)
	}
	defer mqClient.Close()
	log.Println("✅ RabbitMQ connected")

	// 6. 创建依赖
	tweetRepo := tweetRepository.NewTweetRepository(db)
	followRepo := followRepository.NewFollowRepository(db)
	timelineCache := tweetCache.NewTimelineCache(redisClient)
	eventProducer, err := producer.NewEventProducer(mqClient)
	if err != nil {
		log.Fatalf("❌ Failed to create event producer: %v", err)
	}

	// 7. 创建 Service
	tweetSvc := tweetService.NewTweetService(
		tweetRepo,
		followRepo,
		timelineCache,
		eventProducer,
	)

	// 8. 初始化 Consul 注册中心
	// registryAddr 已初始化

	svcRegistry, err := registry.NewConsulRegistry(registryAddr)
	if err != nil {
		log.Printf("⚠️ Failed to connect to consul: %v", err)
	} else {
		serviceName := getEnv("SERVICE_NAME", "tweet-service")

		// 动态获取容器 IP
		serviceAddr := getLocalIP()
		if serviceAddr == "" {
			serviceAddr = getEnv("SERVICE_ADDR", "localhost")
		}

		servicePortStr := getEnv("SERVICE_PORT", "9092")
		servicePort, _ := strconv.Atoi(servicePortStr)

		// 使用 Hostname 确保 ID 唯一 (tweet-service-node1-9092)
		hostname, _ := os.Hostname()
		serviceID := fmt.Sprintf("%s-%s-%s", serviceName, hostname, servicePortStr)

		tags := []string{"tweet", "grpc"}

		if err := svcRegistry.RegisterService(serviceName, serviceID, serviceAddr, servicePort, tags); err != nil {
			log.Printf("❌ Failed to register service: %v", err)
		} else {
			defer svcRegistry.DeregisterService(serviceID)
		}
	}

	// 9. 创建 gRPC Server
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)
	tweetv1.RegisterTweetServiceServer(grpcServer, tweetGrpc.NewTweetServer(tweetSvc))

	// 📊 注册 gRPC Metrics
	grpc_prometheus.Register(grpcServer)
	grpc_prometheus.EnableHandlingTimeHistogram()

	// 🆕 注册 Reflection（用于 grpcurl）
	reflection.Register(grpcServer)

	// 9. 启动 gRPC 服务
	lis, err := net.Listen("tcp", ":9092")
	if err != nil {
		log.Fatalf("❌ Failed to listen: %v", err)
	}

	log.Println("========================================")
	log.Println("🚀 Tweet Service listening on :9092")
	log.Println("📡 gRPC endpoints:")
	log.Println("   - CreateTweet")
	log.Println("   - GetTweet")
	log.Println("   - DeleteTweet")
	log.Println("   - GetUserTimeline")
	log.Println("   - GetFeeds")
	log.Println("========================================")

	// 10. 优雅关闭
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
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
