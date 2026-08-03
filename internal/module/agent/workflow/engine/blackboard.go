package engine

import (
	"fmt"
	"reflect"
	"sync"
	"time"
)

// StateView is the read-only state contract exposed to workflow nodes.
type StateView interface {
	GetSnapshot() map[string]map[string]interface{}
	GetValue(nodeID, field string) (interface{}, bool)
	Version() uint64
}

// StateEvent records one coordinator-applied state transition.
type StateEvent struct {
	Sequence  uint64                 `json:"sequence"`
	NodeID    string                 `json:"node_id"`
	Delta     map[string]interface{} `json:"delta"`
	AppliedAt int64                  `json:"applied_at"`
}

// StateCommit is an immutable persistence boundary emitted by the scheduler.
// Storage adapters may persist it without receiving a mutable Blackboard.
type StateCommit struct {
	Snapshot     map[string]map[string]interface{} `json:"snapshot"`
	StateVersion uint64                            `json:"state_version"`
	Events       []StateEvent                      `json:"events"`
}

// Blackboard stores immutable state generations. Only the scheduler
// coordinator should call ApplyDelta during local workflow execution.
type Blackboard struct {
	mu      sync.RWMutex
	state   map[string]map[string]interface{}
	version uint64
	events  []StateEvent
}

func NewBlackboard() *Blackboard {
	return &Blackboard{state: make(map[string]map[string]interface{})}
}

// LoadSnapshot replaces state for compatibility with legacy checkpoints.
func (b *Blackboard) LoadSnapshot(snapshot map[string]map[string]interface{}) {
	b.LoadSnapshotAtVersion(snapshot, 0)
}

// LoadSnapshotAtVersion restores a persisted immutable generation.
func (b *Blackboard) LoadSnapshotAtVersion(snapshot map[string]map[string]interface{}, version uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = cloneState(snapshot)
	b.version = version
	b.events = nil
}

// GetSnapshot returns a defensive deep copy of the current generation.
func (b *Blackboard) GetSnapshot() map[string]map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return cloneState(b.state)
}

// View captures one immutable generation for a node execution.
func (b *Blackboard) View() StateView {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return &stateView{state: b.state, version: b.version}
}

// ApplyDelta creates a new state generation and appends its transition event.
func (b *Blackboard) ApplyDelta(nodeID string, delta map[string]interface{}) StateEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	event := StateEvent{
		Sequence:  b.version + 1,
		NodeID:    nodeID,
		Delta:     cloneFields(delta),
		AppliedAt: time.Now().UnixMilli(),
	}
	b.applyEventLocked(event)
	return cloneEvent(event)
}

// Replay applies persisted events without changing their sequence or timestamp.
// A gap, duplicate or out-of-order event fails closed before mutating that event.
func (b *Blackboard) Replay(events []StateEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, event := range events {
		expected := b.version + 1
		if event.Sequence != expected {
			return fmt.Errorf("workflow state event sequence mismatch: expected=%d actual=%d", expected, event.Sequence)
		}
		if event.NodeID == "" {
			return fmt.Errorf("workflow state event %d has empty node_id", event.Sequence)
		}
		b.applyEventLocked(cloneEvent(event))
	}
	return nil
}

// Commit returns a defensive copy suitable for persistence callbacks.
func (b *Blackboard) Commit() StateCommit {
	b.mu.RLock()
	defer b.mu.RUnlock()

	events := make([]StateEvent, 0, len(b.events))
	for _, event := range b.events {
		events = append(events, cloneEvent(event))
	}
	return StateCommit{
		Snapshot:     cloneState(b.state),
		StateVersion: b.version,
		Events:       events,
	}
}

func (b *Blackboard) applyEventLocked(event StateEvent) {
	nextState := make(map[string]map[string]interface{}, len(b.state)+1)
	for existingNodeID, fields := range b.state {
		nextState[existingNodeID] = fields
	}
	nodeFields := cloneFields(b.state[event.NodeID])
	for key, value := range event.Delta {
		nodeFields[key] = cloneValue(value)
	}
	nextState[event.NodeID] = nodeFields

	b.version = event.Sequence
	b.state = nextState
	b.events = append(b.events, cloneEvent(event))
}

func (b *Blackboard) GetValue(nodeID, field string) (interface{}, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	fields, ok := b.state[nodeID]
	if !ok {
		return nil, false
	}
	value, ok := fields[field]
	if !ok {
		return nil, false
	}
	return cloneValue(value), true
}

func (b *Blackboard) Version() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.version
}

// EventsAfter returns defensive copies of append-only events after sequence.
func (b *Blackboard) EventsAfter(sequence uint64) []StateEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]StateEvent, 0, len(b.events))
	for _, event := range b.events {
		if event.Sequence > sequence {
			result = append(result, cloneEvent(event))
		}
	}
	return result
}

type stateView struct {
	state   map[string]map[string]interface{}
	version uint64
}

func (v *stateView) GetSnapshot() map[string]map[string]interface{} {
	if v == nil {
		return map[string]map[string]interface{}{}
	}
	return cloneState(v.state)
}

func (v *stateView) GetValue(nodeID, field string) (interface{}, bool) {
	if v == nil {
		return nil, false
	}
	fields, ok := v.state[nodeID]
	if !ok {
		return nil, false
	}
	value, ok := fields[field]
	if !ok {
		return nil, false
	}
	return cloneValue(value), true
}

func (v *stateView) Version() uint64 {
	if v == nil {
		return 0
	}
	return v.version
}

func cloneState(source map[string]map[string]interface{}) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{}, len(source))
	for nodeID, fields := range source {
		result[nodeID] = cloneFields(fields)
	}
	return result
}

func cloneFields(source map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(source))
	for key, value := range source {
		result[key] = cloneValue(value)
	}
	return result
}

func cloneEvent(source StateEvent) StateEvent {
	source.Delta = cloneFields(source.Delta)
	return source
}

func cloneValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	return cloneReflectValue(reflect.ValueOf(value)).Interface()
}

func cloneReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := cloneReflectValue(value.Elem())
		result := reflect.New(value.Type()).Elem()
		result.Set(cloned)
		return result
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(cloneReflectValue(value.Elem()))
		return result
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			result.SetMapIndex(iterator.Key(), cloneReflectValue(iterator.Value()))
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			result.Index(index).Set(cloneReflectValue(value.Index(index)))
		}
		return result
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			result.Index(index).Set(cloneReflectValue(value.Index(index)))
		}
		return result
	default:
		return value
	}
}
