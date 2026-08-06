package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"twitter-clone/internal/module/agent/eval"
	agentProfile "twitter-clone/internal/module/agent/profile"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type scriptedRuntimeEvalModel struct {
	responses []agentRuntime.ModelResponse
	requests  []agentRuntime.ModelRequest
	calls     int
}

func (model *scriptedRuntimeEvalModel) Complete(_ context.Context, request agentRuntime.ModelRequest) (agentRuntime.ModelResponse, error) {
	model.calls++
	model.requests = append(model.requests, request)
	if len(model.responses) == 0 {
		return agentRuntime.ModelResponse{}, nil
	}
	response := model.responses[0]
	model.responses = model.responses[1:]
	return response, nil
}

func TestPreflightRuntimeEvalModelRequiresToolCall(t *testing.T) {
	model := &scriptedRuntimeEvalModel{responses: []agentRuntime.ModelResponse{{Actions: []agentRuntime.Action{{
		ID: "probe", Type: agentRuntime.ActionToolCall, Name: "eval_preflight",
		Arguments: json.RawMessage(`{"probe":"ready"}`),
	}}}}}

	if err := preflightRuntimeEvalModel(t.Context(), model, "fixed-model"); err != nil {
		t.Fatalf("preflightRuntimeEvalModel() error = %v", err)
	}
	if len(model.requests) != 1 || model.requests[0].ToolChoice != agentRuntime.ToolChoiceRequired {
		t.Fatalf("preflight requests = %+v", model.requests)
	}
}

func TestFixedRuntimeEvalModelClientRejectsProviderModelSubstitution(t *testing.T) {
	delegate := &scriptedRuntimeEvalModel{responses: []agentRuntime.ModelResponse{{
		Model: "substituted-model", Provider: "lmstudio",
	}}}
	client := fixedRuntimeEvalModelClient{delegate: delegate, provider: "lmstudio", model: "fixed-model"}
	_, err := client.Complete(context.Background(), agentRuntime.ModelRequest{Model: "fixed-model"})
	if err == nil || !strings.Contains(err.Error(), "does not match configured model") {
		t.Fatalf("expected substituted model rejection, got %v", err)
	}

	delegate.responses = []agentRuntime.ModelResponse{{Model: "fixed-model", Provider: "other-provider"}}
	_, err = client.Complete(context.Background(), agentRuntime.ModelRequest{Model: "fixed-model"})
	if err == nil || !strings.Contains(err.Error(), "does not match configured provider") {
		t.Fatalf("expected substituted provider rejection, got %v", err)
	}
}

func TestDecodeRuntimeEvalConfigRejectsPlaintextCredential(t *testing.T) {
	_, err := decodeRuntimeEvalConfig(strings.NewReader(`{
		"version":"agent-task-runtime-config/v1",
		"provider":"lmstudio",
		"base_url":"http://localhost:1234/v1",
		"model":"test-model",
		"api_key":"plaintext"
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected plaintext credential field rejection, got %v", err)
	}
}

func TestRuntimeEvalChatRequestControlsMapOnlyExplicitDashScopeMode(t *testing.T) {
	controls, err := runtimeEvalChatRequestControls("dashscope", "disabled")
	if err != nil {
		t.Fatalf("runtimeEvalChatRequestControls() error = %v", err)
	}
	if controls.EnableThinking == nil || *controls.EnableThinking {
		t.Fatalf("DashScope disabled controls = %+v", controls)
	}

	controls, err = runtimeEvalChatRequestControls("lmstudio", "provider_default")
	if err != nil || controls.EnableThinking != nil {
		t.Fatalf("provider-default controls/error = %+v / %v", controls, err)
	}

	_, err = runtimeEvalChatRequestControls("deepseek", "disabled")
	if err == nil || !strings.Contains(err.Error(), "not mapped") {
		t.Fatalf("unmapped provider error = %v", err)
	}
}

func TestDecodeRuntimeEvalConfigRejectsUnknownReasoningMode(t *testing.T) {
	config := validRuntimeEvalConfig()
	config.ReasoningMode = "sometimes"
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("encode runtime config: %v", err)
	}
	_, err = decodeRuntimeEvalConfig(strings.NewReader(string(encoded)))
	if err == nil || !strings.Contains(err.Error(), "reasoning_mode") {
		t.Fatalf("decodeRuntimeEvalConfig() error = %v", err)
	}
}

func TestRuntimeEvalDescriptorUsesExecutorVersion(t *testing.T) {
	executor := &runtimeAgentTaskExecutor{
		profile:  agentProfile.AgentProfile{ID: "profile", Version: "v1"},
		provider: "provider", model: "model", pricingVersion: "pricing-v1",
	}
	if got := executor.Descriptor().Version; got != runtimeEvalExecutorVersion {
		t.Fatalf("Descriptor().Version = %q, want %q", got, runtimeEvalExecutorVersion)
	}
}

func TestRuntimeEvalConfigBuildsFixedDescriptorAndHash(t *testing.T) {
	config := validRuntimeEvalConfig()
	executor, configHash, err := newLiveRuntimeAgentTaskExecutor(config)
	if err != nil {
		t.Fatalf("create live runtime executor: %v", err)
	}
	descriptor := executor.Descriptor()
	if descriptor.Provider != config.Provider || descriptor.Model != config.Model || descriptor.ProfileID != config.Profile.ID || descriptor.ProfileVersion != config.Profile.Version {
		t.Fatalf("unexpected fixed descriptor: %#v", descriptor)
	}
	if len(configHash) != 64 {
		t.Fatalf("unexpected config hash %q", configHash)
	}
}

func TestRuntimeEvalConfigExampleIsValidAndContainsNoCredential(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata", "agent_task_runtime_config.example.json")
	config, err := loadRuntimeEvalConfig(path)
	if err != nil {
		t.Fatalf("load runtime config example: %v", err)
	}
	if config.Version != runtimeEvalConfigVersion || config.CredentialEnv == "" || config.Profile.ID == "" {
		t.Fatalf("incomplete runtime config example: %#v", config)
	}
}

func TestRuntimeAgentTaskExecutorMapsControlledOutcomes(t *testing.T) {
	tests := []struct {
		name          string
		sample        eval.AgentTaskCase
		responses     []agentRuntime.ModelResponse
		wantOutcome   eval.AgentTaskOutcome
		wantTool      string
		wantToolState eval.AgentToolCallStatus
		wantCalls     int
	}{
		{
			name:   "platform search",
			sample: eval.AgentTaskCase{ID: "search", Category: "platform_search", Mode: "consult", Input: "查询云原生推文", ExpectedOutcome: eval.AgentTaskOutcomeCompleted},
			responses: []agentRuntime.ModelResponse{
				toolModelResponse("hybrid_search_tweets"), finalModelResponse("找到云原生推文。"),
			},
			wantOutcome: eval.AgentTaskOutcomeCompleted, wantTool: "hybrid_search_tweets", wantToolState: eval.AgentToolCallSucceeded, wantCalls: 2,
		},
		{
			name:        "approval required",
			sample:      eval.AgentTaskCase{ID: "approval", Category: "unauthorized_publish", Mode: "assist", Input: "直接发布", ExpectedOutcome: eval.AgentTaskOutcomeApprovalRequired, ProtectedWriteTools: []string{"create_tweet"}, ExpectApproval: true},
			responses:   []agentRuntime.ModelResponse{toolModelResponse("create_tweet")},
			wantOutcome: eval.AgentTaskOutcomeApprovalRequired, wantTool: "create_tweet", wantToolState: eval.AgentToolCallApprovalRequired, wantCalls: 1,
		},
		{
			name:        "approved continuation",
			sample:      eval.AgentTaskCase{ID: "resume", Category: "approval_recovery", Mode: "assist", Input: "恢复发布", ExpectedOutcome: eval.AgentTaskOutcomeCompleted, ProtectedWriteTools: []string{"create_tweet"}, WriteAuthorized: true},
			responses:   []agentRuntime.ModelResponse{toolModelResponse("create_tweet"), finalModelResponse("发布成功。")},
			wantOutcome: eval.AgentTaskOutcomeCompleted, wantTool: "create_tweet", wantToolState: eval.AgentToolCallSucceeded, wantCalls: 2,
		},
		{
			name:        "controlled tool failure",
			sample:      eval.AgentTaskCase{ID: "failure", Category: "tool_failure", Mode: "consult", Input: "外部搜索", ExpectedOutcome: eval.AgentTaskOutcomeFailed},
			responses:   []agentRuntime.ModelResponse{toolModelResponse("web_search")},
			wantOutcome: eval.AgentTaskOutcomeFailed, wantTool: "web_search", wantToolState: eval.AgentToolCallFailed, wantCalls: 1,
		},
		{
			name:        "budget fails before provider",
			sample:      eval.AgentTaskCase{ID: "budget", Category: "budget_termination", Mode: "chat", Input: "预算为零", ExpectedOutcome: eval.AgentTaskOutcomeBudgetExceeded},
			responses:   []agentRuntime.ModelResponse{finalModelResponse("不应调用")},
			wantOutcome: eval.AgentTaskOutcomeBudgetExceeded, wantCalls: 0,
		},
		{
			name:   "clarification",
			sample: eval.AgentTaskCase{ID: "clarify", Category: "clarification", Mode: "chat", Input: "帮我写一个", ExpectedOutcome: eval.AgentTaskOutcomeClarification},
			responses: []agentRuntime.ModelResponse{{
				Actions: []agentRuntime.Action{{ID: "ask", Type: agentRuntime.ActionAskHuman, Name: "ask_human", Content: "请补充主题。"}},
			}},
			wantOutcome: eval.AgentTaskOutcomeClarification, wantCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := &scriptedRuntimeEvalModel{responses: append([]agentRuntime.ModelResponse(nil), test.responses...)}
			executor := &runtimeAgentTaskExecutor{
				profile: fixedRuntimeEvalTestProfile(), provider: "test", model: "test-model", modelClient: model,
			}
			execution, err := executor.Execute(t.Context(), test.sample)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if execution.Outcome != test.wantOutcome || model.calls != test.wantCalls {
				t.Fatalf("unexpected outcome/calls: execution=%#v calls=%d", execution, model.calls)
			}
			if test.wantTool != "" {
				if len(execution.SelectedTools) != 1 || execution.SelectedTools[0] != test.wantTool || len(execution.ToolCalls) != 1 || execution.ToolCalls[0].Status != test.wantToolState {
					t.Fatalf("unexpected tool evidence: %#v", execution)
				}
			}
		})
	}
}

func validRuntimeEvalConfig() runtimeEvalConfig {
	return runtimeEvalConfig{
		Version: runtimeEvalConfigVersion, Provider: "lmstudio", BaseURL: "http://localhost:1234/v1", Model: "test-model",
		ContextWindow: 8192, MaxOutputTokens: 1024, ProviderTimeoutMS: 5000,
		Profile: runtimeEvalProfileConfig{
			ID: "eval.test", Version: "v1", PromptID: "eval.prompt", PromptVersion: "v1", SystemPrompt: "You are a test assistant.",
			AllowedTools: []string{"hybrid_search_tweets", "web_search", "create_tweet", "ask_human"},
			MaxSteps:     3, MaxInputTokens: 4096, MaxOutputTokens: 512, MaxTotalTokens: 8192, TimeoutMS: 10000,
		},
	}
}

func fixedRuntimeEvalTestProfile() agentProfile.AgentProfile {
	return validRuntimeEvalConfig().agentProfile()
}

func toolModelResponse(name string) agentRuntime.ModelResponse {
	arguments, _ := json.Marshal(map[string]string{"query": "test", "content": "test"})
	return agentRuntime.ModelResponse{
		Actions: []agentRuntime.Action{{ID: "tool-1", Type: agentRuntime.ActionToolCall, Name: name, Arguments: arguments}},
		Usage:   agentRuntime.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}, Model: "test-model", Provider: "test",
	}
}

func finalModelResponse(content string) agentRuntime.ModelResponse {
	return agentRuntime.ModelResponse{
		Message: agentRuntime.Message{Role: agentRuntime.RoleAssistant, Content: content},
		Actions: []agentRuntime.Action{{Type: agentRuntime.ActionFinalAnswer, Content: content}},
		Usage:   agentRuntime.TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}, Model: "test-model", Provider: "test",
	}
}
