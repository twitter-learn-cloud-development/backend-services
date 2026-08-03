package websearch

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
)

type ResolvedProvider struct {
	Provider   Provider
	CacheScope string
}

type ProviderConfigResolver interface {
	ResolveWebSearchProvider(context.Context, AccessSubject, string) (ResolvedProvider, error)
}

// AtomicProviderConfigResolver allows composition to be completed after the
// AgentService is constructed without exposing a partially mutable provider.
type AtomicProviderConfigResolver struct {
	mu       sync.RWMutex
	resolver ProviderConfigResolver
}

func NewAtomicProviderConfigResolver() *AtomicProviderConfigResolver {
	return &AtomicProviderConfigResolver{}
}

func (resolver *AtomicProviderConfigResolver) Set(next ProviderConfigResolver) {
	if resolver == nil {
		return
	}
	resolver.mu.Lock()
	resolver.resolver = next
	resolver.mu.Unlock()
}

func (resolver *AtomicProviderConfigResolver) ResolveWebSearchProvider(
	ctx context.Context,
	subject AccessSubject,
	configID string,
) (ResolvedProvider, error) {
	if resolver == nil {
		return ResolvedProvider{}, ErrUnavailable
	}
	resolver.mu.RLock()
	next := resolver.resolver
	resolver.mu.RUnlock()
	if next == nil {
		return ResolvedProvider{}, ErrUnavailable
	}
	return next.ResolveWebSearchProvider(ctx, subject, configID)
}

type TenantRoutingProvider struct {
	fallback Provider
	resolver ProviderConfigResolver
	cache    Cache
	ttl      time.Duration
}

func NewTenantRoutingProvider(
	fallback Provider,
	resolver ProviderConfigResolver,
	cache Cache,
	ttl time.Duration,
) Provider {
	if fallback == nil && resolver == nil {
		return nil
	}
	return &TenantRoutingProvider{fallback: fallback, resolver: resolver, cache: cache, ttl: ttl}
}

func (provider *TenantRoutingProvider) Name() string {
	if provider != nil && provider.fallback != nil {
		return provider.fallback.Name()
	}
	return "tenant-web-search"
}

func (provider *TenantRoutingProvider) Search(
	ctx context.Context,
	request Request,
) (agentEvidence.WebSearchResult, error) {
	if provider == nil {
		return agentEvidence.WebSearchResult{}, ErrUnavailable
	}
	request.ProviderConfigID = strings.TrimSpace(request.ProviderConfigID)
	if request.ProviderConfigID == "" {
		if provider.fallback == nil {
			return agentEvidence.WebSearchResult{}, ErrUnavailable
		}
		return provider.fallback.Search(ctx, request)
	}
	if request.Subject.UserID == 0 {
		return agentEvidence.WebSearchResult{}, ErrAccessIdentityRequired
	}
	if provider.resolver == nil {
		return agentEvidence.WebSearchResult{}, ErrUnavailable
	}
	resolved, err := provider.resolver.ResolveWebSearchProvider(
		ctx,
		request.Subject,
		request.ProviderConfigID,
	)
	if err != nil {
		return agentEvidence.WebSearchResult{}, err
	}
	if resolved.Provider == nil || strings.TrimSpace(resolved.CacheScope) == "" {
		return agentEvidence.WebSearchResult{}, ErrUnavailable
	}
	normalized, err := NormalizeRequest(request, HardMaxSearchResults)
	if err != nil {
		return agentEvidence.WebSearchResult{}, err
	}
	key := cacheKey(
		"tenant-search",
		resolved.CacheScope,
		resolved.Provider.Name(),
		normalized.Query,
		strconv.Itoa(normalized.Limit),
	)
	if provider.cache != nil && provider.ttl > 0 {
		if payload, found, cacheErr := provider.cache.Get(ctx, key); cacheErr == nil && found {
			var result agentEvidence.WebSearchResult
			if json.Unmarshal(payload, &result) == nil &&
				result.Schema == agentEvidence.WebSearchSchema &&
				result.Provider == resolved.Provider.Name() {
				return result, nil
			}
		} else if cacheErr != nil {
			slog.WarnContext(ctx, "tenant web search cache read failed")
		}
	}
	result, err := resolved.Provider.Search(ctx, normalized)
	if err != nil {
		return agentEvidence.WebSearchResult{}, err
	}
	if provider.cache != nil && provider.ttl > 0 {
		if payload, marshalErr := json.Marshal(result); marshalErr == nil {
			if cacheErr := provider.cache.Set(ctx, key, payload, provider.ttl); cacheErr != nil {
				slog.WarnContext(ctx, "tenant web search cache write failed")
			}
		}
	}
	return result, nil
}

var _ ProviderConfigResolver = (*AtomicProviderConfigResolver)(nil)
