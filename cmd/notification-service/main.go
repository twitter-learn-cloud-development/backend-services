package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/infrastructure/persistence"
	"twitter-clone/internal/module/notification/repository"
	"twitter-clone/internal/module/notification/worker"
	consulConfig "twitter-clone/pkg/config"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/pkg/snowflake"
)

func main() {
	log.Println("========================================")
	log.Println("🚀 Notification Service (Worker)")
	log.Println("========================================")

	// 0. 初始化 Logger
	logger.InitLogger()

	// 加载 .env
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using default/environment config")
	}

	// 1. 初始化 Snowflake
	if err := snowflake.Init(2); err != nil { // Node ID 2
		log.Fatalf("❌ Failed to init snowflake: %v", err)
	}

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

	// 7. Start Consumer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		log.Fatalf("❌ Failed to start consumer: %v", err)
	}

	// 8. Wait for signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down notification service...")
	// 给一点时间让正在处理的消息完成
	time.Sleep(1 * time.Second)
	log.Println("✅ Service exited")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
