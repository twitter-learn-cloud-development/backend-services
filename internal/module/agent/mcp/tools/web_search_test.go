package tools

import (
	"context"
	"errors"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentWebSearch "twitter-clone/internal/module/agent/websearch"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type mcpWebSearchProviderStub struct {
	request agentWebSearch.Request
	result  agentEvidence.WebSearchResult
	err     error
}

func (stub *mcpWebSearchProviderStub) Name() string {
	return "stub"
}

func (stub *mcpWebSearchProviderStub) Search(
	_ context.Context,
	request agentWebSearch.Request,
) (agentEvidence.WebSearchResult, error) {
	stub.request = request
	return stub.result, stub.err
}

func TestRegisterWebSearchRequiresConfiguredProvider(t *testing.T) {
	t.Parallel()

	mcpServer := server.NewMCPServer("test", "v1")
	RegisterWebSearch(mcpServer, nil)
	if mcpServer.GetTool(WebSearchToolName) != nil {
		t.Fatal("web_search registered without provider")
	}
}

func TestRegisterWebSearchReturnsStructuredEvidence(t *testing.T) {
	t.Parallel()

	provider := &mcpWebSearchProviderStub{result: agentEvidence.WebSearchResult{
		Schema:   agentEvidence.WebSearchSchema,
		Provider: "stub",
		Query:    "Go release",
		Items: []agentEvidence.WebSearchEvidence{{
			Rank: 1, URL: "https://go.dev/", Title: "Go",
		}},
	}}
	mcpServer := server.NewMCPServer("test", "v1")
	RegisterWebSearch(mcpServer, provider)
	registered := mcpServer.GetTool(WebSearchToolName)
	if registered == nil {
		t.Fatal("web_search was not registered")
	}
	result, err := registered.Handler(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: WebSearchToolName,
			Arguments: map[string]any{
				"query":                               "Go release",
				"count":                               float64(3),
				agentWebSearch.InternalUserIDArgument: "7",
				agentWebSearch.InternalRunIDArgument:  "run-1",
				agentWebSearch.InternalProviderConfigIDArgument: "config-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if result.IsError ||
		provider.request.Query != "Go release" ||
		provider.request.Limit != 3 ||
		provider.request.Subject.UserID != 7 ||
		provider.request.Subject.RunID != "run-1" ||
		provider.request.ProviderConfigID != "config-1" {
		t.Fatalf("result/request = %+v/%+v", result, provider.request)
	}
	structured, ok := result.StructuredContent.(agentEvidence.WebSearchResult)
	if !ok || structured.Schema != agentEvidence.WebSearchSchema || len(structured.Items) != 1 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
}

func TestRegisterWebSearchRedactsProviderErrors(t *testing.T) {
	t.Parallel()

	provider := &mcpWebSearchProviderStub{err: errors.New("secret provider response")}
	mcpServer := server.NewMCPServer("test", "v1")
	RegisterWebSearch(mcpServer, provider)
	result, err := mcpServer.GetTool(WebSearchToolName).Handler(
		context.Background(),
		mcp.CallToolRequest{Params: mcp.CallToolParams{
			Name:      WebSearchToolName,
			Arguments: map[string]any{"query": "query"},
		}},
	)
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("result = %+v", result)
	}
	for _, content := range result.Content {
		textContent, ok := content.(mcp.TextContent)
		if ok && textContent.Text == "secret provider response" {
			t.Fatalf("provider error leaked: %+v", result.Content)
		}
	}
}
