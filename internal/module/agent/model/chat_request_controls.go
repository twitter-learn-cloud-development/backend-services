package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ChatCompletionRequestControls contains the small, explicitly supported
// subset of non-standard OpenAI-compatible request fields. Keeping this
// allowlisted prevents provider configuration from becoming arbitrary JSON.
type ChatCompletionRequestControls struct {
	EnableThinking *bool
}

func (controls ChatCompletionRequestControls) empty() bool {
	return controls.EnableThinking == nil
}

// WithChatCompletionRequestControls clones client and injects allowlisted
// fields only into Chat Completions requests. The caller-owned client is not
// mutated, so other API clients sharing its transport remain unaffected.
func WithChatCompletionRequestControls(
	client *http.Client,
	controls ChatCompletionRequestControls,
) (*http.Client, error) {
	if client == nil {
		return nil, errors.New("HTTP client is required")
	}
	clone := *client
	if controls.empty() {
		return &clone, nil
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clone.Transport = &chatCompletionControlTransport{base: transport, controls: controls}
	return &clone, nil
}

type chatCompletionControlTransport struct {
	base     http.RoundTripper
	controls ChatCompletionRequestControls
}

func (transport *chatCompletionControlTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport == nil || transport.base == nil {
		return nil, errors.New("chat completion control transport is not configured")
	}
	if request == nil {
		return nil, errors.New("HTTP request is nil")
	}
	if transport.controls.empty() || request.Method != http.MethodPost ||
		request.URL == nil || !strings.HasSuffix(request.URL.Path, "/chat/completions") {
		return transport.base.RoundTrip(request)
	}
	if request.Body == nil {
		return nil, errors.New("chat completion request body is empty")
	}

	body, err := io.ReadAll(request.Body)
	closeErr := request.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read chat completion request body: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close chat completion request body: %w", closeErr)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode chat completion request body: %w", err)
	}
	if transport.controls.EnableThinking != nil {
		if _, exists := payload["enable_thinking"]; exists {
			return nil, errors.New("chat completion request already defines enable_thinking")
		}
		encoded, err := json.Marshal(*transport.controls.EnableThinking)
		if err != nil {
			return nil, fmt.Errorf("encode enable_thinking control: %w", err)
		}
		payload["enable_thinking"] = encoded
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode controlled chat completion request: %w", err)
	}

	controlled := request.Clone(request.Context())
	controlled.Body = io.NopCloser(bytes.NewReader(encoded))
	controlled.ContentLength = int64(len(encoded))
	controlled.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(encoded)), nil
	}
	controlled.TransferEncoding = nil
	return transport.base.RoundTrip(controlled)
}
