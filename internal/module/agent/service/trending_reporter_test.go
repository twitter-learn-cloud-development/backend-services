package service

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/pkg/ai"
)

type mockTweetClient struct {
	tweetv1.TweetServiceClient
	mu          sync.Mutex
	createCount int
}

func (m *mockTweetClient) CreateTweet(ctx context.Context, in *tweetv1.CreateTweetRequest, opts ...grpc.CallOption) (*tweetv1.CreateTweetResponse, error) {
	// 增加模拟耗时，确保其他并发协程在锁释放前就已经尝试获取锁，从而验证分布式锁的排他性
	time.Sleep(100 * time.Millisecond)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCount++
	return &tweetv1.CreateTweetResponse{
		Tweet: &tweetv1.Tweet{
			Id: 999,
		},
	}, nil
}

func TestTrendingReporter_LockAndReport(t *testing.T) {
	// 1. 开启 miniredis
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to run miniredis: %v", err)
	}
	defer s.Close()

	rClient := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})

	// 2. 初始化一些热点话题，并写入几条推文保证召回量非空
	err = rClient.ZAdd(context.Background(), "trends:global", &redis.Z{
		Score:  100.0,
		Member: "AIGC",
	}).Err()
	assert.NoError(t, err)

	// 3. 开启 Mock Embedding 环境变量
	os.Setenv("MOCK_EMBEDDING", "true")
	defer os.Unsetenv("MOCK_EMBEDDING")

	// 4. 创建 AI client
	aiCli := ai.NewClient("http://localhost:1234/v1")

	// Mock Tweet Service Client
	mockTweetSvc := &mockTweetClient{}

	// 5. 初始化 TrendingReporter (ES and Qdrant are nil, which is safe due to nil guards!)
	reporter := NewTrendingReporter(
		rClient,
		nil, // esClient
		nil, // qdrantClient
		aiCli,
		mockTweetSvc,
		"text-embedding-bge-m3",
		"qwen",
		100,
	)

	// 6. 开启 3 个并发协程执行 reportTrending
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reporter.reportTrending(context.Background())
		}()
	}
	wg.Wait()

	// 7. 断言有且仅有 1 个协程成功创建了推文，其余均因锁冲突而未发帖
	assert.Equal(t, 1, mockTweetSvc.createCount)
}
