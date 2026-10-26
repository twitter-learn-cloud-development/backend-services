package cache

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/go-redis/redis/v8"
)

// Redis scores are doubles; neighboring uint64 IDs can share a score. Read
// the cursor bucket and the tail bucket atomically, then seek on exact IDs.
// At uint64 precision one bucket holds at most 4096 distinct integer IDs.
const exactTimelinePageScript = `
local result = {}
local seen = {}
local function add(ids)
    for _, id in ipairs(ids) do
        if not seen[id] then seen[id] = true; table.insert(result, id) end
    end
end
local function bucket(score)
    local ids = redis.call('ZREVRANGEBYSCORE', KEYS[1], score, score, 'LIMIT', 0, 4097)
    if #ids > 4096 then return nil end
    return ids
end
local max = '+inf'
if ARGV[1] ~= '0' then
    local tied = bucket(ARGV[1])
    if not tied then return redis.error_reply('invalid timeline score bucket') end
    add(tied)
    max = '(' .. ARGV[1]
end
local rows = redis.call('ZREVRANGEBYSCORE', KEYS[1], max, '-inf', 'WITHSCORES', 'LIMIT', 0, ARGV[2])
for i = 1, #rows, 2 do add({rows[i]}) end
if #rows > 0 then
    local tied = bucket(rows[#rows])
    if not tied then return redis.error_reply('invalid timeline score bucket') end
    add(tied)
end
return result
`

func decodeTimelinePage(value interface{}, cursor uint64, limit int) ([]uint64, error) {
	items, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid timeline page response")
	}
	ids := make([]uint64, 0, len(items))
	seen := make(map[uint64]bool)
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("invalid timeline member")
		}
		id, err := strconv.ParseUint(text, 10, 64)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != text {
			return nil, fmt.Errorf("invalid timeline member")
		}
		if (cursor == 0 || id < cursor) && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (c *TimelineCache) exactTimelinePage(ctx context.Context, key string, cursor uint64, limit int) ([]uint64, error) {
	if limit < 1 || limit > TimelineMaxSize+1 {
		return nil, fmt.Errorf("invalid timeline page limit")
	}
	value, err := c.redis.Eval(ctx, exactTimelinePageScript, []string{key}, strconv.FormatUint(cursor, 10), limit).Result()
	if err != nil {
		return nil, fmt.Errorf("read timeline ID page: %w", err)
	}
	return decodeTimelinePage(value, cursor, limit)
}

func enqueueTimelinePage(ctx context.Context, pipe redis.Pipeliner, key string, cursor uint64, limit int) *redis.Cmd {
	return pipe.Eval(ctx, exactTimelinePageScript, []string{key}, strconv.FormatUint(cursor, 10), limit)
}
