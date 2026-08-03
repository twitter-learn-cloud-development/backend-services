package observability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const defaultInMemoryEventLimit = 2000

type InMemoryEventStore struct {
	mu      sync.Mutex
	streams map[string][]TraceEvent
	next    map[string]uint64
	notify  map[string]chan struct{}
	limit   int
	now     func() time.Time
}

func NewInMemoryEventStore(limit int) *InMemoryEventStore {
	if limit <= 0 {
		limit = defaultInMemoryEventLimit
	}
	return &InMemoryEventStore{
		streams: make(map[string][]TraceEvent), next: make(map[string]uint64),
		notify: make(map[string]chan struct{}), limit: limit, now: time.Now,
	}
}

func (store *InMemoryEventStore) RecordRun(_ context.Context, record RunRecord) error {
	copy := record
	return store.append(TraceEvent{Kind: TraceEventRun, Run: &copy}, record.UserID, record.RunID)
}

func (store *InMemoryEventStore) RecordStep(_ context.Context, record StepRecord) error {
	copy := record
	return store.append(TraceEvent{Kind: TraceEventStep, Step: &copy}, record.UserID, record.RunID)
}

func (store *InMemoryEventStore) RecordLLMCall(_ context.Context, record LLMCallRecord) error {
	copy := record
	return store.append(TraceEvent{Kind: TraceEventLLMCall, LLMCall: &copy}, record.UserID, record.RunID)
}

func (store *InMemoryEventStore) RecordToolCall(_ context.Context, record ToolCallRecord) error {
	copy := record
	return store.append(TraceEvent{Kind: TraceEventToolCall, ToolCall: &copy}, record.UserID, record.RunID)
}

func (store *InMemoryEventStore) append(event TraceEvent, userID uint64, runID string) error {
	if store == nil || userID == 0 || strings.TrimSpace(runID) == "" {
		return errors.New("trace event identity is incomplete")
	}
	key := traceEventMemoryKey(userID, runID)
	store.mu.Lock()
	defer store.mu.Unlock()
	store.next[key]++
	event.Cursor = fmt.Sprintf("%d-0", store.next[key])
	event.CreatedAt = store.now()
	events := append(store.streams[key], event)
	if len(events) > store.limit {
		events = append([]TraceEvent(nil), events[len(events)-store.limit:]...)
	}
	store.streams[key] = events
	if channel := store.notify[key]; channel != nil {
		close(channel)
		delete(store.notify, key)
	}
	return nil
}

func (store *InMemoryEventStore) ReadTraceEvents(
	ctx context.Context,
	userID uint64,
	runID string,
	afterCursor string,
	limit int,
	block time.Duration,
) (TraceEventBatch, error) {
	if store == nil || userID == 0 || strings.TrimSpace(runID) == "" {
		return TraceEventBatch{}, errors.New("trace event identity is incomplete")
	}
	cursor, err := NormalizeTraceEventCursor(afterCursor)
	if err != nil {
		return TraceEventBatch{}, err
	}
	limit = ClampTraceEventBatchSize(limit)
	key := traceEventMemoryKey(userID, runID)

	store.mu.Lock()
	batch := readMemoryTraceEvents(store.streams[key], cursor, limit)
	if len(batch.Events) > 0 || block <= 0 {
		store.mu.Unlock()
		return batch, nil
	}
	notify := store.notify[key]
	if notify == nil {
		notify = make(chan struct{})
		store.notify[key] = notify
	}
	store.mu.Unlock()

	timer := time.NewTimer(block)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return TraceEventBatch{}, ctx.Err()
	case <-timer.C:
		return TraceEventBatch{Events: []TraceEvent{}}, nil
	case <-notify:
		store.mu.Lock()
		batch = readMemoryTraceEvents(store.streams[key], cursor, limit)
		store.mu.Unlock()
		return batch, nil
	}
}

func readMemoryTraceEvents(events []TraceEvent, cursor string, limit int) TraceEventBatch {
	batch := TraceEventBatch{Events: []TraceEvent{}}
	if len(events) == 0 {
		if cursor != "0-0" {
			batch.Reset = true
		}
		return batch
	}
	if cursor != "0-0" {
		comparison, _ := CompareTraceEventCursor(cursor, events[0].Cursor)
		batch.Reset = comparison < 0
		comparison, _ = CompareTraceEventCursor(cursor, events[len(events)-1].Cursor)
		if comparison > 0 {
			batch.Reset = true
			cursor = "0-0"
		}
	}
	for _, event := range events {
		comparison, _ := CompareTraceEventCursor(event.Cursor, cursor)
		if comparison <= 0 {
			continue
		}
		batch.Events = append(batch.Events, event)
		if len(batch.Events) == limit {
			break
		}
	}
	return batch
}

func traceEventMemoryKey(userID uint64, runID string) string {
	return fmt.Sprintf("%d:%s", userID, strings.TrimSpace(runID))
}
