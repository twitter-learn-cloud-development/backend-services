package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentModel "twitter-clone/internal/module/agent/model"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	AgentToolActivitySucceeded = "succeeded"
	AgentToolActivityFailed    = "failed"
	AgentToolActivityPending   = "pending"

	AgentCitationPlatformTweet = "platform_tweet"
	AgentCitationWebPage       = "web_page"
	platformTweetCitationPath  = "/tweets/"
	legacyTweetCitationPath    = "/tweet/"

	maxUnifiedToolActivities  = 32
	maxUnifiedCitations       = 20
	maxCitationSnippetRunes   = 280
	maxStructuredEvidenceSize = 1 << 20
)

type AgentToolActivity struct {
	StepIndex   int
	ToolName    string
	Status      string
	ResultCount int
}

type AgentCitation struct {
	CitationID string
	SourceType string
	SourceID   string
	URL        string
	Title      string
	Snippet    string
}

type unifiedAgentExecution struct {
	ChatResult     *ChatResult
	ToolActivities []AgentToolActivity
	Citations      []AgentCitation
}

func collectRuntimeResultEvidence(
	result agentRuntime.RunResult,
) ([]AgentToolActivity, []AgentCitation) {
	activities := make([]AgentToolActivity, 0)
	citations := make([]AgentCitation, 0)
	seenCitations := make(map[string]int)

	for _, step := range result.Steps {
		for _, action := range step.Actions {
			if action.Type != agentRuntime.ActionToolCall {
				continue
			}

			activity := AgentToolActivity{
				StepIndex: step.Index,
				ToolName:  action.Name,
				Status:    AgentToolActivityPending,
			}
			for _, observation := range step.Observations {
				if observation.ActionID != action.ID {
					continue
				}
				if observation.IsError {
					activity.Status = AgentToolActivityFailed
					continue
				}
				activity.Status = AgentToolActivitySucceeded
				var candidates []AgentCitation
				switch {
				case isPlatformTweetSearchTool(action.Name):
					searchResult, ok := decodePlatformTweetSearchEvidence(observation.StructuredContent)
					if !ok {
						continue
					}
					activity.ResultCount = len(searchResult.Items)
					for _, item := range searchResult.Items {
						if citation, valid := platformTweetCitation(item); valid {
							candidates = append(candidates, citation)
						}
					}
				case action.Name == "web_search":
					searchResult, ok := decodeWebSearchEvidence(observation.StructuredContent)
					if !ok {
						continue
					}
					activity.ResultCount = len(searchResult.Items)
					for _, item := range searchResult.Items {
						if citation, valid := webPageCitation(item); valid {
							candidates = append(candidates, citation)
						}
					}
				case action.Name == "page_read":
					pageResult, ok := decodeWebPageEvidence(observation.StructuredContent)
					if !ok {
						continue
					}
					activity.ResultCount = 1
					if citation, valid := webPageDocumentCitation(pageResult); valid {
						candidates = append(candidates, citation)
					}
				}
				for _, citation := range candidates {
					if len(citations) >= maxUnifiedCitations {
						break
					}
					if index, exists := seenCitations[citation.CitationID]; exists {
						if action.Name == "page_read" && citation.Snippet != "" {
							citations[index] = citation
						}
						continue
					}
					seenCitations[citation.CitationID] = len(citations)
					citations = append(citations, citation)
				}
			}
			activities = append(activities, activity)
			if len(activities) >= maxUnifiedToolActivities {
				return activities, citations
			}
		}
	}
	return activities, citations
}

func isPlatformTweetSearchTool(name string) bool {
	switch name {
	case "hybrid_search_tweets", "search_tweets_by_semantic":
		return true
	default:
		return false
	}
}

func decodePlatformTweetSearchEvidence(
	raw json.RawMessage,
) (agentEvidence.PlatformTweetSearchResult, bool) {
	if len(raw) == 0 || len(raw) > maxStructuredEvidenceSize {
		return agentEvidence.PlatformTweetSearchResult{}, false
	}
	var result agentEvidence.PlatformTweetSearchResult
	if err := json.Unmarshal(raw, &result); err != nil ||
		result.Schema != agentEvidence.PlatformTweetSearchSchema {
		return agentEvidence.PlatformTweetSearchResult{}, false
	}
	return result, true
}

func decodeWebSearchEvidence(
	raw json.RawMessage,
) (agentEvidence.WebSearchResult, bool) {
	if len(raw) == 0 || len(raw) > maxStructuredEvidenceSize {
		return agentEvidence.WebSearchResult{}, false
	}
	var result agentEvidence.WebSearchResult
	if err := json.Unmarshal(raw, &result); err != nil ||
		result.Schema != agentEvidence.WebSearchSchema ||
		strings.TrimSpace(result.Provider) == "" {
		return agentEvidence.WebSearchResult{}, false
	}
	return result, true
}

func decodeWebPageEvidence(
	raw json.RawMessage,
) (agentEvidence.WebPageResult, bool) {
	if len(raw) == 0 || len(raw) > maxStructuredEvidenceSize {
		return agentEvidence.WebPageResult{}, false
	}
	var result agentEvidence.WebPageResult
	if err := json.Unmarshal(raw, &result); err != nil ||
		result.Schema != agentEvidence.WebPageSchema ||
		strings.TrimSpace(result.URL) == "" {
		return agentEvidence.WebPageResult{}, false
	}
	return result, true
}

func platformTweetCitation(
	item agentEvidence.PlatformTweetSearchEvidence,
) (AgentCitation, bool) {
	tweetID, err := strconv.ParseUint(strings.TrimSpace(item.TweetID), 10, 64)
	if err != nil || tweetID == 0 {
		return AgentCitation{}, false
	}
	sourceID := strconv.FormatUint(tweetID, 10)
	return AgentCitation{
		CitationID: AgentCitationPlatformTweet + ":" + sourceID,
		SourceType: AgentCitationPlatformTweet,
		SourceID:   sourceID,
		URL:        platformTweetCitationURL(sourceID),
		Snippet:    boundedCitationSnippet(item.Content),
	}, true
}

func webPageCitation(item agentEvidence.WebSearchEvidence) (AgentCitation, bool) {
	parsed, err := url.Parse(strings.TrimSpace(item.URL))
	if err != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Hostname() == "" ||
		parsed.User != nil {
		return AgentCitation{}, false
	}
	parsed.Fragment = ""
	if err := agentModel.NewEndpointPolicy().ValidateResourceURL(parsed.String(), "web-citation"); err != nil {
		return AgentCitation{}, false
	}
	normalizedURL := parsed.String()
	digest := sha256.Sum256([]byte(normalizedURL))
	sourceID := fmt.Sprintf("%x", digest[:12])
	title := boundedCitationSnippet(item.Title)
	if title == "" {
		title = parsed.Hostname()
	}
	return AgentCitation{
		CitationID: AgentCitationWebPage + ":" + sourceID,
		SourceType: AgentCitationWebPage,
		SourceID:   sourceID,
		URL:        normalizedURL,
		Title:      title,
		Snippet:    boundedCitationSnippet(item.Snippet),
	}, true
}

func webPageDocumentCitation(result agentEvidence.WebPageResult) (AgentCitation, bool) {
	contentType := strings.ToLower(strings.TrimSpace(result.ContentType))
	if contentType != "text/html" &&
		contentType != "application/xhtml+xml" &&
		contentType != "text/plain" {
		return AgentCitation{}, false
	}
	citation, valid := webPageCitation(agentEvidence.WebSearchEvidence{
		URL:     result.URL,
		Title:   result.Title,
		Snippet: result.Excerpt,
	})
	if !valid {
		return AgentCitation{}, false
	}
	return citation, true
}

func citationsFromTweetResults(tweets []TweetResult) []AgentCitation {
	citations := make([]AgentCitation, 0, minInt(len(tweets), maxUnifiedCitations))
	seen := make(map[uint64]struct{}, len(tweets))
	for _, tweet := range tweets {
		if tweet.TweetID == 0 {
			continue
		}
		if _, exists := seen[tweet.TweetID]; exists {
			continue
		}
		seen[tweet.TweetID] = struct{}{}
		sourceID := strconv.FormatUint(tweet.TweetID, 10)
		citations = append(citations, AgentCitation{
			CitationID: AgentCitationPlatformTweet + ":" + sourceID,
			SourceType: AgentCitationPlatformTweet,
			SourceID:   sourceID,
			URL:        platformTweetCitationURL(sourceID),
			Snippet:    boundedCitationSnippet(tweet.Summary),
		})
		if len(citations) >= maxUnifiedCitations {
			break
		}
	}
	return citations
}

func platformTweetCitationURL(sourceID string) string {
	return platformTweetCitationPath + sourceID
}

func validPlatformTweetCitationURL(resourceURL, sourceID string) bool {
	return resourceURL == platformTweetCitationURL(sourceID) ||
		resourceURL == legacyTweetCitationPath+sourceID
}

func boundedCitationSnippet(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maxCitationSnippetRunes {
		return value
	}
	return string(runes[:maxCitationSnippetRunes])
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
