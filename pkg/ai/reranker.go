package ai

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Document 待重排的文档结构，包含ID与内容文本
type Document struct {
	ID   string
	Text string
}

// RerankResult 重排后的结果，包含原文档与相关性得分
type RerankResult struct {
	Document Document
	Score    float64
}

// Reranker 精排重排序接口
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []Document) ([]RerankResult, error)
}

// 🎯 全局复用高性能 HTTP 客户端，防止高并发下 Socket 枯竭
var sharedHTTPClient = &http.Client{
	Timeout: 1500 * time.Millisecond, // 严格限制三方精排超时为 1.5s
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	},
}

// NewReranker 实例化工厂方法，支持 dashscope, siliconflow 真实接口以及本地 Mock 降级器
func NewReranker(rerankerType, apiKey, apiURL, modelName string) Reranker {
	switch strings.ToLower(rerankerType) {
	case "dashscope":
		if apiURL == "" {
			apiURL = "https://dashscope.aliyuncs.com/api/v1/services/rerank/text/rerank"
		}
		if modelName == "" {
			modelName = "gte-rerank"
		}
		return &DashScopeReranker{
			client: sharedHTTPClient,
			apiKey: apiKey,
			url:    apiURL,
			model:  modelName,
		}
	case "siliconflow":
		if apiURL == "" {
			apiURL = "https://api.siliconflow.cn/v1/rerank"
		}
		if modelName == "" {
			modelName = "BAAI/bge-reranker-v2-m3"
		}
		return &SiliconFlowReranker{
			client: sharedHTTPClient,
			apiKey: apiKey,
			url:    apiURL,
			model:  modelName,
		}
	default:
		return &LocalMockReranker{}
	}
}

// LocalMockReranker 本地降级重排实现，基于 Bigram 的 Jaccard 相似度匹配
// 规避了直接字符分割在中文环境下退化为无序字符命中的漏洞
type LocalMockReranker struct{}

func (l *LocalMockReranker) Rerank(ctx context.Context, query string, docs []Document) ([]RerankResult, error) {
	results := make([]RerankResult, len(docs))
	for i, doc := range docs {
		score := jaccardSimilarity(query, doc.Text)
		results[i] = RerankResult{
			Document: doc,
			Score:    score,
		}
	}

	// 降序排列
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// toBigrams 将文本分割成长度为 2 的字符切片集合，模拟简易的 N-gram 中文分词
func toBigrams(text string) map[string]struct{} {
	runes := []rune(strings.ToLower(text))
	bigrams := make(map[string]struct{})
	if len(runes) < 2 {
		if len(runes) == 1 {
			bigrams[string(runes)] = struct{}{}
		}
		return bigrams
	}
	for i := 0; i < len(runes)-1; i++ {
		bigrams[string(runes[i:i+2])] = struct{}{}
	}
	return bigrams
}

// jaccardSimilarity 计算 Bigram 集合的交并比
func jaccardSimilarity(text1, text2 string) float64 {
	set1 := toBigrams(text1)
	set2 := toBigrams(text2)
	if len(set1) == 0 || len(set2) == 0 {
		return 0.0
	}
	intersection := 0
	for k := range set1 {
		if _, ok := set2[k]; ok {
			intersection++
		}
	}
	union := len(set1) + len(set2) - intersection
	return float64(intersection) / float64(union)
}
