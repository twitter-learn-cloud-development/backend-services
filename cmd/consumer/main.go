package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/infrastructure/cache"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/infrastructure/persistence"
	"twitter-clone/internal/infrastructure/schema"
	followProjection "twitter-clone/internal/module/follow/projection"
	followRepository "twitter-clone/internal/module/follow/repository"
	tweetCache "twitter-clone/internal/module/tweet/cache"
	tweetRepository "twitter-clone/internal/module/tweet/repository"
	"twitter-clone/internal/mq/consumer"
	ai "twitter-clone/pkg/ai"
	"twitter-clone/pkg/es"
	"twitter-clone/pkg/logger"
	"twitter-clone/pkg/metric"
	"twitter-clone/pkg/pkg/snowflake"
	"twitter-clone/pkg/qdrant"
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

	// 2. 初始化数据库
	dbConfig, err := persistence.ServiceDBConfig("consumer")
	if err != nil {
		log.Fatal(err)
	}
	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect database: %v", err)
	}
	log.Println("✅ Database connected")

	// 3. 自动迁移
	if err := schema.CheckRuntime(context.Background(), db, "consumer"); err != nil {
		log.Fatal(err)
	}
	log.Println("Database schema verified; runtime DDL disabled")

	// 4. 初始化 Redis
	redisConfig := cache.DefaultRedisConfig()
	redisClient, err := cache.NewRedis(redisConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect redis: %v", err)
	}
	log.Println("✅ Redis connected")

	// 初始化 Snowflake
	snowflake.MustInit(redisClient)
	log.Println("✅ Snowflake initialized (Node ID: 1)")

	// 5. 初始化 RabbitMQ
	mqConfig := mq.DefaultRabbitMQConfig()
	mqClient, err := mq.NewRabbitMQ(mqConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect rabbitmq: %v", err)
	}
	defer mqClient.Close()
	failureMQClient, err := mq.NewRabbitMQ(mqConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect dedicated RabbitMQ failure publisher: %v", err)
	}
	defer failureMQClient.Close()
	log.Println("✅ RabbitMQ consumer and confirmed failure publisher connected")

	// 6. ES 初始化
	var esClient *es.Client
	if err := es.Init(); err != nil {
		log.Printf("⚠️  Failed to init elasticsearch: %v. Continuing in degraded mode without search index support.", err)
	} else {
		esClient = es.GetClient()
		// 创建推文索引（已存在则跳过）
		if err := esClient.CreateTweetIndex(context.Background()); err != nil {
			log.Printf("⚠️  Failed to create tweet index: %v. Search features might be limited.", err)
		}
	}

	// 6.1 Qdrant 向量库初始化
	var qdrantClient *qdrant.Client
	qdrantURL := os.Getenv("QDRANT_URL")
	if qdrantURL == "" {
		qdrantURL = "http://localhost:6333"
	}
	qdrantClient = qdrant.NewClient(qdrantURL)
	log.Println("✅ Qdrant client initialized")
	profile := consumer.ConfiguredTweetEmbeddingProfile()
	if !profile.Valid() {
		log.Print("⚠️  Invalid tweet embedding profile; vector tasks will fail explicitly while text tasks remain available")
	} else {
		bootstrapCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := qdrantClient.CreateCollection(bootstrapCtx, "tweets", profile.Dimension); err != nil {
			log.Printf("⚠️  Failed to create qdrant collection: %v. Search features might be limited.", err)
		}
		cancel()
	}

	// 7. 创建依赖
	followRepo := followRepository.NewFollowRepository(db)
	timelineCache := tweetCache.NewTimelineCache(redisClient)
	outboxRepo := tweetRepository.NewOutboxRepository(db)

	aiClient := ai.NewClientWithConfig(os.Getenv("LM_STUDIO_API_URL"), os.Getenv("LM_STUDIO_API_TOKEN"), nil)

	// 7.1 初始化智能趋势处理器（如果分词器启动失败，则降级继续，防止主进程 crash）
	var trendsProcessor *consumer.TrendsProcessor
	var trendsErr error
	trendsProcessor, trendsErr = consumer.NewTrendsProcessor()
	if trendsErr != nil {
		log.Printf("⚠️  Failed to initialize trends processor: %v. Trends feature will fallback to basic hashtag regex.", trendsErr)
	} else {
		log.Println("✅ Trends processor (GSE NER) initialized successfully")
	}

	// 8. 创建 Consumer
	timelineConsumer, err := consumer.NewTimelineConsumer(
		mqClient, followRepo, timelineCache, redisClient, esClient, qdrantClient,
		aiClient, outboxRepo, trendsProcessor, failureMQClient,
	)
	if err != nil {
		log.Fatalf("❌ Failed to create consumer: %v", err)
	}
	timelineConsumer.SetProjectionRepository(tweetRepository.NewTweetRepository(db).(domain.TweetProjectionRepository))
	moderationObserver, err := consumer.NewPrometheusModerationCleanupObserver(nil)
	if err != nil {
		log.Fatalf("failed to initialize moderation cleanup metrics: %v", err)
	}
	timelineConsumer.SetModerationCleanupObserver(moderationObserver)
	tweetCreatedObserver, err := consumer.NewPrometheusTweetCreatedObserver(nil)
	if err != nil {
		log.Fatalf("failed to initialize tweet-created metrics: %v", err)
	}
	timelineConsumer.SetTweetCreatedObserver(tweetCreatedObserver)
	outboxObserver, err := consumer.NewPrometheusOutboxWorkerObserver(nil)
	if err != nil {
		log.Fatalf("failed to initialize timeline outbox worker metrics: %v", err)
	}
	timelineConsumer.SetOutboxWorkerObserver(outboxObserver)
	projectionObserver, err := consumer.NewPrometheusProjectionObserver(nil)
	if err != nil {
		log.Fatal("failed to initialize projection metrics")
	}
	timelineConsumer.SetProjectionObserver(projectionObserver)
	engagementObserver, err := consumer.NewEngagementMetrics(nil)
	if err != nil {
		log.Fatal("engagement metrics unavailable")
	}
	timelineConsumer.SetEngagementObserver(engagementObserver)
	timelineConsumer.SetEngagementConnectionConfig(*mqConfig)
	decayObserver, err := consumer.NewTrendsDecayMetrics(nil)
	if err != nil {
		log.Fatal("trend decay metrics unavailable")
	}
	timelineConsumer.SetTrendsDecayObserver(decayObserver)
	metric.StartMetricsServer(getEnvPositiveInt("CONSUMER_METRICS_PORT", 2116))

	// 9. 启动 Consumer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var followLoops sync.WaitGroup
	var followBroker, followPublisher *mq.RabbitMQ
	if os.Getenv("FOLLOW_PROJECTION_WORKER_ENABLED") == "true" {
		if err := schema.CheckRuntime(ctx, db, "consumer", &followProjection.AuthorState{}, &followProjection.IngressReceipt{}); err != nil {
			log.Fatal(err)
		}
		followBroker, err = mq.NewRabbitMQ(mqConfig)
		if err != nil {
			log.Fatal("follow broker unavailable")
		}
		defer followBroker.Close()
		followPublisher, err = mq.NewRabbitMQ(mqConfig)
		if err != nil {
			log.Fatal("follow confirm publisher unavailable")
		}
		defer followPublisher.Close()
		if err := followBroker.SetQoS(10); err != nil {
			log.Fatal("follow qos unavailable")
		}
		observer, err := followProjection.NewMetrics(nil)
		if err != nil {
			log.Fatal("follow metrics unavailable")
		}
		store := &followProjection.SQLStore{DB: db, NewID: snowflake.GenerateID}
		runner := &followProjection.Runner{Broker: followBroker, Publisher: followPublisher, Tasks: outboxRepo, Owner: "follow-projection", Engine: followProjection.Engine{Store: store, Cache: &followProjection.RedisCache{Client: redisClient, InvalidateCelebrityLocal: timelineCache.InvalidateCelebrityLocal}, Observer: observer}}
		runner.Backlog = store.Backlog
		if err := runner.Declare(); err != nil {
			log.Fatal("follow recovery topology unavailable")
		}
		followLoops.Add(1)
		go func() { defer followLoops.Done(); runner.Run(ctx) }()
	}

	// 监听退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动消费者（阻塞）
	timelineDone := make(chan struct{})
	go func() {
		defer close(timelineDone)
		if err := timelineConsumer.Start(ctx); err != nil {
			log.Fatalf("❌ Consumer error: %v", err)
		}
	}()

	log.Println("========================================")
	log.Println("✅ Timeline Consumer is running...")
	log.Println("📥 Listening for events:")
	log.Println("   - tweet.created")
	log.Println("   - tweet.deleted")
	log.Println("   - tweet.moderated")
	log.Println("Press Ctrl+C to stop")
	log.Println("========================================")

	// 等待退出信号
	<-sigChan
	log.Println("\n⏹️  Shutting down consumer...")

	cancel()
	<-timelineDone
	if followBroker != nil {
		_ = followBroker.Close()
	}
	followLoops.Wait()
	log.Println("✅ Consumer stopped gracefully")
}

func getEnvPositiveInt(key string, fallback int) int {
	raw := os.Getenv(key)
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
