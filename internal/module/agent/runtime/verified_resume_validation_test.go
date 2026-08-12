package runtime

import (
	"context"
	"testing"
)

func TestVerifiedRunnerResumeRejectsTruncatedHistory(t *testing.T) {
	checkpoint := verifiedApprovalCheckpoint("run-truncated", 0)
	resumable := &scriptedResumableGoalRunner{resumeResults: []RunResult{{
		Context:  checkpoint.Run.Context,
		Status:   RunStatusCompleted,
		Messages: cloneMessages(checkpoint.Run.Messages),
		Steps:    nil,
		Usage:    checkpoint.Run.Usage,
	}}}
	runner := NewVerifiedRunner(resumable, RequiredEvidenceVerifier{}, nil)
	environment := &fakeGoalEnvironment{
		name:  "twitter",
		tools: []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
	}

	_, err := runner.Resume(context.Background(), VerifiedResumeRequest{
		Checkpoint:  checkpoint,
		ApprovalID:  "approval-1",
		Tools:       []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
		Environment: environment,
	})
	if !HasErrorCode(err, ErrorInvalidRequest) {
		t.Fatalf("Resume() error = %v", err)
	}
	if len(environment.snapshotCalls) != 0 {
		t.Fatalf("snapshot phases = %v, want none", environment.snapshotCalls)
	}
}
