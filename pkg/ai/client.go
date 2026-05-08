package ai

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

type Client struct {
	api *openai.Client
}

// NewClient 初始化 AI 客户端连接 LM Studio
func NewClient(baseURL string) *Client {
	// LM Studio 本地调用不需要真实的 Token，随便填一个即可
	config := openai.DefaultConfig("lm-studio")
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}
	config.BaseURL = baseURL

	return &Client{
		api: openai.NewClientWithConfig(config),
	}
}

// GetEmbedding 调用 Jina 模型生成文本向量
func (c *Client) GetEmbedding(ctx context.Context, text string, model string) ([]float32, error) {
	req := openai.EmbeddingRequest{
		Input: []string{text},
		// ⚠️ 注意：这里的 Model 名字必须和你 LM Studio 里面加载的名字完全一致！
		// 如果报错说找不到模型，去 http://localhost:1234/v1/models 看一下 exact id
		Model: openai.EmbeddingModel(model),
	}

	resp, err := c.api.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create embedding failed: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	return resp.Data[0].Embedding, nil
}
