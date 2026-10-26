package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

const beginFeedRecoveryScript = `
local state = redis.call('GET', KEYS[2])
if state == 'empty' or (state == 'filled' and redis.call('EXISTS', KEYS[1]) == 1) then return 0 end
local acquired = redis.call('SET', KEYS[3], ARGV[1], 'NX', 'PX', 3000)
if acquired then return 1 end
return 0
`

const commitFeedRecoveryScript = `
if redis.call('GET', KEYS[3]) ~= ARGV[1] then return 0 end
local kind = redis.call('TYPE', KEYS[1])['ok']
if kind ~= 'none' and kind ~= 'zset' then return redis.error_reply('invalid feed projection type') end
for i = 4, #ARGV, 2 do redis.call('ZADD', KEYS[1], ARGV[i], ARGV[i + 1]) end
redis.call('ZREMRANGEBYRANK', KEYS[1], 0, -1001)
redis.call('EXPIRE', KEYS[1], ARGV[2])
local state = 'empty'
if redis.call('ZCARD', KEYS[1]) > 0 then state = 'filled' end
redis.call('SET', KEYS[2], state, 'EX', ARGV[3])
redis.call('DEL', KEYS[3])
return 1
`

const releaseFeedRecoveryScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end
return 0
`

func (c *TimelineCache) feedRecoveryKeys(userID uint64) []string {
	key := c.getTimelineKey(userID)
	return []string{key, key + ":initialized:v2", key + ":rebuild:v2"}
}

// Recovery preserves concurrent fanout writes. Empty and missing differ, and
// a lost nonempty ZSet invalidates its marker. No cache entry proves permission.
func (c *TimelineCache) TryBeginFeedRecovery(ctx context.Context, userID uint64) (string, bool, error) {
	if userID == 0 {
		return "", false, fmt.Errorf("invalid feed recovery user")
	}
	var secret [16]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", false, err
	}
	token := hex.EncodeToString(secret[:])
	acquired, err := c.redis.Eval(ctx, beginFeedRecoveryScript, c.feedRecoveryKeys(userID), token).Int()
	if err != nil {
		return "", false, fmt.Errorf("begin feed projection recovery: %w", err)
	}
	return token, acquired == 1, nil
}

func (c *TimelineCache) CommitFeedRecovery(ctx context.Context, userID uint64, token string, ids []uint64) (bool, error) {
	if userID == 0 || token == "" || len(ids) > TimelineMaxSize {
		return false, fmt.Errorf("invalid feed recovery commit")
	}
	args := []interface{}{token, int64(TimelineExpiration / time.Second), 300}
	seen := make(map[uint64]bool)
	for _, id := range ids {
		if id == 0 || seen[id] {
			return false, fmt.Errorf("invalid feed recovery IDs")
		}
		seen[id] = true
		args = append(args, strconv.FormatFloat(float64(id), 'g', -1, 64), strconv.FormatUint(id, 10))
	}
	committed, err := c.redis.Eval(ctx, commitFeedRecoveryScript, c.feedRecoveryKeys(userID), args...).Int()
	if err != nil {
		return false, fmt.Errorf("commit feed projection recovery: %w", err)
	}
	return committed == 1, nil
}

func (c *TimelineCache) ReleaseFeedRecovery(ctx context.Context, userID uint64, token string) error {
	return c.redis.Eval(ctx, releaseFeedRecoveryScript, []string{c.feedRecoveryKeys(userID)[2]}, token).Err()
}
