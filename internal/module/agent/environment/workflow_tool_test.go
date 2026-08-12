package environment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	workflowPublicationID = "64b64c9f7f0c2f11b9f0a001"
	workflowRevisionID    = "64b64c9f7f0c2f11b9f0a003"
	workflowDSLHash       = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

type staticWorkflowToolCatalog struct {
	bindings []WorkflowToolBinding
	err      error
	userID   uint64
	limit    int
	calls    int
}

func (catalog *staticWorkflowToolCatalog) ListWorkflowTools(
	_ context.Context,
	userID uint64,
	limit int,
) ([]WorkflowToolBinding, error) {
	catalog.calls++
	catalog.userID = userID
	catalog.limit = limit
	return append([]WorkflowToolBinding(nil), catalog.bindings...), catalog.err
}

func TestWorkflowToolEnvironmentFiltersByTenantCatalogAndTask(t *testing.T) {
	catalog := &staticWorkflowToolCatalog{bindings: []WorkflowToolBinding{
		workflowBinding("workflow_64b64c9f7f0c2f11b9f0a020", workflowPublicationID),
		workflowBinding("workflow_64b64c9f7f0c2f11b9f0a010", "64b64c9f7f0c2f11b9f0a004"),
	}}
	environment, err := NewWorkflowToolEnvironment(catalog, 42, WithWorkflowToolLimit(7))
	if err != nil {
		t.Fatalf("NewWorkflowToolEnvironment() error = %v", err)
	}
	tools, err := environment.Tools(context.Background(), workflowTask(
		"workflow_64b64c9f7f0c2f11b9f0a010",
	))
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if catalog.userID != 42 || catalog.limit != 7 {
		t.Fatalf("catalog scope = user %d limit %d", catalog.userID, catalog.limit)
	}
	if len(tools) != 1 || tools[0].Name != "workflow_64b64c9f7f0c2f11b9f0a010" {
		t.Fatalf("Tools() = %+v, want exact task intersection", tools)
	}
	tools[0].InputSchema[0] = '['
	if catalog.bindings[1].Tool.InputSchema[0] != '{' {
		t.Fatal("Tools() exposed mutable catalog input schema")
	}
}

func TestWorkflowToolEnvironmentSnapshotTracksImmutableBindingWithoutLeakingContent(t *testing.T) {
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	secret := "workflow-description-and-schema-secret"
	binding := workflowBinding("workflow_64b64c9f7f0c2f11b9f0a010", workflowPublicationID)
	binding.Tool.Description = secret
	binding.Tool.InputSchema = json.RawMessage(`{"type":"object","properties":{"token":{"default":"` + secret + `"}}}`)
	catalog := &staticWorkflowToolCatalog{bindings: []WorkflowToolBinding{binding}}
	environment, err := NewWorkflowToolEnvironment(catalog, 42, WithWorkflowToolClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewWorkflowToolEnvironment() error = %v", err)
	}
	task := workflowTask(binding.Tool.Name)

	before, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	})
	if err != nil {
		t.Fatalf("Snapshot(before) error = %v", err)
	}
	catalog.bindings[0].Tool.Description = "changed " + secret
	catalog.bindings[0].Tool.InputSchema = json.RawMessage(`{"type":"object","required":["changed"]}`)
	after, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseAfter,
	})
	if err != nil {
		t.Fatalf("Snapshot(after) error = %v", err)
	}
	if before.Digest != after.Digest || before.Reference != after.Reference || before.ID != after.ID {
		t.Fatalf("catalog identity changed with presentation metadata: before=%+v after=%+v", before, after)
	}
	if before.Environment != WorkflowToolEnvironmentName || !before.CapturedAt.Equal(now.UTC()) {
		t.Fatalf("Snapshot() identity/time = %+v", before)
	}
	serialized := string(before.Metadata) + before.Reference + before.Digest + before.ID
	for _, forbidden := range []string{secret, workflowPublicationID, workflowRevisionID, workflowDSLHash, "properties"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("Snapshot() leaked workflow content or raw identity %q: %s", forbidden, serialized)
		}
	}
	if !strings.Contains(string(before.Metadata), `"binding_digest":"sha256:`) ||
		!strings.Contains(string(before.Metadata), `"phase":"before"`) ||
		!strings.Contains(string(after.Metadata), `"phase":"after"`) {
		t.Fatalf("Snapshot() metadata is incomplete: before=%s after=%s", before.Metadata, after.Metadata)
	}

	catalog.bindings[0].WorkflowRevisionNumber++
	changed, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseAfter,
	})
	if err != nil {
		t.Fatalf("Snapshot(changed binding) error = %v", err)
	}
	if changed.Digest == before.Digest {
		t.Fatal("Snapshot() did not detect immutable revision binding change")
	}
}

func TestWorkflowToolEnvironmentFailsClosedForInvalidBindings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkflowToolBinding)
	}{
		{name: "identity", mutate: func(binding *WorkflowToolBinding) { binding.WorkflowRevisionID = "invalid" }},
		{name: "revision", mutate: func(binding *WorkflowToolBinding) { binding.PublicationRevision = 0 }},
		{name: "hash", mutate: func(binding *WorkflowToolBinding) { binding.WorkflowDSLHash = "invalid" }},
		{name: "category", mutate: func(binding *WorkflowToolBinding) { binding.Tool.Category = "unknown" }},
		{name: "schema", mutate: func(binding *WorkflowToolBinding) { binding.Tool.InputSchema = json.RawMessage(`{"type":`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding := workflowBinding("workflow_64b64c9f7f0c2f11b9f0a010", workflowPublicationID)
			test.mutate(&binding)
			environment, err := NewWorkflowToolEnvironment(
				&staticWorkflowToolCatalog{bindings: []WorkflowToolBinding{binding}},
				42,
			)
			if err != nil {
				t.Fatalf("NewWorkflowToolEnvironment() error = %v", err)
			}
			if _, err = environment.Tools(context.Background(), workflowTask(binding.Tool.Name)); err == nil {
				t.Fatal("Tools() error = nil, want invalid binding rejection")
			}
		})
	}
}

func TestWorkflowToolEnvironmentRejectsDuplicatesCanceledContextAndInvalidConstruction(t *testing.T) {
	if _, err := NewWorkflowToolEnvironment(nil, 42); err == nil {
		t.Fatal("NewWorkflowToolEnvironment(nil catalog) error = nil")
	}
	if _, err := NewWorkflowToolEnvironment(&staticWorkflowToolCatalog{}, 0); err == nil {
		t.Fatal("NewWorkflowToolEnvironment(zero user) error = nil")
	}
	if _, err := NewWorkflowToolEnvironment(&staticWorkflowToolCatalog{}, 42, WithWorkflowToolLimit(101)); err == nil {
		t.Fatal("NewWorkflowToolEnvironment(invalid limit) error = nil")
	}
	if _, err := NewWorkflowToolEnvironment(&staticWorkflowToolCatalog{}, 42, WithWorkflowToolClock(nil)); err == nil {
		t.Fatal("NewWorkflowToolEnvironment(nil clock) error = nil")
	}

	binding := workflowBinding("workflow_64b64c9f7f0c2f11b9f0a010", workflowPublicationID)
	catalog := &staticWorkflowToolCatalog{bindings: []WorkflowToolBinding{binding, binding}}
	environment, err := NewWorkflowToolEnvironment(catalog, 42)
	if err != nil {
		t.Fatalf("NewWorkflowToolEnvironment() error = %v", err)
	}
	if _, err = environment.Tools(context.Background(), workflowTask(binding.Tool.Name)); err == nil ||
		!strings.Contains(err.Error(), "duplicate workflow catalog tool") {
		t.Fatalf("Tools(duplicate) error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	beforeCalls := catalog.calls
	if _, err = environment.Tools(canceled, workflowTask(binding.Tool.Name)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Tools(canceled) error = %v", err)
	}
	if catalog.calls != beforeCalls {
		t.Fatal("Tools(canceled) called catalog")
	}
	if _, err = environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: workflowTask(binding.Tool.Name), Phase: agentRuntime.SnapshotPhaseBefore, Scope: []string{"workflow:raw"},
	}); err == nil || !strings.Contains(err.Error(), "does not support resource scope") {
		t.Fatalf("Snapshot(scope) error = %v", err)
	}
}

func workflowBinding(toolName string, publicationID string) WorkflowToolBinding {
	boundWorkflowID := strings.TrimPrefix(toolName, "workflow_")
	return WorkflowToolBinding{
		Tool: agentRuntime.ToolDefinition{
			Name: toolName, Description: "published workflow",
			InputSchema: json.RawMessage(`{"type":"object"}`), Category: agentRuntime.ToolCategoryRead,
		},
		PublicationID: publicationID, PublicationRevision: 3,
		WorkflowID: boundWorkflowID, WorkflowRevisionID: workflowRevisionID,
		WorkflowRevisionNumber: 2, WorkflowDSLHash: workflowDSLHash,
	}
}

func workflowTask(tools ...string) agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "task-workflow-tool", Goal: "Run the selected published workflow.", AllowedTools: tools,
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: "workflow-result", Description: "The bound workflow completed successfully.", Required: true,
		}},
	}
}
