package rag

import (
	"context"
	"errors"
	"testing"
)

type fakeRouterProvider struct {
	embedding      []float32
	embeddingError error
	chatResponse   string
	chatError      error
}

type failingAnchorProvider struct{ calls int }

func (provider *failingAnchorProvider) GetEmbedding(context.Context, string, string) ([]float32, error) {
	provider.calls++
	if provider.calls == 2 {
		return nil, errors.New("anchor failure")
	}
	return []float32{1, 0}, nil
}

func (provider fakeRouterProvider) GetEmbedding(context.Context, string, string) ([]float32, error) {
	return provider.embedding, provider.embeddingError
}

func (provider fakeRouterProvider) GetChatCompletion(context.Context, string, string, string) (string, error) {
	return provider.chatResponse, provider.chatError
}

func TestCascadeRouterUsesStableLexicalPriority(t *testing.T) {
	router := NewCascadeRouter(nil, "")
	decision, err := router.RouteWithMetadata(context.Background(), "上次搜索的结果是什么？", "")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if decision.Intent != IntentEpisodicMemory || decision.Stage != RouteStageLexical {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestCascadeRouterReportsDefaultStageWithoutModel(t *testing.T) {
	router := NewCascadeRouter(nil, "")
	decision, err := router.RouteWithMetadata(context.Background(), "Explain distributed systems", "")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if decision.Intent != IntentGlobalKnowledge || decision.Stage != RouteStageDefault {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestBestSemanticIntentUsesStablePriorityForTies(t *testing.T) {
	anchors := map[Intent][]float32{
		IntentGlobalKnowledge: {1, 0},
		IntentEpisodicMemory:  {1, 0},
		IntentPersonaMemory:   {1, 0},
	}
	intent, score := bestSemanticIntent([]float32{1, 0}, anchors)
	if intent != IntentEpisodicMemory || score != 1 {
		t.Fatalf("unexpected semantic tie-break: intent=%s score=%f", intent, score)
	}
}

func TestCascadeRouterRecordsSemanticFailureBeforeLLMFallback(t *testing.T) {
	provider := fakeRouterProvider{
		embeddingError: errors.New("embedding unavailable"),
		chatResponse:   `{"intent":"persona_memory","rewritten_query":"user profile"}`,
	}
	router := NewCascadeRouterWithConfig(CascadeRouterConfig{
		EmbeddingClient:   provider,
		ChatClient:        provider,
		ChatModel:         "router-model",
		SemanticThreshold: 0.9,
	})
	router.semanticAnchors[IntentPersonaMemory] = []float32{1, 0}

	decision, err := router.RouteWithMetadata(context.Background(), "Tell me about myself", "embedding-model")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if decision.Intent != IntentPersonaMemory || decision.Stage != RouteStageLLMFallback {
		t.Fatalf("unexpected decision: %#v", decision)
	}
	if decision.SemanticError != "embedding unavailable" || decision.LLMError != "" {
		t.Fatalf("unexpected provider diagnostics: %#v", decision)
	}
}

func TestCascadeRouterReportsInvalidLLMResponse(t *testing.T) {
	provider := fakeRouterProvider{chatResponse: "not-json"}
	router := NewCascadeRouterWithConfig(CascadeRouterConfig{ChatClient: provider, ChatModel: "router-model"})

	decision, err := router.RouteWithMetadata(context.Background(), "Tell me something ambiguous", "")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if decision.Intent != IntentGlobalKnowledge || decision.Stage != RouteStageLLMFallback || decision.LLMError == "" {
		t.Fatalf("expected observable LLM fallback failure: %#v", decision)
	}
}

func TestInitSemanticAnchorsDoesNotPublishPartialGeneration(t *testing.T) {
	provider := &failingAnchorProvider{}
	router := NewCascadeRouterWithConfig(CascadeRouterConfig{EmbeddingClient: provider})
	router.semanticAnchors[IntentGlobalKnowledge] = []float32{0, 1}

	if err := router.InitSemanticAnchors(context.Background(), "embedding-model"); err == nil {
		t.Fatal("expected anchor initialization to fail")
	}
	if len(router.semanticAnchors) != 1 || router.semanticAnchors[IntentGlobalKnowledge][1] != 1 {
		t.Fatalf("partial anchor generation became visible: %#v", router.semanticAnchors)
	}
}
