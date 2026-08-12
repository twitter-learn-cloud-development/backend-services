package service

import (
	"context"
	"encoding/json"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestE2E05PlatformSearchMigrationDualRecordsSingleExecution(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "one grounded platform result",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets",
				StructuredContent: json.RawMessage(`{"schema":"platform.tweet_search.v1","items":[{"tweet_id":"2084827196752420864","content":"verified platform evidence"}]}`),
			}},
		}},
	}}
	observer := &goalRuntimeShadowObserverFake{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
		}}}),
		WithGoalRuntimeShadow(GoalRuntimeShadowConfig{
			Enabled: true, PlatformSearchEnabled: true,
		}, observer),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "search platform posts about cloud native",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("legacy runtime calls = %d, want exactly 1", runner.calls)
	}
	if len(observer.observations) != 1 {
		t.Fatalf("shadow observations = %d, want 1", len(observer.observations))
	}
	observation := observer.observations[0]
	if observation.LegacyOutcome != GoalShadowLegacyCompleted ||
		observation.GoalOutcome != agentRuntime.VerificationPassed ||
		observation.EvidenceComparison != GoalShadowComparisonConsistent {
		t.Fatalf("shadow comparison = %+v", observation)
	}
	if observation.TaskOutcome == nil ||
		observation.TaskOutcome.ExecutionSource != agentRuntime.TaskOutcomeExecutionObserved ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunVerified ||
		len(observation.TaskOutcome.PlanDigests) != 0 {
		t.Fatalf("goal task outcome = %+v", observation.TaskOutcome)
	}
	if len(observation.TaskOutcome.Evidence.Items) != 1 ||
		observation.TaskOutcome.Evidence.Items[0].Reference != "/tweets/2084827196752420864" ||
		observation.TaskOutcome.Evidence.Items[0].Source != "hybrid_search_tweets" {
		t.Fatalf("goal evidence = %+v", observation.TaskOutcome.Evidence.Items)
	}
	if len(result.Citations) != 1 || result.Citations[0].URL != "/tweets/2084827196752420864" {
		t.Fatalf("legacy citations = %+v", result.Citations)
	}
}

func TestPlatformSearchObservedOutcomeBlocksLegacyTextOnlyEvidence(t *testing.T) {
	observation := evaluatePlatformSearchGoalShadow(
		context.Background(), "search Go", platformSearchShadowRun(t, false), nil,
	)

	if observation.TaskOutcome == nil ||
		observation.TaskOutcome.ExecutionSource != agentRuntime.TaskOutcomeExecutionObserved ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunBlocked ||
		observation.TaskOutcome.Verification.Status != agentRuntime.VerificationFailed ||
		len(observation.TaskOutcome.Evidence.Items) != 0 {
		t.Fatalf("text-only task outcome = %+v", observation.TaskOutcome)
	}
	if observation.TaskOutcome.Verification.Checks[0].CriterionID != agentEvidence.PlatformSearchResultCriterion {
		t.Fatalf("verification checks = %+v", observation.TaskOutcome.Verification.Checks)
	}
}
