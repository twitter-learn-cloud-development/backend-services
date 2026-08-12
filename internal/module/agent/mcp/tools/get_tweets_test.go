package tools

import (
	"testing"

	tweetv1 "twitter-clone/api/tweet/v1"
	agentEvidence "twitter-clone/internal/module/agent/evidence"
)

func TestNewPlatformTweetDetailEvidenceProducesAuthoritativeStructuredResult(t *testing.T) {
	t.Parallel()

	result := newPlatformTweetDetailEvidence([]*tweetv1.Tweet{
		{Id: 9007199254740993, Content: "authoritative full content", CreatedAt: 123},
		nil,
		{Id: 0, Content: "invalid"},
	})
	if result.Schema != agentEvidence.PlatformTweetDetailSchema || len(result.Items) != 1 {
		t.Fatalf("detail evidence = %+v", result)
	}
	if result.Items[0].TweetID != "9007199254740993" ||
		result.Items[0].Content != "authoritative full content" {
		t.Fatalf("detail item = %+v", result.Items[0])
	}
}
