package service

import (
	"context"
	"strings"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const GoalShadowCapabilityResearchDraft = "content.research_draft"

func (s *AgentService) observeResearchThenDraftGoalShadow(
	ctx context.Context,
	goal string,
	source agentEvidence.GroundedDraftSource,
	result agentRuntime.RunResult,
	runErr error,
) {
	if s == nil || !s.goalRuntimeShadow.Enabled || !s.goalRuntimeShadow.ResearchDraftEnabled ||
		s.goalRuntimeShadowObserver == nil {
		return
	}
	s.goalRuntimeShadowObserver.ObserveGoalRuntimeShadow(
		evaluateResearchThenDraftGoalShadow(ctx, goal, source, result, runErr),
	)
}

func evaluateResearchThenDraftGoalShadow(
	ctx context.Context,
	goal string,
	source agentEvidence.GroundedDraftSource,
	result agentRuntime.RunResult,
	runErr error,
) GoalRuntimeShadowObservation {
	observation := GoalRuntimeShadowObservation{
		Capability:         GoalShadowCapabilityResearchDraft,
		LegacyOutcome:      classifyGroundedDraftLegacyOutcome(source, result, runErr),
		GoalOutcome:        agentRuntime.VerificationInconclusive,
		EvidenceComparison: GoalShadowComparisonExecutionIncomplete,
	}
	if runErr != nil || result.Status != agentRuntime.RunStatusCompleted {
		return observation
	}

	task, valid := researchThenDraftShadowTask(goal, source, result.Context.RunID)
	if !valid {
		observation.EvidenceComparison = GoalShadowComparisonEvaluatorError
		return observation
	}
	collector := agentEvidence.ResearchThenDraftGoalCollector{Source: source}
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
	verification, err := (agentEvidence.ResearchThenDraftGoalVerifier{Source: source}).Verify(
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

func researchThenDraftShadowTask(
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
		ID:           "shadow-research-then-draft:" + strings.TrimSpace(runID),
		Goal:         goal,
		AllowedTools: allowedTools,
		CompletionCriteria: []agentRuntime.CompletionCriterion{
			{
				ID:          agentEvidence.GroundedDraftSourcesCriterion,
				Description: "Trusted source evidence was observed for the draft.",
				Required:    true,
			},
			{
				ID:          agentEvidence.ResearchThenDraftOrderCriterion,
				Description: "Trusted research completed before the terminal draft action.",
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
