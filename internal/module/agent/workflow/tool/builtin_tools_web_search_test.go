package tool

import (
	"context"
	"errors"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"
	"twitter-clone/internal/module/agent/workflow/guardrails"
)

type webSearchProviderStub struct {
	request agentWebSearch.Request
	result  agentEvidence.WebSearchResult
	err     error
}

func (stub *webSearchProviderStub) Name() string {
	return "stub"
}

func (stub *webSearchProviderStub) Search(
	_ context.Context,
	request agentWebSearch.Request,
) (agentEvidence.WebSearchResult, error) {
	stub.request = request
	return stub.result, stub.err
}

func TestWebSearchToolFailsClosedWithoutProvider(t *testing.T) {
	t.Parallel()

	_, err := NewWebSearchTool().Execute(context.Background(), map[string]interface{}{
		"query": "latest Go release",
	})
	if !errors.Is(err, agentWebSearch.ErrUnavailable) {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestWebSearchToolReturnsStructuredProviderResult(t *testing.T) {
	t.Parallel()

	provider := &webSearchProviderStub{result: agentEvidence.WebSearchResult{
		Schema:   agentEvidence.WebSearchSchema,
		Provider: "stub",
		Query:    "latest Go release",
		Items: []agentEvidence.WebSearchEvidence{{
			Rank: 1, URL: "https://go.dev/", Title: "Go", Snippet: "Official site",
		}},
	}}
	ctx := guardrails.InjectUserContext(context.Background(), 42)
	ctx = InjectExecutionMetadata(ctx, ExecutionMetadata{RunID: "run-search-1"})
	result, err := NewWebSearchTool(provider).Execute(ctx, map[string]interface{}{
		"query":              "latest Go release",
		"count":              float64(3),
		"provider_config_id": "config-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if provider.request.Query != "latest Go release" ||
		provider.request.Limit != 3 ||
		provider.request.Subject.UserID != 42 ||
		provider.request.Subject.RunID != "run-search-1" ||
		provider.request.ProviderConfigID != "config-1" {
		t.Fatalf("provider request = %+v", provider.request)
	}
	if result["schema"] != agentEvidence.WebSearchSchema ||
		result["provider"] != "stub" {
		t.Fatalf("result = %+v", result)
	}
	modelText, _ := result["results"].(string)
	if modelText == "" {
		t.Fatalf("model text = %q", modelText)
	}
}
