package remote

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

type fakeRemoteSession struct {
	mu        sync.Mutex
	listCalls int
	toolCalls int
	pingCalls int
	closes    int
}

func (session *fakeRemoteSession) ListTools(
	context.Context,
	mcp.ListToolsRequest,
) (*mcp.ListToolsResult, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.listCalls++
	return &mcp.ListToolsResult{Tools: []mcp.Tool{mcp.NewTool("lookup")}}, nil
}

func (session *fakeRemoteSession) CallTool(
	context.Context,
	mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.toolCalls++
	return mcp.NewToolResultText("ok"), nil
}

func (session *fakeRemoteSession) Ping(context.Context) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.pingCalls++
	return nil
}

func (session *fakeRemoteSession) Close() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.closes++
	return nil
}

func (session *fakeRemoteSession) snapshot() (listCalls, toolCalls, pingCalls, closes int) {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.listCalls, session.toolCalls, session.pingCalls, session.closes
}

func TestSDKDiscovererReusesAndInvalidatesPooledSessions(t *testing.T) {
	var mu sync.Mutex
	var sessions []*fakeRemoteSession
	factory := func(context.Context, DiscoveryRequest) (remoteSession, error) {
		session := &fakeRemoteSession{}
		mu.Lock()
		sessions = append(sessions, session)
		mu.Unlock()
		return session, nil
	}
	discoverer := NewSDKDiscoverer(nil, time.Second,
		WithClientPool(ClientPoolConfig{
			Enabled: true, MaxSessions: 4, MaxSessionsPerConnection: 1,
			IdleTimeout: time.Minute, AcquireTimeout: time.Second,
		}),
		withRemoteSessionFactory(factory),
	)
	request := DiscoveryRequest{
		ConnectionID: "mcpconn_one", CredentialVersion: 1,
		Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
	}
	if _, err := discoverer.Discover(context.Background(), request); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if _, err := discoverer.Call(context.Background(), request, "lookup", map[string]interface{}{}); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if err := discoverer.Ping(context.Background(), request); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	mu.Lock()
	if len(sessions) != 1 {
		t.Fatalf("opened sessions = %d, want 1", len(sessions))
	}
	first := sessions[0]
	mu.Unlock()
	listCalls, toolCalls, pingCalls, closes := first.snapshot()
	if listCalls != 1 || toolCalls != 1 || pingCalls != 1 || closes != 0 {
		t.Fatalf("first session calls = list:%d tool:%d ping:%d close:%d", listCalls, toolCalls, pingCalls, closes)
	}

	discoverer.Invalidate(request)
	if _, err := discoverer.Call(context.Background(), request, "lookup", nil); !errors.Is(err, ErrConnectionInvalidated) {
		t.Fatalf("Call(old identity) error = %v", err)
	}
	_, _, _, closes = first.snapshot()
	if closes != 1 {
		t.Fatalf("invalidated session closes = %d, want 1", closes)
	}

	rotated := request
	rotated.CredentialVersion = 2
	if _, err := discoverer.Call(context.Background(), rotated, "lookup", nil); err != nil {
		t.Fatalf("Call(rotated identity) error = %v", err)
	}
	mu.Lock()
	opened := len(sessions)
	second := sessions[1]
	mu.Unlock()
	if opened != 2 {
		t.Fatalf("opened sessions after rotation = %d, want 2", opened)
	}
	if err := discoverer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	_, _, _, closes = second.snapshot()
	if closes != 1 {
		t.Fatalf("pooled session closes = %d, want 1", closes)
	}
}

func TestClientPoolInvalidatesEveryCredentialIdentityForConnection(t *testing.T) {
	var sessions []*fakeRemoteSession
	pool := newClientPool(ClientPoolConfig{
		Enabled: true, MaxSessions: 4, MaxSessionsPerConnection: 1,
		IdleTimeout: time.Minute, AcquireTimeout: time.Second,
	}, func(context.Context, DiscoveryRequest) (remoteSession, error) {
		session := &fakeRemoteSession{}
		sessions = append(sessions, session)
		return session, nil
	}, nil)
	first := DiscoveryRequest{
		ConnectionID: "mcpconn_rotating", CredentialVersion: 1, CredentialIdentity: "managed:v1",
		Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
	}
	second := first
	second.CredentialIdentity = "managed:v2"
	firstLease, err := pool.Acquire(context.Background(), first)
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	firstLease.Release(true)
	secondLease, err := pool.Acquire(context.Background(), second)
	if err != nil {
		t.Fatalf("Acquire(second) error = %v", err)
	}
	secondLease.Release(true)

	pool.Invalidate(DiscoveryRequest{ConnectionID: first.ConnectionID})
	if len(sessions) != 2 {
		t.Fatalf("opened sessions = %d, want 2", len(sessions))
	}
	for index, session := range sessions {
		_, _, _, closes := session.snapshot()
		if closes != 1 {
			t.Fatalf("session %d closes = %d, want 1", index, closes)
		}
	}
	if _, err := pool.Acquire(context.Background(), first); !errors.Is(err, ErrConnectionInvalidated) {
		t.Fatalf("Acquire(invalidated first identity) error = %v", err)
	}
	if _, err := pool.Acquire(context.Background(), second); !errors.Is(err, ErrConnectionInvalidated) {
		t.Fatalf("Acquire(invalidated second identity) error = %v", err)
	}
}

func TestClientPoolAppliesBackpressureAndPrunesIdleSessions(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	var session *fakeRemoteSession
	pool := newClientPool(ClientPoolConfig{
		Enabled: true, MaxSessions: 1, MaxSessionsPerConnection: 1,
		IdleTimeout: time.Minute, AcquireTimeout: 20 * time.Millisecond,
	}, func(context.Context, DiscoveryRequest) (remoteSession, error) {
		session = &fakeRemoteSession{}
		return session, nil
	}, nil)
	pool.now = func() time.Time { return now }
	request := DiscoveryRequest{
		ConnectionID: "mcpconn_capacity", CredentialVersion: 1,
		Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
	}
	lease, err := pool.Acquire(context.Background(), request)
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	if _, err := pool.Acquire(context.Background(), request); !errors.Is(err, ErrClientPoolSaturated) {
		t.Fatalf("Acquire(saturated) error = %v", err)
	}
	lease.Release(true)
	now = now.Add(2 * time.Minute)
	pool.Prune()
	_, _, _, closes := session.snapshot()
	if closes != 1 {
		t.Fatalf("idle session closes = %d, want 1", closes)
	}
	if stats := func() PoolStats {
		pool.mu.Lock()
		defer pool.mu.Unlock()
		return pool.statsLocked()
	}(); stats.Total != 0 || stats.Idle != 0 || stats.InUse != 0 {
		t.Fatalf("pool stats after prune = %+v", stats)
	}
}

func TestClientPoolRejectsCanceledAcquireBeforeReusingIdleSession(t *testing.T) {
	opened := 0
	pool := newClientPool(ClientPoolConfig{
		Enabled: true, MaxSessions: 1, MaxSessionsPerConnection: 1,
		IdleTimeout: time.Minute, AcquireTimeout: time.Second,
	}, func(context.Context, DiscoveryRequest) (remoteSession, error) {
		opened++
		return &fakeRemoteSession{}, nil
	}, nil)
	request := DiscoveryRequest{
		ConnectionID: "mcpconn_canceled", CredentialVersion: 1,
		Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
	}
	lease, err := pool.Acquire(context.Background(), request)
	if err != nil {
		t.Fatalf("Acquire(first) error = %v", err)
	}
	lease.Release(true)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	lease, err = pool.Acquire(canceledCtx, request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire(canceled) error = %v, want context.Canceled", err)
	}
	if lease != nil {
		t.Fatal("Acquire(canceled) returned a lease")
	}
	if opened != 1 {
		t.Fatalf("opened sessions = %d, want 1", opened)
	}
	pool.mu.Lock()
	stats := pool.statsLocked()
	pool.mu.Unlock()
	if stats.Total != 1 || stats.Idle != 1 || stats.InUse != 0 {
		t.Fatalf("pool stats after canceled acquire = %+v", stats)
	}
}
