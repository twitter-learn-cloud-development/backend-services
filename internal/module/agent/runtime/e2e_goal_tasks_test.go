package runtime

import (
	"context"
	"encoding/json"
	"testing"
)

func TestE2E02AmbiguousRequestProducesClarificationOutcome(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{{
			ID: "clarify", Kind: ShortPlanStepAskHuman,
			Objective:    "ask which account should receive the consequential action",
			CriterionIDs: []string{"scope-confirmed"},
		}}},
	}}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	request := plannedVerifiedTestRequest()
	request.Task = TaskSpec{
		ID: "E2E-02", Goal: "clarify an ambiguous consequential request",
		CompletionCriteria: []CompletionCriterion{{
			ID: "scope-confirmed", Description: "the target scope is confirmed", Required: true,
		}},
	}
	pending := Action{
		ID: "ask-scope", Type: ActionAskHuman,
		Content: "Which account should receive this action?",
	}
	execution := &scriptedGoalRunner{results: []RunResult{{
		Context: request.Run.Context, Status: RunStatusAwaitingHuman,
		Messages: []Message{{Role: RoleAssistant, Content: pending.Content, Actions: []Action{pending}}},
		Steps:    []Step{{Index: 1, Actions: []Action{pending}}}, PendingAction: &pending,
		PendingResumeKind: ResumeKindHumanResponse,
	}}}
	runner := NewPlannedVerifiedRunner(
		coordinator,
		NewVerifiedRunner(execution, RequiredEvidenceVerifier{}, nil),
	)

	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	outcome, err := BuildTaskOutcome(request.Task, result)
	if err != nil {
		t.Fatalf("BuildTaskOutcome() error = %v", err)
	}
	if outcome.Status != GoalRunSuspended || len(outcome.PlanDigests) != 1 ||
		len(outcome.RecoveryDecisions) != 0 || len(outcome.Artifacts) != 0 ||
		result.Verified.Checkpoint == nil || result.Verified.Run.PendingAction == nil {
		t.Fatalf("outcome/result = %+v/%+v", outcome, result.Verified)
	}
	if request := execution.requests[0]; request.InitialToolChoice != ToolChoiceNone || len(request.Tools) != 0 {
		t.Fatalf("clarification execution request = %+v", request)
	}
}

func TestE2E11ResearchThenDraftProducesVerifiedArtifact(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{
			{
				ID: "research", Kind: ShortPlanStepTool, Objective: "collect grounded research",
				ToolName: "search", CriterionIDs: []string{"research-grounded"},
			},
			{
				ID: "draft", Kind: ShortPlanStepRespond, Objective: "draft only from collected evidence",
				CriterionIDs: []string{"research-grounded", "draft-produced"},
			},
		}},
	}}}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	request := plannedVerifiedTestRequest()
	request.Task = TaskSpec{
		ID: "E2E-11", Goal: "research cloud native evidence and produce a grounded draft",
		CompletionCriteria: []CompletionCriterion{
			{ID: "research-grounded", Description: "research evidence was observed", Required: true},
			{ID: "draft-produced", Description: "a grounded draft artifact was produced", Required: true},
		},
		AllowedTools: []string{"search"},
	}
	request.Run.Tools = []ToolDefinition{{Name: "search", Category: ToolCategoryRead}}
	execution := &scriptedGoalRunner{results: []RunResult{{
		Context: request.Run.Context, Status: RunStatusCompleted,
		FinalAnswer: "A concise draft grounded in source tweet 42.",
		Messages:    []Message{{Role: RoleAssistant, Content: "A concise draft grounded in source tweet 42."}},
		Steps: []Step{{Index: 1, Observations: []Observation{{
			ActionID: "search-42", Name: "search",
			StructuredContent: json.RawMessage(`{"tweet_ids":["42"]}`),
		}}}},
	}}}
	collector := CompositeEvidenceCollector{Collectors: []EvidenceCollector{
		StructuredObservationEvidenceCollector{Bindings: map[string][]string{
			"search": {"research-grounded"},
		}},
		FinalAnswerArtifactEvidenceCollector{
			ArtifactType: "content.draft",
			CriterionIDs: []string{"research-grounded", "draft-produced"},
		},
	}}
	runner := NewPlannedVerifiedRunner(
		coordinator,
		NewVerifiedRunner(execution, RequiredEvidenceVerifier{}, collector),
	)

	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	outcome, err := BuildTaskOutcome(request.Task, result)
	if err != nil {
		t.Fatalf("BuildTaskOutcome() error = %v", err)
	}
	if outcome.Status != GoalRunVerified || outcome.FinalAnswerDigest == "" ||
		len(outcome.Artifacts) != 1 || outcome.Artifacts[0].Type != "content.draft" ||
		outcome.Artifacts[0].Status != VerificationPassed ||
		len(outcome.Artifacts[0].SupportingEvidence) != 2 || len(outcome.Evidence.Items) != 2 {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestE2E18ToolFailureRepairsOnceWithAuditableOutcome(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{
		{Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{
			{
				ID: "attempt", Kind: ShortPlanStepTool, Objective: "try the governed tool",
				ToolName: "search", CriterionIDs: []string{"completed"},
			},
			{
				ID: "respond", Kind: ShortPlanStepRespond, Objective: "respond after the tool",
				CriterionIDs: []string{"completed"},
			},
		}}},
		{Proposal: ShortPlanProposal{Version: ShortPlanVersionV1, Steps: []ShortPlanStep{{
			ID: "recover", Kind: ShortPlanStepRespond, Objective: "complete without repeating the failed action",
			CriterionIDs: []string{"completed"},
		}}}},
	}}
	coordinator, _ := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	request := plannedVerifiedTestRequest()
	request.Task = TaskSpec{
		ID: "E2E-18", Goal: "repair one retryable governed tool failure",
		CompletionCriteria: []CompletionCriterion{{
			ID: "completed", Description: "a verified terminal result exists", Required: true,
		}},
		AllowedTools: []string{"search"}, MaxRepairAttempts: 1,
	}
	request.Run.Tools = []ToolDefinition{{Name: "search", Category: ToolCategoryRead}}
	execution := &requestAwareToolFailureRunner{}
	runner := NewPlannedVerifiedRunner(
		coordinator,
		NewVerifiedRunner(
			execution,
			RequiredEvidenceVerifier{},
			FinalAnswerArtifactEvidenceCollector{
				ArtifactType: "agent.response", CriterionIDs: []string{"completed"},
			},
		),
	)

	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	outcome, err := BuildTaskOutcome(request.Task, result)
	if err != nil {
		t.Fatalf("BuildTaskOutcome() error = %v", err)
	}
	if outcome.Status != GoalRunVerified || len(outcome.PlanDigests) != 2 ||
		len(outcome.RecoveryDecisions) != 1 ||
		outcome.RecoveryDecisions[0].Reason != ShortPlanRecoveryExecutionFailed ||
		len(outcome.Artifacts) != 1 || outcome.Artifacts[0].Status != VerificationPassed {
		t.Fatalf("outcome = %+v", outcome)
	}
	foundFailedObservation := false
	for _, step := range result.Verified.Run.Steps {
		for _, observation := range step.Observations {
			foundFailedObservation = foundFailedObservation || observation.IsError
		}
	}
	if !foundFailedObservation || len(execution.requests) != 2 || result.RecoveryAttempts != 1 {
		t.Fatalf("result = %+v", result)
	}
}
