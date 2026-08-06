package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
	"go.mongodb.org/mongo-driver/bson/primitive"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/guardrails"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

var workflowReadOnlyMCPTools = readOnlyMCPToolSet()

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
	if s.RuntimeV2Enabled(agentRuntime.ModeWorkflow) {
		return s.executeWorkflowStrategyRuntime(ctx, strategy, inputs)
	}
	return s.executeWorkflowStrategyLegacy(ctx, strategy, inputs)
}

func (s *AgentService) executeWorkflowStrategyRuntime(ctx context.Context, strategy string, inputs map[string]interface{}) (map[string]interface{}, error) {
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
	if s.runtimeRunner == nil {
		return nil, errors.New("agent runtime runner is not configured")
	}
	if s.runtimeTools == nil {
		return nil, errors.New("agent runtime tool catalog is not configured")
	}

	tools, err := s.runtimeTools.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runtime tools failed: %w", err)
	}
	profile, err := s.resolveAgentProfile(ctx, workflowAgentProfileID(strategy), userID)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow profile failed: %w", err)
	}
	tools = filterRuntimeToolsByName(profile.FilterTools(tools), workflowAllowedTools(inputs))

	systemPrompt := profile.Prompt.SystemPrompt
	if custom := workflowStringInput(inputs, "system_prompt"); custom != "" {
		systemPrompt += "\n\n用户补充约束：\n" + custom
	}
	userPrompt := objective
	if plan := workflowStringInput(inputs, "plan"); plan != "" {
		userPrompt = fmt.Sprintf("目标：\n%s\n\n待执行计划：\n%s", objective, plan)
	}
	budget := profile.Budget
	budget.MaxSteps = workflowMaxIterations(inputs)
	budget.MaxOutputTokens = workflowIntInput(inputs, "max_tokens", 2048)

	result, err := s.runtimeRunner.Run(ctx, agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID: primitive.NewObjectID().Hex(), UserID: userID,
			Mode: agentRuntime.ModeWorkflow, Budget: budget,
			WorkflowID:          workflowTool.ExecutionMetadataFromContext(ctx).WorkflowID,
			AgentProfileID:      profile.ID,
			AgentProfileVersion: profile.Version,
			PromptTemplateID:    profile.Prompt.ID, PromptTemplateVersion: profile.Prompt.Version,
		},
		Model: firstWorkflowString(workflowStringInput(inputs, "model"), s.chatModel),
		Messages: []agentRuntime.Message{
			{Role: agentRuntime.RoleSystem, Content: systemPrompt},
			{Role: agentRuntime.RoleUser, Content: userPrompt},
		},
		Tools: tools,
	})
	s.recordRuntimeResult(ctx, result, err, profile.ID)
	if err != nil {
		return nil, fmt.Errorf("strategy runtime failed: %w", err)
	}
	if result.Status != agentRuntime.RunStatusCompleted || strings.TrimSpace(result.FinalAnswer) == "" {
		return nil, fmt.Errorf("strategy runtime ended with status %s", result.Status)
	}
	return map[string]interface{}{
		"text":       result.FinalAnswer,
		"iterations": len(result.Steps),
		"tool_trace": runtimeToolTrace(result),
	}, nil
}

// executeWorkflowStrategyLegacy preserves the pre-Runtime loop for rollback.
func (s *AgentService) executeWorkflowStrategyLegacy(ctx context.Context, strategy string, inputs map[string]interface{}) (map[string]interface{}, error) {
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
	maxIterations := workflowMaxIterations(inputs)
	modelName := firstWorkflowString(workflowStringInput(inputs, "model"), s.chatModel)
	maxTokens := workflowIntInput(inputs, "max_tokens", 2048)

	toolTrace := make([]map[string]interface{}, 0)
	for iteration := 1; iteration <= maxIterations; iteration++ {
		reservation, requestEstimate, err := s.reserveLegacyStrategyBudget(ctx, modelName, messages, openAITools, maxTokens)
		if err != nil {
			return nil, fmt.Errorf("reserve strategy LLM budget: %w", err)
		}
		resp, err := s.llmClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:     modelName,
			Messages:  messages,
			Tools:     openAITools,
			MaxTokens: maxTokens,
		})
		if err != nil {
			reservation.Release()
			return nil, fmt.Errorf("strategy LLM call failed: %w", err)
		}
		usage, usageErr := s.resolveLegacyStrategyUsage(ctx, modelName, requestEstimate, resp)
		if usageErr != nil {
			_ = reservation.Commit(usage)
			return nil, fmt.Errorf("resolve strategy LLM usage: %w", usageErr)
		}
		if err := reservation.Commit(usage); err != nil {
			return nil, fmt.Errorf("commit strategy LLM budget: %w", err)
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

			metadata := workflowTool.ExecutionMetadataFromContext(ctx)
			result, err := s.executeMCPToolGoverned(ctx, mcpClient, workflowTool.ExecutionRequest{
				ToolName: toolCall.Function.Name,
				Inputs:   args,
				Identity: workflowTool.CallerIdentity{UserID: userID},
				RunID:    metadata.RunID,
				StepID:   toolCall.ID,
				Source:   workflowTool.SourceWorkflow,
			}, mcp.CallToolRequest{
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
				"arguments": workflowTool.RedactInputs(args, nil),
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

func (s *AgentService) reserveLegacyStrategyBudget(
	ctx context.Context,
	model string,
	messages []openai.ChatCompletionMessage,
	tools []openai.Tool,
	maxTokens int,
) (*agentRuntime.UsageReservation, agentRuntime.TokenUsage, error) {
	if maxTokens <= 0 {
		return nil, agentRuntime.TokenUsage{}, errors.New("strategy max_tokens must be positive")
	}
	counter := s.runtimeTokens
	if counter == nil {
		counter = agentRuntime.NewHeuristicTokenCounter()
	}
	payload, err := json.Marshal(struct {
		Messages []openai.ChatCompletionMessage `json:"messages"`
		Tools    []openai.Tool                  `json:"tools,omitempty"`
	}{Messages: messages, Tools: tools})
	if err != nil {
		return nil, agentRuntime.TokenUsage{}, fmt.Errorf("encode strategy request for budget estimate: %w", err)
	}
	requestEstimate := agentRuntime.TokenUsage{
		InputTokens: counter.CountText(string(payload)),
		Estimated:   true,
	}
	requestEstimate.TotalTokens = requestEstimate.InputTokens
	reservationEstimate := requestEstimate
	reservationEstimate.OutputTokens = maxTokens
	reservationEstimate.TotalTokens += maxTokens

	if tracker, ok := agentRuntime.BudgetTrackerFromContext(ctx); ok && tracker.Budget().MaxEstimatedCostMicros > 0 {
		if s.runtimeCostEstimator == nil {
			return nil, agentRuntime.TokenUsage{}, errors.New("workflow cost budget requires a model cost estimator")
		}
		cost, err := s.runtimeCostEstimator.EstimateCost(model, reservationEstimate)
		if err != nil {
			return nil, agentRuntime.TokenUsage{}, fmt.Errorf("estimate strategy reservation cost: %w", err)
		}
		reservationEstimate.EstimatedCostMicros = cost.Micros
		reservationEstimate.CostEstimated = true
		reservationEstimate.PricingVersion = cost.PricingVersion
	}
	reservation, err := agentRuntime.ReserveBudgetUsage(ctx, reservationEstimate)
	return reservation, requestEstimate, err
}

func (s *AgentService) resolveLegacyStrategyUsage(
	ctx context.Context,
	requestedModel string,
	requestEstimate agentRuntime.TokenUsage,
	response openai.ChatCompletionResponse,
) (agentRuntime.TokenUsage, error) {
	usage := agentRuntime.TokenUsage{
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
		TotalTokens:  response.Usage.TotalTokens,
	}
	if usage.InputTokens <= 0 && usage.OutputTokens <= 0 && usage.TotalTokens <= 0 {
		counter := s.runtimeTokens
		if counter == nil {
			counter = agentRuntime.NewHeuristicTokenCounter()
		}
		payload, err := json.Marshal(response.Choices)
		if err != nil {
			return usage, fmt.Errorf("encode strategy response for budget estimate: %w", err)
		}
		usage.InputTokens = requestEstimate.InputTokens
		usage.OutputTokens = counter.CountText(string(payload))
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		usage.Estimated = true
	} else if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}

	tracker, tracked := agentRuntime.BudgetTrackerFromContext(ctx)
	if !tracked || tracker.Budget().MaxEstimatedCostMicros <= 0 {
		return usage, nil
	}
	if s.runtimeCostEstimator == nil {
		return usage, errors.New("workflow cost budget requires a model cost estimator")
	}
	model := firstWorkflowString(response.Model, requestedModel)
	cost, err := s.runtimeCostEstimator.EstimateCost(model, usage)
	if err != nil {
		return usage, fmt.Errorf("estimate completed strategy cost: %w", err)
	}
	usage.EstimatedCostMicros = cost.Micros
	usage.CostEstimated = usage.Estimated
	usage.PricingVersion = cost.PricingVersion
	return usage, nil
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

func workflowMaxIterations(inputs map[string]interface{}) int {
	maxIterations := workflowIntInput(inputs, "max_iterations", 5)
	if maxIterations < 1 {
		return 1
	}
	if maxIterations > 8 {
		return 8
	}
	return maxIterations
}

func filterRuntimeToolsByName(tools []agentRuntime.ToolDefinition, allowed map[string]struct{}) []agentRuntime.ToolDefinition {
	filtered := make([]agentRuntime.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allowed[tool.Name]; ok {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func runtimeToolTrace(result agentRuntime.RunResult) []map[string]interface{} {
	trace := make([]map[string]interface{}, 0)
	for _, step := range result.Steps {
		actions := make(map[string]agentRuntime.Action, len(step.Actions))
		for _, action := range step.Actions {
			actions[action.ID] = action
		}
		for _, observation := range step.Observations {
			arguments := map[string]interface{}{}
			if action, ok := actions[observation.ActionID]; ok && len(action.Arguments) > 0 {
				_ = json.Unmarshal(action.Arguments, &arguments)
			}
			trace = append(trace, map[string]interface{}{
				"iteration": step.Index,
				"tool":      observation.Name,
				"arguments": workflowTool.RedactInputs(arguments, nil),
				"is_error":  observation.IsError,
			})
		}
	}
	return trace
}

func workflowStrategyPrompt(strategy string) string {
	return workflowStrategyAgentProfile(strategy).Prompt.SystemPrompt
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
