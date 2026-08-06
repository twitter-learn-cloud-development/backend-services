package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/dsl"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type temporaryExternalMCPError struct{}

func (temporaryExternalMCPError) Error() string   { return "external MCP request timed out" }
func (temporaryExternalMCPError) Timeout() bool   { return true }
func (temporaryExternalMCPError) Temporary() bool { return true }

type failingExternalMCPWorkflowCaller struct {
	calls int
}

func (caller *failingExternalMCPWorkflowCaller) Call(
	_ context.Context,
	_ externalmcp.DiscoveryRequest,
	_ string,
	_ map[string]interface{},
) (*mcp.CallToolResult, error) {
	caller.calls++
	return nil, temporaryExternalMCPError{}
}

type retryingExternalMCPWriteCaller struct {
	calls     int
	arguments []map[string]interface{}
}

func (caller *retryingExternalMCPWriteCaller) Call(
	_ context.Context,
	_ externalmcp.DiscoveryRequest,
	_ string,
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	caller.calls++
	caller.arguments = append(caller.arguments, cloneExternalMCPArguments(arguments))
	if caller.calls == 1 {
		return nil, temporaryExternalMCPError{}
	}
	return mcp.NewToolResultText("created once"), nil
}

func TestExternalMCPRiskyWorkflowSuspendsAndExecutesOnceAfterApproval(t *testing.T) {
	const qualifiedName = "mcp_server.mutate"
	properties, err := json.Marshal(map[string]interface{}{
		"tool_name": qualifiedName,
		"mcp_arguments": map[string]interface{}{
			"value": "{{start.user_input}}",
		},
		"timeout_sec": 20,
	})
	require.NoError(t, err)
	dslJSON, err := json.Marshal(dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "mutate", Type: "tool", Properties: properties},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-mutate", Source: "start", Target: "mutate"},
			{ID: "mutate-end", Source: "mutate", Target: "end"},
		},
	})
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 42, Name: "external MCP approval", DSLJSON: string(dslJSON),
	}
	repo := newApprovalWorkflowRepositoryFake(workflow)
	store := &externalMCPRuntimeStore{
		connection: externalmcp.Connection{
			ID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Transport: externalmcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
			AuthType: externalmcp.AuthNone, Status: externalmcp.ConnectionStatusActive,
			DiscoveryStatus: externalmcp.DiscoveryStatusReady, ActiveSnapshotID: "mcpsnap_1",
			ToolPolicies: []externalmcp.ToolPolicy{{
				SnapshotID: "mcpsnap_1", ToolName: "mutate", QualifiedName: qualifiedName,
				Category: externalmcp.ToolCategoryRisky, Enabled: true,
			}},
		},
		snapshot: externalmcp.ToolSchemaSnapshot{
			ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Tools: []externalmcp.ToolSchema{{
				Name: "mutate", QualifiedName: qualifiedName, Description: "change remote state",
				InputSchemaJSON:  `{"type":"object","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"]}`,
				DeclaredReadOnly: false,
			}},
		},
	}
	caller := &externalMCPRuntimeCaller{}
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(caller),
	)
	executor := workflowTool.NewExecutor(
		workflowTool.NewRegistry(),
		workflowTool.WithApprovalGate(NewPersistentApprovalGate(repo, time.Hour)),
	)
	svc := &AgentService{
		repo: repo, workflowToolExecutor: executor, externalMCPManager: manager,
	}

	suspended, err := svc.RunWorkflow(context.Background(), 42, workflow.ID.Hex(), `{"user_input":"approved value"}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuspended, suspended.Run.Status)
	require.NotEmpty(t, suspended.ResumeToken)
	require.Zero(t, caller.calls)

	approvals, total, err := svc.ListToolApprovals(context.Background(), 42, repository.ToolApprovalStatusPending, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, qualifiedName, approvals[0].ToolName)
	require.Equal(t, string(workflowTool.CategoryRisky), approvals[0].Category)
	require.Equal(t, "[REDACTED]", approvals[0].RedactedInputs["value"])
	require.NotContains(t, approvals[0].RedactedInputs, "tool_name")
	require.NotContains(t, approvals[0].RedactedInputs, "timeout_sec")

	approved, err := svc.DecideToolApproval(
		context.Background(), 42, approvals[0].ID, repository.ToolApprovalStatusApproved, "", approvals[0].Revision,
	)
	require.NoError(t, err)
	resumed, err := svc.ResumeWorkflowRun(
		context.Background(), 42, suspended.Run.ID.Hex(), approved.ID, suspended.ResumeToken, `{}`,
	)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuccess, resumed.Run.Status)
	require.Equal(t, 1, caller.calls)
	require.Equal(t, "mutate", caller.toolName)
	require.Equal(t, "approved value", caller.arguments["value"])
	require.NotContains(t, caller.arguments, "tool_name")
	require.NotContains(t, caller.arguments, "timeout_sec")
	require.NotContains(t, caller.arguments, "user_id")
	require.Equal(t, "found one result", resumed.Snapshot["mutate"]["content"])

	approvalOID, err := primitive.ObjectIDFromHex(approved.ID)
	require.NoError(t, err)
	storedApproval, err := repo.GetToolApproval(context.Background(), approvalOID, 42)
	require.NoError(t, err)
	require.Equal(t, repository.ToolApprovalStatusConsumed, storedApproval.Status)
}

func TestExternalMCPRiskyWorkflowDoesNotRetryAfterApprovedCallFails(t *testing.T) {
	const qualifiedName = "mcp_server.mutate"
	properties, err := json.Marshal(map[string]interface{}{
		"tool_name": qualifiedName,
		"mcp_arguments": map[string]interface{}{
			"value": "{{start.user_input}}",
		},
		"timeout_sec": 20,
	})
	require.NoError(t, err)
	dslJSON, err := json.Marshal(dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{
				ID:         "mutate",
				Type:       "tool",
				Properties: properties,
				Retry: &dsl.RetryPolicyDSL{
					MaxAttempts:      3,
					InitialBackoffMS: 1,
					MaxBackoffMS:     1,
					Multiplier:       1,
				},
			},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-mutate", Source: "start", Target: "mutate"},
			{ID: "mutate-end", Source: "mutate", Target: "end"},
		},
	})
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 42, Name: "external MCP no retry", DSLJSON: string(dslJSON),
	}
	repo := newApprovalWorkflowRepositoryFake(workflow)
	store := &externalMCPRuntimeStore{
		connection: externalmcp.Connection{
			ID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Transport: externalmcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
			AuthType: externalmcp.AuthNone, Status: externalmcp.ConnectionStatusActive,
			DiscoveryStatus: externalmcp.DiscoveryStatusReady, ActiveSnapshotID: "mcpsnap_1",
			ToolPolicies: []externalmcp.ToolPolicy{{
				SnapshotID: "mcpsnap_1", ToolName: "mutate", QualifiedName: qualifiedName,
				Category: externalmcp.ToolCategoryRisky, Enabled: true,
			}},
		},
		snapshot: externalmcp.ToolSchemaSnapshot{
			ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Tools: []externalmcp.ToolSchema{{
				Name: "mutate", QualifiedName: qualifiedName, Description: "change remote state",
				InputSchemaJSON:  `{"type":"object","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"]}`,
				DeclaredReadOnly: false,
			}},
		},
	}
	caller := &failingExternalMCPWorkflowCaller{}
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(caller),
	)
	executor := workflowTool.NewExecutor(
		workflowTool.NewRegistry(),
		workflowTool.WithApprovalGate(NewPersistentApprovalGate(repo, time.Hour)),
	)
	svc := &AgentService{
		repo: repo, workflowToolExecutor: executor, externalMCPManager: manager,
	}

	suspended, err := svc.RunWorkflow(context.Background(), 42, workflow.ID.Hex(), `{"user_input":"approved value"}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuspended, suspended.Run.Status)
	require.Zero(t, caller.calls)

	approvals, total, err := svc.ListToolApprovals(context.Background(), 42, repository.ToolApprovalStatusPending, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	approved, err := svc.DecideToolApproval(
		context.Background(), 42, approvals[0].ID, repository.ToolApprovalStatusApproved, "", approvals[0].Revision,
	)
	require.NoError(t, err)

	resumed, err := svc.ResumeWorkflowRun(
		context.Background(), 42, suspended.Run.ID.Hex(), approved.ID, suspended.ResumeToken, `{}`,
	)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusFailed, resumed.Run.Status)
	require.Contains(t, resumed.Run.ErrorMessage, "external MCP call failed")
	require.NotContains(t, resumed.Run.ErrorMessage, "timed out")
	require.Equal(t, 1, caller.calls, "risky external MCP calls must never be retried automatically")
}

func TestExternalMCPWriteWorkflowInjectsStableIdempotencyKeyAndRetriesSafely(t *testing.T) {
	const (
		qualifiedName = "mcp_server.create_record"
		keyArgument   = "idempotency_key"
	)
	properties, err := json.Marshal(map[string]interface{}{
		"tool_name": qualifiedName,
		"mcp_arguments": map[string]interface{}{
			"value":     "{{start.user_input}}",
			keyArgument: "untrusted-dsl-value",
		},
		"timeout_sec": 20,
	})
	require.NoError(t, err)
	dslJSON, err := json.Marshal(dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "create", Type: "tool", Properties: properties},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-create", Source: "start", Target: "create"},
			{ID: "create-end", Source: "create", Target: "end"},
		},
	})
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 42, Name: "external MCP idempotent write", DSLJSON: string(dslJSON),
	}
	repo := newApprovalWorkflowRepositoryFake(workflow)
	store := &externalMCPRuntimeStore{
		connection: externalmcp.Connection{
			ID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Transport: externalmcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/mcp",
			AuthType: externalmcp.AuthNone, Status: externalmcp.ConnectionStatusActive,
			DiscoveryStatus: externalmcp.DiscoveryStatusReady, ActiveSnapshotID: "mcpsnap_1",
			ToolPolicies: []externalmcp.ToolPolicy{{
				SnapshotID: "mcpsnap_1", ToolName: "create_record", QualifiedName: qualifiedName,
				Category: externalmcp.ToolCategoryWrite, Enabled: true,
			}},
		},
		snapshot: externalmcp.ToolSchemaSnapshot{
			ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Tools: []externalmcp.ToolSchema{{
				Name: "create_record", QualifiedName: qualifiedName, Description: "create remote state",
				InputSchemaJSON:    `{"type":"object","additionalProperties":false,"properties":{"value":{"type":"string"},"idempotency_key":{"type":"string"}},"required":["value","idempotency_key"]}`,
				DeclaredIdempotent: true, IdempotencyKeyArgument: keyArgument,
			}},
		},
	}
	caller := &retryingExternalMCPWriteCaller{}
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(caller),
	)
	executor := workflowTool.NewExecutor(
		workflowTool.NewRegistry(),
		workflowTool.WithApprovalGate(NewPersistentApprovalGate(repo, time.Hour)),
	)
	svc := &AgentService{repo: repo, workflowToolExecutor: executor, externalMCPManager: manager}

	suspended, err := svc.RunWorkflow(context.Background(), 42, workflow.ID.Hex(), `{"user_input":"created value"}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuspended, suspended.Run.Status)
	require.Zero(t, caller.calls)

	approvals, total, err := svc.ListToolApprovals(context.Background(), 42, repository.ToolApprovalStatusPending, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, string(workflowTool.CategoryWrite), approvals[0].Category)
	require.Equal(t, "[REDACTED]", approvals[0].RedactedInputs[keyArgument])
	approved, err := svc.DecideToolApproval(
		context.Background(), 42, approvals[0].ID, repository.ToolApprovalStatusApproved, "", approvals[0].Revision,
	)
	require.NoError(t, err)

	resumed, err := svc.ResumeWorkflowRun(
		context.Background(), 42, suspended.Run.ID.Hex(), approved.ID, suspended.ResumeToken, `{}`,
	)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuccess, resumed.Run.Status)
	require.Equal(t, 2, caller.calls)
	require.Len(t, caller.arguments, 2)
	firstKey, firstOK := caller.arguments[0][keyArgument].(string)
	secondKey, secondOK := caller.arguments[1][keyArgument].(string)
	require.True(t, firstOK)
	require.True(t, secondOK)
	require.Equal(t, firstKey, secondKey)
	require.NotEqual(t, "untrusted-dsl-value", firstKey)
	require.Contains(t, firstKey, "tc_mcp_")
	require.NotContains(t, firstKey, suspended.Run.ID.Hex())
	require.Equal(t, "created value", caller.arguments[1]["value"])
	require.NotContains(t, caller.arguments[1], "user_id")
	require.Equal(t, "created once", resumed.Snapshot["create"]["content"])
}
