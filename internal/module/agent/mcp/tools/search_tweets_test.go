package tools

import (
	"testing"

	tweetv1 "twitter-clone/api/tweet/v1"
	agentEvidence "twitter-clone/internal/module/agent/evidence"
)

func TestNewPlatformTweetSearchEvidenceProducesVersionedSafeIDs(t *testing.T) {
	t.Parallel()

	result := newPlatformTweetSearchEvidence("golang", []*tweetv1.Tweet{
		{Id: 9007199254740993, Content: "source", CreatedAt: 123},
		nil,
		{Id: 0, Content: "invalid"},
	})

	if result.Schema != agentEvidence.PlatformTweetSearchSchema || result.Query != "golang" {
		t.Fatalf("search evidence = %+v", result)
	}
	if len(result.Items) != 1 ||
		result.Items[0].TweetID != "9007199254740993" ||
		result.Items[0].Content != "source" {
		t.Fatalf("search evidence items = %+v", result.Items)
	}
}

func TestNewPlatformTweetSearchEvidenceKeepsEmptyResultStructured(t *testing.T) {
	t.Parallel()

	result := newPlatformTweetSearchEvidence("missing", nil)
	if result.Schema != agentEvidence.PlatformTweetSearchSchema ||
		result.Items == nil ||
		len(result.Items) != 0 {
		t.Fatalf("empty search evidence = %+v", result)
	}
}
