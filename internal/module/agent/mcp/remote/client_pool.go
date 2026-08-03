package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	PoolEventOpened      = "opened"
	PoolEventReused      = "reused"
	PoolEventClosed      = "closed"
	PoolEventOpenFailed  = "open_failed"
	PoolEventSaturated   = "saturated"
	PoolEventInvalidated = "invalidated"
)

type ClientPoolConfig struct {
	Enabled                  bool
	MaxSessions              int
	MaxSessionsPerConnection int
	IdleTimeout              time.Duration
	AcquireTimeout           time.Duration
}

func DefaultClientPoolConfig() ClientPoolConfig {
	return ClientPoolConfig{
		Enabled:                  true,
		MaxSessions:              64,
		MaxSessionsPerConnection: 2,
		IdleTimeout:              5 * time.Minute,
		AcquireTimeout:           2 * time.Second,
	}
}

func normalizeClientPoolConfig(config ClientPoolConfig) ClientPoolConfig {
	defaults := DefaultClientPoolConfig()
	if config.MaxSessions <= 0 {
		config.MaxSessions = defaults.MaxSessions
	}
	if config.MaxSessionsPerConnection <= 0 {
		config.MaxSessionsPerConnection = defaults.MaxSessionsPerConnection
	}
	if config.MaxSessionsPerConnection > config.MaxSessions {
		config.MaxSessionsPerConnection = config.MaxSessions
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = defaults.IdleTimeout
	}
	if config.AcquireTimeout <= 0 {
		config.AcquireTimeout = defaults.AcquireTimeout
	}
	return config
}

type PoolStats struct {
	Total   int
	Idle    int
	InUse   int
	Opening int
}

type PoolObserver interface {
	RecordPoolEvent(event string)
	SetPoolStats(stats PoolStats)
}

type remoteSession interface {
	ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Ping(context.Context) error
	Close() error
}

type remoteSessionFactory func(context.Context, DiscoveryRequest) (remoteSession, error)

type pooledSession struct {
	client   remoteSession
	lastUsed time.Time
}

type sessionBucket struct {
	identity     string
	connectionID string
	invalidated  bool
	total        int
	idle         []*pooledSession
}

type clientPool struct {
	mu        sync.Mutex
	config    ClientPoolConfig
	factory   remoteSessionFactory
	observer  PoolObserver
	now       func() time.Time
	buckets   map[string]*sessionBucket
	blocked   map[string]time.Time
	notify    chan struct{}
	total     int
	opening   int
	closed    bool
	closeOnce sync.Once
}

type clientLease struct {
	pool    *clientPool
	bucket  *sessionBucket
	session *pooledSession
	once    sync.Once
}

func newClientPool(
	config ClientPoolConfig,
	factory remoteSessionFactory,
	observer PoolObserver,
) *clientPool {
	config = normalizeClientPoolConfig(config)
	return &clientPool{
		config: config, factory: factory, observer: observer, now: time.Now,
		buckets: make(map[string]*sessionBucket), blocked: make(map[string]time.Time),
		notify: make(chan struct{}),
	}
}

func (pool *clientPool) Acquire(ctx context.Context, request DiscoveryRequest) (*clientLease, error) {
	if pool == nil || pool.factory == nil {
		return nil, errors.New("external MCP session factory is unavailable")
	}
	acquireCtx, cancel := context.WithTimeout(ctx, pool.config.AcquireTimeout)
	defer cancel()
	identity := clientPoolIdentity(request)

	for {
		pool.mu.Lock()
		toClose := pool.pruneLocked(pool.now())
		if pool.closed {
			stats := pool.statsLocked()
			pool.mu.Unlock()
			closeSessions(toClose)
			pool.observeStats(stats)
			return nil, ErrClientPoolClosed
		}
		if err := ctx.Err(); err != nil {
			stats := pool.statsLocked()
			pool.mu.Unlock()
			closeSessions(toClose)
			pool.observeStats(stats)
			return nil, err
		}
		if acquireCtx.Err() != nil {
			stats := pool.statsLocked()
			pool.mu.Unlock()
			closeSessions(toClose)
			pool.observeEvent(PoolEventSaturated)
			pool.observeStats(stats)
			return nil, ErrClientPoolSaturated
		}
		if blockedUntil, blocked := pool.blocked[identity]; blocked && blockedUntil.After(pool.now()) {
			stats := pool.statsLocked()
			pool.mu.Unlock()
			closeSessions(toClose)
			pool.observeStats(stats)
			return nil, ErrConnectionInvalidated
		}
		bucket := pool.buckets[identity]
		if bucket != nil && len(bucket.idle) > 0 {
			last := len(bucket.idle) - 1
			session := bucket.idle[last]
			bucket.idle = bucket.idle[:last]
			stats := pool.statsLocked()
			pool.mu.Unlock()
			closeSessions(toClose)
			pool.observeEvent(PoolEventReused)
			pool.observeStats(stats)
			return &clientLease{pool: pool, bucket: bucket, session: session}, nil
		}
		if bucket == nil {
			bucket = &sessionBucket{identity: identity, connectionID: request.ConnectionID}
			pool.buckets[identity] = bucket
		}
		if bucket.total < pool.config.MaxSessionsPerConnection && pool.total < pool.config.MaxSessions {
			bucket.total++
			pool.total++
			pool.opening++
			stats := pool.statsLocked()
			pool.mu.Unlock()
			closeSessions(toClose)
			pool.observeStats(stats)

			session, err := pool.factory(acquireCtx, request)
			if err != nil {
				pool.mu.Lock()
				bucket.total--
				pool.total--
				pool.opening--
				pool.removeBucketIfEmptyLocked(bucket)
				pool.signalLocked()
				stats = pool.statsLocked()
				pool.mu.Unlock()
				pool.observeEvent(PoolEventOpenFailed)
				pool.observeStats(stats)
				return nil, err
			}

			pool.mu.Lock()
			pool.opening--
			invalidated := pool.closed || bucket.invalidated
			if blockedUntil, blocked := pool.blocked[identity]; blocked && blockedUntil.After(pool.now()) {
				invalidated = true
			}
			if invalidated {
				bucket.total--
				pool.total--
				pool.removeBucketIfEmptyLocked(bucket)
				pool.signalLocked()
			}
			stats = pool.statsLocked()
			pool.mu.Unlock()
			pool.observeStats(stats)
			if invalidated {
				_ = session.Close()
				pool.observeEvent(PoolEventClosed)
				if pool.closed {
					return nil, ErrClientPoolClosed
				}
				return nil, ErrConnectionInvalidated
			}
			pool.observeEvent(PoolEventOpened)
			return &clientLease{
				pool: pool, bucket: bucket,
				session: &pooledSession{client: session, lastUsed: pool.now()},
			}, nil
		}

		wait := pool.notify
		stats := pool.statsLocked()
		pool.mu.Unlock()
		closeSessions(toClose)
		pool.observeStats(stats)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-acquireCtx.Done():
			pool.observeEvent(PoolEventSaturated)
			return nil, ErrClientPoolSaturated
		case <-wait:
		}
	}
}

func (lease *clientLease) Client() remoteSession {
	if lease == nil || lease.session == nil {
		return nil
	}
	return lease.session.client
}

func (lease *clientLease) Release(reusable bool) {
	if lease == nil || lease.pool == nil || lease.session == nil || lease.bucket == nil {
		return
	}
	lease.once.Do(func() {
		pool := lease.pool
		pool.mu.Lock()
		if reusable && !pool.closed && !lease.bucket.invalidated {
			lease.session.lastUsed = pool.now()
			lease.bucket.idle = append(lease.bucket.idle, lease.session)
			pool.signalLocked()
			stats := pool.statsLocked()
			pool.mu.Unlock()
			pool.observeStats(stats)
			return
		}
		lease.bucket.total--
		pool.total--
		pool.removeBucketIfEmptyLocked(lease.bucket)
		pool.signalLocked()
		stats := pool.statsLocked()
		pool.mu.Unlock()
		_ = lease.session.client.Close()
		pool.observeEvent(PoolEventClosed)
		pool.observeStats(stats)
	})
}

func (pool *clientPool) Invalidate(request DiscoveryRequest) {
	if pool == nil {
		return
	}
	identity := clientPoolIdentity(request)
	pool.mu.Lock()
	now := pool.now()
	identities := []string{identity}
	if request.ConnectionID != "" {
		identities = identities[:0]
		for candidate, bucket := range pool.buckets {
			if bucket.connectionID == request.ConnectionID {
				identities = append(identities, candidate)
			}
		}
		if len(identities) == 0 {
			identities = append(identities, identity)
		}
	}
	var toClose []*pooledSession
	for _, candidate := range identities {
		pool.blocked[candidate] = now.Add(maxDuration(pool.config.IdleTimeout, 5*time.Minute))
		bucket := pool.buckets[candidate]
		if bucket == nil {
			continue
		}
		bucket.invalidated = true
		toClose = append(toClose, bucket.idle...)
		bucket.total -= len(bucket.idle)
		pool.total -= len(bucket.idle)
		bucket.idle = nil
		delete(pool.buckets, candidate)
	}
	pool.signalLocked()
	stats := pool.statsLocked()
	pool.mu.Unlock()
	closeSessions(toClose)
	pool.observeEvent(PoolEventInvalidated)
	if len(toClose) > 0 {
		pool.observeEvent(PoolEventClosed)
	}
	pool.observeStats(stats)
}

func (pool *clientPool) Prune() {
	if pool == nil {
		return
	}
	pool.mu.Lock()
	toClose := pool.pruneLocked(pool.now())
	stats := pool.statsLocked()
	pool.mu.Unlock()
	closeSessions(toClose)
	for range toClose {
		pool.observeEvent(PoolEventClosed)
	}
	pool.observeStats(stats)
}

func (pool *clientPool) Close() error {
	if pool == nil {
		return nil
	}
	pool.closeOnce.Do(func() {
		pool.mu.Lock()
		pool.closed = true
		var toClose []*pooledSession
		for identity, bucket := range pool.buckets {
			bucket.invalidated = true
			toClose = append(toClose, bucket.idle...)
			bucket.total -= len(bucket.idle)
			pool.total -= len(bucket.idle)
			bucket.idle = nil
			delete(pool.buckets, identity)
		}
		pool.signalLocked()
		stats := pool.statsLocked()
		pool.mu.Unlock()
		closeSessions(toClose)
		for range toClose {
			pool.observeEvent(PoolEventClosed)
		}
		pool.observeStats(stats)
	})
	return nil
}

func (pool *clientPool) pruneLocked(now time.Time) []*pooledSession {
	var expired []*pooledSession
	for identity, until := range pool.blocked {
		if !until.After(now) {
			delete(pool.blocked, identity)
		}
	}
	for identity, bucket := range pool.buckets {
		kept := bucket.idle[:0]
		for _, session := range bucket.idle {
			if now.Sub(session.lastUsed) >= pool.config.IdleTimeout {
				expired = append(expired, session)
				bucket.total--
				pool.total--
				continue
			}
			kept = append(kept, session)
		}
		bucket.idle = kept
		if bucket.total == 0 {
			delete(pool.buckets, identity)
		}
	}
	if len(expired) > 0 {
		pool.signalLocked()
	}
	return expired
}

func (pool *clientPool) removeBucketIfEmptyLocked(bucket *sessionBucket) {
	if bucket != nil && bucket.total == 0 && pool.buckets[bucket.identity] == bucket {
		delete(pool.buckets, bucket.identity)
	}
}

func (pool *clientPool) statsLocked() PoolStats {
	idle := 0
	for _, bucket := range pool.buckets {
		idle += len(bucket.idle)
	}
	inUse := pool.total - idle - pool.opening
	if inUse < 0 {
		inUse = 0
	}
	return PoolStats{Total: pool.total, Idle: idle, InUse: inUse, Opening: pool.opening}
}

func (pool *clientPool) signalLocked() {
	close(pool.notify)
	pool.notify = make(chan struct{})
}

func (pool *clientPool) observeEvent(event string) {
	if pool.observer != nil {
		pool.observer.RecordPoolEvent(event)
	}
}

func (pool *clientPool) observeStats(stats PoolStats) {
	if pool.observer != nil {
		pool.observer.SetPoolStats(stats)
	}
}

func clientPoolIdentity(request DiscoveryRequest) string {
	material := fmt.Sprintf(
		"external-mcp-session:v2\x00%s\x00%d\x00%s\x00%s\x00%s",
		request.ConnectionID,
		request.CredentialVersion,
		request.CredentialIdentity,
		request.Transport,
		request.Endpoint,
	)
	if request.ConnectionID == "" {
		material += "\x00" + request.BearerToken
	}
	digest := sha256.Sum256([]byte(material))
	return hex.EncodeToString(digest[:])
}

func closeSessions(sessions []*pooledSession) {
	for _, session := range sessions {
		if session != nil && session.client != nil {
			_ = session.client.Close()
		}
	}
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}
