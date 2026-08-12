package service

import (
	"context"
	"encoding/json"
	"testing"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sashabaranov/go-openai"
)

func TestEncodeMCPStructuredContentPreservesJSONWithoutTextParsing(t *testing.T) {
	t.Parallel()

	encoded, err := encodeMCPStructuredContent(map[string]any{
		"schema": "platform.tweet_search.v1",
		"items":  []any{map[string]any{"tweet_id": "9007199254740993"}},
	})
	if err != nil {
		t.Fatalf("encodeMCPStructuredContent() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("structured JSON is invalid: %v", err)
	}
	if decoded["schema"] != "platform.tweet_search.v1" {
		t.Fatalf("decoded structured content = %+v", decoded)
	}
}

func TestRestorePersistedMCPToolResultPreservesStructuredEvidence(t *testing.T) {
	original := mcp.NewToolResultStructured(map[string]any{
		"schema":   "platform.tweet_publish.v1",
		"tweet_id": "9007199254740993",
	}, "published")
	result, err := restorePersistedMCPToolResult(persistedMCPToolResult(original, "published"))
	if err != nil {
		t.Fatalf("restorePersistedMCPToolResult() error = %v", err)
	}
	encoded, err := encodeMCPStructuredContent(result.StructuredContent)
	if err != nil {
		t.Fatalf("encode restored structured content: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode restored structured content: %v", err)
	}
	if decoded["schema"] != "platform.tweet_publish.v1" || decoded["tweet_id"] != "9007199254740993" {
		t.Fatalf("restored structured content = %+v", decoded)
	}
}

func TestRestorePersistedMCPToolResultSupportsTextOnlyAndRejectsMalformed(t *testing.T) {
	result, err := restorePersistedMCPToolResult(map[string]interface{}{persistedMCPTextOutput: "read result"})
	if err != nil {
		t.Fatalf("restorePersistedMCPToolResult() error = %v", err)
	}
	if result.StructuredContent != nil || extractTextFromToolResult(result) != "read result" {
		t.Fatalf("restored text result = %+v", result)
	}
	for _, outputs := range []map[string]interface{}{
		nil,
		{persistedMCPStructuredOutput: map[string]any{"schema": "x"}},
		{persistedMCPTextOutput: 7},
	} {
		if _, err := restorePersistedMCPToolResult(outputs); err == nil {
			t.Fatalf("malformed persisted outputs were accepted: %+v", outputs)
		}
	}
}

func TestMCPToolsToRuntimeAppliesFailClosedCategories(t *testing.T) {
	definitions := mcpToolsToRuntime([]mcp.Tool{
		{Name: "hybrid_search_tweets", Description: "search"},
		{Name: "web_search", Description: "public web search"},
		{Name: "page_read", Description: "public page reader"},
		{Name: "create_tweet", Description: "publish"},
		{Name: "delete_account", Description: "unknown capability"},
	})

	if len(definitions) != 5 {
		t.Fatalf("mcpToolsToRuntime() definitions = %d, want 5", len(definitions))
	}
	assertRuntimeToolPolicy(t, definitions[0], agentRuntime.ToolCategoryRead, false)
	assertRuntimeToolPolicy(t, definitions[1], agentRuntime.ToolCategoryRead, false)
	assertRuntimeToolPolicy(t, definitions[2], agentRuntime.ToolCategoryRead, false)
	assertRuntimeToolPolicy(t, definitions[3], agentRuntime.ToolCategoryWrite, true)
	assertRuntimeToolPolicy(t, definitions[4], agentRuntime.ToolCategoryRisky, true)
}

func TestCreateTweetModelSchemaHidesTrustedExecutionFields(t *testing.T) {
	tool := mcp.NewTool(
		agentEnvironment.TweetPublishToolName,
		mcp.WithString("content", mcp.Required()),
		mcp.WithString("user_id", mcp.Required()),
		mcp.WithString("idempotency_key", mcp.Required()),
	)
	definitions := mcpToolsToRuntime([]mcp.Tool{tool})
	if len(definitions) != 1 {
		t.Fatalf("definitions = %d", len(definitions))
	}
	assertModelVisibleTweetSchema(t, definitions[0].InputSchema)

	openAITools := mcpToolsToOpenAI([]mcp.Tool{tool})
	if len(openAITools) != 1 || openAITools[0].Function == nil {
		t.Fatalf("OpenAI tools = %+v", openAITools)
	}
	encoded, err := json.Marshal(openAITools[0].Function.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	assertModelVisibleTweetSchema(t, encoded)
}

func TestInjectGovernedMCPInputsOverwritesForgedTweetIdentity(t *testing.T) {
	inputs := injectGovernedMCPInputs(workflowTool.ExecutionRequest{
		ToolName: agentEnvironment.TweetPublishToolName,
		Inputs: map[string]interface{}{
			"content": "draft", "user_id": "999", "idempotency_key": "forged",
		},
		Identity:       workflowTool.CallerIdentity{UserID: 42},
		IdempotencyKey: "run-1:publish-1:create_tweet",
	})
	if inputs["user_id"] != "42" || inputs["idempotency_key"] != "run-1:publish-1:create_tweet" {
		t.Fatalf("trusted inputs = %+v", inputs)
	}
}

func assertModelVisibleTweetSchema(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if _, exists := schema.Properties["content"]; !exists {
		t.Fatalf("content property is missing: %s", raw)
	}
	if _, exists := schema.Properties["user_id"]; exists {
		t.Fatalf("user_id leaked into model schema: %s", raw)
	}
	if _, exists := schema.Properties["idempotency_key"]; exists {
		t.Fatalf("idempotency_key leaked into model schema: %s", raw)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "content" {
		t.Fatalf("required fields = %+v", schema.Required)
	}
}
func TestPageReadMCPToolSpecRedactsURL(t *testing.T) {
	t.Parallel()

	spec := (&AgentService{}).mcpToolSpec("page_read")
	if spec.Category != "read" || spec.RequiresApproval() {
		t.Fatalf("page_read spec = %+v", spec)
	}
	found := false
	for _, field := range spec.SensitiveFields {
		if field == "url" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("page_read sensitive fields = %+v", spec.SensitiveFields)
	}
}

func TestInjectTrustedToolArgumentsOverwritesWebAccessIdentity(t *testing.T) {
	t.Parallel()

	ctx := workflowTool.InjectExecutionMetadata(context.Background(), workflowTool.ExecutionMetadata{
		RunID: "trusted-run",
	})
	arguments := injectTrustedToolArguments(ctx, 42, "page_read", map[string]any{
		"url":                                 "https://example.com",
		agentWebSearch.InternalUserIDArgument: uint64(7),
		agentWebSearch.InternalRunIDArgument:  "forged-run",
	})
	if arguments[agentWebSearch.InternalUserIDArgument] != uint64(42) ||
		arguments[agentWebSearch.InternalRunIDArgument] != "trusted-run" {
		t.Fatalf("trusted arguments = %+v", arguments)
	}
}

func TestInjectTrustedToolArgumentsOverwritesWebProviderConfig(t *testing.T) {
	t.Parallel()

	ctx := withWebSearchProviderConfig(context.Background(), "trusted-config")
	ctx = workflowTool.InjectExecutionMetadata(ctx, workflowTool.ExecutionMetadata{RunID: "trusted-run"})
	arguments := injectTrustedToolArguments(ctx, 42, "web_search", map[string]any{
		agentWebSearch.InternalProviderConfigIDArgument: "forged-config",
	})
	if arguments[agentWebSearch.InternalProviderConfigIDArgument] != "trusted-config" {
		t.Fatalf("trusted arguments = %+v", arguments)
	}

	arguments = injectTrustedToolArguments(context.Background(), 42, "web_search", map[string]any{
		agentWebSearch.InternalProviderConfigIDArgument: "forged-config",
	})
	if _, exists := arguments[agentWebSearch.InternalProviderConfigIDArgument]; exists {
		t.Fatalf("untrusted provider config survived injection: %+v", arguments)
	}
}

func TestWebSearchMCPToolSpecRedactsQuery(t *testing.T) {
	t.Parallel()

	spec := (&AgentService{}).mcpToolSpec("web_search")
	if spec.Category != "read" || spec.RequiresApproval() {
		t.Fatalf("web_search spec = %+v", spec)
	}
	found := false
	for _, field := range spec.SensitiveFields {
		if field == "query" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("web_search sensitive fields = %+v", spec.SensitiveFields)
	}
}

func TestOpenAIMessagesToRuntimePreservesConversationPairing(t *testing.T) {
	messages := openAIMessagesToRuntime([]openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "system"},
		{Role: openai.ChatMessageRoleAssistant, Content: "calling"},
		{Role: openai.ChatMessageRoleTool, Name: "search", ToolCallID: "call-1", Content: "result"},
		{Role: "unsupported", Content: "ignored"},
	})

	if len(messages) != 3 {
		t.Fatalf("openAIMessagesToRuntime() messages = %d, want 3", len(messages))
	}
	toolMessage := messages[2]
	if toolMessage.Role != agentRuntime.RoleTool || toolMessage.Name != "search" || toolMessage.ToolCallID != "call-1" {
		t.Fatalf("converted tool message = %+v", toolMessage)
	}
}

func TestRuntimeResultMetadataMarksEstimatedUsage(t *testing.T) {
	metadata := runtimeResultMetadata(agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-1"},
		Usage:   agentRuntime.TokenUsage{TotalTokens: 42, Estimated: true},
	}, "profile", "v2", "prompt-v3")

	if metadata["runtime_tokens"] != 42 || metadata["runtime_tokens_estimated"] != true {
		t.Fatalf("runtime metadata = %+v", metadata)
	}
	if metadata["agent_profile_version"] != "v2" || metadata["prompt_version"] != "prompt-v3" {
		t.Fatalf("runtime profile metadata = %+v", metadata)
	}
}

func assertRuntimeToolPolicy(
	t *testing.T,
	definition agentRuntime.ToolDefinition,
	wantCategory agentRuntime.ToolCategory,
	wantApproval bool,
) {
	t.Helper()
	if definition.Category != wantCategory || definition.ApprovalRequired() != wantApproval {
		t.Fatalf(
			"tool %q policy = category %q approval %t, want %q/%t",
			definition.Name,
			definition.Category,
			definition.ApprovalRequired(),
			wantCategory,
			wantApproval,
		)
	}
}
