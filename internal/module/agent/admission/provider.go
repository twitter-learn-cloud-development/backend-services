package admission

import (
	"context"
	"strings"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func (s *Shared) AcquireCall(ctx context.Context, provider string, request *agentRuntime.ModelRequest) (context.Context, func(), error) {
	if request == nil || provider == "" || len(provider) > 256 {
		return ctx, nil, ErrUnavailable
	}
	if request.MaxOutputTokens < 1 || request.MaxOutputTokens > s.config.OutputTokenLimit {
		request.MaxOutputTokens = s.config.OutputTokenLimit
	}
	counter := agentRuntime.NewHeuristicTokenCounter()
	usage := counter.EstimateRequest(*request)
	tokens := int64(usage.InputTokens) + int64(request.MaxOutputTokens)
	if tokens < 1 || tokens > s.config.MaxTokensPerMinute {
		s.observe("provider", "budget_limited")
		return ctx, nil, ErrBudgetExceeded
	}
	// Conservative per-minute reservations are never refunded: cancellation or a
	// lost Provider response cannot establish how much the Provider consumed.
	return s.acquire(ctx, "provider", []scope{{"provider:all", s.config.MaxProviderCalls}, {"provider:" + strings.ToLower(provider), s.config.MaxPerProvider}}, tokens, tokens*s.config.CostMicrosPerToken)
}
