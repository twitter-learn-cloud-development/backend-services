package service

import (
	"context"
	"errors"
	"fmt"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type externalMCPWorkflowNode struct {
	id       string
	toolName string
	service  *AgentService
}

func (node *externalMCPWorkflowNode) ID() string   { return node.id }
func (node *externalMCPWorkflowNode) Type() string { return "tool" }

func (node *externalMCPWorkflowNode) Execute(
	ctx context.Context,
	_ engine.StateView,
	inputs map[string]interface{},
) (map[string]interface{}, error) {
	if node == nil || node.service == nil || node.service.workflowToolExecutor == nil {
		return nil, errors.New("external MCP workflow executor is not configured")
	}
	userID, ok := guardrails.AuthenticatedUserID(ctx)
	if !ok || userID == 0 {
		return nil, workflowTool.ErrUnauthenticated
	}
	manager, err := node.service.externalMCP()
	if err != nil {
		return nil, err
	}
	definition, err := manager.GetGovernedTool(ctx, userID, node.toolName)
	if err != nil {
		return nil, fmt.Errorf("resolve external MCP workflow tool %s: %w", node.toolName, err)
	}

	metadata := workflowTool.ExecutionMetadataFromContext(ctx)
	idempotencyKey := toolIdempotencyKey(metadata.RunID, node.id, node.toolName)
	toolInputs := externalMCPWorkflowInputs(inputs)
	if definition.Policy.Category == externalmcp.ToolCategoryWrite {
		remoteKey, keyErr := externalmcp.DeriveRemoteIdempotencyKey(idempotencyKey)
		if keyErr != nil {
			return nil, keyErr
		}
		// This property is platform-owned. Any value supplied by the DSL,
		// model or user is overwritten before validation and approval.
		toolInputs[definition.Schema.IdempotencyKeyArgument] = remoteKey
	}
	remoteArguments := cloneExternalMCPArguments(toolInputs)
	outputs, err := node.service.workflowToolExecutor.ExecuteAdHoc(
		ctx,
		workflowTool.ExecutionRequest{
			ToolName:       node.toolName,
			Inputs:         toolInputs,
			Identity:       workflowTool.CallerIdentity{UserID: userID},
			RunID:          metadata.RunID,
			StepID:         node.id,
			Source:         workflowTool.SourceWorkflow,
			IdempotencyKey: idempotencyKey,
		},
		externalMCPToolSpec(definition),
		workflowTool.HandlerFunc(func(handlerCtx context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
			// Resolve again immediately before the side effect. Policy, tenant,
			// connection status and active schema changes therefore fail closed
			// even when they happen while the workflow is awaiting approval.
			result, callErr := manager.CallGovernedTool(
				handlerCtx,
				userID,
				node.toolName,
				cloneExternalMCPArguments(remoteArguments),
				idempotencyKey,
			)
			if callErr != nil {
				return nil, callErr
			}
			return externalMCPResultOutputs(result), nil
		}),
	)
	if err == nil {
		node.service.recordExternalMCPUse(ctx, definition, metadata.RunID)
		return outputs, nil
	}
	var pending *workflowTool.ApprovalPendingError
	if errors.As(err, &pending) {
		return nil, engine.NewSuspensionErrorWithCause(node.id, "external MCP tool approval required", "", map[string]interface{}{
			"approval_request_id": pending.ApprovalID,
			"tool_name":           node.toolName,
			"idempotency_key":     idempotencyKey,
		}, err)
	}
	if definition.Policy.Category == externalmcp.ToolCategoryRisky {
		return nil, &nonRetryableExternalMCPError{cause: err}
	}
	return nil, err
}

func externalMCPWorkflowInputs(inputs map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{}, len(inputs))
	for key, value := range inputs {
		switch key {
		case "tool_name", "timeout_sec", "external_mcp":
			continue
		case "mcp_arguments":
			arguments, ok := value.(map[string]interface{})
			if !ok {
				cleaned[key] = value
				continue
			}
			for argument, argumentValue := range arguments {
				cleaned[argument] = argumentValue
			}
		default:
			cleaned[key] = value
		}
	}
	return cleaned
}

type nonRetryableExternalMCPError struct {
	cause error
}

func (err *nonRetryableExternalMCPError) Error() string {
	if err == nil || err.cause == nil {
		return "external MCP side effect failed with an unknown outcome"
	}
	return err.cause.Error()
}

func (err *nonRetryableExternalMCPError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (*nonRetryableExternalMCPError) IsRetryable() bool { return false }
