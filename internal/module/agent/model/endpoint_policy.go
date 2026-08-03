package model

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrEndpointNotAllowed = errors.New("outbound endpoint is not allowed")

type EndpointPolicy struct {
	allowedHosts map[string]struct{}
}

func NewEndpointPolicy(allowedHosts ...string) *EndpointPolicy {
	policy := &EndpointPolicy{allowedHosts: make(map[string]struct{}, len(allowedHosts))}
	for _, host := range allowedHosts {
		host = canonicalHost(host)
		if host != "" {
			policy.allowedHosts[host] = struct{}{}
		}
	}
	return policy
}

func (policy *EndpointPolicy) Validate(rawURL, provider string) error {
	return policy.validate(rawURL, provider, false)
}

// ValidateResourceURL validates a concrete public resource URL. Unlike a
// provider base endpoint, resource URLs may contain query parameters.
func (policy *EndpointPolicy) ValidateResourceURL(rawURL, provider string) error {
	return policy.validate(rawURL, provider, true)
}

func (policy *EndpointPolicy) validate(rawURL, provider string, allowQuery bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("%w: invalid URL: %v", ErrEndpointNotAllowed, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: scheme must be http or https", ErrEndpointNotAllowed)
	}
	if parsed.User != nil || parsed.Fragment != "" || (!allowQuery && parsed.RawQuery != "") {
		return fmt.Errorf("%w: credentials, query strings and fragments are forbidden", ErrEndpointNotAllowed)
	}
	host := canonicalHost(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("%w: host is required", ErrEndpointNotAllowed)
	}
	if policy != nil {
		if _, allowed := policy.allowedHosts[host]; allowed {
			return nil
		}
	}

	localProvider := isLocalProvider(provider)
	if isLocalHostname(host) {
		if localProvider {
			return nil
		}
		return fmt.Errorf("%w: local host %q requires an explicit local provider or allowlist", ErrEndpointNotAllowed, host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() && localProvider {
			return nil
		}
		if isNonPublicIP(ip) {
			return fmt.Errorf("%w: non-public IP %q", ErrEndpointNotAllowed, host)
		}
		return nil
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w: internal host %q requires an allowlist entry", ErrEndpointNotAllowed, host)
	}
	if !strings.Contains(host, ".") {
		return fmt.Errorf("%w: single-label host %q requires an allowlist entry", ErrEndpointNotAllowed, host)
	}
	return nil
}

// NewRestrictedHTTPClient disables redirects and pins each outbound dial to
// an IP that has passed the same public/local policy. This closes the usual
// redirect and DNS-rebinding gaps left by URL-only validation.
func NewRestrictedHTTPClient(policy *EndpointPolicy, provider string) *http.Client {
	if policy == nil {
		policy = NewEndpointPolicy()
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid dial address %q", ErrEndpointNotAllowed, address)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, resolved := range ips {
			if err := policy.validateResolvedIP(host, provider, resolved.IP); err != nil {
				lastErr = err
				continue
			}
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
			if err == nil {
				return connection, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("%w: host %q resolved to no addresses", ErrEndpointNotAllowed, host)
		}
		return nil, lastErr
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("%w: HTTP redirects are disabled", ErrEndpointNotAllowed)
		},
	}
}

func (policy *EndpointPolicy) validateResolvedIP(host, provider string, ip net.IP) error {
	host = canonicalHost(host)
	if policy != nil {
		if _, allowed := policy.allowedHosts[host]; allowed {
			return nil
		}
	}
	if isLocalProvider(provider) {
		if isLocalHostname(host) && (ip.IsLoopback() || ip.IsPrivate()) {
			return nil
		}
		if configuredIP := net.ParseIP(host); configuredIP != nil && configuredIP.IsLoopback() && ip.IsLoopback() {
			return nil
		}
	}
	if isNonPublicIP(ip) {
		return fmt.Errorf("%w: host %q resolved to non-public IP %s", ErrEndpointNotAllowed, host, ip)
	}
	return nil
}

func canonicalHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if parsed, err := url.Parse(host); err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}
	return strings.Trim(host, "[]")
}

func isLocalProvider(provider string) bool {
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(provider))
	switch normalized {
	case "lmstudio", "ollama", "local":
		return true
	default:
		return false
	}
}

func isLocalHostname(host string) bool {
	return host == "localhost" || host == "host.docker.internal" || strings.HasSuffix(host, ".localhost")
}

func isNonPublicIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}
