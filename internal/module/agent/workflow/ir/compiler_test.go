package ir

import (
	"encoding/json"
	"strings"
	"testing"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

func TestCompileNormalizesLegacyVersionAndProducesStableOrder(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "second", Type: "tool"},
			{ID: "first", Type: "tool"},
			{ID: "join", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e3", Source: "first", Target: "join"},
			{ID: "e1", Source: "start", Target: "first"},
			{ID: "e4", Source: "second", Target: "join"},
			{ID: "e2", Source: "start", Target: "second"},
		},
	}

	plan, err := Compile(definition)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if plan.DSLVersion() != dsl.CurrentVersion || plan.WorkflowVersion() != 1 {
		t.Fatalf("unexpected normalized versions: %s/%d", plan.DSLVersion(), plan.WorkflowVersion())
	}
	got := strings.Join(plan.TopologicalOrder(), ",")
	if got != "start,second,first,join" {
		t.Fatalf("unexpected deterministic order %s", got)
	}
	for iteration := 0; iteration < 25; iteration++ {
		recompiled, compileErr := Compile(definition)
		if compileErr != nil {
			t.Fatalf("Compile() iteration %d error = %v", iteration, compileErr)
		}
		if current := strings.Join(recompiled.TopologicalOrder(), ","); current != got {
			t.Fatalf("topological order changed: first=%s current=%s", got, current)
		}
	}
}

func TestCompileRejectsUnknownVersionAndInvalidReference(t *testing.T) {
	_, err := Compile(&dsl.WorkflowDSL{
		DSLVersion: "2.0",
		Nodes:      []dsl.NodeDSL{{ID: "start", Type: "start"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported dsl_version") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}

	properties := json.RawMessage(`{"prompt":"{{start.user_input}}"}`)
	_, err = Compile(&dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "writer", Type: "llm", Properties: properties},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "without an upstream dependency path") {
		t.Fatalf("expected dependency path error, got %v", err)
	}
}

func TestCompileRejectsUnsafeWorkflowBudget(t *testing.T) {
	_, err := Compile(&dsl.WorkflowDSL{
		Budget: &dsl.BudgetDSL{MaxParallelNodes: 65},
		Nodes:  []dsl.NodeDSL{{ID: "start", Type: "start"}},
	})
	if err == nil || !strings.Contains(err.Error(), "max_parallel_nodes") {
		t.Fatalf("Compile() error = %v, want max_parallel_nodes validation", err)
	}

	_, err = Compile(&dsl.WorkflowDSL{
		Budget: &dsl.BudgetDSL{MaxEstimatedCostMicros: -1},
		Nodes:  []dsl.NodeDSL{{ID: "start", Type: "start"}},
	})
	if err == nil || !strings.Contains(err.Error(), "max_estimated_cost_micros") {
		t.Fatalf("Compile() error = %v, want cost validation", err)
	}
}

func TestCompileRequiresMatchingReducerForParallelWriteConflict(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "left", Type: "tool", Writes: []dsl.StateWriteDSL{{Path: "shared.summary"}}},
			{ID: "right", Type: "tool", Writes: []dsl.StateWriteDSL{{Path: "shared.summary"}}},
			{ID: "join", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "left"},
			{ID: "e2", Source: "start", Target: "right"},
			{ID: "e3", Source: "left", Target: "join"},
			{ID: "e4", Source: "right", Target: "join"},
		},
	}

	_, err := Compile(definition)
	if err == nil || !strings.Contains(err.Error(), "without a reducer") {
		t.Fatalf("expected parallel write conflict, got %v", err)
	}

	definition.Nodes[1].Writes[0].Reducer = "append"
	definition.Nodes[2].Writes[0].Reducer = "append"
	plan, err := Compile(definition)
	if err != nil {
		t.Fatalf("matching reducers should compile: %v", err)
	}
	left, _ := plan.Node("left")
	if left.Writes[0].Reducer != dsl.ReducerAppend {
		t.Fatalf("unexpected normalized reducer: %#v", left.Writes[0])
	}

	definition.Nodes[2].Writes[0].Reducer = "sum"
	if _, err = Compile(definition); err == nil || !strings.Contains(err.Error(), "must use one reducer") {
		t.Fatalf("mismatched reducers should fail: %v", err)
	}

	definition.Nodes[2].Writes[0].Reducer = "custom-script"
	if _, err = Compile(definition); err == nil || !strings.Contains(err.Error(), "unsupported reducer") {
		t.Fatalf("unknown reducer should fail: %v", err)
	}
}

func TestCompileRejectsDuplicateWritePathWithinNode(t *testing.T) {
	_, err := Compile(&dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "writer", Type: "tool", Writes: []dsl.StateWriteDSL{
				{Path: "shared.value", Source: "first"},
				{Path: "shared.value", Source: "second"},
			}},
		},
		Edges: []dsl.EdgeDSL{{ID: "e1", Source: "start", Target: "writer"}},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate state write path") {
		t.Fatalf("expected duplicate path error, got %v", err)
	}
}

func TestCompileDefaultsStateWriteSourceToPathField(t *testing.T) {
	plan, err := Compile(&dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "writer", Type: "tool", Writes: []dsl.StateWriteDSL{{Path: "shared.summary"}}},
		},
		Edges: []dsl.EdgeDSL{{ID: "e1", Source: "start", Target: "writer"}},
	})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	node, ok := plan.Node("writer")
	if !ok || len(node.Writes) != 1 || node.Writes[0].Source != "summary" {
		t.Fatalf("unexpected compiled state write: %#v", node.Writes)
	}
}

func TestPlanAccessorsReturnDefensiveCopies(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{{ID: "start", Type: "start", Properties: json.RawMessage(`{"value":"original"}`)}},
	}
	plan, err := Compile(definition)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	node, ok := plan.Node("start")
	if !ok {
		t.Fatal("compiled node not found")
	}
	node.Properties[0] = 'x'
	again, _ := plan.Node("start")
	if string(again.Properties) != `{"value":"original"}` {
		t.Fatalf("plan properties were mutated: %s", again.Properties)
	}
}
