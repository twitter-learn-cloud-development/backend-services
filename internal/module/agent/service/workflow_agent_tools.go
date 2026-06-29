package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"

	"twitter-clone/internal/module/agent/workflow/guardrails"
)

var workflowReadOnlyMCPTools = map[string]struct{}{
	"search_tweets_by_semantic": {},
	"hybrid_search_tweets":      {},
	"get_user_tweets":           {},
	"get_tweets_by_ids":         {},
	"search_users":              {},
}

// ExecuteWorkflowMCPTool invokes an allow-listed MCP capability through the
// same authenticated, pooled client used by the built-in Agent modes.
func (s *AgentService) ExecuteWorkflowMCPTool(ctx context.Context, toolName string, inputs map[string]interface{}) (map[string]interface{}, error) {
	if _, ok := workflowReadOnlyMCPTools[toolName]; !ok {
		return nil, fmt.Errorf("MCP tool %q is not available to custom workflows", toolName)
	}
	userID, ok := guardrails.AuthenticatedUserID(ctx)
	if !ok {
		return nil, errors.New("authenticated workflow user is missing")
	}

	mcpClient, _, err := s.getOrInitMCPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialize MCP client: %w", err)
	}
	result, err := s.callToolWithAuth(ctx, mcpClient, userID, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: inputs,
		},
	})
	if err != nil {
		s.resetMCPClient()
		return nil, fmt.Errorf("call MCP tool %s: %w", toolName, err)
	}
	if result.IsError {
		return nil, fmt.Errorf("MCP tool %s failed: %s", toolName, extractTextFromToolResult(result))
	}
	return map[string]interface{}{"result": extractTextFromToolResult(result)}, nil
}

// ExecuteWorkflowStrategy runs a bounded tool-using strategy. Write tools are
// intentionally excluded; state-changing operations remain explicit DAG nodes.
func (s *AgentService) ExecuteWorkflowStrategy(ctx context.Context, strategy string, inputs map[string]interface{}) (map[string]interface{}, error) {
	userID, ok := guardrails.AuthenticatedUserID(ctx)
	if !ok {
		return nil, errors.New("authenticated workflow user is missing")
	}

	objective := workflowStringInput(inputs, "objective")
	if objective == "" {
		objective = workflowStringInput(inputs, "prompt")
	}
	if objective == "" {
		return nil, errors.New("strategy objective is required")
	}

	mcpClient, tools, err := s.getOrInitMCPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialize MCP client: %w", err)
	}
	allowedNames := workflowAllowedTools(inputs)
	filteredTools := make([]mcp.Tool, 0, len(tools))
	for _, candidate := range tools {
		if _, allowed := allowedNames[candidate.Name]; allowed {
			if _, readOnly := workflowReadOnlyMCPTools[candidate.Name]; readOnly {
				filteredTools = append(filteredTools, candidate)
			}
		}
	}

	systemPrompt := workflowStrategyPrompt(strategy)
	if custom := workflowStringInput(inputs, "system_prompt"); custom != "" {
		systemPrompt += "\n\n用户补充约束：\n" + custom
	}
	userPrompt := objective
	if plan := workflowStringInput(inputs, "plan"); plan != "" {
		userPrompt = fmt.Sprintf("目标：\n%s\n\n待执行计划：\n%s", objective, plan)
	}

	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userPrompt},
	}
	openAITools := mcpToolsToOpenAI(filteredTools)
	maxIterations := workflowIntInput(inputs, "max_iterations", 5)
	if maxIterations < 1 {
		maxIterations = 1
	}
	if maxIterations > 8 {
		maxIterations = 8
	}

	toolTrace := make([]map[string]interface{}, 0)
	for iteration := 1; iteration <= maxIterations; iteration++ {
		resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:     firstWorkflowString(workflowStringInput(inputs, "model"), s.chatModel),
			Messages:  messages,
			Tools:     openAITools,
			MaxTokens: workflowIntInput(inputs, "max_tokens", 2048),
		})
		if err != nil {
			return nil, fmt.Errorf("strategy LLM call failed: %w", err)
		}
		if len(resp.Choices) == 0 {
			return nil, errors.New("strategy LLM returned no choices")
		}

		choice := resp.Choices[0]
		if choice.FinishReason != openai.FinishReasonToolCalls || len(choice.Message.ToolCalls) == 0 {
			return map[string]interface{}{
				"text":       choice.Message.Content,
				"iterations": iteration,
				"tool_trace": toolTrace,
			}, nil
		}

		messages = append(messages, choice.Message)
		for _, toolCall := range choice.Message.ToolCalls {
			if _, allowed := allowedNames[toolCall.Function.Name]; !allowed {
				return nil, fmt.Errorf("strategy requested disallowed tool %q", toolCall.Function.Name)
			}
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("decode tool %s arguments: %w", toolCall.Function.Name, err)
			}

			result, err := s.callToolWithAuth(ctx, mcpClient, userID, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      toolCall.Function.Name,
					Arguments: args,
				},
			})
			if err != nil {
				s.resetMCPClient()
				return nil, fmt.Errorf("strategy tool %s failed: %w", toolCall.Function.Name, err)
			}
			resultText := extractTextFromToolResult(result)
			toolTrace = append(toolTrace, map[string]interface{}{
				"iteration": iteration,
				"tool":      toolCall.Function.Name,
				"arguments": args,
				"is_error":  result.IsError,
			})
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    resultText,
				ToolCallID: toolCall.ID,
			})
		}
	}

	return nil, fmt.Errorf("strategy exceeded maximum iterations (%d)", maxIterations)
}

func workflowAllowedTools(inputs map[string]interface{}) map[string]struct{} {
	raw := workflowStringInput(inputs, "allowed_tools")
	if raw == "" {
		return map[string]struct{}{
			"hybrid_search_tweets": {},
			"search_users":         {},
			"get_user_tweets":      {},
		}
	}
	allowed := make(map[string]struct{})
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if _, ok := workflowReadOnlyMCPTools[name]; ok {
			allowed[name] = struct{}{}
		}
	}
	return allowed
}

func workflowStrategyPrompt(strategy string) string {
	if strings.EqualFold(strategy, "PlanExecutor") {
		return `你是 Plan-Execute 执行器。严格按给定计划逐步执行；需要真实平台数据时调用工具；每次工具结果都必须验证后再继续。最后输出已完成步骤、关键证据、未完成项和最终答案。禁止声称执行了未实际调用的工具。`
	}
	return `你是一个受限 ReAct 智能体。围绕目标循环执行 Thought -> Action -> Observation，但不要向用户暴露冗长思维过程。需要真实平台数据时调用工具，观察结果后再决定下一步。最终只输出结论、证据和必要的后续建议。禁止编造工具结果，禁止执行任何写操作。`
}

func workflowStringInput(inputs map[string]interface{}, key string) string {
	value, _ := inputs[key].(string)
	return strings.TrimSpace(value)
}

func workflowIntInput(inputs map[string]interface{}, key string, fallback int) int {
	switch value := inputs[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return fallback
}

func firstWorkflowString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
