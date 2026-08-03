package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentModel "twitter-clone/internal/module/agent/model"
)

const (
	BraveProviderName       = "brave"
	DefaultBraveBaseURL     = "https://api.search.brave.com/res/v1/web/search"
	DefaultSearchTimeout    = 10 * time.Second
	DefaultMaxResponseBytes = int64(2 << 20)
	DefaultMaxSearchResults = 10
	DefaultMaxConcurrent    = 8
	HardMaxSearchTimeout    = 30 * time.Second
	HardMaxResponseBytes    = int64(8 << 20)
	HardMaxSearchResults    = 10
	HardMaxConcurrent       = 64
	maxSearchTitleRunes     = 300
	maxSearchSnippetRunes   = 1200
	webSearchUserAgent      = "twitter-clone-agent/1.0"
	braveSubscriptionHeader = "X-Subscription-Token"
)

type BraveConfig struct {
	BaseURL          string
	APIKey           string
	Timeout          time.Duration
	MaxResults       int
	MaxResponseBytes int64
	MaxConcurrent    int
	EndpointPolicy   *agentModel.EndpointPolicy
	Admission        chan struct{}
}

type BraveProvider struct {
	baseURL          *url.URL
	apiKey           string
	client           *http.Client
	maxResults       int
	maxResponseBytes int64
	admission        chan struct{}
}

func NewBraveProvider(config BraveConfig) (*BraveProvider, error) {
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	if config.BaseURL == "" {
		config.BaseURL = DefaultBraveBaseURL
	}
	config.APIKey = strings.TrimSpace(config.APIKey)
	if config.APIKey == "" {
		return nil, fmt.Errorf("%w: Brave API key is required", ErrUnavailable)
	}
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
	if err := config.EndpointPolicy.Validate(config.BaseURL, BraveProviderName); err != nil {
		return nil, fmt.Errorf("validate Brave Search endpoint: %w", err)
	}
	parsed, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: parse Brave Search endpoint: %v", ErrUnavailable, err)
	}
	client := agentModel.NewRestrictedHTTPClient(config.EndpointPolicy, BraveProviderName)
	client.Timeout = config.Timeout
	admission := config.Admission
	if admission == nil {
		admission = make(chan struct{}, config.MaxConcurrent)
	} else if cap(admission) == 0 {
		return nil, fmt.Errorf("%w: Brave Search admission capacity must be positive", ErrUnavailable)
	}
	return &BraveProvider{
		baseURL:          parsed,
		apiKey:           config.APIKey,
		client:           client,
		maxResults:       config.MaxResults,
		maxResponseBytes: config.MaxResponseBytes,
		admission:        admission,
	}, nil
}

func (p *BraveProvider) Name() string {
	return BraveProviderName
}

func (p *BraveProvider) Search(
	ctx context.Context,
	request Request,
) (agentEvidence.WebSearchResult, error) {
	if p == nil || p.client == nil || p.baseURL == nil || p.apiKey == "" {
		return agentEvidence.WebSearchResult{}, ErrUnavailable
	}
	request, err := NormalizeRequest(request, p.maxResults)
	if err != nil {
		return agentEvidence.WebSearchResult{}, err
	}
	select {
	case p.admission <- struct{}{}:
		defer func() { <-p.admission }()
	case <-ctx.Done():
		return agentEvidence.WebSearchResult{}, ctx.Err()
	}

	endpoint := *p.baseURL
	query := endpoint.Query()
	query.Set("q", request.Query)
	query.Set("count", fmt.Sprintf("%d", request.Limit))
	query.Set("result_filter", "web")
	query.Set("text_decorations", "false")
	endpoint.RawQuery = query.Encode()

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: build request", ErrProvider)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", webSearchUserAgent)
	httpRequest.Header.Set(braveSubscriptionHeader, p.apiKey)

	response, err := p.client.Do(httpRequest)
	if err != nil {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: request: %w", ErrProvider, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return agentEvidence.WebSearchResult{}, fmt.Errorf(
			"%w: HTTP status %d",
			ErrProvider,
			response.StatusCode,
		)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, p.maxResponseBytes+1))
	if err != nil {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: read response", ErrProvider)
	}
	if int64(len(body)) > p.maxResponseBytes {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: response exceeds size limit", ErrProvider)
	}

	var payload braveResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: decode response", ErrProvider)
	}
	items := make([]agentEvidence.WebSearchEvidence, 0, min(request.Limit, len(payload.Web.Results)))
	for _, candidate := range payload.Web.Results {
		safeURL, ok := normalizeResultURL(candidate.URL)
		if !ok {
			continue
		}
		title := normalizeProviderText(candidate.Title, maxSearchTitleRunes)
		if title == "" {
			title = safeURL
		}
		items = append(items, agentEvidence.WebSearchEvidence{
			Rank:    len(items) + 1,
			URL:     safeURL,
			Title:   title,
			Snippet: normalizeProviderText(candidate.Description, maxSearchSnippetRunes),
		})
		if len(items) >= request.Limit {
			break
		}
	}
	return agentEvidence.WebSearchResult{
		Schema:   agentEvidence.WebSearchSchema,
		Provider: BraveProviderName,
		Query:    request.Query,
		Items:    items,
	}, nil
}

type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

func normalizeResultURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Hostname() == "" ||
		parsed.User != nil {
		return "", false
	}
	parsed.Fragment = ""
	return parsed.String(), true
}

func normalizeProviderText(raw string, maxRunes int) string {
	return boundedText(html.UnescapeString(stripMarkup(raw)), maxRunes)
}

func stripMarkup(value string) string {
	var builder strings.Builder
	var quote rune
	inTag := false
	for _, current := range value {
		if !inTag {
			if current == '<' {
				inTag = true
				quote = 0
				continue
			}
			builder.WriteRune(current)
			continue
		}
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		switch current {
		case '\'', '"':
			quote = current
		case '>':
			inTag = false
		}
	}
	return builder.String()
}

func boundedText(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if maxRunes > 0 && len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return value
}
