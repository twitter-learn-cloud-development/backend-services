package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestPlannedVerifiedRunnerMapsDirectResponseWithoutPromotingObjective(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{{
			ID: "respond", Kind: ShortPlanStepRespond, Objective: "untrusted objective marker",
			CriterionIDs: []string{"source-found"},
		}}},
	}}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	base := plannedVerifiedTestRequest()
	scripted := &scriptedGoalRunner{results: []RunResult{{
		Context: base.Run.Context, Status: RunStatusCompleted, FinalAnswer: "done",
		Messages: []Message{{Role: RoleAssistant, Content: "done"}},
		Steps:    []Step{{Index: 1}},
	}}}
	runner := NewPlannedVerifiedRunner(
		coordinator,
		NewVerifiedRunner(scripted, alwaysPassVerifier{}, nil),
	)

	result, err := runner.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Verified.Status != GoalRunVerified || len(scripted.requests) != 1 {
		t.Fatalf("result/runner calls = %+v/%d", result, len(scripted.requests))
	}
	request := scripted.requests[0]
	if request.InitialToolChoice != ToolChoiceNone || len(request.Tools) != 0 {
		t.Fatalf("direct response request = %+v", request)
	}
	instruction := request.Messages[len(request.Messages)-1].Content
	if strings.Contains(instruction, "untrusted objective marker") {
		t.Fatalf("untrusted plan objective was promoted into developer context")
	}
	if !strings.Contains(instruction, request.Context.StrategyPlanDigest) {
		t.Fatalf("execution instruction is not digest-bound: %q", instruction)
	}
}
