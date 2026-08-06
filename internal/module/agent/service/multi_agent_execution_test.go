package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

type multiAgentRuntimeRunnerFake struct {
	requests        []agentRuntime.RunRequest
	parentBudgets   []agentRuntime.Budget
	failRole        string
	sawParentBudget bool
}

func (r *multiAgentRuntimeRunnerFake) Run(
	ctx context.Context,
	request agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	r.requests = append(r.requests, request)
	if tracker, ok := agentRuntime.BudgetTrackerFromContext(ctx); ok {
		r.sawParentBudget = true
		r.parentBudgets = append(r.parentBudgets, tracker.Budget())
	}

	result := agentRuntime.RunResult{
		Context: request.Context,
		Status:  agentRuntime.RunStatusCompleted,
		Steps:   []agentRuntime.Step{{Index: 1, RoleID: request.Context.RoleID}},
		Usage: agentRuntime.TokenUsage{
			InputTokens: 100, OutputTokens: 20, TotalTokens: 120,
			EstimatedCostMicros: 1000, CostEstimated: true, PricingVersion: "test-v1",
		},
	}
	if err := agentRuntime.RecordBudgetUsage(ctx, result.Usage); err != nil {
		result.Status = agentRuntime.RunStatusFailed
		return result, err
	}

	switch request.Context.RoleID {
	case multiAgentRoleResearcher:
		result.FinalAnswer = "Evidence brief grounded in the matching platform post."
		result.Steps[0].Actions = []agentRuntime.Action{{
			ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
		}}
		result.Steps[0].Observations = []agentRuntime.Observation{{
			ActionID: "search-1", Name: "hybrid_search_tweets", Content: "matching result",
			StructuredContent: json.RawMessage(`{
				"schema":"platform.tweet_search.v1",
				"items":[{"tweet_id":"9007199254740993","content":"verified platform evidence"}]
			}`),
		}}
	case multiAgentRoleDrafter:
		result.FinalAnswer = "A complete evidence-grounded draft with useful detail."
	case multiAgentRoleReviewer:
		result.FinalAnswer = "Final reviewed draft with useful detail and no unrelated claims."
	default:
		result.Status = agentRuntime.RunStatusFailed
		return result, errors.New("unexpected role")
	}
	if r.failRole == request.Context.RoleID {
		result.Status = agentRuntime.RunStatusFailed
		return result, &agentRuntime.RunError{Code: agentRuntime.ErrorModel, Step: 1, Message: "scripted role failure"}
	}
	return result, nil
}

func newExecutableMultiAgentPlanner(t *testing.T) agentStrategy.Planner {
	t.Helper()
	planner, err := NewBuiltInAgentExecutionStrategyPlanner(agentStrategy.Policy{
		Enabled: true, ExecutorAvailable: true, MinimumComplexityScore: 6,
		MaxRoles: 3, MaxParallelRoles: 1, MaxEstimatedLatency: 50 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewBuiltInAgentExecutionStrategyPlanner() error = %v", err)
	}
	return planner
}

func multiAgentResearchRequest() UnifiedAgentRequest {
	return UnifiedAgentRequest{
		UserID:                 42,
		Content:                "Research and compare multiple sources, produce multiple detailed draft options, then review and verify the final answer.",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
	}
}

func TestRunAgentExecutesIsolatedMultiAgentRolesAndAggregatesParentRun(t *testing.T) {
	repo := &assistRuntimeRepository{recent: []*repository.DialogueMessage{
		{Role: repository.RoleUser, Content: "UNRELATED_HISTORY_MARKER"},
		{Role: repository.RoleAssistant, Content: "old answer"},
	}}
	runner := &multiAgentRuntimeRunnerFake{}
	runStore := &memoryAgentExecutionRunStore{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead},
			{Name: "create_tweet", Category: agentRuntime.ToolCategoryWrite, RequiresApproval: true},
		}}),
		WithAgentExecutionStrategyPlanner(newExecutableMultiAgentPlanner(t)),
		WithMultiAgentExecution(true),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), multiAgentResearchRequest())
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.ExecutionStrategyPlan.SelectedStrategy != agentStrategy.KindMultiAgent ||
		result.ExecutionStrategyPlan.ReasonCode != agentStrategy.ReasonMultiAdmitted ||
		result.Response != "Final reviewed draft with useful detail and no unrelated claims." ||
		!result.PublishableDraft {
		t.Fatalf("RunAgent() result = %+v", result)
	}
	if len(runner.requests) != 3 || !runner.sawParentBudget || len(runner.parentBudgets) != 3 {
		t.Fatalf("role calls/parent budget = %d/%v/%d", len(runner.requests), runner.sawParentBudget, len(runner.parentBudgets))
	}
	wantRoles := []string{multiAgentRoleResearcher, multiAgentRoleDrafter, multiAgentRoleReviewer}
	wantProfiles := []string{profileMultiPlatformResearcher, profileMultiDrafter, profileMultiReviewer}
	for index, runtimeRequest := range runner.requests {
		if runtimeRequest.Context.RoleID != wantRoles[index] ||
			runtimeRequest.Context.AgentProfileID != wantProfiles[index] ||
			runtimeRequest.Context.ParentRunID != result.RunID ||
			runtimeRequest.Context.StrategyPlanDigest != result.ExecutionStrategyPlan.PlanDigest {
			t.Fatalf("role request %d context = %+v", index, runtimeRequest.Context)
		}
		if runtimeRequest.Context.RunID == result.RunID || !strings.Contains(runtimeRequest.Context.RunID, ":role:"+wantRoles[index]) {
			t.Fatalf("role request %d run id = %q", index, runtimeRequest.Context.RunID)
		}
		if runtimeRequest.Context.AgentProfileVersion != "v1" || runtimeRequest.Context.PromptTemplateVersion != "v1" {
			t.Fatalf("role request %d version = %s/%s", index, runtimeRequest.Context.AgentProfileVersion, runtimeRequest.Context.PromptTemplateVersion)
		}
	}
	if len(runner.requests[0].Tools) != 1 || runner.requests[0].Tools[0].Name != "hybrid_search_tweets" ||
		len(runner.requests[1].Tools) != 0 || len(runner.requests[2].Tools) != 0 {
		t.Fatalf("role tool scopes = %v/%v/%v", runner.requests[0].Tools, runner.requests[1].Tools, runner.requests[2].Tools)
	}
	if !messagesContain(runner.requests[0].Messages, "UNRELATED_HISTORY_MARKER") ||
		messagesContain(runner.requests[1].Messages, "UNRELATED_HISTORY_MARKER") ||
		messagesContain(runner.requests[2].Messages, "UNRELATED_HISTORY_MARKER") {
		t.Fatalf("history isolation failed for role messages")
	}
	if !messagesContain(runner.requests[1].Messages, "agent.multi_role_handoff.v1") ||
		!messagesContain(runner.requests[2].Messages, "A complete evidence-grounded draft") {
		t.Fatalf("role handoff is missing")
	}
	if len(result.Citations) != 1 || result.Citations[0].SourceID != "9007199254740993" ||
		len(result.ToolActivities) != 1 {
		t.Fatalf("multi-agent evidence = %+v / %+v", result.Citations, result.ToolActivities)
	}
	if len(repo.saved) != 2 || repo.saved[1].Content != result.Response ||
		repo.saved[1].Metadata["execution_strategy"] != string(agentStrategy.KindMultiAgent) {
		t.Fatalf("saved conversation = %+v", repo.saved)
	}
	if runStore.run == nil || runStore.run.ID != result.RunID ||
		runStore.run.Status != repository.AgentExecutionRunCompleted ||
		runStore.run.AgentProfileID != profileMultiAggregate ||
		runStore.run.StepCount != 3 || runStore.run.TotalTokens != 360 ||
		runStore.run.MaxSteps != 5 || runStore.run.MaxTotalTokens != 24000 ||
		runStore.run.MaxEstimatedCostMicros != 100000 {
		t.Fatalf("aggregate authoritative run = %+v", runStore.run)
	}
	if runStore.run.AgentProfileVersion != "v1" || runStore.run.PromptTemplateVersion != "v1" ||
		repo.saved[1].Metadata["profile_set_version"] != "v1" {
		t.Fatalf("aggregate profile set evidence = %+v / %+v", runStore.run, repo.saved[1].Metadata)
	}
}

func TestRunAgentSelectsV2MultiAgentProfileSetAtomically(t *testing.T) {
	resolver, err := NewBuiltInProfileResolver([]profile.Release{{
		ProfileID:            profileUnifiedResearchDraft,
		StableVersion:        "v1",
		CandidateVersion:     "v2",
		CandidateBasisPoints: profile.MaxReleaseBasisPoints,
	}})
	if err != nil {
		t.Fatalf("NewBuiltInProfileResolver() error = %v", err)
	}
	repo := &assistRuntimeRepository{}
	runner := &multiAgentRuntimeRunnerFake{}
	runStore := &memoryAgentExecutionRunStore{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithProfileResolver(resolver),
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
		}}}),
		WithAgentExecutionStrategyPlanner(newExecutableMultiAgentPlanner(t)),
		WithMultiAgentExecution(true),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), multiAgentResearchRequest())
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if len(runner.requests) != 3 {
		t.Fatalf("runtime role calls = %d, want 3", len(runner.requests))
	}
	for index, request := range runner.requests {
		if request.Context.AgentProfileVersion != "v2" || request.Context.PromptTemplateVersion != "v2" {
			t.Fatalf("role request %d version = %s/%s", index, request.Context.AgentProfileVersion, request.Context.PromptTemplateVersion)
		}
	}
	if !messagesContain(runner.requests[1].Messages, "citations[].snippet") ||
		!messagesContain(runner.requests[2].Messages, "180-600 Chinese characters") {
		t.Fatal("v2 profile set prompts were not applied to drafter and reviewer")
	}
	if runStore.run == nil || runStore.run.AgentProfileVersion != "v2" ||
		runStore.run.PromptTemplateVersion != "v2" || result.RunID != runStore.run.ID {
		t.Fatalf("v2 aggregate run evidence = %+v / %+v", runStore.run, result)
	}
	if len(repo.saved) != 2 || repo.saved[1].Metadata["profile_set_anchor"] != profileUnifiedResearchDraft ||
		repo.saved[1].Metadata["profile_set_version"] != "v2" {
		t.Fatalf("v2 conversation profile set evidence = %+v", repo.saved)
	}
}

func TestRunAgentMultiAgentRoleFailureStopsWithoutSingleAgentRetry(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &multiAgentRuntimeRunnerFake{failRole: multiAgentRoleDrafter}
	runStore := &memoryAgentExecutionRunStore{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
		}}}),
		WithAgentExecutionStrategyPlanner(newExecutableMultiAgentPlanner(t)),
		WithMultiAgentExecution(true),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
	)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), multiAgentResearchRequest())
	if !errors.Is(err, ErrMultiAgentRoleFailed) || !agentRuntime.HasErrorCode(err, agentRuntime.ErrorModel) {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if len(runner.requests) != 2 {
		t.Fatalf("runtime role calls = %d, want 2 without reviewer or fallback", len(runner.requests))
	}
	if len(repo.saved) != 0 {
		t.Fatalf("failed multi-agent run persisted conversation messages: %+v", repo.saved)
	}
	if runStore.run == nil || runStore.run.Status != repository.AgentExecutionRunFailed ||
		runStore.run.FailureCode != string(agentRuntime.ErrorModel) ||
		runStore.run.StepCount != 2 || runStore.run.TotalTokens != 240 {
		t.Fatalf("failed aggregate run = %+v", runStore.run)
	}
}

func messagesContain(messages []agentRuntime.Message, value string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, value) {
			return true
		}
	}
	return false
}
