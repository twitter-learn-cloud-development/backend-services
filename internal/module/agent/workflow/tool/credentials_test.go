package tool

import (
	"context"
	"errors"
	"testing"
)

func TestEnvironmentCredentialResolverUsesReferenceAllowlist(t *testing.T) {
	resolver := NewEnvironmentCredentialResolver(map[string]string{
		"tenant.default": "TENANT_LLM_KEY",
	})
	resolver.lookup = func(name string) (string, bool) {
		if name == "TENANT_LLM_KEY" {
			return "secret-value", true
		}
		return "", false
	}

	value, err := resolver.Resolve(context.Background(), "tenant.default")
	if err != nil || value != "secret-value" {
		t.Fatalf("Resolve() value/error = %q/%v", value, err)
	}
	if _, err := resolver.Resolve(context.Background(), "PATH"); !errors.Is(err, ErrCredentialReferenceNotFound) {
		t.Fatalf("Resolve(arbitrary env) error = %v", err)
	}
}
