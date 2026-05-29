package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/go-redis/redis/v8"
)

// L1Cache 一级本地内存缓存，解决高并发下的 Hot Key 瓶颈并防 GC STW
type L1Cache struct {
	bc          *bigcache.BigCache
	redisClient *redis.Client
	pubsub      *redis.PubSub
	cancel      context.CancelFunc
}

// NewL1Cache 创建 L1Cache，maxMemoryMB 为内存占用限制大小 (例如 256MB)
func NewL1Cache(redisClient *redis.Client, maxMemoryMB int) (*L1Cache, error) {
	config := bigcache.DefaultConfig(10 * time.Minute)
	config.CleanWindow = 1 * time.Minute
	config.HardMaxCacheSize = maxMemoryMB

	bc, err := bigcache.New(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bigcache: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	l1 := &L1Cache{
		bc:          bc,
		redisClient: redisClient,
		cancel:      cancel,
	}

	l1.startInvalidationListener(ctx)
	return l1, nil
}

// startInvalidationListener 监听 Redis Pub/Sub 失效广播通道，以维持分布式多实例的 L1 缓存强一致性
func (l *L1Cache) startInvalidationListener(ctx context.Context) {
	l.pubsub = l.redisClient.Subscribe(ctx, "tweet_invalidations")

	go func() {
		ch := l.pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					log.Println("⚠️  L1Cache invalidation channel closed, attempting to resubscribe...")
					time.Sleep(1 * time.Second)
					// 防御式重订阅
					l.pubsub = l.redisClient.Subscribe(ctx, "tweet_invalidations")
					ch = l.pubsub.Channel()
					continue
				}

				// 仅做微秒级轻量本地删除操作，绝不在 Pub/Sub 消费环路内发起远程 I/O 或 DB 查询
				key := msg.Payload
				err := l.bc.Delete(key)
				if err != nil && err != bigcache.ErrEntryNotFound {
					log.Printf("⚠️  L1Cache failed to invalidate key %s: %v", key, err)
				} else if err == nil {
					log.Printf("🧹 L1Cache local invalidation processed: key=%s", key)
				}
			}
		}
	}()
}

// Get 获取 L1 缓存数据
func (l *L1Cache) Get(key string) ([]byte, error) {
	return l.bc.Get(key)
}

// Set 设置 L1 缓存数据
func (l *L1Cache) Set(key string, value []byte) error {
	return l.bc.Set(key, value)
}

// Delete 删除 L1 缓存数据
func (l *L1Cache) Delete(key string) error {
	err := l.bc.Delete(key)
	if err == bigcache.ErrEntryNotFound {
		return nil
	}
	return err
}

// InvalidateGlobal 触发全局失效广播并清除本地 L1 缓存
func (l *L1Cache) InvalidateGlobal(ctx context.Context, key string) error {
	// 1. 本地删除
	_ = l.Delete(key)

	// 2. 广播通知其他 Pod 实例
	err := l.redisClient.Publish(ctx, "tweet_invalidations", key).Err()
	if err != nil {
		return fmt.Errorf("failed to publish invalidation event for key %s: %w", key, err)
	}
	return nil
}

// Close 关闭 L1Cache，释放相关连接与上下文
func (l *L1Cache) Close() error {
	if l.cancel != nil {
		l.cancel()
	}
	if l.pubsub != nil {
		_ = l.pubsub.Close()
	}
	return l.bc.Close()
}
