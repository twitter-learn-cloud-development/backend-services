package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfiguredClientAuthenticatesChatAndEmbedding(t *testing.T) {
	t.Setenv("PREMIUM_AI_BASE_URL", "")
	t.Setenv("PREMIUM_AI_TOKEN", "")
	t.Setenv("DASHSCOPE_API_URL", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("request %s has no configured bearer token", r.URL.Path)
		}
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			if request.Model != "qwen-plus" {
				t.Errorf("chat model = %q", request.Model)
			}
			_, _ = w.Write([]byte(`{"id":"chat-1","object":"chat.completion","created":1,"model":"qwen-plus","choices":[{"index":0,"message":{"role":"assistant","content":"summary"},"finish_reason":"stop"}]}`))
		case "/v1/embeddings":
			if request.Model != "text-embedding-v3" {
				t.Errorf("embedding model = %q", request.Model)
			}
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-v3"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClientWithConfig(server.URL+"/v1", "test-token", server.Client())
	answer, err := client.GetChatCompletion(context.Background(), "system", "user", "qwen-plus")
	if err != nil || answer != "summary" {
		t.Fatalf("GetChatCompletion() = %q, %v", answer, err)
	}
	vector, err := client.GetEmbedding(context.Background(), "text", "text-embedding-v3")
	if err != nil || len(vector) != 2 {
		t.Fatalf("GetEmbedding() = %v, %v", vector, err)
	}
}
