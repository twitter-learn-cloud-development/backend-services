package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	tweetv1 "twitter-clone/api/tweet/v1"
	agentModel "twitter-clone/internal/module/agent/model"
	agentObservability "twitter-clone/internal/module/agent/observability"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	"twitter-clone/internal/module/agent/workflow/guardrails"
	"twitter-clone/pkg/ai"
	platformTrace "twitter-clone/pkg/trace"
)

// LLMChatTool provides text generation for workflow LLM nodes.
type LLMChatTool struct {
	aiClient        *ai.Client
	model           string
	baseURL         string
	apiKey          string
	maxTokens       int
	credentials     CredentialResolver
	providerConfigs ProviderConfigResolver
	endpointPolicy  *agentModel.EndpointPolicy
	tokenCounter    agentRuntime.TokenCounter
	costEstimator   agentRuntime.CostEstimator
	traceRecorder   agentObservability.Recorder
	contentSampler  agentObservability.ContentSampler
}

type LLMChatToolOption func(*LLMChatTool)

func WithLLMCredentialResolver(resolver CredentialResolver) LLMChatToolOption {
	return func(tool *LLMChatTool) {
		tool.credentials = resolver
	}
}

func WithLLMProviderConfigResolver(resolver ProviderConfigResolver) LLMChatToolOption {
	return func(tool *LLMChatTool) {
		tool.providerConfigs = resolver
	}
}

func WithLLMEndpointPolicy(policy *agentModel.EndpointPolicy) LLMChatToolOption {
	return func(tool *LLMChatTool) {
		tool.endpointPolicy = policy
	}
}

func WithLLMTokenCounter(counter agentRuntime.TokenCounter) LLMChatToolOption {
	return func(tool *LLMChatTool) {
		if counter != nil {
			tool.tokenCounter = counter
		}
	}
}

func WithLLMCostEstimator(estimator agentRuntime.CostEstimator) LLMChatToolOption {
	return func(tool *LLMChatTool) {
		tool.costEstimator = estimator
	}
}

func WithLLMTraceRecorder(recorder agentObservability.Recorder) LLMChatToolOption {
	return func(tool *LLMChatTool) {
		tool.traceRecorder = recorder
	}
}

func WithLLMContentSampler(sampler agentObservability.ContentSampler) LLMChatToolOption {
	return func(tool *LLMChatTool) {
		tool.contentSampler = sampler
	}
}

func WithLLMConfiguredEndpoint(baseURL, apiKey string) LLMChatToolOption {
	return func(tool *LLMChatTool) {
		tool.baseURL = strings.TrimSpace(baseURL)
		tool.apiKey = apiKey
	}
}

func NewLLMChatTool(aiClient *ai.Client, model string) *LLMChatTool {
	return NewLLMChatToolWithOptions(aiClient, model)
}

func NewLLMChatToolWithOptions(aiClient *ai.Client, model string, options ...LLMChatToolOption) *LLMChatTool {
	tool := &LLMChatTool{
		aiClient:  aiClient,
		model:     model,
		maxTokens: 1024,
		credentials: NewEnvironmentCredentialResolver(map[string]string{
			"dashscope.default": "DASHSCOPE_API_KEY",
			"openai.default":    "OPENAI_API_KEY",
		}),
		endpointPolicy: agentModel.NewEndpointPolicy(strings.Split(os.Getenv("AGENT_LLM_ALLOWED_HOSTS"), ",")...),
		tokenCounter:   agentRuntime.NewHeuristicTokenCounter(),
	}
	for _, option := range options {
		if option != nil {
			option(tool)
		}
	}
	return tool
}

func NewLLMChatToolWithConfig(aiClient *ai.Client, model string, baseURL string, apiKey string) *LLMChatTool {
	allowedHost := endpointHost(baseURL)
	policyHosts := strings.Split(os.Getenv("AGENT_LLM_ALLOWED_HOSTS"), ",")
	policyHosts = append(policyHosts, allowedHost)
	return NewLLMChatToolWithOptions(
		aiClient,
		model,
		WithLLMConfiguredEndpoint(baseURL, apiKey),
		WithLLMEndpointPolicy(agentModel.NewEndpointPolicy(policyHosts...)),
	)
}

func (t *LLMChatTool) Name() string {
	return "LLMChat"
}

func (t *LLMChatTool) Description() string {
	return "Call an OpenAI-compatible LLM. User-managed endpoints use provider_config_id; environment credentials remain available through credential_ref."
}

func (t *LLMChatTool) InputSchema() string {
	return `{"type":"object","properties":{"prompt":{"type":"string"},"system_prompt":{"type":"string"},"provider_config_id":{"type":"string"},"provider":{"type":"string"},"base_url":{"type":"string"},"credential_ref":{"type":"string"},"model":{"type":"string"},"max_tokens":{"type":"number"}},"required":["prompt"]}`
}

func (t *LLMChatTool) Spec() ToolSpec {
	return ToolSpec{
		Name: t.Name(), Description: t.Description(), InputSchema: []byte(t.InputSchema()),
		Category: CategoryRead, Permission: PermissionAuthenticated,
		Timeout: 3 * time.Minute, Retry: RetryPolicy{MaxAttempts: 1}, Approval: ApprovalNever,
		SensitiveFields: []string{"prompt", "system_prompt", "api_key", "credential_ref"},
	}
}

func (t *LLMChatTool) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	if t.aiClient == nil {
		return nil, errors.New("ai client is not initialized")
	}

	prompt := stringInput(inputs, "prompt", "")
	if prompt == "" {
		return nil, errors.New("missing or invalid required input parameter 'prompt'")
	}

	systemPrompt := stringInput(inputs, "system_prompt", "You are a helpful assistant.")
	model := stringInput(inputs, "model", t.model)
	maxTokens := intInput(inputs, "max_tokens", t.maxTokens)

	resp, err := t.executeCompletion(ctx, inputs, systemPrompt, prompt, model, maxTokens)
	if err != nil {
		return nil, fmt.Errorf("llm completion error: %w", err)
	}

	return map[string]interface{}{"text": resp}, nil
}

func (t *LLMChatTool) executeCompletion(ctx context.Context, inputs map[string]interface{}, systemPrompt string, prompt string, model string, maxTokens int) (text string, err error) {
	provider := strings.ToLower(stringInput(inputs, "provider", ""))
	startedAt := time.Now()
	var usage agentRuntime.TokenUsage
	defer func() {
		t.recordCompletionTrace(ctx, systemPrompt, prompt, text, model, provider, usage, startedAt, err)
	}()
	baseURL := stringInput(inputs, "base_url", "")
	if strings.TrimSpace(stringInput(inputs, "api_key", "")) != "" {
		return "", errors.New("plaintext api_key is forbidden; use credential_ref")
	}
	apiKey := ""
	credentialRef := stringInput(inputs, "credential_ref", "")
	providerConfigID := stringInput(inputs, "provider_config_id", "")
	if providerConfigID != "" {
		if credentialRef != "" {
			return "", errors.New("provider_config_id cannot be combined with credential_ref")
		}
		if t.providerConfigs == nil {
			return "", errors.New("provider config resolver is not configured")
		}
		userID, err := requiredUint64Input(inputs, "user_id")
		if err != nil {
			return "", fmt.Errorf("resolve provider_config_id owner: %w", err)
		}
		resolved, err := t.providerConfigs.ResolveWorkflowProviderConfig(ctx, userID, providerConfigID)
		if err != nil {
			return "", fmt.Errorf("resolve provider_config_id: %w", err)
		}
		provider = strings.ToLower(strings.TrimSpace(resolved.Provider))
		baseURL = strings.TrimSpace(resolved.BaseURL)
		model = strings.TrimSpace(resolved.Model)
		apiKey = resolved.APIKey
	}
	if credentialRef != "" {
		if t.credentials == nil {
			return "", errors.New("credential resolver is not configured")
		}
		var err error
		apiKey, err = t.credentials.Resolve(ctx, credentialRef)
		if err != nil {
			return "", fmt.Errorf("resolve credential_ref: %w", err)
		}
	}

	if provider == "dashscope" {
		baseURL = firstNonEmpty(baseURL, os.Getenv("DASHSCOPE_API_URL"), "https://dashscope.aliyuncs.com/compatible-mode/v1")
		apiKey = firstNonEmpty(apiKey, os.Getenv("DASHSCOPE_API_KEY"))
	}
	if provider == "lmstudio" || provider == "lm-studio" {
		baseURL = firstNonEmpty(baseURL, t.baseURL, os.Getenv("LM_STUDIO_API_URL"), "http://localhost:1234/v1")
		apiKey = firstNonEmpty(apiKey, t.apiKey, "lm-studio")
	}
	if provider == "" {
		baseURL = firstNonEmpty(baseURL, t.baseURL)
		apiKey = firstNonEmpty(apiKey, t.apiKey)
	}
	if model == "" {
		return "", errors.New("missing LLM model; set provider_config_id or node properties.model")
	}
	reservation, err := t.reserveCompletionBudget(ctx, systemPrompt, prompt, model, maxTokens)
	if err != nil {
		return "", err
	}
	defer reservation.Release()

	if baseURL != "" {
		policy := t.endpointPolicy
		if policy == nil {
			policy = agentModel.NewEndpointPolicy()
		}
		if err := policy.Validate(baseURL, provider); err != nil {
			return "", fmt.Errorf("validate LLM base_url: %w", err)
		}
		if apiKey == "" {
			apiKey = "local"
		}
		cfg := openai.DefaultConfig(apiKey)
		cfg.BaseURL = baseURL
		cfg.HTTPClient = platformTrace.InstrumentHTTPClient(
			agentModel.NewRestrictedHTTPClient(policy, provider),
			"agent.provider.http",
			nil,
		)
		client := openai.NewClientWithConfig(cfg)
		resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: model,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
				{Role: openai.ChatMessageRoleUser, Content: prompt},
			},
			MaxTokens: maxTokens,
		})
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("empty response choices")
		}
		usage = agentRuntime.TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		}
		usage, err = t.completeUsage(ctx, model, systemPrompt, prompt, resp.Choices[0].Message.Content, usage)
		if err != nil {
			return "", err
		}
		if err := reservation.Commit(usage); err != nil {
			return "", err
		}
		text = resp.Choices[0].Message.Content
		return text, nil
	}

	text, err = t.aiClient.GetChatCompletion(ctx, systemPrompt, prompt, model)
	if err != nil {
		return "", err
	}
	usage, err = t.completeUsage(ctx, model, systemPrompt, prompt, text, agentRuntime.TokenUsage{})
	if err != nil {
		return "", err
	}
	if err := reservation.Commit(usage); err != nil {
		return "", err
	}
	return text, nil
}

func (t *LLMChatTool) recordCompletionTrace(
	ctx context.Context,
	systemPrompt, prompt, completion, model, provider string,
	usage agentRuntime.TokenUsage,
	startedAt time.Time,
	callErr error,
) {
	if t == nil || t.traceRecorder == nil {
		return
	}
	metadata := ExecutionMetadataFromContext(ctx)
	userID, ok := guardrails.AuthenticatedUserID(ctx)
	if !ok || userID == 0 || metadata.RunID == "" || metadata.StepID == "" {
		return
	}
	finishedAt := time.Now()
	status := "success"
	if callErr != nil {
		status = "failed"
	}
	promptHash, promptLength := workflowPromptDigest(systemPrompt, prompt)
	completionHash, completionLength := workflowTextDigest(completion)
	promptSample := sampleWorkflowTraceContent(
		t.contentSampler, "prompt:"+metadata.RunID+":"+metadata.StepID+":"+promptHash,
		systemPrompt+"\n\n"+prompt,
	)
	completionSample := sampleWorkflowTraceContent(
		t.contentSampler, "completion:"+metadata.RunID+":"+metadata.StepID+":"+completionHash, completion,
	)
	templateID, templateVersion := workflowPromptTemplate(metadata)
	record := agentObservability.LLMCallRecord{
		RecordID: metadata.RunID + ":llm:" + metadata.StepID,
		RunID:    metadata.RunID, WorkflowID: metadata.WorkflowID,
		UserID: userID, Source: string(firstExecutionSource(metadata.Source, SourceWorkflow)),
		StepID: metadata.StepID, Model: model, Provider: provider, Status: status,
		ErrorClass: workflowLLMErrorClass(callErr), PromptHash: promptHash, PromptLength: promptLength,
		PromptTemplateID: templateID, PromptTemplateVersion: templateVersion,
		PromptSample: promptSample.Value, PromptSampleStatus: promptSample.Status,
		CompletionHash: completionHash, CompletionLength: completionLength,
		CompletionSample: completionSample.Value, CompletionSampleStatus: completionSample.Status,
		ContentSamplePolicy: promptSample.Policy,
		Usage: agentObservability.TokenUsage{
			InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens,
			Estimated: usage.Estimated, EstimatedCostMicros: usage.EstimatedCostMicros,
			CostEstimated: usage.CostEstimated, PricingVersion: usage.PricingVersion,
		},
		StartedAt: startedAt, FinishedAt: finishedAt, DurationMS: finishedAt.Sub(startedAt).Milliseconds(), UpdatedAt: finishedAt,
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := t.traceRecorder.RecordLLMCall(persistCtx, record); err != nil {
		slog.WarnContext(persistCtx, "persist workflow LLM trace failed", "run_id", metadata.RunID, "step_id", metadata.StepID, "error", err)
	}
}

func sampleWorkflowTraceContent(sampler agentObservability.ContentSampler, key, content string) agentObservability.ContentSample {
	if sampler == nil {
		return agentObservability.ContentSample{
			Status: agentObservability.ContentSampleStatusDisabled,
			Policy: agentObservability.ContentSamplePolicyDisabled,
		}
	}
	return sampler.Sample(key, content)
}

func workflowPromptTemplate(metadata ExecutionMetadata) (string, string) {
	templateID := "workflow.node." + metadata.StepID
	if metadata.WorkflowID != "" {
		templateID = "workflow." + metadata.WorkflowID + ".node." + metadata.StepID
	}
	version := metadata.WorkflowRevisionID
	if version == "" && metadata.WorkflowRevisionNumber > 0 {
		version = fmt.Sprintf("revision-%d", metadata.WorkflowRevisionNumber)
	}
	return templateID, version
}

func workflowPromptDigest(systemPrompt, prompt string) (string, int) {
	value := systemPrompt + "\x00" + prompt
	return workflowTextDigest(value)
}

func workflowTextDigest(value string) (string, int) {
	if value == "" {
		return "", 0
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:]), len(value)
}

func workflowLLMErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if agentRuntime.HasErrorCode(err, agentRuntime.ErrorBudgetExceeded) {
		return string(agentRuntime.ErrorBudgetExceeded)
	}
	return "provider_error"
}

func (t *LLMChatTool) reserveCompletionBudget(
	ctx context.Context,
	systemPrompt, prompt, model string,
	maxTokens int,
) (*agentRuntime.UsageReservation, error) {
	counter := t.tokenCounter
	if counter == nil {
		counter = agentRuntime.NewHeuristicTokenCounter()
	}
	estimate := counter.EstimateRequest(agentRuntime.ModelRequest{
		Model: model,
		Messages: []agentRuntime.Message{
			{Role: agentRuntime.RoleSystem, Content: systemPrompt},
			{Role: agentRuntime.RoleUser, Content: prompt},
		},
		MaxOutputTokens: maxTokens,
	})
	estimate.OutputTokens = maxTokens
	estimate.TotalTokens = estimate.InputTokens + maxTokens
	if tracker, ok := agentRuntime.BudgetTrackerFromContext(ctx); ok && tracker.Budget().MaxEstimatedCostMicros > 0 {
		if t.costEstimator == nil {
			return nil, errors.New("workflow cost budget requires a model cost estimator")
		}
		cost, err := t.costEstimator.EstimateCost(model, estimate)
		if err != nil {
			return nil, fmt.Errorf("estimate workflow LLM reservation cost: %w", err)
		}
		estimate.EstimatedCostMicros = cost.Micros
		estimate.CostEstimated = true
		estimate.PricingVersion = cost.PricingVersion
	}
	return agentRuntime.ReserveBudgetUsage(ctx, estimate)
}

func (t *LLMChatTool) completeUsage(
	ctx context.Context,
	model, systemPrompt, prompt, content string,
	usage agentRuntime.TokenUsage,
) (agentRuntime.TokenUsage, error) {
	if usage.TotalTokens <= 0 {
		counter := t.tokenCounter
		if counter == nil {
			counter = agentRuntime.NewHeuristicTokenCounter()
		}
		requestUsage := counter.EstimateRequest(agentRuntime.ModelRequest{
			Model: model,
			Messages: []agentRuntime.Message{
				{Role: agentRuntime.RoleSystem, Content: systemPrompt},
				{Role: agentRuntime.RoleUser, Content: prompt},
			},
		})
		responseUsage := counter.EstimateResponse(agentRuntime.ModelResponse{
			Message: agentRuntime.Message{Role: agentRuntime.RoleAssistant, Content: content},
		})
		usage = requestUsage
		usage.Add(responseUsage)
		usage.Estimated = true
	}
	tracker, tracked := agentRuntime.BudgetTrackerFromContext(ctx)
	requiresCost := tracked && tracker.Budget().MaxEstimatedCostMicros > 0
	if requiresCost && t.costEstimator == nil {
		return agentRuntime.TokenUsage{}, errors.New("workflow cost budget requires a model cost estimator")
	}
	if requiresCost {
		cost, err := t.costEstimator.EstimateCost(model, usage)
		if err != nil {
			return agentRuntime.TokenUsage{}, fmt.Errorf("estimate workflow LLM cost: %w", err)
		}
		usage.EstimatedCostMicros = cost.Micros
		usage.CostEstimated = usage.Estimated
		usage.PricingVersion = cost.PricingVersion
	}
	return usage, nil
}

func endpointHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// PublishTweetTool publishes tweets through the tweet-service gRPC boundary.
type PublishTweetTool struct {
	tweetClient tweetv1.TweetServiceClient
}

func NewPublishTweetTool(tweetClient tweetv1.TweetServiceClient) *PublishTweetTool {
	return &PublishTweetTool{tweetClient: tweetClient}
}

func (t *PublishTweetTool) Name() string {
	return "PublishTweet"
}

func (t *PublishTweetTool) Description() string {
	return "Publish content through tweet-service. Inputs include content, authenticated user_id, optional max_chars, and overflow_strategy."
}

func (t *PublishTweetTool) InputSchema() string {
	return `{"type":"object","properties":{"content":{"type":"string"},"max_chars":{"type":"integer"},"overflow_strategy":{"type":"string","enum":["error","truncate"]}},"required":["content"]}`
}

func (t *PublishTweetTool) Spec() ToolSpec {
	return ToolSpec{
		Name: t.Name(), Description: t.Description(), InputSchema: []byte(t.InputSchema()),
		Category: CategoryWrite, Permission: PermissionAuthenticated,
		Timeout: 15 * time.Second, Retry: RetryPolicy{MaxAttempts: 1},
		Idempotency: IdempotencyPolicy{Required: true}, Approval: ApprovalRequired,
		SensitiveFields: []string{"content"},
	}
}

func (t *PublishTweetTool) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	if t.tweetClient == nil {
		return nil, errors.New("tweet gRPC client is not initialized")
	}

	content, ok := inputs["content"].(string)
	if !ok || content == "" {
		return nil, errors.New("missing or invalid required parameter 'content'")
	}
	content = strings.TrimSpace(content)

	maxChars := intInput(inputs, "max_chars", 0)
	if maxChars > 0 && len([]rune(content)) > maxChars {
		switch strings.ToLower(stringInput(inputs, "overflow_strategy", "error")) {
		case "truncate":
			content = truncateRunes(content, maxChars)
		default:
			return nil, fmt.Errorf("content exceeds node max_chars limit: %d characters", maxChars)
		}
	}

	userIDRaw := inputs["user_id"]
	var userID uint64
	switch v := userIDRaw.(type) {
	case string:
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid user_id format: %w", err)
		}
		userID = id
	case float64:
		userID = uint64(v)
	case int:
		userID = uint64(v)
	case uint64:
		userID = v
	default:
		return nil, fmt.Errorf("unsupported user_id type: %T", userIDRaw)
	}

	resp, err := t.tweetClient.CreateTweet(ctx, &tweetv1.CreateTweetRequest{
		UserId:         userID,
		Content:        content,
		IdempotencyKey: ExecutionMetadataFromContext(ctx).IdempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to call TweetService.CreateTweet gRPC: %w", err)
	}

	return map[string]interface{}{
		"tweet_id": resp.Tweet.Id,
		"status":   "success",
	}, nil
}

// WebSearchTool keeps the persisted Workflow component name stable while
// delegating provider behavior to the websearch adapter.
type WebSearchTool struct {
	provider agentWebSearch.Provider
}

func NewWebSearchTool(providers ...agentWebSearch.Provider) *WebSearchTool {
	var provider agentWebSearch.Provider
	if len(providers) > 0 {
		provider = providers[0]
	}
	return &WebSearchTool{provider: provider}
}

func (t *WebSearchTool) Name() string {
	return "WebSearch"
}

func (t *WebSearchTool) Description() string {
	return "Search public web content for the provided query."
}

func (t *WebSearchTool) InputSchema() string {
	return `{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":400},"count":{"type":"number","minimum":1,"maximum":10},"provider_config_id":{"type":"string","maxLength":64}},"required":["query"]}`
}

func (t *WebSearchTool) Spec() ToolSpec {
	return ToolSpec{
		Name: t.Name(), Description: t.Description(), InputSchema: []byte(t.InputSchema()),
		Category: CategoryRead, Permission: PermissionAuthenticated,
		Timeout:         20 * time.Second,
		Retry:           RetryPolicy{MaxAttempts: 2, InitialBackoff: 100 * time.Millisecond, MaxBackoff: 500 * time.Millisecond},
		Approval:        ApprovalNever,
		SensitiveFields: []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	query, ok := inputs["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return nil, errors.New("missing or invalid parameter 'query'")
	}
	if t == nil || t.provider == nil {
		return nil, agentWebSearch.ErrUnavailable
	}
	result, err := t.provider.Search(ctx, agentWebSearch.Request{
		Query:            query,
		Limit:            intInput(inputs, "count", agentWebSearch.DefaultResultLimit),
		Subject:          webAccessSubjectFromContext(ctx),
		ProviderConfigID: stringInput(inputs, "provider_config_id", ""),
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"schema":   result.Schema,
		"provider": result.Provider,
		"query":    result.Query,
		"items":    result.Items,
		"results":  agentWebSearch.FormatForModel(result),
	}, nil
}

// PageReadTool reads bounded public page text as untrusted evidence. It is
// intentionally separate from WebSearch so a workflow must opt in to the
// additional network request and budget charge.
type PageReadTool struct {
	reader agentWebSearch.PageReader
}

func NewPageReadTool(readers ...agentWebSearch.PageReader) *PageReadTool {
	var reader agentWebSearch.PageReader
	if len(readers) > 0 {
		reader = readers[0]
	}
	return &PageReadTool{reader: reader}
}

func (t *PageReadTool) Name() string {
	return "PageRead"
}

func (t *PageReadTool) Description() string {
	return "Read bounded visible text from a public web page as untrusted evidence."
}

func (t *PageReadTool) InputSchema() string {
	return `{"type":"object","properties":{"url":{"type":"string","minLength":1,"maxLength":2048},"max_runes":{"type":"number","minimum":1,"maximum":32000}},"required":["url"]}`
}

func (t *PageReadTool) Spec() ToolSpec {
	return ToolSpec{
		Name: t.Name(), Description: t.Description(), InputSchema: []byte(t.InputSchema()),
		Category: CategoryRead, Permission: PermissionAuthenticated,
		Timeout:         20 * time.Second,
		Retry:           RetryPolicy{MaxAttempts: 2, InitialBackoff: 100 * time.Millisecond, MaxBackoff: 500 * time.Millisecond},
		Approval:        ApprovalNever,
		SensitiveFields: []string{"url"},
	}
}

func (t *PageReadTool) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	rawURL, ok := inputs["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return nil, errors.New("missing or invalid parameter 'url'")
	}
	if t == nil || t.reader == nil {
		return nil, agentWebSearch.ErrPageUnavailable
	}
	result, err := t.reader.Read(ctx, agentWebSearch.PageRequest{
		URL:      rawURL,
		MaxRunes: intInput(inputs, "max_runes", agentWebSearch.DefaultMaxPageRunes),
		Subject:  webAccessSubjectFromContext(ctx),
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"schema":       result.Schema,
		"url":          result.URL,
		"title":        result.Title,
		"content_type": result.ContentType,
		"content":      result.Content,
		"excerpt":      result.Excerpt,
		"truncated":    result.Truncated,
		"safety":       result.Safety,
		"results":      agentWebSearch.FormatPageForModel(result),
	}, nil
}

func webAccessSubjectFromContext(ctx context.Context) agentWebSearch.AccessSubject {
	userID, _ := guardrails.AuthenticatedUserID(ctx)
	return agentWebSearch.AccessSubject{
		UserID: userID,
		RunID:  ExecutionMetadataFromContext(ctx).RunID,
	}
}

func stringInput(inputs map[string]interface{}, key string, fallback string) string {
	value, ok := inputs[key]
	if !ok || value == nil {
		return fallback
	}
	str, ok := value.(string)
	if !ok || strings.TrimSpace(str) == "" {
		return fallback
	}
	return strings.TrimSpace(str)
}

func intInput(inputs map[string]interface{}, key string, fallback int) int {
	value, ok := inputs[key]
	if !ok || value == nil {
		return fallback
	}
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case string:
		parsed, err := strconv.Atoi(v)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func requiredUint64Input(inputs map[string]interface{}, key string) (uint64, error) {
	value, ok := inputs[key]
	if !ok || value == nil {
		return 0, fmt.Errorf("missing %s", key)
	}
	switch typed := value.(type) {
	case uint64:
		if typed > 0 {
			return typed, nil
		}
	case int:
		if typed > 0 {
			return uint64(typed), nil
		}
	case float64:
		if typed > 0 {
			return uint64(typed), nil
		}
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err == nil && parsed > 0 {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("invalid %s", key)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncateRunes(value string, max int) string {
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
