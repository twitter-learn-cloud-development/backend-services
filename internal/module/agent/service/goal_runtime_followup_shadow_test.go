package service

import (
	"context"
	"encoding/json"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestE2E06PlatformTweetFollowUpShadowBuildsObservedOutcome(t *testing.T) {
	result := agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-e2e-06-shadow"},
		Status:  agentRuntime.RunStatusCompleted,
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "detail-1", Type: agentRuntime.ActionToolCall, Name: "get_tweets_by_ids",
				Arguments: json.RawMessage(`{"tweet_ids":"9007199254740993"}`),
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "detail-1", Name: "get_tweets_by_ids",
				StructuredContent: json.RawMessage(`{"schema":"platform.tweet_detail.v1","items":[{"tweet_id":"9007199254740993","content":"authoritative detail"}]}`),
			}},
		}},
	}
	observation := evaluatePlatformTweetFollowUpGoalShadow(
		context.Background(), "show the first result", "9007199254740993",
		"/tweets/9007199254740993", result, nil,
	)
	if observation.LegacyOutcome != GoalShadowLegacyCompleted ||
		observation.GoalOutcome != agentRuntime.VerificationPassed ||
		observation.EvidenceComparison != GoalShadowComparisonConsistent {
		t.Fatalf("shadow observation = %+v", observation)
	}
	if observation.TaskOutcome == nil ||
		observation.TaskOutcome.ExecutionSource != agentRuntime.TaskOutcomeExecutionObserved ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunVerified ||
		len(observation.TaskOutcome.PlanDigests) != 0 ||
		len(observation.TaskOutcome.Evidence.Items) != 2 {
		t.Fatalf("task outcome = %+v", observation.TaskOutcome)
	}
	checks := observation.TaskOutcome.Verification.Checks
	if len(checks) != 2 || checks[0].CriterionID != agentEvidence.PlatformTweetPriorReferenceCriterion ||
		checks[1].CriterionID != agentEvidence.PlatformTweetDetailResultCriterion {
		t.Fatalf("verification checks = %+v", checks)
	}
}
