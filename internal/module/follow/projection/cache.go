package projection

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"strconv"
	"twitter-clone/internal/domain"
)

// Lua checks server time as well as a persistent monotonic execution epoch. A
// delayed Begin cannot replace a newer token, nor execute beyond the SQL lease.
const beginScript = `
local now=redis.call('TIME'); local ms=tonumber(now[1])*1000+math.floor(tonumber(now[2])/1000)
if tonumber(ARGV[3])<=ms then return 0 end
local kind=redis.call('TYPE',KEYS[1])['ok']; if kind~='none' and kind~='hash' then return redis.error_reply('invalid fence type') end
local global=redis.call('TYPE',KEYS[2])['ok']; if global~='none' and global~='set' then return redis.error_reply('invalid celebrity type') end
local old=redis.call('HGET',KEYS[1],'epoch')
if old and (#old>#ARGV[1] or (#old==#ARGV[1] and old>ARGV[1])) then return 0 end
if old==ARGV[1] and redis.call('HGET',KEYS[1],'token')~=ARGV[2] then return 0 end
redis.call('HSET',KEYS[1],'epoch',ARGV[1],'token',ARGV[2],'until',ARGV[3])
redis.call('EXPIRE',KEYS[1],604800)
if ARGV[4]=='1' then redis.call('SADD',KEYS[2],ARGV[5]) else redis.call('SREM',KEYS[2],ARGV[5]) end
return 1`

const fenceCheck = `
local now=redis.call('TIME'); local ms=tonumber(now[1])*1000+math.floor(tonumber(now[2])/1000)
if redis.call('HGET',KEYS[1],'epoch')~=ARGV[1] or redis.call('HGET',KEYS[1],'token')~=ARGV[2] then return 0 end
local untilValue=redis.call('HGET',KEYS[1],'until'); if not untilValue or tonumber(untilValue)<=ms then return 0 end
`
const followerScript = fenceCheck + `
local kind=redis.call('TYPE',KEYS[2])['ok']; if kind~='none' and kind~='set' then return redis.error_reply('invalid follower celebrity type') end
if ARGV[3]=='1' then redis.call('SADD',KEYS[2],ARGV[4]); redis.call('EXPIRE',KEYS[2],604800) else redis.call('SREM',KEYS[2],ARGV[4]) end
return 1`
const pairScript = fenceCheck + `
local kind=redis.call('TYPE',KEYS[2])['ok']; if kind~='none' and kind~='zset' then return redis.error_reply('invalid timeline type') end
local unreadKind=redis.call('TYPE',KEYS[3])['ok']; if unreadKind~='none' and unreadKind~='string' then return redis.error_reply('invalid unread type') end
local unread=tonumber(redis.call('GET',KEYS[3]) or '0'); if not unread then return redis.error_reply('invalid unread value') end
for i=3,#ARGV,3 do
  if ARGV[i]=='1' then redis.call('ZADD',KEYS[2],ARGV[i+2],ARGV[i+1])
  else local removed=redis.call('ZREM',KEYS[2],ARGV[i+1]); if removed>0 and unread>0 then unread=unread-1 end end
end
redis.call('ZREMRANGEBYRANK',KEYS[2],0,-1001)
if redis.call('EXISTS',KEYS[2])==1 then redis.call('EXPIRE',KEYS[2],604800) end
if redis.call('EXISTS',KEYS[3])==1 then redis.call('SET',KEYS[3],unread,'KEEPTTL') end
redis.call('DEL',KEYS[4],KEYS[5])
return 1`

type RedisCache struct {
	Client                   *redis.Client
	InvalidateCelebrityLocal func(uint64)
}

func (c *RedisCache) Celebrity(ctx context.Context, author uint64) (bool, error) {
	return c.Client.SIsMember(ctx, "global:celebrities", uintString(author)).Result()
}

func fenceKey(author uint64) string { return "follow:projection:fence:" + uintString(author) }
func (c *RedisCache) eval(ctx context.Context, script string, keys []string, args ...interface{}) error {
	value, err := c.Client.Eval(ctx, script, keys, args...).Int()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("follow cache write unavailable")
	}
	if value != 1 {
		return errors.New("follow cache fence rejected")
	}
	return nil
}
func (c *RedisCache) Begin(ctx context.Context, author uint64, state AuthorState, f Fence) error {
	if author == 0 || state.AuthorID != author || f.Epoch == 0 || f.Token == "" || f.Until <= 0 {
		return errors.New("invalid follow cache fence")
	}
	flag := "0"
	if state.Celebrity {
		flag = "1"
	}
	err := c.eval(ctx, beginScript, []string{fenceKey(author), "global:celebrities"}, uintString(f.Epoch), f.Token, f.Until, flag, uintString(author))
	if err == nil && c.InvalidateCelebrityLocal != nil {
		c.InvalidateCelebrityLocal(author)
	}
	return err
}
func (c *RedisCache) Follower(ctx context.Context, author, follower uint64, active bool, f Fence) error {
	if author == 0 || follower == 0 {
		return errors.New("invalid follow cache identity")
	}
	flag := "0"
	if active {
		flag = "1"
	}
	return c.eval(ctx, followerScript, []string{fenceKey(author), "user:celebrities:" + uintString(follower)}, uintString(f.Epoch), f.Token, flag, uintString(author))
}
func (c *RedisCache) PairTweets(ctx context.Context, author, follower uint64, following bool, rows []TweetRow, f Fence) error {
	if author == 0 || follower == 0 || len(rows) > PageSize {
		return errors.New("invalid follow cache page")
	}
	args := []interface{}{uintString(f.Epoch), f.Token}
	for _, row := range rows {
		if row.ID == 0 {
			return errors.New("invalid tweet identity")
		}
		flag := "0"
		if following && row.DeletedAt == 0 && (row.VisibleType == domain.VisiblePublic || row.VisibleType == domain.VisibleFollows) {
			flag = "1"
		}
		args = append(args, flag, uintString(row.ID), strconv.FormatFloat(float64(row.ID), 'g', -1, 64))
	}
	timeline := "timeline:" + uintString(follower)
	return c.eval(ctx, pairScript, []string{fenceKey(author), timeline, fmt.Sprint("unread:timeline:", follower), timeline + ":initialized:v2", timeline + ":rebuild:v2"}, args...)
}
