package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

func TestCompensationPlanUsesReverseTopologyAndSuccessfulNodesOnly(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{
				ID: "reserve", Type: "tool",
				Compensation: &dsl.CompensationDSL{
					ToolName:   "ReleaseReservation",
					Properties: json.RawMessage(`{"reservation_id":"{{reserve.id}}","metadata":{"requested_by":"{{start.user_input}}"}}`),
				},
			},
			{
				ID: "charge", Type: "tool",
				Compensation: &dsl.CompensationDSL{
					ToolName: "RefundCharge", TimeoutSec: 9,
					Properties: json.RawMessage(`{"charge_id":"{{charge.id}}","items":["{{reserve.id}}",2]}`),
					Retry:      &dsl.RetryPolicyDSL{MaxAttempts: 3},
				},
			},
			{
				ID: "fail", Type: "tool",
				Compensation: &dsl.CompensationDSL{ToolName: "ShouldNotRun"},
			},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-reserve", Source: "start", Target: "reserve"},
			{ID: "reserve-charge", Source: "reserve", Target: "charge"},
			{ID: "charge-fail", Source: "charge", Target: "fail"},
		},
	}
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "reserve", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"id": "reservation-1"}, nil
		}},
		&mockNode{id: "charge", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"id": "charge-1"}, nil
		}},
		&mockNode{id: "fail", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			return nil, errors.New("downstream failure")
		}},
	}

	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Execute(context.Background(), map[string]interface{}{"user_input": "alice"}); err == nil {
		t.Fatal("Execute() error = nil, want downstream failure")
	}
	plan := scheduler.CompensationPlan()
	if len(plan) != 2 {
		t.Fatalf("len(CompensationPlan()) = %d, want 2: %#v", len(plan), plan)
	}
	if plan[0].Sequence != 1 || plan[0].SourceNodeID != "charge" || plan[0].ToolName != "RefundCharge" || plan[0].Inputs["charge_id"] != "charge-1" {
		t.Fatalf("unexpected first compensation: %#v", plan[0])
	}
	items, ok := plan[0].Inputs["items"].([]interface{})
	if !ok || len(items) != 2 || items[0] != "reservation-1" {
		t.Fatalf("nested compensation input was not resolved: %#v", plan[0].Inputs)
	}
	if plan[1].Sequence != 2 || plan[1].SourceNodeID != "reserve" || plan[1].Inputs["reservation_id"] != "reservation-1" {
		t.Fatalf("unexpected second compensation: %#v", plan[1])
	}
	metadata, ok := plan[1].Inputs["metadata"].(map[string]interface{})
	if !ok || metadata["requested_by"] != "alice" {
		t.Fatalf("nested upstream input was not resolved: %#v", plan[1].Inputs)
	}
	if plan[0].TimeoutSec != 9 || plan[0].Retry == nil || plan[0].Retry.MaxAttempts != 3 {
		t.Fatalf("execution metadata was not preserved: %#v", plan[0])
	}

	plan[0].Inputs["charge_id"] = "mutated"
	plan[0].Retry.MaxAttempts = 99
	again := scheduler.CompensationPlan()
	if again[0].Inputs["charge_id"] != "charge-1" || again[0].Retry.MaxAttempts != 3 {
		t.Fatalf("compensation plan leaked mutable state: %#v", again[0])
	}
}

func TestExecuteCompensationTaskUsesDeterministicRetryPolicy(t *testing.T) {
	task := CompensationTask{
		StepID: "write$compensate",
		Retry: &dsl.RetryPolicyDSL{
			MaxAttempts: 3, InitialBackoffMS: 1, MaxBackoffMS: 2, Multiplier: 2,
		},
	}
	calls := 0
	outputs, attempts, err := ExecuteCompensationTask(context.Background(), task, func(context.Context, int) (map[string]interface{}, error) {
		calls++
		if calls < 3 {
			return nil, retryableNodeError{message: "temporary compensation failure"}
		}
		return map[string]interface{}{"undone": true}, nil
	})
	if err != nil {
		t.Fatalf("ExecuteCompensationTask() error = %v", err)
	}
	if attempts != 3 || calls != 3 || outputs["undone"] != true {
		t.Fatalf("unexpected execution result: attempts=%d calls=%d outputs=%#v", attempts, calls, outputs)
	}
}
