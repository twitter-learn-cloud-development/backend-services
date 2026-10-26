package middleware

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const AllowedOriginsEnv = "GATEWAY_ALLOWED_ORIGINS"

var defaultDevelopmentOrigins = []string{
	"http://localhost:5173",
	"http://127.0.0.1:5173",
}

// OriginPolicy is an exact-origin allowlist shared by HTTP CORS and WebSocket
// upgrades. It intentionally does not support wildcard origins.
type OriginPolicy struct {
	allowed map[string]struct{}
}

func NewOriginPolicy(raw string) (*OriginPolicy, error) {
	values := strings.Split(raw, ",")
	if strings.TrimSpace(raw) == "" {
		values = defaultDevelopmentOrigins
	}
	policy := &OriginPolicy{allowed: make(map[string]struct{}, len(values))}
	for _, value := range values {
		origin, err := normalizeOrigin(value)
		if err != nil {
			return nil, err
		}
		policy.allowed[origin] = struct{}{}
	}
	if len(policy.allowed) == 0 {
		return nil, fmt.Errorf("origin allowlist is empty")
	}
	return policy, nil
}

func OriginPolicyFromEnvironment() (*OriginPolicy, error) {
	return NewOriginPolicy(os.Getenv(AllowedOriginsEnv))
}

func (p *OriginPolicy) Allows(origin string) bool {
	if p == nil || strings.TrimSpace(origin) == "" {
		return strings.TrimSpace(origin) == ""
	}
	normalized, err := normalizeOrigin(origin)
	if err != nil {
		return false
	}
	_, ok := p.allowed[normalized]
	return ok
}

func (p *OriginPolicy) AllowsRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	return p.Allows(r.Header.Get("Origin"))
}

func normalizeOrigin(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "*" {
		return "", fmt.Errorf("invalid origin %q", value)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid origin %q", value)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("origin %q must use http or https", value)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("origin %q must not contain credentials, path, query, or fragment", value)
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), nil
}
