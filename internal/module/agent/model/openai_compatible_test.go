package model

import (
	"context"
	"encoding/json"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/sashabaranov/go-openai"
)

type fakeChatCompletionClient struct {
	request  openai.ChatCompletionRequest
	response openai.ChatCompletionResponse
	err      error
}

func (c *fakeChatCompletionClient) CreateChatCompletion(
	_ context.Context,
	request openai.ChatCompletionRequest,
) (openai.ChatCompletionResponse, error) {
	c.request = request
	return c.response, c.err
}

func TestOpenAICompatibleClientFinalAnswer(t *testing.T) {
	fake := &fakeChatCompletionClient{response: openai.ChatCompletionResponse{
		Model: "resolved-model",
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "answer"},
		}},
		Usage: openai.Usage{PromptTokens: 8, CompletionTokens: 2, TotalTokens: 10},
	}}
	client := NewOpenAICompatibleClient(fake, "configured-model", "test-provider")

	response, err := client.Complete(context.Background(), agentRuntime.ModelRequest{
		Messages:        []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "question"}},
		MaxOutputTokens: 123,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(response.Actions) != 1 || response.Actions[0].Type != agentRuntime.ActionFinalAnswer {
		t.Fatalf("Complete() actions = %+v", response.Actions)
	}
	if response.Model != "resolved-model" || response.Provider != "test-provider" {
		t.Fatalf("Complete() model/provider = %q/%q", response.Model, response.Provider)
	}
	if fake.request.Model != "configured-model" || fake.request.MaxTokens != 123 {
		t.Fatalf("OpenAI request model/max tokens = %q/%d", fake.request.Model, fake.request.MaxTokens)
	}
	if response.Usage.TotalTokens != 10 {
		t.Fatalf("Complete() total tokens = %d", response.Usage.TotalTokens)
	}
}

func TestOpenAICompatibleClientUsesRequestModelOverride(t *testing.T) {
	fake := &fakeChatCompletionClient{response: openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{Content: "answer"},
		}},
	}}
	client := NewOpenAICompatibleClient(fake, "configured-model", "test-provider")

	response, err := client.Complete(context.Background(), agentRuntime.ModelRequest{
		Model:    "workflow-model",
		Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "question"}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if fake.request.Model != "workflow-model" || response.Model != "workflow-model" {
		t.Fatalf("request/response model = %q/%q", fake.request.Model, response.Model)
	}
}

func TestOpenAICompatibleClientForwardsRequiredToolChoice(t *testing.T) {
	fake := &fakeChatCompletionClient{response: openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "answer"}}},
	}}
	client := NewOpenAICompatibleClient(fake, "model", "provider")

	_, err := client.Complete(context.Background(), agentRuntime.ModelRequest{
		Tools:      []agentRuntime.ToolDefinition{{Name: "search", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ToolChoice: agentRuntime.ToolChoiceRequired,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if fake.request.ToolChoice != string(agentRuntime.ToolChoiceRequired) {
		t.Fatalf("OpenAI tool choice = %#v", fake.request.ToolChoice)
	}
}

func TestOpenAICompatibleClientRejectsUnsupportedToolChoice(t *testing.T) {
	client := NewOpenAICompatibleClient(&fakeChatCompletionClient{}, "model", "provider")
	_, err := client.Complete(context.Background(), agentRuntime.ModelRequest{ToolChoice: agentRuntime.ToolChoice("sometimes")})
	if err == nil {
		t.Fatal("Complete() error = nil, want unsupported tool choice")
	}
}

func TestOpenAICompatibleClientSupportsDeveloperMessages(t *testing.T) {
	fake := &fakeChatCompletionClient{response: openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: "answer"}}},
	}}
	client := NewOpenAICompatibleClient(fake, "model", "provider")

	_, err := client.Complete(context.Background(), agentRuntime.ModelRequest{Messages: []agentRuntime.Message{
		{Role: agentRuntime.RoleDeveloper, Content: "policy"},
		{Role: agentRuntime.RoleUser, Content: "question"},
	}})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(fake.request.Messages) != 2 || fake.request.Messages[0].Role != "developer" {
		t.Fatalf("OpenAI messages = %+v", fake.request.Messages)
	}
}

func TestOpenAICompatibleClientToolCallsAndObservationPairing(t *testing.T) {
	fake := &fakeChatCompletionClient{response: openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleAssistant,
				ToolCalls: []openai.ToolCall{{
					ID:   "call-2",
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      "search",
						Arguments: `{"query":"go"}`,
					},
				}},
			},
		}},
	}}
	client := NewOpenAICompatibleClient(fake, "model", "provider")

	response, err := client.Complete(context.Background(), agentRuntime.ModelRequest{
		Messages: []agentRuntime.Message{
			{
				Role: agentRuntime.RoleAssistant,
				Actions: []agentRuntime.Action{{
					ID: "call-1", Type: agentRuntime.ActionToolCall, Name: "search",
					Arguments: json.RawMessage(`{"query":"agent"}`),
				}},
			},
			{Role: agentRuntime.RoleTool, ToolCallID: "call-1", Name: "search", Content: "result"},
		},
		Tools: []agentRuntime.ToolDefinition{{
			Name: "search", Description: "search tweets", InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(response.Actions) != 1 || response.Actions[0].ID != "call-2" {
		t.Fatalf("Complete() actions = %+v", response.Actions)
	}
	if len(fake.request.Messages) != 2 {
		t.Fatalf("OpenAI messages = %d", len(fake.request.Messages))
	}
	if fake.request.Messages[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("assistant tool call = %+v", fake.request.Messages[0].ToolCalls)
	}
	if fake.request.Messages[1].ToolCallID != "call-1" {
		t.Fatalf("tool observation = %+v", fake.request.Messages[1])
	}
}

func TestOpenAICompatibleClientClassifiesControlActions(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		arguments  string
		wantType   agentRuntime.ActionType
		wantPrompt string
	}{
		{name: "rag", toolName: "rag_search", arguments: `{"query":"memory"}`, wantType: agentRuntime.ActionRAGSearch},
		{name: "human", toolName: "ask_human", arguments: `{"question":"approve?"}`, wantType: agentRuntime.ActionAskHuman, wantPrompt: "approve?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeChatCompletionClient{response: openai.ChatCompletionResponse{
				Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{
					ToolCalls: []openai.ToolCall{{
						ID: "control", Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{Name: tt.toolName, Arguments: tt.arguments},
					}},
				}}},
			}}
			client := NewOpenAICompatibleClient(fake, "model", "provider")
			response, err := client.Complete(context.Background(), agentRuntime.ModelRequest{})
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			if response.Actions[0].Type != tt.wantType || response.Actions[0].Content != tt.wantPrompt {
				t.Fatalf("control action = %+v", response.Actions[0])
			}
		})
	}
}
