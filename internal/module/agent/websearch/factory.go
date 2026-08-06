package websearch

import (
	"errors"
	"fmt"
	"strings"
	"time"

	agentModel "twitter-clone/internal/module/agent/model"
)

// ProviderFactory owns deployment-level safety limits and a shared concurrency
// gate. Tenant configuration can select only a supported provider, endpoint,
// and secret.
type ProviderFactory struct {
	timeout          time.Duration
	maxResults       int
	maxResponseBytes int64
	endpointPolicy   *agentModel.EndpointPolicy
	admission        chan struct{}
}

type ProviderFactoryConfig struct {
	Timeout          time.Duration
	MaxResults       int
	MaxResponseBytes int64
	MaxConcurrent    int
	EndpointPolicy   *agentModel.EndpointPolicy
}

func NewProviderFactory(config ProviderFactoryConfig) (*ProviderFactory, error) {
	if config.Timeout <= 0 {
		config.Timeout = DefaultSearchTimeout
	}
	if config.Timeout > HardMaxSearchTimeout {
		config.Timeout = HardMaxSearchTimeout
	}
	if config.MaxResults <= 0 {
		config.MaxResults = DefaultMaxSearchResults
	}
	if config.MaxResults > HardMaxSearchResults {
		config.MaxResults = HardMaxSearchResults
	}
	if config.MaxResponseBytes <= 0 {
		config.MaxResponseBytes = DefaultMaxResponseBytes
	}
	if config.MaxResponseBytes > HardMaxResponseBytes {
		config.MaxResponseBytes = HardMaxResponseBytes
	}
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = DefaultMaxConcurrent
	}
	if config.MaxConcurrent > HardMaxConcurrent {
		config.MaxConcurrent = HardMaxConcurrent
	}
	if config.EndpointPolicy == nil {
		config.EndpointPolicy = agentModel.NewEndpointPolicy()
	}
	return &ProviderFactory{
		timeout:          config.Timeout,
		maxResults:       config.MaxResults,
		maxResponseBytes: config.MaxResponseBytes,
		endpointPolicy:   config.EndpointPolicy,
		admission:        make(chan struct{}, config.MaxConcurrent),
	}, nil
}

func (factory *ProviderFactory) NewFor(providerName, baseURL, apiKey string) (Provider, error) {
	if factory == nil || factory.endpointPolicy == nil || factory.admission == nil {
		return nil, ErrUnavailable
	}
	config := ProviderClientConfig{
		BaseURL:          baseURL,
		APIKey:           apiKey,
		Timeout:          factory.timeout,
		MaxResults:       factory.maxResults,
		MaxResponseBytes: factory.maxResponseBytes,
		MaxConcurrent:    cap(factory.admission),
		EndpointPolicy:   factory.endpointPolicy,
		Admission:        factory.admission,
	}
	switch normalizeProviderName(providerName) {
	case BraveProviderName:
		return NewBraveProvider(BraveConfig(config))
	case QianfanProviderName:
		return NewQianfanProvider(QianfanConfig(config))
	default:
		return nil, fmt.Errorf("%w: unsupported web search provider %q", ErrUnavailable, providerName)
	}
}

func (factory *ProviderFactory) ValidateEndpointFor(providerName, baseURL string) error {
	if factory == nil || factory.endpointPolicy == nil {
		return ErrUnavailable
	}
	providerName = normalizeProviderName(providerName)
	defaultBaseURL, ok := DefaultProviderBaseURL(providerName)
	if !ok {
		return fmt.Errorf("%w: unsupported web search provider %q", ErrInvalidRequest, providerName)
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if err := factory.endpointPolicy.Validate(baseURL, providerName); err != nil {
		return errors.Join(ErrInvalidRequest, err)
	}
	return nil
}

func (factory *ProviderFactory) New(baseURL, apiKey string) (Provider, error) {
	return factory.NewFor(BraveProviderName, baseURL, apiKey)
}

func (factory *ProviderFactory) ValidateEndpoint(baseURL string) error {
	return factory.ValidateEndpointFor(BraveProviderName, baseURL)
}

func DefaultProviderBaseURL(providerName string) (string, bool) {
	switch normalizeProviderName(providerName) {
	case BraveProviderName:
		return DefaultBraveBaseURL, true
	case QianfanProviderName:
		return DefaultQianfanBaseURL, true
	default:
		return "", false
	}
}

func IsSupportedProvider(providerName string) bool {
	_, ok := DefaultProviderBaseURL(providerName)
	return ok
}

func normalizeProviderName(providerName string) string {
	return strings.ToLower(strings.TrimSpace(providerName))
}

type ProviderClientConfig struct {
	BaseURL          string
	APIKey           string
	Timeout          time.Duration
	MaxResults       int
	MaxResponseBytes int64
	MaxConcurrent    int
	EndpointPolicy   *agentModel.EndpointPolicy
	Admission        chan struct{}
}

// Compatibility aliases keep existing callers source-compatible while new
// code uses the provider-neutral factory.
type BraveProviderFactory = ProviderFactory
type BraveProviderFactoryConfig = ProviderFactoryConfig

func NewBraveProviderFactory(config BraveProviderFactoryConfig) (*BraveProviderFactory, error) {
	return NewProviderFactory(ProviderFactoryConfig(config))
}
