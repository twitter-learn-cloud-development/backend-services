package websearch

import (
	"errors"
	"strings"
	"time"

	agentModel "twitter-clone/internal/module/agent/model"
)

// BraveProviderFactory owns deployment-level safety limits and a shared
// concurrency gate. Tenant configuration can select only endpoint and secret.
type BraveProviderFactory struct {
	timeout          time.Duration
	maxResults       int
	maxResponseBytes int64
	endpointPolicy   *agentModel.EndpointPolicy
	admission        chan struct{}
}

type BraveProviderFactoryConfig struct {
	Timeout          time.Duration
	MaxResults       int
	MaxResponseBytes int64
	MaxConcurrent    int
	EndpointPolicy   *agentModel.EndpointPolicy
}

func NewBraveProviderFactory(config BraveProviderFactoryConfig) (*BraveProviderFactory, error) {
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
	return &BraveProviderFactory{
		timeout:          config.Timeout,
		maxResults:       config.MaxResults,
		maxResponseBytes: config.MaxResponseBytes,
		endpointPolicy:   config.EndpointPolicy,
		admission:        make(chan struct{}, config.MaxConcurrent),
	}, nil
}

func (factory *BraveProviderFactory) New(baseURL, apiKey string) (Provider, error) {
	if factory == nil || factory.endpointPolicy == nil || factory.admission == nil {
		return nil, ErrUnavailable
	}
	return NewBraveProvider(BraveConfig{
		BaseURL:          baseURL,
		APIKey:           apiKey,
		Timeout:          factory.timeout,
		MaxResults:       factory.maxResults,
		MaxResponseBytes: factory.maxResponseBytes,
		MaxConcurrent:    cap(factory.admission),
		EndpointPolicy:   factory.endpointPolicy,
		Admission:        factory.admission,
	})
}

func (factory *BraveProviderFactory) ValidateEndpoint(baseURL string) error {
	if factory == nil || factory.endpointPolicy == nil {
		return ErrUnavailable
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBraveBaseURL
	}
	if err := factory.endpointPolicy.Validate(baseURL, BraveProviderName); err != nil {
		return errors.Join(ErrInvalidRequest, err)
	}
	return nil
}
