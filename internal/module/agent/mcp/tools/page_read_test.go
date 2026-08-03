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

type pageReaderStub struct {
	request agentWebSearch.PageRequest
	result  agentEvidence.WebPageResult
	err     error
}

func (stub *pageReaderStub) Read(
	_ context.Context,
	request agentWebSearch.PageRequest,
) (agentEvidence.WebPageResult, error) {
	stub.request = request
	return stub.result, stub.err
}

func TestRegisterPageReadRequiresReader(t *testing.T) {
	t.Parallel()

	mcpServer := server.NewMCPServer("test", "v1")
	RegisterPageRead(mcpServer, nil)
	if mcpServer.GetTool(PageReadToolName) != nil {
		t.Fatal("page_read registered without reader")
	}
}

func TestRegisterPageReadReturnsStructuredEvidence(t *testing.T) {
	t.Parallel()

	reader := &pageReaderStub{result: agentEvidence.WebPageResult{
		Schema: agentEvidence.WebPageSchema, URL: "https://example.com",
		Title: "Example", ContentType: "text/plain", Content: "source", Excerpt: "source",
	}}
	mcpServer := server.NewMCPServer("test", "v1")
	RegisterPageRead(mcpServer, reader)
	registered := mcpServer.GetTool(PageReadToolName)
	if registered == nil {
		t.Fatal("page_read was not registered")
	}
	result, err := registered.Handler(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: PageReadToolName,
			Arguments: map[string]any{
				"url":                                 "https://example.com",
				agentWebSearch.InternalUserIDArgument: "9",
				agentWebSearch.InternalRunIDArgument:  "run-9",
			},
		},
	})
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	if result.IsError ||
		reader.request.URL != "https://example.com" ||
		reader.request.Subject.UserID != 9 ||
		reader.request.Subject.RunID != "run-9" {
		t.Fatalf("result/request = %+v/%+v", result, reader.request)
	}
	structured, ok := result.StructuredContent.(agentEvidence.WebPageResult)
	if !ok || structured.Schema != agentEvidence.WebPageSchema {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
}

func TestRegisterPageReadRedactsReaderErrors(t *testing.T) {
	t.Parallel()

	reader := &pageReaderStub{err: errors.New("secret upstream response")}
	mcpServer := server.NewMCPServer("test", "v1")
	RegisterPageRead(mcpServer, reader)
	result, err := mcpServer.GetTool(PageReadToolName).Handler(
		context.Background(),
		mcp.CallToolRequest{Params: mcp.CallToolParams{
			Name:      PageReadToolName,
			Arguments: map[string]any{"url": "https://example.com"},
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
		if ok && textContent.Text == "secret upstream response" {
			t.Fatalf("reader error leaked: %+v", result.Content)
		}
	}
}
