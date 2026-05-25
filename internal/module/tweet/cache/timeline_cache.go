package cache

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
	"twitter-clone/internal/domain"

	"github.com/go-redis/redis/v8"
)

const (
	// TimelineKeyPrefix Timeline 缓存键前缀
	TimelineKeyPrefix = "timeline:"

	// TimelineMaxSize Timeline 最大缓存数量
	TimelineMaxSize = 1000

	// TimelineExpiration Timeline 过期时间
	TimelineExpiration = 7 * 24 * time.Hour

	// GlobalCelebrityKey 全局大V集合Key
	GlobalCelebrityKey = "global:celebrities"

	// UserCelebrityKeyPrefix 用户关注大V集合Key前缀
	UserCelebrityKeyPrefix = "user:celebrities:"
)

type localCacheItem struct {
	val       bool
	expiredAt time.Time
}

type celebrityLocalCache struct {
	mu    sync.RWMutex
	items map[uint64]localCacheItem
	ttl   time.Duration
}

// TimelineCache Timeline 缓存
type TimelineCache struct {
	redis      *redis.Client
	localCache *celebrityLocalCache
}

// NewTimelineCache 创建 Timeline 缓存
func NewTimelineCache(redis *redis.Client) *TimelineCache {
	return &TimelineCache{
		redis: redis,
		localCache: &celebrityLocalCache{
			items: make(map[uint64]localCacheItem),
			ttl:   1 * time.Minute, // 默认大V本地一级缓存失效时间为 1 分钟
		},
	}
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

// GetTrendingTopics 获取热门话题
func (c *TimelineCache) GetTrendingTopics(ctx context.Context, limit int) ([]*domain.TrendingTopic, error) {
	// ZREVRANGE trends:global 0 limit-1 WITHSCORES
	res, err := c.redis.ZRevRangeWithScores(ctx, "trends:global", 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get trending topics: %w", err)
	}

	topics := make([]*domain.TrendingTopic, 0, len(res))
	for _, z := range res {
		topics = append(topics, &domain.TrendingTopic{
			Topic: z.Member.(string),
			Score: int32(z.Score),
		})
	}
	return topics, nil
}

// IsCelebrity 检查用户是否是大V
func (c *TimelineCache) IsCelebrity(ctx context.Context, userID uint64) (bool, error) {
	// 1. 优先从本地一级缓存读取
	c.localCache.mu.RLock()
	item, exists := c.localCache.items[userID]
	c.localCache.mu.RUnlock()

	if exists && time.Now().Before(item.expiredAt) {
		return item.val, nil
	}

	// 2. 缓存未命中或已失效，从 Redis 读取
	isCelebrity, err := c.redis.SIsMember(ctx, GlobalCelebrityKey, userID).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check celebrity status: %w", err)
	}

	// 3. 回填本地缓存
	c.localCache.mu.Lock()
	c.localCache.items[userID] = localCacheItem{
		val:       isCelebrity,
		expiredAt: time.Now().Add(c.localCache.ttl),
	}
	c.localCache.mu.Unlock()

	return isCelebrity, nil
}

// AddCelebrity 添加到全局大V集合
func (c *TimelineCache) AddCelebrity(ctx context.Context, userID uint64) error {
	err := c.redis.SAdd(ctx, GlobalCelebrityKey, userID).Err()
	if err != nil {
		return fmt.Errorf("failed to add celebrity: %w", err)
	}

	// 同步更新本地 L1 缓存
	c.localCache.mu.Lock()
	c.localCache.items[userID] = localCacheItem{
		val:       true,
		expiredAt: time.Now().Add(c.localCache.ttl),
	}
	c.localCache.mu.Unlock()

	return nil
}

// RemoveCelebrity 从全局大V中移除
func (c *TimelineCache) RemoveCelebrity(ctx context.Context, userID uint64) error {
	err := c.redis.SRem(ctx, GlobalCelebrityKey, userID).Err()
	if err != nil {
		return fmt.Errorf("failed to remove celebrity: %w", err)
	}

	// 同步从本地 L1 缓存中置为 false
	c.localCache.mu.Lock()
	c.localCache.items[userID] = localCacheItem{
		val:       false,
		expiredAt: time.Now().Add(c.localCache.ttl),
	}
	c.localCache.mu.Unlock()

	return nil
}

// GetCelebrityFollowees 获取用户关注的大V ID 列表
func (c *TimelineCache) GetCelebrityFollowees(ctx context.Context, userID uint64) ([]uint64, error) {
	key := c.getUserCelebrityKey(userID)
	results, err := c.redis.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get celebrity followees from redis: %w", err)
	}

	followeeIDs := make([]uint64, 0, len(results))
	for _, res := range results {
		id, err := strconv.ParseUint(res, 10, 64)
		if err != nil {
			continue
		}
		followeeIDs = append(followeeIDs, id)
	}
	return followeeIDs, nil
}

// AddCelebrityFollowee 用户关注大V时添加至关联集合
func (c *TimelineCache) AddCelebrityFollowee(ctx context.Context, userID uint64, celebrityID uint64) error {
	key := c.getUserCelebrityKey(userID)
	err := c.redis.SAdd(ctx, key, celebrityID).Err()
	if err != nil {
		return fmt.Errorf("failed to add celebrity followee: %w", err)
	}
	return nil
}

// RemoveCelebrityFollowee 用户取消关注大V时从关联集合中移除
func (c *TimelineCache) RemoveCelebrityFollowee(ctx context.Context, userID uint64, celebrityID uint64) error {
	key := c.getUserCelebrityKey(userID)
	err := c.redis.SRem(ctx, key, celebrityID).Err()
	if err != nil {
		return fmt.Errorf("failed to remove celebrity followee: %w", err)
	}
	return nil
}

// getUserCelebrityKey 获取用户关注大V的 Key
func (c *TimelineCache) getUserCelebrityKey(userID uint64) string {
	return fmt.Sprintf("%s%d", UserCelebrityKeyPrefix, userID)
}

// BatchAddCelebrityFollowees 批量为多个用户添加关注的某个大V
func (c *TimelineCache) BatchAddCelebrityFollowees(ctx context.Context, userIDs []uint64, celebrityID uint64) error {
	if len(userIDs) == 0 {
		return nil
	}

	pipe := c.redis.Pipeline()
	for _, userID := range userIDs {
		key := c.getUserCelebrityKey(userID)
		pipe.SAdd(ctx, key, celebrityID)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to batch add celebrity followees: %w", err)
	}
	return nil
}

// BatchRemoveCelebrityFollowees 批量为多个用户移除关注的某个大V
func (c *TimelineCache) BatchRemoveCelebrityFollowees(ctx context.Context, userIDs []uint64, celebrityID uint64) error {
	if len(userIDs) == 0 {
		return nil
	}

	pipe := c.redis.Pipeline()
	for _, userID := range userIDs {
		key := c.getUserCelebrityKey(userID)
		pipe.SRem(ctx, key, celebrityID)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to batch remove celebrity followees: %w", err)
	}
	return nil
}

// SyncGlobalCelebrities 差分校准全局大V
func (c *TimelineCache) SyncGlobalCelebrities(ctx context.Context, dbCelebrities []uint64) error {
	// 1. 将 dbCelebrities 放入一个 map 方便 O(1) 查找
	dbMap := make(map[uint64]bool, len(dbCelebrities))
	for _, id := range dbCelebrities {
		dbMap[id] = true
	}

	// 2. 获取 Redis 中的当前大V列表
	results, err := c.redis.SMembers(ctx, GlobalCelebrityKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get global celebrities: %w", err)
	}

	redisMap := make(map[uint64]bool, len(results))
	for _, res := range results {
		id, err := strconv.ParseUint(res, 10, 64)
		if err != nil {
			continue
		}
		redisMap[id] = true
	}

	// 3. 差分比对：
	pipe := c.redis.Pipeline()

	// - 在 DB 中但不在 Redis 中的，SADD
	for id := range dbMap {
		if !redisMap[id] {
			pipe.SAdd(ctx, GlobalCelebrityKey, id)
		}
	}

	// - 在 Redis 中但已不在 DB 中的，SREM
	for id := range redisMap {
		if !dbMap[id] {
			pipe.SRem(ctx, GlobalCelebrityKey, id)
		}
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to sync global celebrities: %w", err)
	}
	return nil
}
