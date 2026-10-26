package consumer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	amqp "github.com/rabbitmq/amqp091-go"
	"sort"
	"strings"
	"time"
	"twitter-clone/internal/events"
	"unicode/utf8"
)

var errEngagementIdentityConflict = errors.New("engagement identity conflict")
var errEngagementInvalidTime = errors.New("invalid engagement occurrence time")

type engagementEvent struct {
	TweetID, UserID, AuthorID, EntityID        uint64
	Kind, Key, Digest, Traceparent, Tracestate string
	Occurred                                   int64
	Weight                                     int64
}

func decodeEngagement(msg amqp.Delivery, kind string) (engagementEvent, error) {
	if len(msg.Body) == 0 || len(msg.Body) > 256*1024 {
		return engagementEvent{}, errors.New("invalid engagement body")
	}
	e := engagementEvent{Kind: kind}
	var schema, key string
	if kind == RoutingKeyTweetLiked {
		var record events.TweetLikedEvent
		if json.Unmarshal(msg.Body, &record) != nil {
			return e, errors.New("invalid liked schema")
		}
		e.TweetID = record.TweetID
		e.UserID = record.UserID
		e.AuthorID = record.TweetUser
		e.EntityID = record.LikeID
		e.Occurred = record.OccurredAtUnixMS
		e.Weight = 2
		e.Traceparent = record.Traceparent
		e.Tracestate = record.Tracestate
		schema, key = record.SchemaVersion, record.EventKey
	} else if kind == RoutingKeyCommentCreated {
		var record events.CommentCreatedEvent
		if json.Unmarshal(msg.Body, &record) != nil {
			return e, errors.New("invalid comment schema")
		}
		e.TweetID = record.TweetID
		e.UserID = record.UserID
		e.AuthorID = record.TweetUser
		e.EntityID = record.CommentID
		e.Occurred = record.OccurredAtUnixMS
		e.Weight = 5
		e.Traceparent = record.Traceparent
		e.Tracestate = record.Tracestate
		schema, key = record.SchemaVersion, record.EventKey
	} else {
		return e, errors.New("unsupported engagement route")
	}
	if e.TweetID == 0 || e.UserID == 0 || e.AuthorID == 0 || e.Occurred <= 0 {
		return e, errors.New("missing engagement identity or occurrence time")
	}
	if schema != "" && schema != events.EngagementSchemaVersion {
		return e, errors.New("unsupported engagement version")
	}
	if schema == events.EngagementSchemaVersion {
		if e.EntityID == 0 {
			return e, errors.New("missing engagement entity")
		}
		expected := events.LikeEventKey(e.EntityID)
		if kind == RoutingKeyCommentCreated {
			expected = events.CommentEventKey(e.EntityID)
		}
		if key != expected {
			return e, errors.New("invalid engagement event key")
		}
		e.Key = key
	} else {
		if key != "" || (kind == RoutingKeyTweetLiked && e.EntityID != 0) {
			return e, errors.New("partial engagement version")
		}
		if kind == RoutingKeyCommentCreated {
			if e.EntityID == 0 {
				return e, errors.New("missing comment identity")
			}
			e.Key = events.CommentEventKey(e.EntityID)
		} else {
			body, _ := json.Marshal([]uint64{e.TweetID, e.UserID, e.AuthorID, uint64(e.Occurred)})
			e.Key = fmt.Sprintf("legacy-like:%x", sha256.Sum256(body))
		}
	}
	identity, _ := json.Marshal(struct {
		Kind                        string
		Tweet, User, Author, Entity uint64
		Occurred                    int64
	}{e.Kind, e.TweetID, e.UserID, e.AuthorID, e.EntityID, e.Occurred})
	e.Digest = fmt.Sprintf("%x", sha256.Sum256(identity))
	return e, nil
}

// Validate every type/value before the first write: Lua isolation does not roll
// back runtime errors. The outcome marker, rate TTL and scores commit together.
var engagementScript = redis.NewScript(`
local markerKind=redis.call('TYPE',KEYS[1])['ok']; if markerKind~='none' and markerKind~='string' then return -3 end
local marker=redis.call('GET',KEYS[1]); if marker then if string.sub(marker,1,64)~=ARGV[1] then return -2 end;return 0 end
local now=redis.call('TIME');local ms=tonumber(now[1])*1000+math.floor(tonumber(now[2])/1000)
local occurred=tonumber(ARGV[2]); if occurred>ms+300000 then return -4 end
if occurred<ms-172800000 then redis.call('SET',KEYS[1],ARGV[1]..':expired','EX',604800);return 4 end
local tagsKind=redis.call('TYPE',KEYS[2])['ok'];if tagsKind~='none' and tagsKind~='string' then return -3 end
if (redis.call('GET',KEYS[2]) or '')~=ARGV[3] then return -1 end
local globalKind=redis.call('TYPE',KEYS[3])['ok'];if globalKind~='none' and globalKind~='zset' then return -3 end
local createKind=redis.call('TYPE',KEYS[4])['ok'];if createKind~='none' and createKind~='string' then return -3 end
local topicCount=tonumber(ARGV[5]);if topicCount==0 and ARGV[6]=='1' and createKind=='none' then return -5 end
local counts={}
for i=1,topicCount do
 local rateKind=redis.call('TYPE',KEYS[4+i])['ok'];if rateKind~='none' and rateKind~='string' then return -3 end
 local raw=redis.call('GET',KEYS[4+i]);if raw and (not string.match(raw,'^%d+$') or (#raw>1 and string.sub(raw,1,1)=='0')) then return -3 end
 local count=tonumber(raw or '0');if not count or count<0 or count%1~=0 then return -3 end
 local score=tonumber(redis.call('ZSCORE',KEYS[3],ARGV[6+i]) or '0');if not score or score~=score or math.abs(score)>1000000000000 then return -3 end
 counts[i]=math.min(count,3)
end
local scored=0
for i=1,topicCount do
 if counts[i]<3 then
  redis.call('INCR',KEYS[4+i]);if redis.call('TTL',KEYS[4+i])<0 then redis.call('EXPIRE',KEYS[4+i],3600) end
  redis.call('ZINCRBY',KEYS[3],ARGV[4],ARGV[6+i]);scored=scored+1
 elseif redis.call('TTL',KEYS[4+i])<0 then redis.call('EXPIRE',KEYS[4+i],3600) end
end
if scored>0 then redis.call('EXPIRE',KEYS[3],86400) end
local result=1;if topicCount==0 then result=2 elseif scored==0 then result=3 end
redis.call('SET',KEYS[1],ARGV[1]..':'..result,'EX',604800)
return result
`)

type engagementProjector struct{ redis *redis.Client }

func (p *engagementProjector) Project(ctx context.Context, e engagementEvent, eligible bool) (string, error) {
	if p == nil || p.redis == nil {
		return "", errors.New("engagement projector unavailable")
	}
	tagsKey := fmt.Sprintf("tweet_tags:%d", e.TweetID)
	mapping, err := p.redis.GetRange(ctx, tagsKey, 0, 4096).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", errors.New("engagement topic mapping unavailable")
	}
	if len(mapping) > 4096 {
		return "", errors.New("engagement topic mapping exceeds bound")
	}
	topics := []string{}
	if eligible && mapping != "" {
		seen := map[string]bool{}
		for _, raw := range strings.Split(mapping, ",") {
			if raw == "" || !utf8.ValidString(raw) || normalizeTrendTopic(raw) != raw || utf8.RuneCountInString(raw) > maxTrendTopicRunes {
				return "", errors.New("invalid engagement topic mapping")
			}
			if !seen[raw] {
				topics = append(topics, raw)
				seen[raw] = true
			}
		}
		if len(topics) > maxTweetCreatedTrendTopics {
			return "", errors.New("engagement topics exceed bound")
		}
		sort.Strings(topics)
	}
	keyDigest := sha256.Sum256([]byte(e.Kind + ":" + e.Key))
	keys := []string{fmt.Sprintf("idempotency:engagement:v1:%x", keyDigest), tagsKey, "trends:global", fmt.Sprintf("idempotency:timeline:tweet_created:trends:v1:%d", e.TweetID)}
	eligibleFlag := "0"
	if eligible {
		eligibleFlag = "1"
	}
	args := []interface{}{e.Digest, e.Occurred, mapping, e.Weight, len(topics), eligibleFlag}
	for _, topic := range topics {
		keys = append(keys, fmt.Sprintf("lock:user_tag_count:%d:%s", e.UserID, topic))
		args = append(args, topic)
	}
	result, err := engagementScript.Run(ctx, p.redis, keys, args...).Int()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("engagement atomic projection unavailable")
	}
	switch result {
	case 0:
		return "duplicate", nil
	case 1:
		return "applied", nil
	case 2:
		return "no_topics", nil
	case 3:
		return "limited", nil
	case 4:
		return "expired", nil
	case -2:
		return "", errEngagementIdentityConflict
	case -4:
		return "", errEngagementInvalidTime
	default:
		return "", errors.New("engagement projection state unavailable")
	}
}

func engagementCurrentAge(e engagementEvent) bool {
	return e.Occurred >= time.Now().Add(-48*time.Hour).UnixMilli()
}
