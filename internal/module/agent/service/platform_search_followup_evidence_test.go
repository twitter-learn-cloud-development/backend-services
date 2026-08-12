package service

import (
	"encoding/json"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestRuntimeHasPlatformTweetDetailEvidenceRequiresBoundActionAndStructuredResult(t *testing.T) {
	t.Parallel()
	result := agentRuntime.RunResult{Steps: []agentRuntime.Step{{
		Actions: []agentRuntime.Action{{
			ID: "detail-1", Type: agentRuntime.ActionToolCall, Name: "get_tweets_by_ids",
			Arguments: json.RawMessage(`{"tweet_ids":"9007199254740993"}`),
		}},
		Observations: []agentRuntime.Observation{{
			ActionID: "detail-1", Name: "get_tweets_by_ids",
			StructuredContent: json.RawMessage(`{"schema":"platform.tweet_detail.v1","items":[{"tweet_id":"9007199254740993","content":"full content"}]}`),
		}},
	}}}
	if !runtimeHasPlatformTweetDetailEvidence(result, "9007199254740993") {
		t.Fatal("valid detail action and observation were rejected")
	}
	if runtimeHasPlatformTweetDetailEvidence(result, "9007199254740994") {
		t.Fatal("detail evidence passed for a different prior reference")
	}

	result.Steps[0].Actions[0].Arguments = json.RawMessage(`{"tweet_ids":"9007199254740993,9007199254740994"}`)
	if runtimeHasPlatformTweetDetailEvidence(result, "9007199254740993") {
		t.Fatal("multi-ID detail action passed a single selected-reference check")
	}
}
