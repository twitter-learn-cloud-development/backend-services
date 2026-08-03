package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

var ErrProviderClientNotFound = errors.New("provider client not found")

type ProviderAttempt struct {
	Model    string
	Provider string
	Err      error
}

type RouteError struct {
	Requested string
	Attempts  []ProviderAttempt
}

func (e *RouteError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("model route %q exhausted after %d attempt(s)", e.Requested, len(e.Attempts))
}

func (e *RouteError) Unwrap() error {
	if e == nil || len(e.Attempts) == 0 {
		return nil
	}
	return e.Attempts[len(e.Attempts)-1].Err
}

type ProviderRouter struct {
	catalog   *Catalog
	providers map[string]agentRuntime.ModelClient
}

func NewProviderRouter(catalog *Catalog, providers map[string]agentRuntime.ModelClient) (*ProviderRouter, error) {
	if catalog == nil {
		return nil, errors.New("model catalog is required")
	}
	cloned := make(map[string]agentRuntime.ModelClient, len(providers))
	for provider, client := range providers {
		provider = strings.TrimSpace(provider)
		if provider == "" || client == nil {
			return nil, errors.New("provider name and client are required")
		}
		cloned[provider] = client
	}
	return &ProviderRouter{catalog: catalog, providers: cloned}, nil
}

func (router *ProviderRouter) Complete(
	ctx context.Context,
	request agentRuntime.ModelRequest,
) (agentRuntime.ModelResponse, error) {
	if router == nil || router.catalog == nil {
		return agentRuntime.ModelResponse{}, errors.New("provider router is not configured")
	}
	required := []Capability{CapabilityChat}
	if len(request.Tools) > 0 {
		required = append(required, CapabilityToolCall)
	}
	candidates, err := router.catalog.Candidates(request.Model, required...)
	if err != nil {
		return agentRuntime.ModelResponse{}, err
	}

	attempts := make([]ProviderAttempt, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return agentRuntime.ModelResponse{}, err
		}
		client, ok := router.providers[candidate.Provider]
		if !ok {
			attempts = append(attempts, ProviderAttempt{
				Model: candidate.ID, Provider: candidate.Provider,
				Err: fmt.Errorf("%w: %s", ErrProviderClientNotFound, candidate.Provider),
			})
			continue
		}
		routed := request
		routed.Model = candidate.ID
		if candidate.MaxOutputTokens > 0 &&
			(routed.MaxOutputTokens <= 0 || routed.MaxOutputTokens > candidate.MaxOutputTokens) {
			routed.MaxOutputTokens = candidate.MaxOutputTokens
		}
		response, callErr := client.Complete(ctx, routed)
		if callErr != nil {
			attempts = append(attempts, ProviderAttempt{
				Model: candidate.ID, Provider: candidate.Provider, Err: callErr,
			})
			continue
		}
		if strings.TrimSpace(response.Model) == "" {
			response.Model = candidate.ID
		}
		response.Provider = candidate.Provider
		if response.Usage.EstimatedCostMicros == 0 {
			estimate := estimateDefinitionCost(candidate, response.Usage)
			response.Usage.EstimatedCostMicros = estimate.Micros
			response.Usage.CostEstimated = response.Usage.Estimated
			response.Usage.PricingVersion = estimate.PricingVersion
		}
		return response, nil
	}
	return agentRuntime.ModelResponse{}, &RouteError{Requested: request.Model, Attempts: attempts}
}

// EstimateCost reserves the most expensive reachable Chat route. This keeps a
// fallback from silently exceeding the budget admitted for the primary model.
func (router *ProviderRouter) EstimateCost(model string, usage agentRuntime.TokenUsage) (agentRuntime.CostEstimate, error) {
	if router == nil || router.catalog == nil {
		return agentRuntime.CostEstimate{}, errors.New("provider router is not configured")
	}
	candidates, err := router.catalog.Candidates(model, CapabilityChat)
	if err != nil {
		return agentRuntime.CostEstimate{}, err
	}
	var highest agentRuntime.CostEstimate
	for _, candidate := range candidates {
		estimate := estimateDefinitionCost(candidate, usage)
		if estimate.Micros > highest.Micros || highest.PricingVersion == "" {
			highest = estimate
		}
	}
	return highest, nil
}

func estimateDefinitionCost(definition Definition, usage agentRuntime.TokenUsage) agentRuntime.CostEstimate {
	return agentRuntime.CostEstimate{
		Micros: conservativeTokenCost(usage.InputTokens, definition.Pricing.InputMicrosPerMillionTokens) +
			conservativeTokenCost(usage.OutputTokens, definition.Pricing.OutputMicrosPerMillionTokens),
		PricingVersion: definition.Pricing.Version,
	}
}

func conservativeTokenCost(tokens int, microsPerMillion int64) int64 {
	if tokens <= 0 || microsPerMillion <= 0 {
		return 0
	}
	if int64(tokens) > math.MaxInt64/microsPerMillion {
		return math.MaxInt64
	}
	product := int64(tokens) * microsPerMillion
	if product > math.MaxInt64-999_999 {
		return math.MaxInt64
	}
	return (product + 999_999) / 1_000_000
}
