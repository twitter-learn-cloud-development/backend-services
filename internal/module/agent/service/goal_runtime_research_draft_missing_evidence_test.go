package service

import (
	"context"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestE2E11MissingResearchDoesNotBecomeVerifiedCompletion(t *testing.T) {
	t.Parallel()
	answer := "Draft without observed research."
	run := agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "e2e-11-missing-research"},
		Status:  agentRuntime.RunStatusCompleted, FinalAnswer: answer,
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "final-1", Type: agentRuntime.ActionFinalAnswer, Content: answer,
			}},
		}},
	}
	observation := evaluateResearchThenDraftGoalShadow(
		context.Background(),
		"research before drafting",
		agentEvidence.GroundedDraftSourcePlatform,
		run,
		nil,
	)
	if observation.LegacyOutcome != GoalShadowLegacyEvidenceMissing ||
		observation.GoalOutcome != agentRuntime.VerificationFailed ||
		observation.EvidenceComparison != GoalShadowComparisonMissingBoth ||
		observation.TaskOutcome == nil ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunBlocked {
		t.Fatalf("observation = %+v", observation)
	}
	assertGroundedDraftShadowCheck(
		t, observation, agentEvidence.ResearchThenDraftOrderCriterion,
		agentEvidence.ResearchThenDraftOrderMissingCode,
	)
}
