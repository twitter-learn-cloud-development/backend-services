package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
)

type mcpRuntimeToolExecutor struct {
	service *AgentService
}

// RuntimeToolCatalog decouples Runtime entry points from MCP discovery. Tool
// execution remains behind ToolExecutor and happens only when the model calls.
type RuntimeToolCatalog interface {
	ListTools(ctx context.Context) ([]agentRuntime.ToolDefinition, error)
}

type mcpRuntimeToolCatalog struct {
	service *AgentService
}

func (c *mcpRuntimeToolCatalog) ListTools(ctx context.Context) ([]agentRuntime.ToolDefinition, error) {
	if c == nil || c.service == nil {
		return nil, fmt.Errorf("MCP runtime tool catalog is not configured")
	}
	_, tools, err := c.service.getOrInitMCPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialize MCP client: %w", err)
	}
	return mcpToolsToRuntime(tools), nil
}

func (e *mcpRuntimeToolExecutor) Execute(ctx context.Context, call agentRuntime.ToolCall) (agentRuntime.ToolResult, error) {
	if e == nil || e.service == nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("MCP runtime tool executor is not configured")
	}
	var arguments map[string]any
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("decode MCP tool %s arguments: %w", call.Name, err)
	}
	if isWorkflowRuntimeToolName(call.Name) {
		return e.executeWorkflowTool(ctx, call, arguments)
	}
	if externalmcp.IsQualifiedToolName(call.Name) {
		return e.executeExternalMCP(ctx, call, arguments)
	}

	mcpClient, _, err := e.service.getOrInitMCPClient(ctx)
	if err != nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("initialize MCP client: %w", err)
	}
	governedRunID := runtimeTraceRunID(call.RunContext)
	governedStepID := runtimeRoleScopedID(call.RunContext.RoleID, call.ActionID)
	result, err := e.service.executeMCPToolGoverned(ctx, mcpClient, workflowTool.ExecutionRequest{
		ToolName:       call.Name,
		Inputs:         arguments,
		Identity:       workflowTool.CallerIdentity{UserID: call.RunContext.UserID},
		RunID:          governedRunID,
		StepID:         governedStepID,
		Source:         workflowTool.SourceRuntime,
		IdempotencyKey: toolIdempotencyKey(governedRunID, governedStepID, call.Name),
	}, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      call.Name,
			Arguments: arguments,
		},
	})
	if err != nil {
		e.service.resetMCPClient()
		if errors.Is(err, workflowTool.ErrApprovalRequired) {
			return agentRuntime.ToolResult{}, &agentRuntime.RunError{
				Code: agentRuntime.ErrorApprovalRequired, ActionID: call.ActionID,
				ApprovalID: workflowApprovalID(err),
				Message:    fmt.Sprintf("tool %q requires approval", call.Name), Cause: err,
			}
		}
		return agentRuntime.ToolResult{}, fmt.Errorf("call MCP tool %s: %w", call.Name, err)
	}
	content := extractTextFromToolResult(result)
	if result.IsError {
		return agentRuntime.ToolResult{}, fmt.Errorf("MCP tool %s failed: %s", call.Name, content)
	}
	structuredContent, err := encodeMCPStructuredContent(result.StructuredContent)
	if err != nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("encode MCP tool %s structured content: %w", call.Name, err)
	}
	return agentRuntime.ToolResult{
		Content:           content,
		StructuredContent: structuredContent,
	}, nil
}

// ExecuteApprovalGated marks this adapter as the only Runtime path allowed to
// cross the persistent ToolExecutor approval boundary. Authorization remains
// inside ToolExecutor; this method does not itself grant approval.
func (e *mcpRuntimeToolExecutor) ExecuteApprovalGated(ctx context.Context, call agentRuntime.ToolCall) (agentRuntime.ToolResult, error) {
	return e.Execute(ctx, call)
}

func workflowApprovalID(err error) string {
	var pending *workflowTool.ApprovalPendingError
	if !errors.As(err, &pending) {
		return ""
	}
	return strings.TrimSpace(pending.ApprovalID)
}

func encodeMCPStructuredContent(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if string(encoded) == "null" {
		return nil, nil
	}
	return json.RawMessage(encoded), nil
}

func (s *AgentService) executeMCPToolGoverned(
	ctx context.Context,
	mcpClient *client.Client,
	execution workflowTool.ExecutionRequest,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	if s == nil || s.workflowToolExecutor == nil {
		return nil, errors.New("workflow tool executor is not configured")
	}
	spec := s.mcpToolSpec(execution.ToolName)
	var rawResult *mcp.CallToolResult
	_, err := s.workflowToolExecutor.ExecuteAdHoc(ctx, execution, spec, workflowTool.HandlerFunc(
		func(handlerCtx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
			request.Params.Arguments = inputs
			result, callErr := s.callToolWithAuth(handlerCtx, mcpClient, execution.Identity.UserID, request)
			if callErr != nil {
				return nil, callErr
			}
			rawResult = result
			content := extractTextFromToolResult(result)
			if result.IsError {
				return nil, fmt.Errorf("MCP tool %s failed: %s", execution.ToolName, content)
			}
			return map[string]interface{}{"result": content}, nil
		},
	))
	if err != nil {
		return nil, err
	}
	if rawResult == nil {
		return nil, fmt.Errorf("MCP tool %s returned no result", execution.ToolName)
	}
	return rawResult, nil
}

func (s *AgentService) mcpToolSpec(name string) workflowTool.ToolSpec {
	schema := json.RawMessage(`{"type":"object"}`)
	description := "MCP tool"
	if s != nil {
		s.mcpMu.RLock()
		for _, definition := range s.mcpTools {
			if definition.Name != name {
				continue
			}
			description = definition.Description
			if encoded, err := json.Marshal(definition.InputSchema); err == nil {
				schema = encoded
			}
			break
		}
		s.mcpMu.RUnlock()
	}

	category := workflowTool.CategoryRisky
	retry := workflowTool.RetryPolicy{MaxAttempts: 1}
	approval := workflowTool.ApprovalRequired
	idempotency := workflowTool.IdempotencyPolicy{}
	sensitiveFields := []string{"api_key", "authorization", "cookie", "content"}
	switch {
	case isReadOnlyMCPTool(name):
		category = workflowTool.CategoryRead
		retry = workflowTool.RetryPolicy{MaxAttempts: 2, InitialBackoff: 100 * time.Millisecond, MaxBackoff: time.Second}
		approval = workflowTool.ApprovalNever
		if name == "web_search" {
			sensitiveFields = append(sensitiveFields, "query")
		}
		if name == "page_read" {
			sensitiveFields = append(sensitiveFields, "url")
		}
	case strings.EqualFold(name, "create_tweet"):
		category = workflowTool.CategoryWrite
		idempotency.Required = true
	}
	return workflowTool.ToolSpec{
		Name: name, Description: description, InputSchema: schema,
		Category: category, Permission: workflowTool.PermissionAuthenticated,
		Timeout: 20 * time.Second, Retry: retry, Idempotency: idempotency,
		Approval:        approval,
		SensitiveFields: sensitiveFields,
	}
}

func toolIdempotencyKey(runID, stepID, toolName string) string {
	if runID == "" || stepID == "" || toolName == "" {
		return ""
	}
	return runID + ":" + stepID + ":" + toolName
}

func mcpToolsToRuntime(tools []mcp.Tool) []agentRuntime.ToolDefinition {
	definitions := make([]agentRuntime.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			continue
		}
		category := agentRuntime.ToolCategoryRisky
		switch {
		case isReadOnlyMCPTool(tool.Name):
			category = agentRuntime.ToolCategoryRead
		case tool.Name == "create_tweet":
			category = agentRuntime.ToolCategoryWrite
		}
		definitions = append(definitions, agentRuntime.ToolDefinition{
			Name:             tool.Name,
			Description:      tool.Description,
			InputSchema:      schema,
			Category:         category,
			RequiresApproval: category != agentRuntime.ToolCategoryRead,
		})
	}
	return definitions
}

func openAIMessagesToRuntime(messages []openai.ChatCompletionMessage) []agentRuntime.Message {
	converted := make([]agentRuntime.Message, 0, len(messages))
	for _, message := range messages {
		role, ok := openAIRoleToRuntime(message.Role)
		if !ok {
			continue
		}
		converted = append(converted, agentRuntime.Message{
			Role:       role,
			Content:    message.Content,
			Name:       message.Name,
			ToolCallID: message.ToolCallID,
		})
	}
	return converted
}

func openAIRoleToRuntime(role string) (agentRuntime.MessageRole, bool) {
	switch role {
	case openai.ChatMessageRoleSystem:
		return agentRuntime.RoleSystem, true
	case openai.ChatMessageRoleUser:
		return agentRuntime.RoleUser, true
	case openai.ChatMessageRoleAssistant:
		return agentRuntime.RoleAssistant, true
	case openai.ChatMessageRoleTool:
		return agentRuntime.RoleTool, true
	default:
		return "", false
	}
}

func runtimeResultMetadata(result agentRuntime.RunResult, profileID, profileVersion, promptVersion string) map[string]any {
	metadata := map[string]any{
		"runtime_version":               "v2",
		"runtime_run_id":                result.Context.RunID,
		"runtime_mode":                  string(result.Context.Mode),
		"runtime_steps":                 len(result.Steps),
		"runtime_tokens":                result.Usage.TotalTokens,
		"runtime_tokens_estimated":      result.Usage.Estimated,
		"runtime_estimated_cost_micros": result.Usage.EstimatedCostMicros,
		"runtime_cost_estimated":        result.Usage.CostEstimated,
		"runtime_pricing_version":       result.Usage.PricingVersion,
		"runtime_status":                string(result.Status),
	}
	if profileID != "" {
		metadata["agent_profile"] = profileID
	}
	if profileVersion != "" {
		metadata["agent_profile_version"] = profileVersion
	}
	if promptVersion != "" {
		metadata["prompt_version"] = promptVersion
	}
	return metadata
}
