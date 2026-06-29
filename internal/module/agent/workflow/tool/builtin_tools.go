package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sashabaranov/go-openai"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/pkg/ai"
)

// LLMChatTool provides text generation for workflow LLM nodes.
type LLMChatTool struct {
	aiClient  *ai.Client
	model     string
	baseURL   string
	apiKey    string
	maxTokens int
}

func NewLLMChatTool(aiClient *ai.Client, model string) *LLMChatTool {
	return &LLMChatTool{
		aiClient:  aiClient,
		model:     model,
		maxTokens: 1024,
	}
}

func NewLLMChatToolWithConfig(aiClient *ai.Client, model string, baseURL string, apiKey string) *LLMChatTool {
	return &LLMChatTool{
		aiClient:  aiClient,
		model:     model,
		baseURL:   baseURL,
		apiKey:    apiKey,
		maxTokens: 1024,
	}
}

func (t *LLMChatTool) Name() string {
	return "LLMChat"
}

func (t *LLMChatTool) Description() string {
	return "Call an OpenAI-compatible LLM. Inputs support prompt, system_prompt, provider, base_url, api_key, model and max_tokens."
}

func (t *LLMChatTool) InputSchema() string {
	return `{"type":"object","properties":{"prompt":{"type":"string"},"system_prompt":{"type":"string"},"provider":{"type":"string"},"base_url":{"type":"string"},"api_key":{"type":"string"},"model":{"type":"string"},"max_tokens":{"type":"number"}},"required":["prompt"]}`
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
	if model == "" {
		return nil, errors.New("missing LLM model; set node properties.model or LM_STUDIO_MODEL_CHAT")
	}
	maxTokens := intInput(inputs, "max_tokens", t.maxTokens)

	resp, err := t.executeCompletion(ctx, inputs, systemPrompt, prompt, model, maxTokens)
	if err != nil {
		return nil, fmt.Errorf("llm completion error: %w", err)
	}

	return map[string]interface{}{"text": resp}, nil
}

func (t *LLMChatTool) executeCompletion(ctx context.Context, inputs map[string]interface{}, systemPrompt string, prompt string, model string, maxTokens int) (string, error) {
	provider := strings.ToLower(stringInput(inputs, "provider", ""))
	baseURL := stringInput(inputs, "base_url", "")
	apiKey := stringInput(inputs, "api_key", "")

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

	if baseURL != "" {
		if apiKey == "" {
			apiKey = "local"
		}
		cfg := openai.DefaultConfig(apiKey)
		cfg.BaseURL = baseURL
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
		return resp.Choices[0].Message.Content, nil
	}

	return t.aiClient.GetChatCompletion(ctx, systemPrompt, prompt, model)
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
	return `{"type":"object","properties":{"content":{"type":"string"},"user_id":{"type":"string"},"max_chars":{"type":"integer"},"overflow_strategy":{"type":"string","enum":["error","truncate"]}},"required":["content","user_id"]}`
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
		UserId:  userID,
		Content: content,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to call TweetService.CreateTweet gRPC: %w", err)
	}

	return map[string]interface{}{
		"tweet_id": resp.Tweet.Id,
		"status":   "success",
	}, nil
}

// WebSearchTool is a placeholder search integration for workflow testing.
type WebSearchTool struct{}

func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{}
}

func (t *WebSearchTool) Name() string {
	return "WebSearch"
}

func (t *WebSearchTool) Description() string {
	return "Search public web content for the provided query."
}

func (t *WebSearchTool) InputSchema() string {
	return `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`
}

func (t *WebSearchTool) Execute(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
	query, ok := inputs["query"].(string)
	if !ok || query == "" {
		return nil, errors.New("missing or invalid parameter 'query'")
	}

	mockResults := fmt.Sprintf("Mock web search results for '%s'. Replace WebSearchTool with a production search provider when available.", query)
	return map[string]interface{}{"results": mockResults}, nil
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
