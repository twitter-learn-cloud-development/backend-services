package websearch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type routingResolverStub struct {
	providers map[string]*countingSearchProvider
	calls     int
}

func (resolver *routingResolverStub) ResolveWebSearchProvider(
	_ context.Context,
	subject AccessSubject,
	configID string,
) (ResolvedProvider, error) {
	resolver.calls++
	provider := resolver.providers[configID]
	if provider == nil {
		return ResolvedProvider{}, ErrUnavailable
	}
	return ResolvedProvider{
		Provider:   provider,
		CacheScope: fmt.Sprintf("user:%d:config:%s:revision:1", subject.UserID, configID),
	}, nil
}

func TestTenantRoutingProviderIsolatesConfigCache(t *testing.T) {
	t.Parallel()

	cache := &memoryWebCacheStub{values: make(map[string][]byte)}
	first := &countingSearchProvider{}
	second := &countingSearchProvider{}
	resolver := &routingResolverStub{providers: map[string]*countingSearchProvider{
		"config-a": first,
		"config-b": second,
	}}
	provider := NewTenantRoutingProvider(nil, resolver, cache, time.Minute)
	for range 2 {
		_, err := provider.Search(context.Background(), Request{
			Query: "Go release", Limit: 3,
			Subject:          AccessSubject{UserID: 7, RunID: "run-1"},
			ProviderConfigID: "config-a",
		})
		if err != nil {
			t.Fatalf("Search(config-a) error = %v", err)
		}
	}
	_, err := provider.Search(context.Background(), Request{
		Query: "Go release", Limit: 3,
		Subject:          AccessSubject{UserID: 7, RunID: "run-1"},
		ProviderConfigID: "config-b",
	})
	if err != nil {
		t.Fatalf("Search(config-b) error = %v", err)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("provider calls = %d/%d, want 1/1", first.calls, second.calls)
	}
}

func TestTenantRoutingProviderRequiresTrustedSubjectForTenantConfig(t *testing.T) {
	t.Parallel()

	provider := NewTenantRoutingProvider(nil, &routingResolverStub{}, nil, 0)
	_, err := provider.Search(context.Background(), Request{
		Query: "query", ProviderConfigID: "config-a",
	})
	if !errors.Is(err, ErrAccessIdentityRequired) {
		t.Fatalf("Search() error = %v", err)
	}
}

var _ ProviderConfigResolver = (*routingResolverStub)(nil)
