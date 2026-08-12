package service

import (
	"context"
	"strings"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func (s *AgentService) observeGroundedDraftGoalShadow(
	ctx context.Context,
	goal string,
	source agentEvidence.GroundedDraftSource,
	result agentRuntime.RunResult,
	runErr error,
) {
	if s == nil || !s.goalRuntimeShadow.Enabled || !s.goalRuntimeShadow.GroundedDraftEnabled ||
		s.goalRuntimeShadowObserver == nil {
		return
	}
	s.goalRuntimeShadowObserver.ObserveGoalRuntimeShadow(
		evaluateGroundedDraftGoalShadow(ctx, goal, source, result, runErr),
	)
}

func evaluateGroundedDraftGoalShadow(
	ctx context.Context,
	goal string,
	source agentEvidence.GroundedDraftSource,
	result agentRuntime.RunResult,
	runErr error,
) GoalRuntimeShadowObservation {
	observation := GoalRuntimeShadowObservation{
		Capability:         CapabilityContentDraft,
		LegacyOutcome:      classifyGroundedDraftLegacyOutcome(source, result, runErr),
		GoalOutcome:        agentRuntime.VerificationInconclusive,
		EvidenceComparison: GoalShadowComparisonExecutionIncomplete,
	}
	if runErr != nil || result.Status != agentRuntime.RunStatusCompleted {
		return observation
	}

	task, valid := groundedDraftShadowTask(goal, source, result.Context.RunID)
	if !valid {
		observation.EvidenceComparison = GoalShadowComparisonEvaluatorError
		return observation
	}
	collector := agentEvidence.GroundedDraftGoalCollector{Source: source}
	items, err := collector.Collect(ctx, agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: result, Attempt: 0,
	})
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
	verification, err := (agentEvidence.GroundedDraftGoalVerifier{Source: source}).Verify(
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

	legacyHasEvidence := groundedDraftLegacyHasEvidence(source, result)
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

func groundedDraftShadowTask(
	goal string,
	source agentEvidence.GroundedDraftSource,
	runID string,
) (agentRuntime.TaskSpec, bool) {
	allowedTools := []string{"hybrid_search_tweets"}
	if source == agentEvidence.GroundedDraftSourceWeb {
		allowedTools = []string{"web_search", "page_read"}
	} else if source != agentEvidence.GroundedDraftSourcePlatform {
		return agentRuntime.TaskSpec{}, false
	}
	return agentRuntime.TaskSpec{
		ID:           "shadow-grounded-draft:" + strings.TrimSpace(runID),
		Goal:         goal,
		AllowedTools: allowedTools,
		CompletionCriteria: []agentRuntime.CompletionCriterion{
			{
				ID:          agentEvidence.GroundedDraftSourcesCriterion,
				Description: "Trusted source evidence was observed for the draft.",
				Required:    true,
			},
			{
				ID:          agentEvidence.GroundedDraftArtifactCriterion,
				Description: "The draft artifact links a claim to trusted source evidence.",
				Required:    true,
			},
		},
	}, true
}

func classifyGroundedDraftLegacyOutcome(
	source agentEvidence.GroundedDraftSource,
	result agentRuntime.RunResult,
	runErr error,
) string {
	if runErr != nil || result.Status == agentRuntime.RunStatusFailed {
		return GoalShadowLegacyFailed
	}
	switch result.Status {
	case agentRuntime.RunStatusCompleted:
		if groundedDraftLegacyHasEvidence(source, result) {
			return GoalShadowLegacyCompleted
		}
		return GoalShadowLegacyEvidenceMissing
	case agentRuntime.RunStatusAwaitingHuman, agentRuntime.RunStatusApprovalRequired:
		return GoalShadowLegacySuspended
	default:
		return GoalShadowLegacyUnknown
	}
}

func groundedDraftLegacyHasEvidence(
	source agentEvidence.GroundedDraftSource,
	result agentRuntime.RunResult,
) bool {
	if strings.TrimSpace(result.FinalAnswer) == "" {
		return false
	}
	switch source {
	case agentEvidence.GroundedDraftSourcePlatform:
		return runtimeHasSuccessfulToolEvidence(result, "hybrid_search_tweets")
	case agentEvidence.GroundedDraftSourceWeb:
		return runtimeHasCitableWebSearchEvidence(result)
	default:
		return false
	}
}
