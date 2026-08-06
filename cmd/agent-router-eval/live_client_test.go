package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLiveRouterClientRecordsEmbeddingAndLLMUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/embeddings":
			_, _ = w.Write([]byte(`{"data":[{"embedding":[1,0],"index":0}],"model":"embed-model","usage":{"prompt_tokens":3,"total_tokens":3}}`))
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"{\"intent\":\"global\",\"rewritten_query\":\"query\"}"},"finish_reason":"stop"}],"model":"chat-model","usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`))
		default:
			t.Fatalf("unexpected provider path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	baseURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1) + "/v1"

	client, err := newLiveRouterClient(liveRouterClientConfig{
		Timeout: time.Second,
		Embedding: &liveProviderConfig{
			Provider: "lmstudio", BaseURL: baseURL, APIKey: "test", Model: "embed-model",
			InputMicrosPerMillionTokens: 1_000_000, PricingVersion: "embed-v1",
		},
		LLM: &liveProviderConfig{
			Provider: "lmstudio", BaseURL: baseURL, APIKey: "test", Model: "chat-model",
			InputMicrosPerMillionTokens: 1_000_000, OutputMicrosPerMillionTokens: 2_000_000, PricingVersion: "chat-v1",
		},
	})
	if err != nil {
		t.Fatalf("create live router client: %v", err)
	}
	vector, err := client.GetEmbedding(t.Context(), "query", "")
	if err != nil || len(vector) != 2 {
		t.Fatalf("embedding response: vector=%#v err=%v", vector, err)
	}
	response, err := client.GetChatCompletion(t.Context(), "system", "query", "")
	if err != nil || response == "" {
		t.Fatalf("chat response: response=%q err=%v", response, err)
	}
	embedding, llm := client.reports()
	if embedding.Requests != 1 || embedding.InputTokens != 3 || embedding.TotalTokens != 3 || embedding.EstimatedCostMicros != 3 {
		t.Fatalf("unexpected embedding usage: %#v", embedding)
	}
	if llm.Requests != 1 || llm.InputTokens != 4 || llm.OutputTokens != 2 || llm.TotalTokens != 6 || llm.EstimatedCostMicros != 8 {
		t.Fatalf("unexpected LLM usage: %#v", llm)
	}
}

func TestLiveRouterClientRejectsUntrustedLocalProviderLabel(t *testing.T) {
	_, err := newLiveRouterClient(liveRouterClientConfig{
		Embedding: &liveProviderConfig{
			Provider: "cloud", BaseURL: "http://localhost:1234/v1", APIKey: "test", Model: "embed-model",
		},
	})
	if err == nil {
		t.Fatal("expected local endpoint with cloud provider label to be rejected")
	}
}
