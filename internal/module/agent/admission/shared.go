package admission

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"sync"
	"time"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

var ErrUnavailable = errors.New("shared admission unavailable")
var ErrLeaseLost = errors.New("shared admission lease lost")
var ErrBudgetExceeded = errors.New("shared provider reservation budget exceeded")

type Config struct {
	Namespace                                                             string
	Lease                                                                 time.Duration
	MaxRuns, MaxPerUser, MaxPerWorkflow, MaxProviderCalls, MaxPerProvider int
	MaxTokensPerMinute, MaxCostMicrosPerMinute                            int64
	OutputTokenLimit                                                      int
	CostMicrosPerToken                                                    int64
}

func DefaultConfig() Config {
	return Config{Namespace: "agent-v1", Lease: 30 * time.Second, MaxRuns: 64, MaxPerUser: 2, MaxPerWorkflow: 2, MaxProviderCalls: 8, MaxPerProvider: 8, MaxTokensPerMinute: 250000, OutputTokenLimit: 4096}
}
func (c Config) Validate() error {
	if c.Namespace == "" || len(c.Namespace) > 128 || c.Lease < 6*time.Second || c.Lease > time.Minute || c.OutputTokenLimit < 1 || c.OutputTokenLimit > 32768 || c.MaxTokensPerMinute < 1 || c.MaxTokensPerMinute > 1000000000 || c.MaxCostMicrosPerMinute < 0 || c.MaxCostMicrosPerMinute > 1000000000000 || c.CostMicrosPerToken < 0 || c.CostMicrosPerToken > 1000000 {
		return errors.New("invalid shared admission policy")
	}
	for _, limit := range []int{c.MaxRuns, c.MaxPerUser, c.MaxPerWorkflow, c.MaxProviderCalls, c.MaxPerProvider} {
		if limit < 1 || limit > 1024 {
			return errors.New("invalid shared concurrency limit")
		}
	}
	if c.MaxCostMicrosPerMinute > 0 && c.CostMicrosPerToken == 0 {
		return errors.New("cost reservation requires explicit upper pricing bound")
	}
	return nil
}

type scope struct {
	name  string
	limit int
}
type Shared struct {
	client         *redis.Client
	config         Config
	prefix, digest string
	observer       Observer
}
type Observer interface{ Observe(string, string) }

func New(client *redis.Client, config Config, observer Observer) (*Shared, error) {
	if client == nil {
		return nil, ErrUnavailable
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	digest := ConfigDigest(config)
	namespace := fmt.Sprintf("%x", sha256.Sum256([]byte(config.Namespace)))
	return &Shared{client: client, config: config, prefix: "agent:admission:{" + namespace + "}:", digest: digest, observer: observer}, nil
}
func (s *Shared) PolicyDigest() string { return s.digest }

func ConfigDigest(config Config) string {
	body, _ := json.Marshal(config)
	return ValueDigest(string(body))
}
func ValueDigest(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

// Initialization never clears slots/budgets and rejects policy drift. There is
// no automatic initialization when an execution path finds missing state.
func (s *Shared) Initialize(ctx context.Context) error {
	result, err := initializeScript.Run(ctx, s.client, []string{s.prefix + "ready"}, s.digest).Int()
	if err != nil || result != 1 {
		return ErrUnavailable
	}
	return nil
}
func (s *Shared) Ready(ctx context.Context) error {
	value, err := s.client.Get(ctx, s.prefix+"ready").Result()
	if err != nil || value != s.digest {
		return ErrUnavailable
	}
	return nil
}

var initializeScript = redis.NewScript(`local kind=redis.call('TYPE',KEYS[1])['ok'];if kind~='none' and kind~='string' then return 0 end;local old=redis.call('GET',KEYS[1]);if old and old~=ARGV[1] then return 0 end;if not old then redis.call('SET',KEYS[1],ARGV[1]) end;return 1`)

var leaseScript = redis.NewScript(`
if redis.call('TYPE',KEYS[1])['ok']~='string' or redis.call('GET',KEYS[1])~=ARGV[2] then return -1 end
local now=redis.call('TIME');local ms=tonumber(now[1])*1000+math.floor(tonumber(now[2])/1000);local action=ARGV[1]
local budgetKind=redis.call('TYPE',KEYS[2])['ok'];if budgetKind~='none' and budgetKind~='hash' then return -1 end
for i=3,#KEYS do local kind=redis.call('TYPE',KEYS[i])['ok'];if kind~='none' and kind~='zset' then return -1 end;if redis.call('ZCARD',KEYS[i])>2048 then return -1 end end
if action=='release' then for i=3,#KEYS do redis.call('ZREM',KEYS[i],ARGV[3]) end;return 1 end
if action=='renew' then
 for i=3,#KEYS do local score=redis.call('ZSCORE',KEYS[i],ARGV[3]);if not score or tonumber(score)<=ms then return -3 end end
 for i=3,#KEYS do redis.call('ZADD',KEYS[i],ms+tonumber(ARGV[4]),ARGV[3]);redis.call('PEXPIRE',KEYS[i],tonumber(ARGV[4])*3) end;return 1
end
for i=3,#KEYS do local count=redis.call('ZCOUNT',KEYS[i],'('..ms,'+inf');if count>=tonumber(ARGV[7+i]) then return -2 end end
local tokens=tonumber(ARGV[5]);local cost=tonumber(ARGV[6]);local bucket=math.floor(ms/60000);local usedTokens=0;local usedCost=0
if tokens>0 then
 local raw=redis.call('HMGET',KEYS[2],'bucket','tokens','cost')
 if raw[1] then
  for i=1,3 do if not raw[i] or #raw[i]>16 or not string.match(raw[i],'^%d+$') then return -1 end end
  local prior=tonumber(raw[1]);if prior>bucket then return -1 end
  if prior==bucket then usedTokens=tonumber(raw[2]);usedCost=tonumber(raw[3]) end
 elseif budgetKind=='hash' then return -1 end
 if usedTokens+tokens>tonumber(ARGV[7]) or (tonumber(ARGV[8])>0 and usedCost+cost>tonumber(ARGV[8])) then return -4 end
end
for i=3,#KEYS do redis.call('ZREMRANGEBYSCORE',KEYS[i],'-inf',ms);redis.call('ZADD',KEYS[i],ms+tonumber(ARGV[4]),ARGV[3]);redis.call('PEXPIRE',KEYS[i],tonumber(ARGV[4])*3) end
if tokens>0 then redis.call('HSET',KEYS[2],'bucket',string.format('%.0f',bucket),'tokens',string.format('%.0f',usedTokens+tokens),'cost',string.format('%.0f',usedCost+cost));redis.call('EXPIRE',KEYS[2],120) end
return 1
`)

type markerKey struct{}
type marker struct {
	shared  *Shared
	covered map[string]bool
}

func (s *Shared) Acquire(ctx context.Context, request agentRuntime.AdmissionRequest) (context.Context, agentRuntime.ReleaseFunc, error) {
	scopes := []scope{{"runs", s.config.MaxRuns}}
	if request.UserID != 0 {
		scopes = append(scopes, scope{fmt.Sprintf("user:%d", request.UserID), s.config.MaxPerUser})
	}
	if request.WorkflowID != "" {
		if len(request.WorkflowID) > 128 {
			return ctx, nil, ErrUnavailable
		}
		scopes = append(scopes, scope{"workflow:" + request.WorkflowID, s.config.MaxPerWorkflow})
	}
	covered := map[string]bool{}
	if parent, ok := ctx.Value(markerKey{}).(marker); ok && parent.shared == s {
		for name := range parent.covered {
			covered[name] = true
		}
	}
	needed := []scope{}
	for _, item := range scopes {
		if !covered[item.name] {
			needed = append(needed, item)
			covered[item.name] = true
		}
	}
	if len(needed) == 0 {
		if ctx.Err() != nil {
			return ctx, nil, ctx.Err()
		}
		return ctx, func() {}, nil
	}
	admitted, release, err := s.acquire(ctx, "run", needed, 0, 0)
	if err != nil {
		return ctx, nil, err
	}
	return context.WithValue(admitted, markerKey{}, marker{s, covered}), agentRuntime.ReleaseFunc(release), nil
}
func (s *Shared) key(name string) string {
	digest := sha256.Sum256([]byte(name))
	return s.prefix + fmt.Sprintf("scope:%x", digest)
}
func (s *Shared) acquire(ctx context.Context, kind string, scopes []scope, tokens, cost int64) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return ctx, nil, ErrUnavailable
	}
	token := hex.EncodeToString(raw)
	keys := []string{s.prefix + "ready", s.prefix + "provider-budget"}
	for _, item := range scopes {
		keys = append(keys, s.key(item.name))
	}
	args := []interface{}{"acquire", s.digest, token, s.config.Lease.Milliseconds(), tokens, cost, s.config.MaxTokensPerMinute, s.config.MaxCostMicrosPerMinute, 0}
	for _, item := range scopes {
		args = append(args, item.limit)
	}
	execute := func(parent context.Context, action string) error {
		bounded, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		args[0] = action
		result, err := leaseScript.Run(bounded, s.client, keys, args...).Int()
		if err != nil {
			return ErrUnavailable
		}
		switch result {
		case 1:
			return nil
		case -2:
			return agentRuntime.ErrConcurrencyLimitExceeded
		case -3:
			return ErrLeaseLost
		case -4:
			return ErrBudgetExceeded
		default:
			return ErrUnavailable
		}
	}
	if err := execute(ctx, "acquire"); err != nil {
		s.observe(kind, classify(err))
		return ctx, nil, err
	}
	s.observe(kind, "acquired")
	leased, cancel := context.WithCancel(ctx)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(s.config.Lease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-leased.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				if err := execute(leased, "renew"); err != nil {
					if leased.Err() == nil {
						s.observe(kind, "lease_lost")
					}
					cancel()
					return
				}
			}
		}
	}()
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel()
			close(stop)
			<-done
			err := execute(context.Background(), "release")
			if err != nil {
				s.observe(kind, "release_uncertain")
			} else {
				s.observe(kind, "released")
			}
		})
	}
	return leased, release, nil
}
func (s *Shared) observe(kind, result string) {
	if s.observer != nil {
		s.observer.Observe(kind, result)
	}
}
func classify(err error) string {
	if errors.Is(err, agentRuntime.ErrConcurrencyLimitExceeded) {
		return "limited"
	}
	if errors.Is(err, ErrBudgetExceeded) {
		return "budget_limited"
	}
	return "unavailable"
}
