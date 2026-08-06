package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
)

var ErrWorkflowRunCanceled = errors.New("workflow run canceled by user")

const defaultWorkflowCancellationPollInterval = 500 * time.Millisecond

func envWorkflowDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (s *AgentService) CancelWorkflowRun(
	ctx context.Context,
	userID uint64,
	runID string,
	reason string,
) (*repository.WorkflowRunRecord, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	oid, err := primitive.ObjectIDFromHex(runID)
	if err != nil {
		return nil, fmt.Errorf("invalid run_id: %w", err)
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 500 {
		return nil, errors.New("cancel reason exceeds 500 characters")
	}
	controlRepo, ok := s.repo.(repository.WorkflowRunCancellationRepository)
	if !ok {
		return nil, errors.New("workflow run cancellation repository is not available")
	}
	return controlRepo.RequestWorkflowRunCancellation(ctx, oid, userID, reason)
}

func (s *AgentService) workflowExecutionContext(
	ctx context.Context,
	run *repository.WorkflowRunRecord,
) (context.Context, func()) {
	controlRepo, ok := s.repo.(repository.WorkflowRunCancellationRepository)
	if !ok || run == nil || run.ID.IsZero() {
		return ctx, func() {}
	}
	executionCtx, cancelExecution := context.WithCancelCause(ctx)
	watchCtx, stopWatcher := context.WithCancel(ctx)
	done := make(chan struct{})
	interval := s.workflowCancelPoll
	if interval <= 0 {
		interval = defaultWorkflowCancellationPollInterval
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			requested, err := controlRepo.IsWorkflowRunCancellationRequested(watchCtx, run.ID, run.UserID)
			if err == nil && requested {
				cancelExecution(ErrWorkflowRunCanceled)
				return
			}
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return executionCtx, func() {
		stopWatcher()
		cancelExecution(context.Canceled)
		<-done
	}
}

func (s *AgentService) commitWorkflowRunExecutionState(
	ctx context.Context,
	run *repository.WorkflowRunRecord,
) error {
	if controlRepo, ok := s.repo.(repository.WorkflowRunCancellationRepository); ok {
		committed, err := controlRepo.CommitWorkflowRunExecutionState(ctx, run)
		if err != nil {
			return err
		}
		if committed != nil {
			*run = *committed
		}
		return nil
	}
	return s.repo.UpdateWorkflowRun(ctx, run)
}
