package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
	"gorm.io/gorm"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/infrastructure/cache"
	canalRelay "twitter-clone/internal/infrastructure/canal"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/infrastructure/persistence"
	"twitter-clone/internal/infrastructure/schema"
	tweetRepository "twitter-clone/internal/module/tweet/repository"
)

func main() {
	log.Println("========================================")
	log.Println("🚀 Canal Outbox Event Relay Service")
	log.Println("========================================")

	// 加载 .env 配置文件
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using default/environment config")
	}

	// 1. 初始化数据库连接（用于位点和 GC 清理）
	dbConfig, err := persistence.ServiceDBConfig("canal-relay")
	if err != nil {
		log.Fatal(err)
	}
	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect to MySQL database: %v", err)
	}
	log.Println("✅ MySQL Database connected successfully")
	if err := schema.CheckRuntime(context.Background(), db, "canal-relay"); err != nil {
		log.Fatal(err)
	}

	// 2. 初始化 Redis（用于位点持久化保存）
	redisConfig := cache.DefaultRedisConfig()
	redisClient, err := cache.NewRedis(redisConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}
	log.Println("✅ Redis connected successfully")

	// 3. 初始化 RabbitMQ 客户端（用于事件投递）
	mqConfig := mq.DefaultRabbitMQConfig()
	mqClient, err := mq.NewRabbitMQ(mqConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect to RabbitMQ: %v", err)
	}
	log.Println("✅ RabbitMQ connected successfully")

	// 4. 初始化 Canal 配置
	canalCfg := canal.NewDefaultConfig()
	canalCfg.Addr = fmt.Sprintf("%s:%d", dbConfig.Host, dbConfig.Port)
	canalCfg.User = dbConfig.User
	canalCfg.Password = dbConfig.Password
	canalCfg.Dump.ExecutionPath = "" // 禁用备份 dump
	// 避免 ServerID 和 MySQL 主库及其他组件冲突，取 9292
	canalCfg.ServerID = 9292

	canalInstance, err := canal.NewCanal(canalCfg)
	if err != nil {
		log.Fatalf("❌ Failed to create canal instance: %v", err)
	}

	// 5. 初始化位点持久化器和中继器
	posStore := canalRelay.NewRedisPositionStore(redisClient, "canal:position:tweet-service")
	confirmedPublisher, err := canalRelay.NewConfirmedRelayPublisher(mqClient)
	if err != nil {
		log.Fatal("Relay publisher confirm setup failed")
	}
	relay := canalRelay.NewOutboxEventRelay(canalInstance, confirmedPublisher, posStore)
	if os.Getenv("CANAL_PUBLICATION_RECEIPTS_ENABLED") == "true" {
		if err := schema.CheckRuntime(context.Background(), db, "canal-relay", &domain.OutboxPublication{}); err != nil {
			log.Fatal(err)
		}
		relay.SetPublicationRecorder(tweetRepository.NewOutboxPublicationRepository(db))
	}

	if err := relay.Start(); err != nil {
		log.Fatalf("❌ Failed to start canal relay: %v", err)
	}

	logs, err := availableBinaryLogs(db)
	if err != nil {
		log.Fatalf("Failed to inspect MySQL binary logs: %v", err)
	}
	savedPosition, err := posStore.GetPosition()
	if err != nil && err != redis.Nil {
		log.Fatalf("Failed to read Canal resume position: %v", err)
	}
	resumePosition, recovered, err := resolveResumePosition(savedPosition, logs)
	if err != nil {
		log.Fatalf("Cannot resume Canal safely: %v", err)
	}
	if err := validateResumePolicy(savedPosition, recovered, os.Getenv("CANAL_ALLOW_CURRENT_POSITION_BOOTSTRAP") == "true", os.Getenv("CANAL_ALLOW_PURGED_POSITION_RECOVERY") == "true"); err != nil {
		log.Fatal(err)
	}
	if recovered {
		log.Printf("WARNING: saved binlog %s was purged; replaying from oldest retained binlog %s. Earlier events require separate Outbox reconciliation.", savedPosition.Name, resumePosition.Name)
	}

	// 6. 异步启动 Canal 监听器
	canalErrors := make(chan error, 1)
	go func() {
		if resumePosition.Name != "" {
			log.Printf("Resuming Canal from position: %v", resumePosition)
			canalErrors <- canalInstance.RunFrom(resumePosition)
		} else {
			log.Println("Starting Canal from current master position")
			canalErrors <- canalInstance.Run()
		}
	}()
	log.Println("✅ Canal outbox relay started")

	// 7. 启动后台低优先级自动清理 GC 协程
	// 每天运行一次（为了测试，设置周期较长，生产上也是 12-24h 周期）
	// Age alone cannot prove publication. Keep events until an independently
	// verified publication-aware retention job is available.
	log.Println("Outbox age-only GC disabled; publication-aware retention required")

	// 8. 信号拦截，优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
	case <-relay.Errors():
		log.Fatal("Canal publication worker stopped; last confirmed checkpoint retained")
	case err := <-canalErrors:
		log.Fatalf("Canal synchronization stopped: %v", err)
	}

	log.Println("🛑 Shutting down Canal Relay Service...")

	// 8.1 停止 GC 协程

	// Stop without advancing past unpublished records; restart replays them.
	relay.Stop()

	// 8.3 安全关闭基础设施连接
	_ = mqClient.Close()
	_ = redisClient.Close()

	log.Println("👋 Canal Relay Service exited gracefully")
}

// startOutboxGC 启动分批低开销的 GC 定时任务
func startOutboxGC(ctx context.Context, db *gorm.DB, interval time.Duration, retainAge time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// 使用毫秒时间戳计算截止时间
				cutoff := time.Now().Add(-retainAge).UnixNano() / int64(time.Millisecond)
				log.Printf("🧹 Outbox GC: Start cleaning events older than %v (cutoff: %d)", retainAge, cutoff)

				var totalDeleted int64
				for {
					// 判定 context 是否退出
					select {
					case <-ctx.Done():
						log.Println("🧹 Outbox GC: Cancelled during execution")
						return
					default:
					}

					// 🎯 采用 Chunked delete 模式分批删除，防大事务锁表和日志暴涨
					res := db.Exec("DELETE FROM outbox_events WHERE created_at < ? LIMIT 1000", cutoff)
					if res.Error != nil {
						log.Printf("⚠️ Outbox GC: Failed to clean chunk: %v", res.Error)
						break
					}
					rows := res.RowsAffected
					totalDeleted += rows
					if rows == 0 {
						break // 清理完毕，退出当前轮次
					}
					log.Printf("🧹 Outbox GC: Cleared %d old events in this chunk", rows)
					time.Sleep(50 * time.Millisecond) // 延迟冷却，防 CPU / IO 暴涨
				}
				log.Printf("🧹 Outbox GC: Run finished. Total events cleared: %d", totalDeleted)

			case <-ctx.Done():
				log.Println("🧹 Outbox GC: Stopped")
				return
			}
		}
	}()
	log.Printf("🚀 Outbox GC daemon started (interval: %v, retainAge: %v)", interval, retainAge)
}
