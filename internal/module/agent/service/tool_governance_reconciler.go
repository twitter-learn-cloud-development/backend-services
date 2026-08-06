package service

import (
	"context"
	"log/slog"
	"time"

	"twitter-clone/internal/module/agent/repository"
)

type ToolGovernanceReconcileMetrics interface {
	RecordReconciliation(result string, actions map[string]int64)
}

type ToolGovernanceReconciler struct {
	repo     repository.ToolGovernanceReconcileRepository
	interval time.Duration
	timeout  time.Duration
	metrics  ToolGovernanceReconcileMetrics
}

func NewToolGovernanceReconciler(
	repo repository.ToolGovernanceReconcileRepository,
	interval time.Duration,
	metrics ToolGovernanceReconcileMetrics,
) *ToolGovernanceReconciler {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	timeout := interval / 2
	if timeout < time.Second {
		timeout = time.Second
	}
	if timeout > 15*time.Second {
		timeout = 15 * time.Second
	}
	return &ToolGovernanceReconciler{repo: repo, interval: interval, timeout: timeout, metrics: metrics}
}

func (r *ToolGovernanceReconciler) Run(ctx context.Context) {
	if r == nil || r.repo == nil {
		return
	}
	r.reconcile(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcile(ctx)
		}
	}
}

func (r *ToolGovernanceReconciler) Reconcile(ctx context.Context) (repository.ToolGovernanceReconcileResult, error) {
	if r == nil || r.repo == nil {
		return repository.ToolGovernanceReconcileResult{}, nil
	}
	reconcileCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.repo.ReconcileToolGovernance(reconcileCtx, time.Now())
}

func (r *ToolGovernanceReconciler) reconcile(ctx context.Context) {
	result, err := r.Reconcile(ctx)
	if err != nil {
		if r.metrics != nil {
			r.metrics.RecordReconciliation("failed", nil)
		}
		slog.ErrorContext(ctx, "agent tool governance reconciliation failed", "error", err)
		return
	}
	actions := map[string]int64{
		"expired_approval":        result.ExpiredApprovals,
		"released_approval_lease": result.ReleasedApprovalLeases,
		"failed_execution_lease":  result.FailedExecutionLeases,
		"failed_suspended_run":    result.FailedSuspendedRuns,
		"failed_agent_run":        result.FailedAgentRuns,
	}
	if r.metrics != nil {
		r.metrics.RecordReconciliation("succeeded", actions)
	}
	if result.ExpiredApprovals+result.ReleasedApprovalLeases+result.FailedExecutionLeases+result.FailedSuspendedRuns+result.FailedAgentRuns > 0 {
		slog.InfoContext(ctx, "agent tool governance reconciled", "actions", actions)
	}
}
