package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
)

type budgetTestNode struct {
	id      string
	execute func(context.Context) error
}

func (n *budgetTestNode) ID() string   { return n.id }
func (n *budgetTestNode) Type() string { return "tool" }
func (n *budgetTestNode) Execute(ctx context.Context, _ StateView, _ map[string]interface{}) (map[string]interface{}, error) {
	if n.execute != nil {
		if err := n.execute(ctx); err != nil {
			return nil, err
		}
	}
	return map[string]interface{}{"ok": true}, nil
}

func TestSchedulerEnforcesNodeExecutionBudget(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{{ID: "start", Type: "start"}, {ID: "work", Type: "tool"}},
		Edges: []dsl.EdgeDSL{{ID: "edge", Source: "start", Target: "work"}},
	}
	tracker, err := agentRuntime.NewBudgetTracker(agentRuntime.Budget{MaxSteps: 1})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}
	scheduler, err := NewScheduler(definition, []WorkflowNode{
		&budgetTestNode{id: "start"}, &budgetTestNode{id: "work"},
	}, WithExecutionBudget(tracker, 1))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Execute(context.Background(), nil); !agentRuntime.HasErrorCode(err, agentRuntime.ErrorBudgetExceeded) {
		t.Fatalf("Execute() error = %v, want budget_exceeded", err)
	}
	if snapshot := tracker.Snapshot(); snapshot.NodeExecutions != 1 {
		t.Fatalf("node executions = %d, want 1", snapshot.NodeExecutions)
	}
}

func TestSchedulerLimitsParallelNodeHandlers(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "one", Type: "tool"}, {ID: "two", Type: "tool"}, {ID: "three", Type: "tool"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "one"},
			{ID: "e2", Source: "start", Target: "two"},
			{ID: "e3", Source: "start", Target: "three"},
		},
	}
	tracker, _ := agentRuntime.NewBudgetTracker(agentRuntime.Budget{MaxSteps: 10})
	var inFlight atomic.Int32
	var maximum atomic.Int32
	handler := func(context.Context) error {
		current := inFlight.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	}
	scheduler, err := NewScheduler(definition, []WorkflowNode{
		&budgetTestNode{id: "start"},
		&budgetTestNode{id: "one", execute: handler},
		&budgetTestNode{id: "two", execute: handler},
		&budgetTestNode{id: "three", execute: handler},
	}, WithExecutionBudget(tracker, 2))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Execute(context.Background(), nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum in-flight handlers = %d, want 2", maximum.Load())
	}
}

func TestSchedulerPersistsBudgetInSuspensionCheckpoint(t *testing.T) {
	definition := &dsl.WorkflowDSL{Nodes: []dsl.NodeDSL{{ID: "wait", Type: "wait"}}}
	tracker, _ := agentRuntime.NewBudgetTracker(agentRuntime.Budget{MaxSteps: 5})
	scheduler, err := NewScheduler(definition, []WorkflowNode{&budgetTestNode{
		id: "wait",
		execute: func(context.Context) error {
			return NewSuspensionError("wait", "approval", "", nil)
		},
	}}, WithExecutionBudget(tracker, 1))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	err = scheduler.Execute(context.Background(), nil)
	var suspension *SuspensionError
	if !errors.As(err, &suspension) {
		t.Fatalf("Execute() error = %v, want suspension", err)
	}
	checkpoint := scheduler.GetCheckpoint(suspension)
	if checkpoint.Budget.NodeExecutions != 1 {
		t.Fatalf("checkpoint node executions = %d, want 1", checkpoint.Budget.NodeExecutions)
	}
}
