package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type Intent string

const (
	IntentPersonaMemory   Intent = "persona_memory"
	IntentEpisodicMemory  Intent = "episodic"
	IntentGlobalKnowledge Intent = "global"
	IntentDirectAction    Intent = "direct_action"
)

type RouteStage string

const (
	RouteStageLexical     RouteStage = "lexical"
	RouteStageSemantic    RouteStage = "semantic"
	RouteStageLLMFallback RouteStage = "llm_fallback"
	RouteStageDefault     RouteStage = "default"
)

type RouteDecision struct {
	Intent         Intent
	RewrittenQuery string
	Stage          RouteStage
	SemanticScore  float64
	SemanticError  string
	LLMError       string
}

type RouterEmbeddingClient interface {
	GetEmbedding(context.Context, string, string) ([]float32, error)
}

type RouterChatClient interface {
	GetChatCompletion(context.Context, string, string, string) (string, error)
}

type RouterAIClient interface {
	RouterEmbeddingClient
	RouterChatClient
}

type CascadeRouterConfig struct {
	EmbeddingClient   RouterEmbeddingClient
	ChatClient        RouterChatClient
	ChatModel         string
	SemanticThreshold float64
}

type lexicalRule struct {
	Intent  Intent
	Keyword string
}

var cascadeIntentPriority = []Intent{
	IntentEpisodicMemory,
	IntentPersonaMemory,
	IntentDirectAction,
	IntentGlobalKnowledge,
}

type CascadeRouter struct {
	embeddingClient   RouterEmbeddingClient
	chatClient        RouterChatClient
	chatModel         string
	semanticThreshold float64
	lexicalRules      []lexicalRule
	semanticAnchors   map[Intent][]float32
}

func NewCascadeRouter(aiClient RouterAIClient, model string) *CascadeRouter {
	return NewCascadeRouterWithConfig(CascadeRouterConfig{
		EmbeddingClient: aiClient,
		ChatClient:      aiClient,
		ChatModel:       model,
	})
}

func NewCascadeRouterWithConfig(config CascadeRouterConfig) *CascadeRouter {
	semanticThreshold := config.SemanticThreshold
	if semanticThreshold <= 0 || semanticThreshold > 1 {
		semanticThreshold = 0.82
	}
	return &CascadeRouter{
		embeddingClient:   config.EmbeddingClient,
		chatClient:        config.ChatClient,
		chatModel:         strings.TrimSpace(config.ChatModel),
		semanticThreshold: semanticThreshold,
		lexicalRules: []lexicalRule{
			// Temporal phrases take precedence over generic knowledge/action words.
			{Intent: IntentEpisodicMemory, Keyword: "\u521a\u624d"},
			{Intent: IntentEpisodicMemory, Keyword: "\u4e0a\u6b21"},
			{Intent: IntentEpisodicMemory, Keyword: "\u4e0a\u4e00\u8f6e"},
			{Intent: IntentEpisodicMemory, Keyword: "\u4e4b\u524d\u804a"},
			{Intent: IntentEpisodicMemory, Keyword: "\u6211\u4eec\u804a\u8fc7"},
			{Intent: IntentEpisodicMemory, Keyword: "\u5386\u53f2\u4f1a\u8bdd"},
			{Intent: IntentEpisodicMemory, Keyword: "\u8bb0\u5f97\u5417"},
			{Intent: IntentEpisodicMemory, Keyword: "\u524d\u9762\u8bf4"},
			{Intent: IntentEpisodicMemory, Keyword: "last time"},
			{Intent: IntentEpisodicMemory, Keyword: "previous"},
			{Intent: IntentEpisodicMemory, Keyword: "earlier"},
			{Intent: IntentEpisodicMemory, Keyword: "recall"},

			{Intent: IntentPersonaMemory, Keyword: "\u6211\u7684\u504f\u597d"},
			{Intent: IntentPersonaMemory, Keyword: "\u6211\u7684\u4e60\u60ef"},
			{Intent: IntentPersonaMemory, Keyword: "\u6211\u7684\u6280\u672f\u6808"},
			{Intent: IntentPersonaMemory, Keyword: "\u6211\u7684\u8d44\u6599"},
			{Intent: IntentPersonaMemory, Keyword: "\u6211\u662f\u8c01"},
			{Intent: IntentPersonaMemory, Keyword: "\u6211\u559c\u6b22"},
			{Intent: IntentPersonaMemory, Keyword: "\u8bb0\u4f4f\u6211"},
			{Intent: IntentPersonaMemory, Keyword: "persona"},
			{Intent: IntentPersonaMemory, Keyword: "preference"},
			{Intent: IntentPersonaMemory, Keyword: "profile"},
			{Intent: IntentPersonaMemory, Keyword: "technical stack"},
			{Intent: IntentPersonaMemory, Keyword: "remember me"},

			{Intent: IntentDirectAction, Keyword: "\u53d1\u5e03"},
			{Intent: IntentDirectAction, Keyword: "\u53d1\u63a8"},
			{Intent: IntentDirectAction, Keyword: "\u521b\u5efa\u63a8\u6587"},
			{Intent: IntentDirectAction, Keyword: "\u4fdd\u5b58"},
			{Intent: IntentDirectAction, Keyword: "\u6267\u884c\u5de5\u4f5c\u6d41"},
			{Intent: IntentDirectAction, Keyword: "\u8fd0\u884c\u5de5\u4f5c\u6d41"},
			{Intent: IntentDirectAction, Keyword: "publish"},
			{Intent: IntentDirectAction, Keyword: "create tweet"},
			{Intent: IntentDirectAction, Keyword: "run workflow"},

			{Intent: IntentGlobalKnowledge, Keyword: "\u4ec0\u4e48\u662f"},
			{Intent: IntentGlobalKnowledge, Keyword: "\u5b9a\u4e49"},
			{Intent: IntentGlobalKnowledge, Keyword: "\u539f\u7406"},
			{Intent: IntentGlobalKnowledge, Keyword: "\u6587\u6863"},
			{Intent: IntentGlobalKnowledge, Keyword: "\u8d44\u6599"},
			{Intent: IntentGlobalKnowledge, Keyword: "\u641c\u7d22"},
			{Intent: IntentGlobalKnowledge, Keyword: "\u67e5\u8be2"},
			{Intent: IntentGlobalKnowledge, Keyword: "\u6700\u65b0"},
			{Intent: IntentGlobalKnowledge, Keyword: "\u5bf9\u6bd4"},
			{Intent: IntentGlobalKnowledge, Keyword: "what is"},
			{Intent: IntentGlobalKnowledge, Keyword: "define"},
			{Intent: IntentGlobalKnowledge, Keyword: "search"},
			{Intent: IntentGlobalKnowledge, Keyword: "docs"},
		},
		semanticAnchors: make(map[Intent][]float32),
	}
}

func (r *CascadeRouter) InitSemanticAnchors(ctx context.Context, embeddingModel string) error {
	if r == nil || r.embeddingClient == nil {
		return nil
	}

	definitions := map[Intent]string{
		IntentPersonaMemory:   "user preferences, technical stack, long-term habits, profile and personalization",
		IntentEpisodicMemory:  "previous conversations, recent discussion, historical context and user memory",
		IntentGlobalKnowledge: "public knowledge, factual information, technical docs, tweets and external context",
		IntentDirectAction:    "explicit action execution such as publishing, saving, creating content or running workflows",
	}

	anchors := make(map[Intent][]float32, len(definitions))
	for _, intent := range cascadeIntentPriority {
		text := definitions[intent]
		vec, err := r.embeddingClient.GetEmbedding(ctx, text, embeddingModel)
		if err != nil {
			return fmt.Errorf("generate semantic anchor for %s failed: %w", intent, err)
		}
		anchors[intent] = vec
	}
	r.semanticAnchors = anchors
	return nil
}

func (r *CascadeRouter) Route(ctx context.Context, query string, embeddingModel string) (Intent, string, error) {
	decision, err := r.RouteWithMetadata(ctx, query, embeddingModel)
	return decision.Intent, decision.RewrittenQuery, err
}

func (r *CascadeRouter) RouteWithMetadata(ctx context.Context, query string, embeddingModel string) (RouteDecision, error) {
	if r == nil {
		return RouteDecision{Intent: IntentGlobalKnowledge, RewrittenQuery: query, Stage: RouteStageDefault}, nil
	}

	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return RouteDecision{Intent: IntentGlobalKnowledge, RewrittenQuery: query, Stage: RouteStageDefault}, nil
	}

	for _, rule := range r.lexicalRules {
		if strings.Contains(normalized, strings.ToLower(rule.Keyword)) {
			return RouteDecision{Intent: rule.Intent, RewrittenQuery: query, Stage: RouteStageLexical}, nil
		}
	}

	decision := RouteDecision{Intent: IntentGlobalKnowledge, RewrittenQuery: query, Stage: RouteStageDefault}
	if r.embeddingClient != nil && len(r.semanticAnchors) > 0 {
		queryVec, err := r.embeddingClient.GetEmbedding(ctx, query, embeddingModel)
		if err == nil {
			bestIntent, bestScore := bestSemanticIntent(queryVec, r.semanticAnchors)
			decision.SemanticScore = bestScore
			if bestScore >= r.semanticThreshold {
				return RouteDecision{
					Intent: bestIntent, RewrittenQuery: query,
					Stage: RouteStageSemantic, SemanticScore: bestScore,
				}, nil
			}
		} else {
			decision.SemanticError = err.Error()
		}
	}

	if r.chatClient == nil {
		return decision, nil
	}
	decision.Stage = RouteStageLLMFallback

	systemPrompt := `You are an intent router inside an agent workflow engine. Output JSON only, no Markdown.
Allowed intent values:
- persona_memory: user profile, preferences, technical stack, long-term habits
- episodic: previous conversations, recent context, historical memory
- global: public knowledge, docs, facts, retrieval
- direct_action: explicit action execution such as publishing, saving, or running workflow
Output format: {"intent":"global","rewritten_query":"rewritten retrieval query"}`

	resp, err := r.chatClient.GetChatCompletion(ctx, systemPrompt, query, r.chatModel)
	if err != nil {
		decision.LLMError = err.Error()
		return decision, nil
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
		decision.LLMError = "invalid router JSON response"
		return decision, nil
	}

	intent, ok := parseIntent(parsed.Intent)
	if !ok {
		decision.LLMError = fmt.Sprintf("unsupported router intent %q", strings.TrimSpace(parsed.Intent))
		return decision, nil
	}
	rewritten := strings.TrimSpace(parsed.RewrittenQuery)
	if rewritten == "" {
		rewritten = query
	}
	decision.Intent = intent
	decision.RewrittenQuery = rewritten
	return decision, nil
}

func bestSemanticIntent(queryVec []float32, anchors map[Intent][]float32) (Intent, float64) {
	bestIntent := IntentGlobalKnowledge
	bestScore := -1.0
	for _, intent := range cascadeIntentPriority {
		anchorVec, exists := anchors[intent]
		if !exists {
			continue
		}
		score := cosineSimilarity(queryVec, anchorVec)
		if score > bestScore {
			bestScore = score
			bestIntent = intent
		}
	}
	return bestIntent, bestScore
}

func parseIntent(raw string) (Intent, bool) {
	switch Intent(strings.TrimSpace(raw)) {
	case IntentPersonaMemory:
		return IntentPersonaMemory, true
	case IntentEpisodicMemory:
		return IntentEpisodicMemory, true
	case IntentGlobalKnowledge:
		return IntentGlobalKnowledge, true
	case IntentDirectAction:
		return IntentDirectAction, true
	default:
		return IntentGlobalKnowledge, false
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
