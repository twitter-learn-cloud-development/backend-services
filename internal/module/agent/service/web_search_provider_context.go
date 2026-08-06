package service

import (
	"context"
	"strings"
)

type webSearchProviderConfigContextKey struct{}

func withWebSearchProviderConfig(ctx context.Context, configID string) context.Context {
	configID = strings.TrimSpace(configID)
	if configID == "" {
		return ctx
	}
	return context.WithValue(ctx, webSearchProviderConfigContextKey{}, configID)
}

func webSearchProviderConfigFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(webSearchProviderConfigContextKey{}).(string)
	return strings.TrimSpace(value)
}
