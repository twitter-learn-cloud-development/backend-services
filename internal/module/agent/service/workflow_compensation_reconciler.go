package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
)

type WorkflowCompensationReconcileResult struct {
	Scanned             int64
	Recovered           int64
	DeferredManualRetry int64
	Contended           int64
	SkippedRun          int64
	Failed              int64
}

type WorkflowCompensationReconciler struct {
	service     *AgentService
	repo        repository.WorkflowCompensationRecoveryRepository
	interval    time.Duration
	scanTimeout time.Duration
	batchSize   int
	metrics     ToolGovernanceReconcileMetrics
}

func NewWorkflowCompensationReconciler(
	service *AgentService,
	repo repository.WorkflowCompensationRecoveryRepository,
	interval time.Duration,
	batchSize int,
	metrics ToolGovernanceReconcileMetrics,
) *WorkflowCompensationReconciler {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if batchSize < 1 {
		batchSize = 50
	}
	if batchSize > 500 {
		batchSize = 500
	}
	scanTimeout := interval / 2
	if scanTimeout < time.Second {
		scanTimeout = time.Second
	}
	if scanTimeout > 15*time.Second {
		scanTimeout = 15 * time.Second
	}
	return &WorkflowCompensationReconciler{
		service: service, repo: repo, interval: interval, scanTimeout: scanTimeout,
		batchSize: batchSize, metrics: metrics,
	}
}

func (r *WorkflowCompensationReconciler) Run(ctx context.Context) {
	if r == nil || r.service == nil || r.service.repo == nil || r.repo == nil {
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

func (r *WorkflowCompensationReconciler) Reconcile(ctx context.Context) (WorkflowCompensationReconcileResult, error) {
	var result WorkflowCompensationReconcileResult
	if r == nil || r.service == nil || r.service.repo == nil || r.repo == nil {
		return result, nil
	}
	scanCtx, cancel := context.WithTimeout(ctx, r.scanTimeout)
	candidates, err := r.repo.ListExpiredWorkflowCompensationCandidates(scanCtx, time.Now(), r.batchSize)
	cancel()
	if err != nil {
		return result, err
	}
	result.Scanned = int64(len(candidates))
	var reconcileErr error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(reconcileErr, err)
		}
		run, err := r.service.repo.GetWorkflowRun(ctx, candidate.RunID, candidate.UserID)
		if err != nil {
			result.Failed++
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("load compensation run %s: %w", candidate.RunID.Hex(), err))
			continue
		}
		if run.Status != WorkflowRunStatusFailed && run.Status != WorkflowRunStatusCompensating {
			result.SkippedRun++
			continue
		}
		outcome, err := r.service.driveWorkflowCompensationsWithPolicy(
			ctx,
			&WorkflowExecutionResult{Run: run},
			primitive.NilObjectID,
			false,
			r.service.backgroundWorkflowCompensationPolicy,
		)
		if err != nil {
			if errors.Is(err, repository.ErrWorkflowCompensationUnavailable) || errors.Is(err, repository.ErrWorkflowCompensationClaimInvalid) {
				result.Contended++
				continue
			}
			result.Failed++
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("recover compensation run %s: %w", candidate.RunID.Hex(), err))
			continue
		}
		if outcome != nil && outcome.Run != nil && outcome.Run.Status == WorkflowRunStatusCompensationFailed {
			result.DeferredManualRetry++
		} else {
			result.Recovered++
		}
	}
	return result, reconcileErr
}

func (r *WorkflowCompensationReconciler) reconcile(ctx context.Context) {
	result, err := r.Reconcile(ctx)
	actions := map[string]int64{
		"expired_compensation_recovered":      result.Recovered,
		"expired_compensation_manual_retry":   result.DeferredManualRetry,
		"expired_compensation_contended":      result.Contended,
		"expired_compensation_skipped_run":    result.SkippedRun,
		"expired_compensation_recovery_error": result.Failed,
	}
	if err != nil {
		if r.metrics != nil {
			r.metrics.RecordReconciliation("failed", actions)
		}
		slog.ErrorContext(ctx, "workflow compensation reconciliation failed", "error", err, "actions", actions)
		return
	}
	if r.metrics != nil {
		r.metrics.RecordReconciliation("succeeded", actions)
	}
	if result.Scanned > 0 {
		slog.InfoContext(ctx, "workflow compensations reconciled", "scanned", result.Scanned, "actions", actions)
	}
}
