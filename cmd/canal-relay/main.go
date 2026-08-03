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
	"github.com/joho/godotenv"
	"gorm.io/gorm"

	"twitter-clone/internal/infrastructure/cache"
	canalRelay "twitter-clone/internal/infrastructure/canal"
	"twitter-clone/internal/infrastructure/mq"
	"twitter-clone/internal/infrastructure/persistence"
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
	dbConfig := persistence.DefaultDBConfig()
	db, err := persistence.NewDB(dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect to MySQL database: %v", err)
	}
	log.Println("✅ MySQL Database connected successfully")

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
	relay := canalRelay.NewOutboxEventRelay(canalInstance, mqClient, posStore)

	if err := relay.Start(); err != nil {
		log.Fatalf("❌ Failed to start canal relay: %v", err)
	}

	// 6. 异步启动 Canal 监听器
	go func() {
		pos, err := posStore.GetPosition()
		if err == nil && pos.Name != "" {
			log.Printf("🔄 Resuming Canal from position: %v", pos)
			if err := canalInstance.RunFrom(pos); err != nil {
				log.Printf("❌ Canal run from position error: %v", err)
			}
		} else {
			log.Println("🔄 Starting Canal from current master position")
			if err := canalInstance.Run(); err != nil {
				log.Printf("❌ Canal run error: %v", err)
			}
		}
	}()
	log.Println("✅ Canal outbox relay started")

	// 7. 启动后台低优先级自动清理 GC 协程
	// 每天运行一次（为了测试，设置周期较长，生产上也是 12-24h 周期）
	gcCtx, gcCancel := context.WithCancel(context.Background())
	startOutboxGC(gcCtx, db, 12*time.Hour, 24*time.Hour)

	// 8. 信号拦截，优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down Canal Relay Service...")

	// 8.1 停止 GC 协程
	gcCancel()

	// 8.2 优雅退出中继器（内部会排空内存队列并保存位点）
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
