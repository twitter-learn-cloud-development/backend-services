package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type requestAwareToolFailureRunner struct {
	requests []RunRequest
}

func (runner *requestAwareToolFailureRunner) Run(
	_ context.Context,
	request RunRequest,
) (RunResult, error) {
	runner.requests = append(runner.requests, request)
	if len(runner.requests) == 1 {
		action := Action{ID: "search-failed", Type: ActionToolCall, Name: "search"}
		messages := cloneMessages(request.Messages)
		messages = append(messages, Message{Role: RoleAssistant, Actions: []Action{action}})
		return RunResult{
			Context: request.Context, Status: RunStatusFailed, Messages: messages,
			Steps: []Step{{
				Index: 1, Actions: []Action{action},
				Observations: []Observation{{ActionID: action.ID, Name: action.Name, IsError: true}},
			}},
			Usage: TokenUsage{TotalTokens: 7},
		}, &RunError{Code: ErrorTool, ActionID: action.ID, Message: "raw-provider-secret"}
	}
	messages := cloneMessages(request.Messages)
	messages = append(messages, Message{Role: RoleAssistant, Content: "recovered"})
	return RunResult{
		Context: request.Context, Status: RunStatusCompleted, FinalAnswer: "recovered",
		Messages: messages, Steps: []Step{{Index: 1}}, Usage: TokenUsage{TotalTokens: 3},
	}, nil
}

func TestPlannedVerifiedRunnerRecoversOnceFromToolFailure(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{
		{
			Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{
				{ID: "search", Kind: ShortPlanStepTool, Objective: "find evidence", ToolName: "search", CriterionIDs: []string{"source-found"}},
				{ID: "answer", Kind: ShortPlanStepRespond, Objective: "answer", CriterionIDs: []string{"source-found"}},
			}},
			Usage: TokenUsage{TotalTokens: 5},
		},
		{
			Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{{
				ID: "recover-answer", Kind: ShortPlanStepRespond, Objective: "answer safely",
				CriterionIDs: []string{"source-found"},
			}}},
			Usage: TokenUsage{TotalTokens: 4},
		},
	}}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	execution := &requestAwareToolFailureRunner{}
	runner := NewPlannedVerifiedRunner(
		coordinator,
		NewVerifiedRunner(execution, alwaysPassVerifier{}, nil),
	)
	request := plannedVerifiedTestRequest()
	request.Task.MaxRepairAttempts = 1

	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Verified.Status != GoalRunVerified || result.RecoveryAttempts != 1 ||
		result.LastRecoveryReason != ShortPlanRecoveryExecutionFailed || len(result.RecoveryPlans) != 1 {
		t.Fatalf("recovery result = %+v", result)
	}
	if result.Usage.TotalTokens != 19 || len(execution.requests) != 2 || len(planner.requests) != 2 {
		t.Fatalf("usage/execution/planning = %+v/%d/%d", result.Usage, len(execution.requests), len(planner.requests))
	}
	if feedback := planner.requests[1].RecoveryFeedback; feedback == nil ||
		feedback.Reason != ShortPlanRecoveryExecutionFailed {
		t.Fatalf("recovery feedback = %+v", feedback)
	}
	second := execution.requests[1]
	foundSanitizedToolMessage := false
	for _, message := range second.Messages {
		if strings.Contains(message.Content, "raw-provider-secret") {
			t.Fatalf("raw failure leaked into recovery messages: %+v", second.Messages)
		}
		if message.Role == RoleTool && message.ToolCallID == "search-failed" {
			foundSanitizedToolMessage = message.Content == sanitizedToolFailureMessage
		}
	}
	if !foundSanitizedToolMessage || second.InitialToolChoice != ToolChoiceNone || len(second.Tools) != 0 {
		t.Fatalf("recovery request = %+v", second)
	}
}

func TestPlannedVerifiedRunnerReplansMissingEvidenceWithSameTool(t *testing.T) {
	plan := ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{
		{ID: "search", Kind: ShortPlanStepTool, Objective: "find evidence", ToolName: "search", CriterionIDs: []string{"source-found"}},
		{ID: "answer", Kind: ShortPlanStepRespond, Objective: "answer", CriterionIDs: []string{"source-found"}},
	}}
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{
		{Proposal: plan, Usage: TokenUsage{TotalTokens: 2}},
		{Proposal: plan, Usage: TokenUsage{TotalTokens: 3}},
	}}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	request := plannedVerifiedTestRequest()
	request.Task.MaxRepairAttempts = 1
	scripted := &scriptedGoalRunner{results: []RunResult{
		{
			Context: request.Run.Context, Status: RunStatusCompleted,
			Messages: []Message{{Role: RoleAssistant, Content: "not grounded"}},
			Steps:    []Step{{Index: 1}}, Usage: TokenUsage{TotalTokens: 2},
		},
		{
			Context: request.Run.Context, Status: RunStatusCompleted,
			Messages: []Message{{Role: RoleAssistant, Content: "grounded"}},
			Steps: []Step{{Index: 1, Observations: []Observation{{
				ActionID: "search-2", Name: "search",
				StructuredContent: json.RawMessage(`{"id":"42"}`),
			}}}},
			Usage: TokenUsage{TotalTokens: 2},
		},
	}}
	verified := NewVerifiedRunner(
		scripted,
		RequiredEvidenceVerifier{},
		StructuredObservationEvidenceCollector{Bindings: map[string][]string{"search": {"source-found"}}},
	)

	result, err := NewPlannedVerifiedRunner(coordinator, verified).Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Verified.Status != GoalRunVerified || result.RecoveryAttempts != 1 ||
		result.LastRecoveryReason != ShortPlanRecoveryEvidenceMissing {
		t.Fatalf("result = %+v", result)
	}
	if got := planner.requests[1].TargetCriterionIDs; len(got) != 1 || got[0] != "source-found" {
		t.Fatalf("recovery targets = %v", got)
	}
}
