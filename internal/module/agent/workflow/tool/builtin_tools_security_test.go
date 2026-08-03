package tool

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	agentModel "twitter-clone/internal/module/agent/model"
	agentObservability "twitter-clone/internal/module/agent/observability"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/pkg/ai"
)

func TestLLMChatToolSchemaExposesReferencesWithoutPlaintextKey(t *testing.T) {
	tool := NewLLMChatTool(&ai.Client{}, "model")
	schema := tool.InputSchema()
	if strings.Contains(schema, `"api_key"`) || !strings.Contains(schema, `"credential_ref"`) || !strings.Contains(schema, `"provider_config_id"`) {
		t.Fatalf("InputSchema() = %s", schema)
	}
	require.Contains(t, tool.Spec().SensitiveFields, "prompt")
	require.Contains(t, tool.Spec().SensitiveFields, "system_prompt")
}

func TestLLMChatToolRejectsPlaintextAPIKey(t *testing.T) {
	tool := NewLLMChatTool(&ai.Client{}, "model")
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"prompt": "hello", "api_key": "plaintext-secret",
	})
	if err == nil || !strings.Contains(err.Error(), "plaintext api_key is forbidden") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestLLMChatToolRejectsPrivateCustomEndpointBeforeNetworkCall(t *testing.T) {
	tool := NewLLMChatTool(&ai.Client{}, "model")
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"prompt": "hello", "provider": "custom", "base_url": "http://10.0.0.8/v1",
	})
	if !errors.Is(err, agentModel.ErrEndpointNotAllowed) {
		t.Fatalf("Execute() error = %v, want ErrEndpointNotAllowed", err)
	}
}

func TestLLMChatToolRejectsUnknownCredentialReference(t *testing.T) {
	tool := NewLLMChatTool(&ai.Client{}, "model")
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"prompt": "hello", "credential_ref": "unknown.secret",
	})
	if !errors.Is(err, ErrCredentialReferenceNotFound) {
		t.Fatalf("Execute() error = %v, want ErrCredentialReferenceNotFound", err)
	}
}

type providerConfigResolverStub struct {
	userID uint64
	config ResolvedProviderConfig
}

func (resolver *providerConfigResolverStub) ResolveWorkflowProviderConfig(_ context.Context, userID uint64, _ string) (ResolvedProviderConfig, error) {
	resolver.userID = userID
	return resolver.config, nil
}

func TestLLMChatToolProviderConfigIsTenantScopedAndAuthoritative(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload.Model != "config-model" || request.Header.Get("Authorization") != "Bearer config-secret" {
			t.Errorf("request model/auth = %q/%q", payload.Model, request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"chat-1","object":"chat.completion","model":"config-model","choices":[{"index":0,"message":{"role":"assistant","content":"configured answer"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	resolver := &providerConfigResolverStub{config: ResolvedProviderConfig{
		Provider: "lmstudio", BaseURL: server.URL + "/v1", Model: "config-model", APIKey: "config-secret",
	}}
	tool := NewLLMChatToolWithOptions(
		&ai.Client{}, "default-model",
		WithLLMProviderConfigResolver(resolver),
		WithLLMEndpointPolicy(agentModel.NewEndpointPolicy("127.0.0.1")),
	)
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"prompt": "hello", "provider_config_id": "config-id", "user_id": uint64(42),
		"provider": "custom", "base_url": "https://attacker.example/v1", "model": "attacker-model",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if resolver.userID != 42 || result["text"] != "configured answer" {
		t.Fatalf("resolver user/result = %d/%+v", resolver.userID, result)
	}
}

func TestLLMChatToolReservesAndRecordsWorkflowTokenBudget(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls++
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"chat-1","object":"chat.completion","model":"model","choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`))
	}))
	defer server.Close()

	tool := NewLLMChatToolWithOptions(
		&ai.Client{}, "model",
		WithLLMEndpointPolicy(agentModel.NewEndpointPolicy("127.0.0.1")),
	)
	tracker, _ := agentRuntime.NewBudgetTracker(agentRuntime.Budget{MaxTotalTokens: 100})
	ctx := agentRuntime.ContextWithBudgetTracker(context.Background(), tracker)
	result, err := tool.Execute(ctx, map[string]interface{}{
		"prompt": "hello", "base_url": server.URL + "/v1", "model": "model", "max_tokens": 10,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result["text"] != "answer" || calls != 1 {
		t.Fatalf("result/calls = %+v/%d", result, calls)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Usage.TotalTokens != 7 || snapshot.Usage.Estimated {
		t.Fatalf("budget usage = %+v", snapshot.Usage)
	}
	if snapshot.Reserved.TotalTokens != 0 {
		t.Fatalf("reserved usage = %+v", snapshot.Reserved)
	}
}

func TestLLMChatToolRecordsRedactedTraceWithExecutorIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"chat-1","object":"chat.completion","model":"trace-model","choices":[{"index":0,"message":{"role":"assistant","content":"private completion"},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":4,"total_tokens":13}}`))
	}))
	defer server.Close()

	traces := agentObservability.NewInMemoryRecorder()
	registry := NewRegistry()
	require.NoError(t, registry.Register(NewLLMChatToolWithOptions(
		&ai.Client{}, "trace-model",
		WithLLMEndpointPolicy(agentModel.NewEndpointPolicy("127.0.0.1")),
		WithLLMTraceRecorder(traces),
	)))
	executor := NewExecutor(registry)
	executionCtx := InjectExecutionMetadata(context.Background(), ExecutionMetadata{
		WorkflowID: "workflow-id", WorkflowRevisionID: "revision-id", WorkflowRevisionNumber: 3,
	})
	result, err := executor.ExecuteRegistered(executionCtx, ExecutionRequest{
		ToolName: "LLMChat", Identity: CallerIdentity{UserID: 52}, RunID: "workflow-run",
		StepID: "llm-node", Source: SourceWorkflow,
		Inputs: map[string]interface{}{
			"prompt": "private prompt", "provider": "custom", "base_url": server.URL + "/v1",
			"model": "trace-model", "max_tokens": 32,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "private completion", result["text"])
	bundle, err := traces.GetTraceBundle(context.Background(), 52, "workflow-run")
	require.NoError(t, err)
	require.Len(t, bundle.LLMCalls, 1)
	require.Equal(t, "llm-node", bundle.LLMCalls[0].StepID)
	require.Equal(t, 13, bundle.LLMCalls[0].Usage.TotalTokens)
	require.NotEmpty(t, bundle.LLMCalls[0].PromptHash)
	require.Equal(t, "workflow.workflow-id.node.llm-node", bundle.LLMCalls[0].PromptTemplateID)
	require.Equal(t, "revision-id", bundle.LLMCalls[0].PromptTemplateVersion)
	require.Equal(t, agentObservability.ContentSampleStatusDisabled, bundle.LLMCalls[0].PromptSampleStatus)
	encoded, err := json.Marshal(bundle)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private prompt")
	require.NotContains(t, string(encoded), "private completion")
}

func TestLLMChatToolRejectsOverBudgetBeforeNetworkCall(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	tool := NewLLMChatToolWithOptions(
		&ai.Client{}, "model",
		WithLLMEndpointPolicy(agentModel.NewEndpointPolicy("127.0.0.1")),
	)
	tracker, _ := agentRuntime.NewBudgetTracker(agentRuntime.Budget{MaxTotalTokens: 5})
	ctx := agentRuntime.ContextWithBudgetTracker(context.Background(), tracker)
	_, err := tool.Execute(ctx, map[string]interface{}{
		"prompt": "hello", "base_url": server.URL + "/v1", "model": "model", "max_tokens": 10,
	})
	if !agentRuntime.HasErrorCode(err, agentRuntime.ErrorBudgetExceeded) {
		t.Fatalf("Execute() error = %v, want budget_exceeded", err)
	}
	if calls != 0 {
		t.Fatalf("network calls = %d, want 0", calls)
	}
}
