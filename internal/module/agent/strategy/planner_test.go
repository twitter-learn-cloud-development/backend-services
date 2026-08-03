package strategy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestDeterministicPlannerAdmitsBoundedComplexTask(t *testing.T) {
	planner := newTestPlanner(t, Policy{
		Enabled: true, ExecutorAvailable: true, MinimumComplexityScore: 6,
		MaxRoles: 3, MaxParallelRoles: 1, MaxEstimatedLatency: 50 * time.Second,
	})
	request := complexTestRequest()

	plan, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.CandidateStrategy != KindMultiAgent || plan.SelectedStrategy != KindMultiAgent ||
		plan.Decision != DecisionSelected || plan.ReasonCode != ReasonMultiAdmitted {
		t.Fatalf("Plan() decision = %+v", plan)
	}
	if plan.TemplateID != "research.v1" || len(plan.Roles) != 3 || plan.MaxParallelRoles != 1 {
		t.Fatalf("Plan() roles = %+v", plan)
	}
	if len(plan.Roles[0].AllowedTools) != 1 || plan.Roles[0].AllowedTools[0] != "search" ||
		len(plan.Roles[1].AllowedTools) != 0 || len(plan.Roles[2].AllowedTools) != 0 {
		t.Fatalf("Plan() tool scopes = %+v", plan.Roles)
	}
	if plan.EstimatedTotalTokens != 24_000 || plan.EstimatedCostMicros != 100_000 ||
		plan.EstimatedLatencyMillis != 50_000 {
		t.Fatalf("Plan() estimates = %+v", plan)
	}
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}

	again, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatalf("Plan(second) error = %v", err)
	}
	if again.PlanDigest != plan.PlanDigest {
		t.Fatalf("deterministic digest drifted: %q != %q", again.PlanDigest, plan.PlanDigest)
	}
}

func TestDeterministicPlannerFallsBackAtEveryAdmissionBoundary(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Request, *Policy)
		want   ReasonCode
	}{
		{
			name: "tool scope", want: ReasonMultiToolScopeUnavailable,
			mutate: func(request *Request, _ *Policy) { request.AllowedTools = nil },
		},
		{
			name: "unbounded budget", want: ReasonMultiBudgetUnbounded,
			mutate: func(request *Request, _ *Policy) { request.Budget.MaxTotalTokens = 0 },
		},
		{
			name: "step budget", want: ReasonMultiStepBudget,
			mutate: func(request *Request, _ *Policy) { request.Budget.MaxSteps = 4 },
		},
		{
			name: "token budget", want: ReasonMultiTokenBudget,
			mutate: func(request *Request, _ *Policy) { request.Budget.MaxTotalTokens = 23_999 },
		},
		{
			name: "cost budget", want: ReasonMultiCostBudget,
			mutate: func(request *Request, _ *Policy) { request.Budget.MaxEstimatedCostMicros = 99_999 },
		},
		{
			name: "latency budget", want: ReasonMultiLatencyBudget,
			mutate: func(request *Request, _ *Policy) { request.Budget.Timeout = 49 * time.Second },
		},
		{
			name: "policy latency", want: ReasonMultiPolicyLatency,
			mutate: func(_ *Request, policy *Policy) { policy.MaxEstimatedLatency = 49 * time.Second },
		},
		{
			name: "executor unavailable", want: ReasonMultiExecutorUnavailable,
			mutate: func(_ *Request, policy *Policy) { policy.ExecutorAvailable = false },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := Policy{
				Enabled: true, ExecutorAvailable: true, MinimumComplexityScore: 6,
				MaxRoles: 3, MaxParallelRoles: 1, MaxEstimatedLatency: 50 * time.Second,
			}
			request := complexTestRequest()
			test.mutate(&request, &policy)
			planner := newTestPlanner(t, policy)
			plan, err := planner.Plan(context.Background(), request)
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}
			if plan.CandidateStrategy != KindMultiAgent || plan.SelectedStrategy != KindSingleAgent ||
				plan.Decision != DecisionFallback || plan.ReasonCode != test.want {
				t.Fatalf("Plan() = %+v, want reason %q", plan, test.want)
			}
		})
	}
}

func TestDeterministicPlannerKeepsDisabledAndSimpleRunsSingle(t *testing.T) {
	disabled := newTestPlanner(t, Policy{
		Enabled: false, ExecutorAvailable: false, MaxRoles: 3, MaxParallelRoles: 1,
	})
	plan, err := disabled.Plan(context.Background(), complexTestRequest())
	if err != nil {
		t.Fatalf("Plan(disabled) error = %v", err)
	}
	if plan.Decision != DecisionDisabled || plan.ReasonCode != ReasonMultiFeatureDisabled ||
		plan.CandidateStrategy != KindMultiAgent || plan.SelectedStrategy != KindSingleAgent {
		t.Fatalf("Plan(disabled) = %+v", plan)
	}

	enabled := newTestPlanner(t, Policy{
		Enabled: true, ExecutorAvailable: true, MaxRoles: 3, MaxParallelRoles: 1,
	})
	request := complexTestRequest()
	request.Query = "搜索资料并写一条推文"
	plan, err = enabled.Plan(context.Background(), request)
	if err != nil {
		t.Fatalf("Plan(simple) error = %v", err)
	}
	if plan.CandidateStrategy != KindSingleAgent || plan.SelectedStrategy != KindSingleAgent ||
		plan.ReasonCode != ReasonSingleComplexityBelowLimit || len(plan.Roles) != 0 {
		t.Fatalf("Plan(simple) = %+v", plan)
	}
}

func TestValidatePlanRejectsDigestMutation(t *testing.T) {
	planner := newTestPlanner(t, Policy{
		Enabled: true, ExecutorAvailable: true, MaxRoles: 3, MaxParallelRoles: 1,
	})
	plan, err := planner.Plan(context.Background(), complexTestRequest())
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	plan.Roles[0].AllowedTools = append(plan.Roles[0].AllowedTools, "unexpected")
	if err := ValidatePlan(plan); err == nil {
		t.Fatal("ValidatePlan() error = nil after plan mutation")
	}
}

func TestBindProfileSetPinsVersionIntoPlanDigest(t *testing.T) {
	planner := newTestPlanner(t, Policy{
		Enabled: true, ExecutorAvailable: true, MaxRoles: 3, MaxParallelRoles: 1,
	})
	plan, err := planner.Plan(context.Background(), complexTestRequest())
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	originalDigest := plan.PlanDigest
	pinned, err := BindProfileSet(plan, "unified.research_draft", "v2")
	if err != nil {
		t.Fatalf("BindProfileSet() error = %v", err)
	}
	if pinned.ProfileSetAnchor != "unified.research_draft" || pinned.ProfileSetVersion != "v2" ||
		pinned.PlanDigest == originalDigest {
		t.Fatalf("pinned plan = %+v", pinned)
	}
	if err := ValidatePlan(pinned); err != nil {
		t.Fatalf("ValidatePlan(pinned) error = %v", err)
	}
	pinned.ProfileSetVersion = "v1"
	if err := ValidatePlan(pinned); err == nil {
		t.Fatal("ValidatePlan(mutated profile set) error = nil")
	}
	if _, err := BindProfileSet(plan, "unified.research_draft", ""); err == nil {
		t.Fatal("BindProfileSet(incomplete) error = nil")
	}
}

func TestDeterministicPlannerHonorsContextCancellation(t *testing.T) {
	planner := newTestPlanner(t, Policy{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := planner.Plan(ctx, complexTestRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Plan() error = %v, want context.Canceled", err)
	}
}

func TestDeterministicPlannerConcurrentReadsStayDeterministic(t *testing.T) {
	planner := newTestPlanner(t, Policy{
		Enabled: true, ExecutorAvailable: true, MaxRoles: 3, MaxParallelRoles: 1,
	})
	request := complexTestRequest()
	expected, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	const workers = 32
	var group sync.WaitGroup
	errorsFound := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			plan, planErr := planner.Plan(context.Background(), request)
			if planErr != nil {
				errorsFound <- planErr
				return
			}
			if plan.PlanDigest != expected.PlanDigest {
				errorsFound <- fmt.Errorf("plan digest = %q, want %q", plan.PlanDigest, expected.PlanDigest)
			}
		}()
	}
	group.Wait()
	close(errorsFound)
	for found := range errorsFound {
		t.Error(found)
	}
}

func newTestPlanner(t *testing.T, policy Policy) *DeterministicPlanner {
	t.Helper()
	planner, err := NewDeterministicPlanner(policy, []Template{{
		ID: "research.v1", ExecutionProfile: "runtime.research",
		RequiredCapabilityIDs: []string{"search", "draft"}, MaxParallelRoles: 1,
		Roles: []RoleTemplate{
			{
				RoleID: "researcher", CapabilityIDs: []string{"search"}, AllowedTools: []string{"search"},
				MaxSteps: 3, RequiredTotalTokens: 10_000, RequiredCostMicros: 45_000,
				EstimatedLatency: 25 * time.Second,
			},
			{
				RoleID: "drafter", CapabilityIDs: []string{"draft"}, MaxSteps: 1,
				RequiredTotalTokens: 9_000, RequiredCostMicros: 35_000, EstimatedLatency: 17 * time.Second,
			},
			{
				RoleID: "reviewer", CapabilityIDs: []string{"draft"}, MaxSteps: 1,
				RequiredTotalTokens: 5_000, RequiredCostMicros: 20_000, EstimatedLatency: 8 * time.Second,
			},
		},
	}})
	if err != nil {
		t.Fatalf("NewDeterministicPlanner() error = %v", err)
	}
	return planner
}

func complexTestRequest() Request {
	return Request{
		Query:            "请深入研究并比较三个来源，写三条候选内容，再审查事实和表达质量",
		ExecutionProfile: "runtime.research", CapabilityIDs: []string{"draft", "search"},
		AllowedTools: []string{"search"},
		Budget: agentRuntime.Budget{
			MaxSteps: 5, MaxTotalTokens: 24_000, MaxEstimatedCostMicros: 100_000,
			Timeout: 50 * time.Second,
		},
	}
}
