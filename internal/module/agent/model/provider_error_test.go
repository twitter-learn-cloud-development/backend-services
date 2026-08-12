package model

import (
	"context"
	"errors"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/sashabaranov/go-openai"
)

func TestClassifyProviderFailureUsesStableFallbackPolicy(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantCode    agentRuntime.ModelProviderFailureCode
		wantAllowed bool
	}{
		{
			name: "unauthorized", err: &openai.APIError{HTTPStatusCode: 401},
			wantCode: agentRuntime.ModelProviderFailureUnauthorized,
		},
		{
			name: "invalid request", err: &openai.APIError{HTTPStatusCode: 400},
			wantCode: agentRuntime.ModelProviderFailureInvalidInput,
		},
		{
			name: "rate limited", err: &openai.APIError{HTTPStatusCode: 429},
			wantCode: agentRuntime.ModelProviderFailureRateLimited, wantAllowed: true,
		},
		{
			name: "server unavailable", err: &openai.APIError{HTTPStatusCode: 503},
			wantCode: agentRuntime.ModelProviderFailureUnavailable, wantAllowed: true,
		},
		{
			name: "run deadline", err: context.DeadlineExceeded,
			wantCode: agentRuntime.ModelProviderFailureTimeout,
		},
		{
			name: "legacy unclassified", err: errors.New("legacy adapter failure"),
			wantCode: agentRuntime.ModelProviderFailureUnclassified, wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, allowed := classifyProviderFailure(tt.err)
			if code != tt.wantCode || allowed != tt.wantAllowed {
				t.Fatalf("classifyProviderFailure() = %q/%v, want %q/%v", code, allowed, tt.wantCode, tt.wantAllowed)
			}
		})
	}
}
