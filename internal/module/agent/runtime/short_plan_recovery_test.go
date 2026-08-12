package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestPlanningCoordinatorCoordinatesSanitizedRecovery(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: validResearchShortPlanProposal(), Usage: TokenUsage{TotalTokens: 4},
	}}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.CompletedSteps = 1

	result, err := coordinator.CoordinateRecovery(
		context.Background(), request,
		ShortPlanRecoveryFeedback{Reason: ShortPlanRecoveryExecutionFailed},
	)
	if err != nil || result.Plan.Digest == "" || len(planner.requests) != 1 {
		t.Fatalf("result/error/requests = %+v/%v/%d", result, err, len(planner.requests))
	}
	feedback := planner.requests[0].RecoveryFeedback
	if feedback == nil || feedback.Reason != ShortPlanRecoveryExecutionFailed {
		t.Fatalf("recovery feedback = %+v", feedback)
	}
}

func TestPlanningCoordinatorRejectsUntrustedRecoveryFeedback(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	request := shortPlanTestRequest()
	request.RecoveryFeedback = &ShortPlanRecoveryFeedback{Reason: ShortPlanRecoveryEvidenceMissing}

	_, err := coordinator.Coordinate(context.Background(), request)
	if !HasErrorCode(err, ErrorInvalidRequest) || len(planner.requests) != 0 {
		t.Fatalf("error/requests = %v/%d", err, len(planner.requests))
	}
}

func TestShortPlanRecoveryPromptContainsNoRawFailure(t *testing.T) {
	request := shortPlanTestRequest()
	request.RecoveryFeedback = &ShortPlanRecoveryFeedback{Reason: ShortPlanRecoveryExecutionFailed}
	messages, err := buildShortPlanModelMessages(request, request.Budget.MaxSteps)
	if err != nil {
		t.Fatalf("buildShortPlanModelMessages() error = %v", err)
	}
	joined := ""
	for _, message := range messages {
		joined += message.Content
	}
	if !strings.Contains(joined, "governed tool action failed") ||
		strings.Contains(joined, "provider unavailable") {
		t.Fatalf("recovery prompt = %q", joined)
	}
}

func TestPlanningCoordinatorRejectsInvalidRecoveryReasonBeforeModel(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})

	_, err := coordinator.CoordinateRecovery(
		context.Background(), shortPlanTestRequest(),
		ShortPlanRecoveryFeedback{Reason: "raw-provider-error"},
	)
	if !HasErrorCode(err, ErrorInvalidRequest) || len(planner.requests) != 0 {
		t.Fatalf("error/requests = %v/%d", err, len(planner.requests))
	}
}
