package ir

import (
	"encoding/json"
	"strings"
	"testing"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

func TestCompileCompensationAllowsCurrentAndUpstreamReferences(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{
				ID: "reserve", Type: "tool", Properties: json.RawMessage(`{"tool_name":"Reserve"}`),
				Compensation: &dsl.CompensationDSL{
					ToolName: "Release", TimeoutSec: 7,
					Properties: json.RawMessage(`{"reservation_id":"{{reserve.id}}","requested_by":"{{start.user_input}}"}`),
					Retry:      &dsl.RetryPolicyDSL{MaxAttempts: 2, InitialBackoffMS: 10},
				},
			},
		},
		Edges: []dsl.EdgeDSL{{ID: "start-reserve", Source: "start", Target: "reserve"}},
	}

	plan, err := Compile(definition)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	node, ok := plan.Node("reserve")
	if !ok || node.Compensation == nil {
		t.Fatal("compiled compensation is missing")
	}
	if node.Compensation.ToolName != "Release" || node.Compensation.TimeoutSec != 7 || node.Compensation.Retry.MaxAttempts != 2 {
		t.Fatalf("unexpected compensation: %#v", node.Compensation)
	}

	definition.Nodes[1].Compensation.ToolName = "Mutated"
	node.Compensation.Properties[0] = 'x'
	again, _ := plan.Node("reserve")
	if again.Compensation.ToolName != "Release" || string(again.Compensation.Properties) != `{"reservation_id":"{{reserve.id}}","requested_by":"{{start.user_input}}"}` {
		t.Fatalf("compiled compensation was mutated: %#v", again.Compensation)
	}
}

func TestCompileRejectsInvalidCompensation(t *testing.T) {
	tests := []struct {
		name       string
		definition *dsl.WorkflowDSL
		want       string
	}{
		{
			name: "non tool node",
			definition: &dsl.WorkflowDSL{Nodes: []dsl.NodeDSL{{
				ID: "writer", Type: "llm", Compensation: &dsl.CompensationDSL{ToolName: "Undo"},
			}}},
			want: "only supported for tool nodes",
		},
		{
			name: "missing tool",
			definition: &dsl.WorkflowDSL{Nodes: []dsl.NodeDSL{{
				ID: "write", Type: "tool", Compensation: &dsl.CompensationDSL{},
			}}},
			want: "tool_name is invalid",
		},
		{
			name: "future reference",
			definition: &dsl.WorkflowDSL{
				Nodes: []dsl.NodeDSL{
					{ID: "write", Type: "tool", Compensation: &dsl.CompensationDSL{
						ToolName: "Undo", Properties: json.RawMessage(`{"value":"{{later.value}}"}`),
					}},
					{ID: "later", Type: "tool"},
				},
				Edges: []dsl.EdgeDSL{{ID: "write-later", Source: "write", Target: "later"}},
			},
			want: "without an upstream dependency path",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Compile(test.definition)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compile() error = %v, want substring %q", err, test.want)
			}
		})
	}
}
