package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/sashabaranov/go-openai"
)

type ChatCompletionClient interface {
	CreateChatCompletion(ctx context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

type OpenAICompatibleClient struct {
	client   ChatCompletionClient
	model    string
	provider string
}

func NewOpenAICompatibleClient(client ChatCompletionClient, model, provider string) *OpenAICompatibleClient {
	return &OpenAICompatibleClient{
		client:   client,
		model:    strings.TrimSpace(model),
		provider: strings.TrimSpace(provider),
	}
}

func (c *OpenAICompatibleClient) Complete(
	ctx context.Context,
	request agentRuntime.ModelRequest,
) (agentRuntime.ModelResponse, error) {
	if c == nil || c.client == nil {
		return agentRuntime.ModelResponse{}, errors.New("OpenAI-compatible client is not configured")
	}
	modelName := strings.TrimSpace(request.Model)
	if modelName == "" {
		modelName = c.model
	}
	if modelName == "" {
		return agentRuntime.ModelResponse{}, errors.New("OpenAI-compatible model is required")
	}

	messages, err := toOpenAIMessages(request.Messages)
	if err != nil {
		return agentRuntime.ModelResponse{}, err
	}
	tools, err := toOpenAITools(request.Tools)
	if err != nil {
		return agentRuntime.ModelResponse{}, err
	}

	completionRequest := openai.ChatCompletionRequest{
		Model:    modelName,
		Messages: messages,
		Tools:    tools,
	}
	if !request.ToolChoice.Valid() {
		return agentRuntime.ModelResponse{}, fmt.Errorf("unsupported tool choice %q", request.ToolChoice)
	}
	if request.ToolChoice != "" {
		completionRequest.ToolChoice = string(request.ToolChoice)
	}
	if request.MaxOutputTokens > 0 {
		completionRequest.MaxTokens = request.MaxOutputTokens
	}

	response, err := c.client.CreateChatCompletion(ctx, completionRequest)
	if err != nil {
		return agentRuntime.ModelResponse{}, err
	}
	if len(response.Choices) == 0 {
		return agentRuntime.ModelResponse{}, nil
	}

	choice := response.Choices[0]
	actions := make([]agentRuntime.Action, 0, len(choice.Message.ToolCalls))
	for _, toolCall := range choice.Message.ToolCalls {
		action := agentRuntime.Action{
			ID:        toolCall.ID,
			Type:      classifyToolAction(toolCall.Function.Name),
			Name:      toolCall.Function.Name,
			Arguments: json.RawMessage(toolCall.Function.Arguments),
		}
		if action.Type == agentRuntime.ActionAskHuman {
			action.Content = humanQuestion(action.Arguments)
		}
		actions = append(actions, action)
	}
	if len(actions) == 0 && strings.TrimSpace(choice.Message.Content) != "" {
		actions = append(actions, agentRuntime.Action{
			Type:    agentRuntime.ActionFinalAnswer,
			Content: choice.Message.Content,
		})
	}

	resolvedModel := strings.TrimSpace(response.Model)
	if resolvedModel == "" {
		resolvedModel = modelName
	}
	return agentRuntime.ModelResponse{
		Message: agentRuntime.Message{
			Role:    agentRuntime.RoleAssistant,
			Content: choice.Message.Content,
			Actions: actions,
		},
		Actions: actions,
		Usage: agentRuntime.TokenUsage{
			InputTokens:  response.Usage.PromptTokens,
			OutputTokens: response.Usage.CompletionTokens,
			TotalTokens:  response.Usage.TotalTokens,
		},
		Model:    resolvedModel,
		Provider: c.provider,
	}, nil
}

func toOpenAIMessages(messages []agentRuntime.Message) ([]openai.ChatCompletionMessage, error) {
	converted := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, message := range messages {
		role, err := toOpenAIRole(message.Role)
		if err != nil {
			return nil, err
		}
		convertedMessage := openai.ChatCompletionMessage{
			Role:       role,
			Content:    message.Content,
			Name:       message.Name,
			ToolCallID: message.ToolCallID,
		}
		for _, action := range message.Actions {
			if action.Type == agentRuntime.ActionFinalAnswer {
				continue
			}
			name := action.Name
			if action.Type == agentRuntime.ActionAskHuman && name == "" {
				name = "ask_human"
			}
			if action.Type == agentRuntime.ActionRAGSearch && name == "" {
				name = "rag_search"
			}
			arguments := action.Arguments
			if len(arguments) == 0 {
				arguments = json.RawMessage("{}")
			}
			convertedMessage.ToolCalls = append(convertedMessage.ToolCalls, openai.ToolCall{
				ID:   action.ID,
				Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      name,
					Arguments: string(arguments),
				},
			})
		}
		converted = append(converted, convertedMessage)
	}
	return converted, nil
}

func toOpenAITools(tools []agentRuntime.ToolDefinition) ([]openai.Tool, error) {
	converted := make([]openai.Tool, 0, len(tools))
	for _, tool := range tools {
		var parameters map[string]any
		if len(tool.InputSchema) == 0 {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		} else if err := json.Unmarshal(tool.InputSchema, &parameters); err != nil {
			return nil, fmt.Errorf("decode tool %s schema: %w", tool.Name, err)
		}
		converted = append(converted, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  parameters,
			},
		})
	}
	return converted, nil
}

func toOpenAIRole(role agentRuntime.MessageRole) (string, error) {
	switch role {
	case agentRuntime.RoleSystem:
		return openai.ChatMessageRoleSystem, nil
	case agentRuntime.RoleDeveloper:
		return "developer", nil
	case agentRuntime.RoleUser:
		return openai.ChatMessageRoleUser, nil
	case agentRuntime.RoleAssistant:
		return openai.ChatMessageRoleAssistant, nil
	case agentRuntime.RoleTool:
		return openai.ChatMessageRoleTool, nil
	default:
		return "", fmt.Errorf("unsupported runtime message role %q", role)
	}
}

func classifyToolAction(name string) agentRuntime.ActionType {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ask_human":
		return agentRuntime.ActionAskHuman
	case "rag_search":
		return agentRuntime.ActionRAGSearch
	default:
		return agentRuntime.ActionToolCall
	}
}

func humanQuestion(arguments json.RawMessage) string {
	var payload struct {
		Question string `json:"question"`
		Prompt   string `json:"prompt"`
	}
	if err := json.Unmarshal(arguments, &payload); err != nil {
		return ""
	}
	if strings.TrimSpace(payload.Question) != "" {
		return payload.Question
	}
	return payload.Prompt
}
