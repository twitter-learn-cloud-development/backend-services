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

	followv1 "twitter-clone/api/follow/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/infrastructure/persistence"
	followGrpc "twitter-clone/internal/module/follow/grpc"
	followRepository "twitter-clone/internal/module/follow/repository"
	followService "twitter-clone/internal/module/follow/service"
	tweetCache "twitter-clone/internal/module/tweet/cache"
	tweetRepository "twitter-clone/internal/module/tweet/repository"
	"twitter-clone/internal/mq/producer"
	"twitter-clone/pkg/pkg/snowflake"
	"twitter-clone/pkg/registry"
	"twitter-clone/pkg/trace"

	"twitter-clone/pkg/metric"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
)

func main() {
	log.Println("========================================")
	log.Println("🚀 Follow Service (gRPC)")
	log.Println("========================================")

	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using default/environment config")
	}

	// 🔍 初始化链路追踪
	jaegerHost := getEnv("JAEGER_AGENT_HOST", "localhost")
	trace.InitTracer("follow-service", jaegerHost)

	// 1. 初始化 Snowflake
	if err := snowflake.Init(1); err != nil {
		log.Fatalf("❌ Failed to init snowflake: %v", err)
	}
	log.Println("✅ Snowflake initialized (Node ID: 1)")

	// 📊 初始化 Prometheus 指标 (Follow Service uses 2113)
	metric.InitMetrics()
	metric.StartMetricsServer(2113)

	// 2. 初始化数据库
	dbConfig := persistence.DefaultDBConfig()
	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect database: %v", err)
	}
	log.Println("✅ Database connected")

	// 3. 自动迁移
	if err := db.AutoMigrate(&domain.Follow{}, &domain.Tweet{}); err != nil {
		log.Fatalf("❌ Failed to migrate database: %v", err)
	}
	log.Println("✅ Database migrated")

	// 4. 初始化 Redis
	redisConfig := cache.DefaultRedisConfig()
	redisClient, err := cache.NewRedis(redisConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect redis: %v", err)
	}
	log.Println("✅ Redis connected")

	// 5. 初始化 RabbitMQ
	mqConfig := mq.DefaultRabbitMQConfig()
	mqClient, err := mq.NewRabbitMQ(mqConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect rabbitmq: %v", err)
	}
	defer mqClient.Close()
	log.Println("✅ RabbitMQ connected")

	// 6. 创建依赖
	followRepo := followRepository.NewFollowRepository(db)
	tweetRepo := tweetRepository.NewTweetRepository(db)
	timelineCache := tweetCache.NewTimelineCache(redisClient)
	eventProducer, err := producer.NewEventProducer(mqClient)
	if err != nil {
		log.Fatalf("❌ Failed to create event producer: %v", err)
	}

	// 7. 创建 Service
	followSvc := followService.NewFollowService(
		followRepo,
		tweetRepo,
		timelineCache,
		eventProducer,
	)

	// 8. 初始化 Consul 注册中心
	consulHost := getEnv("CONSUL_HOST", "localhost")
	consulPort := getEnv("CONSUL_PORT", "8500")
	registryAddr := consulHost + ":" + consulPort

	svcRegistry, err := registry.NewConsulRegistry(registryAddr)
	if err != nil {
		log.Printf("⚠️ Failed to connect to consul: %v", err)
	} else {
		serviceName := getEnv("SERVICE_NAME", "follow-service")

		// 动态获取容器 IP
		serviceAddr := getLocalIP()
		if serviceAddr == "" {
			serviceAddr = getEnv("SERVICE_ADDR", "localhost")
		}

		servicePortStr := getEnv("SERVICE_PORT", "9093")
		servicePort, _ := strconv.Atoi(servicePortStr)

		// 使用 Hostname 确保 ID 唯一
		hostname, _ := os.Hostname()
		serviceID := fmt.Sprintf("%s-%s-%s", serviceName, hostname, servicePortStr)

		tags := []string{"follow", "grpc"}

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
	followv1.RegisterFollowServiceServer(grpcServer, followGrpc.NewFollowServer(followSvc))

	// 📊 注册 gRPC Metrics
	grpc_prometheus.Register(grpcServer)
	grpc_prometheus.EnableHandlingTimeHistogram()

	// 🆕 注册 Reflection（用于 grpcurl）
	reflection.Register(grpcServer)

	// 9. 启动 gRPC 服务
	lis, err := net.Listen("tcp", ":9093")
	if err != nil {
		log.Fatalf("❌ Failed to listen: %v", err)
	}

	log.Println("========================================")
	log.Println("🚀 Follow Service listening on :9093")
	log.Println("📡 gRPC endpoints:")
	log.Println("   - Follow")
	log.Println("   - Unfollow")
	log.Println("   - IsFollowing")
	log.Println("   - GetFollowers")
	log.Println("   - GetFollowees")
	log.Println("   - GetFollowStats")
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
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
