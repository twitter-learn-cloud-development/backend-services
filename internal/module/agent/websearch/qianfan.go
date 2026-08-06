package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentModel "twitter-clone/internal/module/agent/model"
)

const (
	QianfanProviderName   = "qianfan"
	DefaultQianfanBaseURL = "https://qianfan.baidubce.com/v2/ai_search/web_search"
	qianfanSearchSource   = "baidu_search_v2"
)

type QianfanConfig ProviderClientConfig

type QianfanProvider struct {
	baseURL          *url.URL
	apiKey           string
	client           *http.Client
	maxResults       int
	maxResponseBytes int64
	admission        chan struct{}
}

func NewQianfanProvider(config QianfanConfig) (*QianfanProvider, error) {
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	if config.BaseURL == "" {
		config.BaseURL = DefaultQianfanBaseURL
	}
	config.APIKey = strings.TrimSpace(config.APIKey)
	if config.APIKey == "" {
		return nil, fmt.Errorf("%w: Qianfan API key is required", ErrUnavailable)
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
	if err := config.EndpointPolicy.Validate(config.BaseURL, QianfanProviderName); err != nil {
		return nil, fmt.Errorf("validate Qianfan Search endpoint: %w", err)
	}
	parsed, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: parse Qianfan Search endpoint: %v", ErrUnavailable, err)
	}
	client := agentModel.NewRestrictedHTTPClient(config.EndpointPolicy, QianfanProviderName)
	client.Timeout = config.Timeout
	admission := config.Admission
	if admission == nil {
		admission = make(chan struct{}, config.MaxConcurrent)
	} else if cap(admission) == 0 {
		return nil, fmt.Errorf("%w: Qianfan Search admission capacity must be positive", ErrUnavailable)
	}
	return &QianfanProvider{
		baseURL:          parsed,
		apiKey:           config.APIKey,
		client:           client,
		maxResults:       config.MaxResults,
		maxResponseBytes: config.MaxResponseBytes,
		admission:        admission,
	}, nil
}

func (provider *QianfanProvider) Name() string {
	return QianfanProviderName
}

func (provider *QianfanProvider) Search(
	ctx context.Context,
	request Request,
) (agentEvidence.WebSearchResult, error) {
	if provider == nil || provider.client == nil || provider.baseURL == nil || provider.apiKey == "" {
		return agentEvidence.WebSearchResult{}, ErrUnavailable
	}
	request, err := NormalizeRequest(request, provider.maxResults)
	if err != nil {
		return agentEvidence.WebSearchResult{}, err
	}
	select {
	case provider.admission <- struct{}{}:
		defer func() { <-provider.admission }()
	case <-ctx.Done():
		return agentEvidence.WebSearchResult{}, ctx.Err()
	}

	payload, err := json.Marshal(qianfanRequest{
		Messages:           []qianfanMessage{{Role: "user", Content: request.Query}},
		SearchSource:       qianfanSearchSource,
		ResourceTypeFilter: []qianfanResourceType{{Type: "web", TopK: request.Limit}},
		SafeSearch:         true,
	})
	if err != nil {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: encode request", ErrProvider)
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		provider.baseURL.String(),
		bytes.NewReader(payload),
	)
	if err != nil {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: build request", ErrProvider)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("User-Agent", webSearchUserAgent)
	httpRequest.Header.Set("Authorization", "Bearer "+provider.apiKey)

	response, err := provider.client.Do(httpRequest)
	if err != nil {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: request: %w", ErrProvider, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: HTTP status %d", ErrProvider, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, provider.maxResponseBytes+1))
	if err != nil {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: read response", ErrProvider)
	}
	if int64(len(body)) > provider.maxResponseBytes {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: response exceeds size limit", ErrProvider)
	}

	var decoded qianfanResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: decode response", ErrProvider)
	}
	if qianfanResponseHasError(decoded.Code) {
		return agentEvidence.WebSearchResult{}, fmt.Errorf("%w: provider returned an error", ErrProvider)
	}

	items := make([]agentEvidence.WebSearchEvidence, 0, min(request.Limit, len(decoded.References)))
	for _, candidate := range decoded.References {
		if candidate.Type != "" && !strings.EqualFold(candidate.Type, "web") {
			continue
		}
		safeURL, ok := normalizeResultURL(candidate.URL)
		if !ok {
			continue
		}
		title := normalizeProviderText(candidate.Title, maxSearchTitleRunes)
		if title == "" {
			title = normalizeProviderText(candidate.WebAnchor, maxSearchTitleRunes)
		}
		if title == "" {
			title = safeURL
		}
		snippet := candidate.Snippet
		if strings.TrimSpace(snippet) == "" {
			snippet = candidate.Content
		}
		items = append(items, agentEvidence.WebSearchEvidence{
			Rank:    len(items) + 1,
			URL:     safeURL,
			Title:   title,
			Snippet: normalizeProviderText(snippet, maxSearchSnippetRunes),
		})
		if len(items) >= request.Limit {
			break
		}
	}
	return agentEvidence.WebSearchResult{
		Schema:   agentEvidence.WebSearchSchema,
		Provider: QianfanProviderName,
		Query:    request.Query,
		Items:    items,
	}, nil
}

type qianfanMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type qianfanResourceType struct {
	Type string `json:"type"`
	TopK int    `json:"top_k"`
}

type qianfanRequest struct {
	Messages           []qianfanMessage      `json:"messages"`
	SearchSource       string                `json:"search_source"`
	ResourceTypeFilter []qianfanResourceType `json:"resource_type_filter"`
	SafeSearch         bool                  `json:"safe_search"`
}

type qianfanResponse struct {
	Code       json.RawMessage    `json:"code"`
	References []qianfanReference `json:"references"`
}

type qianfanReference struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	WebAnchor string `json:"web_anchor"`
	Snippet   string `json:"snippet"`
	Content   string `json:"content"`
	Type      string `json:"type"`
}

func qianfanResponseHasError(code json.RawMessage) bool {
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(string(code)), "\""))
	switch normalized {
	case "", "null", "0", "success":
		return false
	default:
		return true
	}
}
