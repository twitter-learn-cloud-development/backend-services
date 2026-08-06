package service

import (
	"encoding/json"
	"testing"

	"twitter-clone/internal/module/agent/workflow/dsl"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

func TestNormalizeWorkflowDSLAddsVersionAndPreservesUIExtension(t *testing.T) {
	service := &AgentService{workflowToolExecutor: workflowTool.NewExecutor(workflowTool.NewRegistry())}
	raw := `{
		"name":"versioned",
		"nodes":[{"id":"start","type":"start"},{"id":"end","type":"end"}],
		"edges":[{"id":"e1","source":"start","target":"end"}],
		"ui":{"nodes":[{"id":"start","position":{"x":12,"y":34}}]}
	}`

	normalized, err := service.normalizeWorkflowDSLJSON(raw)
	if err != nil {
		t.Fatalf("normalizeWorkflowDSLJSON() error = %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &payload); err != nil {
		t.Fatalf("decode normalized DSL: %v", err)
	}
	if payload["dsl_version"] != dsl.CurrentVersion {
		t.Fatalf("expected dsl_version %s, got %#v", dsl.CurrentVersion, payload["dsl_version"])
	}
	if payload["workflow_version"] != float64(1) {
		t.Fatalf("expected workflow_version 1, got %#v", payload["workflow_version"])
	}
	if _, exists := payload["ui"]; !exists {
		t.Fatal("normalization dropped the frontend ui extension")
	}
}

func TestNormalizeWorkflowDSLRejectsUnknownVersion(t *testing.T) {
	service := &AgentService{workflowToolExecutor: workflowTool.NewExecutor(workflowTool.NewRegistry())}
	_, err := service.normalizeWorkflowDSLJSON(`{
		"dsl_version":"2.0",
		"nodes":[{"id":"start","type":"start"}],
		"edges":[]
	}`)
	if err == nil {
		t.Fatal("expected unsupported DSL version error")
	}
}
