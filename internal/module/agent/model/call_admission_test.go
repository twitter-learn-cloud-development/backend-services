package model

import (
	"context"
	"errors"
	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/require"
	"testing"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type callGuardFake struct {
	calls, released int
	err             error
	cancel          bool
	provider        string
}

func (f *callGuardFake) AcquireCall(ctx context.Context, provider string, r *agentRuntime.ModelRequest) (context.Context, func(), error) {
	f.calls++
	f.provider = provider
	if f.err != nil {
		return ctx, nil, f.err
	}
	r.MaxOutputTokens = 12
	if f.cancel {
		child, cancel := context.WithCancel(ctx)
		cancel()
		ctx = child
	}
	return ctx, func() { f.released++ }, nil
}

type cancellationClient struct{ called bool }

func (c *cancellationClient) CreateChatCompletion(ctx context.Context, r openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	c.called = true
	return openai.ChatCompletionResponse{}, ctx.Err()
}

func TestModelCallAdmissionDenyClampReleaseAndValidation(t *testing.T) {
	guard := &callGuardFake{err: errors.New("quota denied")}
	fake := &fakeChatCompletionClient{}
	client := NewOpenAICompatibleClient(fake, "model", "provider", WithCallAdmission(guard))
	request := agentRuntime.ModelRequest{Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "q"}}, MaxOutputTokens: 100}
	_, err := client.Complete(context.Background(), request)
	require.ErrorIs(t, err, guard.err)
	require.Empty(t, fake.request.Model)
	require.Zero(t, guard.released)
	guard.err = nil
	fake.err = errors.New("provider failed")
	_, err = client.Complete(context.Background(), request)
	require.ErrorIs(t, err, fake.err)
	require.Equal(t, 12, fake.request.MaxTokens)
	require.Equal(t, 1, guard.released)
	require.Equal(t, "provider", guard.provider)
	request.Messages[0].Role = "invalid"
	_, err = client.Complete(context.Background(), request)
	require.Error(t, err)
	require.Equal(t, 2, guard.calls)
}
func TestModelCallAdmissionCancellationReachesProvider(t *testing.T) {
	guard := &callGuardFake{cancel: true}
	fake := &cancellationClient{}
	client := NewOpenAICompatibleClient(fake, "model", "provider", WithCallAdmission(guard))
	_, err := client.Complete(context.Background(), agentRuntime.ModelRequest{})
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, fake.called)
	require.Equal(t, 1, guard.released)
}

func TestCompatibilityCallAdmissionGuardsToolsAndClampsOutput(t *testing.T) {
	guard := &callGuardFake{}
	fake := &fakeChatCompletionClient{}
	request := openai.ChatCompletionRequest{Model: "model", MaxTokens: 100, Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "q"}}, Tools: []openai.Tool{{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{Name: "search", Parameters: map[string]interface{}{"type": "object"}}}}}
	_, err := CompleteWithAdmission(context.Background(), fake, "provider", request, guard)
	require.NoError(t, err)
	require.Equal(t, 12, fake.request.MaxTokens)
	require.Equal(t, 1, guard.released)
	guard.err = errors.New("limited")
	fake.request.Model = ""
	_, err = CompleteWithAdmission(context.Background(), fake, "provider", request, guard)
	require.ErrorIs(t, err, guard.err)
	require.Empty(t, fake.request.Model)
	request.Messages[0].MultiContent = []openai.ChatMessagePart{{Type: openai.ChatMessagePartTypeText, Text: "extra"}}
	_, err = CompleteWithAdmission(context.Background(), fake, "provider", request, guard)
	require.Error(t, err)
	require.Equal(t, 2, guard.calls)
}
