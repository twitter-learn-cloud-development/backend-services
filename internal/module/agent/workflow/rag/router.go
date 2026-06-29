package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"twitter-clone/pkg/ai"
)

type Intent string

const (
	IntentPersonaMemory   Intent = "persona_memory"
	IntentEpisodicMemory  Intent = "episodic"
	IntentGlobalKnowledge Intent = "global"
	IntentDirectAction    Intent = "direct_action"
)

type CascadeRouter struct {
	aiClient        *ai.Client
	model           string
	lexicalRules    map[Intent][]string
	semanticAnchors map[Intent][]float32
}

func NewCascadeRouter(aiClient *ai.Client, model string) *CascadeRouter {
	return &CascadeRouter{
		aiClient: aiClient,
		model:    model,
		lexicalRules: map[Intent][]string{
			IntentPersonaMemory: {
				"\u6211\u7684\u504f\u597d", "\u6211\u7684\u4e60\u60ef", "\u6211\u7684\u6280\u672f\u6808", "\u6211\u7684\u8d44\u6599",
				"\u6211\u662f\u8c01", "\u6211\u559c\u6b22", "\u8bb0\u4f4f\u6211",
				"persona", "preference", "profile",
			},
			IntentEpisodicMemory: {
				"\u521a\u624d", "\u4e0a\u6b21", "\u4e0a\u4e00\u8f6e", "\u4e4b\u524d\u804a",
				"\u6211\u4eec\u804a\u8fc7", "\u5386\u53f2\u4f1a\u8bdd", "\u8bb0\u5f97\u5417", "\u524d\u9762\u8bf4",
				"last time", "previous", "earlier",
			},
			IntentGlobalKnowledge: {
				"\u4ec0\u4e48\u662f", "\u5b9a\u4e49", "\u539f\u7406", "\u6587\u6863", "\u8d44\u6599",
				"\u641c\u7d22", "\u67e5\u8be2", "\u6700\u65b0", "\u5bf9\u6bd4",
				"what is", "search", "docs",
			},
			IntentDirectAction: {
				"\u53d1\u5e03", "\u53d1\u63a8", "\u521b\u5efa\u63a8\u6587", "\u4fdd\u5b58",
				"\u6267\u884c\u5de5\u4f5c\u6d41", "\u8fd0\u884c\u5de5\u4f5c\u6d41",
				"publish", "create tweet", "run workflow",
			},
		},
		semanticAnchors: make(map[Intent][]float32),
	}
}

func (r *CascadeRouter) InitSemanticAnchors(ctx context.Context, embeddingModel string) error {
	if r == nil || r.aiClient == nil {
		return nil
	}

	definitions := map[Intent]string{
		IntentPersonaMemory:   "user preferences, technical stack, long-term habits, profile and personalization",
		IntentEpisodicMemory:  "previous conversations, recent discussion, historical context and user memory",
		IntentGlobalKnowledge: "public knowledge, factual information, technical docs, tweets and external context",
		IntentDirectAction:    "explicit action execution such as publishing, saving, creating content or running workflows",
	}

	for intent, text := range definitions {
		vec, err := r.aiClient.GetEmbedding(ctx, text, embeddingModel)
		if err != nil {
			return fmt.Errorf("generate semantic anchor for %s failed: %w", intent, err)
		}
		r.semanticAnchors[intent] = vec
	}
	return nil
}

func (r *CascadeRouter) Route(ctx context.Context, query string, embeddingModel string) (Intent, string, error) {
	if r == nil {
		return IntentGlobalKnowledge, query, nil
	}

	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return IntentGlobalKnowledge, query, nil
	}

	for intent, keywords := range r.lexicalRules {
		for _, keyword := range keywords {
			if strings.Contains(normalized, strings.ToLower(keyword)) {
				return intent, query, nil
			}
		}
	}

	if r.aiClient != nil && len(r.semanticAnchors) > 0 {
		queryVec, err := r.aiClient.GetEmbedding(ctx, query, embeddingModel)
		if err == nil {
			bestIntent := IntentGlobalKnowledge
			bestScore := -1.0
			for intent, anchorVec := range r.semanticAnchors {
				score := cosineSimilarity(queryVec, anchorVec)
				if score > bestScore {
					bestScore = score
					bestIntent = intent
				}
			}
			if bestScore >= 0.82 {
				return bestIntent, query, nil
			}
		}
	}

	if r.aiClient == nil {
		return IntentGlobalKnowledge, query, nil
	}

	systemPrompt := `You are an intent router inside an agent workflow engine. Output JSON only, no Markdown.
Allowed intent values:
- persona_memory: user profile, preferences, technical stack, long-term habits
- episodic: previous conversations, recent context, historical memory
- global: public knowledge, docs, facts, retrieval
- direct_action: explicit action execution such as publishing, saving, or running workflow
Output format: {"intent":"global","rewritten_query":"rewritten retrieval query"}`

	resp, err := r.aiClient.GetChatCompletion(ctx, systemPrompt, query, r.model)
	if err != nil {
		return IntentGlobalKnowledge, query, nil
	}

	var parsed struct {
		Intent         string `json:"intent"`
		RewrittenQuery string `json:"rewritten_query"`
	}
	cleaned := strings.TrimSpace(resp)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return IntentGlobalKnowledge, query, nil
	}

	intent := normalizeIntent(parsed.Intent)
	rewritten := strings.TrimSpace(parsed.RewrittenQuery)
	if rewritten == "" {
		rewritten = query
	}
	return intent, rewritten, nil
}

func normalizeIntent(raw string) Intent {
	switch Intent(strings.TrimSpace(raw)) {
	case IntentPersonaMemory:
		return IntentPersonaMemory
	case IntentEpisodicMemory:
		return IntentEpisodicMemory
	case IntentGlobalKnowledge:
		return IntentGlobalKnowledge
	case IntentDirectAction:
		return IntentDirectAction
	default:
		return IntentGlobalKnowledge
	}
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
