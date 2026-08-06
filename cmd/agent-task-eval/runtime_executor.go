package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentModel "twitter-clone/internal/module/agent/model"
	agentProfile "twitter-clone/internal/module/agent/profile"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/sashabaranov/go-openai"
)

const (
	runtimeEvalConfigVersion   = "agent-task-runtime-config/v1"
	runtimeEvalExecutorVersion = "agent-task-runtime/v5"
)

var credentialEnvPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

type runtimeEvalConfig struct {
	Version                      string                   `json:"version"`
	Provider                     string                   `json:"provider"`
	BaseURL                      string                   `json:"base_url"`
	Model                        string                   `json:"model"`
	CredentialEnv                string                   `json:"credential_env"`
	ReasoningMode                string                   `json:"reasoning_mode,omitempty"`
	ContextWindow                int                      `json:"context_window"`
	MaxOutputTokens              int                      `json:"max_output_tokens"`
	InputMicrosPerMillionTokens  int64                    `json:"input_micros_per_million_tokens,omitempty"`
	OutputMicrosPerMillionTokens int64                    `json:"output_micros_per_million_tokens,omitempty"`
	PricingVersion               string                   `json:"pricing_version,omitempty"`
	ProviderTimeoutMS            int64                    `json:"provider_timeout_ms"`
	Profile                      runtimeEvalProfileConfig `json:"profile"`
}

type runtimeEvalProfileConfig struct {
	ID                     string   `json:"id"`
	Version                string   `json:"version"`
	PromptID               string   `json:"prompt_id"`
	PromptVersion          string   `json:"prompt_version"`
	SystemPrompt           string   `json:"system_prompt"`
	AllowedTools           []string `json:"allowed_tools"`
	MaxSteps               int      `json:"max_steps"`
	MaxInputTokens         int      `json:"max_input_tokens"`
	MaxOutputTokens        int      `json:"max_output_tokens"`
	MaxTotalTokens         int      `json:"max_total_tokens"`
	MaxEstimatedCostMicros int64    `json:"max_estimated_cost_micros,omitempty"`
	TimeoutMS              int64    `json:"timeout_ms"`
}

type runtimeEvalProviderSettings struct {
	Provider                     string
	BaseURL                      string
	Model                        string
	CredentialEnv                string
	ReasoningMode                string
	ContextWindow                int
	MaxOutputTokens              int
	InputMicrosPerMillionTokens  int64
	OutputMicrosPerMillionTokens int64
	PricingVersion               string
	ProviderTimeoutMS            int64
}

type runtimeAgentTaskExecutor struct {
	profile        agentProfile.AgentProfile
	provider       string
	model          string
	pricingVersion string
	modelClient    agentRuntime.ModelClient
	costEstimator  agentRuntime.CostEstimator
}

type fixedRuntimeEvalModelClient struct {
	delegate agentRuntime.ModelClient
	provider string
	model    string
}

func (client fixedRuntimeEvalModelClient) Complete(ctx context.Context, request agentRuntime.ModelRequest) (agentRuntime.ModelResponse, error) {
	if client.delegate == nil {
		return agentRuntime.ModelResponse{}, errors.New("fixed runtime evaluation model client is not configured")
	}
	response, err := client.delegate.Complete(ctx, request)
	if err != nil {
		return agentRuntime.ModelResponse{}, err
	}
	if strings.TrimSpace(response.Provider) != client.provider {
		return agentRuntime.ModelResponse{}, fmt.Errorf("runtime evaluation provider response %q does not match configured provider %q", response.Provider, client.provider)
	}
	if strings.TrimSpace(response.Model) != client.model {
		return agentRuntime.ModelResponse{}, fmt.Errorf("runtime evaluation model response %q does not match configured model %q", response.Model, client.model)
	}
	return response, nil
}

func loadRuntimeEvalConfig(path string) (runtimeEvalConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return runtimeEvalConfig{}, fmt.Errorf("open runtime evaluation config: %w", err)
	}
	defer file.Close()
	return decodeRuntimeEvalConfig(file)
}

func decodeRuntimeEvalConfig(reader io.Reader) (runtimeEvalConfig, error) {
	if reader == nil {
		return runtimeEvalConfig{}, errors.New("runtime evaluation config reader is nil")
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var config runtimeEvalConfig
	if err := decoder.Decode(&config); err != nil {
		return runtimeEvalConfig{}, fmt.Errorf("decode runtime evaluation config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return runtimeEvalConfig{}, errors.New("runtime evaluation config contains multiple JSON values")
		}
		return runtimeEvalConfig{}, fmt.Errorf("decode runtime evaluation config trailer: %w", err)
	}
	return normalizeRuntimeEvalConfig(config)
}

func normalizeRuntimeEvalConfig(config runtimeEvalConfig) (runtimeEvalConfig, error) {
	config.Version = strings.TrimSpace(config.Version)
	config.Provider = strings.TrimSpace(config.Provider)
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.Model = strings.TrimSpace(config.Model)
	config.CredentialEnv = strings.TrimSpace(config.CredentialEnv)
	config.ReasoningMode = strings.ToLower(strings.TrimSpace(config.ReasoningMode))
	config.PricingVersion = strings.TrimSpace(config.PricingVersion)
	config.Profile.ID = strings.TrimSpace(config.Profile.ID)
	config.Profile.Version = strings.TrimSpace(config.Profile.Version)
	config.Profile.PromptID = strings.TrimSpace(config.Profile.PromptID)
	config.Profile.PromptVersion = strings.TrimSpace(config.Profile.PromptVersion)
	config.Profile.SystemPrompt = strings.TrimSpace(config.Profile.SystemPrompt)
	for index := range config.Profile.AllowedTools {
		config.Profile.AllowedTools[index] = strings.TrimSpace(config.Profile.AllowedTools[index])
	}
	if config.Version != runtimeEvalConfigVersion {
		return runtimeEvalConfig{}, fmt.Errorf("runtime evaluation config version must be %q", runtimeEvalConfigVersion)
	}
	if config.Provider == "" || config.BaseURL == "" || config.Model == "" {
		return runtimeEvalConfig{}, errors.New("runtime evaluation provider, base_url and model are required")
	}
	if config.CredentialEnv != "" && !credentialEnvPattern.MatchString(config.CredentialEnv) {
		return runtimeEvalConfig{}, errors.New("runtime evaluation credential_env must be an uppercase environment variable name")
	}
	if config.ReasoningMode != "" && config.ReasoningMode != "provider_default" &&
		config.ReasoningMode != "disabled" && config.ReasoningMode != "enabled" {
		return runtimeEvalConfig{}, errors.New("runtime evaluation reasoning_mode must be provider_default, disabled or enabled")
	}
	if config.ContextWindow <= 0 || config.MaxOutputTokens <= 0 || config.MaxOutputTokens > config.ContextWindow {
		return runtimeEvalConfig{}, errors.New("runtime evaluation context_window and max_output_tokens are invalid")
	}
	if config.InputMicrosPerMillionTokens < 0 || config.OutputMicrosPerMillionTokens < 0 {
		return runtimeEvalConfig{}, errors.New("runtime evaluation pricing cannot be negative")
	}
	if (config.InputMicrosPerMillionTokens > 0 || config.OutputMicrosPerMillionTokens > 0) && config.PricingVersion == "" {
		return runtimeEvalConfig{}, errors.New("runtime evaluation priced model requires pricing_version")
	}
	if config.ProviderTimeoutMS < 100 || config.ProviderTimeoutMS > int64((10*time.Minute)/time.Millisecond) {
		return runtimeEvalConfig{}, errors.New("runtime evaluation provider_timeout_ms must be between 100 and 600000")
	}
	if config.Profile.MaxSteps < 1 || config.Profile.MaxSteps > agentRuntime.MaxAllowedSteps {
		return runtimeEvalConfig{}, fmt.Errorf("runtime evaluation profile max_steps must be between 1 and %d", agentRuntime.MaxAllowedSteps)
	}
	if config.Profile.MaxInputTokens <= 0 || config.Profile.MaxOutputTokens <= 0 || config.Profile.MaxTotalTokens <= 0 {
		return runtimeEvalConfig{}, errors.New("runtime evaluation profile token budgets must be positive")
	}
	if config.Profile.MaxOutputTokens > config.MaxOutputTokens || config.Profile.MaxTotalTokens < config.Profile.MaxOutputTokens {
		return runtimeEvalConfig{}, errors.New("runtime evaluation profile output budget exceeds the fixed model limits")
	}
	if config.Profile.MaxEstimatedCostMicros < 0 {
		return runtimeEvalConfig{}, errors.New("runtime evaluation profile cost budget cannot be negative")
	}
	if config.Profile.MaxEstimatedCostMicros > 0 && config.PricingVersion == "" {
		return runtimeEvalConfig{}, errors.New("runtime evaluation cost budget requires fixed model pricing")
	}
	if config.Profile.TimeoutMS < 100 || config.Profile.TimeoutMS > int64((30*time.Minute)/time.Millisecond) {
		return runtimeEvalConfig{}, errors.New("runtime evaluation profile timeout_ms must be between 100 and 1800000")
	}
	profile := config.agentProfile()
	if _, err := agentProfile.NewCatalog([]agentProfile.AgentProfile{profile}, nil); err != nil {
		return runtimeEvalConfig{}, fmt.Errorf("validate fixed runtime evaluation profile: %w", err)
	}
	definition := config.modelDefinition()
	if _, err := agentModel.NewCatalog([]agentModel.Definition{definition}); err != nil {
		return runtimeEvalConfig{}, fmt.Errorf("validate fixed runtime evaluation model: %w", err)
	}
	policy := agentModel.NewEndpointPolicy()
	if err := policy.Validate(config.BaseURL, config.Provider); err != nil {
		return runtimeEvalConfig{}, fmt.Errorf("validate runtime evaluation endpoint: %w", err)
	}
	return config, nil
}

func newLiveRuntimeAgentTaskExecutor(config runtimeEvalConfig) (*runtimeAgentTaskExecutor, string, error) {
	config, err := normalizeRuntimeEvalConfig(config)
	if err != nil {
		return nil, "", err
	}
	router, err := newRuntimeEvalProvider(config.providerSettings())
	if err != nil {
		return nil, "", err
	}
	profileCatalog, err := agentProfile.NewCatalog([]agentProfile.AgentProfile{config.agentProfile()}, nil)
	if err != nil {
		return nil, "", err
	}
	fixedProfile, err := profileCatalog.Resolve(context.Background(), config.Profile.ID, agentProfile.SelectionSubject{StickyKey: "agent-task-eval"})
	if err != nil {
		return nil, "", err
	}
	configHash, err := hashRuntimeEvalConfig(config)
	if err != nil {
		return nil, "", err
	}
	return &runtimeAgentTaskExecutor{
		profile: fixedProfile, provider: config.Provider, model: config.Model,
		pricingVersion: config.PricingVersion,
		modelClient:    fixedRuntimeEvalModelClient{delegate: router, provider: config.Provider, model: config.Model},
		costEstimator:  router,
	}, configHash, nil
}

func hashRuntimeEvalConfig(config runtimeEvalConfig) (string, error) {
	config, err := normalizeRuntimeEvalConfig(config)
	if err != nil {
		return "", err
	}
	configHash, err := eval.HashCanonicalJSON(struct {
		Config          runtimeEvalConfig `json:"config"`
		ExecutorVersion string            `json:"executor_version"`
	}{Config: config, ExecutorVersion: runtimeEvalExecutorVersion})
	if err != nil {
		return "", fmt.Errorf("hash runtime evaluation config: %w", err)
	}
	return configHash, nil
}

func newRuntimeEvalProvider(settings runtimeEvalProviderSettings) (*agentModel.ProviderRouter, error) {
	apiKey := ""
	if settings.CredentialEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(settings.CredentialEnv))
	}
	if apiKey == "" {
		if !isLocalRuntimeEvalProvider(settings.Provider) {
			return nil, fmt.Errorf("runtime evaluation credential environment variable %q is empty", settings.CredentialEnv)
		}
		apiKey = "local-eval"
	}
	policy := agentModel.NewEndpointPolicy()
	httpClient := agentModel.NewRestrictedHTTPClient(policy, settings.Provider)
	httpClient.Timeout = time.Duration(settings.ProviderTimeoutMS) * time.Millisecond
	controls, err := runtimeEvalChatRequestControls(settings.Provider, settings.ReasoningMode)
	if err != nil {
		return nil, err
	}
	httpClient, err = agentModel.WithChatCompletionRequestControls(httpClient, controls)
	if err != nil {
		return nil, fmt.Errorf("configure runtime evaluation chat request controls: %w", err)
	}
	openAIConfig := openai.DefaultConfig(apiKey)
	openAIConfig.BaseURL = settings.BaseURL
	openAIConfig.HTTPClient = httpClient
	providerClient := agentModel.NewOpenAICompatibleClient(
		openai.NewClientWithConfig(openAIConfig),
		settings.Model,
		settings.Provider,
	)
	modelCatalog, err := agentModel.NewCatalog([]agentModel.Definition{runtimeEvalModelDefinition(settings)})
	if err != nil {
		return nil, err
	}
	return agentModel.NewProviderRouter(
		modelCatalog,
		map[string]agentRuntime.ModelClient{settings.Provider: providerClient},
	)
}

func runtimeEvalChatRequestControls(
	provider string,
	reasoningMode string,
) (agentModel.ChatCompletionRequestControls, error) {
	reasoningMode = strings.ToLower(strings.TrimSpace(reasoningMode))
	if reasoningMode == "" || reasoningMode == "provider_default" {
		return agentModel.ChatCompletionRequestControls{}, nil
	}
	normalizedProvider := strings.NewReplacer("-", "", "_", "", " ", "").Replace(
		strings.ToLower(strings.TrimSpace(provider)),
	)
	if normalizedProvider != "dashscope" {
		return agentModel.ChatCompletionRequestControls{}, fmt.Errorf(
			"runtime evaluation reasoning_mode %q is not mapped for provider %q",
			reasoningMode,
			provider,
		)
	}
	enableThinking := reasoningMode == "enabled"
	return agentModel.ChatCompletionRequestControls{EnableThinking: &enableThinking}, nil
}

func (config runtimeEvalConfig) modelDefinition() agentModel.Definition {
	return runtimeEvalModelDefinition(config.providerSettings())
}

func (config runtimeEvalConfig) providerSettings() runtimeEvalProviderSettings {
	return runtimeEvalProviderSettings{
		Provider: config.Provider, BaseURL: config.BaseURL, Model: config.Model,
		CredentialEnv: config.CredentialEnv, ReasoningMode: config.ReasoningMode,
		ContextWindow:                config.ContextWindow,
		MaxOutputTokens:              config.MaxOutputTokens,
		InputMicrosPerMillionTokens:  config.InputMicrosPerMillionTokens,
		OutputMicrosPerMillionTokens: config.OutputMicrosPerMillionTokens,
		PricingVersion:               config.PricingVersion, ProviderTimeoutMS: config.ProviderTimeoutMS,
	}
}

func runtimeEvalModelDefinition(settings runtimeEvalProviderSettings) agentModel.Definition {
	return agentModel.Definition{
		ID: settings.Model, Provider: settings.Provider, ContextWindow: settings.ContextWindow,
		MaxOutputTokens: settings.MaxOutputTokens,
		Pricing: agentModel.Pricing{
			InputMicrosPerMillionTokens:  settings.InputMicrosPerMillionTokens,
			OutputMicrosPerMillionTokens: settings.OutputMicrosPerMillionTokens,
			Version:                      settings.PricingVersion,
		},
		Capabilities: []agentModel.Capability{agentModel.CapabilityChat, agentModel.CapabilityToolCall},
	}
}

func (config runtimeEvalConfig) agentProfile() agentProfile.AgentProfile {
	return runtimeEvalAgentProfile(config.Profile)
}

func runtimeEvalAgentProfile(config runtimeEvalProfileConfig) agentProfile.AgentProfile {
	return agentProfile.AgentProfile{
		ID: config.ID, Version: config.Version,
		Prompt: agentProfile.PromptProfile{
			ID: config.PromptID, Version: config.PromptVersion, SystemPrompt: config.SystemPrompt,
		},
		Budget: agentRuntime.Budget{
			MaxSteps: config.MaxSteps, MaxInputTokens: config.MaxInputTokens,
			MaxOutputTokens: config.MaxOutputTokens, MaxTotalTokens: config.MaxTotalTokens,
			MaxEstimatedCostMicros: config.MaxEstimatedCostMicros,
			Timeout:                time.Duration(config.TimeoutMS) * time.Millisecond,
		},
		AllowedTools: append([]string(nil), config.AllowedTools...),
	}
}

func (executor *runtimeAgentTaskExecutor) Descriptor() eval.AgentTaskExecutionDescriptor {
	if executor == nil {
		return eval.AgentTaskExecutionDescriptor{}
	}
	return eval.AgentTaskExecutionDescriptor{
		Kind: "runtime_live", Version: runtimeEvalExecutorVersion, Strategy: "single_agent",
		Provider: executor.provider, Model: executor.model,
		ProfileID: executor.profile.ID, ProfileVersion: executor.profile.Version,
		PricingVersion: executor.pricingVersion,
	}
}

func (executor *runtimeAgentTaskExecutor) Preflight(ctx context.Context) error {
	if executor == nil || executor.modelClient == nil {
		return errors.New("runtime agent task executor is not configured")
	}
	return preflightRuntimeEvalModel(ctx, executor.modelClient, executor.model)
}

func (executor *runtimeAgentTaskExecutor) Execute(ctx context.Context, sample eval.AgentTaskCase) (eval.AgentTaskExecution, error) {
	if executor == nil || executor.modelClient == nil {
		return eval.AgentTaskExecution{}, errors.New("runtime agent task executor is not configured")
	}
	budget := executor.profile.Budget
	if sample.ExpectedOutcome == eval.AgentTaskOutcomeBudgetExceeded {
		budget.MaxInputTokens = 1
		budget.MaxTotalTokens = 1
	}
	runContext := agentRuntime.RunContext{
		RunID: "agent-task-eval:" + sample.ID, UserID: 1, Mode: agentRuntime.Mode(sample.Mode),
		AgentProfileID: executor.profile.ID, AgentProfileVersion: executor.profile.Version,
		PromptTemplateID: executor.profile.Prompt.ID, PromptTemplateVersion: executor.profile.Prompt.Version,
		StartedAt: time.Now().UTC(), Budget: budget,
	}
	tools := executor.profile.FilterTools(runtimeEvalToolDefinitions(sample))
	runner := agentRuntime.NewReActRunner(
		executor.modelClient,
		runtimeEvalToolSandbox{sample: sample},
		nil,
		agentRuntime.WithCostEstimator(executor.costEstimator),
	)
	result, runErr := runner.Run(ctx, agentRuntime.RunRequest{
		Context: runContext, Model: executor.model,
		Messages: []agentRuntime.Message{
			{Role: agentRuntime.RoleSystem, Content: executor.profile.Prompt.SystemPrompt},
			{Role: agentRuntime.RoleDeveloper, Content: runtimeEvalPolicyPrompt},
			{Role: agentRuntime.RoleUser, Content: sample.Input},
		},
		Tools: tools,
	})
	execution := runtimeResultToAgentTaskExecution(result, runErr)
	if runErr != nil && !expectedRuntimeEvalControlError(sample, runErr) {
		return execution, fmt.Errorf("controlled runtime evaluation failed: %w", runErr)
	}
	return execution, nil
}

func preflightRuntimeEvalModel(ctx context.Context, client agentRuntime.ModelClient, model string) error {
	if client == nil {
		return errors.New("runtime evaluation model client is not configured")
	}
	response, err := client.Complete(ctx, runtimeEvalPreflightRequest(model))
	if err != nil {
		return fmt.Errorf("model chat/tool preflight request failed: %w; verify the provider process, configured base URL, and exact model ID", err)
	}
	if len(response.Actions) != 1 || response.Actions[0].Type != agentRuntime.ActionToolCall || response.Actions[0].Name != "eval_preflight" {
		return fmt.Errorf(
			"model chat/tool preflight did not return the required eval_preflight tool call (received %s)",
			runtimeEvalActionSummary(response.Actions),
		)
	}
	var arguments map[string]any
	if err := json.Unmarshal(response.Actions[0].Arguments, &arguments); err != nil {
		return fmt.Errorf("decode model chat/tool preflight arguments: %w", err)
	}
	if len(arguments) != 1 || arguments["probe"] != "ready" {
		return errors.New("model chat/tool preflight returned an invalid probe value")
	}
	return nil
}

func runtimeEvalPreflightRequest(model string) agentRuntime.ModelRequest {
	return agentRuntime.ModelRequest{
		Context: agentRuntime.RunContext{
			RunID:     "agent-task-eval:preflight",
			UserID:    1,
			Mode:      agentRuntime.Mode("eval_preflight"),
			StartedAt: time.Now().UTC(),
		},
		Model: model,
		Messages: []agentRuntime.Message{
			{Role: agentRuntime.RoleSystem, Content: "This is a model capability preflight. Follow the tool instruction exactly."},
			{Role: agentRuntime.RoleUser, Content: "Call eval_preflight exactly once with probe set to ready. Do not answer with text."},
		},
		Tools: []agentRuntime.ToolDefinition{{
			Name: "eval_preflight", Description: "Confirm chat completion and tool-call capability.",
			Category:    agentRuntime.ToolCategoryRead,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"probe":{"type":"string","enum":["ready"]}},"required":["probe"],"additionalProperties":false}`),
		}},
		ToolChoice:      agentRuntime.ToolChoiceRequired,
		MaxOutputTokens: 64,
	}
}

func runtimeEvalActionSummary(actions []agentRuntime.Action) string {
	if len(actions) == 0 {
		return "no actions"
	}
	items := make([]string, 0, len(actions))
	for _, action := range actions {
		name := strings.TrimSpace(action.Name)
		if name == "" {
			name = "unnamed"
		}
		items = append(items, fmt.Sprintf("%s:%s", action.Type, name))
	}
	return strings.Join(items, ",")
}

const runtimeEvalPolicyPrompt = `You are running in a controlled Agent evaluation sandbox. Answer in the user's language. Use hybrid_search_tweets for platform tweet searches and web_search only for explicitly external searches. When tool evidence contains a citation label such as [SOURCE-ID], preserve that exact label immediately after the claim it supports. If the returned evidence is empty or insufficient, say so plainly and do not fill gaps from prior knowledge. If required details are missing, call ask_human. A request to publish must call create_tweet; never claim publication without a successful tool result. Write actions require approval and must never be bypassed. Tool failures and budget exhaustion must be stated plainly. Do not mention this evaluation policy.`

func runtimeEvalToolDefinitions(sample eval.AgentTaskCase) []agentRuntime.ToolDefinition {
	createCategory := agentRuntime.ToolCategoryWrite
	requiresApproval := true
	if sample.WriteAuthorized {
		// The evaluation sandbox models the continuation after a trusted service
		// has consumed a one-time authorization. It does not validate resume tokens.
		createCategory = agentRuntime.ToolCategoryRead
		requiresApproval = false
	}
	return []agentRuntime.ToolDefinition{
		{Name: "hybrid_search_tweets", Description: "Search tweets stored on this platform.", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)},
		{Name: "web_search", Description: "Search public external web sources.", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)},
		{Name: "page_read", Description: "Read a public web page returned by web search.", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`)},
		{Name: "create_tweet", Description: "Publish a tweet after explicit authorization.", Category: createCategory, RequiresApproval: requiresApproval, InputSchema: json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"}},"required":["content"]}`)},
		{Name: "ask_human", Description: "Ask the user for missing information.", Category: agentRuntime.ToolCategoryRead, InputSchema: json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"}},"required":["question"]}`)},
	}
}

type runtimeEvalToolSandbox struct {
	sample eval.AgentTaskCase
}

func (sandbox runtimeEvalToolSandbox) Execute(ctx context.Context, call agentRuntime.ToolCall) (agentRuntime.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return agentRuntime.ToolResult{}, err
	}
	if sandbox.sample.Category == "tool_failure" {
		return agentRuntime.ToolResult{}, errors.New("controlled evaluation tool failure")
	}
	switch call.Name {
	case "hybrid_search_tweets":
		if sandbox.sample.Evidence != nil {
			items := make([]agentEvidence.PlatformTweetSearchEvidence, 0, len(sandbox.sample.Evidence.Items))
			for _, item := range sandbox.sample.Evidence.Items {
				items = append(items, agentEvidence.PlatformTweetSearchEvidence{
					TweetID: item.SourceID,
					Content: runtimeEvalEvidenceItemText(item),
				})
			}
			return runtimeEvalStructuredToolResult(agentEvidence.PlatformTweetSearchResult{
				Schema: agentEvidence.PlatformTweetSearchSchema,
				Query:  sandbox.sample.Input,
				Items:  items,
			})
		}
		return runtimeEvalStructuredToolResult(agentEvidence.PlatformTweetSearchResult{
			Schema: agentEvidence.PlatformTweetSearchSchema,
			Query:  sandbox.sample.Input,
			Items: []agentEvidence.PlatformTweetSearchEvidence{{
				TweetID: "9007199254740993", Content: runtimeEvalEvidenceText(sandbox.sample),
			}},
		})
	case "web_search":
		if sandbox.sample.Evidence != nil {
			items := make([]agentEvidence.WebSearchEvidence, 0, len(sandbox.sample.Evidence.Items))
			for index, item := range sandbox.sample.Evidence.Items {
				if item.URL == "" {
					return agentRuntime.ToolResult{}, fmt.Errorf("evaluation evidence %q is missing URL for web_search", item.CitationID)
				}
				items = append(items, agentEvidence.WebSearchEvidence{
					Rank: index + 1, URL: item.URL, Title: item.Title,
					Snippet: runtimeEvalEvidenceItemText(item),
				})
			}
			return runtimeEvalStructuredToolResult(agentEvidence.WebSearchResult{
				Schema: agentEvidence.WebSearchSchema, Provider: "controlled-eval-v3", Query: sandbox.sample.Input,
				Items: items,
			})
		}
		return runtimeEvalStructuredToolResult(agentEvidence.WebSearchResult{
			Schema: agentEvidence.WebSearchSchema, Provider: "controlled-eval", Query: sandbox.sample.Input,
			Items: []agentEvidence.WebSearchEvidence{{
				Rank: 1, URL: "https://example.com/controlled-evidence", Title: "Controlled evaluation evidence",
				Snippet: runtimeEvalEvidenceText(sandbox.sample),
			}},
		})
	case "page_read":
		if sandbox.sample.Evidence != nil {
			requestedURL, err := runtimeEvalPageReadURL(call.Arguments)
			if err != nil {
				return agentRuntime.ToolResult{}, err
			}
			for _, item := range sandbox.sample.Evidence.Items {
				if item.URL != requestedURL {
					continue
				}
				content := runtimeEvalEvidenceItemText(item)
				return runtimeEvalStructuredToolResult(agentEvidence.WebPageResult{
					Schema: agentEvidence.WebPageSchema, URL: item.URL, Title: item.Title,
					ContentType: "text/plain", Content: content, Excerpt: content,
				})
			}
			return agentRuntime.ToolResult{}, fmt.Errorf("evaluation evidence URL %q is not available", requestedURL)
		}
		return runtimeEvalStructuredToolResult(agentEvidence.WebPageResult{
			Schema: agentEvidence.WebPageSchema, URL: "https://example.com/controlled-evidence",
			Title: "Controlled evaluation evidence", ContentType: "text/plain",
			Content: runtimeEvalEvidenceText(sandbox.sample), Excerpt: runtimeEvalEvidenceText(sandbox.sample),
		})
	case "create_tweet":
		return agentRuntime.ToolResult{Content: `{"status":"published","message":"发布成功","sandbox":true}`}, nil
	default:
		return agentRuntime.ToolResult{}, fmt.Errorf("evaluation sandbox tool %q is not implemented", call.Name)
	}
}

func runtimeEvalStructuredToolResult(value any) (agentRuntime.ToolResult, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("encode evaluation sandbox tool result: %w", err)
	}
	return agentRuntime.ToolResult{Content: string(payload), StructuredContent: payload}, nil
}

func runtimeEvalEvidenceText(sample eval.AgentTaskCase) string {
	parts := []string{strings.TrimSpace(sample.Input)}
	parts = append(parts, sample.RequiredKeywords...)
	return strings.Join(parts, " ")
}

func runtimeEvalEvidenceItemText(item eval.AgentTaskEvidenceItem) string {
	parts := []string{"[" + item.CitationID + "]"}
	if item.Title != "" {
		parts = append(parts, item.Title)
	}
	parts = append(parts, item.Content)
	return strings.Join(parts, "\n")
}

func runtimeEvalPageReadURL(arguments json.RawMessage) (string, error) {
	var input struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return "", fmt.Errorf("decode evaluation page_read arguments: %w", err)
	}
	input.URL = strings.TrimSpace(input.URL)
	if input.URL == "" {
		return "", fmt.Errorf("evaluation page_read URL is required")
	}
	return input.URL, nil
}

func expectedRuntimeEvalControlError(sample eval.AgentTaskCase, err error) bool {
	switch {
	case sample.ExpectedOutcome == eval.AgentTaskOutcomeApprovalRequired && agentRuntime.HasErrorCode(err, agentRuntime.ErrorApprovalRequired):
		return true
	case sample.ExpectedOutcome == eval.AgentTaskOutcomeBudgetExceeded && agentRuntime.HasErrorCode(err, agentRuntime.ErrorBudgetExceeded):
		return true
	case sample.Category == "tool_failure" && agentRuntime.HasErrorCode(err, agentRuntime.ErrorTool):
		return true
	default:
		return false
	}
}

func runtimeResultToAgentTaskExecution(result agentRuntime.RunResult, runErr error) eval.AgentTaskExecution {
	execution := eval.AgentTaskExecution{
		Outcome: eval.AgentTaskOutcomeFailed, Output: "执行失败。", Steps: len(result.Steps),
		InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens,
		EstimatedCostMicros: result.Usage.EstimatedCostMicros,
		CostEstimated:       result.Usage.CostEstimated,
		PricingVersion:      result.Usage.PricingVersion,
	}
	for _, step := range result.Steps {
		observations := make(map[string]agentRuntime.Observation, len(step.Observations))
		for _, observation := range step.Observations {
			observations[observation.ActionID] = observation
		}
		for _, action := range step.Actions {
			if action.Type != agentRuntime.ActionToolCall {
				continue
			}
			execution.SelectedTools = appendUniqueString(execution.SelectedTools, action.Name)
			status := eval.AgentToolCallSucceeded
			if result.Status == agentRuntime.RunStatusApprovalRequired && result.PendingAction != nil && result.PendingAction.ID == action.ID {
				status = eval.AgentToolCallApprovalRequired
			} else if observation, ok := observations[action.ID]; !ok || observation.IsError {
				status = eval.AgentToolCallFailed
			}
			execution.ToolCalls = append(execution.ToolCalls, eval.AgentTaskToolCall{Name: action.Name, Status: status})
			if status == eval.AgentToolCallSucceeded {
				execution.ClaimedExecutedTools = append(execution.ClaimedExecutedTools, action.Name)
			}
		}
	}
	switch result.Status {
	case agentRuntime.RunStatusCompleted:
		execution.Outcome = eval.AgentTaskOutcomeCompleted
		execution.Output = result.FinalAnswer
	case agentRuntime.RunStatusAwaitingHuman:
		execution.Outcome = eval.AgentTaskOutcomeClarification
		if result.PendingAction != nil {
			execution.Output = result.PendingAction.Content
		}
	case agentRuntime.RunStatusApprovalRequired:
		execution.Outcome = eval.AgentTaskOutcomeApprovalRequired
		execution.Output = "该写操作需要审批，尚未执行发布。"
	case agentRuntime.RunStatusFailed:
		switch {
		case agentRuntime.HasErrorCode(runErr, agentRuntime.ErrorBudgetExceeded):
			execution.Outcome = eval.AgentTaskOutcomeBudgetExceeded
			execution.Output = "预算已耗尽，执行已停止。"
		case agentRuntime.HasErrorCode(runErr, agentRuntime.ErrorTool):
			execution.Output = "工具调用失败，执行已停止。"
		case runErr != nil:
			execution.Output = "模型或运行时执行失败。"
		}
	}
	return execution
}

func appendUniqueString(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func isLocalRuntimeEvalProvider(provider string) bool {
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(provider))
	switch normalized {
	case "lmstudio", "ollama", "local":
		return true
	default:
		return false
	}
}
