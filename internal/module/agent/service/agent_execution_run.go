package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrAgentExecutionRunStoreUnavailable = errors.New("agent execution run store is unavailable")
	ErrAgentExecutionRuntimeNotEntered   = errors.New("agent execution did not enter the governed runtime")
)

const agentExecutionStateCommitTimeout = 3 * time.Second

type agentExecutionCaptureContextKey struct{}

type agentExecutionCapture struct {
	mu              sync.Mutex
	runID           string
	dialogueID      string
	resumeAttemptID string
	called          bool
	request         agentRuntime.RunRequest
	result          agentRuntime.RunResult
	err             error
}

type capturedAgentExecution struct {
	runID           string
	dialogueID      string
	resumeAttemptID string
	called          bool
	request         agentRuntime.RunRequest
	result          agentRuntime.RunResult
	err             error
}

func (s *AgentService) beginUnifiedAgentExecutionRun(
	ctx context.Context,
	request UnifiedAgentRequest,
	plan AgentCapabilityPlan,
	strategyPlan agentStrategy.Plan,
) (context.Context, *repository.AgentExecutionRun, error) {
	if !s.recoverableAgentRuns {
		return ctx, nil, nil
	}
	if s.agentExecutionRunStore == nil {
		return ctx, nil, ErrAgentExecutionRunStoreUnavailable
	}
	now := time.Now()
	runID := primitive.NewObjectID().Hex()
	strategyPlan = agentStrategy.ClonePlan(strategyPlan)
	run := &repository.AgentExecutionRun{
		ID:                    runID,
		UserID:                request.UserID,
		ExecutionProfile:      strings.TrimSpace(plan.ExecutionProfile),
		CapabilityIDs:         append([]string(nil), plan.CapabilityIDs...),
		SkillID:               strings.TrimSpace(request.SkillID),
		SkillVersion:          strings.TrimSpace(request.SkillVersion),
		TaskTemplateID:        strings.TrimSpace(request.TaskTemplateID),
		TaskTemplateRevision:  request.TaskTemplateRevision,
		ExecutionStrategyPlan: &strategyPlan,
		Status:                repository.AgentExecutionRunRunning,
		Revision:              1,
		StateVersion:          repository.AgentExecutionStateVersion,
		InputDigest:           agentExecutionScopedDigest(runID, request.Content),
		ResumeSupported:       false,
		StartedAt:             now,
		UpdatedAt:             now,
	}
	if err := s.agentExecutionRunStore.CreateAgentExecutionRun(ctx, run); err != nil {
		return ctx, nil, fmt.Errorf("create agent execution run: %w", err)
	}
	if s.unifiedAgentProductObserver != nil {
		s.unifiedAgentProductObserver.ObserveTaskStarted(UnifiedAgentTaskStartedObservation{
			ExecutionProfile: run.ExecutionProfile,
			Strategy:         strategyPlan.SelectedStrategy,
		})
	}
	capture := &agentExecutionCapture{runID: run.ID}
	return context.WithValue(ctx, agentExecutionCaptureContextKey{}, capture), run, nil
}

func (s *AgentService) finishUnifiedAgentExecutionRun(
	ctx context.Context,
	run *repository.AgentExecutionRun,
	result *UnifiedAgentResult,
	executionErr error,
) error {
	if run == nil {
		return nil
	}
	if s.agentExecutionRunStore == nil {
		return ErrAgentExecutionRunStoreUnavailable
	}
	captured, ok := capturedAgentExecutionFromContext(ctx)
	if !ok {
		return errors.New("agent execution capture is unavailable")
	}
	status, failureCode, lifecycleErr := classifyAgentExecutionOutcome(ctx, captured, executionErr)
	if lifecycleErr != nil && executionErr == nil {
		executionErr = lifecycleErr
	}

	output := ""
	if result != nil {
		output = result.Response
	}
	if output == "" {
		output = captured.result.FinalAnswer
	}
	pendingType := ""
	pendingName := ""
	pendingID := ""
	pendingResumeKind := string(captured.result.PendingResumeKind)
	if captured.result.PendingAction != nil {
		pendingType = string(captured.result.PendingAction.Type)
		pendingName = captured.result.PendingAction.Name
		pendingID = captured.result.PendingAction.ID
	}
	if pendingResumeKind == "" {
		switch {
		case pendingType == string(agentRuntime.ActionAskHuman):
			pendingResumeKind = repository.AgentExecutionResumeHuman
		case status == repository.AgentExecutionRunApprovalRequired:
			pendingResumeKind = repository.AgentExecutionResumeApproval
		}
	}
	runtimeContext := captured.request.Context
	usage := captured.result.Usage
	checkpoint := sealedAgentRunCheckpoint{}
	resumeSupported := false
	var approvalBinding *repository.ToolApprovalRequest
	humanResumeAction := pendingType == string(agentRuntime.ActionAskHuman) ||
		(pendingType == string(agentRuntime.ActionToolCall) &&
			pendingResumeKind == repository.AgentExecutionResumeHuman &&
			captured.result.PendingToolContinuation != nil)
	if status == repository.AgentExecutionRunAwaitingHuman && humanResumeAction {
		if _, resumable := s.runtimeRunner.(agentRuntime.ResumableAgentRunner); resumable &&
			s.agentCheckpointCipher != nil {
			checkpoint, lifecycleErr = s.sealAgentRunCheckpoint(run, captured.request, captured.result)
			if lifecycleErr != nil {
				status = repository.AgentExecutionRunFailed
				failureCode = "checkpoint_unavailable"
				if executionErr == nil {
					executionErr = lifecycleErr
				}
			} else {
				resumeSupported = true
			}
		}
	}
	if status == repository.AgentExecutionRunApprovalRequired &&
		pendingType == string(agentRuntime.ActionToolCall) && s.unifiedAgentApprovalRecovery {
		if _, resumable := s.runtimeRunner.(agentRuntime.ResumableAgentRunner); resumable &&
			s.agentCheckpointCipher != nil {
			approvalBinding, lifecycleErr = s.resolveRuntimeApprovalBinding(ctx, run, captured)
			if lifecycleErr == nil {
				checkpoint, lifecycleErr = s.sealAgentRunCheckpoint(run, captured.request, captured.result)
			}
			if lifecycleErr != nil {
				status = repository.AgentExecutionRunFailed
				failureCode = "checkpoint_unavailable"
				if executionErr == nil {
					executionErr = lifecycleErr
				}
			} else {
				resumeSupported = true
			}
		}
	}
	commit := repository.AgentExecutionRunCommit{
		RunID:                   run.ID,
		UserID:                  run.UserID,
		ExpectedRevision:        run.Revision,
		DialogueID:              captured.dialogueID,
		Status:                  status,
		Mode:                    string(runtimeContext.Mode),
		Model:                   captured.request.Model,
		AgentProfileID:          runtimeContext.AgentProfileID,
		AgentProfileVersion:     runtimeContext.AgentProfileVersion,
		PromptTemplateID:        runtimeContext.PromptTemplateID,
		PromptTemplateVersion:   runtimeContext.PromptTemplateVersion,
		ResultDigest:            agentExecutionScopedDigest(run.ID, output),
		PublishableDraft:        status == repository.AgentExecutionRunCompleted && result != nil && result.PublishableDraft,
		FailureCode:             failureCode,
		FailureDigest:           agentExecutionErrorDigest(run.ID, executionErr, captured.err),
		PendingActionType:       pendingType,
		PendingActionName:       pendingName,
		PendingActionID:         pendingID,
		PendingResumeKind:       pendingResumeKind,
		StepCount:               len(captured.result.Steps),
		InputTokens:             usage.InputTokens,
		OutputTokens:            usage.OutputTokens,
		TotalTokens:             usage.TotalTokens,
		UsageEstimated:          usage.Estimated,
		EstimatedCostMicros:     usage.EstimatedCostMicros,
		CostEstimated:           usage.CostEstimated,
		PricingVersion:          usage.PricingVersion,
		MaxSteps:                runtimeContext.Budget.MaxSteps,
		MaxTotalTokens:          runtimeContext.Budget.MaxTotalTokens,
		MaxEstimatedCostMicros:  runtimeContext.Budget.MaxEstimatedCostMicros,
		ResumeSupported:         resumeSupported,
		CheckpointVersion:       checkpoint.Version,
		CheckpointKeyID:         checkpoint.KeyID,
		CheckpointNonce:         checkpoint.Nonce,
		CheckpointCiphertext:    checkpoint.Ciphertext,
		CheckpointDigest:        checkpoint.Digest,
		CheckpointSizeBytes:     checkpoint.SizeBytes,
		ExpectedResumeAttemptID: captured.resumeAttemptID,
		UpdatedAt:               time.Now(),
	}
	if captured.called {
		commit.AccountingVersion = repository.ExecutionAccountingVersion
	}
	if approvalBinding != nil && resumeSupported {
		commit.ApprovalRequestID = approvalBinding.ID.Hex()
		commit.ApprovalInputDigest = approvalBinding.InputDigest
		commit.ApprovalIdempotencyKey = approvalBinding.IdempotencyKey
		commit.ApprovalExpiresAt = approvalBinding.ExpiresAt
	}
	commitParent := ctx
	if commitParent == nil {
		commitParent = context.Background()
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(commitParent), agentExecutionStateCommitTimeout)
	defer cancel()
	updated, err := s.agentExecutionRunStore.CommitAgentExecutionRun(commitCtx, commit)
	if err != nil {
		return fmt.Errorf("commit agent execution run: %w", err)
	}
	*run = *updated
	if s.unifiedAgentProductObserver != nil {
		s.unifiedAgentProductObserver.ObserveTaskCommitted(
			unifiedAgentCommittedObservation(updated, captured, result, commit.UpdatedAt),
		)
	}
	if updated.PublishableDraft {
		s.recordDraftReadyProductEvent(ctx, updated)
	}
	return lifecycleErr
}

func unifiedAgentCommittedObservation(
	run *repository.AgentExecutionRun,
	captured capturedAgentExecution,
	result *UnifiedAgentResult,
	committedAt time.Time,
) UnifiedAgentTaskCommittedObservation {
	observation := UnifiedAgentTaskCommittedObservation{Duration: -1, StepCount: -1}
	if run == nil {
		return observation
	}
	observation.ExecutionProfile = run.ExecutionProfile
	observation.Status = run.Status
	observation.StepCount = run.StepCount
	observation.PublishableDraft = run.PublishableDraft
	observation.Usage = agentRuntime.TokenUsage{
		InputTokens:         run.InputTokens,
		OutputTokens:        run.OutputTokens,
		TotalTokens:         run.TotalTokens,
		Estimated:           run.UsageEstimated,
		EstimatedCostMicros: run.EstimatedCostMicros,
		CostEstimated:       run.CostEstimated,
		PricingVersion:      run.PricingVersion,
	}
	if run.ExecutionStrategyPlan != nil {
		observation.Strategy = run.ExecutionStrategyPlan.SelectedStrategy
	}
	if !run.StartedAt.IsZero() && !committedAt.IsZero() && !committedAt.Before(run.StartedAt) {
		observation.Duration = committedAt.Sub(run.StartedAt)
	}
	if result != nil {
		observation.ToolActivities = append([]AgentToolActivity(nil), result.ToolActivities...)
		observation.Citations = append([]AgentCitation(nil), result.Citations...)
	}
	if len(observation.ToolActivities) == 0 && captured.called {
		activities, _ := collectRuntimeResultEvidence(captured.result)
		observation.ToolActivities = activities
	}
	return observation
}

func (s *AgentService) resolveRuntimeApprovalBinding(
	ctx context.Context,
	run *repository.AgentExecutionRun,
	captured capturedAgentExecution,
) (*repository.ToolApprovalRequest, error) {
	if run == nil || captured.result.PendingAction == nil ||
		captured.result.PendingAction.Type != agentRuntime.ActionToolCall {
		return nil, errors.New("runtime approval action is unavailable")
	}
	if captured.result.PendingResumeKind == agentRuntime.ResumeKindDelegatedToolApproval {
		return s.resolveWorkflowToolApprovalBinding(ctx, run, captured)
	}
	approvalID := strings.TrimSpace(captured.result.ApprovalID)
	approvalOID, err := primitive.ObjectIDFromHex(approvalID)
	if err != nil {
		return nil, fmt.Errorf("runtime approval id is invalid: %w", err)
	}
	approvalRepo, err := s.toolApprovalRepository()
	if err != nil {
		return nil, err
	}
	approval, err := approvalRepo.GetToolApproval(ctx, approvalOID, run.UserID)
	if err != nil {
		return nil, err
	}
	action := captured.result.PendingAction
	expectedKey := toolIdempotencyKey(run.ID, action.ID, action.Name)
	if approval.RunID != run.ID || approval.StepID != action.ID || approval.ToolName != action.Name ||
		approval.Source != string(workflowTool.SourceRuntime) ||
		strings.TrimSpace(approval.InputDigest) == "" || approval.IdempotencyKey != expectedKey {
		return nil, errors.New("runtime approval binding does not match the suspended action")
	}
	if approval.Status != repository.ToolApprovalStatusPending && approval.Status != repository.ToolApprovalStatusApproved {
		return nil, fmt.Errorf("runtime approval status %q cannot suspend a run", approval.Status)
	}
	if approval.ExpiresAt.IsZero() || !approval.ExpiresAt.After(time.Now()) {
		return nil, errors.New("runtime approval expired before checkpoint commit")
	}
	return approval, nil
}

func (s *AgentService) GetAgentExecutionRun(
	ctx context.Context,
	userID uint64,
	runID string,
) (*repository.AgentExecutionRun, error) {
	if s == nil || !s.recoverableAgentRuns || s.agentExecutionRunStore == nil {
		return nil, ErrAgentExecutionRunStoreUnavailable
	}
	return s.agentExecutionRunStore.GetAgentExecutionRun(ctx, strings.TrimSpace(runID), userID)
}

func (s *AgentService) runRuntime(
	ctx context.Context,
	request agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	result, err := s.runtimeRunner.Run(ctx, request)
	captureAgentRuntimeExecution(ctx, request, result, err)
	return result, err
}

func captureAgentRuntimeExecution(
	ctx context.Context,
	request agentRuntime.RunRequest,
	result agentRuntime.RunResult,
	err error,
) {
	if capture := agentExecutionCaptureFromContext(ctx); capture != nil {
		capture.mu.Lock()
		capture.called = true
		capture.request = cloneAgentRuntimeRequest(request)
		capture.result = result
		capture.err = err
		capture.mu.Unlock()
	}
}

func agentExecutionRunID(ctx context.Context) string {
	if capture := agentExecutionCaptureFromContext(ctx); capture != nil {
		capture.mu.Lock()
		defer capture.mu.Unlock()
		if capture.runID != "" {
			return capture.runID
		}
	}
	return primitive.NewObjectID().Hex()
}

func noteAgentExecutionDialogue(ctx context.Context, dialogueID string) {
	if capture := agentExecutionCaptureFromContext(ctx); capture != nil {
		capture.mu.Lock()
		capture.dialogueID = strings.TrimSpace(dialogueID)
		capture.mu.Unlock()
	}
}

func agentExecutionCaptureFromContext(ctx context.Context) *agentExecutionCapture {
	if ctx == nil {
		return nil
	}
	capture, _ := ctx.Value(agentExecutionCaptureContextKey{}).(*agentExecutionCapture)
	return capture
}

func capturedAgentExecutionFromContext(ctx context.Context) (capturedAgentExecution, bool) {
	capture := agentExecutionCaptureFromContext(ctx)
	if capture == nil {
		return capturedAgentExecution{}, false
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capturedAgentExecution{
		runID: capture.runID, dialogueID: capture.dialogueID, resumeAttemptID: capture.resumeAttemptID,
		called:  capture.called,
		request: cloneAgentRuntimeRequest(capture.request), result: capture.result, err: capture.err,
	}, true
}

func classifyAgentExecutionOutcome(
	ctx context.Context,
	captured capturedAgentExecution,
	executionErr error,
) (repository.AgentExecutionRunStatus, string, error) {
	if !captured.called {
		return repository.AgentExecutionRunFailed, "runtime_not_entered", ErrAgentExecutionRuntimeNotEntered
	}
	switch captured.result.Status {
	case agentRuntime.RunStatusAwaitingHuman:
		return repository.AgentExecutionRunAwaitingHuman, "", nil
	case agentRuntime.RunStatusApprovalRequired:
		return repository.AgentExecutionRunApprovalRequired, "", nil
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(executionErr, context.Canceled) ||
		agentRuntime.HasErrorCode(captured.err, agentRuntime.ErrorCanceled) {
		return repository.AgentExecutionRunCanceled, "canceled", nil
	}
	if executionErr == nil && captured.err == nil && captured.result.Status == agentRuntime.RunStatusCompleted {
		return repository.AgentExecutionRunCompleted, "", nil
	}
	return repository.AgentExecutionRunFailed, agentExecutionFailureCode(executionErr, captured.err), nil
}

func agentExecutionFailureCode(errorsToInspect ...error) string {
	for _, err := range errorsToInspect {
		if err == nil {
			continue
		}
		var runErr *agentRuntime.RunError
		if errors.As(err, &runErr) && runErr.Code != "" {
			return string(runErr.Code)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return string(agentRuntime.ErrorTimeout)
		}
		if errors.Is(err, context.Canceled) {
			return string(agentRuntime.ErrorCanceled)
		}
	}
	return "agent_execution_failed"
}

func agentExecutionErrorDigest(runID string, errorsToInspect ...error) string {
	for _, err := range errorsToInspect {
		if err != nil {
			return agentExecutionScopedDigest(runID, err.Error())
		}
	}
	return ""
}

func agentExecutionScopedDigest(runID, value string) string {
	runID = strings.TrimSpace(runID)
	value = strings.TrimSpace(value)
	if runID == "" || value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(runID + "\x00" + value))
	return hex.EncodeToString(digest[:])
}

func cloneAgentRuntimeRequest(request agentRuntime.RunRequest) agentRuntime.RunRequest {
	cloned := request
	cloned.Messages = append([]agentRuntime.Message(nil), request.Messages...)
	cloned.Tools = append([]agentRuntime.ToolDefinition(nil), request.Tools...)
	return cloned
}
