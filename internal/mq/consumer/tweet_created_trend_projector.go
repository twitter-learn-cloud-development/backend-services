package consumer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	maxTweetCreatedTrendTopics = 32
	trendProjectionMarkerTTL   = 72 * time.Hour
	tweetTrendMappingTTL       = 48 * time.Hour
	trendRateLimitTTL          = time.Hour
	globalTrendTTL             = 24 * time.Hour
)

var fallbackTweetHashtagPattern = regexp.MustCompile(`#([\p{Han}A-Za-z0-9_][\p{Han}A-Za-z0-9_-]{0,63})`)

var tweetCreatedTrendProjectionScript = redis.NewScript(`
local marker_type = redis.call('TYPE', KEYS[1])['ok']
if marker_type ~= 'none' and marker_type ~= 'string' then
  return redis.error_reply('unexpected trend projection marker type')
end
if redis.call('EXISTS', KEYS[1]) == 1 then
  return 0
end

local tags_type = redis.call('TYPE', KEYS[2])['ok']
if tags_type ~= 'none' and tags_type ~= 'string' then
  return redis.error_reply('unexpected tweet tags type')
end
local trends_type = redis.call('TYPE', KEYS[3])['ok']
if trends_type ~= 'none' and trends_type ~= 'zset' then
  return redis.error_reply('unexpected global trends type')
end

local topic_count = tonumber(ARGV[5])
for i = 1, topic_count do
  local rate_type = redis.call('TYPE', KEYS[3 + i])['ok']
  if rate_type ~= 'none' and rate_type ~= 'string' then
    return redis.error_reply('unexpected trend rate limit type')
  end
end

if topic_count > 0 then
  redis.call('SET', KEYS[2], ARGV[6], 'EX', ARGV[2])
  for i = 1, topic_count do
    local argument_offset = 7 + ((i - 1) * 2)
    local topic = ARGV[argument_offset]
    local score = ARGV[argument_offset + 1]
    local count = redis.call('INCR', KEYS[3 + i])
    if count == 1 then
      redis.call('EXPIRE', KEYS[3 + i], ARGV[3])
    end
    if count <= 3 then
      redis.call('ZINCRBY', KEYS[3], score, topic)
    end
  end
  redis.call('EXPIRE', KEYS[3], ARGV[4])
end

redis.call('SET', KEYS[1], '1', 'EX', ARGV[1])
return 1
`)

type tweetCreatedTrendProjector struct {
	redis *redis.Client
}

type scoredTrendTopic struct {
	name  string
	score int64
}

func newTweetCreatedTrendProjector(redisClient *redis.Client) *tweetCreatedTrendProjector {
	return &tweetCreatedTrendProjector{redis: redisClient}
}

// Project applies all trend side effects atomically and returns false for a replay.
func (p *tweetCreatedTrendProjector) Project(
	ctx context.Context,
	tweetID uint64,
	authorID uint64,
	topics map[string]int64,
) (bool, error) {
	if p == nil || p.redis == nil {
		return false, errors.New("tweet created trend projector is unavailable")
	}
	if tweetID == 0 || authorID == 0 {
		return false, errors.New("tweet and author identifiers are required")
	}

	canonicalTopics := canonicalTrendTopics(topics)
	keys := []string{
		fmt.Sprintf("idempotency:timeline:tweet_created:trends:v1:%d", tweetID),
		fmt.Sprintf("tweet_tags:%d", tweetID),
		"trends:global",
	}
	tagNames := make([]string, 0, len(canonicalTopics))
	for _, topic := range canonicalTopics {
		tagNames = append(tagNames, topic.name)
		keys = append(keys, fmt.Sprintf("lock:user_tag_count:%d:%s", authorID, topic.name))
	}

	args := []interface{}{
		int64(trendProjectionMarkerTTL / time.Second),
		int64(tweetTrendMappingTTL / time.Second),
		int64(trendRateLimitTTL / time.Second),
		int64(globalTrendTTL / time.Second),
		len(canonicalTopics),
		strings.Join(tagNames, ","),
	}
	for _, topic := range canonicalTopics {
		args = append(args, topic.name, topic.score)
	}

	result, err := tweetCreatedTrendProjectionScript.Run(ctx, p.redis, keys, args...).Int64()
	if err != nil {
		return false, fmt.Errorf("project tweet-created trends: %w", err)
	}
	switch result {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("project tweet-created trends: unexpected result %s", strconv.FormatInt(result, 10))
	}
}

func extractFallbackTweetTopics(content string) map[string]int64 {
	topics := make(map[string]int64)
	for _, match := range fallbackTweetHashtagPattern.FindAllStringSubmatch(content, -1) {
		if len(match) <= 1 {
			continue
		}
		topic := strings.ToLower(normalizeTrendTopic(match[1]))
		if topic != "" {
			topics[topic] = 30
		}
	}
	return topics
}

func canonicalTrendTopics(topics map[string]int64) []scoredTrendTopic {
	merged := make(map[string]int64, len(topics))
	for rawTopic, score := range topics {
		if score <= 0 {
			continue
		}
		topic := strings.ToLower(normalizeTrendTopic(rawTopic))
		if topic == "" {
			continue
		}
		if current, exists := merged[topic]; !exists || score > current {
			merged[topic] = score
		}
	}

	ordered := make([]scoredTrendTopic, 0, len(merged))
	for topic, score := range merged {
		ordered = append(ordered, scoredTrendTopic{name: topic, score: score})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score == ordered[j].score {
			return ordered[i].name < ordered[j].name
		}
		return ordered[i].score > ordered[j].score
	})
	if len(ordered) > maxTweetCreatedTrendTopics {
		ordered = ordered[:maxTweetCreatedTrendTopics]
	}
	return ordered
}
