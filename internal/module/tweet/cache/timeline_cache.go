package cache

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"strconv"
	"time"
)

const (
	// TimelineKeyPrefix Timeline 缓存键前缀
	TimelineKeyPrefix = "timeline:"

	// TimelineMaxSize Timeline 最大缓存数量
	TimelineMaxSize = 1000

	// TimelineExpiration Timeline 过期时间
	TimelineExpiration = 7 * 24 * time.Hour
)

// TimelineCache Timeline 缓存
type TimelineCache struct {
	redis *redis.Client
}

// NewTimelineCache 创建 Timeline 缓存
func NewTimelineCache(redis *redis.Client) *TimelineCache {
	return &TimelineCache{redis: redis}
}

// GetTimeline 获取用户的 Timeline（返回推文 ID 列表）
func (c *TimelineCache) GetTimeline(ctx context.Context, userID uint64, cursor uint64, limit int) ([]uint64, error) {
	key := c.getTimelineKey(userID)

	// 使用 ZREVRANGEBYSCORE 按分数（推文 ID）倒序获取
	// 因为 Snowflake ID 趋势递增，所以 ID 越大 = 时间越晚
	var maxScore string
	if cursor > 0 {
		maxScore = fmt.Sprintf("(%d", cursor)
	} else {
		maxScore = "+inf"
	}

	// ZREVRANGEBYSCORE key max min LIMIT offset count
	results, err := c.redis.ZRevRangeByScore(ctx, key, &redis.ZRangeBy{
		Min:    "-inf",
		Max:    maxScore,
		Offset: 0,
		Count:  int64(limit),
	}).Result()

	if err != nil {
		return nil, fmt.Errorf("failed to get timeline from redis: %w", err)
	}

	//转换为uint64
	tweetIDs := make([]uint64, 0, len(results))
	for _, result := range results {
		tweetID, err := strconv.ParseUint(result, 10, 64)
		if err != nil {
			continue
		}
		tweetIDs = append(tweetIDs, tweetID)
	}

	return tweetIDs, nil
}

// AddToTimeline 添加推文到用户的 Timeline
func (c *TimelineCache) AddToTimeline(ctx context.Context, userID uint64, tweetID uint64) error {
	key := c.getTimelineKey(userID)

	// 使用推文 ID 作为分数（因为 Snowflake ID 趋势递增）
	score := float64(tweetID)

	pipe := c.redis.Pipeline()

	// 添加到 Sorted Set
	pipe.ZAdd(ctx, key, &redis.Z{
		Score:  score,
		Member: tweetID,
	})

	// 只保留最新的 N 条
	pipe.ZRemRangeByRank(ctx, key, 0, -TimelineMaxSize-1)

	// 设置过期时间
	pipe.Expire(ctx, key, TimelineExpiration)

	//执行管道
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to add to timeline: %w", err)
	}

	return nil

}

// BatchAddToTimeline 批量添加推文到多个用户的 Timeline
func (c *TimelineCache) BatchAddToTimeline(ctx context.Context, userIDs []uint64, tweetID uint64) error {
	if len(userIDs) == 0 {
		return nil
	}

	pipe := c.redis.Pipeline()
	score := float64(tweetID)

	for _, userID := range userIDs {
		key := c.getTimelineKey(userID)

		// 添加到 Sorted Set
		pipe.ZAdd(ctx, key, &redis.Z{
			Score:  score,
			Member: tweetID,
		})

		// 只保留最新的 N 条
		pipe.ZRemRangeByRank(ctx, key, 0, -TimelineMaxSize-1)

		// 设置过期时间
		pipe.Expire(ctx, key, TimelineExpiration)
	}

	// 批量执行
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to batch add to timeline: %w", err)
	}

	return nil

}

// RemoveFromTimeline 从 Timeline 中删除推文
func (c *TimelineCache) RemoveFromTimeline(ctx context.Context, userID uint64, tweetID uint64) error {
	key := c.getTimelineKey(userID)

	err := c.redis.ZRem(ctx, key, tweetID).Err()
	if err != nil {
		return fmt.Errorf("failed to remove from timeline: %w", err)
	}

	return nil
}

// RemoveFromTimeline 从 Timeline 中删除推文
func (c *TimelineCache) BatchRemoveFromTimeline(ctx context.Context, userIDs []uint64, tweetID uint64) error {
	if len(userIDs) == 0 {
		return nil
	}

	pipe := c.redis.Pipeline()

	for _, userID := range userIDs {
		key := c.getTimelineKey(userID)
		pipe.ZRem(ctx, key, tweetID)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to batch remove from timeline: %w", err)
	}

	return nil
}

// ClearTimeline 清空用户的 Timeline
func (c *TimelineCache) ClearTimeline(ctx context.Context, userID uint64) error {
	key := c.getTimelineKey(userID)

	err := c.redis.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to clear timeline: %w", err)
	}

	return nil
}

// GetTimelineSize 获取 Timeline 大小
func (c *TimelineCache) GetTimelineSize(ctx context.Context, userID uint64) (int64, error) {
	key := c.getTimelineKey(userID)

	size, err := c.redis.ZCard(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get timeline size: %w", err)
	}

	return size, nil
}

// getTimelineKey 获取 Timeline 的 Redis Key
func (c *TimelineCache) getTimelineKey(userID uint64) string {
	return fmt.Sprintf("%s%d", TimelineKeyPrefix, userID)
}
