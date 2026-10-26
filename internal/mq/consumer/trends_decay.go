package consumer

import (
	"context"
	"errors"
	"github.com/go-redis/redis/v8"
	"log"
	"time"
)

const trendsDecayMaxMembers = 10000
const trendsDecayKeepMembers = 100

// The bucket and bounded score changes share one Redis execution. There is no
// expiring process lease between changing scores and recording completion.
var trendsDecayScript = redis.NewScript(`
local stateKind=redis.call('TYPE',KEYS[2])['ok'];if stateKind~='none' and stateKind~='string' then return -1 end
local now=redis.call('TIME');local bucket=math.floor(tonumber(now[1])/60)
local last=redis.call('GET',KEYS[2]);if last and (not string.match(last,'^%d+$') or #last>12 or (#last>1 and string.sub(last,1,1)=='0')) then return -1 end
if last and tonumber(last)>bucket then return -3 end
if last and tonumber(last)==bucket then return 0 end
local kind=redis.call('TYPE',KEYS[1])['ok'];if kind~='none' and kind~='zset' then return -1 end
local count=redis.call('ZCARD',KEYS[1]);if count>tonumber(ARGV[1]) then return -2 end
local ttl=redis.call('PTTL',KEYS[1]);local top=redis.call('ZREVRANGE',KEYS[1],0,tonumber(ARGV[2])-1,'WITHSCORES')
for i=2,#top,2 do local value=tonumber(top[i]);if not value or value~=value or value<0 or value>1000000000000 then return -1 end end
if count>tonumber(ARGV[2]) then redis.call('ZREMRANGEBYRANK',KEYS[1],0,-tonumber(ARGV[2])-1) end
for i=1,#top,2 do redis.call('ZADD',KEYS[1],tonumber(top[i+1])*0.95,top[i]) end
if count>0 then if ttl>=0 then redis.call('PEXPIRE',KEYS[1],ttl) else redis.call('EXPIRE',KEYS[1],86400) end end
redis.call('SET',KEYS[2],tostring(bucket),'EX',604800)
if count==0 then return 2 end
return 1
`)

func decayTrendBucket(ctx context.Context, client *redis.Client) (string, error) {
	if client == nil {
		return "failed", errors.New("trend decay unavailable")
	}
	result, err := trendsDecayScript.Run(ctx, client, []string{"trends:global", "trends:decay:bucket:v1"}, trendsDecayMaxMembers, trendsDecayKeepMembers).Int()
	if err != nil {
		if ctx.Err() != nil {
			return "cancelled", ctx.Err()
		}
		return "failed", errors.New("trend decay execution unavailable")
	}
	switch result {
	case 0:
		return "duplicate", nil
	case 1:
		return "applied", nil
	case 2:
		return "empty", nil
	case -2:
		return "oversized", errors.New("trend decay cardinality exceeds bound")
	case -3:
		return "clock_regression", errors.New("trend decay clock regressed")
	default:
		return "invalid_state", errors.New("trend decay state invalid")
	}
}
func (c *TimelineConsumer) startTrendsDecayWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		c.decayTrends(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (c *TimelineConsumer) decayTrends(ctx context.Context) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	result, err := decayTrendBucket(bounded, c.redisClient)
	if c.decayObserver != nil {
		c.decayObserver.Observe(result, time.Since(start))
	}
	if err != nil && ctx.Err() == nil {
		log.Printf("trend decay incomplete: result=%s", result)
	}
}
