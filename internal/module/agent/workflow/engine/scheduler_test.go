package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/workflow/dsl"
)

// mockNode 模拟测试节点
type mockNode struct {
	id       string
	nodeType string
	execFunc func(inputs map[string]interface{}) (map[string]interface{}, error)
}

func (m *mockNode) ID() string {
	return m.id
}

func (m *mockNode) Type() string {
	return m.nodeType
}

func (m *mockNode) Execute(ctx context.Context, state StateView, inputs map[string]interface{}) (map[string]interface{}, error) {
	if m.execFunc != nil {
		return m.execFunc(inputs)
	}
	return nil, nil
}

// TestScheduler_DAG_Execution 测试正常的 DAG 拓扑排序并发执行
func TestScheduler_DAG_Execution(t *testing.T) {
	// 构建一个简单的 DSL
	// start -> node_1 -> node_2
	dslObj := &dsl.WorkflowDSL{
		ID:   1,
		Name: "TestFlow",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "node_1", Type: "llm", Properties: json.RawMessage(`{"prompt": "Hello {{start.user_input}}"}`)},
			{ID: "node_2", Type: "tool", Properties: json.RawMessage(`{"input_param": "{{node_1.text}}"}`)},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "node_1", SourceHandle: "output", TargetHandle: "input"},
			{ID: "e2", Source: "node_1", Target: "node_2", SourceHandle: "output", TargetHandle: "input"},
		},
	}

	node1Executed := false
	node2Executed := false

	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{
			id:       "node_1",
			nodeType: "llm",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				node1Executed = true
				prompt := inputs["prompt"].(string)
				if prompt != "Hello Alice" {
					return nil, errors.New("invalid parameter replacement")
				}
				return map[string]interface{}{"text": "Greetings"}, nil
			},
		},
		&mockNode{
			id:       "node_2",
			nodeType: "tool",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				node2Executed = true
				param := inputs["input_param"].(string)
				if param != "Greetings" {
					return nil, errors.New("invalid data pipeline transmission")
				}
				return map[string]interface{}{"status": "done"}, nil
			},
		},
	}

	scheduler, err := NewScheduler(dslObj, nodes)
	if err != nil {
		t.Fatalf("failed to compile scheduler: %v", err)
	}

	initialInputs := map[string]interface{}{
		"user_input": "Alice",
	}

	ctx := context.Background()
	err = scheduler.Execute(ctx, initialInputs)
	if err != nil {
		t.Fatalf("workflow execution failed: %v", err)
	}

	if !node1Executed || !node2Executed {
		t.Error("some nodes were skipped incorrectly")
	}

	// 检验 Blackboard 中存储的结果
	val, exists := scheduler.GetBlackboard().GetValue("node_2", "status")
	if !exists || val.(string) != "done" {
		t.Error("final blackboard value check failed")
	}
}

// TestScheduler_CycleDetection 测试 Kahn 算法在编译期检查出循环依赖图
func TestScheduler_CycleDetection(t *testing.T) {
	// 构建一个成环的 DSL
	// start -> node_1 -> node_2 -> node_1
	dslObj := &dsl.WorkflowDSL{
		ID:   2,
		Name: "CycleFlow",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "node_1", Type: "llm"},
			{ID: "node_2", Type: "tool"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "node_1"},
			{ID: "e2", Source: "node_1", Target: "node_2"},
			{ID: "e3", Source: "node_2", Target: "node_1"}, // 成环边
		},
	}

	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "node_1", nodeType: "llm"},
		&mockNode{id: "node_2", nodeType: "tool"},
	}

	_, err := NewScheduler(dslObj, nodes)
	if err == nil {
		t.Error("expected error due to cycle detection, but got nil")
	}
}

func TestScheduler_RejectsUnknownEdgeEndpoint(t *testing.T) {
	dslObj := &dsl.WorkflowDSL{
		ID:   3,
		Name: "InvalidEndpointFlow",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "missing_node"},
		},
	}

	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
	}

	_, err := NewScheduler(dslObj, nodes)
	if err == nil {
		t.Fatal("expected unknown endpoint error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown target node") {
		t.Fatalf("expected unknown target node error, got %v", err)
	}
}

func TestScheduler_RouterBranchDoesNotSkipJoinNode(t *testing.T) {
	dslObj := &dsl.WorkflowDSL{
		ID:   4,
		Name: "RouterJoinFlow",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "router", Type: "router"},
			{ID: "true_node", Type: "tool"},
			{ID: "false_node", Type: "tool"},
			{ID: "join", Type: "tool", Properties: json.RawMessage(`{"input": "{{true_node.value}}"}`)},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "router"},
			{ID: "e2", Source: "router", Target: "true_node", SourceHandle: "true"},
			{ID: "e3", Source: "router", Target: "false_node", SourceHandle: "false"},
			{ID: "e4", Source: "true_node", Target: "join"},
			{ID: "e5", Source: "false_node", Target: "join"},
		},
	}

	falseNodeExecuted := false
	joinExecuted := false
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{
			id:       "router",
			nodeType: "router",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{"_branch": "true"}, nil
			},
		},
		&mockNode{
			id:       "true_node",
			nodeType: "tool",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{"value": "active"}, nil
			},
		},
		&mockNode{
			id:       "false_node",
			nodeType: "tool",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				falseNodeExecuted = true
				return map[string]interface{}{"value": "inactive"}, nil
			},
		},
		&mockNode{
			id:       "join",
			nodeType: "tool",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				joinExecuted = true
				if inputs["input"] != "active" {
					return nil, errors.New("join did not receive active branch output")
				}
				return map[string]interface{}{"status": "joined"}, nil
			},
		},
	}

	scheduler, err := NewScheduler(dslObj, nodes)
	if err != nil {
		t.Fatalf("failed to compile scheduler: %v", err)
	}
	if err := scheduler.Execute(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("workflow execution failed: %v", err)
	}
	if falseNodeExecuted {
		t.Fatal("inactive router branch should not execute")
	}
	if !joinExecuted {
		t.Fatal("join node should execute when at least one upstream branch is active")
	}
}

func TestScheduler_ReturnsNodeExecutionError(t *testing.T) {
	dslObj := &dsl.WorkflowDSL{
		ID:   5,
		Name: "ErrorFlow",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "boom", Type: "tool"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "boom"},
		},
	}

	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{
			id:       "boom",
			nodeType: "tool",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				return nil, errors.New("boom")
			},
		},
	}

	scheduler, err := NewScheduler(dslObj, nodes)
	if err != nil {
		t.Fatalf("failed to compile scheduler: %v", err)
	}
	err = scheduler.Execute(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected node execution error, got nil")
	}
	if !strings.Contains(err.Error(), "node [boom] execution error") {
		t.Fatalf("expected node execution error, got %v", err)
	}
}

func TestScheduler_RecordsNodeTraces(t *testing.T) {
	dslObj := &dsl.WorkflowDSL{
		ID:   6,
		Name: "TraceFlow",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "router", Type: "router"},
			{ID: "active", Type: "tool"},
			{ID: "inactive", Type: "tool"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "router"},
			{ID: "e2", Source: "router", Target: "active", SourceHandle: "true"},
			{ID: "e3", Source: "router", Target: "inactive", SourceHandle: "false"},
		},
	}

	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{
			id:       "router",
			nodeType: "router",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{"_branch": "true"}, nil
			},
		},
		&mockNode{
			id:       "active",
			nodeType: "tool",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{"ok": true}, nil
			},
		},
		&mockNode{id: "inactive", nodeType: "tool"},
	}

	scheduler, err := NewScheduler(dslObj, nodes)
	if err != nil {
		t.Fatalf("failed to compile scheduler: %v", err)
	}
	if err := scheduler.Execute(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("workflow execution failed: %v", err)
	}

	traceByID := make(map[string]NodeTrace)
	for _, trace := range scheduler.GetTraces() {
		traceByID[trace.NodeID] = trace
	}

	if traceByID["start"].Status != NodeStatusSuccess {
		t.Fatalf("expected start success trace, got %#v", traceByID["start"])
	}
	if traceByID["active"].Status != NodeStatusSuccess {
		t.Fatalf("expected active success trace, got %#v", traceByID["active"])
	}
	if traceByID["inactive"].Status != NodeStatusSkipped {
		t.Fatalf("expected inactive skipped trace, got %#v", traceByID["inactive"])
	}
}

func TestScheduler_SuspendAndResumeFromCheckpoint(t *testing.T) {
	dslObj := &dsl.WorkflowDSL{
		ID:   7,
		Name: "SuspendResumeFlow",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "before_wait", Type: "tool"},
			{ID: "wait_approval", Type: "wait"},
			{ID: "after_wait", Type: "tool", Properties: json.RawMessage(`{"approved": "{{wait_approval.approved}}"}`)},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "before_wait"},
			{ID: "e2", Source: "before_wait", Target: "wait_approval"},
			{ID: "e3", Source: "wait_approval", Target: "after_wait"},
		},
	}

	beforeRuns := 0
	afterRuns := 0
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{
			id:       "before_wait",
			nodeType: "tool",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				beforeRuns++
				return map[string]interface{}{"value": "done"}, nil
			},
		},
		&mockNode{
			id:       "wait_approval",
			nodeType: "wait",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				return nil, NewSuspensionError("wait_approval", "approval required", "approval-token", nil)
			},
		},
		&mockNode{
			id:       "after_wait",
			nodeType: "tool",
			execFunc: func(inputs map[string]interface{}) (map[string]interface{}, error) {
				afterRuns++
				if inputs["approved"] != "true" {
					return nil, errors.New("resume input was not resolved")
				}
				return map[string]interface{}{"status": "continued"}, nil
			},
		},
	}

	scheduler, err := NewScheduler(dslObj, nodes)
	if err != nil {
		t.Fatalf("failed to compile scheduler: %v", err)
	}
	err = scheduler.Execute(context.Background(), map[string]interface{}{})
	var suspension *SuspensionError
	if !errors.As(err, &suspension) {
		t.Fatalf("expected suspension error, got %v", err)
	}
	if beforeRuns != 1 {
		t.Fatalf("expected upstream node to run once before suspension, got %d", beforeRuns)
	}
	if afterRuns != 0 {
		t.Fatalf("downstream node should not run before resume, got %d", afterRuns)
	}

	checkpoint := scheduler.GetCheckpoint(suspension)
	resumed, err := NewScheduler(dslObj, nodes)
	if err != nil {
		t.Fatalf("failed to compile resumed scheduler: %v", err)
	}
	if err := resumed.ExecuteFromCheckpoint(context.Background(), checkpoint, map[string]interface{}{"approved": "true"}); err != nil {
		t.Fatalf("resume execution failed: %v", err)
	}
	if beforeRuns != 1 {
		t.Fatalf("upstream node was re-executed during resume, got %d runs", beforeRuns)
	}
	if afterRuns != 1 {
		t.Fatalf("expected downstream node to run once after resume, got %d", afterRuns)
	}

	status, ok := resumed.GetBlackboard().GetValue("after_wait", "status")
	if !ok || status != "continued" {
		t.Fatalf("expected resumed blackboard output, got %v", status)
	}
}

func TestSchedulerMergesParallelResultsInDeclarationOrder(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "slow", Type: "tool"},
			{ID: "fast", Type: "tool"},
			{ID: "join", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "slow"},
			{ID: "e2", Source: "start", Target: "fast"},
			{ID: "e3", Source: "slow", Target: "join"},
			{ID: "e4", Source: "fast", Target: "join"},
		},
	}
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "slow", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			time.Sleep(20 * time.Millisecond)
			return map[string]interface{}{"value": "slow"}, nil
		}},
		&mockNode{id: "fast", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"value": "fast"}, nil
		}},
		&mockNode{id: "join", nodeType: "end"},
	}

	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Execute(context.Background(), map[string]interface{}{"user_input": "test"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	events := scheduler.GetBlackboard().EventsAfter(0)
	nodeIDs := make([]string, 0, len(events))
	for _, event := range events {
		nodeIDs = append(nodeIDs, event.NodeID)
	}
	if got := strings.Join(nodeIDs, ","); got != "start,start,slow,fast,join" {
		t.Fatalf("parallel results followed completion order instead of declaration order: %s", got)
	}
}

func TestSchedulerAppliesDeclaredGlobalStateWrite(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "writer", Type: "tool", Writes: []dsl.StateWriteDSL{{Path: "shared.summary", Source: "text"}}},
		},
		Edges: []dsl.EdgeDSL{{ID: "e1", Source: "start", Target: "writer"}},
	}
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "writer", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"text": "final"}, nil
		}},
	}
	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Execute(context.Background(), nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	value, exists := scheduler.GetBlackboard().GetValue("shared", "summary")
	if !exists || value != "final" {
		t.Fatalf("declared state write was not applied: %#v", value)
	}
}

func TestSchedulerReducesParallelWritesInDeclarationOrder(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "slow", Type: "tool", Writes: []dsl.StateWriteDSL{{Path: "shared.items", Source: "value", Reducer: dsl.ReducerAppend}}},
			{ID: "fast", Type: "tool", Writes: []dsl.StateWriteDSL{{Path: "shared.items", Source: "value", Reducer: dsl.ReducerAppend}}},
			{ID: "join", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "slow"},
			{ID: "e2", Source: "start", Target: "fast"},
			{ID: "e3", Source: "slow", Target: "join"},
			{ID: "e4", Source: "fast", Target: "join"},
		},
	}
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "slow", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			time.Sleep(20 * time.Millisecond)
			return map[string]interface{}{"value": "slow"}, nil
		}},
		&mockNode{id: "fast", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"value": []string{"fast-a", "fast-b"}}, nil
		}},
		&mockNode{id: "join", nodeType: "end"},
	}

	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Execute(context.Background(), nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	value, exists := scheduler.GetBlackboard().GetValue("shared", "items")
	if !exists {
		t.Fatal("reduced state path is missing")
	}
	items, ok := value.([]interface{})
	if !ok || len(items) != 3 || items[0] != "slow" || items[1] != "fast-a" || items[2] != "fast-b" {
		t.Fatalf("parallel reducer did not follow declaration order: %#v", value)
	}
}

func TestSchedulerReducerFailureDoesNotPartiallyApplyNodeState(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "writer", Type: "tool", Writes: []dsl.StateWriteDSL{{Path: "shared.total", Source: "value", Reducer: dsl.ReducerSum}}},
		},
		Edges: []dsl.EdgeDSL{{ID: "e1", Source: "start", Target: "writer"}},
	}
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "writer", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"value": "not-a-number"}, nil
		}},
	}
	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	err = scheduler.Execute(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "sum reducer requires numeric input") {
		t.Fatalf("expected reducer type error, got %v", err)
	}
	if _, exists := scheduler.GetBlackboard().GetValue("writer", "value"); exists {
		t.Fatal("node output was partially applied after reducer failure")
	}
	if _, exists := scheduler.GetBlackboard().GetValue("shared", "total"); exists {
		t.Fatal("global output was partially applied after reducer failure")
	}
}

func TestBuiltInStateReducers(t *testing.T) {
	tests := []struct {
		name     string
		reducer  string
		current  interface{}
		incoming interface{}
		exists   bool
		assert   func(t *testing.T, value interface{})
	}{
		{name: "sum", reducer: dsl.ReducerSum, current: 2, incoming: 3.5, exists: true, assert: func(t *testing.T, value interface{}) {
			if value != 5.5 {
				t.Fatalf("sum = %#v", value)
			}
		}},
		{name: "min", reducer: dsl.ReducerMin, current: 9, incoming: 4, exists: true, assert: func(t *testing.T, value interface{}) {
			if value != float64(4) {
				t.Fatalf("min = %#v", value)
			}
		}},
		{name: "max", reducer: dsl.ReducerMax, current: 9, incoming: 14, exists: true, assert: func(t *testing.T, value interface{}) {
			if value != float64(14) {
				t.Fatalf("max = %#v", value)
			}
		}},
		{name: "merge", reducer: dsl.ReducerMerge, current: map[string]interface{}{"left": true, "same": "old"}, incoming: map[string]string{"right": "yes", "same": "new"}, exists: true, assert: func(t *testing.T, value interface{}) {
			merged := value.(map[string]interface{})
			if merged["left"] != true || merged["right"] != "yes" || merged["same"] != "new" {
				t.Fatalf("merge = %#v", value)
			}
		}},
		{name: "first", reducer: dsl.ReducerFirst, current: "old", incoming: "new", exists: true, assert: func(t *testing.T, value interface{}) {
			if value != "old" {
				t.Fatalf("first = %#v", value)
			}
		}},
		{name: "last", reducer: dsl.ReducerLast, current: "old", incoming: "new", exists: true, assert: func(t *testing.T, value interface{}) {
			if value != "new" {
				t.Fatalf("last = %#v", value)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := reduceStateValue(test.reducer, test.current, test.incoming, test.exists)
			if err != nil {
				t.Fatalf("reduceStateValue() error = %v", err)
			}
			test.assert(t, value)
		})
	}
}

func TestSchedulerParallelNodesReceiveSameReadOnlyGeneration(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "producer", Type: "tool"},
			{ID: "observer", Type: "tool"},
			{ID: "join", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "producer"},
			{ID: "e2", Source: "start", Target: "observer"},
			{ID: "e3", Source: "producer", Target: "join"},
			{ID: "e4", Source: "observer", Target: "join"},
		},
	}
	var observerState StateView
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "producer", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"value": "produced"}, nil
		}},
		&stateInspectingNode{id: "observer", execute: func(state StateView) (map[string]interface{}, error) {
			observerState = state
			_, sawParallelOutput := state.GetValue("producer", "value")
			return map[string]interface{}{"saw_parallel_output": sawParallelOutput}, nil
		}},
		&mockNode{id: "join", nodeType: "end"},
	}

	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Execute(context.Background(), nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	saw, _ := scheduler.GetBlackboard().GetValue("observer", "saw_parallel_output")
	if saw != false {
		t.Fatalf("parallel node observed a sibling delta from the same wave: %#v", saw)
	}
	if observerState == nil || observerState.Version() >= scheduler.GetBlackboard().Version() {
		t.Fatalf("observer did not retain an earlier immutable generation: view=%v current=%d", observerState, scheduler.GetBlackboard().Version())
	}
}

func TestSchedulerEmitsImmutableCommitAfterEachMutatingWave(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "step", Type: "tool"},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "step"},
			{ID: "e2", Source: "step", Target: "end"},
		},
	}
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "step", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"value": "done"}, nil
		}},
		&mockNode{id: "end", nodeType: "end"},
	}
	var versions []uint64
	scheduler, err := NewScheduler(definition, nodes, WithStateCommitHook(func(_ context.Context, commit StateCommit) error {
		versions = append(versions, commit.StateVersion)
		commit.Snapshot["start"]["user_input"] = "mutated"
		return nil
	}))
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	if err := scheduler.Execute(context.Background(), map[string]interface{}{"user_input": "stable"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := len(versions); got != 3 {
		t.Fatalf("expected 3 wave commits, got %d (%v)", got, versions)
	}
	if versions[0] != 2 || versions[1] != 3 || versions[2] != 4 {
		t.Fatalf("unexpected commit versions: %v", versions)
	}
	value, _ := scheduler.GetBlackboard().GetValue("start", "user_input")
	if value != "stable" {
		t.Fatalf("commit callback mutated scheduler state: %#v", value)
	}
}

func TestSchedulerWaitsForStartedSiblingAfterNodeFailure(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "failure", Type: "tool"},
			{ID: "sibling", Type: "tool"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "e1", Source: "start", Target: "failure"},
			{ID: "e2", Source: "start", Target: "sibling"},
		},
	}
	siblingStarted := make(chan struct{})
	releaseSibling := make(chan struct{})
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&mockNode{id: "failure", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			<-siblingStarted
			return nil, errors.New("boom")
		}},
		&mockNode{id: "sibling", nodeType: "tool", execFunc: func(map[string]interface{}) (map[string]interface{}, error) {
			close(siblingStarted)
			<-releaseSibling
			return map[string]interface{}{"done": true}, nil
		}},
	}
	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- scheduler.Execute(context.Background(), nil) }()
	<-siblingStarted
	select {
	case err := <-done:
		t.Fatalf("scheduler returned before the started sibling exited: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseSibling)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "node [failure] execution error") {
		t.Fatalf("expected deterministic failure after sibling exit, got %v", err)
	}
}

func TestSchedulerCancellationWaitsForNodeExit(t *testing.T) {
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "blocking", Type: "tool"},
		},
		Edges: []dsl.EdgeDSL{{ID: "e1", Source: "start", Target: "blocking"}},
	}
	started := make(chan struct{})
	exited := make(chan struct{})
	nodes := []WorkflowNode{
		&mockNode{id: "start", nodeType: "start"},
		&contextAwareNode{id: "blocking", execute: func(ctx context.Context) (map[string]interface{}, error) {
			close(started)
			<-ctx.Done()
			time.Sleep(20 * time.Millisecond)
			close(exited)
			return nil, ctx.Err()
		}},
	}
	scheduler, err := NewScheduler(definition, nodes)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Execute(ctx, nil) }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("scheduler returned before the canceled node exited")
	}
}

type stateInspectingNode struct {
	id      string
	execute func(StateView) (map[string]interface{}, error)
}

func (n *stateInspectingNode) ID() string   { return n.id }
func (n *stateInspectingNode) Type() string { return "tool" }
func (n *stateInspectingNode) Execute(_ context.Context, state StateView, _ map[string]interface{}) (map[string]interface{}, error) {
	return n.execute(state)
}

type contextAwareNode struct {
	id      string
	execute func(context.Context) (map[string]interface{}, error)
}

func (n *contextAwareNode) ID() string   { return n.id }
func (n *contextAwareNode) Type() string { return "tool" }
func (n *contextAwareNode) Execute(ctx context.Context, _ StateView, _ map[string]interface{}) (map[string]interface{}, error) {
	return n.execute(ctx)
}
