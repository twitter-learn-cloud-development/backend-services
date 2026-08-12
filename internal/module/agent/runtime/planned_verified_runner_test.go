package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type alwaysPassVerifier struct{}

func (alwaysPassVerifier) Verify(context.Context, VerificationRequest) (VerificationResult, error) {
	return VerificationResult{Status: VerificationPassed}, nil
}

func TestPlannedVerifiedRunnerBindsReadPlanToExistingRunner(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{
			{ID: "search", Kind: ShortPlanStepTool, Objective: "find a grounded source", ToolName: "search", CriterionIDs: []string{"source-found"}},
			{ID: "respond", Kind: ShortPlanStepRespond, Objective: "answer from the source", CriterionIDs: []string{"source-found"}},
		}},
		Usage: TokenUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5, EstimatedCostMicros: 7},
	}}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	base := plannedVerifiedTestRequest()
	scripted := &scriptedGoalRunner{results: []RunResult{{
		Context: base.Run.Context, Status: RunStatusCompleted, FinalAnswer: "grounded",
		Messages: []Message{{Role: RoleAssistant, Content: "grounded"}},
		Steps: []Step{{Index: 1, Observations: []Observation{{
			ActionID: "search-1", Name: "search", StructuredContent: json.RawMessage(`{"id":"42"}`),
		}}}},
		Usage: TokenUsage{InputTokens: 4, OutputTokens: 3, TotalTokens: 7, EstimatedCostMicros: 11},
	}}}
	verified := NewVerifiedRunner(
		scripted,
		RequiredEvidenceVerifier{},
		StructuredObservationEvidenceCollector{Bindings: map[string][]string{"search": {"source-found"}}},
	)
	runner := NewPlannedVerifiedRunner(coordinator, verified)

	result, err := runner.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Verified.Status != GoalRunVerified || result.Usage.TotalTokens != 12 ||
		result.Usage.EstimatedCostMicros != 18 {
		t.Fatalf("result = %+v", result)
	}
	if len(scripted.requests) != 1 {
		t.Fatalf("runner requests = %d", len(scripted.requests))
	}
	request := scripted.requests[0]
	if request.InitialToolChoice != ToolChoiceRequired || len(request.Tools) != 1 ||
		request.Tools[0].Name != "search" || request.Context.Budget.MaxTotalTokens != 95 ||
		request.Context.Budget.MaxEstimatedCostMicros != 93 {
		t.Fatalf("bound request = %+v", request)
	}
	if request.Context.StrategyPlanDigest == "" ||
		!strings.Contains(request.Messages[len(request.Messages)-1].Content, request.Context.StrategyPlanDigest) {
		t.Fatalf("plan binding = %+v", request)
	}
	if len(planner.requests) != 1 || len(planner.requests[0].AvailableTools) != 2 {
		t.Fatalf("planning request = %+v", planner.requests)
	}
}

func TestPlannedVerifiedRunnerPreservesClarificationCheckpoint(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{{
			ID: "clarify", Kind: ShortPlanStepAskHuman, Objective: "ask which source scope is required",
			CriterionIDs: []string{"source-found"},
		}}},
	}}}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	base := plannedVerifiedTestRequest()
	pending := Action{ID: "ask-1", Type: ActionAskHuman, Content: "Which source scope should I use?"}
	scripted := &scriptedGoalRunner{results: []RunResult{{
		Context: base.Run.Context, Status: RunStatusAwaitingHuman,
		Messages:      []Message{{Role: RoleAssistant, Content: pending.Content, Actions: []Action{pending}}},
		Steps:         []Step{{Index: 1, Actions: []Action{pending}}},
		PendingAction: &pending, PendingResumeKind: ResumeKindHumanResponse,
	}}}
	runner := NewPlannedVerifiedRunner(
		coordinator,
		NewVerifiedRunner(scripted, RequiredEvidenceVerifier{}, nil),
	)

	result, err := runner.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Verified.Status != GoalRunSuspended || result.Verified.Checkpoint == nil ||
		result.Verified.Checkpoint.Run.Context.StrategyPlanDigest == "" {
		t.Fatalf("result = %+v", result)
	}
	if request := scripted.requests[0]; request.InitialToolChoice != ToolChoiceNone || len(request.Tools) != 0 {
		t.Fatalf("clarification request = %+v", request)
	}
}

func TestPlannedVerifiedRunnerRejectsWritePlanBeforeExecution(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{{
			ID: "publish", Kind: ShortPlanStepTool, Objective: "publish the result", ToolName: "publish",
			CriterionIDs: []string{"source-found"},
		}}},
	}}}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	base := plannedVerifiedTestRequest()
	base.Task.AllowedTools = []string{"publish"}
	base.Run.Tools = []ToolDefinition{{Name: "publish", Category: ToolCategoryWrite}}
	scripted := &scriptedGoalRunner{}
	runner := NewPlannedVerifiedRunner(coordinator, NewVerifiedRunner(scripted, alwaysPassVerifier{}, nil))

	_, err := runner.Run(context.Background(), base)
	if !HasErrorCode(err, ErrorUnsupported) || len(scripted.requests) != 0 {
		t.Fatalf("error/runner calls = %v/%d", err, len(scripted.requests))
	}
}

func TestPlannedVerifiedRunnerRejectsConflictingPlanDigest(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{{
			ID: "respond", Kind: ShortPlanStepRespond, Objective: "answer directly",
			CriterionIDs: []string{"source-found"},
		}}},
	}}}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	base := plannedVerifiedTestRequest()
	base.Run.Context.StrategyPlanDigest = "different-plan"
	scripted := &scriptedGoalRunner{}
	runner := NewPlannedVerifiedRunner(coordinator, NewVerifiedRunner(scripted, alwaysPassVerifier{}, nil))

	_, err := runner.Run(context.Background(), base)
	if !HasErrorCode(err, ErrorInvalidRequest) || len(scripted.requests) != 0 {
		t.Fatalf("error/runner calls = %v/%d", err, len(scripted.requests))
	}
}

func plannedVerifiedTestRequest() PlannedVerifiedRunRequest {
	return PlannedVerifiedRunRequest{
		Task: TaskSpec{
			ID: "task-planned-search", Goal: "return a grounded result",
			CompletionCriteria: []CompletionCriterion{{
				ID: "source-found", Description: "a source was observed", Required: true,
			}},
			AllowedTools: []string{"search", "unused"},
		},
		Run: RunRequest{
			Context: RunContext{
				RunID: "run-planned", UserID: 7,
				Budget: Budget{MaxSteps: 4, MaxTotalTokens: 100, MaxEstimatedCostMicros: 100},
			},
			Model:    "test-model",
			Messages: []Message{{Role: RoleUser, Content: "find it"}},
			Tools: []ToolDefinition{
				{Name: "search", Category: ToolCategoryRead},
				{Name: "unused", Category: ToolCategoryRead},
			},
		},
	}
}
