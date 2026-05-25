package consumer

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
)

func TestHashtagBatcher_AddAndFlush(t *testing.T) {
	// 1. 初始化本地 Redis，如果连接失败则 Skip 测试，不阻塞无 Redis 环境的 CI
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Skipping integration test because local redis:6379 is not reachable")
	}

	// 准备干净的测试 Key
	client.Del(ctx, "trends:global")
	defer client.Del(ctx, "trends:global")

	// 2. 实例化 Batcher 并启动 Ticker
	interval := 100 * time.Millisecond
	batcher := NewHashtagBatcher(client, interval)
	batcher.Start()
	defer batcher.Stop()

	// 3. 并发写入测试：50 个协程分别高频累加
	const goroutines = 50
	const iterations = 100
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				batcher.Add("world")
				batcher.Add("golang")
			}
		}()
	}

	wg.Wait()

	// 保证 Ticker 至少触发了 Flush
	time.Sleep(interval * 2)

	// 4. 断言 Redis ZSet 累加结果
	worldScore, err := client.ZScore(ctx, "trends:global", "world").Result()
	if err != nil {
		t.Fatalf("Failed to query ZScore for 'world': %v", err)
	}
	expected := float64(goroutines * iterations)
	if worldScore != expected {
		t.Errorf("Expected score for 'world' to be %f, got %f", expected, worldScore)
	}

	golangScore, err := client.ZScore(ctx, "trends:global", "golang").Result()
	if err != nil {
		t.Fatalf("Failed to query ZScore for 'golang': %v", err)
	}
	if golangScore != expected {
		t.Errorf("Expected score for 'golang' to be %f, got %f", expected, golangScore)
	}
}

func TestHashtagBatcher_GracefulShutdown(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Skipping integration test because local redis:6379 is not reachable")
	}

	client.Del(ctx, "trends:global")
	defer client.Del(ctx, "trends:global")

	// 设置一个非常长的刷新间隔（如 1 小时），促使它在运行期间绝不会触发 Ticker Flush
	batcher := NewHashtagBatcher(client, 1*time.Hour)
	batcher.Start()

	// 写入数据
	batcher.Add("graceful")
	batcher.Add("graceful")

	// 模拟优雅退出，这应该立刻触发最后一次强制 Flush 并等待协程退出
	batcher.Stop()

	// 验证退出后，内存中的缓存已强制刷进 Redis 并没有丢失
	score, err := client.ZScore(ctx, "trends:global", "graceful").Result()
	if err != nil {
		t.Fatalf("Failed to query ZScore after shutdown: %v", err)
	}
	if score != 2.0 {
		t.Errorf("Expected score for 'graceful' to be 2.0, got %f", score)
	}
}
