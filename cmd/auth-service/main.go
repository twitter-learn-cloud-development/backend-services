package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	authV1 "twitter-clone/api/auth/v1"
	userv1 "twitter-clone/api/user/v1"
	redisCache "twitter-clone/internal/infrastructure/cache"
	auth "twitter-clone/internal/module/auth"
	consulConfig "twitter-clone/pkg/config"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/metric"
	"twitter-clone/pkg/pkg/snowflake"
	"twitter-clone/pkg/profiler"
	"twitter-clone/pkg/registry"
	"twitter-clone/pkg/trace"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	// 启动 Profiler 持续性能监控
	profiler.Init("auth-service")

	//初始化Logger
	logger.InitLogger()

	//加载环境变量配置
	if err := godotenv.Load(); err != nil {
		logger.Warn(context.Background(), "No .env file found, using default/environment config")
	}

	//初始化链路追踪
	jaegerEndpoint := getEnv("JAEGER_COLLECTOR_ENDPOINT", "http://localhost:14268/api/traces")
	trace.InitTracer("auth-service", jaegerEndpoint)

	// 初始化 Redis
	redisConfig := redisCache.DefaultRedisConfig()
	redisClient, err := redisCache.NewRedis(redisConfig)
	if err != nil {
		logger.Fatal(context.Background(), "Failed to connect redis: %v", zap.Error(err))
	}
	logger.Info(context.Background(), "Redis connected")
	// 初始化 Snowflake
	snowflake.MustInit(redisClient)
	logger.Info(context.Background(), "Snowflake initialized (Node ID: 1)")

	//初始化Prometheus指标
	metric.InitMetrics()
	metric.StartMetricsServer(2114)

	//初始化Consul连接信息
	consulHost := getEnv("CONSUL_HOST", "localhost")
	consulPort := getEnv("CONSUL_PORT", "8500")
	registryAddr := consulHost + ":" + consulPort

	cfg := &auth.RawConsulConfig{}

	//初始化Consul，并获取PrivateKey、Expiration、kid
	if consulClient, err := consulConfig.NewConsulConfigClient(registryAddr); err == nil {
		privateKey, errPriv := consulClient.GetConfig("config/global/private_key")
		kid, errKid := consulClient.GetConfig("config/global/kid")
		expiration, errExp := consulClient.GetConfig("config/global/expiration")

		if errPriv != nil || errKid != nil || errExp != nil || privateKey == "" || kid == "" || expiration == "" {
			logger.Info(context.Background(), "🔑 [Bootstrapping] Consul configuration not fully initialized, generating new RSA key pair...")
			// 1. 生成 RSA 私钥
			rsaPrivKey, errGen := rsa.GenerateKey(rand.Reader, 2048)
			if errGen != nil {
				logger.Fatal(context.Background(), "Failed to generate RSA private key", zap.Error(errGen))
			}

			// 2. PEM 序列化
			var pemPrivateBlock = &pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(rsaPrivKey),
			}
			privateKeyPEM := pem.EncodeToMemory(pemPrivateBlock)

			// 3. 产生 KID 和默认过期时间
			generatedKid := fmt.Sprintf("kid-%d", time.Now().UnixNano())
			defaultExpiration := "168h" // 7天

			// 4. 写入 Consul (双向保证安全)
			if errPut := consulClient.PutConfig("config/global/private_key", string(privateKeyPEM)); errPut != nil {
				logger.Fatal(context.Background(), "Failed to save private key to Consul", zap.Error(errPut))
			}
			if errPut := consulClient.PutConfig("config/global/kid", generatedKid); errPut != nil {
				logger.Fatal(context.Background(), "Failed to save kid to Consul", zap.Error(errPut))
			}
			if errPut := consulClient.PutConfig("config/global/expiration", defaultExpiration); errPut != nil {
				logger.Fatal(context.Background(), "Failed to save expiration to Consul", zap.Error(errPut))
			}

			// 5. 重新读取配置，完成引导
			privateKey = string(privateKeyPEM)
			kid = generatedKid
			expiration = defaultExpiration
			logger.Info(context.Background(), "🔑 [Bootstrapping] Successfully generated and uploaded RSA key config to Consul")
		}

		cfg.PrivateKey = privateKey
		cfg.Kid = kid
		cfg.Expiration = expiration

		logger.Info(context.Background(), "🔐 Loaded auth configurations from Consul",
			zap.String("kid", kid),
			zap.String("expiration", expiration))
	} else {
		logger.Fatal(context.Background(), "consul连接失败", zap.Error(err))
	}

	//初始化consul注册中心
	svcRegistry, err := registry.NewConsulRegistry(registryAddr)
	if err != nil {
		logger.Warn(context.Background(), "Failed Consul registry", zap.Error(err))
	} else {
		serviceName := getEnv("SERVICE_NAME", "auth-service")
		serviceAddr := getLocalIP()
		if serviceAddr == "" {
			serviceAddr = getEnv("SERVICE_ADDR", "localhost")
		}

		servicePortStr := getEnv("SERVICE_PORT", "9097")
		servicePort, err := strconv.Atoi(servicePortStr)
		if err != nil {
			logger.Fatal(context.Background(), "Error converting port", zap.Error(err))
		}

		hostname, _ := os.Hostname()
		serviceID := fmt.Sprintf("%s-%s-%s", serviceName, hostname, servicePortStr)
		tags := []string{"auth", "grpc"}

		if err := svcRegistry.RegisterService(serviceName, serviceID, serviceAddr, servicePort, tags); err != nil {
			logger.Fatal(context.Background(), "Failed to register service", zap.Error(err))
		} else {
			defer svcRegistry.DeregisterService(serviceID)
			logger.Info(context.Background(), "Service registered successfully", zap.String("serviceID", serviceID))
		}
	}

	// 获取远程 user-service 的 gRPC 天线客户端
	userSvcAddr := getEnv("USER_SERVICE_ADDR", "localhost:9091")
	userConn, err := grpc.NewClient(userSvcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.Fatal(context.Background(), "Failed to dial user service", zap.Error(err))
	}
	userGRPCClient := userv1.NewUserServiceClient(userConn)

	// WIRE 灵魂组装一击：一键算出所有依赖，直接拿到 AuthService 实例！
	authServiceInstance, err := auth.InitAuthServer(cfg, userGRPCClient)
	if err != nil {
		logger.Fatal(context.Background(), "Wire initialization failed", zap.Error(err))
	}

	//创建gRPC Server
	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
		grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
	)
	//TODO:等待auth模块完成，再进行注册
	authV1.RegisterAuthServiceServer(grpcServer, authServiceInstance)

	//注册gRPC Metrics
	grpc_prometheus.Register(grpcServer)
	grpc_prometheus.EnableHandlingTimeHistogram()

	//注册Reflection（用于 grpcurl）
	reflection.Register(grpcServer)

	//启动 gRPC 服务
	lis, err := net.Listen("tcp", ":9097")
	if err != nil {
		logger.Fatal(context.Background(), "Failed to listen", zap.Error(err))
	}
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal(context.Background(), "Failed to serve", zap.Error(err))
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info(context.Background(), "Shutting down server...")
	grpcServer.GracefulStop()
	lis.Close()
	logger.Info(context.Background(), "Server exited")

}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
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
