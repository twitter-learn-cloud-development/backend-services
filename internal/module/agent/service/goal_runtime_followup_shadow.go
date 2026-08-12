package service

import (
	"context"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func (s *AgentService) observePlatformTweetFollowUpGoalShadow(
	ctx context.Context,
	goal string,
	expectedTweetID string,
	priorReference string,
	result agentRuntime.RunResult,
	runErr error,
) {
	if s == nil || !s.goalRuntimeShadow.Enabled || !s.goalRuntimeShadow.PlatformSearchEnabled ||
		s.goalRuntimeShadowObserver == nil {
		return
	}
	s.goalRuntimeShadowObserver.ObserveGoalRuntimeShadow(
		evaluatePlatformTweetFollowUpGoalShadow(
			ctx, goal, expectedTweetID, priorReference, result, runErr,
		),
	)
}

func evaluatePlatformTweetFollowUpGoalShadow(
	ctx context.Context,
	goal string,
	expectedTweetID string,
	priorReference string,
	result agentRuntime.RunResult,
	runErr error,
) GoalRuntimeShadowObservation {
	observation := GoalRuntimeShadowObservation{
		Capability:         CapabilityPlatformSearch,
		LegacyOutcome:      classifyPlatformTweetFollowUpLegacyOutcome(result, expectedTweetID, runErr),
		GoalOutcome:        agentRuntime.VerificationInconclusive,
		EvidenceComparison: GoalShadowComparisonExecutionIncomplete,
	}
	if runErr != nil || result.Status != agentRuntime.RunStatusCompleted {
		return observation
	}

	task := agentRuntime.TaskSpec{
		ID:           "shadow-platform-tweet-follow-up:" + result.Context.RunID,
		Goal:         goal,
		AllowedTools: []string{"get_tweets_by_ids"},
		CompletionCriteria: []agentRuntime.CompletionCriterion{
			{
				ID:          agentEvidence.PlatformTweetPriorReferenceCriterion,
				Description: "A trusted prior platform tweet reference was selected.",
				Required:    true,
			},
			{
				ID:          agentEvidence.PlatformTweetDetailResultCriterion,
				Description: "The authoritative detail tool returned the selected tweet.",
				Required:    true,
			},
		},
	}
	collector := agentEvidence.PlatformTweetFollowUpGoalCollector{
		ExpectedTweetID: expectedTweetID,
		PriorReference:  priorReference,
	}
	items, err := collector.Collect(ctx, agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: result,
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
	verification, err := (agentEvidence.PlatformTweetFollowUpGoalVerifier{
		ExpectedTweetID: expectedTweetID,
		PriorReference:  priorReference,
	}).Verify(ctx, agentRuntime.VerificationRequest{
		Task: task, Run: result, Evidence: ledger,
	})
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

	legacyHasEvidence := runtimeHasPlatformTweetDetailEvidence(result, expectedTweetID)
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

func classifyPlatformTweetFollowUpLegacyOutcome(
	result agentRuntime.RunResult,
	expectedTweetID string,
	runErr error,
) string {
	if runErr != nil || result.Status == agentRuntime.RunStatusFailed {
		return GoalShadowLegacyFailed
	}
	switch result.Status {
	case agentRuntime.RunStatusCompleted:
		if runtimeHasPlatformTweetDetailEvidence(result, expectedTweetID) {
			return GoalShadowLegacyCompleted
		}
		return GoalShadowLegacyEvidenceMissing
	case agentRuntime.RunStatusAwaitingHuman, agentRuntime.RunStatusApprovalRequired:
		return GoalShadowLegacySuspended
	default:
		return GoalShadowLegacyUnknown
	}
}
