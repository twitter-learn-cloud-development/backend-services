package main

import (
	"encoding/json"
	"strings"
	"testing"

	"twitter-clone/internal/module/agent/eval"
	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestRuntimeEvalToolSandboxProjectsEvidenceContract(t *testing.T) {
	sample := eval.AgentTaskCase{
		ID: "evidence", Input: "release summary",
		Evidence: &eval.AgentTaskEvidenceContract{
			Status: eval.AgentTaskEvidenceSufficient,
			Items: []eval.AgentTaskEvidenceItem{{
				CitationID: "REL-1", SourceID: "9001", URL: "https://example.com/release",
				Title: "Release", Content: "Version 3.2 adds audit replay.",
			}},
		},
	}
	sandbox := runtimeEvalToolSandbox{sample: sample}

	platformResult, err := sandbox.Execute(t.Context(), agentRuntime.ToolCall{Name: "hybrid_search_tweets"})
	if err != nil {
		t.Fatalf("platform Execute() error = %v", err)
	}
	var platform agentEvidence.PlatformTweetSearchResult
	if err := json.Unmarshal(platformResult.StructuredContent, &platform); err != nil {
		t.Fatalf("decode platform result: %v", err)
	}
	if len(platform.Items) != 1 || platform.Items[0].TweetID != "9001" || !strings.Contains(platform.Items[0].Content, "[REL-1]") {
		t.Fatalf("platform result = %+v", platform)
	}

	webResult, err := sandbox.Execute(t.Context(), agentRuntime.ToolCall{Name: "web_search"})
	if err != nil {
		t.Fatalf("web Execute() error = %v", err)
	}
	var web agentEvidence.WebSearchResult
	if err := json.Unmarshal(webResult.StructuredContent, &web); err != nil {
		t.Fatalf("decode web result: %v", err)
	}
	if web.Provider != "controlled-eval-v3" || len(web.Items) != 1 || web.Items[0].URL != sample.Evidence.Items[0].URL || !strings.Contains(web.Items[0].Snippet, "[REL-1]") {
		t.Fatalf("web result = %+v", web)
	}

	pageResult, err := sandbox.Execute(t.Context(), agentRuntime.ToolCall{
		Name: "page_read", Arguments: json.RawMessage(`{"url":"https://example.com/release"}`),
	})
	if err != nil {
		t.Fatalf("page Execute() error = %v", err)
	}
	var page agentEvidence.WebPageResult
	if err := json.Unmarshal(pageResult.StructuredContent, &page); err != nil {
		t.Fatalf("decode page result: %v", err)
	}
	if page.URL != sample.Evidence.Items[0].URL || !strings.Contains(page.Content, "[REL-1]") {
		t.Fatalf("page result = %+v", page)
	}
}

func TestRuntimeEvalToolSandboxRejectsUnknownEvidenceURL(t *testing.T) {
	sandbox := runtimeEvalToolSandbox{sample: eval.AgentTaskCase{
		Evidence: &eval.AgentTaskEvidenceContract{
			Status: eval.AgentTaskEvidenceSufficient,
			Items:  []eval.AgentTaskEvidenceItem{{CitationID: "REL-1", SourceID: "1", URL: "https://example.com/release", Content: "release"}},
		},
	}}
	_, err := sandbox.Execute(t.Context(), agentRuntime.ToolCall{
		Name: "page_read", Arguments: json.RawMessage(`{"url":"https://evil.example/not-allowlisted"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "is not available") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestRuntimeEvalToolSandboxReturnsEmptyInsufficientEvidence(t *testing.T) {
	sandbox := runtimeEvalToolSandbox{sample: eval.AgentTaskCase{
		Input: "unknown release",
		Evidence: &eval.AgentTaskEvidenceContract{
			Status:                  eval.AgentTaskEvidenceInsufficient,
			InsufficientOutputAnyOf: []string{"insufficient evidence"},
		},
	}}
	result, err := sandbox.Execute(t.Context(), agentRuntime.ToolCall{Name: "web_search"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var evidence agentEvidence.WebSearchResult
	if err := json.Unmarshal(result.StructuredContent, &evidence); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(evidence.Items) != 0 {
		t.Fatalf("items = %+v", evidence.Items)
	}
}

func TestStrategyRuntimeCitationsUsesContractIDsAndInsufficientSentinel(t *testing.T) {
	contract := &eval.AgentTaskEvidenceContract{
		Status: eval.AgentTaskEvidenceSufficient,
		Items:  []eval.AgentTaskEvidenceItem{{CitationID: "REL-1", SourceID: "9001", URL: "https://example.com/release"}},
	}
	if got := strategyRuntimeCitationID(contract, "9001", ""); got != "REL-1" {
		t.Fatalf("platform citation ID = %q", got)
	}
	if got := strategyRuntimeCitationID(contract, "1", "https://example.com/release"); got != "REL-1" {
		t.Fatalf("web citation ID = %q", got)
	}

	citations, err := strategyRuntimeCitations("web_search", agentRuntime.RunResult{}, &eval.AgentTaskEvidenceContract{Status: eval.AgentTaskEvidenceInsufficient})
	if err != nil {
		t.Fatalf("strategyRuntimeCitations() error = %v", err)
	}
	if len(citations) != 1 || citations[0].CitationID != "no-evidence" || citations[0].SourceType != "control" {
		t.Fatalf("citations = %+v", citations)
	}
}
