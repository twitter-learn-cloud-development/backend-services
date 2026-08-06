package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	workflowCompensationCheckpointKind = "workflow_compensation"
	defaultWorkflowCompensationLease   = 5 * time.Minute
)

type workflowCompensationCheckpoint struct {
	Kind     string `json:"kind"`
	Sequence int    `json:"sequence"`
}

func (s *AgentService) attachWorkflowCompensationPlan(
	ctx context.Context,
	run *repository.WorkflowRunRecord,
	scheduler *engine.Scheduler,
	executionErr error,
) error {
	if executionErr == nil || scheduler == nil {
		return executionErr
	}
	var suspension *engine.SuspensionError
	if errors.As(executionErr, &suspension) {
		return executionErr
	}
	if err := s.persistWorkflowCompensationPlan(ctx, run, scheduler.CompensationPlan()); err != nil {
		return errors.Join(executionErr, fmt.Errorf("persist workflow compensation plan: %w", err))
	}
	return executionErr
}

func (s *AgentService) persistWorkflowCompensationPlan(
	ctx context.Context,
	run *repository.WorkflowRunRecord,
	tasks []engine.CompensationTask,
) error {
	if len(tasks) == 0 {
		return nil
	}
	if run == nil || run.ID.IsZero() || run.WorkflowID.IsZero() || run.WorkflowRevisionID.IsZero() || run.UserID == 0 {
		return errors.New("workflow run identity is incomplete")
	}
	compensationRepo, ok := s.repo.(repository.WorkflowCompensationRepository)
	if !ok {
		return errors.New("workflow compensation repository is not available")
	}
	now := time.Now()
	records := make([]*repository.WorkflowCompensationRecord, 0, len(tasks))
	for _, task := range tasks {
		inputJSON, err := json.Marshal(task.Inputs)
		if err != nil {
			return fmt.Errorf("marshal compensation input for node %s: %w", task.SourceNodeID, err)
		}
		retryJSON, err := json.Marshal(task.Retry)
		if err != nil {
			return fmt.Errorf("marshal compensation retry for node %s: %w", task.SourceNodeID, err)
		}
		planJSON, err := json.Marshal(task)
		if err != nil {
			return fmt.Errorf("marshal compensation plan for node %s: %w", task.SourceNodeID, err)
		}
		inputHash := sha256.Sum256(inputJSON)
		planHash := sha256.Sum256(planJSON)
		records = append(records, &repository.WorkflowCompensationRecord{
			RunID: run.ID, WorkflowID: run.WorkflowID, WorkflowRevisionID: run.WorkflowRevisionID,
			UserID: run.UserID, Sequence: task.Sequence, SourceNodeID: task.SourceNodeID,
			StepID: task.StepID, ToolName: task.ToolName, InputJSON: string(inputJSON),
			InputHash: hex.EncodeToString(inputHash[:]), PlanHash: hex.EncodeToString(planHash[:]),
			TimeoutSec: task.TimeoutSec, RetryJSON: string(retryJSON),
			IdempotencyKey: run.ID.Hex() + ":" + task.StepID + ":" + task.ToolName,
			Status:         repository.WorkflowCompensationStatusPlanned, CreatedAt: now, UpdatedAt: now,
		})
	}
	return compensationRepo.SaveWorkflowCompensationPlan(ctx, records)
}

func (s *AgentService) finalizeWorkflowFailureWithCompensation(
	ctx context.Context,
	run *repository.WorkflowRunRecord,
	snapshot map[string]map[string]interface{},
	traces []engine.NodeTrace,
	scheduler *engine.Scheduler,
	executionErr error,
) (*WorkflowExecutionResult, error) {
	executionErr = s.attachWorkflowCompensationPlan(ctx, run, scheduler, executionErr)
	result, err := s.finishWorkflowRun(ctx, run, snapshot, traces, executionErr, scheduler.GetBudgetSnapshot())
	if err != nil || result == nil {
		return result, err
	}
	return s.driveWorkflowCompensations(ctx, result, primitive.NilObjectID, false)
}

func (s *AgentService) driveWorkflowCompensations(
	ctx context.Context,
	result *WorkflowExecutionResult,
	approvalID primitive.ObjectID,
	retryFailed bool,
) (*WorkflowExecutionResult, error) {
	return s.driveWorkflowCompensationsWithPolicy(ctx, result, approvalID, retryFailed, nil)
}

type workflowCompensationExecutionPolicy func(*repository.WorkflowCompensationRecord) error

func (s *AgentService) driveWorkflowCompensationsWithPolicy(
	ctx context.Context,
	result *WorkflowExecutionResult,
	approvalID primitive.ObjectID,
	retryFailed bool,
	policy workflowCompensationExecutionPolicy,
) (*WorkflowExecutionResult, error) {
	if result == nil || result.Run == nil {
		return nil, errors.New("workflow compensation run is required")
	}
	repo, ok := s.repo.(repository.WorkflowCompensationRepository)
	if !ok {
		return result, nil
	}
	records, err := repo.ListWorkflowCompensations(ctx, result.Run.ID, result.Run.UserID)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return result, nil
	}
	if err := s.markWorkflowRunCompensating(ctx, result.Run); err != nil {
		return nil, err
	}

	for {
		records, err = repo.ListWorkflowCompensations(ctx, result.Run.ID, result.Run.UserID)
		if err != nil {
			return nil, err
		}
		next := nextWorkflowCompensation(records)
		if next == nil {
			return s.completeWorkflowCompensationRun(ctx, result)
		}
		if next.Status == repository.WorkflowCompensationStatusFailed && !retryFailed {
			return s.failWorkflowCompensationRun(ctx, result, next, errors.New(next.ErrorMessage))
		}
		if next.Status == repository.WorkflowCompensationStatusSuspended && (approvalID.IsZero() || approvalID != next.ApprovalRequestID) {
			return nil, repository.ErrWorkflowCompensationUnavailable
		}
		if next.Status == repository.WorkflowCompensationStatusExecuting && next.LeaseUntil.After(time.Now()) {
			return nil, repository.ErrWorkflowCompensationUnavailable
		}
		if policy != nil {
			if policyErr := policy(next); policyErr != nil {
				return s.deferWorkflowCompensationForManualRetry(ctx, result, repo, next, policyErr)
			}
		}

		attemptID := workflowTool.NewAttemptID()
		claimed, err := repo.ClaimWorkflowCompensation(
			ctx, result.Run.ID, result.Run.UserID, next.Sequence, attemptID,
			time.Now().Add(workflowCompensationLease(next)), approvalID, retryFailed,
		)
		if err != nil {
			return nil, err
		}
		outputs, executionErr := s.executeWorkflowCompensation(ctx, result.Run, claimed)
		if executionErr != nil {
			var pending *workflowTool.ApprovalPendingError
			if errors.As(executionErr, &pending) {
				pendingID, parseErr := primitive.ObjectIDFromHex(pending.ApprovalID)
				if parseErr != nil {
					return nil, fmt.Errorf("invalid compensation approval id: %w", parseErr)
				}
				if err := repo.SuspendWorkflowCompensation(ctx, claimed.ID, attemptID, pendingID); err != nil {
					return nil, err
				}
				return s.suspendWorkflowCompensationRun(ctx, result, claimed, pendingID)
			}
			if err := repo.FailWorkflowCompensation(ctx, claimed.ID, attemptID, executionErr.Error()); err != nil {
				return nil, errors.Join(executionErr, err)
			}
			return s.failWorkflowCompensationRun(ctx, result, claimed, executionErr)
		}
		outputJSON, err := json.Marshal(outputs)
		if err != nil {
			return nil, fmt.Errorf("marshal workflow compensation output: %w", err)
		}
		if err := repo.CompleteWorkflowCompensation(ctx, claimed.ID, attemptID, string(outputJSON)); err != nil {
			return nil, err
		}
		approvalID = primitive.NilObjectID
		retryFailed = false
	}
}

func (s *AgentService) deferWorkflowCompensationForManualRetry(
	ctx context.Context,
	result *WorkflowExecutionResult,
	repo repository.WorkflowCompensationRepository,
	record *repository.WorkflowCompensationRecord,
	cause error,
) (*WorkflowExecutionResult, error) {
	if record == nil {
		return nil, errors.New("workflow compensation record is required")
	}
	attemptID := workflowTool.NewAttemptID()
	claimed, err := repo.ClaimWorkflowCompensation(
		ctx, result.Run.ID, result.Run.UserID, record.Sequence, attemptID,
		time.Now().Add(workflowCompensationLease(record)), primitive.NilObjectID, false,
	)
	if err != nil {
		return nil, err
	}
	message := "workflow compensation requires explicit retry"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = cause.Error()
	}
	if err := repo.FailWorkflowCompensation(ctx, claimed.ID, attemptID, message); err != nil {
		return nil, err
	}
	claimed.ErrorMessage = message
	claimed.Status = repository.WorkflowCompensationStatusFailed
	return s.failWorkflowCompensationRun(ctx, result, claimed, errors.New(message))
}

func (s *AgentService) backgroundWorkflowCompensationPolicy(record *repository.WorkflowCompensationRecord) error {
	if s == nil || s.workflowToolExecutor == nil || s.workflowToolExecutor.Registry() == nil {
		return errors.New("background compensation recovery requires an available tool registry; retry explicitly")
	}
	registered, ok := s.workflowToolExecutor.Registry().Get(record.ToolName)
	if !ok {
		return fmt.Errorf("background compensation tool %s is unavailable; retry explicitly", record.ToolName)
	}
	if registered.Spec.RequiresApproval() {
		return fmt.Errorf("background compensation tool %s requires approval; retry explicitly to obtain a resume token", record.ToolName)
	}
	return nil
}

func (s *AgentService) executeWorkflowCompensation(
	ctx context.Context,
	run *repository.WorkflowRunRecord,
	record *repository.WorkflowCompensationRecord,
) (map[string]interface{}, error) {
	if s.workflowToolExecutor == nil {
		return nil, errors.New("workflow tool executor is not configured")
	}
	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(record.InputJSON), &inputs); err != nil {
		return nil, fmt.Errorf("decode workflow compensation inputs: %w", err)
	}
	var retry *dsl.RetryPolicyDSL
	if record.RetryJSON != "" && record.RetryJSON != "null" {
		if err := json.Unmarshal([]byte(record.RetryJSON), &retry); err != nil {
			return nil, fmt.Errorf("decode workflow compensation retry: %w", err)
		}
	}
	task := engine.CompensationTask{
		Sequence: record.Sequence, SourceNodeID: record.SourceNodeID, StepID: record.StepID,
		ToolName: record.ToolName, Inputs: inputs, TimeoutSec: record.TimeoutSec, Retry: retry,
	}
	execCtx := guardrails.InjectUserContext(ctx, run.UserID)
	executionMetadata := workflowExecutionMetadata(run)
	executionMetadata.IdempotencyKey = record.IdempotencyKey
	execCtx = workflowTool.InjectExecutionMetadata(execCtx, executionMetadata)
	outputs, _, err := engine.ExecuteCompensationTask(execCtx, task, func(attemptCtx context.Context, _ int) (map[string]interface{}, error) {
		return s.workflowToolExecutor.ExecuteRegistered(attemptCtx, workflowTool.ExecutionRequest{
			ToolName: record.ToolName, Inputs: inputs,
			Identity: workflowTool.CallerIdentity{UserID: run.UserID},
			RunID:    run.ID.Hex(), StepID: record.StepID, Source: workflowTool.SourceWorkflow,
			IdempotencyKey: record.IdempotencyKey,
		})
	})
	return outputs, err
}

func nextWorkflowCompensation(records []*repository.WorkflowCompensationRecord) *repository.WorkflowCompensationRecord {
	for _, record := range records {
		if record != nil && record.Status != repository.WorkflowCompensationStatusSucceeded {
			return record
		}
	}
	return nil
}

func workflowCompensationLease(record *repository.WorkflowCompensationRecord) time.Duration {
	lease := defaultWorkflowCompensationLease
	if record != nil && record.TimeoutSec > 0 {
		configured := time.Duration(record.TimeoutSec)*time.Second + time.Minute
		if configured > lease {
			lease = configured
		}
	}
	return lease
}

func (s *AgentService) markWorkflowRunCompensating(ctx context.Context, run *repository.WorkflowRunRecord) error {
	if run.Status == WorkflowRunStatusCompensating {
		return nil
	}
	run.Status = WorkflowRunStatusCompensating
	run.FinishedAt = time.Time{}
	clearWorkflowRunSuspension(run)
	return s.repo.UpdateWorkflowRun(ctx, run)
}

func (s *AgentService) completeWorkflowCompensationRun(ctx context.Context, result *WorkflowExecutionResult) (*WorkflowExecutionResult, error) {
	result.Run.Status = WorkflowRunStatusCompensated
	result.Run.FinishedAt = time.Now()
	clearWorkflowRunSuspension(result.Run)
	if err := s.repo.UpdateWorkflowRun(ctx, result.Run); err != nil {
		return nil, err
	}
	result.Response = "工作流执行失败，但已完成全部补偿操作。"
	return result, nil
}

func (s *AgentService) failWorkflowCompensationRun(ctx context.Context, result *WorkflowExecutionResult, record *repository.WorkflowCompensationRecord, cause error) (*WorkflowExecutionResult, error) {
	result.Run.Status = WorkflowRunStatusCompensationFailed
	result.Run.FinishedAt = time.Now()
	clearWorkflowRunSuspension(result.Run)
	message := fmt.Sprintf("compensation step %s failed", record.StepID)
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message += ": " + cause.Error()
	}
	if result.Run.ErrorMessage != "" {
		result.Run.ErrorMessage = result.Run.ErrorMessage + "; " + message
	} else {
		result.Run.ErrorMessage = message
	}
	if err := s.repo.UpdateWorkflowRun(ctx, result.Run); err != nil {
		return nil, err
	}
	result.Response = "工作流执行失败，且补偿操作未能全部完成。"
	return result, nil
}

func (s *AgentService) suspendWorkflowCompensationRun(ctx context.Context, result *WorkflowExecutionResult, record *repository.WorkflowCompensationRecord, approvalID primitive.ObjectID) (*WorkflowExecutionResult, error) {
	resumeToken, err := newWorkflowResumeToken()
	if err != nil {
		return nil, err
	}
	checkpointJSON, err := json.Marshal(workflowCompensationCheckpoint{
		Kind: workflowCompensationCheckpointKind, Sequence: record.Sequence,
	})
	if err != nil {
		return nil, err
	}
	result.Run.Status = WorkflowRunStatusSuspended
	result.Run.WaitingNodeID = record.StepID
	result.Run.ApprovalRequestID = approvalID
	result.Run.ResumeToken = ""
	result.Run.ResumeTokenHash = hashWorkflowResumeToken(resumeToken)
	result.Run.ResumeAttemptID = ""
	result.Run.ResumeGrantIssuedAt = time.Time{}
	result.Run.ResumeGrantExpiresAt = time.Time{}
	result.Run.CheckpointJSON = string(checkpointJSON)
	result.Run.SuspendedAt = time.Now()
	result.Run.FinishedAt = time.Time{}
	if err := s.repo.UpdateWorkflowRun(ctx, result.Run); err != nil {
		return nil, err
	}
	result.ResumeToken = resumeToken
	result.Response = "补偿操作需要人工审批，工作流已安全挂起。"
	return result, nil
}

func clearWorkflowRunSuspension(run *repository.WorkflowRunRecord) {
	run.CheckpointJSON = ""
	run.WaitingNodeID = ""
	run.ApprovalRequestID = primitive.NilObjectID
	run.ResumeToken = ""
	run.ResumeTokenHash = ""
	run.ResumeAttemptID = ""
	run.ResumeGrantIssuedAt = time.Time{}
	run.ResumeGrantExpiresAt = time.Time{}
	run.SuspendedAt = time.Time{}
}

func workflowCompensationCheckpointFromJSON(raw string) (workflowCompensationCheckpoint, bool) {
	var checkpoint workflowCompensationCheckpoint
	if raw == "" || json.Unmarshal([]byte(raw), &checkpoint) != nil {
		return workflowCompensationCheckpoint{}, false
	}
	return checkpoint, checkpoint.Kind == workflowCompensationCheckpointKind && checkpoint.Sequence > 0
}

func isRecoverableCompensationRunStatus(status string) bool {
	switch status {
	case WorkflowRunStatusFailed, WorkflowRunStatusCompensating, WorkflowRunStatusCompensationFailed:
		return true
	default:
		return false
	}
}
