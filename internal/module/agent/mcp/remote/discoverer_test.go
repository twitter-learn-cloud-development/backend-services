package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentModel "twitter-clone/internal/module/agent/model"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestSDKDiscovererListsStreamableHTTPToolsWithBearerHeader(t *testing.T) {
	mcpServer := server.NewMCPServer("external-test", "1.0.0")
	mcpServer.AddTool(mcp.NewTool("lookup", mcp.WithString("query", mcp.Required())), func(
		_ context.Context,
		request mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok:" + request.GetString("query", "")), nil
	})
	handler := server.NewStreamableHTTPServer(mcpServer, server.WithStateLess(true))
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer discovery-token" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(writer, request)
	}))
	defer httpServer.Close()

	discoverer := NewSDKDiscoverer(agentModel.NewEndpointPolicy("127.0.0.1"), 5*time.Second)
	tools, err := discoverer.Discover(context.Background(), DiscoveryRequest{
		Transport: TransportStreamableHTTP, Endpoint: httpServer.URL, BearerToken: "discovery-token",
	})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "lookup" {
		t.Fatalf("Discover() tools = %+v", tools)
	}
	result, err := discoverer.Call(context.Background(), DiscoveryRequest{
		Transport: TransportStreamableHTTP, Endpoint: httpServer.URL, BearerToken: "discovery-token",
	}, "lookup", map[string]interface{}{"query": "go"})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("Call() result = %+v", result)
	}
}

func TestNewRemoteClientAcceptsSSEAndRejectsUnknownTransport(t *testing.T) {
	client, err := newRemoteClient(TransportSSE, "https://mcp.example.com/sse", nil, http.DefaultClient)
	if err != nil {
		t.Fatalf("newRemoteClient(sse) error = %v", err)
	}
	_ = client.Close()
	if _, err := newRemoteClient("stdio", "https://mcp.example.com", nil, http.DefaultClient); err == nil {
		t.Fatal("newRemoteClient(stdio) error = nil")
	}
}
