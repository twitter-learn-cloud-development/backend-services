package websearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
)

type Cache interface {
	Get(context.Context, string) ([]byte, bool, error)
	Set(context.Context, string, []byte, time.Duration) error
}

type CachedProvider struct {
	next  Provider
	cache Cache
	ttl   time.Duration
}

func NewCachedProvider(next Provider, cache Cache, ttl time.Duration) Provider {
	if next == nil || cache == nil || ttl <= 0 {
		return next
	}
	return &CachedProvider{next: next, cache: cache, ttl: ttl}
}

func (provider *CachedProvider) Name() string {
	if provider == nil || provider.next == nil {
		return ""
	}
	return provider.next.Name()
}

func (provider *CachedProvider) Search(
	ctx context.Context,
	request Request,
) (agentEvidence.WebSearchResult, error) {
	if provider == nil || provider.next == nil {
		return agentEvidence.WebSearchResult{}, ErrUnavailable
	}
	key := cacheKey(
		"search",
		provider.next.Name(),
		strings.Join(strings.Fields(request.Query), " "),
		strconv.Itoa(request.Limit),
	)
	if payload, found, err := provider.cache.Get(ctx, key); err == nil && found {
		var result agentEvidence.WebSearchResult
		if json.Unmarshal(payload, &result) == nil &&
			result.Schema == agentEvidence.WebSearchSchema &&
			result.Provider == provider.next.Name() {
			return result, nil
		}
	} else if err != nil {
		slog.WarnContext(ctx, "web search cache read failed", "provider", provider.next.Name())
	}
	result, err := provider.next.Search(ctx, request)
	if err != nil {
		return agentEvidence.WebSearchResult{}, err
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr == nil {
		if cacheErr := provider.cache.Set(ctx, key, payload, provider.ttl); cacheErr != nil {
			slog.WarnContext(ctx, "web search cache write failed", "provider", provider.next.Name())
		}
	}
	return result, nil
}

type CachedPageReader struct {
	next  PageReader
	cache Cache
	ttl   time.Duration
}

func NewCachedPageReader(next PageReader, cache Cache, ttl time.Duration) PageReader {
	if next == nil || cache == nil || ttl <= 0 {
		return next
	}
	return &CachedPageReader{next: next, cache: cache, ttl: ttl}
}

func (reader *CachedPageReader) Read(
	ctx context.Context,
	request PageRequest,
) (agentEvidence.WebPageResult, error) {
	if reader == nil || reader.next == nil {
		return agentEvidence.WebPageResult{}, ErrPageUnavailable
	}
	normalized, err := NormalizePageRequest(request, HardMaxPageRunes)
	if err != nil {
		return agentEvidence.WebPageResult{}, err
	}
	key := cacheKey("page", normalized.URL, strconv.Itoa(normalized.MaxRunes))
	if payload, found, cacheErr := reader.cache.Get(ctx, key); cacheErr == nil && found {
		var result agentEvidence.WebPageResult
		if json.Unmarshal(payload, &result) == nil &&
			result.Schema == agentEvidence.WebPageSchema &&
			result.URL == normalized.URL {
			return result, nil
		}
	} else if cacheErr != nil {
		slog.WarnContext(ctx, "web page cache read failed")
	}
	result, err := reader.next.Read(ctx, normalized)
	if err != nil {
		return agentEvidence.WebPageResult{}, err
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr == nil {
		if cacheErr := reader.cache.Set(ctx, key, payload, reader.ttl); cacheErr != nil {
			slog.WarnContext(ctx, "web page cache write failed")
		}
	}
	return result, nil
}

func cacheKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("v1:%s", hex.EncodeToString(sum[:]))
}
