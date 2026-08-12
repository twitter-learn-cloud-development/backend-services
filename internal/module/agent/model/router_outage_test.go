package model

import (
	"context"
	"errors"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestProviderRouterDeniesFallbackForPermanentFailure(t *testing.T) {
	primary := &recordingModelClient{err: NewProviderCallError(
		agentRuntime.ModelProviderFailureInvalidInput, false, errors.New("invalid request body"),
	)}
	fallback := &recordingModelClient{response: agentRuntime.ModelResponse{
		Message: agentRuntime.Message{Content: "must not run"},
	}}
	router := outageTestRouter(t, primary, fallback)

	response, err := router.Complete(context.Background(), agentRuntime.ModelRequest{Model: "primary"})
	var routeErr *RouteError
	if !errors.As(err, &routeErr) {
		t.Fatalf("Complete() error = %v, want RouteError", err)
	}
	if len(primary.requests) != 1 || len(fallback.requests) != 0 {
		t.Fatalf("provider calls = primary:%d fallback:%d", len(primary.requests), len(fallback.requests))
	}
	if response.ModelRouting == nil || response.ModelRouting.TerminalDecision != agentRuntime.ModelRouteFallbackDenied ||
		len(response.ModelRouting.Attempts) != 1 ||
		response.ModelRouting.Attempts[0].FailureCode != agentRuntime.ModelProviderFailureInvalidInput {
		t.Fatalf("routing trace = %+v", response.ModelRouting)
	}
}

func TestProviderRouterRecordsAllowedFallbackAndExhaustion(t *testing.T) {
	t.Run("fallback succeeds", func(t *testing.T) {
		primary := &recordingModelClient{err: NewProviderCallError(
			agentRuntime.ModelProviderFailureUnavailable, true, errors.New("primary unavailable"),
		)}
		fallback := &recordingModelClient{response: agentRuntime.ModelResponse{
			Message: agentRuntime.Message{Content: "fallback answer"},
		}}
		router := outageTestRouter(t, primary, fallback)

		response, err := router.Complete(context.Background(), agentRuntime.ModelRequest{Model: "primary"})
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if response.ModelRouting == nil || response.ModelRouting.TerminalDecision != agentRuntime.ModelRouteSelected ||
			response.ModelRouting.SelectedModel != "fallback" || len(response.ModelRouting.Attempts) != 1 ||
			response.ModelRouting.Attempts[0].Decision != agentRuntime.ModelRouteFallbackAllowed {
			t.Fatalf("routing trace = %+v", response.ModelRouting)
		}
	})

	t.Run("fallback exhausted", func(t *testing.T) {
		primary := &recordingModelClient{err: errors.New("primary unavailable")}
		fallback := &recordingModelClient{err: errors.New("fallback unavailable")}
		router := outageTestRouter(t, primary, fallback)

		response, err := router.Complete(context.Background(), agentRuntime.ModelRequest{Model: "primary"})
		var routeErr *RouteError
		if !errors.As(err, &routeErr) {
			t.Fatalf("Complete() error = %v, want RouteError", err)
		}
		if response.ModelRouting == nil || response.ModelRouting.TerminalDecision != agentRuntime.ModelRouteFallbackExhausted ||
			len(response.ModelRouting.Attempts) != 2 ||
			response.ModelRouting.Attempts[0].Decision != agentRuntime.ModelRouteFallbackAllowed ||
			response.ModelRouting.Attempts[1].Decision != agentRuntime.ModelRouteFallbackExhausted {
			t.Fatalf("routing trace = %+v", response.ModelRouting)
		}
	})
}

func outageTestRouter(
	t *testing.T,
	primary agentRuntime.ModelClient,
	fallback agentRuntime.ModelClient,
) *ProviderRouter {
	t.Helper()
	catalog, err := NewCatalog([]Definition{
		{ID: "primary", Provider: "cloud", ContextWindow: 8192, Capabilities: []Capability{CapabilityChat}, Fallbacks: []string{"fallback"}},
		{ID: "fallback", Provider: "local", ContextWindow: 4096, Capabilities: []Capability{CapabilityChat}},
	})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}
	router, err := NewProviderRouter(catalog, map[string]agentRuntime.ModelClient{
		"cloud": primary,
		"local": fallback,
	})
	if err != nil {
		t.Fatalf("NewProviderRouter() error = %v", err)
	}
	return router
}
