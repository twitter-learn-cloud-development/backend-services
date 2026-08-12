package runtime

import (
	"context"
	"testing"
)

type suspensionAwareVerifier struct {
	result VerificationResult
	calls  int
}

func (verifier *suspensionAwareVerifier) Verify(
	_ context.Context,
	_ VerificationRequest,
) (VerificationResult, error) {
	return verifier.result, nil
}

func (verifier *suspensionAwareVerifier) VerifySuspension(
	_ context.Context,
	_ VerificationRequest,
) (VerificationResult, error) {
	verifier.calls++
	return verifier.result, nil
}

func TestVerifiedRunnerUsesOptInSuspensionVerifier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		verify     VerificationResult
		wantStatus GoalRunStatus
		checkpoint bool
	}{
		{
			name:       "verified suspension remains resumable",
			verify:     VerificationResult{Status: VerificationPassed},
			wantStatus: GoalRunSuspended,
			checkpoint: true,
		},
		{
			name:       "invalid suspension is blocked",
			verify:     VerificationResult{Status: VerificationFailed},
			wantStatus: GoalRunBlocked,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			base := goalRunRequest("run-suspension")
			pending := Action{ID: "question-1", Type: ActionAskHuman, Content: "Resolve conflict?"}
			execution := &scriptedGoalRunner{results: []RunResult{{
				Context:           base.Context,
				Status:            RunStatusAwaitingHuman,
				Messages:          []Message{{Role: RoleAssistant, Actions: []Action{pending}}},
				Steps:             []Step{{Index: 1, Actions: []Action{pending}}},
				PendingAction:     &pending,
				PendingResumeKind: ResumeKindHumanResponse,
			}}}
			verifier := &suspensionAwareVerifier{result: test.verify}
			runner := NewVerifiedRunner(execution, verifier, nil)

			result, err := runner.Run(context.Background(), VerifiedRunRequest{
				Task: goalTask(0), Run: base,
			})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if result.Status != test.wantStatus || verifier.calls != 1 ||
				(result.Checkpoint != nil) != test.checkpoint {
				t.Fatalf("Run() status/calls/checkpoint = %q/%d/%t, want %q/1/%t",
					result.Status, verifier.calls, result.Checkpoint != nil,
					test.wantStatus, test.checkpoint)
			}
		})
	}
}
