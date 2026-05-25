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

// SiliconFlowReranker 硅基流动平台精排客户端
type SiliconFlowReranker struct {
	client *http.Client
	apiKey string
	url    string
	model  string
}

type SiliconFlowRerankRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            int      `json:"top_n,omitempty"`
	ReturnDocuments bool     `json:"return_documents"`
}

type SiliconFlowRerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func (s *SiliconFlowReranker) Rerank(ctx context.Context, query string, docs []Document) ([]RerankResult, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	docTexts := make([]string, len(docs))
	for i, doc := range docs {
		docTexts[i] = doc.Text
	}

	reqBody := SiliconFlowRerankRequest{
		Model:           s.model,
		Query:           query,
		Documents:       docTexts,
		TopN:            len(docs),
		ReturnDocuments: false,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("siliconflow rerank marshal error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("siliconflow rerank create request error: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("siliconflow rerank request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("siliconflow rerank read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("siliconflow rerank error status: %d, body: %s", resp.StatusCode, string(respBytes))
	}

	var rerankResp SiliconFlowRerankResponse
	if err := json.Unmarshal(respBytes, &rerankResp); err != nil {
		return nil, fmt.Errorf("siliconflow rerank unmarshal response failed: %w", err)
	}

	// 硅基流动的业务错误处理，如果存在 code 且非 0，则抛出错误
	if rerankResp.Code != 0 {
		return nil, fmt.Errorf("siliconflow rerank business error: code=%d, msg=%s", rerankResp.Code, rerankResp.Message)
	}

	results := make([]RerankResult, len(rerankResp.Results))
	for i, res := range rerankResp.Results {
		if res.Index < 0 || res.Index >= len(docs) {
			return nil, fmt.Errorf("siliconflow rerank returned invalid index: %d", res.Index)
		}
		results[i] = RerankResult{
			Document: docs[res.Index],
			Score:    res.RelevanceScore,
		}
	}

	// 按照得分降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}
