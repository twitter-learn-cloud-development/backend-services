package tool

import (
	"errors"
	"sync"
	"time"
)

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type CircuitStateObserver interface {
	SetCircuitState(toolName string, state CircuitState)
}

type CircuitBreaker interface {
	Allow(toolName string) error
	RecordSuccess(toolName string)
	RecordFailure(toolName string)
}

type noopCircuitBreaker struct{}

func (noopCircuitBreaker) Allow(string) error   { return nil }
func (noopCircuitBreaker) RecordSuccess(string) {}
func (noopCircuitBreaker) RecordFailure(string) {}

type CircuitBreakerConfig struct {
	FailureThreshold int
	OpenTimeout      time.Duration
	Now              func() time.Time
	Observer         CircuitStateObserver
}

type circuitEntry struct {
	state            CircuitState
	failures         int
	openedAt         time.Time
	halfOpenInFlight bool
}

// ToolCircuitBreaker keeps one small state machine per registered tool. The
// catalog is bounded by deployment configuration, so its state does not grow
// with users, runs, or requests.
type ToolCircuitBreaker struct {
	mu               sync.Mutex
	entries          map[string]*circuitEntry
	failureThreshold int
	openTimeout      time.Duration
	now              func() time.Time
	observer         CircuitStateObserver
}

func NewToolCircuitBreaker(config CircuitBreakerConfig) *ToolCircuitBreaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 5
	}
	if config.OpenTimeout <= 0 {
		config.OpenTimeout = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &ToolCircuitBreaker{
		entries:          make(map[string]*circuitEntry),
		failureThreshold: config.FailureThreshold,
		openTimeout:      config.OpenTimeout,
		now:              config.Now,
		observer:         config.Observer,
	}
}

func (b *ToolCircuitBreaker) Allow(toolName string) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entry := b.entry(toolName)
	switch entry.state {
	case CircuitOpen:
		if b.now().Sub(entry.openedAt) < b.openTimeout {
			return ErrCircuitOpen
		}
		entry.state = CircuitHalfOpen
		entry.halfOpenInFlight = true
		b.observe(toolName, entry.state)
		return nil
	case CircuitHalfOpen:
		if entry.halfOpenInFlight {
			return ErrCircuitOpen
		}
		entry.halfOpenInFlight = true
	}
	return nil
}

func (b *ToolCircuitBreaker) RecordSuccess(toolName string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entry := b.entry(toolName)
	changed := entry.state != CircuitClosed || entry.failures != 0
	entry.state = CircuitClosed
	entry.failures = 0
	entry.openedAt = time.Time{}
	entry.halfOpenInFlight = false
	if changed {
		b.observe(toolName, entry.state)
	}
}

func (b *ToolCircuitBreaker) RecordFailure(toolName string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entry := b.entry(toolName)
	entry.failures++
	if entry.state == CircuitHalfOpen || entry.failures >= b.failureThreshold {
		entry.state = CircuitOpen
		entry.openedAt = b.now()
		entry.halfOpenInFlight = false
		b.observe(toolName, entry.state)
	}
}

func (b *ToolCircuitBreaker) entry(toolName string) *circuitEntry {
	entry := b.entries[toolName]
	if entry == nil {
		entry = &circuitEntry{state: CircuitClosed}
		b.entries[toolName] = entry
		b.observe(toolName, CircuitClosed)
	}
	return entry
}

func (b *ToolCircuitBreaker) observe(toolName string, state CircuitState) {
	if b.observer != nil {
		b.observer.SetCircuitState(toolName, state)
	}
}

func IsCircuitOpen(err error) bool {
	return errors.Is(err, ErrCircuitOpen)
}
