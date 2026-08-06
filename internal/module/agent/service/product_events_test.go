package service

import (
	"context"
	"sync"
	"testing"
	"time"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentProduct "twitter-clone/internal/module/agent/product"
)

type memoryProductEventStore struct {
	mu     sync.Mutex
	events map[string]agentProduct.Event
}

func (store *memoryProductEventStore) RecordProductEvent(
	_ context.Context,
	event *agentProduct.Event,
) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.events == nil {
		store.events = make(map[string]agentProduct.Event)
	}
	if existing, ok := store.events[event.ID]; ok {
		if !agentProduct.SameFact(&existing, event) {
			return false, agentProduct.ErrEventConflict
		}
		return false, nil
	}
	copyEvent := *event
	store.events[event.ID] = copyEvent
	return true, nil
}

func (store *memoryProductEventStore) CountProductEvents(
	_ context.Context,
	userID uint64,
	subjectType string,
	subjectID string,
	kind string,
	limit int64,
) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var count int64
	for _, event := range store.events {
		if event.UserID != userID || event.SubjectType != subjectType ||
			event.SubjectID != subjectID || event.Kind != kind {
			continue
		}
		count++
		if count >= limit {
			break
		}
	}
	return count, nil
}

func (store *memoryProductEventStore) count(kind string) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	count := 0
	for _, event := range store.events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

type externalMCPProductObserverFake struct {
	mu     sync.Mutex
	events map[string]int
}

func (observer *externalMCPProductObserverFake) RecordProductEvent(_, _, event string) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if observer.events == nil {
		observer.events = make(map[string]int)
	}
	observer.events[event]++
}

func (observer *externalMCPProductObserverFake) count(event string) int {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return observer.events[event]
}

func TestExternalMCPProductEventsAreIdempotentAcrossRuns(t *testing.T) {
	store := &memoryProductEventStore{}
	observer := &externalMCPProductObserverFake{}
	service := &AgentService{productEventStore: store, externalMCPProductObserver: observer}
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	connection := &externalmcp.Connection{
		ID: "mcpconn_1", UserID: 42, Scope: externalmcp.ScopeUser,
		Transport: externalmcp.TransportStreamableHTTP, CreatedAt: now,
	}

	service.recordExternalMCPConnectionFacts(context.Background(), connection)
	service.recordExternalMCPConnectionFacts(context.Background(), connection)
	if store.count(agentProduct.EventConnectorConfigured) != 1 || observer.count("configured") != 1 {
		t.Fatalf("configured events persisted=%d observed=%d, want 1/1",
			store.count(agentProduct.EventConnectorConfigured), observer.count("configured"))
	}
	if store.count(agentProduct.EventConnectorActivated) != 0 {
		t.Fatal("connection was activated before an enabled tool existed")
	}

	connection.FirstActivatedAt = now.Add(time.Minute)
	service.recordExternalMCPConnectionFacts(context.Background(), connection)
	service.recordExternalMCPConnectionFacts(context.Background(), connection)
	if store.count(agentProduct.EventConnectorActivated) != 1 || observer.count("activated") != 1 {
		t.Fatalf("activated events persisted=%d observed=%d, want 1/1",
			store.count(agentProduct.EventConnectorActivated), observer.count("activated"))
	}

	definition := externalmcp.ExecutableTool{
		ConnectionID: connection.ID, ConnectionOwnerID: connection.UserID,
		ConnectionScope: connection.Scope, Transport: connection.Transport,
		ConnectionCreatedAt: connection.CreatedAt, ConnectionActivatedAt: connection.FirstActivatedAt,
	}
	service.recordExternalMCPUse(context.Background(), definition, "run-1")
	service.recordExternalMCPUse(context.Background(), definition, "run-1")
	service.recordExternalMCPUse(context.Background(), definition, "run-2")

	if store.count(agentProduct.EventConnectorUsed) != 2 ||
		store.count(agentProduct.EventConnectorFirstUsed) != 1 ||
		store.count(agentProduct.EventConnectorReused) != 1 {
		t.Fatalf("connector use facts: used=%d first=%d reused=%d, want 2/1/1",
			store.count(agentProduct.EventConnectorUsed),
			store.count(agentProduct.EventConnectorFirstUsed),
			store.count(agentProduct.EventConnectorReused))
	}
	if observer.count("first_used") != 1 || observer.count("reused") != 1 {
		t.Fatalf("connector use metrics first=%d reused=%d, want 1/1",
			observer.count("first_used"), observer.count("reused"))
	}
}

func TestExternalMCPProductEventsRepairReuseAfterPartialWrite(t *testing.T) {
	store := &memoryProductEventStore{}
	observer := &externalMCPProductObserverFake{}
	service := &AgentService{productEventStore: store, externalMCPProductObserver: observer}
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	definition := externalmcp.ExecutableTool{
		ConnectionID: "mcpconn_1", ConnectionOwnerID: 42,
		ConnectionScope: externalmcp.ScopeUser, Transport: externalmcp.TransportStreamableHTTP,
		ConnectionCreatedAt: now, ConnectionActivatedAt: now.Add(time.Minute),
	}

	service.recordExternalMCPUse(context.Background(), definition, "run-1")
	secondUse, err := agentProduct.NewEvent(
		agentProduct.EventConnectorUsed,
		definition.ConnectionOwnerID,
		agentProduct.SubjectExternalMCPConnection,
		definition.ConnectionID,
		"run-2",
		"",
		externalMCPProductDimensions(definition.ConnectionScope, definition.Transport),
		now.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if created, recordErr := store.RecordProductEvent(context.Background(), secondUse); recordErr != nil || !created {
		t.Fatalf("seed second use created=%v error=%v", created, recordErr)
	}

	service.recordExternalMCPUse(context.Background(), definition, "run-2")

	if store.count(agentProduct.EventConnectorReused) != 1 || observer.count("reused") != 1 {
		t.Fatalf("repaired reuse persisted=%d observed=%d, want 1/1",
			store.count(agentProduct.EventConnectorReused), observer.count("reused"))
	}
}
