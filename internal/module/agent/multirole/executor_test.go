package multirole

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/profile"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

type scriptedRoleRunner struct {
	requests []agentRuntime.RunRequest
	failRole string
}

func (runner *scriptedRoleRunner) Run(
	ctx context.Context,
	request agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	runner.requests = append(runner.requests, request)
	result := agentRuntime.RunResult{
		Context: request.Context, Status: agentRuntime.RunStatusCompleted,
		Steps: []agentRuntime.Step{{Index: 1, RoleID: request.Context.RoleID}},
		Usage: agentRuntime.TokenUsage{
			InputTokens: 20, OutputTokens: 10, TotalTokens: 30,
			EstimatedCostMicros: 100, CostEstimated: true, PricingVersion: "test-v1",
		},
	}
	if err := agentRuntime.RecordBudgetUsage(ctx, result.Usage); err != nil {
		result.Status = agentRuntime.RunStatusFailed
		return result, err
	}
	switch request.Context.RoleID {
	case RoleResearcher:
		result.FinalAnswer = "grounded evidence"
		result.Steps[0].Actions = []agentRuntime.Action{{
			ID: "search", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
		}}
		result.Steps[0].Observations = []agentRuntime.Observation{{
			ActionID: "search", Name: "hybrid_search_tweets", Content: "evidence",
		}}
	case RoleDrafter:
		result.FinalAnswer = "complete draft"
	case RoleReviewer:
		result.FinalAnswer = "reviewed final"
	default:
		return result, errors.New("unexpected role")
	}
	if runner.failRole == request.Context.RoleID {
		result.Status = agentRuntime.RunStatusFailed
		return result, &agentRuntime.RunError{Code: agentRuntime.ErrorModel, Step: 1}
	}
	return result, nil
}

func TestExecutorRunsIsolatedSequentialRolesAndAggregates(t *testing.T) {
	runner := &scriptedRoleRunner{}
	executor := NewExecutor(runner, nil)
	result, err := executor.Execute(t.Context(), validMultiRoleRequest(t))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Aggregate.Status != agentRuntime.RunStatusCompleted ||
		result.Aggregate.FinalAnswer != "reviewed final" || len(result.Roles) != 3 ||
		result.Aggregate.Usage.TotalTokens != 90 {
		t.Fatalf("Execute() result = %+v", result)
	}
	if len(runner.requests) != 3 || len(runner.requests[0].Tools) != 1 ||
		len(runner.requests[1].Tools) != 0 || len(runner.requests[2].Tools) != 0 {
		t.Fatalf("role requests = %+v", runner.requests)
	}
	if runner.requests[0].InitialToolChoice != agentRuntime.ToolChoiceRequired ||
		runner.requests[1].InitialToolChoice != "" || runner.requests[2].InitialToolChoice != "" {
		t.Fatalf("role initial tool choices = %q, %q, %q",
			runner.requests[0].InitialToolChoice,
			runner.requests[1].InitialToolChoice,
			runner.requests[2].InitialToolChoice,
		)
	}
	if !requestMessagesContain(runner.requests[0], "prior context") ||
		requestMessagesContain(runner.requests[1], "prior context") ||
		requestMessagesContain(runner.requests[2], "prior context") {
		t.Fatal("history escaped the researcher role")
	}
	if !requestMessagesContain(runner.requests[1], EvidenceHandoffSchema) ||
		!requestMessagesContain(runner.requests[2], "complete draft") {
		t.Fatal("bounded role handoff is missing")
	}
	if result.Aggregate.Context.Budget.MaxSteps != 5 ||
		result.Aggregate.Context.Budget.MaxTotalTokens != 24_000 ||
		result.Aggregate.Context.Budget.MaxEstimatedCostMicros != 100_000 {
		t.Fatalf("parent budget = %+v", result.Aggregate.Context.Budget)
	}
}

func TestExecutorStopsAfterRoleFailureWithoutFallback(t *testing.T) {
	runner := &scriptedRoleRunner{failRole: RoleDrafter}
	result, err := NewExecutor(runner, nil).Execute(t.Context(), validMultiRoleRequest(t))
	if !errors.Is(err, ErrRoleExecutionFailed) || !agentRuntime.HasErrorCode(err, agentRuntime.ErrorModel) {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(runner.requests) != 2 || len(result.Roles) != 2 || result.Aggregate.Status != agentRuntime.RunStatusFailed {
		t.Fatalf("failed execution = %+v requests=%d", result, len(runner.requests))
	}
}

func TestExecutorRejectsPlanThatExceedsParentProfile(t *testing.T) {
	request := validMultiRoleRequest(t)
	request.Profiles.Parent.Budget.MaxTotalTokens = 1

	_, err := NewExecutor(&scriptedRoleRunner{}, nil).Execute(t.Context(), request)
	if !errors.Is(err, ErrPlanUnsupported) || !strings.Contains(err.Error(), "parent profile") {
		t.Fatalf("Execute() error = %v, want bounded parent rejection", err)
	}
}

func TestExecutorRejectsRolePlanThatExceedsProfileBeforeExecution(t *testing.T) {
	request := validMultiRoleRequest(t)
	request.Profiles.Drafter.Budget.MaxTotalTokens = 1
	runner := &scriptedRoleRunner{}

	_, err := NewExecutor(runner, nil).Execute(t.Context(), request)
	if !errors.Is(err, ErrPlanUnsupported) || !strings.Contains(err.Error(), RoleDrafter) {
		t.Fatalf("Execute() error = %v, want role budget rejection", err)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner received %d requests before preflight failure", len(runner.requests))
	}
}

func TestExecutorRejectsMixedProfileSetBeforeExecution(t *testing.T) {
	request := validMultiRoleRequest(t)
	request.Profiles.Drafter.Version = "v2"
	request.Profiles.Drafter.Prompt.Version = "v2"
	runner := &scriptedRoleRunner{}

	_, err := NewExecutor(runner, nil).Execute(t.Context(), request)
	if err == nil || !strings.Contains(err.Error(), "does not match parent profile set version") {
		t.Fatalf("Execute() error = %v, want mixed profile set rejection", err)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner received %d requests before profile set validation", len(runner.requests))
	}
}

func validMultiRoleRequest(t *testing.T) Request {
	t.Helper()
	parent := testRoleProfile("parent", 6, 12_000, 3_000, 36_000, 120_000, 60*time.Second, "hybrid_search_tweets")
	researcher := testRoleProfile("researcher-profile", 3, 8_000, 2_000, 10_000, 45_000, 25*time.Second, "hybrid_search_tweets")
	drafter := testRoleProfile("drafter-profile", 1, 6_000, 3_000, 9_000, 35_000, 17*time.Second)
	reviewer := testRoleProfile("reviewer-profile", 1, 3_000, 2_000, 5_000, 20_000, 8*time.Second)
	template := ResearchDraftTemplate(
		"platform.research_draft.v1", "runtime.research_draft",
		"platform.search", "content.draft", []string{"hybrid_search_tweets"},
	)
	planner, err := agentStrategy.NewDeterministicPlanner(agentStrategy.Policy{
		Enabled: true, ExecutorAvailable: true, MinimumComplexityScore: 1,
		MaxRoles: 3, MaxParallelRoles: 1, MaxEstimatedLatency: 50 * time.Second,
	}, []agentStrategy.Template{template})
	if err != nil {
		t.Fatalf("NewDeterministicPlanner() error = %v", err)
	}
	plan, err := planner.Plan(t.Context(), agentStrategy.Request{
		Query: "research and draft", ExecutionProfile: "runtime.research_draft",
		CapabilityIDs: []string{"platform.search", "content.draft"},
		Budget:        parent.Budget, AllowedTools: parent.AllowedTools,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	return Request{
		ParentContext: agentRuntime.RunContext{RunID: "parent-run", UserID: 7, Mode: agentRuntime.ModeAssist},
		Plan:          plan, Model: "fixed-model", Input: "write a grounded draft",
		History: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "prior context"}},
		Tools: []agentRuntime.ToolDefinition{{
			Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
		}},
		RequiredTool: "hybrid_search_tweets",
		Profiles:     Profiles{Parent: parent, Researcher: researcher, Drafter: drafter, Reviewer: reviewer},
		Handoff: EvidenceHandoffBuilderFunc(func(summary string, _ agentRuntime.RunResult) (string, error) {
			return EncodeEvidenceHandoff(summary, []Citation{{
				CitationID: "platform_tweet:1", SourceType: "platform_tweet", SourceID: "1", Snippet: "evidence",
			}})
		}),
	}
}

func testRoleProfile(
	id string,
	steps, input, output, total int,
	cost int64,
	timeout time.Duration,
	tools ...string,
) profile.AgentProfile {
	return profile.AgentProfile{
		ID: id, Version: "v1",
		Prompt: profile.PromptProfile{ID: id + ".prompt", Version: "v1", SystemPrompt: "system " + id},
		Budget: agentRuntime.Budget{
			MaxSteps: steps, MaxInputTokens: input, MaxOutputTokens: output,
			MaxTotalTokens: total, MaxEstimatedCostMicros: cost, Timeout: timeout,
		},
		AllowedTools: append([]string(nil), tools...),
	}
}

func requestMessagesContain(request agentRuntime.RunRequest, marker string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, marker) {
			return true
		}
	}
	return false
}
