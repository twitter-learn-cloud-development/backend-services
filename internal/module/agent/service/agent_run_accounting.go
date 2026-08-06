package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	defaultAgentRunAccountingChildLimit = 50
	maxAgentRunAccountingChildLimit     = 200

	AgentRunAccountingScopeDirectChildren = "direct_children.v1"
	AgentRunAccountingStateUnavailable    = "unavailable"
	AgentRunAccountingStatePartial        = "partial"
	AgentRunAccountingStateComplete       = "complete"
)

var ErrAgentRunAccountingStoreUnavailable = errors.New("agent run accounting store is unavailable")

type ExecutionBudgetView struct {
	MaxSteps               int
	MaxTotalTokens         int
	MaxEstimatedCostMicros int64
	ConsumedSteps          int
	ConsumedTokens         int
	ConsumedCostMicros     int64
}

type WorkflowRunAccountingView struct {
	RunID             string
	WorkflowID        string
	ParentActionID    string
	Status            string
	State             string
	AccountingVersion string
	Usage             agentRuntime.TokenUsage
	Budget            ExecutionBudgetView
	StartedAt         time.Time
	SuspendedAt       time.Time
	FinishedAt        time.Time
}

type AgentRunAccountingView struct {
	RunID                 string
	RunStatus             string
	Scope                 string
	State                 string
	Complete              bool
	Truncated             bool
	ChildRunCount         int64
	IncludedChildRunCount int
	AccountingVersion     string
	ParentUsage           agentRuntime.TokenUsage
	ParentBudget          ExecutionBudgetView
	ChildUsage            agentRuntime.TokenUsage
	TotalUsage            agentRuntime.TokenUsage
	Children              []WorkflowRunAccountingView
}

func (s *AgentService) GetAgentRunAccounting(
	ctx context.Context,
	userID uint64,
	runID string,
	limit int,
) (*AgentRunAccountingView, error) {
	parent, err := s.GetAgentExecutionRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if s.agentRunAccountingStore == nil {
		return nil, ErrAgentRunAccountingStoreUnavailable
	}
	limit = normalizeAgentRunAccountingLimit(limit)
	children, total, err := s.agentRunAccountingStore.ListDirectChildWorkflowRuns(
		ctx,
		userID,
		strings.TrimSpace(runID),
		limit,
	)
	if err != nil {
		return nil, err
	}
	if total < int64(len(children)) {
		total = int64(len(children))
	}
	return buildAgentRunAccountingView(parent, children, total), nil
}

func buildAgentRunAccountingView(
	parent *repository.AgentExecutionRun,
	children []*repository.WorkflowRunRecord,
	total int64,
) *AgentRunAccountingView {
	parentUsage := agentRuntime.TokenUsage{
		InputTokens: parent.InputTokens, OutputTokens: parent.OutputTokens,
		TotalTokens: parent.TotalTokens, Estimated: parent.UsageEstimated,
		EstimatedCostMicros: parent.EstimatedCostMicros, CostEstimated: parent.CostEstimated,
		PricingVersion: parent.PricingVersion,
	}
	view := &AgentRunAccountingView{
		RunID: parent.ID, RunStatus: string(parent.Status),
		Scope:         AgentRunAccountingScopeDirectChildren,
		ChildRunCount: total, IncludedChildRunCount: len(children),
		AccountingVersion: parent.AccountingVersion,
		ParentUsage:       parentUsage,
		ParentBudget: ExecutionBudgetView{
			MaxSteps: parent.MaxSteps, MaxTotalTokens: parent.MaxTotalTokens,
			MaxEstimatedCostMicros: parent.MaxEstimatedCostMicros,
			ConsumedSteps:          parent.StepCount, ConsumedTokens: parent.TotalTokens,
			ConsumedCostMicros: parent.EstimatedCostMicros,
		},
		Children: make([]WorkflowRunAccountingView, 0, len(children)),
	}
	view.Truncated = total > int64(len(children))
	complete := parent.AccountingVersion == repository.ExecutionAccountingVersion &&
		isTerminalAgentExecutionRunStatus(parent.Status) && !view.Truncated
	available := parent.AccountingVersion == repository.ExecutionAccountingVersion

	for _, child := range children {
		if child == nil {
			complete = false
			continue
		}
		usage := agentRuntime.TokenUsage{
			InputTokens: child.InputTokens, OutputTokens: child.OutputTokens,
			TotalTokens: child.TotalTokens, Estimated: child.UsageEstimated,
			EstimatedCostMicros: child.EstimatedCostMicros, CostEstimated: child.CostEstimated,
			PricingVersion: child.PricingVersion,
		}
		state := AgentRunAccountingStateUnavailable
		childVersioned := child.AccountingVersion == repository.ExecutionAccountingVersion
		if childVersioned {
			available = true
			state = AgentRunAccountingStatePartial
			view.ChildUsage.Add(usage)
			if isTerminalWorkflowRunStatus(child.Status) {
				state = AgentRunAccountingStateComplete
			}
		}
		if state != AgentRunAccountingStateComplete {
			complete = false
		}
		view.Children = append(view.Children, WorkflowRunAccountingView{
			RunID: child.ID.Hex(), WorkflowID: child.WorkflowID.Hex(),
			ParentActionID: child.ParentActionID, Status: child.Status, State: state,
			AccountingVersion: child.AccountingVersion, Usage: usage,
			Budget: ExecutionBudgetView{
				MaxSteps: child.MaxSteps, MaxTotalTokens: child.MaxTotalTokens,
				MaxEstimatedCostMicros: child.MaxEstimatedCostMicros,
				ConsumedSteps:          child.NodeExecutions, ConsumedTokens: child.TotalTokens,
				ConsumedCostMicros: child.EstimatedCostMicros,
			},
			StartedAt: child.StartedAt, SuspendedAt: child.SuspendedAt, FinishedAt: child.FinishedAt,
		})
	}
	view.TotalUsage = parentUsage
	view.TotalUsage.Add(view.ChildUsage)
	view.Complete = complete
	switch {
	case complete:
		view.State = AgentRunAccountingStateComplete
	case available:
		view.State = AgentRunAccountingStatePartial
	default:
		view.State = AgentRunAccountingStateUnavailable
	}
	return view
}

func normalizeAgentRunAccountingLimit(limit int) int {
	if limit <= 0 {
		return defaultAgentRunAccountingChildLimit
	}
	if limit > maxAgentRunAccountingChildLimit {
		return maxAgentRunAccountingChildLimit
	}
	return limit
}

func isTerminalAgentExecutionRunStatus(status repository.AgentExecutionRunStatus) bool {
	switch status {
	case repository.AgentExecutionRunCompleted, repository.AgentExecutionRunFailed,
		repository.AgentExecutionRunCanceled:
		return true
	default:
		return false
	}
}

func isTerminalWorkflowRunStatus(status string) bool {
	switch status {
	case WorkflowRunStatusSuccess, WorkflowRunStatusFailed, WorkflowRunStatusRejected,
		WorkflowRunStatusCompensated, WorkflowRunStatusCompensationFailed, WorkflowRunStatusCanceled:
		return true
	default:
		return false
	}
}
