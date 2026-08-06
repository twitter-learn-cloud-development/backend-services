package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"twitter-clone/internal/module/agent/workflow/dsl"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

func TestGetAvailableModelsExcludesEmbeddingModels(t *testing.T) {
	t.Setenv("DASHSCOPE_MODEL_CHAT", "qwen-plus")
	t.Setenv("LM_STUDIO_MODEL_EMBEDDING", "text-embedding-bge-m3")

	models := GetAvailableModels()

	require.Len(t, models, 1)
	require.Equal(t, "qwen-plus", models[0].Name)
	require.NotContains(t, models[0].Description, "向量")
}

func TestBuildWorkflowNodesSupportsAgentStrategies(t *testing.T) {
	properties, err := json.Marshal(map[string]interface{}{
		"tool_name": "ReActAgent",
		"objective": "{{start.user_input}}",
	})
	require.NoError(t, err)
	registry := workflowTool.NewRegistry()
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"ReActAgent", "test", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) { return nil, nil },
	)))

	nodes, err := buildWorkflowNodes(&dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "react", Type: "agent", Properties: properties},
			{ID: "end", Type: "end"},
		},
	}, workflowTool.NewExecutor(registry))

	require.NoError(t, err)
	require.Len(t, nodes, 3)
	require.Equal(t, "react", nodes[1].ID())
}

func TestWorkflowAssistantContentUsesLastReadableNode(t *testing.T) {
	workflow := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "planner", Type: "llm"},
			{ID: "executor", Type: "agent"},
			{ID: "end", Type: "end"},
		},
	}
	snapshot := map[string]map[string]interface{}{
		"planner":  {"text": "计划内容"},
		"executor": {"text": "最终执行结果"},
		"end":      {},
	}

	require.Equal(t, "最终执行结果", workflowAssistantContent(snapshot, workflow))
}
