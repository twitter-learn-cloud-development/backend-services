package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

func (e *mcpRuntimeToolExecutor) executeExternalMCP(
	ctx context.Context,
	call agentRuntime.ToolCall,
	arguments map[string]interface{},
) (agentRuntime.ToolResult, error) {
	if e == nil || e.service == nil || e.service.workflowToolExecutor == nil {
		return agentRuntime.ToolResult{}, errors.New("external MCP runtime executor is not configured")
	}
	manager, err := e.service.externalMCP()
	if err != nil {
		return agentRuntime.ToolResult{}, err
	}
	definition, err := manager.GetGovernedTool(ctx, call.RunContext.UserID, call.Name)
	if err != nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("resolve external MCP tool %s: %w", call.Name, err)
	}
	executionKey := toolIdempotencyKey(call.RunContext.RunID, call.ActionID, call.Name)
	toolInputs := cloneExternalMCPArguments(arguments)
	if definition.Policy.Category == externalmcp.ToolCategoryWrite {
		remoteKey, keyErr := externalmcp.DeriveRemoteIdempotencyKey(executionKey)
		if keyErr != nil {
			return agentRuntime.ToolResult{}, fmt.Errorf("derive external MCP idempotency key: %w", keyErr)
		}
		toolInputs[definition.Schema.IdempotencyKeyArgument] = remoteKey
	}
	spec := externalMCPToolSpec(definition)
	outputs, err := e.service.workflowToolExecutor.ExecuteAdHoc(
		ctx,
		workflowTool.ExecutionRequest{
			ToolName:       call.Name,
			Inputs:         toolInputs,
			Identity:       workflowTool.CallerIdentity{UserID: call.RunContext.UserID},
			RunID:          call.RunContext.RunID,
			StepID:         call.ActionID,
			Source:         workflowTool.SourceRuntime,
			IdempotencyKey: executionKey,
		},
		spec,
		workflowTool.HandlerFunc(func(handlerCtx context.Context, governedInputs map[string]interface{}) (map[string]interface{}, error) {
			result, callErr := manager.CallGovernedTool(
				handlerCtx,
				call.RunContext.UserID,
				call.Name,
				cloneExternalMCPArguments(governedInputs),
				executionKey,
			)
			if callErr != nil {
				return nil, callErr
			}
			return externalMCPResultOutputs(result), nil
		}),
	)
	if err != nil {
		if errors.Is(err, workflowTool.ErrApprovalRequired) {
			return agentRuntime.ToolResult{}, &agentRuntime.RunError{
				Code:       agentRuntime.ErrorApprovalRequired,
				ActionID:   call.ActionID,
				ApprovalID: workflowApprovalID(err),
				Message:    fmt.Sprintf("tool %q requires approval", call.Name),
				Cause:      err,
			}
		}
		return agentRuntime.ToolResult{}, fmt.Errorf("execute external MCP tool %s: %w", call.Name, err)
	}
	e.service.recordExternalMCPUse(ctx, definition, call.RunContext.RunID)
	content, _ := outputs["content"].(string)
	structured, err := encodeMCPStructuredContent(outputs["structured_content"])
	if err != nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("encode external MCP tool %s structured content: %w", call.Name, err)
	}
	return agentRuntime.ToolResult{Content: content, StructuredContent: structured}, nil
}

func externalMCPToolSpec(definition externalmcp.ExecutableTool) workflowTool.ToolSpec {
	category := workflowTool.CategoryRead
	approval := workflowTool.ApprovalNever
	idempotency := workflowTool.IdempotencyPolicy{}
	retry := workflowTool.RetryPolicy{
		MaxAttempts:    2,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     time.Second,
	}
	switch definition.Policy.Category {
	case externalmcp.ToolCategoryRisky:
		category = workflowTool.CategoryRisky
		approval = workflowTool.ApprovalRequired
		retry.MaxAttempts = 1
	case externalmcp.ToolCategoryWrite:
		category = workflowTool.CategoryWrite
		approval = workflowTool.ApprovalRequired
		idempotency.Required = true
	}
	return workflowTool.ToolSpec{
		Name:            definition.Schema.QualifiedName,
		Description:     definition.Schema.Description,
		InputSchema:     json.RawMessage(definition.Schema.InputSchemaJSON),
		Category:        category,
		Permission:      workflowTool.PermissionAuthenticated,
		Timeout:         20 * time.Second,
		Retry:           retry,
		Idempotency:     idempotency,
		Approval:        approval,
		SensitiveFields: externalMCPInputFields(definition.Schema.InputSchemaJSON),
	}
}

func externalMCPResultOutputs(result *mcp.CallToolResult) map[string]interface{} {
	output := map[string]interface{}{"content": extractTextFromToolResult(result)}
	if result != nil && result.StructuredContent != nil {
		output["structured_content"] = result.StructuredContent
	}
	return output
}

// cloneExternalMCPArguments keeps platform-only identity and governance fields
// out of third-party requests. ToolExecutor still receives the authenticated
// identity for policy, approval, audit and tenant isolation, but the remote
// server receives only the schema-validated arguments supplied to the tool.
func cloneExternalMCPArguments(arguments map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(arguments))
	for key, value := range arguments {
		cloned[key] = cloneExternalMCPValue(value)
	}
	return cloned
}

func cloneExternalMCPValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return cloneExternalMCPArguments(typed)
	case []interface{}:
		cloned := make([]interface{}, len(typed))
		for index, item := range typed {
			cloned[index] = cloneExternalMCPValue(item)
		}
		return cloned
	default:
		return value
	}
}

func externalMCPInputFields(schemaJSON string) []string {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return nil
	}
	fields := make([]string, 0, len(schema.Properties))
	for field := range schema.Properties {
		field = strings.TrimSpace(field)
		if field != "" {
			fields = append(fields, field)
		}
	}
	sort.Strings(fields)
	return fields
}

func externalMCPRuntimeTools(tools []externalmcp.ExecutableTool) []agentRuntime.ToolDefinition {
	definitions := make([]agentRuntime.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		category := agentRuntime.ToolCategoryRisky
		requiresApproval := true
		switch tool.Policy.Category {
		case externalmcp.ToolCategoryRead:
			category = agentRuntime.ToolCategoryRead
			requiresApproval = false
		case externalmcp.ToolCategoryWrite:
			category = agentRuntime.ToolCategoryWrite
		}
		definitions = append(definitions, agentRuntime.ToolDefinition{
			Name:             tool.Schema.QualifiedName,
			Description:      tool.Schema.Description,
			InputSchema:      json.RawMessage(tool.Schema.InputSchemaJSON),
			Category:         category,
			RequiresApproval: requiresApproval,
		})
	}
	return definitions
}
