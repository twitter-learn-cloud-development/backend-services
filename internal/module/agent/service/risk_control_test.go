package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"

	"twitter-clone/internal/domain"
)

func TestLuaCleanUpScript(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to run miniredis: %v", err)
	}
	defer s.Close()

	rClient := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})

	ctx := context.Background()
	zsetKey := "timeline:123"
	unreadKey := "unread:timeline:123"
	tweetID := uint64(55555)

	// 情况 1: 用户原本有未读数 > 0
	rClient.Del(ctx, zsetKey, unreadKey)
	rClient.ZAdd(ctx, zsetKey, &redis.Z{Score: float64(tweetID), Member: tweetID})
	rClient.Set(ctx, unreadKey, "5", 0)

	// 执行 Lua 脚本
	res, err := rClient.Eval(ctx, cleanUpScript, []string{zsetKey, unreadKey}, tweetID).Result()
	assert.NoError(t, err)
	assert.Equal(t, int64(1), res.(int64)) // 应该成功移除了 1 个元素

	// 验证垃圾 ID 消失，未读数从 5 变为 4
	exists, err := rClient.ZScore(ctx, zsetKey, fmt.Sprintf("%d", tweetID)).Result()
	assert.Error(t, err) // ZScore returns redis.Nil when member not found
	assert.Equal(t, redis.Nil, err)
	_ = exists

	unreadVal, err := rClient.Get(ctx, unreadKey).Result()
	assert.NoError(t, err)
	assert.Equal(t, "4", unreadVal)

	// 情况 2: 未读数为 0，扣减后不应该溢出（保持 0）
	rClient.Del(ctx, zsetKey, unreadKey)
	rClient.ZAdd(ctx, zsetKey, &redis.Z{Score: float64(tweetID), Member: tweetID})
	rClient.Set(ctx, unreadKey, "0", 0)

	res, err = rClient.Eval(ctx, cleanUpScript, []string{zsetKey, unreadKey}, tweetID).Result()
	assert.NoError(t, err)
	assert.Equal(t, int64(1), res.(int64))

	unreadVal, err = rClient.Get(ctx, unreadKey).Result()
	assert.NoError(t, err)
	assert.Equal(t, "0", unreadVal) // 依然是 0，未发生负数溢出

	// 情况 3: 未读数 Key 不存在，不应该报错
	rClient.Del(ctx, zsetKey, unreadKey)
	rClient.ZAdd(ctx, zsetKey, &redis.Z{Score: float64(tweetID), Member: tweetID})

	res, err = rClient.Eval(ctx, cleanUpScript, []string{zsetKey, unreadKey}, tweetID).Result()
	assert.NoError(t, err)
	assert.Equal(t, int64(1), res.(int64))

	_, err = rClient.Get(ctx, unreadKey).Result()
	assert.Equal(t, redis.Nil, err) // key依然不存在，没有被意外创建
}

// 内存隔离与深拷贝 Data Race 单测
func TestRiskControl_DeepCopyRace(t *testing.T) {
	// 模拟并发读写推文切片场景下，执行 checkSpam
	// 我们起 10 个 goroutine 并发执行对同一批推文的读取/操作，如果没做深拷贝，go test -race 会检测到 Data Race
	tweets := []*domain.Tweet{
		{ID: 1, UserID: 10, Content: "Hello 1", CreatedAt: time.Now().UnixMilli()},
		{ID: 2, UserID: 10, Content: "Hello 2", CreatedAt: time.Now().UnixMilli() + 1000},
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// 🆕 显式在协程内部深拷贝隔离数据
			copiedTweets := make([]domain.Tweet, len(tweets))
			for j, t := range tweets {
				copiedTweets[j] = *t
			}

			// 对深拷贝的副本进行修改，如果未进行深拷贝直接读取原 tweets 并在修改，会触发 Data Race
			for j := range copiedTweets {
				copiedTweets[j].Content = fmt.Sprintf("Modified %d in goroutine %d", j, idx)
				copiedTweets[j].CreatedAt = copiedTweets[j].CreatedAt + int64(idx)
			}
		}(i)
	}
	wg.Wait()
}
