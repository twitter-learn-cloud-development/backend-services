package runtime

import (
	"context"
	"testing"
)

type fixedFailedGoalRunner struct {
	code ErrorCode
}

func (runner fixedFailedGoalRunner) Run(_ context.Context, request RunRequest) (RunResult, error) {
	return RunResult{
		Context: request.Context, Status: RunStatusFailed,
		Messages: cloneMessages(request.Messages), Steps: []Step{{Index: 1}},
		Usage: TokenUsage{TotalTokens: 6},
	}, &RunError{Code: runner.code, Message: "not eligible for recovery"}
}

func TestPlannedVerifiedRunnerDoesNotRecoverModelFailure(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{{
			ID: "answer", Kind: ShortPlanStepRespond, Objective: "answer",
			CriterionIDs: []string{"source-found"},
		}}},
		Usage: TokenUsage{TotalTokens: 2},
	}}}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	request := plannedVerifiedTestRequest()
	request.Task.MaxRepairAttempts = 1
	runner := NewPlannedVerifiedRunner(
		coordinator,
		NewVerifiedRunner(fixedFailedGoalRunner{code: ErrorModel}, alwaysPassVerifier{}, nil),
	)

	result, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorModel) || result.RecoveryAttempts != 0 ||
		len(result.RecoveryPlans) != 0 || len(planner.requests) != 1 {
		t.Fatalf("result/error/plans = %+v/%v/%d", result, err, len(planner.requests))
	}
}

func TestVerifiedRepairValidationRejectsToolExpansion(t *testing.T) {
	base := goalRunRequest("run-repair-tool-scope")
	base.Context.Budget.MaxSteps = 3
	base.Tools = []ToolDefinition{{Name: "search", Category: ToolCategoryRead}}
	previous := RunResult{
		Context: base.Context, Messages: cloneMessages(base.Messages),
		Steps: []Step{{Index: 1}},
	}
	candidate := base
	candidate.Context.Budget.MaxSteps = 2
	candidate.Messages = cloneMessages(previous.Messages)
	candidate.Tools = append(candidate.Tools, ToolDefinition{Name: "publish", Category: ToolCategoryWrite})

	err := validateVerifiedRepairRequest(base, previous, candidate)
	if !HasErrorCode(err, ErrorUnknownTool) {
		t.Fatalf("validateVerifiedRepairRequest() error = %v", err)
	}
}
