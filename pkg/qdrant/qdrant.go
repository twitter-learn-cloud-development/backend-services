package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client Qdrant REST 客户端
type Client struct {
	url    string
	client *http.Client
}

// SearchResult 搜索结果结构体
type SearchResult struct {
	ID      string                 // 原始推文 ID，从 Payload 中提取
	Score   float64                // 相似度得分
	Payload map[string]interface{} // 伴随数据
}

// NewClient 实例化 Qdrant 客户端，注入高并发池化 HTTPClient
func NewClient(url string) *Client {
	return &Client{
		url: url,
		client: &http.Client{
			Timeout: 3000 * time.Millisecond, // 限制最大查询时间 3s
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// ConvertSnowflakeToQdrantID 将 64位雪花 ID 转换为标准的 UUID 字符串
// 彻底解决 REST API JSON 序列化在 64位无符号大数上的精度截断溢出风险
func ConvertSnowflakeToQdrantID(snowflakeID uint64) string {
	return fmt.Sprintf("00000000-0000-0000-%04x-%012x", snowflakeID>>48, snowflakeID&0xFFFFFFFFFFFF)
}

// CreateCollection 幂等创建 Collection
func (c *Client) CreateCollection(ctx context.Context, name string, dim int) error {
	// 1. 检查 Collection 是否已经存在
	checkURL := fmt.Sprintf("%s/v1/collections/%s", c.url, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return fmt.Errorf("create collection check request failed: %w", err)
	}

	resp, err := c.client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			// Collection 已存在，直接返回
			return nil
		}
	}

	// 2. Collection 不存在，发起创建
	createURL := fmt.Sprintf("%s/v1/collections/%s", c.url, name)
	body := map[string]interface{}{
		"vectors": map[string]interface{}{
			"size":     dim,
			"distance": "Cosine",
		},
		"replication_factor":       2, // 🎯 设定 2 副本冗余，支持集群容灾，防止单点故障引发不可用
		"write_consistency_factor": 1, // 🎯 设定写入强一致性级别为 1，确保至少 1 个节点写入成功即返回
	}
	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal create collection body failed: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodPut, createURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return fmt.Errorf("create collection put request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err = c.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute create collection failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to create collection, status: %d, response: %s", resp.StatusCode, string(respBytes))
	}

	return nil
}

// UpsertPoint 插入或覆盖向量点，同时写入 metadata Payload
func (c *Client) UpsertPoint(ctx context.Context, collection string, pointID uint64, vector []float32, payload map[string]interface{}) error {
	uuidStr := ConvertSnowflakeToQdrantID(pointID)
	upsertURL := fmt.Sprintf("%s/v1/collections/%s/points?wait=true", c.url, collection)

	// 将真正的推文 ID 作为字符串塞入 payload 中，方便检索端直接无精度损耗拉取
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["tweet_id"] = fmt.Sprintf("%d", pointID)

	body := map[string]interface{}{
		"points": []map[string]interface{}{
			{
				"id":      uuidStr,
				"vector":  vector,
				"payload": payload,
			},
		},
	}

	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal upsert body failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, upsertURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return fmt.Errorf("create upsert request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("execute upsert point failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to upsert point, status: %d, response: %s", resp.StatusCode, string(respBytes))
	}

	return nil
}

type qdrantSearchResponse struct {
	Result []struct {
		ID      string                 `json:"id"`
		Score   float64                `json:"score"`
		Payload map[string]interface{} `json:"payload"`
	} `json:"result"`
}

// Search 进行向量相似度检索
func (c *Client) Search(ctx context.Context, collection string, vector []float32, limit int) ([]SearchResult, error) {
	searchURL := fmt.Sprintf("%s/v1/collections/%s/points/search", c.url, collection)

	body := map[string]interface{}{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
	}

	jsonBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal search body failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("create search request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute search failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read search response body failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search failed, status: %d, response: %s", resp.StatusCode, string(respBytes))
	}

	var searchResp qdrantSearchResponse
	if err := json.Unmarshal(respBytes, &searchResp); err != nil {
		return nil, fmt.Errorf("unmarshal search response failed: %w", err)
	}

	results := make([]SearchResult, 0, len(searchResp.Result))
	for _, res := range searchResp.Result {
		// 从 payload 里把先前保存的真正的推文 ID 提出来
		var tweetID string
		if tid, ok := res.Payload["tweet_id"].(string); ok {
			tweetID = tid
		} else {
			// 备用：如无直接回退使用 UUID 本身
			tweetID = res.ID
		}

		results = append(results, SearchResult{
			ID:      tweetID,
			Score:   res.Score,
			Payload: res.Payload,
		})
	}

	return results, nil
}
