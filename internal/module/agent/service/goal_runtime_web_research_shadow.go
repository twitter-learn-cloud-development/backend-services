package service

import (
	"context"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func (s *AgentService) observeWebResearchGoalShadow(
	ctx context.Context,
	goal string,
	result agentRuntime.RunResult,
	runErr error,
) {
	if s == nil || !s.goalRuntimeShadow.Enabled || !s.goalRuntimeShadow.WebResearchEnabled ||
		s.goalRuntimeShadowObserver == nil {
		return
	}
	s.goalRuntimeShadowObserver.ObserveGoalRuntimeShadow(
		evaluateWebResearchGoalShadow(ctx, goal, result, runErr),
	)
}

func evaluateWebResearchGoalShadow(
	ctx context.Context,
	goal string,
	result agentRuntime.RunResult,
	runErr error,
) GoalRuntimeShadowObservation {
	observation := GoalRuntimeShadowObservation{
		Capability:         CapabilityWebSearch,
		LegacyOutcome:      classifyWebResearchLegacyOutcome(result, runErr),
		GoalOutcome:        agentRuntime.VerificationInconclusive,
		EvidenceComparison: GoalShadowComparisonExecutionIncomplete,
	}
	if result.Status != agentRuntime.RunStatusCompleted &&
		result.Status != agentRuntime.RunStatusFailed {
		return observation
	}

	task := agentRuntime.TaskSpec{
		ID:           "shadow-web-research:" + result.Context.RunID,
		Goal:         goal,
		AllowedTools: []string{"web_search", "page_read"},
		CompletionCriteria: []agentRuntime.CompletionCriterion{
			{
				ID:          agentEvidence.WebSearchSourcesCriterion,
				Description: "A configured web provider returned a public source.",
				Required:    true,
			},
			{
				ID:          agentEvidence.WebPageContentCriterion,
				Description: "A page discovered by search was read as structured evidence.",
				Required:    true,
			},
		},
	}
	items, err := (agentEvidence.WebResearchGoalCollector{}).Collect(
		ctx,
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: result},
	)
	if err != nil {
		observation.EvidenceComparison = GoalShadowComparisonEvaluatorError
		return observation
	}
	ledger := agentRuntime.EvidenceLedger{}
	for _, item := range items {
		ledger, err = ledger.With(item)
		if err != nil {
			observation.EvidenceComparison = GoalShadowComparisonEvaluatorError
			return observation
		}
	}
	verification, err := (agentEvidence.WebResearchGoalVerifier{}).Verify(
		ctx,
		agentRuntime.VerificationRequest{Task: task, Run: result, Evidence: ledger},
	)
	if err != nil {
		observation.EvidenceComparison = GoalShadowComparisonEvaluatorError
		return observation
	}
	observation.GoalOutcome = verification.Status
	goalStatus := agentRuntime.GoalRunBlocked
	if verification.Passed() {
		goalStatus = agentRuntime.GoalRunVerified
	}
	outcome, err := agentRuntime.BuildObservedTaskOutcome(task, agentRuntime.VerifiedRunResult{
		Status: goalStatus, Run: result, Verification: verification, Evidence: ledger,
	})
	if err != nil {
		observation.EvidenceComparison = GoalShadowComparisonEvaluatorError
		return observation
	}
	observation.TaskOutcome = &outcome

	legacyHasEvidence := runtimeHasSuccessfulToolEvidence(result, "web_search")
	goalHasEvidence := verification.Passed()
	switch {
	case legacyHasEvidence && goalHasEvidence:
		observation.EvidenceComparison = GoalShadowComparisonConsistent
	case legacyHasEvidence:
		observation.EvidenceComparison = GoalShadowComparisonLegacyOnly
	case goalHasEvidence:
		observation.EvidenceComparison = GoalShadowComparisonGoalOnly
	default:
		observation.EvidenceComparison = GoalShadowComparisonMissingBoth
	}
	return observation
}

func classifyWebResearchLegacyOutcome(
	result agentRuntime.RunResult,
	runErr error,
) string {
	if runErr != nil || result.Status == agentRuntime.RunStatusFailed {
		return GoalShadowLegacyFailed
	}
	switch result.Status {
	case agentRuntime.RunStatusCompleted:
		if runtimeHasSuccessfulToolEvidence(result, "web_search") {
			return GoalShadowLegacyCompleted
		}
		return GoalShadowLegacyEvidenceMissing
	case agentRuntime.RunStatusAwaitingHuman, agentRuntime.RunStatusApprovalRequired:
		return GoalShadowLegacySuspended
	default:
		return GoalShadowLegacyUnknown
	}
}
