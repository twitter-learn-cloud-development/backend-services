package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
	"twitter-clone/internal/module/agent/workflow/tool"
)

const (
	WorkflowRunStatusRunning            = "running"
	WorkflowRunStatusSuspended          = "suspended"
	WorkflowRunStatusSuccess            = "success"
	WorkflowRunStatusFailed             = "failed"
	WorkflowRunStatusRejected           = "rejected"
	WorkflowRunStatusCompensating       = "compensating"
	WorkflowRunStatusCompensated        = "compensated"
	WorkflowRunStatusCompensationFailed = "compensation_failed"
	WorkflowRunStatusCanceling          = "canceling"
	WorkflowRunStatusCanceled           = "canceled"
)

type WorkflowExecutionResult struct {
	Run         *repository.WorkflowRunRecord
	Snapshot    map[string]map[string]interface{}
	Traces      []engine.NodeTrace
	DialogueKey string
	Response    string
	ResumeToken string
}

func (s *AgentService) CreateWorkflow(ctx context.Context, userID uint64, name string, dslJSON string) (*repository.WorkflowDefinition, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	if name == "" {
		return nil, errors.New("workflow name is required")
	}
	normalizedDSL, err := s.normalizeWorkflowDSLJSON(dslJSON)
	if err != nil {
		return nil, err
	}

	workflow := &repository.WorkflowDefinition{
		UserID:  userID,
		Name:    name,
		DSLJSON: normalizedDSL,
	}
	if err := s.repo.CreateWorkflow(ctx, workflow); err != nil {
		return nil, err
	}
	return workflow, nil
}

func (s *AgentService) UpdateWorkflow(ctx context.Context, userID uint64, workflowID string, name string, dslJSON string) (*repository.WorkflowDefinition, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	if name == "" {
		return nil, errors.New("workflow name is required")
	}
	normalizedDSL, err := s.normalizeWorkflowDSLJSON(dslJSON)
	if err != nil {
		return nil, err
	}

	oid, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_id: %w", err)
	}

	workflow := &repository.WorkflowDefinition{
		ID:      oid,
		UserID:  userID,
		Name:    name,
		DSLJSON: normalizedDSL,
	}
	if err := s.repo.UpdateWorkflow(ctx, workflow); err != nil {
		return nil, err
	}
	return s.repo.GetWorkflow(ctx, oid, userID)
}

func (s *AgentService) ListWorkflows(ctx context.Context, userID uint64, page, pageSize int) ([]*repository.WorkflowDefinition, int64, error) {
	if s.repo == nil {
		return nil, 0, errors.New("agent repository is not initialized")
	}
	workflows, total, err := s.repo.ListWorkflows(ctx, userID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	for index, workflow := range workflows {
		workflows[index], err = redactWorkflowDefinition(workflow)
		if err != nil {
			return nil, 0, err
		}
	}
	return workflows, total, nil
}

func (s *AgentService) GetWorkflow(ctx context.Context, userID uint64, workflowID string) (*repository.WorkflowDefinition, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	oid, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_id: %w", err)
	}
	workflow, err := s.repo.GetWorkflow(ctx, oid, userID)
	if err != nil {
		return nil, err
	}
	return redactWorkflowDefinition(workflow)
}

func (s *AgentService) ListWorkflowRevisions(ctx context.Context, userID uint64, workflowID string, page, pageSize int) ([]*repository.WorkflowRevision, int64, error) {
	if s.repo == nil {
		return nil, 0, errors.New("agent repository is not initialized")
	}
	workflowOID, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid workflow_id: %w", err)
	}
	workflow, err := s.repo.GetWorkflow(ctx, workflowOID, userID)
	if err != nil {
		return nil, 0, err
	}
	if _, err := s.resolveCurrentWorkflowRevision(ctx, workflow); err != nil {
		return nil, 0, fmt.Errorf("resolve current workflow revision: %w", err)
	}
	revisionRepo, err := s.workflowRevisionRepository()
	if err != nil {
		return nil, 0, err
	}
	revisions, total, err := revisionRepo.ListWorkflowRevisions(ctx, workflowOID, userID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	for index, revision := range revisions {
		revisions[index], err = redactWorkflowRevision(revision)
		if err != nil {
			return nil, 0, err
		}
	}
	return revisions, total, nil
}

func (s *AgentService) GetWorkflowRevision(ctx context.Context, userID uint64, workflowID, revisionID string) (*repository.WorkflowRevision, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	workflowOID, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_id: %w", err)
	}
	revisionOID, err := primitive.ObjectIDFromHex(revisionID)
	if err != nil {
		return nil, fmt.Errorf("invalid revision_id: %w", err)
	}
	if _, err := s.repo.GetWorkflow(ctx, workflowOID, userID); err != nil {
		return nil, err
	}
	revisionRepo, err := s.workflowRevisionRepository()
	if err != nil {
		return nil, err
	}
	revision, err := revisionRepo.GetWorkflowRevision(ctx, workflowOID, revisionOID, userID)
	if err != nil {
		return nil, err
	}
	if _, err := validateWorkflowRevisionIntegrity(revision); err != nil {
		return nil, err
	}
	return redactWorkflowRevision(revision)
}

func (s *AgentService) RunWorkflow(ctx context.Context, userID uint64, workflowID string, inputJSON string) (*WorkflowExecutionResult, error) {
	return s.RunWorkflowRevision(ctx, userID, workflowID, "", inputJSON)
}

func (s *AgentService) RunWorkflowRevision(ctx context.Context, userID uint64, workflowID, workflowRevisionID, inputJSON string) (*WorkflowExecutionResult, error) {
	return s.runWorkflowRevision(ctx, userID, workflowID, workflowRevisionID, inputJSON, workflowInvocation{})
}

type workflowInvocation struct {
	Source         string
	ParentRunID    string
	ParentActionID string
	NestedRuntime  bool
}

func (s *AgentService) runWorkflowRevision(
	ctx context.Context,
	userID uint64,
	workflowID, workflowRevisionID, inputJSON string,
	invocation workflowInvocation,
) (*WorkflowExecutionResult, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}

	workflowOID, err := primitive.ObjectIDFromHex(workflowID)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_id: %w", err)
	}
	workflow, err := s.repo.GetWorkflow(ctx, workflowOID, userID)
	if err != nil {
		return nil, err
	}
	workflowRevision, err := s.resolveRequestedWorkflowRevision(ctx, workflow, workflowRevisionID)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow revision: %w", err)
	}
	if s.runtimeAdmission != nil {
		var release agentRuntime.ReleaseFunc
		admissionUserID := userID
		if invocation.NestedRuntime {
			// The parent Runtime already owns the per-user slot. The child
			// still acquires its workflow slot so one published workflow
			// cannot bypass its own concurrency limit.
			admissionUserID = 0
		}
		ctx, release, err = s.runtimeAdmission.Acquire(ctx, agentRuntime.AdmissionRequest{
			UserID: admissionUserID, WorkflowID: workflow.ID.Hex(),
		})
		if err != nil {
			return nil, fmt.Errorf("workflow admission failed: %w", err)
		}
		defer release()
	}

	var dslObj dsl.WorkflowDSL
	if err := json.Unmarshal([]byte(workflowRevision.DSLJSON), &dslObj); err != nil {
		return nil, fmt.Errorf("invalid workflow DSL JSON: %w", err)
	}
	if err := validateWorkflowSecurity(&dslObj); err != nil {
		return nil, err
	}

	initialInputs, err := parseWorkflowInput(inputJSON)
	if err != nil {
		return nil, err
	}

	var workflowDialogue *repository.Dialogue
	if boolWorkflowInput(initialInputs, "persist_dialogue") {
		userInput, _ := initialInputs["user_input"].(string)
		dialogueKey, _ := initialInputs["dialogue_key"].(string)
		workflowDialogue, err = s.getOrCreateDialogue(
			ctx,
			userID,
			resolveDialogueKey(0, dialogueKey),
			userInput,
			repository.ModeWorkflow,
		)
		if err != nil {
			return nil, fmt.Errorf("prepare workflow dialogue failed: %w", err)
		}
		initialInputs["dialogue_key"] = workflowDialogue.ID.Hex()
	}

	run := &repository.WorkflowRunRecord{
		WorkflowID:             workflow.ID,
		WorkflowRevisionID:     workflowRevision.ID,
		WorkflowRevisionNumber: workflowRevision.RevisionNumber,
		UserID:                 userID,
		InvocationSource:       invocation.Source,
		ParentRunID:            invocation.ParentRunID,
		ParentActionID:         invocation.ParentActionID,
		Status:                 WorkflowRunStatusRunning,
		InputJSON:              inputJSON,
		StartedAt:              time.Now(),
	}
	if run.InputJSON == "" {
		run.InputJSON = "{}"
	}
	budgetTracker, maxParallelNodes, budgetErr := s.workflowBudgetTracker(&dslObj, agentRuntime.BudgetSnapshot{})
	if budgetErr != nil {
		return nil, budgetErr
	}
	applyWorkflowBudgetLimits(run, budgetTracker.Budget())
	if err := s.repo.CreateWorkflowRun(ctx, run); err != nil {
		return nil, err
	}
	s.recordWorkflowExecution(ctx, run, nil, agentRuntime.BudgetSnapshot{})

	nodeImpls, err := s.buildWorkflowNodes(&dslObj)
	if err != nil {
		result, finishErr := s.finishWorkflowRun(ctx, run, nil, nil, err, budgetTracker.Snapshot())
		return s.persistWorkflowDialogue(ctx, result, workflowDialogue, initialInputs, nil, err, finishErr)
	}

	scheduler, err := engine.NewScheduler(
		&dslObj,
		nodeImpls,
		s.workflowStateCommitOption(run),
		engine.WithExecutionBudget(budgetTracker, maxParallelNodes),
	)
	if err != nil {
		result, finishErr := s.finishWorkflowRun(ctx, run, nil, nil, err, budgetTracker.Snapshot())
		return s.persistWorkflowDialogue(ctx, result, workflowDialogue, initialInputs, nil, err, finishErr)
	}

	executionCtx, stopCancellationWatcher := s.workflowExecutionContext(ctx, run)
	defer stopCancellationWatcher()
	execCtx := guardrails.InjectUserContext(executionCtx, userID)
	execCtx = tool.InjectExecutionMetadata(execCtx, workflowExecutionMetadata(run))
	err = scheduler.Execute(execCtx, initialInputs)
	if cause := context.Cause(executionCtx); errors.Is(cause, ErrWorkflowRunCanceled) {
		err = cause
	}
	snapshot := scheduler.GetBlackboard().GetSnapshot()
	traces := scheduler.GetTraces()
	if stateErr := s.persistWorkflowState(ctx, run, scheduler.GetBlackboard(), true); stateErr != nil {
		failure := fmt.Errorf("persist workflow state events: %w", stateErr)
		result, finishErr := s.finalizeWorkflowFailureWithCompensation(ctx, run, snapshot, traces, scheduler, failure)
		return s.persistWorkflowDialogue(ctx, result, workflowDialogue, initialInputs, &dslObj, failure, finishErr)
	}
	var suspension *engine.SuspensionError
	if errors.As(err, &suspension) {
		checkpoint := scheduler.GetCheckpoint(suspension)
		result, suspendErr := s.suspendWorkflowRun(ctx, run, snapshot, traces, checkpoint, suspension)
		return s.persistWorkflowDialogue(ctx, result, workflowDialogue, initialInputs, &dslObj, nil, suspendErr)
	}
	var result *WorkflowExecutionResult
	var finishErr error
	if err != nil {
		result, finishErr = s.finalizeWorkflowFailureWithCompensation(ctx, run, snapshot, traces, scheduler, err)
	} else {
		result, finishErr = s.finishWorkflowRun(ctx, run, snapshot, traces, nil, scheduler.GetBudgetSnapshot())
	}
	return s.persistWorkflowDialogue(ctx, result, workflowDialogue, initialInputs, &dslObj, err, finishErr)
}

func (s *AgentService) ResumeWorkflowRun(ctx context.Context, userID uint64, runID, approvalID, resumeToken, resumeInputJSON string) (*WorkflowExecutionResult, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	oid, err := primitive.ObjectIDFromHex(runID)
	if err != nil {
		return nil, fmt.Errorf("invalid run_id: %w", err)
	}
	run, err := s.repo.GetWorkflowRun(ctx, oid, userID)
	if err != nil {
		return nil, err
	}
	if run.Status != WorkflowRunStatusSuspended {
		if isRecoverableCompensationRunStatus(run.Status) {
			return s.driveWorkflowCompensations(ctx, &WorkflowExecutionResult{Run: run}, primitive.NilObjectID, true)
		}
		return nil, fmt.Errorf("workflow run %s is not suspended", runID)
	}
	if resumeToken == "" {
		return nil, errors.New("resume_token is required")
	}
	if !run.ApprovalRequestID.IsZero() && approvalID == "" {
		return nil, errors.New("approval_id is required for an approval-gated workflow run")
	}

	var approvalOID primitive.ObjectID
	if approvalID != "" {
		approvalOID, err = primitive.ObjectIDFromHex(approvalID)
		if err != nil {
			return nil, fmt.Errorf("invalid approval_id: %w", err)
		}
		if !run.ApprovalRequestID.IsZero() && approvalOID != run.ApprovalRequestID {
			return nil, errors.New("approval_id does not match the suspended workflow run")
		}
		approvalRepo, repoErr := s.toolApprovalRepository()
		if repoErr != nil {
			return nil, repoErr
		}
		approval, getErr := approvalRepo.GetToolApproval(ctx, approvalOID, userID)
		if getErr != nil {
			return nil, getErr
		}
		if approval.RunID != runID || approval.Status != repository.ToolApprovalStatusApproved {
			return nil, errors.New("tool approval is not approved for this workflow run")
		}
	}
	if _, isCompensation := workflowCompensationCheckpointFromJSON(run.CheckpointJSON); isCompensation {
		originalError := run.ErrorMessage
		resumeRepo, ok := s.repo.(repository.WorkflowResumeRepository)
		if !ok {
			return nil, errors.New("workflow resume repository is not available")
		}
		run, err = resumeRepo.ClaimWorkflowRunResume(
			ctx, oid, userID, approvalOID, hashWorkflowResumeToken(resumeToken), tool.NewAttemptID(),
		)
		if err != nil {
			return nil, err
		}
		run.ErrorMessage = originalError
		return s.driveWorkflowCompensations(ctx, &WorkflowExecutionResult{Run: run}, approvalOID, false)
	}

	workflow, err := s.repo.GetWorkflow(ctx, run.WorkflowID, userID)
	if err != nil {
		return nil, err
	}
	workflowRevision, err := s.resolveWorkflowRevisionForRun(ctx, workflow, run)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow run revision: %w", err)
	}
	if s.runtimeAdmission != nil {
		var release agentRuntime.ReleaseFunc
		ctx, release, err = s.runtimeAdmission.Acquire(ctx, agentRuntime.AdmissionRequest{
			UserID: userID, WorkflowID: workflow.ID.Hex(),
		})
		if err != nil {
			return nil, fmt.Errorf("workflow admission failed: %w", err)
		}
		defer release()
	}

	var dslObj dsl.WorkflowDSL
	if err := json.Unmarshal([]byte(workflowRevision.DSLJSON), &dslObj); err != nil {
		return nil, fmt.Errorf("invalid workflow DSL JSON: %w", err)
	}
	if err := validateWorkflowSecurity(&dslObj); err != nil {
		return nil, err
	}
	var checkpoint engine.WorkflowCheckpoint
	if err := json.Unmarshal([]byte(run.CheckpointJSON), &checkpoint); err != nil {
		return nil, fmt.Errorf("invalid workflow checkpoint JSON: %w", err)
	}
	checkpoint, err = s.rehydrateWorkflowCheckpoint(ctx, run, checkpoint)
	if err != nil {
		return nil, fmt.Errorf("rehydrate workflow checkpoint: %w", err)
	}
	resumeInputs, err := parseWorkflowInput(resumeInputJSON)
	if err != nil {
		return nil, err
	}

	budgetTracker, maxParallelNodes, budgetErr := s.workflowBudgetTracker(&dslObj, checkpoint.Budget)
	if budgetErr != nil {
		return s.finishWorkflowRun(ctx, run, nil, nil, budgetErr)
	}
	applyWorkflowBudgetLimits(run, budgetTracker.Budget())
	nodeImpls, err := s.buildWorkflowNodes(&dslObj)
	if err != nil {
		return s.finishWorkflowRun(ctx, run, nil, nil, err, budgetTracker.Snapshot())
	}
	scheduler, err := engine.NewScheduler(
		&dslObj,
		nodeImpls,
		s.workflowStateCommitOption(run),
		engine.WithExecutionBudget(budgetTracker, maxParallelNodes),
	)
	if err != nil {
		return s.finishWorkflowRun(ctx, run, nil, nil, err, budgetTracker.Snapshot())
	}

	resumeRepo, ok := s.repo.(repository.WorkflowResumeRepository)
	if !ok {
		return nil, errors.New("workflow resume repository is not available")
	}
	run, err = resumeRepo.ClaimWorkflowRunResume(
		ctx, oid, userID, approvalOID, hashWorkflowResumeToken(resumeToken), tool.NewAttemptID(),
	)
	if err != nil {
		return nil, err
	}
	executionCtx, stopCancellationWatcher := s.workflowExecutionContext(ctx, run)
	defer stopCancellationWatcher()
	execCtx := guardrails.InjectUserContext(executionCtx, userID)
	execCtx = tool.InjectExecutionMetadata(execCtx, workflowExecutionMetadata(run))
	err = scheduler.ExecuteFromCheckpoint(execCtx, checkpoint, resumeInputs)
	if cause := context.Cause(executionCtx); errors.Is(cause, ErrWorkflowRunCanceled) {
		err = cause
	}
	snapshot := scheduler.GetBlackboard().GetSnapshot()
	traces := scheduler.GetTraces()
	if stateErr := s.persistWorkflowState(ctx, run, scheduler.GetBlackboard(), true); stateErr != nil {
		failure := fmt.Errorf("persist workflow state events: %w", stateErr)
		return s.finalizeWorkflowFailureWithCompensation(ctx, run, snapshot, traces, scheduler, failure)
	}
	var suspension *engine.SuspensionError
	if errors.As(err, &suspension) {
		nextCheckpoint := scheduler.GetCheckpoint(suspension)
		result, suspendErr := s.suspendWorkflowRun(ctx, run, snapshot, traces, nextCheckpoint, suspension)
		if result != nil {
			result.Response = fmt.Sprintf("工作流已在节点 %s 挂起，等待审批或外部回调后继续执行。", result.Run.WaitingNodeID)
		}
		return result, suspendErr
	}
	var result *WorkflowExecutionResult
	var finishErr error
	if err != nil {
		result, finishErr = s.finalizeWorkflowFailureWithCompensation(ctx, run, snapshot, traces, scheduler, err)
	} else {
		result, finishErr = s.finishWorkflowRun(ctx, run, snapshot, traces, nil, scheduler.GetBudgetSnapshot())
	}
	if result != nil {
		result.Response = workflowAssistantContent(snapshot, &dslObj)
		if err != nil {
			result.Response = fmt.Sprintf("工作流执行失败：%v", err)
		} else if result.Response == "" {
			result.Response = "工作流执行完成，但没有产生可展示的文本结果。"
		}
	}
	return result, finishErr
}

func (s *AgentService) GetWorkflowRun(ctx context.Context, userID uint64, runID string) (*repository.WorkflowRunRecord, error) {
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	oid, err := primitive.ObjectIDFromHex(runID)
	if err != nil {
		return nil, fmt.Errorf("invalid run_id: %w", err)
	}
	return s.repo.GetWorkflowRun(ctx, oid, userID)
}

func (s *AgentService) finishWorkflowRun(
	ctx context.Context,
	run *repository.WorkflowRunRecord,
	snapshot map[string]map[string]interface{},
	traces []engine.NodeTrace,
	execErr error,
	budgetSnapshots ...agentRuntime.BudgetSnapshot,
) (*WorkflowExecutionResult, error) {
	var budgetSnapshot agentRuntime.BudgetSnapshot
	if len(budgetSnapshots) > 0 {
		budgetSnapshot = budgetSnapshots[0]
		applyWorkflowAccountingSnapshot(run, budgetSnapshot)
	}
	run.FinishedAt = time.Now()
	if snapshot != nil || len(traces) > 0 {
		output := make(map[string]interface{})
		for nodeID, values := range snapshot {
			output[nodeID] = values
		}
		output["blackboard"] = snapshot
		output["traces"] = traces
		if len(budgetSnapshots) > 0 {
			output["budget"] = budgetSnapshots[0]
		}
		outputBytes, _ := json.Marshal(output)
		run.OutputJSON = string(outputBytes)
	}
	if run.OutputJSON == "" {
		run.OutputJSON = "{}"
	}

	if execErr != nil {
		if errors.Is(execErr, ErrWorkflowRunCanceled) {
			run.Status = WorkflowRunStatusCanceled
		} else {
			run.Status = WorkflowRunStatusFailed
		}
		run.ErrorMessage = execErr.Error()
	} else {
		run.Status = WorkflowRunStatusSuccess
	}
	run.CheckpointJSON = ""
	run.WaitingNodeID = ""
	run.ApprovalRequestID = primitive.NilObjectID
	run.ResumeToken = ""
	run.ResumeTokenHash = ""
	run.ResumeAttemptID = ""
	run.ResumeGrantIssuedAt = time.Time{}
	run.ResumeGrantExpiresAt = time.Time{}
	run.SuspendedAt = time.Time{}

	if err := s.commitWorkflowRunExecutionState(ctx, run); err != nil {
		return nil, err
	}
	s.recordWorkflowExecution(ctx, run, traces, budgetSnapshot)

	return &WorkflowExecutionResult{
		Run:      run,
		Snapshot: snapshot,
		Traces:   traces,
	}, nil
}

func (s *AgentService) suspendWorkflowRun(ctx context.Context, run *repository.WorkflowRunRecord, snapshot map[string]map[string]interface{}, traces []engine.NodeTrace, checkpoint engine.WorkflowCheckpoint, suspension *engine.SuspensionError) (*WorkflowExecutionResult, error) {
	applyWorkflowAccountingSnapshot(run, checkpoint.Budget)
	run.Status = WorkflowRunStatusSuspended
	run.ErrorMessage = ""
	run.FinishedAt = time.Time{}
	run.SuspendedAt = time.Now()
	if suspension != nil {
		run.WaitingNodeID = suspension.Suspension.NodeID
		if approvalID, ok := suspension.Suspension.Metadata["approval_request_id"].(string); ok && approvalID != "" {
			parsed, err := primitive.ObjectIDFromHex(approvalID)
			if err != nil {
				return nil, fmt.Errorf("invalid approval request id in suspension: %w", err)
			}
			run.ApprovalRequestID = parsed
		}
	}
	resumeToken, err := newWorkflowResumeToken()
	if err != nil {
		return nil, err
	}
	run.ResumeToken = ""
	run.ResumeTokenHash = hashWorkflowResumeToken(resumeToken)
	run.ResumeAttemptID = ""
	run.ResumeGrantIssuedAt = time.Time{}
	run.ResumeGrantExpiresAt = time.Time{}
	checkpoint.ResumeToken = ""

	checkpointBytes, _ := json.Marshal(checkpoint)
	run.CheckpointJSON = string(checkpointBytes)

	output := make(map[string]interface{})
	for nodeID, values := range snapshot {
		output[nodeID] = values
	}
	output["blackboard"] = snapshot
	output["traces"] = traces
	output["checkpoint"] = checkpoint
	output["budget"] = checkpoint.Budget
	outputBytes, _ := json.Marshal(output)
	run.OutputJSON = string(outputBytes)
	if run.OutputJSON == "" {
		run.OutputJSON = "{}"
	}

	if err := s.commitWorkflowRunExecutionState(ctx, run); err != nil {
		return nil, err
	}
	s.recordWorkflowExecution(ctx, run, traces, checkpoint.Budget)
	if run.Status == WorkflowRunStatusCanceled {
		resumeToken = ""
	}
	return &WorkflowExecutionResult{
		Run:         run,
		Snapshot:    snapshot,
		Traces:      traces,
		ResumeToken: resumeToken,
	}, nil
}

func newWorkflowResumeToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate workflow resume token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashWorkflowResumeToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *AgentService) validateWorkflowDSL(dslJSON string) error {
	_, err := s.normalizeWorkflowDSLJSON(dslJSON)
	return err
}

func (s *AgentService) normalizeWorkflowDSLJSON(dslJSON string) (string, error) {
	if dslJSON == "" {
		return "", errors.New("dsl_json is required")
	}
	var dslObj dsl.WorkflowDSL
	if err := json.Unmarshal([]byte(dslJSON), &dslObj); err != nil {
		return "", fmt.Errorf("invalid workflow DSL JSON: %w", err)
	}
	if err := validateWorkflowSecurity(&dslObj); err != nil {
		return "", err
	}
	nodeImpls, err := s.buildWorkflowNodes(&dslObj)
	if err != nil {
		return "", err
	}
	if _, err := engine.NewScheduler(&dslObj, nodeImpls); err != nil {
		return "", err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(dslJSON), &payload); err != nil {
		return "", fmt.Errorf("decode workflow DSL payload: %w", err)
	}
	payload["dsl_version"] = dsl.CurrentVersion
	if dslObj.WorkflowVersion < 1 {
		payload["workflow_version"] = int64(1)
	} else {
		payload["workflow_version"] = dslObj.WorkflowVersion
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode normalized workflow DSL: %w", err)
	}
	return string(normalized), nil
}

func parseWorkflowInput(inputJSON string) (map[string]interface{}, error) {
	if inputJSON == "" {
		return map[string]interface{}{}, nil
	}
	var inputs map[string]interface{}
	if err := json.Unmarshal([]byte(inputJSON), &inputs); err != nil {
		return nil, fmt.Errorf("invalid workflow input JSON: %w", err)
	}
	if err := validateWorkflowInputSecurity(inputs); err != nil {
		return nil, err
	}
	return inputs, nil
}

func boolWorkflowInput(inputs map[string]interface{}, key string) bool {
	value, ok := inputs[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true" || typed == "1"
	default:
		return false
	}
}

func (s *AgentService) persistWorkflowDialogue(
	ctx context.Context,
	result *WorkflowExecutionResult,
	dialogue *repository.Dialogue,
	inputs map[string]interface{},
	dslObj *dsl.WorkflowDSL,
	execErr error,
	resultErr error,
) (*WorkflowExecutionResult, error) {
	if resultErr != nil || result == nil || dialogue == nil {
		return result, resultErr
	}

	result.DialogueKey = dialogue.ID.Hex()
	userInput, _ := inputs["user_input"].(string)
	assistantContent := workflowAssistantContent(result.Snapshot, dslObj)
	if result.Run.Status == WorkflowRunStatusSuspended {
		assistantContent = fmt.Sprintf(
			"工作流已在节点 %s 挂起，等待审批或外部回调后继续执行。",
			result.Run.WaitingNodeID,
		)
	} else if execErr != nil {
		assistantContent = fmt.Sprintf("工作流执行失败：%v", execErr)
	}
	if assistantContent == "" {
		assistantContent = "工作流执行完成，但没有产生可展示的文本结果。"
	}
	result.Response = assistantContent

	metadata := map[string]any{
		"workflow_id": result.Run.WorkflowID.Hex(),
		"run_id":      result.Run.ID.Hex(),
		"status":      result.Run.Status,
	}
	if err := s.saveUserAndAssistantMessages(ctx, dialogue.ID, dialogue.UserID, userInput, assistantContent, metadata); err != nil {
		return nil, fmt.Errorf("persist workflow dialogue messages failed: %w", err)
	}
	return result, nil
}

func workflowAssistantContent(snapshot map[string]map[string]interface{}, dslObj *dsl.WorkflowDSL) string {
	preferredKeys := []string{"text", "response", "result", "content", "answer", "final", "summary", "plan"}
	readNode := func(nodeID string) string {
		values := snapshot[nodeID]
		for _, key := range preferredKeys {
			if text, ok := values[key].(string); ok && text != "" {
				return text
			}
		}
		return ""
	}

	if dslObj != nil {
		for i := len(dslObj.Nodes) - 1; i >= 0; i-- {
			if text := readNode(dslObj.Nodes[i].ID); text != "" {
				return text
			}
		}
	}
	for nodeID := range snapshot {
		if text := readNode(nodeID); text != "" {
			return text
		}
	}
	return ""
}

type dynamicWorkflowToolResolver func(nodeID, toolName string) (engine.WorkflowNode, bool, error)

func (s *AgentService) buildWorkflowNodes(dslObj *dsl.WorkflowDSL) ([]engine.WorkflowNode, error) {
	return buildWorkflowNodesWithResolver(dslObj, s.workflowToolExecutor, func(nodeID, toolName string) (engine.WorkflowNode, bool, error) {
		if !externalmcp.IsQualifiedToolName(toolName) {
			return nil, false, nil
		}
		return &externalMCPWorkflowNode{id: nodeID, toolName: toolName, service: s}, true, nil
	})
}

func buildWorkflowNodes(dslObj *dsl.WorkflowDSL, executor *tool.Executor) ([]engine.WorkflowNode, error) {
	return buildWorkflowNodesWithResolver(dslObj, executor, nil)
}

func buildWorkflowNodesWithResolver(
	dslObj *dsl.WorkflowDSL,
	executor *tool.Executor,
	resolver dynamicWorkflowToolResolver,
) ([]engine.WorkflowNode, error) {
	if executor == nil || executor.Registry() == nil {
		return nil, errors.New("workflow tool executor is not configured")
	}
	nodes := make([]engine.WorkflowNode, 0, len(dslObj.Nodes))
	for _, nodeDSL := range dslObj.Nodes {
		if nodeDSL.Compensation != nil {
			if externalmcp.IsQualifiedToolName(nodeDSL.Compensation.ToolName) {
				return nil, fmt.Errorf("node %s external MCP compensation is not supported", nodeDSL.ID)
			}
			if _, ok := executor.Registry().Get(nodeDSL.Compensation.ToolName); !ok {
				return nil, fmt.Errorf("node %s references unregistered compensation tool %s", nodeDSL.ID, nodeDSL.Compensation.ToolName)
			}
		}
		switch nodeDSL.Type {
		case "start":
			nodes = append(nodes, &passthroughWorkflowNode{id: nodeDSL.ID, nodeType: nodeDSL.Type})
		case "end":
			nodes = append(nodes, &passthroughWorkflowNode{id: nodeDSL.ID, nodeType: nodeDSL.Type})
		case "router":
			nodes = append(nodes, &routerWorkflowNode{id: nodeDSL.ID, props: nodeDSL.Properties})
		case "wait":
			nodes = append(nodes, &waitWorkflowNode{id: nodeDSL.ID, props: nodeDSL.Properties})
		case "llm":
			if _, ok := executor.Registry().Get("LLMChat"); !ok {
				return nil, fmt.Errorf("node %s references unregistered tool LLMChat", nodeDSL.ID)
			}
			nodes = append(nodes, tool.NewToolNode(nodeDSL.ID, "LLMChat", executor))
		case "agent":
			toolName, err := resolveWorkflowToolName(nodeDSL.Properties)
			if err != nil {
				return nil, fmt.Errorf("node %s invalid agent config: %w", nodeDSL.ID, err)
			}
			toolNode, err := resolveWorkflowToolNode(nodeDSL.ID, toolName, executor, resolver)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, toolNode)
		case "tool":
			toolName, err := resolveWorkflowToolName(nodeDSL.Properties)
			if err != nil {
				return nil, fmt.Errorf("node %s invalid tool config: %w", nodeDSL.ID, err)
			}
			toolNode, err := resolveWorkflowToolNode(nodeDSL.ID, toolName, executor, resolver)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, toolNode)
		default:
			return nil, fmt.Errorf("unsupported workflow node type %q for node %s", nodeDSL.Type, nodeDSL.ID)
		}
	}
	return nodes, nil
}

func resolveWorkflowToolNode(
	nodeID string,
	toolName string,
	executor *tool.Executor,
	resolver dynamicWorkflowToolResolver,
) (engine.WorkflowNode, error) {
	if _, ok := executor.Registry().Get(toolName); ok {
		return tool.NewToolNode(nodeID, toolName, executor), nil
	}
	if resolver != nil {
		node, resolved, err := resolver(nodeID, toolName)
		if err != nil {
			return nil, fmt.Errorf("node %s dynamic tool %s: %w", nodeID, toolName, err)
		}
		if resolved {
			return node, nil
		}
	}
	return nil, fmt.Errorf("node %s references unregistered tool %s", nodeID, toolName)
}

func resolveWorkflowToolName(rawProps json.RawMessage) (string, error) {
	var props map[string]interface{}
	if len(rawProps) > 0 {
		if err := json.Unmarshal(rawProps, &props); err != nil {
			return "", fmt.Errorf("invalid tool properties JSON: %w", err)
		}
	}
	if toolName, ok := props["tool_name"].(string); ok && toolName != "" {
		return toolName, nil
	}
	if _, ok := props["content"]; ok {
		return "PublishTweet", nil
	}
	if _, ok := props["query"]; ok {
		return "WebSearch", nil
	}
	return "", errors.New("missing tool_name")
}

type passthroughWorkflowNode struct {
	id       string
	nodeType string
}

func (n *passthroughWorkflowNode) ID() string {
	return n.id
}

func (n *passthroughWorkflowNode) Type() string {
	return n.nodeType
}

func (n *passthroughWorkflowNode) Execute(ctx context.Context, state engine.StateView, inputs map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

type routerWorkflowNode struct {
	id    string
	props json.RawMessage
}

func (n *routerWorkflowNode) ID() string {
	return n.id
}

func (n *routerWorkflowNode) Type() string {
	return "router"
}

func (n *routerWorkflowNode) Execute(ctx context.Context, state engine.StateView, inputs map[string]interface{}) (map[string]interface{}, error) {
	var props map[string]interface{}
	if len(n.props) > 0 {
		_ = json.Unmarshal(n.props, &props)
	}
	if branch, ok := props["branch"].(string); ok && branch != "" {
		return map[string]interface{}{"_branch": branch}, nil
	}
	if branch, ok := props["_branch"].(string); ok && branch != "" {
		return map[string]interface{}{"_branch": branch}, nil
	}
	return map[string]interface{}{"_branch": "true"}, nil
}

type waitWorkflowNode struct {
	id    string
	props json.RawMessage
}

func (n *waitWorkflowNode) ID() string {
	return n.id
}

func (n *waitWorkflowNode) Type() string {
	return "wait"
}

func (n *waitWorkflowNode) Execute(ctx context.Context, state engine.StateView, inputs map[string]interface{}) (map[string]interface{}, error) {
	var props map[string]interface{}
	if len(n.props) > 0 {
		_ = json.Unmarshal(n.props, &props)
	}

	reason := "waiting for external callback"
	if value, ok := props["reason"].(string); ok && value != "" {
		reason = value
	}
	resumeToken := n.id
	if value, ok := props["resume_token"].(string); ok && value != "" {
		resumeToken = value
	}

	metadata := make(map[string]interface{})
	for k, v := range props {
		metadata[k] = v
	}
	return nil, engine.NewSuspensionError(n.id, reason, resumeToken, metadata)
}
