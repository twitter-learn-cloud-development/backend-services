package service

import (
	"context"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	GoalShadowLegacyCompleted       = "completed"
	GoalShadowLegacyEvidenceMissing = "evidence_missing"
	GoalShadowLegacyFailed          = "failed"
	GoalShadowLegacySuspended       = "suspended"
	GoalShadowLegacyUnknown         = "unknown"

	GoalShadowComparisonConsistent          = "consistent"
	GoalShadowComparisonLegacyOnly          = "legacy_only"
	GoalShadowComparisonGoalOnly            = "goal_only"
	GoalShadowComparisonMissingBoth         = "missing_both"
	GoalShadowComparisonExecutionIncomplete = "execution_incomplete"
	GoalShadowComparisonEvaluatorError      = "evaluator_error"
)

// GoalRuntimeShadowConfig is an immutable rollout snapshot. Shadow evaluation
// consumes an already completed Runtime result and never invokes a model or a
// tool, so enabling it cannot duplicate external side effects.
type GoalRuntimeShadowConfig struct {
	Enabled               bool
	PlatformSearchEnabled bool
	WebResearchEnabled    bool
	GroundedDraftEnabled  bool
	ResearchDraftEnabled  bool
}

type GoalRuntimeShadowObservation struct {
	Capability         string
	LegacyOutcome      string
	GoalOutcome        agentRuntime.VerificationStatus
	EvidenceComparison string
	TaskOutcome        *agentRuntime.TaskOutcome
}

type GoalRuntimeShadowObserver interface {
	ObserveGoalRuntimeShadow(GoalRuntimeShadowObservation)
}

type noopGoalRuntimeShadowObserver struct{}

func (noopGoalRuntimeShadowObserver) ObserveGoalRuntimeShadow(GoalRuntimeShadowObservation) {}

func (s *AgentService) observePlatformSearchGoalShadow(
	ctx context.Context,
	goal string,
	result agentRuntime.RunResult,
	runErr error,
) {
	if s == nil || !s.goalRuntimeShadow.Enabled || !s.goalRuntimeShadow.PlatformSearchEnabled ||
		s.goalRuntimeShadowObserver == nil {
		return
	}
	s.goalRuntimeShadowObserver.ObserveGoalRuntimeShadow(
		evaluatePlatformSearchGoalShadow(ctx, goal, result, runErr),
	)
}

func evaluatePlatformSearchGoalShadow(
	ctx context.Context,
	goal string,
	result agentRuntime.RunResult,
	runErr error,
) GoalRuntimeShadowObservation {
	observation := GoalRuntimeShadowObservation{
		Capability:         CapabilityPlatformSearch,
		LegacyOutcome:      classifyPlatformSearchLegacyOutcome(result, runErr),
		GoalOutcome:        agentRuntime.VerificationInconclusive,
		EvidenceComparison: GoalShadowComparisonExecutionIncomplete,
	}
	if runErr != nil || result.Status != agentRuntime.RunStatusCompleted {
		return observation
	}

	task := agentRuntime.TaskSpec{
		ID:           "shadow-platform-search:" + result.Context.RunID,
		Goal:         goal,
		AllowedTools: []string{"hybrid_search_tweets"},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          agentEvidence.PlatformSearchResultCriterion,
			Description: "A trusted first-party platform search result was observed.",
			Required:    true,
		}},
	}
	collector := agentEvidence.PlatformSearchGoalCollector{}
	items, err := collector.Collect(ctx, agentRuntime.EvidenceCollectionRequest{
		Task: task,
		Run:  result,
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
	verification, err := (agentEvidence.PlatformSearchGoalVerifier{}).Verify(ctx, agentRuntime.VerificationRequest{
		Task:     task,
		Run:      result,
		Evidence: ledger,
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
		Status:       goalStatus,
		Run:          result,
		Verification: verification,
		Evidence:     ledger,
	})
	if err != nil {
		observation.EvidenceComparison = GoalShadowComparisonEvaluatorError
		return observation
	}
	observation.TaskOutcome = &outcome

	legacyHasEvidence := runtimeHasSuccessfulToolEvidence(result, "hybrid_search_tweets")
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

func classifyPlatformSearchLegacyOutcome(
	result agentRuntime.RunResult,
	runErr error,
) string {
	if runErr != nil || result.Status == agentRuntime.RunStatusFailed {
		return GoalShadowLegacyFailed
	}
	switch result.Status {
	case agentRuntime.RunStatusCompleted:
		if runtimeHasSuccessfulToolEvidence(result, "hybrid_search_tweets") {
			return GoalShadowLegacyCompleted
		}
		return GoalShadowLegacyEvidenceMissing
	case agentRuntime.RunStatusAwaitingHuman, agentRuntime.RunStatusApprovalRequired:
		return GoalShadowLegacySuspended
	default:
		return GoalShadowLegacyUnknown
	}
}
