package service

import (
	"context"
	"encoding/json"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestE2E09PlatformGroundedDraftDualRecordsSingleExecution(t *testing.T) {
	t.Parallel()
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: groundedDraftPlatformShadowRun(
		"Go 的并发模型适合组织云原生任务。 [/tweets/2084827196752420864]",
	)}
	observer := &goalRuntimeShadowObserverFake{}
	service := newGroundedDraftShadowService(t, repo, runner, observer, false)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "基于站内资料写一篇 Go 云原生草稿",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	assertGroundedDraftMigration(
		t, result, repo, runner, observer, "/tweets/2084827196752420864",
	)
}

func TestE2E09WebGroundedDraftDualRecordsSingleExecution(t *testing.T) {
	t.Parallel()
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: groundedDraftWebShadowRun(
		"Go 官方发布页持续记录版本演进。 [https://go.dev/doc/devel/release]",
	)}
	observer := &goalRuntimeShadowObserverFake{}
	service := newGroundedDraftShadowService(t, repo, runner, observer, true)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "基于公网资料写一篇 Go 版本演进草稿",
		PreferredCapabilityIDs: []string{CapabilityWebSearch, CapabilityContentDraft},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	assertGroundedDraftMigration(
		t, result, repo, runner, observer, "https://go.dev/doc/devel/release",
	)
}

func TestE2E09MissingCitationIsObservedWithoutChangingLegacyResponse(t *testing.T) {
	t.Parallel()
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: groundedDraftPlatformShadowRun(
		"Go 的并发模型适合组织云原生任务。",
	)}
	observer := &goalRuntimeShadowObserverFake{}
	service := newGroundedDraftShadowService(t, repo, runner, observer, false)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "基于站内资料写草稿",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Response != runner.result.FinalAnswer || runner.calls != 1 || len(repo.saved) != 2 ||
		len(observer.observations) != 1 {
		t.Fatalf("response/calls/saved/observations = %q/%d/%d/%d", result.Response, runner.calls, len(repo.saved), len(observer.observations))
	}
	observation := observer.observations[0]
	if observation.LegacyOutcome != GoalShadowLegacyCompleted ||
		observation.GoalOutcome != agentRuntime.VerificationFailed ||
		observation.EvidenceComparison != GoalShadowComparisonLegacyOnly ||
		observation.TaskOutcome == nil ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunBlocked {
		t.Fatalf("observation = %+v", observation)
	}
	assertGroundedDraftShadowCheck(
		t, observation, agentEvidence.GroundedDraftArtifactCriterion,
		agentEvidence.GroundedDraftCitationMissingCode,
	)
}

func TestObserveGroundedDraftGoalShadowHonorsDedicatedFlag(t *testing.T) {
	t.Parallel()
	observer := &goalRuntimeShadowObserverFake{}
	service := &AgentService{goalRuntimeShadowObserver: observer}
	run := groundedDraftPlatformShadowRun(
		"Go 的并发模型适合组织云原生任务。 [/tweets/2084827196752420864]",
	)

	service.goalRuntimeShadow = GoalRuntimeShadowConfig{Enabled: true, PlatformSearchEnabled: true}
	service.observeGroundedDraftGoalShadow(
		context.Background(), "draft", agentEvidence.GroundedDraftSourcePlatform, run, nil,
	)
	if len(observer.observations) != 0 {
		t.Fatalf("disabled grounded draft shadow emitted %d observations", len(observer.observations))
	}
	service.goalRuntimeShadow.GroundedDraftEnabled = true
	service.observeGroundedDraftGoalShadow(
		context.Background(), "draft", agentEvidence.GroundedDraftSourcePlatform, run, nil,
	)
	if len(observer.observations) != 1 {
		t.Fatalf("enabled grounded draft shadow emitted %d observations", len(observer.observations))
	}
}

func newGroundedDraftShadowService(
	t *testing.T,
	repo *assistRuntimeRepository,
	runner *capturingRuntimeRunner,
	observer *goalRuntimeShadowObserverFake,
	web bool,
) *AgentService {
	t.Helper()
	options := []BuiltInAgentCapabilityCatalogOption{}
	tools := []agentRuntime.ToolDefinition{{
		Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
	}}
	if web {
		options = append(options, WithAvailableWebSearchCapability())
		tools = []agentRuntime.ToolDefinition{
			{Name: "web_search", Category: agentRuntime.ToolCategoryRead},
			{Name: "page_read", Category: agentRuntime.ToolCategoryRead},
		}
	}
	catalog, err := NewBuiltInAgentCapabilityCatalog(options...)
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	return NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: tools}),
		WithGoalRuntimeShadow(GoalRuntimeShadowConfig{
			Enabled: true, GroundedDraftEnabled: true,
		}, observer),
	)
}

func assertGroundedDraftMigration(
	t *testing.T,
	result *UnifiedAgentResult,
	repo *assistRuntimeRepository,
	runner *capturingRuntimeRunner,
	observer *goalRuntimeShadowObserverFake,
	wantCitation string,
) {
	t.Helper()
	if runner.calls != 1 || len(repo.saved) != 2 || len(observer.observations) != 1 {
		t.Fatalf("calls/saved/observations = %d/%d/%d", runner.calls, len(repo.saved), len(observer.observations))
	}
	if len(result.Citations) != 1 || result.Citations[0].URL != wantCitation {
		t.Fatalf("legacy citations = %+v", result.Citations)
	}
	observation := observer.observations[0]
	if observation.LegacyOutcome != GoalShadowLegacyCompleted ||
		observation.GoalOutcome != agentRuntime.VerificationPassed ||
		observation.EvidenceComparison != GoalShadowComparisonConsistent ||
		observation.TaskOutcome == nil ||
		observation.TaskOutcome.ExecutionSource != agentRuntime.TaskOutcomeExecutionObserved ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunVerified ||
		len(observation.TaskOutcome.Artifacts) != 1 ||
		observation.TaskOutcome.Artifacts[0].Type != agentEvidence.GroundedDraftArtifactType ||
		len(observation.TaskOutcome.Artifacts[0].SupportingEvidence) != 2 {
		t.Fatalf("observation = %+v", observation)
	}
}

func assertGroundedDraftShadowCheck(
	t *testing.T,
	observation GoalRuntimeShadowObservation,
	criterionID string,
	wantCode string,
) {
	t.Helper()
	for _, check := range observation.TaskOutcome.Verification.Checks {
		if check.CriterionID == criterionID {
			if check.Code != wantCode {
				t.Fatalf("check = %+v, want code %q", check, wantCode)
			}
			return
		}
	}
	t.Fatalf("criterion %q missing from %+v", criterionID, observation.TaskOutcome.Verification.Checks)
}

func groundedDraftPlatformShadowRun(answer string) agentRuntime.RunResult {
	return agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: answer,
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets", Content: "platform results",
				StructuredContent: mustJSONRaw(agentEvidence.PlatformTweetSearchResult{
					Schema: agentEvidence.PlatformTweetSearchSchema, Query: "cloud native Go",
					Items: []agentEvidence.PlatformTweetSearchEvidence{{
						TweetID: "2084827196752420864",
						Content: "Go concurrency can organize cloud-native workloads.",
					}},
				}),
			}},
		}},
	}
}

func groundedDraftWebShadowRun(answer string) agentRuntime.RunResult {
	return agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: answer,
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "web_search",
				Arguments: json.RawMessage(`{"query":"Go release","count":3}`),
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "web_search", Content: "web results",
				StructuredContent: mustJSONRaw(agentEvidence.WebSearchResult{
					Schema: agentEvidence.WebSearchSchema, Provider: "brave", Query: "Go release",
					Items: []agentEvidence.WebSearchEvidence{{
						Rank: 1, URL: "https://go.dev/doc/devel/release",
						Title: "Go release history", Snippet: "Official release history",
					}},
				}),
			}},
		}},
	}
}
