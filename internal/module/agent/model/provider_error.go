package model

import (
	"context"
	"errors"
	"net"

	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/sashabaranov/go-openai"
)

type ProviderCallError struct {
	Code            agentRuntime.ModelProviderFailureCode
	FallbackAllowed bool
	Cause           error
}

func NewProviderCallError(
	code agentRuntime.ModelProviderFailureCode,
	fallbackAllowed bool,
	cause error,
) *ProviderCallError {
	return &ProviderCallError{Code: code, FallbackAllowed: fallbackAllowed, Cause: cause}
}

func (e *ProviderCallError) Error() string {
	if e == nil {
		return "provider call failed"
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Code)
}

func (e *ProviderCallError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func classifyProviderFailure(err error) (agentRuntime.ModelProviderFailureCode, bool) {
	if err == nil {
		return "", false
	}
	var classified *ProviderCallError
	if errors.As(err, &classified) && classified != nil {
		code := classified.Code
		if code == "" {
			code = agentRuntime.ModelProviderFailureUnclassified
		}
		return code, classified.FallbackAllowed
	}
	if errors.Is(err, context.Canceled) {
		return agentRuntime.ModelProviderFailureCanceled, false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return agentRuntime.ModelProviderFailureTimeout, false
	}

	var apiErr *openai.APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		switch {
		case apiErr.HTTPStatusCode == 401 || apiErr.HTTPStatusCode == 403:
			return agentRuntime.ModelProviderFailureUnauthorized, false
		case apiErr.HTTPStatusCode == 408:
			return agentRuntime.ModelProviderFailureTimeout, true
		case apiErr.HTTPStatusCode == 429:
			return agentRuntime.ModelProviderFailureRateLimited, true
		case apiErr.HTTPStatusCode >= 500:
			return agentRuntime.ModelProviderFailureUnavailable, true
		case apiErr.HTTPStatusCode >= 400:
			return agentRuntime.ModelProviderFailureInvalidInput, false
		}
	}

	var networkErr net.Error
	if errors.As(err, &networkErr) {
		if networkErr.Timeout() {
			return agentRuntime.ModelProviderFailureTimeout, true
		}
		return agentRuntime.ModelProviderFailureUnavailable, true
	}
	if errors.Is(err, ErrProviderClientNotFound) {
		return agentRuntime.ModelProviderFailureUnavailable, true
	}
	// Preserve compatibility for provider adapters that have not yet adopted
	// typed errors. Explicit permanent failures must use ProviderCallError.
	return agentRuntime.ModelProviderFailureUnclassified, true
}
