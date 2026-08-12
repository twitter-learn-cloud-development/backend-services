package service

import (
	"context"
	"encoding/json"
	"testing"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestE2E06PlatformTweetFollowUpMigrationDualRecordsSingleExecution(t *testing.T) {
	dialogue := &repository.Dialogue{
		ID: primitive.NewObjectID(), UserID: 42, Mode: repository.ModeConsult,
	}
	repo := &assistRuntimeRepository{
		dialogue: dialogue,
		recent: []*repository.DialogueMessage{{
			Role: repository.RoleAssistant,
			Metadata: map[string]any{
				"capability_ids":                   []any{CapabilityPlatformSearch},
				platformTweetReferencesMetadataKey: []any{"/tweets/2084827196752420864"},
			},
		}},
	}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "authoritative detail",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "detail-1", Type: agentRuntime.ActionToolCall, Name: "get_tweets_by_ids",
				Arguments: json.RawMessage(`{"tweet_ids":"2084827196752420864"}`),
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "detail-1", Name: "get_tweets_by_ids",
				StructuredContent: json.RawMessage(`{"schema":"platform.tweet_detail.v1","items":[{"tweet_id":"2084827196752420864","content":"authoritative detail"}]}`),
			}},
		}},
	}}
	observer := &goalRuntimeShadowObserverFake{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
			{Name: "get_tweets_by_ids", Category: agentRuntime.ToolCategoryRead},
		}}),
		WithGoalRuntimeShadow(GoalRuntimeShadowConfig{
			Enabled: true, PlatformSearchEnabled: true,
		}, observer),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, DialogueKey: dialogue.ID.Hex(), Content: "第一条的详细内容",
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if runner.calls != 1 || len(observer.observations) != 1 {
		t.Fatalf("runtime calls = %d shadow observations = %d", runner.calls, len(observer.observations))
	}
	observation := observer.observations[0]
	if observation.TaskOutcome == nil ||
		observation.TaskOutcome.ExecutionSource != agentRuntime.TaskOutcomeExecutionObserved ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunVerified ||
		observation.EvidenceComparison != GoalShadowComparisonConsistent {
		t.Fatalf("shadow observation = %+v", observation)
	}
	if len(result.Citations) != 1 || result.Citations[0].URL != "/tweets/2084827196752420864" {
		t.Fatalf("legacy citations = %+v", result.Citations)
	}
	if len(observation.TaskOutcome.Evidence.Items) != 2 ||
		observation.TaskOutcome.Evidence.Items[0].Reference != result.Citations[0].URL ||
		observation.TaskOutcome.Evidence.Items[1].Reference != result.Citations[0].URL {
		t.Fatalf("goal evidence = %+v", observation.TaskOutcome.Evidence.Items)
	}
}
