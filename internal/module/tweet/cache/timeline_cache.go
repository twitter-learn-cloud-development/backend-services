package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"time"
	"twitter-clone/internal/domain"
	"twitter-clone/pkg/config"

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

	UnreadTimelineKeyPrefix = "unread:timeline:"

	removeTimelineAndUnreadScript = `
local removed = redis.call('ZREM', KEYS[1], ARGV[1])
if removed > 0 then
    local unread = tonumber(redis.call('GET', KEYS[2]))
    if unread and unread > 0 then
        redis.call('DECR', KEYS[2])
    end
end
return removed
`
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
// BatchRemoveFromTimelineAndUnread atomically removes a tweet and adjusts each
// user's unread counter. Repeating the call is safe because the counter changes
// only when ZREM actually removes the tweet.
func (c *TimelineCache) BatchRemoveFromTimelineAndUnread(ctx context.Context, userIDs []uint64, tweetID uint64) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	pipe := c.redis.Pipeline()
	commands := make([]*redis.Cmd, 0, len(userIDs))
	for _, userID := range userIDs {
		commands = append(commands, pipe.Eval(
			ctx,
			removeTimelineAndUnreadScript,
			[]string{
				c.getTimelineKey(userID),
				fmt.Sprintf("%s%d", UnreadTimelineKeyPrefix, userID),
			},
			tweetID,
		))
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("failed to remove moderated tweet from timelines: %w", err)
	}

	removed := 0
	for _, command := range commands {
		value, err := command.Int64()
		if err != nil {
			return removed, fmt.Errorf("failed to read timeline cleanup result: %w", err)
		}
		removed += int(value)
	}
	return removed, nil
}

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
	l1TTL := time.Duration(config.GetCurrentConfig().L1CacheTTLSeconds) * time.Second
	c.localCache.mu.Lock()
	c.localCache.items[userID] = localCacheItem{
		val:       isCelebrity,
		expiredAt: time.Now().Add(l1TTL),
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
	l1TTL := time.Duration(config.GetCurrentConfig().L1CacheTTLSeconds) * time.Second
	c.localCache.mu.Lock()
	c.localCache.items[userID] = localCacheItem{
		val:       true,
		expiredAt: time.Now().Add(l1TTL),
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
	l1TTL := time.Duration(config.GetCurrentConfig().L1CacheTTLSeconds) * time.Second
	c.localCache.mu.Lock()
	c.localCache.items[userID] = localCacheItem{
		val:       false,
		expiredAt: time.Now().Add(l1TTL),
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

// GetBaseTweet 从 Redis 获取推文基本信息 (L2 缓存)
func (c *TimelineCache) GetBaseTweet(ctx context.Context, tweetID uint64) (*domain.Tweet, error) {
	key := fmt.Sprintf("tweet:base:%d", tweetID)
	val, err := c.redis.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var tweet domain.Tweet
	if err := json.Unmarshal([]byte(val), &tweet); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tweet: %w", err)
	}

	return &tweet, nil
}

// SetBaseTweet 写入推文基本信息到 Redis，带防雪崩随机 TTL
func (c *TimelineCache) SetBaseTweet(ctx context.Context, tweet *domain.Tweet) error {
	key := fmt.Sprintf("tweet:base:%d", tweet.ID)
	data, err := json.Marshal(tweet)
	if err != nil {
		return fmt.Errorf("failed to marshal tweet: %w", err)
	}

	// 🎯 动态配置：优先从 dynamic_config 获取 L2 TTL，并带防雪崩随机 jitter
	l2TTLSec := config.GetCurrentConfig().L2CacheTTLSeconds
	jitter := time.Duration(rand.Intn(1800)) * time.Second
	ttl := time.Duration(l2TTLSec)*time.Second + jitter

	return c.redis.Set(ctx, key, data, ttl).Err()
}

// DeleteBaseTweet 从 Redis 删除推文基本信息
func (c *TimelineCache) DeleteBaseTweet(ctx context.Context, tweetID uint64) error {
	key := fmt.Sprintf("tweet:base:%d", tweetID)
	return c.redis.Del(ctx, key).Err()
}

// MGetBaseTweets 批量从 Redis 获取推文基本信息 (L2 缓存)
func (c *TimelineCache) MGetBaseTweets(ctx context.Context, tweetIDs []uint64) (map[uint64]*domain.Tweet, error) {
	if len(tweetIDs) == 0 {
		return make(map[uint64]*domain.Tweet), nil
	}

	keys := make([]string, len(tweetIDs))
	for i, id := range tweetIDs {
		keys[i] = fmt.Sprintf("tweet:base:%d", id)
	}

	results, err := c.redis.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to mget tweets from redis: %w", err)
	}

	tweetMap := make(map[uint64]*domain.Tweet)
	for i, res := range results {
		if res == nil {
			continue
		}
		str, ok := res.(string)
		if !ok {
			continue
		}
		var tweet domain.Tweet
		if err := json.Unmarshal([]byte(str), &tweet); err == nil {
			tweetMap[tweetIDs[i]] = &tweet
		}
	}

	return tweetMap, nil
}

// InvalidateBaseTweet 广播删除 L1/L2 缓存
func (c *TimelineCache) InvalidateBaseTweet(ctx context.Context, tweetID uint64) error {
	_ = c.DeleteBaseTweet(ctx, tweetID)
	key := fmt.Sprintf("tweet:base:%d", tweetID)
	return c.redis.Publish(ctx, "tweet_invalidations", key).Err()
}

// UserTimelineExpiration 大V时间线缓存过期时间为 7 天
const UserTimelineExpiration = 7 * 24 * time.Hour

func (c *TimelineCache) getUserTimelineKey(userID uint64) string {
	return fmt.Sprintf("user_timeline:%d", userID)
}

func (c *TimelineCache) getUserTimelineInitKey(userID uint64) string {
	return fmt.Sprintf("user_timeline:%d:initialized", userID)
}

// AddToUserTimeline 添加推文到用户自己的时间线 (针对大V缓存)
func (c *TimelineCache) AddToUserTimeline(ctx context.Context, userID uint64, tweetID uint64) error {
	key := c.getUserTimelineKey(userID)
	initKey := c.getUserTimelineInitKey(userID)

	pipe := c.redis.Pipeline()
	pipe.ZAdd(ctx, key, &redis.Z{
		Score:  float64(tweetID),
		Member: tweetID,
	})
	// 只保留最新的 1000 条
	pipe.ZRemRangeByRank(ctx, key, 0, -1001)
	pipe.Expire(ctx, key, UserTimelineExpiration)
	pipe.Set(ctx, initKey, "1", UserTimelineExpiration)

	_, err := pipe.Exec(ctx)
	return err
}

// RemoveFromUserTimeline 从用户自己的时间线删除推文
func (c *TimelineCache) RemoveFromUserTimeline(ctx context.Context, userID uint64, tweetID uint64) error {
	key := c.getUserTimelineKey(userID)
	return c.redis.ZRem(ctx, key, tweetID).Err()
}

// RebuildUserTimeline 重建用户时间线 ZSet 并设置初始化标志
func (c *TimelineCache) RebuildUserTimeline(ctx context.Context, userID uint64, tweetIDs []uint64) error {
	key := c.getUserTimelineKey(userID)
	initKey := c.getUserTimelineInitKey(userID)

	pipe := c.redis.Pipeline()
	pipe.Del(ctx, key)

	if len(tweetIDs) > 0 {
		members := make([]*redis.Z, len(tweetIDs))
		for i, id := range tweetIDs {
			members[i] = &redis.Z{
				Score:  float64(id),
				Member: id,
			}
		}
		pipe.ZAdd(ctx, key, members...)
		pipe.ZRemRangeByRank(ctx, key, 0, -1001)
	}
	pipe.Expire(ctx, key, UserTimelineExpiration)
	pipe.Set(ctx, initKey, "1", UserTimelineExpiration)

	_, err := pipe.Exec(ctx)
	return err
}

// PipelineGetCelebrityTweets 批量使用 Pipeline 获取多个大V的推文 ID (L2 Pull 缓存聚合)
func (c *TimelineCache) PipelineGetCelebrityTweets(ctx context.Context, celebrityIDs []uint64, cursor uint64, limit int) (map[uint64][]uint64, []uint64, error) {
	if len(celebrityIDs) == 0 {
		return make(map[uint64][]uint64), nil, nil
	}

	pipe := c.redis.Pipeline()

	var maxScore string
	if cursor > 0 {
		maxScore = fmt.Sprintf("(%d", cursor)
	} else {
		maxScore = "+inf"
	}

	type cmdGroup struct {
		existsCmd *redis.IntCmd
		zrangeCmd *redis.StringSliceCmd
	}
	cmds := make(map[uint64]cmdGroup)

	for _, id := range celebrityIDs {
		key := c.getUserTimelineKey(id)
		initKey := c.getUserTimelineInitKey(id)

		existsCmd := pipe.Exists(ctx, initKey)
		zrangeCmd := pipe.ZRevRangeByScore(ctx, key, &redis.ZRangeBy{
			Min:    "-inf",
			Max:    maxScore,
			Offset: 0,
			Count:  int64(limit),
		})

		cmds[id] = cmdGroup{
			existsCmd: existsCmd,
			zrangeCmd: zrangeCmd,
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, nil, fmt.Errorf("pipeline exec failed: %w", err)
	}

	results := make(map[uint64][]uint64)
	var missingIDs []uint64

	for id, g := range cmds {
		exists, err := g.existsCmd.Result()
		if err != nil || exists == 0 {
			missingIDs = append(missingIDs, id)
			continue
		}

		members, err := g.zrangeCmd.Result()
		if err != nil {
			continue
		}

		ids := make([]uint64, 0, len(members))
		for _, m := range members {
			tweetID, err := strconv.ParseUint(m, 10, 64)
			if err == nil {
				ids = append(ids, tweetID)
			}
		}
		results[id] = ids
	}

	return results, missingIDs, nil
}
