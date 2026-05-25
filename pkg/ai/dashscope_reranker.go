package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
)

// DashScopeReranker 阿里百炼官方精排客户端
type DashScopeReranker struct {
	client *http.Client
	apiKey string
	url    string
	model  string
}

type DashScopeRerankRequest struct {
	Model  string                `json:"model"`
	Input  DashScopeRerankInput  `json:"input"`
	Params DashScopeRerankParams `json:"params,omitempty"`
}

type DashScopeRerankInput struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type DashScopeRerankParams struct {
	TopN int `json:"top_n,omitempty"`
}

type DashScopeRerankResponse struct {
	Output struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
	} `json:"output"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (d *DashScopeReranker) Rerank(ctx context.Context, query string, docs []Document) ([]RerankResult, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	docTexts := make([]string, len(docs))
	for i, doc := range docs {
		docTexts[i] = doc.Text
	}

	reqBody := DashScopeRerankRequest{
		Model: d.model,
		Input: DashScopeRerankInput{
			Query:     query,
			Documents: docTexts,
		},
		Params: DashScopeRerankParams{
			TopN: len(docs),
		},
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("dashscope rerank marshal error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("dashscope rerank create request error: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dashscope rerank request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dashscope rerank read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dashscope rerank error status: %d, body: %s", resp.StatusCode, string(respBytes))
	}

	var rerankResp DashScopeRerankResponse
	if err := json.Unmarshal(respBytes, &rerankResp); err != nil {
		return nil, fmt.Errorf("dashscope rerank unmarshal response failed: %w", err)
	}

	if rerankResp.Code != "" {
		return nil, fmt.Errorf("dashscope rerank business error: code=%s, msg=%s", rerankResp.Code, rerankResp.Message)
	}

	results := make([]RerankResult, len(rerankResp.Output.Results))
	for i, res := range rerankResp.Output.Results {
		if res.Index < 0 || res.Index >= len(docs) {
			return nil, fmt.Errorf("dashscope rerank returned invalid index: %d", res.Index)
		}
		results[i] = RerankResult{
			Document: docs[res.Index],
			Score:    res.RelevanceScore,
		}
	}

	// 按照相关性得分降序排列
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}
