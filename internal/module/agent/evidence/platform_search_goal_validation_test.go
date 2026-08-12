package evidence

import (
	"context"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestPlatformSearchGoalVerifierRejectsUnownedRequiredCriterion(t *testing.T) {
	task := platformSearchTask()
	task.CompletionCriteria = append(task.CompletionCriteria, agentRuntime.CompletionCriterion{
		ID:          "draft_quality",
		Description: "the generated draft passed a quality review",
		Required:    true,
	})

	_, err := (PlatformSearchGoalVerifier{}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{Task: task},
	)
	if err == nil {
		t.Fatal("Verify() error = nil")
	}
}
