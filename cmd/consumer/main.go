package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/infrastructure/persistence"
	followRepository "twitter-clone/internal/module/follow/repository"
	tweetCache "twitter-clone/internal/module/tweet/cache"
	"twitter-clone/internal/mq/consumer"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/pkg/snowflake"
)

func main() {
	log.Println("========================================")
	log.Println("🚀 Twitter Clone - Timeline Consumer")
	log.Println("========================================")

	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using default/environment config")
	}

	logger.InitLogger()

	// 1. 初始化 Snowflake
	if err := snowflake.Init(1); err != nil {
		log.Fatalf("❌ Failed to init snowflake: %v", err)
	}
	log.Println("✅ Snowflake initialized")

	// 2. 初始化数据库
	dbConfig := persistence.DefaultDBConfig()
	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect database: %v", err)
	}
	log.Println("✅ Database connected")

	// 3. 自动迁移
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.Tweet{},
		&domain.Follow{},
		&domain.Like{},
		&domain.Comment{},
	); err != nil {
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

	// 6. ES 初始化
	if err := es.Init(); err != nil {
		log.Fatalf("❌ Failed to init elasticsearch: %v", err)
	}
	esClient := es.GetClient()

	// 创建推文索引（已存在则跳过）
	if err := esClient.CreateTweetIndex(context.Background()); err != nil {
		log.Fatalf("❌ Failed to create tweet index: %v", err)
	}

	// 7. 创建依赖
	followRepo := followRepository.NewFollowRepository(db)
	timelineCache := tweetCache.NewTimelineCache(redisClient)

	// 8. 创建 Consumer
	timelineConsumer, err := consumer.NewTimelineConsumer(mqClient, followRepo, timelineCache, redisClient, esClient)
	if err != nil {
		log.Fatalf("❌ Failed to create consumer: %v", err)
	}

	// 9. 启动 Consumer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 监听退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动消费者（阻塞）
	go func() {
		if err := timelineConsumer.Start(ctx); err != nil {
			log.Fatalf("❌ Consumer error: %v", err)
		}
	}()

	log.Println("========================================")
	log.Println("✅ Timeline Consumer is running...")
	log.Println("📥 Listening for events:")
	log.Println("   - tweet.created")
	log.Println("   - tweet.deleted")
	log.Println("Press Ctrl+C to stop")
	log.Println("========================================")

	// 等待退出信号
	<-sigChan
	log.Println("\n⏹️  Shutting down consumer...")

	cancel()
	log.Println("✅ Consumer stopped gracefully")
}
