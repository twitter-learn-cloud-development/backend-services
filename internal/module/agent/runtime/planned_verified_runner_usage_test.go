package runtime

import (
	"context"
	"testing"
)

type failedUsageGoalRunner struct {
	result RunResult
	err    error
}

func (runner failedUsageGoalRunner) Run(context.Context, RunRequest) (RunResult, error) {
	return runner.result, runner.err
}

func TestPlannedVerifiedRunnerAccountsFailedExecutionUsage(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{{
			ID: "respond", Kind: ShortPlanStepRespond, Objective: "answer",
			CriterionIDs: []string{"source-found"},
		}}},
		Usage: TokenUsage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
	}}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	base := plannedVerifiedTestRequest()
	executionErr := &RunError{Code: ErrorModel, Step: 1, Message: "model failed after usage"}
	failed := failedUsageGoalRunner{
		result: RunResult{
			Context: base.Run.Context, Status: RunStatusFailed,
			Steps: []Step{{Index: 1, Usage: TokenUsage{TotalTokens: 7}}},
			Usage: TokenUsage{InputTokens: 4, OutputTokens: 3, TotalTokens: 7},
		},
		err: executionErr,
	}
	runner := NewPlannedVerifiedRunner(
		coordinator,
		NewVerifiedRunner(failed, alwaysPassVerifier{}, nil),
	)

	result, err := runner.Run(context.Background(), base)
	if !HasErrorCode(err, ErrorModel) {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Verified.Run.Usage.TotalTokens != 7 || result.Usage.TotalTokens != 12 {
		t.Fatalf("usage = verified:%+v aggregate:%+v", result.Verified.Run.Usage, result.Usage)
	}
}
