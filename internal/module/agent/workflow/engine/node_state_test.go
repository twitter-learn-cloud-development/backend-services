package engine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

type retryableNodeError struct {
	message string
}

func (e retryableNodeError) Error() string     { return e.message }
func (e retryableNodeError) IsRetryable() bool { return true }

func TestValidateNodeStateTransitionRejectsTerminalRestart(t *testing.T) {
	if err := validateNodeStateTransition(NodeStatusSuccess, NodeStatusRunning); err == nil {
		t.Fatal("expected terminal success state to reject restart")
	}
	if err := validateNodeStateTransition(NodeStatusRetrying, NodeStatusRunning); err != nil {
		t.Fatalf("expected retrying -> running transition, got %v", err)
	}
	if err := validateNodeStateTransition(NodeStatusSuspended, NodeStatusSuccess); err != nil {
		t.Fatalf("expected suspended -> success transition, got %v", err)
	}
}

func TestSchedulerRetriesRetryableNodeAndRecordsAttempts(t *testing.T) {
	definition := retryWorkflowDefinition(&dsl.RetryPolicyDSL{
		MaxAttempts:      3,
		InitialBackoffMS: 1,
		MaxBackoffMS:     2,
		Multiplier:       2,
		Jitter:           0.2,
	})
	var calls atomic.Int32
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "work", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			if calls.Add(1) < 3 {
				return nil, retryableNodeError{message: "temporary"}
			}
			return map[string]interface{}{"value": "done"}, nil
		}},
	}
	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Execute(context.Background(), nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected three attempts, got %d", got)
	}
	trace := traceByNodeID(scheduler.GetTraces(), "work")
	if trace.Status != NodeStatusSuccess || trace.Attempt != 3 || trace.MaxAttempts != 3 {
		t.Fatalf("unexpected retry trace: %#v", trace)
	}
}

func TestSchedulerDoesNotRetryOrdinaryBusinessError(t *testing.T) {
	definition := retryWorkflowDefinition(&dsl.RetryPolicyDSL{MaxAttempts: 3, InitialBackoffMS: 1})
	var calls atomic.Int32
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "work", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			calls.Add(1)
			return nil, errors.New("invalid business input")
		}},
	}
	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	err = scheduler.Execute(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "invalid business input") {
		t.Fatalf("expected business error, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("ordinary errors must not be retried, got %d calls", got)
	}
	trace := traceByNodeID(scheduler.GetTraces(), "work")
	if trace.Status != NodeStatusFailed || trace.Attempt != 1 || trace.MaxAttempts != 3 {
		t.Fatalf("unexpected failure trace: %#v", trace)
	}
}

func TestSchedulerDoesNotRetrySuspension(t *testing.T) {
	definition := retryWorkflowDefinition(&dsl.RetryPolicyDSL{MaxAttempts: 3, InitialBackoffMS: 1})
	var calls atomic.Int32
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "work", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			calls.Add(1)
			return nil, NewSuspensionError("work", "approval required", "", nil)
		}},
	}
	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	err = scheduler.Execute(context.Background(), nil)
	var suspension *SuspensionError
	if !errors.As(err, &suspension) {
		t.Fatalf("expected suspension, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("suspension must not be retried, got %d calls", got)
	}
	trace := traceByNodeID(scheduler.GetTraces(), "work")
	if trace.Status != NodeStatusSuspended || trace.Attempt != 1 {
		t.Fatalf("unexpected suspension trace: %#v", trace)
	}
}

func TestSchedulerCancellationInterruptsRetryBackoff(t *testing.T) {
	definition := retryWorkflowDefinition(&dsl.RetryPolicyDSL{
		MaxAttempts:      3,
		InitialBackoffMS: 500,
		MaxBackoffMS:     500,
		Multiplier:       1,
	})
	firstAttempt := make(chan struct{})
	var calls atomic.Int32
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "work", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			if calls.Add(1) == 1 {
				close(firstAttempt)
			}
			return nil, retryableNodeError{message: "temporary"}
		}},
	}
	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Execute(ctx, nil) }()
	<-firstAttempt
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("backoff cancellation should prevent another attempt, got %d calls", got)
	}
	trace := traceByNodeID(scheduler.GetTraces(), "work")
	if trace.Status != NodeStatusCanceled || trace.Attempt != 1 {
		t.Fatalf("unexpected cancellation trace: %#v", trace)
	}
}

func TestSchedulerDeadlineDuringRetryBackoffIsTimedOut(t *testing.T) {
	definition := retryWorkflowDefinition(&dsl.RetryPolicyDSL{
		MaxAttempts:      3,
		InitialBackoffMS: 500,
		MaxBackoffMS:     500,
		Multiplier:       1,
	})
	var calls atomic.Int32
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "work", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			calls.Add(1)
			return nil, retryableNodeError{message: "temporary"}
		}},
	}
	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := scheduler.Execute(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("deadline during backoff should prevent another attempt, got %d calls", got)
	}
	trace := traceByNodeID(scheduler.GetTraces(), "work")
	if trace.Status != NodeStatusTimedOut || trace.Attempt != 1 {
		t.Fatalf("unexpected timeout trace: %#v", trace)
	}
}

func TestNodeRetryDelayIsDeterministicAndBounded(t *testing.T) {
	policy := nodeRetryPolicy{
		maxAttempts: 3,
		initial:     100 * time.Millisecond,
		maximum:     150 * time.Millisecond,
		multiplier:  2,
		jitter:      0.25,
	}
	first := nodeRetryDelay(policy, "node-a", 2)
	second := nodeRetryDelay(policy, "node-a", 2)
	if first != second {
		t.Fatalf("retry jitter must be deterministic: %s != %s", first, second)
	}
	if first < 112500*time.Microsecond || first > 187500*time.Microsecond {
		t.Fatalf("retry delay %s is outside capped jitter range", first)
	}
}

func retryWorkflowDefinition(retry *dsl.RetryPolicyDSL) *dsl.WorkflowDSL {
	return &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "work", Type: "tool", Retry: retry},
		},
		Edges: []dsl.EdgeDSL{{ID: "e1", Source: "start", Target: "work"}},
	}
}

func traceByNodeID(traces []NodeTrace, nodeID string) NodeTrace {
	for _, trace := range traces {
		if trace.NodeID == nodeID {
			return trace
		}
	}
	return NodeTrace{}
}
