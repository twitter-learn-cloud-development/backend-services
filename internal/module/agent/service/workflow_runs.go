package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"

	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/repository"
)

var workflowRunListStatuses = map[string]struct{}{
	WorkflowRunStatusRunning:            {},
	WorkflowRunStatusSuspended:          {},
	WorkflowRunStatusSuccess:            {},
	WorkflowRunStatusFailed:             {},
	WorkflowRunStatusRejected:           {},
	WorkflowRunStatusCompensating:       {},
	WorkflowRunStatusCompensated:        {},
	WorkflowRunStatusCompensationFailed: {},
	WorkflowRunStatusCanceling:          {},
	WorkflowRunStatusCanceled:           {},
}

func (s *AgentService) GetWorkflowRunTrace(
	ctx context.Context,
	userID uint64,
	runID string,
) (*agentObservability.TraceBundle, error) {
	if s.traceReader == nil {
		return nil, errors.New("execution trace reader is not available")
	}
	if _, err := s.GetWorkflowRun(ctx, userID, runID); err != nil {
		return nil, err
	}
	return s.traceReader.GetTraceBundle(ctx, userID, runID)
}

func (s *AgentService) ListWorkflowRuns(
	ctx context.Context,
	userID uint64,
	workflowID string,
	status string,
	page int,
	pageSize int,
) ([]*repository.WorkflowRunRecord, int64, error) {
	if s.repo == nil {
		return nil, 0, errors.New("agent repository is not initialized")
	}
	queryRepo, ok := s.repo.(repository.WorkflowRunQueryRepository)
	if !ok {
		return nil, 0, errors.New("workflow run query repository is not available")
	}
	var workflowOID primitive.ObjectID
	var err error
	if strings.TrimSpace(workflowID) != "" {
		workflowOID, err = primitive.ObjectIDFromHex(workflowID)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid workflow_id: %w", err)
		}
	}
	status = strings.TrimSpace(status)
	if status != "" {
		if _, allowed := workflowRunListStatuses[status]; !allowed {
			return nil, 0, fmt.Errorf("invalid workflow run status %q", status)
		}
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return queryRepo.ListWorkflowRuns(ctx, userID, workflowOID, status, page, pageSize)
}
