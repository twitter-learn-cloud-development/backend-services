package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestChatCompletionRequestControlsInjectThinkingModeWithoutMutatingClient(t *testing.T) {
	var received map[string]json.RawMessage
	baseTransport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode controlled request: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Request:    request,
		}, nil
	})
	base := &http.Client{Transport: baseTransport}
	disabled := false
	controlled, err := WithChatCompletionRequestControls(base, ChatCompletionRequestControls{
		EnableThinking: &disabled,
	})
	if err != nil {
		t.Fatalf("WithChatCompletionRequestControls() error = %v", err)
	}
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://example.com/v1/chat/completions",
		strings.NewReader(`{"model":"fixed","messages":[]}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := controlled.Do(request)
	if err != nil {
		t.Fatalf("controlled.Do() error = %v", err)
	}
	_ = response.Body.Close()
	if string(received["enable_thinking"]) != "false" || string(received["model"]) != `"fixed"` {
		t.Fatalf("controlled payload = %s", received)
	}
	if _, ok := base.Transport.(roundTripFunc); !ok || controlled == base {
		t.Fatal("request controls mutated the caller-owned HTTP client")
	}
}

func TestChatCompletionRequestControlsRejectConflictingField(t *testing.T) {
	disabled := false
	controlled, err := WithChatCompletionRequestControls(&http.Client{Transport: roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			t.Fatal("base transport called for a conflicting controlled field")
			return nil, nil
		},
	)}, ChatCompletionRequestControls{EnableThinking: &disabled})
	if err != nil {
		t.Fatalf("WithChatCompletionRequestControls() error = %v", err)
	}
	request, err := http.NewRequest(
		http.MethodPost,
		"https://example.com/v1/chat/completions",
		strings.NewReader(`{"model":"fixed","enable_thinking":true}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = controlled.Do(request)
	if err == nil || !strings.Contains(err.Error(), "already defines enable_thinking") {
		t.Fatalf("controlled.Do() error = %v", err)
	}
}

func TestChatCompletionRequestControlsIgnoreOtherEndpoints(t *testing.T) {
	called := false
	base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Request:    request,
		}, nil
	})}
	enabled := true
	controlled, err := WithChatCompletionRequestControls(base, ChatCompletionRequestControls{
		EnableThinking: &enabled,
	})
	if err != nil {
		t.Fatalf("WithChatCompletionRequestControls() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/v1/models", nil)
	response, err := controlled.Do(request)
	if err != nil {
		t.Fatalf("controlled.Do() error = %v", err)
	}
	_ = response.Body.Close()
	if !called {
		t.Fatal("non-chat request did not reach the base transport")
	}
}
