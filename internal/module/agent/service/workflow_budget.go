package service

import (
	"fmt"
	"time"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
)

const (
	workflowNodeExecutionHardCap = 1000
	workflowParallelHardCap      = 64
	workflowTimeoutHardCapSec    = 3600
	workflowTokenHardCap         = 10_000_000
	workflowCostHardCapMicros    = int64(1_000_000_000_000)
)

func (s *AgentService) workflowBudgetTracker(
	definition *dsl.WorkflowDSL,
	snapshot agentRuntime.BudgetSnapshot,
) (*agentRuntime.BudgetTracker, int, error) {
	configured := dsl.BudgetDSL{}
	if s != nil {
		configured = s.workflowBudgetDefaults
	}
	if definition != nil && definition.Budget != nil {
		configured = mergeWorkflowBudget(configured, *definition.Budget)
	}
	configured = clampWorkflowBudget(configured)
	budget := agentRuntime.Budget{
		MaxSteps:               configured.MaxNodeExecutions,
		MaxTotalTokens:         configured.MaxTotalTokens,
		MaxEstimatedCostMicros: configured.MaxEstimatedCostMicros,
		Timeout:                time.Duration(configured.TimeoutSec) * time.Second,
	}
	tracker, err := agentRuntime.NewBudgetTrackerFromSnapshot(budget, snapshot)
	if err != nil {
		return nil, 0, fmt.Errorf("initialize workflow budget: %w", err)
	}
	return tracker, configured.MaxParallelNodes, nil
}

func mergeWorkflowBudget(defaults, override dsl.BudgetDSL) dsl.BudgetDSL {
	if override.MaxNodeExecutions > 0 {
		defaults.MaxNodeExecutions = override.MaxNodeExecutions
	}
	if override.MaxParallelNodes > 0 {
		defaults.MaxParallelNodes = override.MaxParallelNodes
	}
	if override.TimeoutSec > 0 {
		defaults.TimeoutSec = override.TimeoutSec
	}
	if override.MaxTotalTokens > 0 {
		defaults.MaxTotalTokens = override.MaxTotalTokens
	}
	// A budget object is explicit configuration. Cost zero is meaningful and
	// disables monetary enforcement, matching the editor and public contract.
	defaults.MaxEstimatedCostMicros = override.MaxEstimatedCostMicros
	return defaults
}

func clampWorkflowBudget(budget dsl.BudgetDSL) dsl.BudgetDSL {
	budget.MaxNodeExecutions = positiveBound(budget.MaxNodeExecutions, 50, workflowNodeExecutionHardCap)
	budget.MaxParallelNodes = positiveBound(budget.MaxParallelNodes, 8, workflowParallelHardCap)
	budget.TimeoutSec = positiveBound(budget.TimeoutSec, 300, workflowTimeoutHardCapSec)
	budget.MaxTotalTokens = positiveBound(budget.MaxTotalTokens, 120_000, workflowTokenHardCap)
	if budget.MaxEstimatedCostMicros < 0 {
		budget.MaxEstimatedCostMicros = 0
	}
	if budget.MaxEstimatedCostMicros > workflowCostHardCapMicros {
		budget.MaxEstimatedCostMicros = workflowCostHardCapMicros
	}
	return budget
}

func positiveBound(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func applyWorkflowBudgetLimits(run *repository.WorkflowRunRecord, budget agentRuntime.Budget) {
	if run == nil {
		return
	}
	run.MaxSteps = budget.MaxSteps
	run.MaxTotalTokens = budget.MaxTotalTokens
	run.MaxEstimatedCostMicros = budget.MaxEstimatedCostMicros
	run.AccountingVersion = repository.ExecutionAccountingVersion
}

func applyWorkflowAccountingSnapshot(run *repository.WorkflowRunRecord, snapshot agentRuntime.BudgetSnapshot) {
	if run == nil {
		return
	}
	run.NodeExecutions = snapshot.NodeExecutions
	run.InputTokens = snapshot.Usage.InputTokens
	run.OutputTokens = snapshot.Usage.OutputTokens
	run.TotalTokens = snapshot.Usage.TotalTokens
	run.UsageEstimated = snapshot.Usage.Estimated
	run.EstimatedCostMicros = snapshot.Usage.EstimatedCostMicros
	run.CostEstimated = snapshot.Usage.CostEstimated
	run.PricingVersion = snapshot.Usage.PricingVersion
	run.AccountingVersion = repository.ExecutionAccountingVersion
}
