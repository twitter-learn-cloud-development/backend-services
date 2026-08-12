package service

import (
	"context"
	"errors"
	"testing"
)

func TestE2E06PlatformSearchFollowUpRejectsMultipleExplicitTweetIDs(t *testing.T) {
	dialogue, repo := platformFollowUpRepository([]any{"/tweets/9007199254740993"})
	runner := &capturingRuntimeRunner{}
	service := newPlatformFollowUpTestService(repo, runner)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, DialogueKey: dialogue.ID.Hex(),
		Content: "比较 9007199254740993 和 9007199254740999 的详细内容",
	})
	if !errors.Is(err, ErrPlatformTweetReferenceAmbiguous) {
		t.Fatalf("RunAgent() error = %v, want ErrPlatformTweetReferenceAmbiguous", err)
	}
	if runner.calls != 0 {
		t.Fatalf("multiple explicit references reached runtime: calls=%d", runner.calls)
	}
}
