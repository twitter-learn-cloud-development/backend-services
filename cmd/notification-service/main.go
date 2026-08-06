package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	notificationv1 "twitter-clone/api/notification/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/infrastructure/persistence"
	notificationGrpc "twitter-clone/internal/module/notification/grpc"
	"twitter-clone/internal/module/notification/repository"
	"twitter-clone/internal/module/notification/worker"
	consulConfig "twitter-clone/pkg/config"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/pkg/snowflake"
	"twitter-clone/pkg/registry"
	"twitter-clone/pkg/trace"
)

func main() {
	log.Println("========================================")
	log.Println("🚀 Notification Service (gRPC & Worker)")
	log.Println("========================================")

	// 0. 初始化 Logger
	logger.InitLogger()
	defer logger.Log.Sync()

	// 加载 .env
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using default/environment config")
	}

	// 🔍 初始化链路追踪
	jaegerEndpoint := getEnv("JAEGER_COLLECTOR_ENDPOINT", "localhost:4317")
	trace.InitTracer("notification-service", jaegerEndpoint)

	// 2. Consul Config (可选)
	consulHost := getEnv("CONSUL_HOST", "localhost")
	consulPort := getEnv("CONSUL_PORT", "8500")
	registryAddr := consulHost + ":" + consulPort

	var consulConfigClient *consulConfig.ConsulConfigClient
	if client, err := consulConfig.NewConsulConfigClient(registryAddr); err == nil {
		consulConfigClient = client
	}

	// 3. Database
	dbConfig := persistence.DefaultDBConfig()
	if consulConfigClient != nil {
		if host, err := consulConfigClient.GetConfig("config/notification-service/db_host"); err == nil {
			dbConfig.Host = host
		}
	}
	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect database: %v", err)
	}

	// 自动迁移
	if err := db.AutoMigrate(&domain.Notification{}); err != nil {
		log.Fatalf("❌ Failed to migrate database: %v", err)
	}
	log.Println("✅ Database migrated")

	// 4. Redis
	redisConfig := cache.DefaultRedisConfig()
	if consulConfigClient != nil {
		if host, err := consulConfigClient.GetConfig("config/notification-service/redis_host"); err == nil {
			redisConfig.Host = host
		}
	}
	redisClient, err := cache.NewRedis(redisConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect redis: %v", err)
	}

	// 1. 初始化 Snowflake
	// 初始化 Snowflake
	snowflake.MustInit(redisClient)
	log.Println("✅ Snowflake initialized (Node ID: 2)")

	// 5. RabbitMQ
	mqConfig := mq.DefaultRabbitMQConfig()
	if consulConfigClient != nil {
		if host, err := consulConfigClient.GetConfig("config/notification-service/mq_host"); err == nil {
			mqConfig.Host = host
		}
	}
	mqClient, err := mq.NewRabbitMQ(mqConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect rabbitmq: %v", err)
	}
	defer mqClient.Close()

	// 6. Dependencies
	repo := repository.NewNotificationRepository(db)
	consumer := worker.NewConsumer(mqClient, repo, redisClient)

	// 7. Start Consumer (in background)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		log.Fatalf("❌ Failed to start consumer: %v", err)
	}
	log.Println("✅ Notification MQ Consumer started")

	// 8. 初始化 Consul 注册中心并注册 gRPC 服务
	servicePortStr := getEnv("SERVICE_PORT", "9095")
	servicePort, _ := strconv.Atoi(servicePortStr)
	serviceName := getEnv("SERVICE_NAME", "notification-service")

	svcRegistry, err := registry.NewConsulRegistry(registryAddr)
	if err != nil {
		log.Printf("⚠️ Failed to connect to consul: %v", err)
	} else {
		// 动态获取容器 IP
		serviceAddr := getLocalIP()
		if serviceAddr == "" {
			serviceAddr = getEnv("SERVICE_ADDR", "localhost")
		}

		hostname, _ := os.Hostname()
		serviceID := fmt.Sprintf("%s-%s-%s", serviceName, hostname, servicePortStr)
		tags := []string{"notification", "grpc"}

		if err := svcRegistry.RegisterService(serviceName, serviceID, serviceAddr, servicePort, tags); err != nil {
			log.Printf("❌ Failed to register service: %v", err)
		} else {
			log.Printf("✅ Notification Service registered to Consul: %s", serviceID)
			defer svcRegistry.DeregisterService(serviceID)
		}
	}

	// 9. 创建 gRPC Server
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	notificationv1.RegisterNotificationServiceServer(grpcServer, notificationGrpc.NewNotificationServer(repo))
	reflection.Register(grpcServer)

	// 10. 监听并启动 gRPC 服务
	lis, err := net.Listen("tcp", ":"+servicePortStr)
	if err != nil {
		log.Fatalf("❌ Failed to listen on port %s: %v", servicePortStr, err)
	}

	go func() {
		log.Printf("🌐 Notification Service listening on :%s", servicePortStr)
		if err := grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			log.Fatalf("❌ Failed to serve gRPC: %v", err)
		}
	}()

	// 11. Wait for signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down notification service...")
	grpcServer.GracefulStop()
	// 给一点时间让正在处理的消息和连接完成
	time.Sleep(1 * time.Second)
	log.Println("✅ Service exited")
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
