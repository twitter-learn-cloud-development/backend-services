package model

import (
	"errors"
	"net"
	"net/http"
	"testing"
)

func TestEndpointPolicyRejectsSSRFPrimitives(t *testing.T) {
	policy := NewEndpointPolicy()
	tests := []string{
		"file:///etc/passwd",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.8/v1",
		"http://metadata.google.internal/v1",
		"http://user:password@example.com/v1",
		"https://example.com/v1?token=secret",
	}
	for _, endpoint := range tests {
		t.Run(endpoint, func(t *testing.T) {
			if err := policy.Validate(endpoint, "custom"); !errors.Is(err, ErrEndpointNotAllowed) {
				t.Fatalf("Validate(%q) error = %v", endpoint, err)
			}
		})
	}
}

func TestEndpointPolicyRejectsPrivateDNSResolutionAndRedirects(t *testing.T) {
	policy := NewEndpointPolicy()
	if err := policy.validateResolvedIP("public.example.com", "custom", net.ParseIP("127.0.0.1")); !errors.Is(err, ErrEndpointNotAllowed) {
		t.Fatalf("validateResolvedIP() error = %v", err)
	}
	if err := policy.validateResolvedIP("localhost", "lmstudio", net.ParseIP("127.0.0.1")); err != nil {
		t.Fatalf("local provider resolution error = %v", err)
	}
	if err := policy.validateResolvedIP("127.0.0.1", "lmstudio", net.ParseIP("127.0.0.1")); err != nil {
		t.Fatalf("local provider loopback IP resolution error = %v", err)
	}
	if err := policy.validateResolvedIP("127.0.0.1", "custom", net.ParseIP("127.0.0.1")); !errors.Is(err, ErrEndpointNotAllowed) {
		t.Fatalf("non-local provider loopback IP resolution error = %v", err)
	}
	client := NewRestrictedHTTPClient(policy, "custom")
	request, _ := http.NewRequest(http.MethodGet, "https://redirect.example.com", nil)
	if err := client.CheckRedirect(request, nil); !errors.Is(err, ErrEndpointNotAllowed) {
		t.Fatalf("CheckRedirect() error = %v", err)
	}
}

func TestEndpointPolicyAllowsPublicAndExplicitLocalEndpoints(t *testing.T) {
	policy := NewEndpointPolicy("llm-gateway.default.svc.cluster.local")
	tests := []struct {
		endpoint string
		provider string
	}{
		{endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1", provider: "dashscope"},
		{endpoint: "http://localhost:1234/v1", provider: "lmstudio"},
		{endpoint: "http://host.docker.internal:1234/v1", provider: "lm-studio"},
		{endpoint: "http://llm-gateway.default.svc.cluster.local/v1", provider: "custom"},
	}
	for _, test := range tests {
		if err := policy.Validate(test.endpoint, test.provider); err != nil {
			t.Fatalf("Validate(%q, %q) error = %v", test.endpoint, test.provider, err)
		}
	}
}

func TestEndpointPolicyAllowsResourceQueryButRejectsCredentialsAndFragments(t *testing.T) {
	t.Parallel()

	policy := NewEndpointPolicy()
	if err := policy.ValidateResourceURL("https://example.com/article?q=go", "web_page"); err != nil {
		t.Fatalf("ValidateResourceURL() error = %v", err)
	}
	for _, rawURL := range []string{
		"https://user:secret@example.com/article",
		"https://example.com/article#fragment",
	} {
		if err := policy.ValidateResourceURL(rawURL, "web_page"); !errors.Is(err, ErrEndpointNotAllowed) {
			t.Fatalf("ValidateResourceURL(%q) error = %v", rawURL, err)
		}
	}
}
