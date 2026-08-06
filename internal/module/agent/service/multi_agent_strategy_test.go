package service

import (
	"context"
	"errors"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

func TestBuiltInMultiAgentPlannerUsesGovernedRoleScopesAndFallback(t *testing.T) {
	planner, err := NewBuiltInAgentExecutionStrategyPlanner(agentStrategy.Policy{
		Enabled: true, ExecutorAvailable: false, MinimumComplexityScore: 6,
		MaxRoles: 3, MaxParallelRoles: 1, MaxEstimatedLatency: 50 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewBuiltInAgentExecutionStrategyPlanner() error = %v", err)
	}
	plan, err := planner.Plan(context.Background(), agentStrategy.Request{
		Query:            "请深入研究并比较三个来源，写三条候选，再审查事实和表达质量",
		ExecutionProfile: ExecutionProfileRuntimeResearchDraft,
		CapabilityIDs:    []string{CapabilityPlatformSearch, CapabilityContentDraft},
		AllowedTools:     []string{"hybrid_search_tweets"},
		Budget: agentRuntime.Budget{
			MaxSteps: 6, MaxTotalTokens: 36_000, MaxEstimatedCostMicros: 120_000,
			Timeout: 60 * time.Second,
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.TemplateID != "platform.research_draft.v1" ||
		plan.CandidateStrategy != agentStrategy.KindMultiAgent ||
		plan.SelectedStrategy != agentStrategy.KindSingleAgent ||
		plan.ReasonCode != agentStrategy.ReasonMultiExecutorUnavailable {
		t.Fatalf("Plan() = %+v", plan)
	}
	if len(plan.Roles) != 3 || len(plan.Roles[0].AllowedTools) != 1 ||
		plan.Roles[0].AllowedTools[0] != "hybrid_search_tweets" ||
		len(plan.Roles[1].AllowedTools) != 0 || len(plan.Roles[2].AllowedTools) != 0 {
		t.Fatalf("role scopes = %+v", plan.Roles)
	}
}

func TestServiceRejectsMultiSelectionUntilAggregateExecutorExists(t *testing.T) {
	planner, err := NewBuiltInAgentExecutionStrategyPlanner(agentStrategy.Policy{
		Enabled: true, ExecutorAvailable: true, MinimumComplexityScore: 6,
		MaxRoles: 3, MaxParallelRoles: 1, MaxEstimatedLatency: 50 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewBuiltInAgentExecutionStrategyPlanner() error = %v", err)
	}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentExecutionStrategyPlanner(planner),
	)
	defer service.Close()

	_, err = service.planUnifiedAgentExecutionStrategy(
		context.Background(),
		UnifiedAgentRequest{
			UserID:  42,
			Content: "请深入研究并比较三个来源，写三条候选，再审查事实和表达质量",
		},
		AgentCapabilityPlan{
			ExecutionProfile: ExecutionProfileRuntimeResearchDraft,
			CapabilityIDs:    []string{CapabilityPlatformSearch, CapabilityContentDraft},
		},
	)
	if !errors.Is(err, ErrMultiAgentExecutionUnavailable) {
		t.Fatalf("planUnifiedAgentExecutionStrategy() error = %v, want ErrMultiAgentExecutionUnavailable", err)
	}
}

func TestServiceAllowsSupportedMultiSelectionWhenExecutorEnabled(t *testing.T) {
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentExecutionStrategyPlanner(newExecutableMultiAgentPlanner(t)),
		WithMultiAgentExecution(true),
	)
	defer service.Close()

	plan, err := service.planUnifiedAgentExecutionStrategy(
		context.Background(),
		multiAgentResearchRequest(),
		AgentCapabilityPlan{
			ExecutionProfile: ExecutionProfileRuntimeResearchDraft,
			CapabilityIDs:    []string{CapabilityPlatformSearch, CapabilityContentDraft},
		},
	)
	if err != nil {
		t.Fatalf("planUnifiedAgentExecutionStrategy() error = %v", err)
	}
	if plan.SelectedStrategy != agentStrategy.KindMultiAgent || plan.ReasonCode != agentStrategy.ReasonMultiAdmitted {
		t.Fatalf("strategy plan = %+v", plan)
	}
}
