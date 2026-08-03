package evidence

const PlatformTweetSearchSchema = "platform.tweet_search.v1"

// PlatformTweetSearchResult is the machine-readable result emitted by
// first-party platform search tools. Text fallback remains available to the
// model, while this payload is consumed only by trusted server-side adapters.
type PlatformTweetSearchResult struct {
	Schema string                        `json:"schema"`
	Query  string                        `json:"query,omitempty"`
	Items  []PlatformTweetSearchEvidence `json:"items"`
}

type PlatformTweetSearchEvidence struct {
	TweetID   string `json:"tweet_id"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

const WebSearchSchema = "web.search.v1"

// WebSearchResult is the trusted projection emitted by the web search
// adapter. Provider payloads must be normalized before entering this schema.
type WebSearchResult struct {
	Schema   string              `json:"schema"`
	Provider string              `json:"provider"`
	Query    string              `json:"query"`
	Items    []WebSearchEvidence `json:"items"`
}

type WebSearchEvidence struct {
	Rank    int    `json:"rank"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet,omitempty"`
}

const WebPageSchema = "web.page.v1"

// WebPageResult is the bounded, server-normalized projection of a public web
// page. Model-facing adapters must use the separately sanitized formatter.
type WebPageResult struct {
	Schema      string        `json:"schema"`
	URL         string        `json:"url"`
	Title       string        `json:"title,omitempty"`
	ContentType string        `json:"content_type"`
	Content     string        `json:"content"`
	Excerpt     string        `json:"excerpt,omitempty"`
	Truncated   bool          `json:"truncated"`
	Safety      WebPageSafety `json:"safety"`
}

type WebPageSafety struct {
	HiddenContentRemoved bool     `json:"hidden_content_removed,omitempty"`
	InjectionSignals     []string `json:"injection_signals,omitempty"`
}
