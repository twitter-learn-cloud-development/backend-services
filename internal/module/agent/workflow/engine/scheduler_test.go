package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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

func (m *mockNode) Execute(ctx context.Context, blackboard *Blackboard, inputs map[string]interface{}) (map[string]interface{}, error) {
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
