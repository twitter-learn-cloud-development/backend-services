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

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

	userv1 "twitter-clone/api/user/v1"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/infrastructure/persistence"
	userGrpc "twitter-clone/internal/module/user/grpc"
	userRepository "twitter-clone/internal/module/user/repository"
	userService "twitter-clone/internal/module/user/service"
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
	log.Println("🚀 User Service (gRPC)")
	log.Println("========================================")

	// 0. 初始化 Logger (TraceID 支持)
	logger.InitLogger()

	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using default/environment config")
	}

	// 🔍 初始化链路追踪
	jaegerEndpoint := getEnv("JAEGER_COLLECTOR_ENDPOINT", "http://localhost:14268/api/traces")
	trace.InitTracer("user-service", jaegerEndpoint)

	// 1. 初始化 Snowflake
	if err := snowflake.Init(1); err != nil {
		log.Fatalf("❌ Failed to init snowflake: %v", err)
	}
	log.Println("✅ Snowflake initialized (Node ID: 1)")

	// 📊 初始化 Prometheus 指标
	metric.InitMetrics()
	// 启动 Metrics Server (User Service uses 2111)
	metric.StartMetricsServer(2111)

	// 4. 初始化 Consul 连接信息 (提前读取用于加载配置)
	consulHost := getEnv("CONSUL_HOST", "localhost")
	consulPort := getEnv("CONSUL_PORT", "8500")
	registryAddr := consulHost + ":" + consulPort

	// 2. 初始化数据库
	dbConfig := persistence.DefaultDBConfig()

	// 🆕 从 Consul 加载配置覆盖默认值
	if consulClient, err := consulConfig.NewConsulConfigClient(registryAddr); err == nil {
		if host, err := consulClient.GetConfig("config/user-service/db_host"); err == nil {
			dbConfig.Host = host
			log.Printf("✅ Loaded DB Host from Consul: %s", host)
		}
		if portStr, err := consulClient.GetConfig("config/user-service/db_port"); err == nil {
			if p, err := strconv.Atoi(portStr); err == nil {
				dbConfig.Port = p
				log.Printf("✅ Loaded DB Port from Consul: %d", p)
			}
		}
		if redisHost, err := consulClient.GetConfig("config/user-service/redis_host"); err == nil {
			// 这里假设 persistence 或 cache 包有方式传递 Redis 配置，
			// 目前 persistence.DefaultDBConfig 不含 Redis，但下一步 Redis client 可能需要。
			// 暂且只打印
			log.Printf("✅ Loaded Redis Host from Consul: %s", redisHost)
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
	if err := db.AutoMigrate(&domain.User{}); err != nil {
		log.Fatalf("❌ Failed to migrate database: %v", err)
	}
	log.Println("✅ Database migrated")

	// 4. 创建依赖
	userRepo := userRepository.NewUserRepository(db)
	userSvc := userService.NewUserService(userRepo)

	// 5. 初始化 Consul 注册中心
	// registryAddr 已在前面初始化

	svcRegistry, err := registry.NewConsulRegistry(registryAddr)
	if err != nil {
		log.Printf("⚠️ Failed to connect to consul: %v", err)
	} else {
		serviceName := getEnv("SERVICE_NAME", "user-service")

		// 动态获取容器 IP
		serviceAddr := getLocalIP()
		if serviceAddr == "" {
			serviceAddr = getEnv("SERVICE_ADDR", "localhost")
		}

		servicePortStr := getEnv("SERVICE_PORT", "9091")
		servicePort, _ := strconv.Atoi(servicePortStr)

		// 使用 Hostname 确保 ID 唯一
		hostname, _ := os.Hostname()
		serviceID := fmt.Sprintf("%s-%s-%s", serviceName, hostname, servicePortStr)

		tags := []string{"user", "grpc"}

		if err := svcRegistry.RegisterService(serviceName, serviceID, serviceAddr, servicePort, tags); err != nil {
			log.Printf("❌ Failed to register service: %v", err)
		} else {
			defer svcRegistry.DeregisterService(serviceID)
		}
	}

	// 6. 创建 gRPC Server
	// 🔍 添加 OpenTelemetry Server Interceptor (StatsHandler)
	// 🔍 添加 OpenTelemetry & Prometheus Server Interceptor
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)
	userv1.RegisterUserServiceServer(grpcServer, userGrpc.NewUserServer(userSvc))

	// 📊 注册 gRPC Metrics
	grpc_prometheus.Register(grpcServer)
	grpc_prometheus.EnableHandlingTimeHistogram()

	// 🆕 注册 Reflection（用于 grpcurl）
	reflection.Register(grpcServer)

	// 7. 启动 gRPC 服务
	lis, err := net.Listen("tcp", ":9091")
	if err != nil {
		log.Fatalf("❌ Failed to listen: %v", err)
	}

	log.Println("========================================")
	log.Println("🚀 User Service listening on :9091")
	log.Println("📡 gRPC endpoints:")
	log.Println("   - Register")
	log.Println("   - Login")
	log.Println("   - GetProfile")
	log.Println("   - UpdateProfile")
	log.Println("   - ChangePassword")
	log.Println("========================================")

	// In a goroutine to allow signal handling
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("❌ Failed to serve: %v", err)
		}
	}()

	// 8. 优雅关闭
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
