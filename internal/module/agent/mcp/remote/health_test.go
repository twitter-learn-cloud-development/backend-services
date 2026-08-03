package remote

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	agentCredential "twitter-clone/internal/module/agent/credential"
)

type healthMemoryStore struct {
	*memoryStore
	mu sync.Mutex
}

func newHealthMemoryStore() *healthMemoryStore {
	return &healthMemoryStore{memoryStore: newMemoryStore()}
}

func (store *healthMemoryStore) ResetMCPConnectionHealth(
	_ context.Context,
	connectionID string,
	userID uint64,
	nextCheckAt time.Time,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	connection, ok := store.connections[connectionID]
	if !ok || connection.UserID != userID || connection.Status != ConnectionStatusActive {
		return ErrConnectionNotFound
	}
	connection.HealthStatus = HealthStatusUnknown
	connection.HealthErrorCode = ""
	connection.HealthFailureCount = 0
	connection.LastHealthCheckedAt = time.Time{}
	connection.LastHealthyAt = time.Time{}
	connection.NextHealthCheckAt = nextCheckAt
	connection.HealthLeaseOwner = ""
	connection.HealthLeaseUntil = time.Time{}
	store.connections[connectionID] = connection
	return nil
}

func (store *healthMemoryStore) ClaimMCPConnectionsForHealth(
	_ context.Context,
	owner string,
	now time.Time,
	leaseUntil time.Time,
	limit int,
) ([]*Connection, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	ids := make([]string, 0, len(store.connections))
	for id := range store.connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	claimed := make([]*Connection, 0, limit)
	for _, id := range ids {
		connection := store.connections[id]
		if connection.Status != ConnectionStatusActive || connection.NextHealthCheckAt.After(now) || connection.HealthLeaseUntil.After(now) {
			continue
		}
		connection.HealthLeaseOwner = owner
		connection.HealthLeaseUntil = leaseUntil
		store.connections[id] = connection
		cloned := cloneConnection(connection)
		claimed = append(claimed, &cloned)
		if len(claimed) >= limit {
			break
		}
	}
	return claimed, nil
}

func (store *healthMemoryStore) CompleteMCPConnectionHealth(
	_ context.Context,
	completion HealthCheckCompletion,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	connection, ok := store.connections[completion.ConnectionID]
	if !ok || connection.UserID != completion.UserID || connection.HealthLeaseOwner != completion.LeaseOwner {
		return ErrHealthLeaseLost
	}
	connection.HealthLeaseOwner = ""
	connection.HealthLeaseUntil = time.Time{}
	connection.NextHealthCheckAt = completion.NextHealthCheckAt
	switch completion.Outcome {
	case HealthOutcomeHealthy:
		connection.HealthStatus = HealthStatusHealthy
		connection.HealthErrorCode = ""
		connection.HealthFailureCount = 0
		connection.LastHealthCheckedAt = completion.CheckedAt
		connection.LastHealthyAt = completion.LastHealthyAt
	case HealthOutcomeFailed:
		connection.HealthStatus = completion.HealthStatus
		connection.HealthErrorCode = completion.ErrorCode
		connection.HealthFailureCount = completion.FailureCount
		connection.LastHealthCheckedAt = completion.CheckedAt
	case HealthOutcomeSkipped:
	default:
		return errors.New("invalid outcome")
	}
	store.connections[completion.ConnectionID] = connection
	return nil
}

func (store *healthMemoryStore) connection(id string) Connection {
	store.mu.Lock()
	defer store.mu.Unlock()
	return cloneConnection(store.connections[id])
}

func (store *healthMemoryStore) makeDue(id string, at time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	connection := store.connections[id]
	connection.NextHealthCheckAt = at
	store.connections[id] = connection
}

type healthProberStub struct {
	mu      sync.Mutex
	results []error
	calls   int
}

type decryptFailureCipher struct{}

func (decryptFailureCipher) Encrypt([]byte, []byte) (agentCredential.EncryptedSecret, error) {
	return agentCredential.EncryptedSecret{}, errors.New("encrypt unavailable")
}

func (decryptFailureCipher) Decrypt(agentCredential.EncryptedSecret, []byte) ([]byte, error) {
	return nil, errors.New("malformed encrypted credential")
}

func (prober *healthProberStub) Ping(context.Context, DiscoveryRequest) error {
	prober.mu.Lock()
	defer prober.mu.Unlock()
	prober.calls++
	if len(prober.results) == 0 {
		return nil
	}
	result := prober.results[0]
	prober.results = prober.results[1:]
	return result
}

func TestHealthCycleUsesLeaseAndTransitionsThroughDegradedState(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store := newHealthMemoryStore()
	store.connections["mcpconn_health"] = Connection{
		ID: "mcpconn_health", UserID: 7, Status: ConnectionStatusActive,
		Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
		AuthType: AuthNone, CredentialVersion: 1, HealthStatus: HealthStatusUnknown,
		NextHealthCheckAt: now,
	}
	prober := &healthProberStub{results: []error{
		nil,
		errors.New("temporary network error"),
		errors.New("temporary network error"),
		nil,
	}}
	manager := NewManager(
		store,
		nil,
		nil,
		nil,
		WithEnabled(true),
		WithHealthProber(prober),
		WithHealthOwner("instance-a"),
		WithHealthChecks(HealthCheckConfig{
			Enabled: true, PollInterval: time.Second, HealthyInterval: time.Minute,
			Timeout: time.Second, LeaseDuration: 2 * time.Second,
			FailureBackoffMin: time.Second, FailureBackoffMax: time.Minute,
			FailureThreshold: 2, BatchSize: 5, MaxConcurrentChecks: 1,
		}),
	)
	manager.now = func() time.Time { return now }

	manager.runHealthCycle(context.Background())
	connection := store.connection("mcpconn_health")
	if connection.HealthStatus != HealthStatusHealthy || connection.HealthFailureCount != 0 || connection.LastHealthyAt.IsZero() {
		t.Fatalf("healthy connection = %+v", connection)
	}

	now = now.Add(time.Minute)
	store.makeDue(connection.ID, now)
	manager.runHealthCycle(context.Background())
	connection = store.connection(connection.ID)
	if connection.HealthStatus != HealthStatusDegraded || connection.HealthFailureCount != 1 || connection.HealthErrorCode != "connection_failed" {
		t.Fatalf("degraded connection = %+v", connection)
	}

	now = now.Add(time.Minute)
	store.makeDue(connection.ID, now)
	manager.runHealthCycle(context.Background())
	connection = store.connection(connection.ID)
	if connection.HealthStatus != HealthStatusUnhealthy || connection.HealthFailureCount != 2 {
		t.Fatalf("unhealthy connection = %+v", connection)
	}

	now = now.Add(time.Minute)
	store.makeDue(connection.ID, now)
	manager.runHealthCycle(context.Background())
	connection = store.connection(connection.ID)
	if connection.HealthStatus != HealthStatusHealthy || connection.HealthFailureCount != 0 || connection.HealthErrorCode != "" {
		t.Fatalf("recovered connection = %+v", connection)
	}
}

func TestHealthClaimPreventsDuplicateReplicaProbe(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store := newHealthMemoryStore()
	store.connections["mcpconn_health"] = Connection{
		ID: "mcpconn_health", UserID: 7, Status: ConnectionStatusActive,
		NextHealthCheckAt: now,
	}
	first, err := store.ClaimMCPConnectionsForHealth(context.Background(), "instance-a", now, now.Add(time.Minute), 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim = %d, %v", len(first), err)
	}
	second, err := store.ClaimMCPConnectionsForHealth(context.Background(), "instance-b", now, now.Add(time.Minute), 10)
	if err != nil || len(second) != 0 {
		t.Fatalf("second claim = %d, %v", len(second), err)
	}
}

func TestHealthPoolSaturationDoesNotIncreaseFailureCount(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store := newHealthMemoryStore()
	store.connections["mcpconn_health"] = Connection{
		ID: "mcpconn_health", UserID: 7, Status: ConnectionStatusActive,
		Transport: TransportStreamableHTTP, AuthType: AuthNone, HealthStatus: HealthStatusDegraded,
		HealthFailureCount: 1, NextHealthCheckAt: now,
	}
	manager := NewManager(
		store, nil, nil, nil,
		WithEnabled(true),
		WithHealthProber(&healthProberStub{results: []error{ErrClientPoolSaturated}}),
		WithHealthOwner("instance-a"),
		WithHealthChecks(HealthCheckConfig{
			Enabled: true, PollInterval: time.Second, HealthyInterval: time.Minute,
			Timeout: time.Second, LeaseDuration: 2 * time.Second,
			FailureBackoffMin: time.Second, FailureBackoffMax: time.Minute,
			FailureThreshold: 2, BatchSize: 1, MaxConcurrentChecks: 1,
		}),
	)
	manager.now = func() time.Time { return now }
	manager.runHealthCycle(context.Background())
	connection := store.connection("mcpconn_health")
	if connection.HealthStatus != HealthStatusDegraded || connection.HealthFailureCount != 1 {
		t.Fatalf("pool-saturated health = %+v", connection)
	}
}

func TestHealthCredentialFailureUsesBoundedErrorCode(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store := newHealthMemoryStore()
	store.connections["mcpconn_credential"] = Connection{
		ID: "mcpconn_credential", UserID: 7, Status: ConnectionStatusActive,
		Transport: TransportStreamableHTTP, AuthType: AuthBearer, HasSecret: true,
		CredentialVersion: 1, HealthStatus: HealthStatusUnknown, NextHealthCheckAt: now,
	}
	prober := &healthProberStub{}
	manager := NewManager(
		store, decryptFailureCipher{}, nil, nil,
		WithEnabled(true),
		WithHealthProber(prober),
		WithHealthOwner("instance-a"),
		WithHealthChecks(HealthCheckConfig{
			Enabled: true, PollInterval: time.Second, HealthyInterval: time.Minute,
			Timeout: time.Second, LeaseDuration: 2 * time.Second,
			FailureBackoffMin: time.Second, FailureBackoffMax: time.Minute,
			FailureThreshold: 2, BatchSize: 1, MaxConcurrentChecks: 1,
		}),
	)
	manager.now = func() time.Time { return now }
	manager.runHealthCycle(context.Background())

	connection := store.connection("mcpconn_credential")
	if connection.HealthErrorCode != "credential_unavailable" || connection.HealthFailureCount != 1 {
		t.Fatalf("credential health = %+v", connection)
	}
	if prober.calls != 0 {
		t.Fatalf("health prober calls = %d, want 0", prober.calls)
	}
}
