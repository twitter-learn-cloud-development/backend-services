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

	messengerv1 "twitter-clone/api/messenger/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/infrastructure/persistence"
	messengerRepo "twitter-clone/internal/module/messenger/repository"
	messengerService "twitter-clone/internal/module/messenger/service"
	consulConfig "twitter-clone/pkg/config"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/pkg/snowflake"
	"twitter-clone/pkg/registry"
	"twitter-clone/pkg/trace"

	"twitter-clone/pkg/metric"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
)

func main() {
	log.Println("========================================")
	log.Println("🚀 Messenger Service (gRPC)")
	log.Println("========================================")

	// 0. 初始化 Logger
	logger.InitLogger()

	// 加载 .env
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using default/environment config")
	}

	// 🔍 初始化链路追踪
	jaegerEndpoint := getEnv("JAEGER_COLLECTOR_ENDPOINT", "http://localhost:14268/api/traces")
	trace.InitTracer("messenger-service", jaegerEndpoint)

	// 1. 初始化 Snowflake (Node ID: 4, 假设 1=tweet, 2=user, 3=follow)
	if err := snowflake.Init(4); err != nil {
		log.Fatalf("❌ Failed to init snowflake: %v", err)
	}
	log.Println("✅ Snowflake initialized (Node ID: 4)")

	// 📊 初始化 Prometheus 指标 (Port 2115)
	metric.InitMetrics()
	metric.StartMetricsServer(2115)

	// 4. 初始化 Consul
	consulHost := getEnv("CONSUL_HOST", "localhost")
	consulPort := getEnv("CONSUL_PORT", "8500")
	registryAddr := consulHost + ":" + consulPort

	// 2. 初始化数据库
	dbConfig := persistence.DefaultDBConfig()
	var consulConfigClient *consulConfig.ConsulConfigClient
	if client, err := consulConfig.NewConsulConfigClient(registryAddr); err == nil {
		consulConfigClient = client
		if host, err := client.GetConfig("config/messenger-service/db_host"); err == nil {
			dbConfig.Host = host
		}
	}

	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect database: %v", err)
	}
	log.Println("✅ Database connected")

	// 3. 自动迁移
	if err := db.AutoMigrate(&domain.Message{}); err != nil {
		log.Fatalf("❌ Failed to migrate database: %v", err)
	}
	log.Println("✅ Database migrated")

	// 4. 初始化 Redis
	redisConfig := cache.DefaultRedisConfig()
	if consulConfigClient != nil {
		if host, err := consulConfigClient.GetConfig("config/messenger-service/redis_host"); err == nil {
			redisConfig.Host = host
		}
	}
	redisClient, err := cache.NewRedis(redisConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect redis: %v", err)
	}
	log.Println("✅ Redis connected")

	// 6. 创建依赖
	messageRepo := messengerRepo.NewMessageRepository(db)

	// 7. 创建 Service
	svc := messengerService.NewMessengerService(messageRepo, redisClient)

	// 8. 注册服务
	svcRegistry, err := registry.NewConsulRegistry(registryAddr)
	if err != nil {
		log.Printf("⚠️ Failed to connect to consul: %v", err)
	} else {
		serviceName := getEnv("SERVICE_NAME", "messenger-service")
		serviceAddr := getLocalIP()
		if serviceAddr == "" {
			serviceAddr = getEnv("SERVICE_ADDR", "localhost")
		}
		servicePortStr := getEnv("SERVICE_PORT", "9094")
		servicePort, _ := strconv.Atoi(servicePortStr)

		hostname, _ := os.Hostname()
		serviceID := fmt.Sprintf("%s-%s-%s", serviceName, hostname, servicePortStr)
		tags := []string{"messenger", "grpc"}

		if err := svcRegistry.RegisterService(serviceName, serviceID, serviceAddr, servicePort, tags); err != nil {
			log.Printf("❌ Failed to register service: %v", err)
		} else {
			defer svcRegistry.DeregisterService(serviceID)
		}
	}

	// 9. 启动 gRPC
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)

	// 直接注册 Service (因为我们在 Service 中实现了 UnimplementedMessengerServiceServer)
	messengerv1.RegisterMessengerServiceServer(grpcServer, svc)

	grpc_prometheus.Register(grpcServer)
	grpc_prometheus.EnableHandlingTimeHistogram()
	reflection.Register(grpcServer)

	port := getEnv("SERVICE_PORT", "9094")
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("❌ Failed to listen: %v", err)
	}

	log.Printf("🚀 Messenger Service listening on :%s", port)

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
