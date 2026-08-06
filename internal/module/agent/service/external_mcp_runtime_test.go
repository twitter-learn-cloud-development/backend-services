package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type externalMCPRuntimeStore struct {
	connection externalmcp.Connection
	snapshot   externalmcp.ToolSchemaSnapshot
}

func (store *externalMCPRuntimeStore) CreateMCPConnection(context.Context, *externalmcp.Connection) error {
	return errors.New("unexpected create")
}

func (store *externalMCPRuntimeStore) UpdateMCPConnection(context.Context, *externalmcp.Connection, int64) error {
	return errors.New("unexpected update")
}

func (store *externalMCPRuntimeStore) ListMCPConnections(context.Context, uint64, int, int) ([]*externalmcp.Connection, int64, error) {
	return nil, 0, errors.New("unexpected list")
}

func (store *externalMCPRuntimeStore) GetMCPConnection(_ context.Context, id string, userID uint64) (*externalmcp.Connection, error) {
	if store.connection.ID != id || store.connection.UserID != userID {
		return nil, externalmcp.ErrConnectionNotFound
	}
	connection := store.connection
	connection.ToolPolicies = append([]externalmcp.ToolPolicy(nil), store.connection.ToolPolicies...)
	return &connection, nil
}

func (store *externalMCPRuntimeStore) GetMCPConnectionByServerID(_ context.Context, serverID string, userID uint64) (*externalmcp.Connection, error) {
	if store.connection.ServerID != serverID || store.connection.UserID != userID {
		return nil, externalmcp.ErrConnectionNotFound
	}
	connection := store.connection
	connection.ToolPolicies = append([]externalmcp.ToolPolicy(nil), store.connection.ToolPolicies...)
	return &connection, nil
}

func (store *externalMCPRuntimeStore) RevokeMCPConnection(context.Context, string, uint64, int64) error {
	return errors.New("unexpected revoke")
}

func (store *externalMCPRuntimeStore) SaveMCPToolSnapshot(context.Context, *externalmcp.ToolSchemaSnapshot) (*externalmcp.ToolSchemaSnapshot, error) {
	return nil, errors.New("unexpected save snapshot")
}

func (store *externalMCPRuntimeStore) GetMCPToolSnapshot(_ context.Context, id, connectionID string, userID uint64) (*externalmcp.ToolSchemaSnapshot, error) {
	if store.snapshot.ID != id || store.snapshot.ConnectionID != connectionID || store.snapshot.UserID != userID {
		return nil, externalmcp.ErrSnapshotNotFound
	}
	snapshot := store.snapshot
	snapshot.Tools = append([]externalmcp.ToolSchema(nil), store.snapshot.Tools...)
	return &snapshot, nil
}

func (store *externalMCPRuntimeStore) ListMCPExecutionBindings(_ context.Context, userID uint64, _ int) ([]externalmcp.ExecutionBinding, error) {
	if store.connection.UserID != userID {
		return nil, nil
	}
	return []externalmcp.ExecutionBinding{{Connection: store.connection, Snapshot: store.snapshot}}, nil
}

type externalMCPRuntimeCaller struct {
	request   externalmcp.DiscoveryRequest
	toolName  string
	arguments map[string]interface{}
	calls     int
}

func (caller *externalMCPRuntimeCaller) Call(
	_ context.Context,
	request externalmcp.DiscoveryRequest,
	toolName string,
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	caller.request = request
	caller.toolName = toolName
	caller.arguments = arguments
	caller.calls++
	result := mcp.NewToolResultText("found one result")
	result.StructuredContent = map[string]interface{}{"count": float64(1)}
	return result, nil
}

type externalMCPAuditRecorder struct {
	mu     sync.Mutex
	events []workflowTool.AuditEvent
}

func (recorder *externalMCPAuditRecorder) Record(_ context.Context, event workflowTool.AuditEvent) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.events = append(recorder.events, event)
}

func (recorder *externalMCPAuditRecorder) last() workflowTool.AuditEvent {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.events[len(recorder.events)-1]
}

func TestExternalMCPRuntimeExecutesThroughGovernedToolExecutor(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	store := &externalMCPRuntimeStore{
		connection: externalmcp.Connection{
			ID: "mcpconn_1", UserID: 41, ServerID: "mcp_server",
			Transport: externalmcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
			AuthType: externalmcp.AuthNone, Status: externalmcp.ConnectionStatusActive,
			DiscoveryStatus: externalmcp.DiscoveryStatusReady, ActiveSnapshotID: "mcpsnap_1",
			ToolPolicies: []externalmcp.ToolPolicy{{
				SnapshotID: "mcpsnap_1", ToolName: "lookup", QualifiedName: qualifiedName,
				Category: externalmcp.ToolCategoryRead, Enabled: true,
			}},
		},
		snapshot: externalmcp.ToolSchemaSnapshot{
			ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 41, ServerID: "mcp_server",
			Tools: []externalmcp.ToolSchema{{
				Name: "lookup", QualifiedName: qualifiedName, Description: "look up public documentation",
				InputSchemaJSON:  `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string"},"user_id":{"type":"string"}},"required":["query","user_id"]}`,
				DeclaredReadOnly: true,
			}},
		},
	}
	caller := &externalMCPRuntimeCaller{}
	manager := externalmcp.NewManager(
		store,
		nil,
		nil,
		nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(caller),
	)
	audit := &externalMCPAuditRecorder{}
	service := &AgentService{
		externalMCPManager: manager,
		workflowToolExecutor: workflowTool.NewExecutor(
			workflowTool.NewRegistry(),
			workflowTool.WithAuditSink(audit),
		),
	}
	executor := &mcpRuntimeToolExecutor{service: service}

	result, err := executor.Execute(context.Background(), agentRuntime.ToolCall{
		RunContext: agentRuntime.RunContext{RunID: "run-mcp-1", UserID: 41},
		ActionID:   "tool-call-1",
		Name:       qualifiedName,
		Arguments:  json.RawMessage(`{"query":"golang","user_id":"remote-account"}`),
	})

	require.NoError(t, err)
	require.Equal(t, "found one result", result.Content)
	require.JSONEq(t, `{"count":1}`, string(result.StructuredContent))
	require.Equal(t, 1, caller.calls)
	require.Equal(t, "lookup", caller.toolName)
	require.Equal(t, "golang", caller.arguments["query"])
	require.Equal(t, uint64(41), caller.arguments["user_id"])
	require.Equal(t, "https://mcp.example.com/mcp", caller.request.Endpoint)
	require.Empty(t, caller.request.BearerToken)

	event := audit.last()
	require.Equal(t, qualifiedName, event.ToolName)
	require.Equal(t, workflowTool.CategoryRead, event.Category)
	require.Equal(t, workflowTool.SourceRuntime, event.Source)
	require.Equal(t, uint64(41), event.UserID)
	require.Equal(t, "run-mcp-1", event.RunID)
	require.Equal(t, "tool-call-1", event.StepID)
	require.Equal(t, "succeeded", event.Decision)
	require.Equal(t, "[REDACTED]", event.Inputs["query"])
}

func TestExternalMCPRuntimeRejectsInvalidInputBeforeRemoteCall(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	store := &externalMCPRuntimeStore{
		connection: externalmcp.Connection{
			ID: "mcpconn_1", UserID: 41, ServerID: "mcp_server",
			Status: externalmcp.ConnectionStatusActive, DiscoveryStatus: externalmcp.DiscoveryStatusReady,
			ActiveSnapshotID: "mcpsnap_1",
			ToolPolicies: []externalmcp.ToolPolicy{{
				SnapshotID: "mcpsnap_1", ToolName: "lookup", QualifiedName: qualifiedName,
				Category: externalmcp.ToolCategoryRead, Enabled: true,
			}},
		},
		snapshot: externalmcp.ToolSchemaSnapshot{
			ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 41, ServerID: "mcp_server",
			Tools: []externalmcp.ToolSchema{{
				Name: "lookup", QualifiedName: qualifiedName,
				InputSchemaJSON:  `{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`,
				DeclaredReadOnly: true,
			}},
		},
	}
	caller := &externalMCPRuntimeCaller{}
	manager := externalmcp.NewManager(store, nil, nil, nil, externalmcp.WithEnabled(true), externalmcp.WithCaller(caller))
	service := &AgentService{
		externalMCPManager: manager,
		workflowToolExecutor: workflowTool.NewExecutor(
			workflowTool.NewRegistry(),
			workflowTool.WithAuditSink(&externalMCPAuditRecorder{}),
		),
	}

	_, err := (&mcpRuntimeToolExecutor{service: service}).Execute(context.Background(), agentRuntime.ToolCall{
		RunContext: agentRuntime.RunContext{RunID: "run-mcp-invalid", UserID: 41},
		ActionID:   "tool-call-invalid",
		Name:       qualifiedName,
		Arguments:  json.RawMessage(`{}`),
	})

	require.ErrorIs(t, err, workflowTool.ErrInvalidInput)
	require.Zero(t, caller.calls)
}

func TestExternalMCPRuntimeMapsGovernedCategoriesAndPolicies(t *testing.T) {
	tools := []externalmcp.ExecutableTool{
		{
			Schema: externalmcp.ToolSchema{QualifiedName: "mcp_server.lookup"},
			Policy: externalmcp.ToolPolicy{Category: externalmcp.ToolCategoryRead},
		},
		{
			Schema: externalmcp.ToolSchema{QualifiedName: "mcp_server.mutate"},
			Policy: externalmcp.ToolPolicy{Category: externalmcp.ToolCategoryRisky},
		},
		{
			Schema: externalmcp.ToolSchema{
				QualifiedName:   "mcp_server.create_record",
				InputSchemaJSON: `{"type":"object","properties":{"idempotency_key":{"type":"string"}}}`,
			},
			Policy: externalmcp.ToolPolicy{Category: externalmcp.ToolCategoryWrite},
		},
	}

	definitions := externalMCPRuntimeTools(tools)
	require.Len(t, definitions, 3)
	require.Equal(t, agentRuntime.ToolCategoryRead, definitions[0].Category)
	require.False(t, definitions[0].RequiresApproval)
	require.Equal(t, agentRuntime.ToolCategoryRisky, definitions[1].Category)
	require.True(t, definitions[1].RequiresApproval)
	require.Equal(t, agentRuntime.ToolCategoryWrite, definitions[2].Category)
	require.True(t, definitions[2].RequiresApproval)

	riskySpec := externalMCPToolSpec(tools[1])
	require.Equal(t, workflowTool.CategoryRisky, riskySpec.Category)
	require.Equal(t, workflowTool.ApprovalRequired, riskySpec.Approval)
	require.Equal(t, 1, riskySpec.Retry.MaxAttempts)

	writeSpec := externalMCPToolSpec(tools[2])
	require.Equal(t, workflowTool.CategoryWrite, writeSpec.Category)
	require.Equal(t, workflowTool.ApprovalRequired, writeSpec.Approval)
	require.True(t, writeSpec.Idempotency.Required)
}
