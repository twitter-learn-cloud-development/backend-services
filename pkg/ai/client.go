package ai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	platformTrace "twitter-clone/pkg/trace"

	"github.com/sashabaranov/go-openai"
)

type Client struct {
	cheapAPI   *openai.Client
	premiumAPI *openai.Client
}

// NewClient 初始化 AI 客户端连接，支持传入 cheapBaseURL，并从环境变量自适应 premium 配置
func NewClient(cheapBaseURL string) *Client {
	return NewClientWithConfig(cheapBaseURL, "", nil)
}

// NewClientWithConfig creates an OpenAI-compatible client with an explicit
// token and HTTP client. The default constructor remains intentionally
// compatible with existing local LM Studio callers.
func NewClientWithConfig(cheapBaseURL, cheapToken string, httpClient *http.Client) *Client {
	// 1. 初始化廉价模型客户端 (本地 LM Studio 或免费 API)
	cheapConfig := openai.DefaultConfig(cheapToken)
	if cheapBaseURL == "" {
		cheapBaseURL = "http://localhost:1234/v1"
	}
	cheapConfig.BaseURL = cheapBaseURL
	if httpClient == nil {
		httpClient = platformTrace.InstrumentHTTPClient(nil, "agent.provider.http", nil)
	}
	cheapConfig.HTTPClient = httpClient
	cheapAPI := openai.NewClientWithConfig(cheapConfig)

	// 2. 初始化高端/昂贵模型客户端 (如百炼、OpenAI)
	premiumBaseURL := os.Getenv("PREMIUM_AI_BASE_URL")
	premiumToken := os.Getenv("PREMIUM_AI_TOKEN")
	if premiumToken == "" {
		premiumToken = os.Getenv("DASHSCOPE_API_KEY") // 阿里百炼 Key 兼容
	}
	if premiumBaseURL == "" {
		premiumBaseURL = os.Getenv("DASHSCOPE_API_URL")
	}

	var premiumAPI *openai.Client
	if premiumBaseURL != "" && premiumToken != "" {
		premiumConfig := openai.DefaultConfig(premiumToken)
		premiumConfig.BaseURL = premiumBaseURL
		premiumConfig.HTTPClient = httpClient
		premiumAPI = openai.NewClientWithConfig(premiumConfig)
	} else {
		// 若未配置 premium, 则回退使用 cheapAPI 作为 fallback
		premiumAPI = cheapAPI
	}

	return &Client{
		cheapAPI:   cheapAPI,
		premiumAPI: premiumAPI,
	}
}

// GetEmbedding 调用 Jina 模型生成文本向量
func (c *Client) GetEmbedding(ctx context.Context, text string, model string) ([]float32, error) {
	// 🆕 压测 Mock 挡板
	if javaMock := ctx.Value("MOCK_EMBEDDING"); javaMock == "true" || ctx.Value("MOCK_EMBEDDING") == true || os.Getenv("MOCK_EMBEDDING") == "true" {
		mockVec := make([]float32, 1024)
		var hash uint32 = 5381
		for i := 0; i < len(text); i++ {
			hash = ((hash << 5) + hash) + uint32(text[i])
		}
		for i := 0; i < 1024; i++ {
			val := float32((hash+uint32(i*17))%100)/1000.0 + 0.01
			mockVec[i] = val
		}
		return mockVec, nil
	}

	req := openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.EmbeddingModel(model),
	}

	// 向量模型通常使用 cheap API
	resp, err := c.cheapAPI.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create embedding failed: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	return resp.Data[0].Embedding, nil
}

// GetChatCompletion 简单包装，默认使用廉价模型，向下兼容
func (c *Client) GetChatCompletion(ctx context.Context, systemPrompt, userPrompt string, model string) (string, error) {
	// 🆕 压测 Mock 挡板
	if os.Getenv("MOCK_EMBEDDING") == "true" {
		if strings.Contains(userPrompt, "Alert") || strings.Contains(systemPrompt, "自愈") || strings.Contains(systemPrompt, "RCA") {
			return `### 1. 告警现状与影响评估
发现接口错误率陡增。

### 2. 疑似根本原因 (Root Cause)
Redis 连接失败导致缓存获取异常，引起下游服务雪崩。

### 3. 推荐紧急止血与根治措施
立即触发特定接口熔断，并重启 Redis 服务。

[STRUCT_START]
{
  "root_cause": "RedisDown",
  "action": "TriggerCircuitBreaker",
  "resource": "GET:/api/v1/feeds"
}
[STRUCT_END]`, nil
		}
		return "【🔥 舆情快报】关于本话题的最新动态：各方讨论热烈，系统正持续监控中。欢迎分享你的看法！", nil
	}

	return c.doRequest(ctx, c.cheapAPI, model, systemPrompt, userPrompt, nil)
}

// GetChatCompletionWithRouting 带熔断降级的 AI 路由器，支持传入心跳汇报 callback
func (c *Client) GetChatCompletionWithRouting(
	ctx context.Context,
	systemPrompt,
	userPrompt string,
	modelCheap,
	modelPremium string,
	complexity string,
	onProgress func(string),
) (string, error) {
	// 🆕 压测 Mock 挡板
	if os.Getenv("MOCK_EMBEDDING") == "true" {
		if strings.Contains(userPrompt, "Alert") || strings.Contains(systemPrompt, "自愈") || strings.Contains(systemPrompt, "RCA") {
			if strings.Contains(userPrompt, "TweetV2Bug") || strings.Contains(userPrompt, "tweet-service-vs") || strings.Contains(userPrompt, "gray") {
				return `### 1. 告警现状与影响评估
检测到新发布的 tweet-service:v2 产生大量空指针异常。

### 2. 疑似根本原因 (Root Cause)
v2 版本代码存在局部漏洞。

### 3. 推荐紧急止血与根治措施
执行灰度切流自愈，把所有流量切回 v1。

[STRUCT_START]
{
  "root_cause": "TweetV2Bug",
  "action": "UpdateGrayTraffic",
  "resource": "tweet-service-vs",
  "weights": {
    "v1": 100,
    "v2": 0
  }
}
[STRUCT_END]`, nil
			}
			return `### 1. 告警现状与影响评估
发现接口错误率陡增。

### 2. 疑似根本原因 (Root Cause)
Redis 连接失败导致缓存获取异常，引起下游服务雪崩。

### 3. 推荐紧急止血与根治措施
立即触发特定接口熔断，并重启 Redis 服务。

[STRUCT_START]
{
  "root_cause": "RedisDown",
  "action": "TriggerCircuitBreaker",
  "resource": "GET:/api/v1/feeds"
}
[STRUCT_END]`, nil
		}
		return "【🔥 舆情快报】关于本话题的最新动态：各方讨论热烈，系统正持续监控中。欢迎分享你的看法！", nil
	}

	// 1. 默认路由至廉价模型
	targetAPI := c.cheapAPI
	targetModel := modelCheap

	// 长度或复杂度跳级路由
	tokenCount := len(systemPrompt) + len(userPrompt)
	if complexity == "High" || tokenCount > 4000 {
		targetAPI = c.premiumAPI
		targetModel = modelPremium
	}

	// 2. 发起首次请求
	resp, err := c.doRequest(ctx, targetAPI, targetModel, systemPrompt, userPrompt, onProgress)
	if err == nil {
		return resp, nil
	}

	// 3. 限流 429 / 500 / 超时等可重试错误触发降级 Failover
	if c.isRetryableError(err) && targetModel == modelCheap {
		fmt.Printf("⚠️ [Failover Triggered] Cheap model failed (%v). Falling back to Premium Model: %s\n", err, modelPremium)
		// 切换为 Premium 模型进行重试，继承相同的 onProgress 心跳接口
		resp, failoverErr := c.doRequest(ctx, c.premiumAPI, modelPremium, systemPrompt, userPrompt, onProgress)
		if failoverErr != nil {
			return "", fmt.Errorf("both cheap and premium models failed: %w (fallback err: %v)", err, failoverErr)
		}
		return resp, nil
	}

	return "", err
}

// doRequest 执行具体的 API 调用，并自适应支持流式（Stream）与非流式返回
func (c *Client) doRequest(
	ctx context.Context,
	api *openai.Client,
	model,
	systemPrompt,
	userPrompt string,
	onProgress func(string),
) (string, error) {
	if api == nil {
		return "", errors.New("openai client is nil")
	}

	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: systemPrompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		MaxTokens: 256,
	}

	// 如果传入了进度回调，则启用 Stream 模式，并提供心跳钩子
	if onProgress != nil {
		stream, err := api.CreateChatCompletionStream(ctx, req)
		if err != nil {
			return "", err
		}
		defer stream.Close()

		var sb strings.Builder
		for {
			response, err := stream.Recv()
			if err != nil {
				if err.Error() == "EOF" {
					break
				}
				return "", err
			}
			if len(response.Choices) > 0 {
				content := response.Choices[0].Delta.Content
				sb.WriteString(content)
				onProgress(content)
			}
		}
		return sb.String(), nil
	}

	resp, err := api.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("empty response choices")
	}
	return resp.Choices[0].Message.Content, nil
}

// isRetryableError 识别当前错误是否适合重新尝试（如 429、5xx 错误、连接超时）
func (c *Client) isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		// HTTP 429 Rate Limit 和 5xx Server Error 适合 failover 重试
		if apiErr.HTTPStatusCode == 429 || apiErr.HTTPStatusCode >= 500 {
			return true
		}
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	errStr := err.Error()
	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "connection") || strings.Contains(errStr, "EOF") {
		return true
	}

	return false
}
