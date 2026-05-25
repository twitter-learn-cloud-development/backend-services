package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLocalMockReranker_Rerank(t *testing.T) {
	reranker := &LocalMockReranker{}
	ctx := context.Background()

	// 候选文档
	docs := []Document{
		{ID: "1", Text: "我喜欢吃苹果，苹果很好吃。"},
		{ID: "2", Text: "今天天气真不错，出去散散步。"},
		{ID: "3", Text: "苹果不喜欢吃我，这很有趣。"}, // 语义与 query 差异较大，且字词顺序不同
		{ID: "4", Text: "我喜欢吃苹果。"},      // 语义与 query 最为相似
	}

	query := "我喜欢吃苹果"

	results, err := reranker.Rerank(ctx, query, docs)
	if err != nil {
		t.Fatalf("local rerank failed: %v", err)
	}

	if len(results) != len(docs) {
		t.Fatalf("expected %d results, got %d", len(docs), len(results))
	}

	// 1. 验证排序：最接近 query 的 "4" 和 "1" 应该排在最前面
	// "我喜欢吃苹果。" (ID: 4) 应该拥有最高的相似度分数
	if results[0].Document.ID != "4" {
		t.Errorf("expected top result to be ID 4, got ID %s (score: %f)", results[0].Document.ID, results[0].Score)
	}

	// 2. 验证 Bigram 区分机制以规避零分词退化：
	// “我喜欢吃苹果” (ID: 4) 应该比 “苹果不喜欢吃我” (ID: 3) 拥有明显更高分
	var scoreID4, scoreID3 float64
	for _, res := range results {
		if res.Document.ID == "4" {
			scoreID4 = res.Score
		} else if res.Document.ID == "3" {
			scoreID3 = res.Score
		}
	}

	t.Logf("Score for ID 4 (我喜欢吃苹果): %f", scoreID4)
	t.Logf("Score for ID 3 (苹果不喜欢吃我): %f", scoreID3)

	if scoreID4 <= scoreID3 {
		t.Errorf("expected score of ID 4 (%f) to be strictly greater than ID 3 (%f)", scoreID4, scoreID3)
	}
}

func TestNewReranker_Factory(t *testing.T) {
	// 测试本地降级器实例化
	reranker := NewReranker("local", "", "", "")
	if _, ok := reranker.(*LocalMockReranker); !ok {
		t.Errorf("expected LocalMockReranker, got %T", reranker)
	}

	// 测试空值回退
	rerankerDefault := NewReranker("", "", "", "")
	if _, ok := rerankerDefault.(*LocalMockReranker); !ok {
		t.Errorf("expected LocalMockReranker for empty type, got %T", rerankerDefault)
	}

	// 测试 dashscope 实例类型
	rerankerDS := NewReranker("dashscope", "mock-key", "", "")
	if ds, ok := rerankerDS.(*DashScopeReranker); !ok {
		t.Errorf("expected DashScopeReranker, got %T", rerankerDS)
	} else {
		if ds.model != "gte-rerank" {
			t.Errorf("expected default model to be gte-rerank, got %s", ds.model)
		}
	}

	// 测试 siliconflow 实例类型
	rerankerSF := NewReranker("siliconflow", "mock-key", "", "")
	if sf, ok := rerankerSF.(*SiliconFlowReranker); !ok {
		t.Errorf("expected SiliconFlowReranker, got %T", rerankerSF)
	} else {
		if sf.model != "BAAI/bge-reranker-v2-m3" {
			t.Errorf("expected default model to be BAAI/bge-reranker-v2-m3, got %s", sf.model)
		}
	}
}

func TestReranker_Timeout(t *testing.T) {
	// 1. 创建一个模拟的 HTTP Server，故意引入 2 秒的网络延迟，超出 Reranker 1.5s 强制超时阈值
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	// 2. 实例化精排器，指定该延迟的测试服务端点
	reranker := NewReranker("dashscope", "mock-key", server.URL, "gte-rerank")

	// 3. 执行调用，断言触发超时返回
	docs := []Document{
		{ID: "1", Text: "测试推文内容"},
	}
	ctx := context.Background()

	_, err := reranker.Rerank(ctx, "测试问题", docs)
	if err == nil {
		t.Fatal("expected timeout error, but got nil")
	}

	t.Logf("Successfully captured expected error: %v", err)

	// 验证错误中包含超时或截止时间超出
	errStr := strings.ToLower(err.Error())
	if !strings.Contains(errStr, "timeout") &&
		!strings.Contains(errStr, "deadline") &&
		!strings.Contains(errStr, "canceled") {
		t.Errorf("expected error message to contain 'timeout' or 'deadline', got: %v", err)
	}
}
