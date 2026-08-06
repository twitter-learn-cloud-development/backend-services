package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"twitter-clone/internal/module/agent/eval"
	agentModel "twitter-clone/internal/module/agent/model"

	"github.com/sashabaranov/go-openai"
)

type liveProviderConfig struct {
	Provider                     string
	BaseURL                      string
	APIKey                       string
	Model                        string
	InputMicrosPerMillionTokens  int64
	OutputMicrosPerMillionTokens int64
	PricingVersion               string
}

type liveRouterClientConfig struct {
	Embedding *liveProviderConfig
	LLM       *liveProviderConfig
	Timeout   time.Duration
}

type liveRouterClient struct {
	embeddingAPI    *openai.Client
	llmAPI          *openai.Client
	embeddingConfig *liveProviderConfig
	llmConfig       *liveProviderConfig

	mu             sync.Mutex
	embeddingUsage eval.RouterProviderReport
	llmUsage       eval.RouterProviderReport
}

func newLiveRouterClient(config liveRouterClientConfig) (*liveRouterClient, error) {
	if config.Embedding == nil && config.LLM == nil {
		return nil, errors.New("at least one live router provider is required")
	}
	client := &liveRouterClient{}
	var err error
	if config.Embedding != nil {
		client.embeddingConfig = cloneLiveProviderConfig(config.Embedding)
		client.embeddingAPI, err = newOpenAICompatibleAPI(*client.embeddingConfig, config.Timeout)
		if err != nil {
			return nil, fmt.Errorf("configure embedding provider: %w", err)
		}
		client.embeddingUsage = providerReport(*client.embeddingConfig)
	}
	if config.LLM != nil {
		client.llmConfig = cloneLiveProviderConfig(config.LLM)
		client.llmAPI, err = newOpenAICompatibleAPI(*client.llmConfig, config.Timeout)
		if err != nil {
			return nil, fmt.Errorf("configure LLM provider: %w", err)
		}
		client.llmUsage = providerReport(*client.llmConfig)
	}
	return client, nil
}

func newOpenAICompatibleAPI(config liveProviderConfig, timeout time.Duration) (*openai.Client, error) {
	config.Provider = strings.TrimSpace(config.Provider)
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.Model = strings.TrimSpace(config.Model)
	if config.Provider == "" || config.BaseURL == "" || config.APIKey == "" || config.Model == "" {
		return nil, errors.New("provider, base URL, API key and model are required")
	}
	policy := agentModel.NewEndpointPolicy()
	if err := policy.Validate(config.BaseURL, config.Provider); err != nil {
		return nil, err
	}
	httpClient := agentModel.NewRestrictedHTTPClient(policy, config.Provider)
	if timeout > 0 {
		httpClient.Timeout = timeout
	}
	openAIConfig := openai.DefaultConfig(config.APIKey)
	openAIConfig.BaseURL = config.BaseURL
	openAIConfig.HTTPClient = httpClient
	return openai.NewClientWithConfig(openAIConfig), nil
}

func (client *liveRouterClient) GetEmbedding(ctx context.Context, text, model string) ([]float32, error) {
	if client == nil || client.embeddingAPI == nil || client.embeddingConfig == nil {
		return nil, errors.New("live embedding provider is not configured")
	}
	model = firstNonEmpty(model, client.embeddingConfig.Model)
	client.recordRequest(true)
	response, err := client.embeddingAPI.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.EmbeddingModel(model),
	})
	if err != nil {
		client.recordFailure(true)
		return nil, err
	}
	if len(response.Data) == 0 {
		client.recordFailure(true)
		return nil, errors.New("empty embedding response")
	}
	client.recordUsage(true, response.Usage)
	return response.Data[0].Embedding, nil
}

func (client *liveRouterClient) GetChatCompletion(ctx context.Context, systemPrompt, userPrompt, model string) (string, error) {
	if client == nil || client.llmAPI == nil || client.llmConfig == nil {
		return "", errors.New("live LLM provider is not configured")
	}
	model = firstNonEmpty(model, client.llmConfig.Model)
	client.recordRequest(false)
	response, err := client.llmAPI.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		},
		MaxTokens: 256,
	})
	if err != nil {
		client.recordFailure(false)
		return "", err
	}
	if len(response.Choices) == 0 {
		client.recordFailure(false)
		return "", errors.New("empty chat completion response")
	}
	client.recordUsage(false, response.Usage)
	return response.Choices[0].Message.Content, nil
}

func (client *liveRouterClient) reports() (eval.RouterProviderReport, eval.RouterProviderReport) {
	if client == nil {
		return eval.RouterProviderReport{}, eval.RouterProviderReport{}
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.embeddingUsage, client.llmUsage
}

func (client *liveRouterClient) recordRequest(embedding bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if embedding {
		client.embeddingUsage.Requests++
		return
	}
	client.llmUsage.Requests++
}

func (client *liveRouterClient) recordFailure(embedding bool) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if embedding {
		client.embeddingUsage.FailedRequests++
		return
	}
	client.llmUsage.FailedRequests++
}

func (client *liveRouterClient) recordUsage(embedding bool, usage openai.Usage) {
	client.mu.Lock()
	defer client.mu.Unlock()
	report := &client.llmUsage
	config := client.llmConfig
	if embedding {
		report = &client.embeddingUsage
		config = client.embeddingConfig
	}
	total := usage.TotalTokens
	if total <= 0 {
		total = usage.PromptTokens + usage.CompletionTokens
	}
	report.InputTokens += usage.PromptTokens
	report.OutputTokens += usage.CompletionTokens
	report.TotalTokens += total
	if config != nil {
		report.EstimatedCostMicros += conservativeCost(usage.PromptTokens, config.InputMicrosPerMillionTokens)
		report.EstimatedCostMicros += conservativeCost(usage.CompletionTokens, config.OutputMicrosPerMillionTokens)
	}
}

func providerReport(config liveProviderConfig) eval.RouterProviderReport {
	return eval.RouterProviderReport{
		Provider:       strings.TrimSpace(config.Provider),
		Endpoint:       strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
		Model:          strings.TrimSpace(config.Model),
		PricingVersion: strings.TrimSpace(config.PricingVersion),
	}
}

func cloneLiveProviderConfig(config *liveProviderConfig) *liveProviderConfig {
	if config == nil {
		return nil
	}
	clone := *config
	return &clone
}

func conservativeCost(tokens int, microsPerMillion int64) int64 {
	if tokens <= 0 || microsPerMillion <= 0 {
		return 0
	}
	if int64(tokens) > math.MaxInt64/microsPerMillion {
		return math.MaxInt64
	}
	product := int64(tokens) * microsPerMillion
	if product > math.MaxInt64-999_999 {
		return math.MaxInt64
	}
	return (product + 999_999) / 1_000_000
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
