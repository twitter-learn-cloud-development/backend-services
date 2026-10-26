package model

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sashabaranov/go-openai"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

// CompleteWithAdmission covers text-only compatibility/strategy SDK callers.
// Runtime adapters already acquire their own call lease and must not use it.
func CompleteWithAdmission(ctx context.Context, client ChatCompletionClient, provider string, request openai.ChatCompletionRequest, admission CallAdmission) (openai.ChatCompletionResponse, error) {
	if admission == nil {
		return client.CreateChatCompletion(ctx, request)
	}
	bounded := agentRuntime.ModelRequest{Model: request.Model, MaxOutputTokens: request.MaxTokens}
	for _, message := range request.Messages {
		if len(message.MultiContent) > 0 {
			return openai.ChatCompletionResponse{}, errors.New("shared admission requires bounded text messages")
		}
		converted := agentRuntime.Message{Role: agentRuntime.MessageRole(message.Role), Content: message.Content, Name: message.Name, ToolCallID: message.ToolCallID}
		for _, call := range message.ToolCalls {
			converted.Actions = append(converted.Actions, agentRuntime.Action{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
		}
		bounded.Messages = append(bounded.Messages, converted)
	}
	for _, tool := range request.Tools {
		if tool.Function == nil {
			return openai.ChatCompletionResponse{}, errors.New("invalid completion tool")
		}
		schema, err := json.Marshal(tool.Function.Parameters)
		if err != nil {
			return openai.ChatCompletionResponse{}, errors.New("invalid completion schema")
		}
		bounded.Tools = append(bounded.Tools, agentRuntime.ToolDefinition{Name: tool.Function.Name, Description: tool.Function.Description, InputSchema: schema})
	}
	leased, release, err := admission.AcquireCall(ctx, provider, &bounded)
	if err != nil {
		return openai.ChatCompletionResponse{}, err
	}
	defer release()
	request.MaxTokens = bounded.MaxOutputTokens
	return client.CreateChatCompletion(leased, request)
}
