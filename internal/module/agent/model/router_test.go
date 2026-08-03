package model

import (
	"context"
	"errors"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type recordingModelClient struct {
	requests []agentRuntime.ModelRequest
	response agentRuntime.ModelResponse
	err      error
}

func (client *recordingModelClient) Complete(_ context.Context, request agentRuntime.ModelRequest) (agentRuntime.ModelResponse, error) {
	client.requests = append(client.requests, request)
	return client.response, client.err
}

func TestProviderRouterUsesExplicitFallbackClient(t *testing.T) {
	catalog, err := NewCatalog([]Definition{
		{ID: "primary", Provider: "cloud", ContextWindow: 8192, MaxOutputTokens: 1024, Capabilities: []Capability{CapabilityChat}, Fallbacks: []string{"fallback"}},
		{ID: "fallback", Provider: "local", ContextWindow: 4096, MaxOutputTokens: 256, Capabilities: []Capability{CapabilityChat}},
	})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	primary := &recordingModelClient{err: errors.New("cloud unavailable")}
	fallback := &recordingModelClient{response: agentRuntime.ModelResponse{Message: agentRuntime.Message{Content: "local answer"}}}
	router, err := NewProviderRouter(catalog, map[string]agentRuntime.ModelClient{"cloud": primary, "local": fallback})
	if err != nil {
		t.Fatalf("NewProviderRouter() error = %v", err)
	}

	response, err := router.Complete(context.Background(), agentRuntime.ModelRequest{Model: "primary", MaxOutputTokens: 999})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if response.Model != "fallback" || response.Provider != "local" || response.Message.Content != "local answer" {
		t.Fatalf("Complete() response = %+v", response)
	}
	if len(primary.requests) != 1 || len(fallback.requests) != 1 || fallback.requests[0].MaxOutputTokens != 256 {
		t.Fatalf("provider requests = primary:%+v fallback:%+v", primary.requests, fallback.requests)
	}
}

func TestProviderRouterRequiresToolCapability(t *testing.T) {
	catalog, err := NewCatalog([]Definition{
		{ID: "plain", Provider: "plain-provider", ContextWindow: 4096, Capabilities: []Capability{CapabilityChat}, Fallbacks: []string{"tools"}},
		{ID: "tools", Provider: "tool-provider", ContextWindow: 4096, Capabilities: []Capability{CapabilityChat, CapabilityToolCall}},
	})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	plain := &recordingModelClient{}
	tools := &recordingModelClient{response: agentRuntime.ModelResponse{Message: agentRuntime.Message{Content: "done"}}}
	router, _ := NewProviderRouter(catalog, map[string]agentRuntime.ModelClient{"plain-provider": plain, "tool-provider": tools})

	_, err = router.Complete(context.Background(), agentRuntime.ModelRequest{
		Model: "plain", Tools: []agentRuntime.ToolDefinition{{Name: "search"}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(plain.requests) != 0 || len(tools.requests) != 1 || tools.requests[0].Model != "tools" {
		t.Fatalf("provider requests = plain:%+v tools:%+v", plain.requests, tools.requests)
	}
}

func TestProviderRouterCostReservationUsesMostExpensiveFallback(t *testing.T) {
	catalog, err := NewCatalog([]Definition{
		{ID: "primary", Provider: "a", ContextWindow: 4096, Capabilities: []Capability{CapabilityChat}, Fallbacks: []string{"fallback"}, Pricing: Pricing{InputMicrosPerMillionTokens: 1_000_000, OutputMicrosPerMillionTokens: 2_000_000, Version: "a-v1"}},
		{ID: "fallback", Provider: "b", ContextWindow: 4096, Capabilities: []Capability{CapabilityChat}, Pricing: Pricing{InputMicrosPerMillionTokens: 3_000_000, OutputMicrosPerMillionTokens: 4_000_000, Version: "b-v2"}},
	})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	router, _ := NewProviderRouter(catalog, map[string]agentRuntime.ModelClient{"a": &recordingModelClient{}, "b": &recordingModelClient{}})
	estimate, err := router.EstimateCost("primary", agentRuntime.TokenUsage{InputTokens: 10, OutputTokens: 5})
	if err != nil {
		t.Fatalf("EstimateCost() error = %v", err)
	}
	if estimate.Micros != 50 || estimate.PricingVersion != "b-v2" {
		t.Fatalf("EstimateCost() = %+v", estimate)
	}
}
