package service

import (
	"context"
	"testing"
	"time"

	"github.com/sashabaranov/go-openai"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
)

type workflowBudgetTokenCounter struct {
	count int
}

func (c workflowBudgetTokenCounter) CountText(string) int { return c.count }

func (c workflowBudgetTokenCounter) CountMessages([]agentRuntime.Message) int { return c.count }

func (c workflowBudgetTokenCounter) EstimateRequest(agentRuntime.ModelRequest) agentRuntime.TokenUsage {
	return agentRuntime.TokenUsage{InputTokens: c.count, TotalTokens: c.count, Estimated: true}
}

func (c workflowBudgetTokenCounter) EstimateResponse(agentRuntime.ModelResponse) agentRuntime.TokenUsage {
	return agentRuntime.TokenUsage{OutputTokens: c.count, TotalTokens: c.count, Estimated: true}
}

type workflowBudgetCostEstimator struct {
	microsPerToken int64
}

func (e workflowBudgetCostEstimator) EstimateCost(_ string, usage agentRuntime.TokenUsage) (agentRuntime.CostEstimate, error) {
	return agentRuntime.CostEstimate{
		Micros:         int64(usage.TotalTokens) * e.microsPerToken,
		PricingVersion: "workflow-budget-test-v1",
	}, nil
}

func TestWorkflowBudgetTrackerMergesOverridesAndRestoresUsage(t *testing.T) {
	service := &AgentService{workflowBudgetDefaults: dsl.BudgetDSL{
		MaxNodeExecutions:      50,
		MaxParallelNodes:       8,
		TimeoutSec:             300,
		MaxTotalTokens:         120_000,
		MaxEstimatedCostMicros: 1_000,
	}}
	definition := &dsl.WorkflowDSL{Budget: &dsl.BudgetDSL{
		MaxNodeExecutions: 75,
		MaxParallelNodes:  12,
		TimeoutSec:        600,
		MaxTotalTokens:    240_000,
	}}
	snapshot := agentRuntime.BudgetSnapshot{
		NodeExecutions: 3,
		Usage:          agentRuntime.TokenUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30},
	}

	tracker, maxParallel, err := service.workflowBudgetTracker(definition, snapshot)
	if err != nil {
		t.Fatalf("workflowBudgetTracker() error = %v", err)
	}
	budget := tracker.Budget()
	if budget.MaxSteps != 75 || maxParallel != 12 || budget.Timeout != 10*time.Minute {
		t.Fatalf("workflow budget = %+v, max parallel = %d", budget, maxParallel)
	}
	if budget.MaxTotalTokens != 240_000 || budget.MaxEstimatedCostMicros != 0 {
		t.Fatalf("workflow token/cost budget = %+v", budget)
	}
	restored := tracker.Snapshot()
	if restored.NodeExecutions != 3 || restored.Usage.TotalTokens != 30 {
		t.Fatalf("restored budget snapshot = %+v", restored)
	}
}

func TestWorkflowAccountingSnapshotPersistsVersionedBudgetAndUsage(t *testing.T) {
	run := &repository.WorkflowRunRecord{}
	applyWorkflowBudgetLimits(run, agentRuntime.Budget{
		MaxSteps: 12, MaxTotalTokens: 900, MaxEstimatedCostMicros: 4_000,
	})
	applyWorkflowAccountingSnapshot(run, agentRuntime.BudgetSnapshot{
		NodeExecutions: 4,
		Usage: agentRuntime.TokenUsage{
			InputTokens: 30, OutputTokens: 10, TotalTokens: 40,
			Estimated: true, EstimatedCostMicros: 75, CostEstimated: true,
			PricingVersion: "pricing-v1",
		},
	})
	if run.AccountingVersion != repository.ExecutionAccountingVersion ||
		run.MaxSteps != 12 || run.MaxTotalTokens != 900 || run.MaxEstimatedCostMicros != 4_000 {
		t.Fatalf("persisted budget = %+v", run)
	}
	if run.NodeExecutions != 4 || run.TotalTokens != 40 || !run.UsageEstimated ||
		run.EstimatedCostMicros != 75 || !run.CostEstimated || run.PricingVersion != "pricing-v1" {
		t.Fatalf("persisted usage = %+v", run)
	}
}

func TestLegacyStrategyRejectsSharedBudgetBeforeModelCall(t *testing.T) {
	service := &AgentService{runtimeTokens: workflowBudgetTokenCounter{count: 10}}
	tracker, err := agentRuntime.NewBudgetTracker(agentRuntime.Budget{MaxTotalTokens: 29})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}
	ctx := agentRuntime.ContextWithBudgetTracker(context.Background(), tracker)

	_, _, err = service.reserveLegacyStrategyBudget(ctx, "test-model", []openai.ChatCompletionMessage{{
		Role: openai.ChatMessageRoleUser, Content: "test",
	}}, nil, 20)
	if !agentRuntime.HasErrorCode(err, agentRuntime.ErrorBudgetExceeded) {
		t.Fatalf("reserveLegacyStrategyBudget() error = %v, want budget exceeded", err)
	}
	if snapshot := tracker.Snapshot(); snapshot.Usage.TotalTokens != 0 || snapshot.Reserved.TotalTokens != 0 {
		t.Fatalf("budget snapshot = %+v", snapshot)
	}
}

func TestLegacyStrategyCommitsProviderUsageToSharedBudget(t *testing.T) {
	service := &AgentService{
		runtimeTokens:        workflowBudgetTokenCounter{count: 10},
		runtimeCostEstimator: workflowBudgetCostEstimator{microsPerToken: 2},
	}
	tracker, err := agentRuntime.NewBudgetTracker(agentRuntime.Budget{
		MaxTotalTokens: 100, MaxEstimatedCostMicros: 100,
	})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}
	ctx := agentRuntime.ContextWithBudgetTracker(context.Background(), tracker)
	reservation, requestEstimate, err := service.reserveLegacyStrategyBudget(
		ctx,
		"test-model",
		[]openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "test"}},
		nil,
		20,
	)
	if err != nil {
		t.Fatalf("reserveLegacyStrategyBudget() error = %v", err)
	}
	usage, err := service.resolveLegacyStrategyUsage(ctx, "test-model", requestEstimate, openai.ChatCompletionResponse{
		Model: "test-model",
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "answer"},
		}},
		Usage: openai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	})
	if err != nil {
		t.Fatalf("resolveLegacyStrategyUsage() error = %v", err)
	}
	if err := reservation.Commit(usage); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Usage.TotalTokens != 15 || snapshot.Usage.EstimatedCostMicros != 30 {
		t.Fatalf("budget usage = %+v", snapshot.Usage)
	}
	if snapshot.Reserved.TotalTokens != 0 || snapshot.Reserved.EstimatedCostMicros != 0 {
		t.Fatalf("budget reservation = %+v", snapshot.Reserved)
	}
}
