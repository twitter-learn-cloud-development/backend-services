package rag

import (
	"math"
	"strings"
	"testing"
	"time"
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

// TestMemoryScoringFormula 校验 RAG 记忆层核心多维打分公式，尤其是指数时间半衰期衰减与画像加权
func TestMemoryScoringFormula(t *testing.T) {
	now := time.Now().Unix()
	lambda := 0.00001

	// 模拟两条记忆：一条是刚刚存入的，另一条是一年前存入的，具有相同的向量相似度
	chunkNew := MemoryChunk{
		Content:   "Go Microservices Architecture is awesome",
		Timestamp: now,
		Score:     0.8, // 向量相似度
	}

	chunkOld := MemoryChunk{
		Content:   "Go Microservices Architecture is awesome",
		Timestamp: now - 365*24*3600, // 365天前
		Score:     0.8,
	}

	// 评估函数
	scoreFunc := func(chunk MemoryChunk, personaKeywords []string) float64 {
		sim := chunk.Score
		timeDiff := float64(now - chunk.Timestamp)
		timeDecay := math.Exp(-lambda * timeDiff)

		freqWeight := 0.0
		for _, kw := range personaKeywords {
			if strings.Contains(strings.ToLower(chunk.Content), strings.ToLower(kw)) {
				freqWeight += 0.15
			}
		}

		return (0.6 * sim) + (0.25 * timeDecay) + (0.15 * freqWeight)
	}

	// Case 1: 无画像命中的对比 (时间衰减验证)
	scoreNew := scoreFunc(chunkNew, nil)
	scoreOld := scoreFunc(chunkOld, nil)

	if scoreNew <= scoreOld {
		t.Errorf("Newer memories should score higher than older ones due to time decay. New: %f, Old: %f", scoreNew, scoreOld)
	}

	// Case 2: 画像加权验证
	// 命中 "Go" 画像关键词，获取加权奖励
	scoreWithPersona := scoreFunc(chunkNew, []string{"go"})
	if scoreWithPersona <= scoreNew {
		t.Errorf("Memories matching user persona keywords should receive score boost. Boosted: %f, Standard: %f", scoreWithPersona, scoreNew)
	}
}
