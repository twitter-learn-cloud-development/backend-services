package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrConcurrencyLimitExceeded = errors.New("concurrency limit exceeded")

type AdmissionRequest struct {
	UserID     uint64
	WorkflowID string
}

type ReleaseFunc func()

type AdmissionController interface {
	Acquire(ctx context.Context, request AdmissionRequest) (context.Context, ReleaseFunc, error)
}

type ConcurrencyLimits struct {
	MaxPerUser     int
	MaxPerWorkflow int
}

// InMemoryConcurrencyLimiter is the single-process admission implementation.
// The interface intentionally permits a future Redis-backed implementation.
type InMemoryConcurrencyLimiter struct {
	mu        sync.Mutex
	limits    ConcurrencyLimits
	users     map[uint64]int
	workflows map[string]int
}

type admissionMarkerKey struct{}

type admissionMarker struct {
	controller *InMemoryConcurrencyLimiter
	userID     uint64
	workflowID string
}

func NewInMemoryConcurrencyLimiter(limits ConcurrencyLimits) *InMemoryConcurrencyLimiter {
	return &InMemoryConcurrencyLimiter{
		limits:    limits,
		users:     make(map[uint64]int),
		workflows: make(map[string]int),
	}
}

func (limiter *InMemoryConcurrencyLimiter) Acquire(
	ctx context.Context,
	request AdmissionRequest,
) (context.Context, ReleaseFunc, error) {
	if limiter == nil {
		return ctx, func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	if marker, ok := ctx.Value(admissionMarkerKey{}).(admissionMarker); ok &&
		marker.controller == limiter && marker.userID == request.UserID &&
		(request.WorkflowID == "" || marker.workflowID == "" || marker.workflowID == request.WorkflowID) {
		return ctx, func() {}, nil
	}

	limiter.mu.Lock()
	if request.UserID != 0 && limiter.limits.MaxPerUser > 0 &&
		limiter.users[request.UserID] >= limiter.limits.MaxPerUser {
		limiter.mu.Unlock()
		return ctx, nil, fmt.Errorf("%w: user run quota", ErrConcurrencyLimitExceeded)
	}
	if request.WorkflowID != "" && limiter.limits.MaxPerWorkflow > 0 &&
		limiter.workflows[request.WorkflowID] >= limiter.limits.MaxPerWorkflow {
		limiter.mu.Unlock()
		return ctx, nil, fmt.Errorf("%w: workflow run quota", ErrConcurrencyLimitExceeded)
	}
	if request.UserID != 0 {
		limiter.users[request.UserID]++
	}
	if request.WorkflowID != "" {
		limiter.workflows[request.WorkflowID]++
	}
	limiter.mu.Unlock()

	admitted := context.WithValue(ctx, admissionMarkerKey{}, admissionMarker{
		controller: limiter,
		userID:     request.UserID,
		workflowID: request.WorkflowID,
	})
	var once sync.Once
	release := func() {
		once.Do(func() {
			limiter.mu.Lock()
			defer limiter.mu.Unlock()
			decrementAdmission(limiter.users, request.UserID)
			decrementAdmission(limiter.workflows, request.WorkflowID)
		})
	}
	return admitted, release, nil
}

func decrementAdmission[K comparable](counts map[K]int, key K) {
	if counts[key] <= 1 {
		delete(counts, key)
		return
	}
	counts[key]--
}
