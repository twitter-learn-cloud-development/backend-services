package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

func TestExternalMCPReadGoalUsesCurrentTenantBinding(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	store := controlledExternalMCPStore(qualifiedName, externalmcp.ToolCategoryRead)
	store.snapshot.Tools[0].DeclaredReadOnly = true
	caller := &externalMCPRuntimeCaller{}
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true), externalmcp.WithCaller(caller),
	)
	audit := &externalMCPAuditRecorder{}
	service := &AgentService{
		externalMCPEnabled: true,
		externalMCPManager: manager,
		workflowToolExecutor: workflowTool.NewExecutor(
			workflowTool.NewRegistry(), workflowTool.WithAuditSink(audit),
		),
	}
	environment, err := service.newExternalMCPEnvironment(41)
	if err != nil {
		t.Fatal(err)
	}
	task := controlledExternalMCPTask(qualifiedName, agentEvidence.ExternalMCPReadObservedCriterion)
	tools, err := environment.Tools(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	model := &controlledModel{responses: []agentRuntime.ModelResponse{
		{Actions: []agentRuntime.Action{{
			ID: "read-1", Type: agentRuntime.ActionToolCall, Name: qualifiedName,
			Arguments: json.RawMessage(`{"query":"golang"}`),
		}}},
		{Message: agentRuntime.Message{Content: "read completed"}},
	}}
	runner := agentRuntime.NewVerifiedRunner(
		agentRuntime.NewReActRunner(model, &mcpRuntimeToolExecutor{service: service}, nil),
		agentEvidence.ExternalMCPReadGoalVerifier{},
		agentEvidence.ExternalMCPReadGoalCollector{},
	)
	result, err := runner.Run(context.Background(), agentRuntime.VerifiedRunRequest{
		Task: task,
		Run: agentRuntime.RunRequest{
			Context: agentRuntime.RunContext{
				RunID: "run-mcp-read", UserID: 41,
				Budget: agentRuntime.Budget{MaxSteps: 3},
			},
			Model: "controlled-model", Tools: tools,
			Messages:          []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "read connector"}},
			InitialToolChoice: agentRuntime.ToolChoiceRequired,
		},
		Environment: environment,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != agentRuntime.GoalRunVerified || !result.Verification.Passed() ||
		len(result.Evidence.Items) != 1 {
		t.Fatalf("verified result = %+v", result)
	}
	if caller.calls != 1 || audit.last().Decision != "succeeded" {
		t.Fatalf("remote calls/audit = %d/%+v", caller.calls, audit.last())
	}

	otherEnvironment, err := service.newExternalMCPEnvironment(42)
	if err != nil {
		t.Fatal(err)
	}

	otherTools, err := otherEnvironment.Tools(context.Background(), task)
	if err != nil || len(otherTools) != 0 {
		t.Fatalf("cross-tenant tools/error = %+v/%v", otherTools, err)
	}
}

func TestExternalMCPWriteApprovalThenRevocationFailsBeforeRemoteCall(t *testing.T) {
	const qualifiedName = "mcp_server.create_record"
	store := controlledExternalMCPStore(qualifiedName, externalmcp.ToolCategoryWrite)
	store.snapshot.Tools[0].DeclaredIdempotent = true
	store.snapshot.Tools[0].IdempotencyKeyArgument = "request_id"
	store.snapshot.Tools[0].InputSchemaJSON = `{"type":"object","additionalProperties":false,"properties":{"value":{"type":"string"},"request_id":{"type":"string"}},"required":["value","request_id"]}`
	caller := &externalMCPRuntimeCaller{}
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true), externalmcp.WithCaller(caller),
	)
	approval := &controlledApprovalGate{}
	audit := &externalMCPAuditRecorder{}
	service := &AgentService{
		externalMCPEnabled: true,
		externalMCPManager: manager,
		workflowToolExecutor: workflowTool.NewExecutor(
			workflowTool.NewRegistry(),
			workflowTool.WithApprovalGate(approval),
			workflowTool.WithAuditSink(audit),
		),
	}
	environment, err := service.newExternalMCPEnvironment(41)
	if err != nil {
		t.Fatal(err)
	}
	task := controlledExternalMCPTask(qualifiedName, "external_mcp_write_blocked_after_revocation")
	tools, err := environment.Tools(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	model := &controlledModel{responses: []agentRuntime.ModelResponse{{Actions: []agentRuntime.Action{{
		ID: "write-1", Type: agentRuntime.ActionToolCall, Name: qualifiedName,
		Arguments: json.RawMessage(`{"value":"update","request_id":"model-forged"}`),
	}}}}}
	runner := agentRuntime.NewVerifiedRunner(
		agentRuntime.NewReActRunner(model, &mcpRuntimeToolExecutor{service: service}, nil),
		agentRuntime.RequiredEvidenceVerifier{}, nil,
	)
	request := agentRuntime.VerifiedRunRequest{
		Task: task,
		Run: agentRuntime.RunRequest{
			Context: agentRuntime.RunContext{
				RunID: "run-mcp-write", UserID: 41,
				Budget: agentRuntime.Budget{MaxSteps: 3},
			},
			Model: "controlled-model", Tools: tools,
			Messages:          []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "write connector"}},
			InitialToolChoice: agentRuntime.ToolChoiceRequired,
		},
		Environment: environment,
	}
	suspended, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if suspended.Status != agentRuntime.GoalRunSuspended || suspended.Checkpoint == nil || caller.calls != 0 {
		t.Fatalf("suspended result/calls = %+v/%d", suspended, caller.calls)
	}
	if event := audit.last(); event.Decision != "approval_required" ||
		event.ErrorCode != workflowTool.CodeApprovalRequired {
		t.Fatalf("approval audit = %+v", event)
	}

	approval.approve()
	store.connection.Status = externalmcp.ConnectionStatusRevoked
	store.connection.Revision++
	_, err = runner.Resume(context.Background(), agentRuntime.VerifiedResumeRequest{
		Checkpoint: *suspended.Checkpoint, ApprovalID: suspended.Run.ApprovalID,
		Tools: tools, Environment: environment,
	})
	if err == nil || (!errors.Is(err, externalmcp.ErrToolDisabled) &&
		!strings.Contains(err.Error(), "task tool is unavailable")) {
		t.Fatalf("Resume() revoked error = %v", err)
	}
	if caller.calls != 0 {
		t.Fatalf("remote calls after revocation = %d", caller.calls)
	}
	if event := audit.last(); event.Decision != "approval_required" {
		t.Fatalf("revocation produced an unexpected execution audit = %+v", event)
	}
}

func controlledExternalMCPStore(
	qualifiedName string,
	category string,
) *externalMCPRuntimeStore {
	toolName := strings.TrimPrefix(qualifiedName, "mcp_server.")
	return &externalMCPRuntimeStore{
		connection: externalmcp.Connection{
			ID: "mcpconn_1", UserID: 41, ServerID: "mcp_server", Revision: 5,
			Transport: externalmcp.TransportStreamableHTTP,
			Endpoint:  "https://mcp.example.com/mcp", AuthType: externalmcp.AuthNone,
			Status:           externalmcp.ConnectionStatusActive,
			DiscoveryStatus:  externalmcp.DiscoveryStatusReady,
			ActiveSnapshotID: "mcpsnap_1",
			ToolPolicies: []externalmcp.ToolPolicy{{
				SnapshotID: "mcpsnap_1", ToolName: toolName, QualifiedName: qualifiedName,
				Category: category, Enabled: true,
			}},
		},
		snapshot: externalmcp.ToolSchemaSnapshot{
			ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 41,
			ServerID: "mcp_server", Version: 3,
			SchemaHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Tools: []externalmcp.ToolSchema{{
				Name: toolName, QualifiedName: qualifiedName,
				InputSchemaJSON: `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string"}},"required":["query"]}`,
			}},
		},
	}
}

func controlledExternalMCPTask(toolName, criterionID string) agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "controlled-external-mcp", Goal: "execute only the authorized external MCP tool",
		AllowedTools: []string{toolName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: criterionID, Description: "the governed MCP outcome is proven", Required: true,
		}},
	}
}
