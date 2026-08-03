package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	agentWebSearch "twitter-clone/internal/module/agent/websearch"

	"github.com/go-redis/redis/v8"
)

const (
	DefaultWebCachePrefix        = "agent:web:cache"
	DefaultWebGovernorPrefix     = "agent:web:governor"
	DefaultWebUserWindow         = time.Minute
	DefaultWebUserMaxRequests    = 30
	DefaultWebRunTTL             = 24 * time.Hour
	DefaultWebRunMaxRequests     = 12
	DefaultWebRunMaxCostMicros   = int64(50_000)
	maxWebAccessOperationRunes   = 32
	maxWebAccessCachePayloadSize = 1 << 20
)

type RedisWebCache struct {
	client *redis.Client
	prefix string
}

func NewRedisWebCache(client *redis.Client, prefix string) (*RedisWebCache, error) {
	if client == nil {
		return nil, errors.New("web cache Redis client is required")
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = DefaultWebCachePrefix
	}
	return &RedisWebCache{client: client, prefix: prefix}, nil
}

func (cache *RedisWebCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if cache == nil || cache.client == nil {
		return nil, false, errors.New("web cache is unavailable")
	}
	payload, err := cache.client.Get(ctx, cache.key(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read web cache: %w", err)
	}
	if len(payload) > maxWebAccessCachePayloadSize {
		return nil, false, errors.New("cached web payload exceeds safety limit")
	}
	return payload, true, nil
}

func (cache *RedisWebCache) Set(
	ctx context.Context,
	key string,
	payload []byte,
	ttl time.Duration,
) error {
	if cache == nil || cache.client == nil {
		return errors.New("web cache is unavailable")
	}
	if ttl < time.Millisecond {
		return errors.New("web cache TTL must be at least one millisecond")
	}
	if len(payload) == 0 || len(payload) > maxWebAccessCachePayloadSize {
		return errors.New("web cache payload is empty or exceeds safety limit")
	}
	if err := cache.client.Set(ctx, cache.key(key), payload, ttl).Err(); err != nil {
		return fmt.Errorf("write web cache: %w", err)
	}
	return nil
}

func (cache *RedisWebCache) key(key string) string {
	return cache.prefix + ":" + strings.TrimSpace(key)
}

type RedisWebAccessGovernorConfig struct {
	Prefix           string
	UserWindow       time.Duration
	UserMaxRequests  int
	RunTTL           time.Duration
	RunMaxRequests   int
	RunMaxCostMicros int64
}

type RedisWebAccessGovernor struct {
	client           *redis.Client
	prefix           string
	userWindow       time.Duration
	userMaxRequests  int
	runTTL           time.Duration
	runMaxRequests   int
	runMaxCostMicros int64
}

func NewRedisWebAccessGovernor(
	client *redis.Client,
	config RedisWebAccessGovernorConfig,
) (*RedisWebAccessGovernor, error) {
	if client == nil {
		return nil, errors.New("web access governor Redis client is required")
	}
	config.Prefix = strings.TrimSpace(config.Prefix)
	if config.Prefix == "" {
		config.Prefix = DefaultWebGovernorPrefix
	}
	if config.UserWindow < time.Millisecond {
		config.UserWindow = DefaultWebUserWindow
	}
	if config.UserMaxRequests <= 0 {
		config.UserMaxRequests = DefaultWebUserMaxRequests
	}
	if config.RunTTL < time.Millisecond {
		config.RunTTL = DefaultWebRunTTL
	}
	if config.RunMaxRequests <= 0 {
		config.RunMaxRequests = DefaultWebRunMaxRequests
	}
	if config.RunMaxCostMicros <= 0 {
		config.RunMaxCostMicros = DefaultWebRunMaxCostMicros
	}
	return &RedisWebAccessGovernor{
		client: client, prefix: config.Prefix,
		userWindow: config.UserWindow, userMaxRequests: config.UserMaxRequests,
		runTTL: config.RunTTL, runMaxRequests: config.RunMaxRequests,
		runMaxCostMicros: config.RunMaxCostMicros,
	}, nil
}

var webAccessAdmissionScript = redis.NewScript(`
local user_count = tonumber(redis.call("GET", KEYS[1]) or "0")
local run_count = tonumber(redis.call("GET", KEYS[2]) or "0")
local run_cost = tonumber(redis.call("GET", KEYS[3]) or "0")
local user_limit = tonumber(ARGV[1])
local run_limit = tonumber(ARGV[2])
local request_cost = tonumber(ARGV[3])
local cost_limit = tonumber(ARGV[4])
local user_ttl = tonumber(ARGV[5])
local run_ttl = tonumber(ARGV[6])

if user_count + 1 > user_limit then
	return 1
end
if run_count + 1 > run_limit then
	return 2
end
if run_cost + request_cost > cost_limit then
	return 3
end

local next_user = redis.call("INCR", KEYS[1])
if next_user == 1 then
	redis.call("PEXPIRE", KEYS[1], user_ttl)
end
local next_run = redis.call("INCR", KEYS[2])
if next_run == 1 then
	redis.call("PEXPIRE", KEYS[2], run_ttl)
end
local next_cost = redis.call("INCRBY", KEYS[3], request_cost)
if next_cost == request_cost then
	redis.call("PEXPIRE", KEYS[3], run_ttl)
end
return 0
`)

func (governor *RedisWebAccessGovernor) Admit(
	ctx context.Context,
	request agentWebSearch.AdmissionRequest,
) error {
	if governor == nil || governor.client == nil {
		return agentWebSearch.ErrAccessGovernor
	}
	request.Subject.RunID = strings.TrimSpace(request.Subject.RunID)
	if request.Subject.UserID == 0 || request.Subject.RunID == "" {
		return agentWebSearch.ErrAccessIdentityRequired
	}
	operation := strings.TrimSpace(string(request.Operation))
	if operation == "" || len([]rune(operation)) > maxWebAccessOperationRunes {
		return fmt.Errorf("%w: invalid operation", agentWebSearch.ErrAccessGovernor)
	}
	if request.CostMicros < 0 {
		return fmt.Errorf("%w: negative cost", agentWebSearch.ErrAccessGovernor)
	}
	bucket := time.Now().UnixMilli() / governor.userWindow.Milliseconds()
	runHash := sha256.Sum256([]byte(request.Subject.RunID))
	runKey := hex.EncodeToString(runHash[:])
	keys := []string{
		fmt.Sprintf("%s:user:%d:%d", governor.prefix, request.Subject.UserID, bucket),
		fmt.Sprintf("%s:run:%s:requests", governor.prefix, runKey),
		fmt.Sprintf("%s:run:%s:cost", governor.prefix, runKey),
	}
	code, err := webAccessAdmissionScript.Run(
		ctx,
		governor.client,
		keys,
		governor.userMaxRequests,
		governor.runMaxRequests,
		request.CostMicros,
		governor.runMaxCostMicros,
		governor.userWindow.Milliseconds(),
		governor.runTTL.Milliseconds(),
	).Int64()
	if err != nil {
		return fmt.Errorf("%w: Redis admission check", agentWebSearch.ErrAccessGovernor)
	}
	switch code {
	case 0:
		return nil
	case 1:
		return agentWebSearch.ErrAccessRateLimited
	case 2, 3:
		return agentWebSearch.ErrAccessBudgetExceeded
	default:
		return fmt.Errorf(
			"%w: unexpected admission result %s",
			agentWebSearch.ErrAccessGovernor,
			strconv.FormatInt(code, 10),
		)
	}
}
