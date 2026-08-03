package rag

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

// TestCosineSimilarity 校验余弦相似度计算逻辑
func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		a      []float32
		b      []float32
		expect float64
	}{
		{[]float32{1, 0}, []float32{1, 0}, 1.0},
		{[]float32{1, 0}, []float32{0, 1}, 0.0},
		{[]float32{1, 1}, []float32{1, 1}, 1.0},
		{[]float32{1, 0}, []float32{-1, 0}, -1.0},
	}

	for _, tc := range tests {
		val := cosineSimilarity(tc.a, tc.b)
		if math.Abs(val-tc.expect) > 0.0001 {
			t.Errorf("cosineSimilarity(%v, %v) = %f; expect %f", tc.a, tc.b, val, tc.expect)
		}
	}
}

func TestParseEpisodicSummaryNormalizesStructuredJSON(t *testing.T) {
	raw := "```json\n{\"memory_type\":\"episodic\",\"summary\":\"Uses Go\",\"facts\":[\"Builds agents\",\"Builds agents\"],\"preferences\":[\"Concise answers\"],\"decisions\":[],\"followups\":[\"Add evals\"]}\n```"
	summary := parseEpisodicSummary(raw)
	if summary.MemoryType != "episodic" || len(summary.Facts) != 1 {
		t.Fatalf("unexpected structured summary: %#v", summary)
	}
	rendered := renderEpisodicSummary(summary)
	for _, expected := range []string{"Uses Go", "Facts: Builds agents", "Preferences: Concise answers", "Follow-ups: Add evals"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered summary %q missing %q", rendered, expected)
		}
	}
}

func TestParseEpisodicSummaryFallsBackToPlainText(t *testing.T) {
	summary := parseEpisodicSummary("durable preference: Go")
	if summary.MemoryType != "episodic" || summary.Summary != "durable preference: Go" {
		t.Fatalf("unexpected fallback summary: %#v", summary)
	}
}

func TestTruncateToTokenBudgetUsesCounter(t *testing.T) {
	counter := agentRuntime.NewHeuristicTokenCounter()
	got := truncateToTokenBudget("abcdefghijklmnopqrstuvwxyz", 4, counter)
	if got == "" || counter.CountText(got) > 4 {
		t.Fatalf("truncated text exceeds token budget: %q (%d)", got, counter.CountText(got))
	}
}

func TestBuildContextBlockAppliesTokenBudgetToPersona(t *testing.T) {
	manager := NewMemoryManager(nil, nil, nil, nil, "", "")
	block, chunks, err := manager.BuildContextBlock(context.Background(), 1, "query", strings.Repeat("中", 80), nil, 24)
	if err != nil {
		t.Fatalf("build context block: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected no retrieved chunks, got %d", len(chunks))
	}
	if tokens := manager.tokenCounter.CountText(block); tokens > 24 {
		t.Fatalf("context block uses %d tokens, limit 24: %q", tokens, block)
	}
}

// TestMemoryScoringFormula 校验 RAG 记忆层核心多维打分公式，尤其是指数时间半衰期衰减与画像加权
func TestMemoryScoringFormula(t *testing.T) {
	now := time.Now().Unix()
	scoring := DefaultScoringConfig()

	// 模拟两条记忆：一条是刚刚存入的，另一条是一年前存入的，具有相同的向量相似度
	chunkNew := MemoryChunk{
		Content:    "Go Microservices Architecture is awesome",
		Timestamp:  now,
		Similarity: 0.8,
		Source:     "episodic",
	}

	chunkOld := MemoryChunk{
		Content:    "Go Microservices Architecture is awesome",
		Timestamp:  now - 365*24*3600,
		Similarity: 0.8,
		Source:     "episodic",
	}

	// Case 1: 无画像命中的对比 (时间衰减验证)
	scoreNew, accepted := scoreMemoryChunk(chunkNew, now, nil, scoring)
	if !accepted {
		t.Fatal("expected recent episodic memory to pass threshold")
	}
	scoreOld, accepted := scoreMemoryChunk(chunkOld, now, nil, scoring)
	if !accepted {
		t.Fatal("expected old episodic memory to pass threshold")
	}

	if scoreNew.Score <= scoreOld.Score {
		t.Errorf("newer memories should score higher due to time decay: new=%f old=%f", scoreNew.Score, scoreOld.Score)
	}

	// Case 2: 画像加权验证
	// 命中 "Go" 画像关键词，获取加权奖励
	scoreWithPersona, _ := scoreMemoryChunk(chunkNew, now, []string{"go"}, scoring)
	if scoreWithPersona.Score <= scoreNew.Score || scoreWithPersona.Breakdown.Frequency <= 0 {
		t.Errorf("persona keyword should increase score: boosted=%#v standard=%#v", scoreWithPersona, scoreNew)
	}
}

func TestEpisodicUserFilterUsesStringTenantID(t *testing.T) {
	filter := episodicUserFilter(42)
	must := filter["must"].([]interface{})
	clause := must[0].(map[string]interface{})
	match := clause["match"].(map[string]interface{})
	if clause["key"] != "user_id" || match["value"] != "42" {
		t.Fatalf("unexpected episodic filter: %#v", filter)
	}
}

func TestScoreMemoryChunkRejectsLowSimilarity(t *testing.T) {
	_, accepted := scoreMemoryChunk(MemoryChunk{
		Content:    "unrelated memory",
		Similarity: 0.64,
		Source:     "episodic",
	}, time.Now().Unix(), nil, DefaultScoringConfig())
	if accepted {
		t.Fatal("expected low-similarity episodic memory to be rejected")
	}
}
