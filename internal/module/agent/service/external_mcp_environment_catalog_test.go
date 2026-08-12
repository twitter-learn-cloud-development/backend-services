package service

import (
	"context"
	"strings"
	"testing"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestExternalMCPEnvironmentUsesCurrentGovernedTenantCatalog(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	store := &externalMCPRuntimeStore{
		connection: externalmcp.Connection{
			ID: "mcpconn_1", UserID: 41, Scope: externalmcp.ScopeUser, ServerID: "mcp_server", Revision: 3,
			Status: externalmcp.ConnectionStatusActive, DiscoveryStatus: externalmcp.DiscoveryStatusReady,
			ActiveSnapshotID: "mcpsnap_1",
			ToolPolicies: []externalmcp.ToolPolicy{{
				SnapshotID: "mcpsnap_1", ToolName: "lookup", QualifiedName: qualifiedName,
				Category: externalmcp.ToolCategoryRead, Enabled: true,
			}},
		},
		snapshot: externalmcp.ToolSchemaSnapshot{
			ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 41,
			ServerID: "mcp_server", Version: 2,
			SchemaHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Tools: []externalmcp.ToolSchema{{
				Name: "lookup", QualifiedName: qualifiedName, Description: "private connector description",
				InputSchemaJSON: `{"type":"object"}`, DeclaredReadOnly: true,
			}},
		},
	}
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(&externalMCPRuntimeCaller{}),
	)
	service := &AgentService{externalMCPEnabled: true, externalMCPManager: manager}
	environment, err := service.newExternalMCPEnvironment(41)
	if err != nil {
		t.Fatalf("newExternalMCPEnvironment() error = %v", err)
	}
	task := externalMCPEnvironmentTask(qualifiedName)

	tools, err := environment.Tools(context.Background(), task)
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != qualifiedName || tools[0].Category != agentRuntime.ToolCategoryRead {
		t.Fatalf("Tools() = %+v, want current governed tool", tools)
	}
	before, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	serialized := string(before.Metadata) + before.Reference
	for _, forbidden := range []string{"mcpconn_1", "mcpsnap_1", store.snapshot.SchemaHash, "private connector description", "type"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("Snapshot() leaked raw MCP binding %q: %s", forbidden, serialized)
		}
	}

	otherTenant, err := service.newExternalMCPEnvironment(99)
	if err != nil {
		t.Fatalf("newExternalMCPEnvironment(other tenant) error = %v", err)
	}
	otherTools, err := otherTenant.Tools(context.Background(), task)
	if err != nil {
		t.Fatalf("other tenant Tools() error = %v", err)
	}
	if len(otherTools) != 0 {
		t.Fatalf("other tenant Tools() = %+v, want empty", otherTools)
	}

	store.connection.ToolPolicies[0].Enabled = false
	revoked, err := environment.Tools(context.Background(), task)
	if err != nil {
		t.Fatalf("Tools(after policy revoke) error = %v", err)
	}
	if len(revoked) != 0 {
		t.Fatalf("Tools(after policy revoke) = %+v, want empty", revoked)
	}
}

func TestExternalMCPEnvironmentTracksActiveSnapshotAndRejectsDisabledService(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	source := &externalMCPEnvironmentSourceFake{tools: []externalmcp.ExecutableTool{{
		ConnectionID: "mcpconn_1", ConnectionOwnerID: 42, ConnectionScope: externalmcp.ScopeUser, ConnectionRevision: 2,
		ServerID: "mcp_server", SnapshotID: "mcpsnap_1", SnapshotVersion: 1,
		SchemaHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Schema: externalmcp.ToolSchema{
			Name: "lookup", QualifiedName: qualifiedName, InputSchemaJSON: `{"type":"object"}`,
		},
		Policy: externalmcp.ToolPolicy{
			SnapshotID: "mcpsnap_1", ToolName: "lookup", QualifiedName: qualifiedName,
			Category: externalmcp.ToolCategoryRisky, Enabled: true,
		},
	}}}
	catalog := &externalMCPEnvironmentCatalog{source: source}
	environment, err := agentEnvironment.NewExternalMCPEnvironment(catalog, 42)
	if err != nil {
		t.Fatalf("NewExternalMCPEnvironment() error = %v", err)
	}
	task := externalMCPEnvironmentTask(qualifiedName)
	before, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	})
	if err != nil {
		t.Fatalf("Snapshot(before) error = %v", err)
	}
	if source.userID != 42 {
		t.Fatalf("catalog source user = %d, want 42", source.userID)
	}
	source.tools[0].SnapshotVersion++
	after, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseAfter,
	})
	if err != nil {
		t.Fatalf("Snapshot(after) error = %v", err)
	}
	if before.Digest == after.Digest {
		t.Fatal("Snapshot() did not track active MCP snapshot change")
	}

	if _, err := (&AgentService{}).newExternalMCPEnvironment(42); err == nil {
		t.Fatal("newExternalMCPEnvironment(disabled) error = nil")
	}
}

type externalMCPEnvironmentSourceFake struct {
	tools  []externalmcp.ExecutableTool
	err    error
	userID uint64
}

func (source *externalMCPEnvironmentSourceFake) ListGovernedTools(
	_ context.Context,
	userID uint64,
) ([]externalmcp.ExecutableTool, error) {
	source.userID = userID
	return append([]externalmcp.ExecutableTool(nil), source.tools...), source.err
}

func externalMCPEnvironmentTask(tools ...string) agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "task-service-external-mcp", Goal: "Use the governed external MCP tool.", AllowedTools: tools,
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: "remote-result", Description: "The remote tool returned governed evidence.", Required: true,
		}},
	}
}
