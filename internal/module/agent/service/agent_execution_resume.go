package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

var (
	ErrAgentExecutionRunNotResumable = errors.New("agent execution run is not resumable")
	ErrAgentExecutionProfileDrift    = errors.New("agent execution profile changed while the run was suspended")
)

type ResumeAgentExecutionRequest struct {
	UserID           uint64
	RunID            string
	ExpectedRevision int64
	HumanResponse    string
	ApprovalID       string
	ResumeToken      string
}

// AgentExecutionRunView intentionally excludes encrypted Checkpoint material,
// resume attempt IDs and lease internals.
type AgentExecutionRunView struct {
	RunID                 string
	DialogueID            string
	ExecutionProfile      string
	CapabilityIDs         []string
	SkillID               string
	SkillVersion          string
	TaskTemplateID        string
	TaskTemplateRevision  int64
	ExecutionStrategyPlan *agentStrategy.Plan
	Status                string
	Revision              int64
	ResumeSupported       bool
	PendingActionType     string
	PendingActionName     string
	PendingActionID       string
	ApprovalID            string
	ApprovalExpiresAt     time.Time
	StepCount             int
	InputTokens           int
	OutputTokens          int
	TotalTokens           int
	EstimatedCostMicros   int64
	PricingVersion        string
	FailureCode           string
	StartedAt             time.Time
	UpdatedAt             time.Time
	SuspendedAt           time.Time
	FinishedAt            time.Time
}

type AgentResumeGrantView struct {
	Run         *repository.AgentExecutionRun
	ApprovalID  string
	ResumeToken string
	ExpiresAt   time.Time
}

func (s *AgentService) GetAgentExecutionRunView(
	ctx context.Context,
	userID uint64,
	runID string,
) (*AgentExecutionRunView, error) {
	run, err := s.GetAgentExecutionRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	return agentExecutionRunViewAt(run, time.Now()), nil
}

func (s *AgentService) ResumeAgentExecutionRun(
	ctx context.Context,
	request ResumeAgentExecutionRequest,
) (*UnifiedAgentResult, error) {
	if s == nil || !s.recoverableAgentRuns || s.agentExecutionRunStore == nil {
		return nil, ErrAgentExecutionRunStoreUnavailable
	}
	resumableRunner, ok := s.runtimeRunner.(agentRuntime.ResumableAgentRunner)
	if !ok || s.agentCheckpointCipher == nil {
		return nil, ErrAgentExecutionRunNotResumable
	}
	request.RunID = strings.TrimSpace(request.RunID)
	request.HumanResponse = strings.TrimSpace(request.HumanResponse)
	request.ApprovalID = strings.TrimSpace(request.ApprovalID)
	request.ResumeToken = strings.TrimSpace(request.ResumeToken)
	approvalMode := request.ApprovalID != "" || request.ResumeToken != ""
	humanMode := request.HumanResponse != ""
	if request.UserID == 0 || request.RunID == "" || request.ExpectedRevision <= 0 || humanMode == approvalMode {
		return nil, fmt.Errorf("%w: choose exactly one resume mode", ErrInvalidUnifiedAgentRequest)
	}
	if humanMode && len([]byte(request.HumanResponse)) > MaxAgentRunHumanResponseBytes {
		return nil, fmt.Errorf("%w: human_response exceeds the size limit", ErrInvalidUnifiedAgentRequest)
	}
	if approvalMode && (!s.unifiedAgentApprovalRecovery || request.ApprovalID == "") {
		return nil, ErrAgentExecutionRunNotResumable
	}

	current, err := s.agentExecutionRunStore.GetAgentExecutionRun(ctx, request.RunID, request.UserID)
	if err != nil {
		return nil, err
	}
	pendingStatus := repository.AgentExecutionRunAwaitingHuman
	resumeTokenHash := ""
	delegatedApproval := false
	var delegatedCheckpoint *agentRuntime.RunCheckpoint
	if approvalMode {
		pendingStatus = repository.AgentExecutionRunApprovalRequired
		if current.ApprovalRequestID != request.ApprovalID || current.PendingActionType != string(agentRuntime.ActionToolCall) {
			return nil, ErrAgentExecutionRunNotResumable
		}
		delegatedApproval = current.PendingResumeKind == repository.AgentExecutionResumeDelegatedApproval
		if delegatedApproval {
			checkpoint, openErr := s.openAgentRunCheckpoint(current)
			if openErr != nil {
				return nil, openErr
			}
			if validateErr := s.validateWorkflowToolApprovalResume(
				ctx,
				current,
				checkpoint,
				request.ApprovalID,
				request.ResumeToken,
			); validateErr != nil {
				return nil, validateErr
			}
			delegatedCheckpoint = &checkpoint
		} else {
			if request.ResumeToken == "" {
				return nil, ErrAgentExecutionRunNotResumable
			}
			resumeTokenHash = hashWorkflowResumeToken(request.ResumeToken)
		}
	} else {
		humanAction := current.PendingActionType == string(agentRuntime.ActionAskHuman) ||
			(current.PendingActionType == string(agentRuntime.ActionToolCall) &&
				current.PendingResumeKind == repository.AgentExecutionResumeHuman)
		if !humanAction {
			return nil, ErrAgentExecutionRunNotResumable
		}
	}

	attemptID := primitive.NewObjectID().Hex()
	leaseDuration := s.agentResumeLeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = DefaultAgentRunResumeLeaseDuration
	}
	claimed, err := s.agentExecutionRunStore.ClaimAgentExecutionRun(ctx, repository.AgentExecutionRunClaim{
		RunID: request.RunID, UserID: request.UserID, ExpectedRevision: request.ExpectedRevision,
		AttemptID: attemptID, LeaseDuration: leaseDuration, ClaimedAt: time.Now(),
		PendingStatus: pendingStatus, ApprovalRequestID: request.ApprovalID, ResumeTokenHash: resumeTokenHash,
		DelegatedApproval: delegatedApproval,
	})
	if err != nil {
		return nil, err
	}

	var checkpoint agentRuntime.RunCheckpoint
	if delegatedCheckpoint != nil {
		checkpoint = *delegatedCheckpoint
	} else {
		checkpoint, err = s.openAgentRunCheckpoint(claimed)
		if err != nil {
			commitErr := s.failClaimedAgentExecutionRun(ctx, claimed, attemptID, "checkpoint_invalid", err)
			return nil, errors.Join(err, commitErr)
		}
	}
	if approvalMode && (checkpoint.PendingAction.Type != agentRuntime.ActionToolCall || checkpoint.PendingApprovalID != request.ApprovalID) {
		err = errors.New("approval checkpoint binding changed")
		releaseErr := s.releaseClaimedAgentExecutionRun(ctx, claimed, attemptID, err)
		return nil, errors.Join(err, releaseErr)
	}
	if humanMode {
		humanCheckpoint := checkpoint.PendingAction.Type == agentRuntime.ActionAskHuman ||
			(checkpoint.PendingAction.Type == agentRuntime.ActionToolCall &&
				checkpoint.PendingResumeKind == agentRuntime.ResumeKindHumanResponse &&
				checkpoint.PendingToolContinuation != nil)
		if !humanCheckpoint {
			err = errors.New("human checkpoint binding changed")
			releaseErr := s.releaseClaimedAgentExecutionRun(ctx, claimed, attemptID, err)
			return nil, errors.Join(err, releaseErr)
		}
	}
	tools, err := s.resolveAgentResumeTools(ctx, claimed, approvalMode && !delegatedApproval)
	if err != nil {
		releaseErr := s.releaseClaimedAgentExecutionRun(ctx, claimed, attemptID, err)
		return nil, errors.Join(err, releaseErr)
	}

	runtimeRequest := agentRuntime.RunRequest{
		Context: checkpoint.Context,
		Model:   checkpoint.Model,
		Tools:   tools,
	}
	resumeExecutionCtx := ctx
	if claimed.ExecutionProfile == ExecutionProfileRuntimeSkill {
		resolved, resolveErr := s.resolveWorkflowSkill(
			ctx,
			claimed.UserID,
			claimed.SkillID,
			claimed.SkillVersion,
		)
		if resolveErr != nil {
			releaseErr := s.releaseClaimedAgentExecutionRun(ctx, claimed, attemptID, resolveErr)
			return nil, errors.Join(resolveErr, releaseErr)
		}
		resumeExecutionCtx = withWorkflowSkillExecution(
			ctx,
			claimed.UserID,
			resolved.Version,
		)
	}
	result, runtimeErr := resumableRunner.Resume(resumeExecutionCtx, agentRuntime.ResumeRequest{
		Checkpoint:    checkpoint,
		HumanResponse: request.HumanResponse,
		ApprovalID:    request.ApprovalID,
		ResumeToken:   request.ResumeToken,
		Tools:         tools,
	})
	s.recordRuntimeResult(ctx, result, runtimeErr, claimed.AgentProfileID)
	capture := &agentExecutionCapture{
		runID: claimed.ID, dialogueID: claimed.DialogueID, resumeAttemptID: attemptID,
		called: true, request: runtimeRequest, result: result, err: runtimeErr,
	}
	resumeCtx := context.WithValue(
		resumeExecutionCtx,
		agentExecutionCaptureContextKey{},
		capture,
	)
	approvalPending := result.Status == agentRuntime.RunStatusApprovalRequired &&
		agentRuntime.HasErrorCode(runtimeErr, agentRuntime.ErrorApprovalRequired)
	if runtimeErr != nil && !approvalPending {
		stateErr := s.finishUnifiedAgentExecutionRun(resumeCtx, claimed, nil, runtimeErr)
		return nil, errors.Join(runtimeErr, stateErr)
	}

	response, err := runtimeUserVisibleResponse(result)
	if err != nil {
		stateErr := s.finishUnifiedAgentExecutionRun(resumeCtx, claimed, nil, err)
		return nil, errors.Join(err, stateErr)
	}
	dialogueID, err := primitive.ObjectIDFromHex(strings.TrimSpace(claimed.DialogueID))
	if err != nil {
		stateErr := s.finishUnifiedAgentExecutionRun(resumeCtx, claimed, nil, err)
		return nil, errors.Join(errors.New("agent execution dialogue identity is invalid"), stateErr)
	}
	metadata := runtimeResultMetadata(
		result,
		claimed.AgentProfileID,
		claimed.AgentProfileVersion,
		claimed.PromptTemplateVersion,
	)
	metadata["execution_profile"] = claimed.ExecutionProfile
	metadata["capability_ids"] = append([]string(nil), claimed.CapabilityIDs...)
	metadata["resume_count"] = claimed.ResumeCount
	if humanMode {
		err = s.saveUserAndAssistantMessages(resumeCtx, dialogueID, claimed.UserID, request.HumanResponse, response, metadata)
	} else {
		err = s.saveAssistantMessage(resumeCtx, dialogueID, claimed.UserID, response, metadata)
	}
	if err != nil {
		stateErr := s.finishUnifiedAgentExecutionRun(resumeCtx, claimed, nil, err)
		return nil, errors.Join(fmt.Errorf("persist resumed agent conversation: %w", err), stateErr)
	}

	toolActivities, citations := collectRuntimeResultEvidence(result)
	chatResult := ChatResult{
		DialogueID: claimed.DialogueID,
		RunID:      claimed.ID,
		RunStatus:  string(result.Status),
		Response:   response,
	}
	publishableDraft := result.Status == agentRuntime.RunStatusCompleted &&
		containsCapability(claimed.CapabilityIDs, CapabilityContentDraft)
	unified := &UnifiedAgentResult{
		ChatResult:       chatResult,
		ExecutionProfile: claimed.ExecutionProfile,
		CapabilityIDs:    append([]string(nil), claimed.CapabilityIDs...),
		RunStatus:        string(result.Status),
		PublishableDraft: publishableDraft,
		ToolActivities:   toolActivities,
		Citations:        citations,
		Artifacts:        buildUnifiedAgentArtifacts(&chatResult, publishableDraft),
		ApprovalState: AgentApprovalState{
			Status: AgentApprovalStatusNotRequired, RunID: claimed.ID,
		},
		SelectedSkillID:              claimed.SkillID,
		SelectedSkillVersion:         claimed.SkillVersion,
		SelectedTaskTemplateID:       claimed.TaskTemplateID,
		SelectedTaskTemplateRevision: claimed.TaskTemplateRevision,
	}
	if claimed.ExecutionStrategyPlan != nil {
		unified.ExecutionStrategyPlan = agentStrategy.ClonePlan(*claimed.ExecutionStrategyPlan)
	}
	if err := s.finishUnifiedAgentExecutionRun(resumeCtx, claimed, unified, nil); err != nil {
		return nil, err
	}
	unified.RunStatus = string(claimed.Status)
	unified.ApprovalState.Revision = claimed.Revision
	unified.ApprovalState.ResumeSupported = claimed.ResumeSupported
	if claimed.Status == repository.AgentExecutionRunAwaitingHuman {
		unified.ApprovalState.Status = AgentApprovalStatusInputRequired
		unified.ApprovalState.Action = string(agentRuntime.ActionAskHuman)
		unified.PublishableDraft = false
		unified.Artifacts = nil
	}
	if claimed.Status == repository.AgentExecutionRunApprovalRequired {
		unified.ApprovalState.Status = AgentApprovalStatusInputRequired
		unified.ApprovalState.ApprovalID = claimed.ApprovalRequestID
		unified.ApprovalState.Action = string(agentRuntime.ActionToolCall)
		unified.ApprovalState.ExpiresAt = claimed.ApprovalExpiresAt.Unix()
		unified.PublishableDraft = false
		unified.Artifacts = nil
	}
	return unified, nil
}

func (s *AgentService) resolveAgentResumeTools(
	ctx context.Context,
	run *repository.AgentExecutionRun,
	approvalResume bool,
) ([]agentRuntime.ToolDefinition, error) {
	if run == nil {
		return nil, ErrAgentExecutionRunNotResumable
	}
	if strings.TrimSpace(run.AgentProfileID) == "" {
		if s.runtimeTools == nil {
			return nil, errors.New("agent runtime tool catalog is unavailable")
		}
		available, err := s.runtimeTools.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list current runtime tools: %w", err)
		}
		if approvalResume {
			return nil, ErrAgentExecutionRunNotResumable
		}
		return readOnlyResumeTools(available), nil
	}
	selected, err := s.resolveAgentProfile(ctx, run.AgentProfileID, run.UserID)
	if err != nil {
		return nil, fmt.Errorf("resolve suspended agent profile: %w", err)
	}
	if selected.Version != run.AgentProfileVersion || selected.Prompt.ID != run.PromptTemplateID ||
		selected.Prompt.Version != run.PromptTemplateVersion {
		return nil, ErrAgentExecutionProfileDrift
	}
	var available []agentRuntime.ToolDefinition
	if run.ExecutionProfile == ExecutionProfileRuntimeWorkflow {
		available, err = s.listPublishedWorkflowRuntimeTools(ctx, run.UserID)
		if err != nil {
			return nil, fmt.Errorf("list current published workflow tools: %w", err)
		}
		selected.AllowedTools = make([]string, 0, len(available))
		for _, tool := range available {
			selected.AllowedTools = append(selected.AllowedTools, tool.Name)
		}
	} else if run.ExecutionProfile == ExecutionProfileRuntimeSkill {
		resolved, resolveErr := s.resolveWorkflowSkill(
			ctx,
			run.UserID,
			run.SkillID,
			run.SkillVersion,
		)
		if resolveErr != nil {
			return nil, resolveErr
		}
		available = []agentRuntime.ToolDefinition{
			workflowSkillToolDefinition(resolved.Publication),
		}
		selected = cloneServiceProfile(resolved.Profile)
		selected.AllowedTools = append([]string(nil), resolved.Version.AllowedTools...)
	} else if run.ExecutionProfile == ExecutionProfileRuntimeExternalMCP {
		manager, managerErr := s.externalMCP()
		if managerErr != nil {
			return nil, managerErr
		}
		var executable []externalmcp.ExecutableTool
		var listErr error
		if approvalResume || run.AgentProfileID == profileUnifiedExternalMCPGoverned {
			executable, listErr = manager.ListGovernedTools(ctx, run.UserID)
		} else {
			executable, listErr = manager.ListExecutableTools(ctx, run.UserID)
		}
		if listErr != nil {
			return nil, fmt.Errorf("list current external MCP tools: %w", listErr)
		}
		available = externalMCPRuntimeTools(executable)
		if len(selected.AllowedTools) == 0 {
			selected.AllowedTools = make([]string, 0, len(available))
			for _, tool := range available {
				selected.AllowedTools = append(selected.AllowedTools, tool.Name)
			}
		}
	} else {
		if len(selected.AllowedTools) == 0 {
			return nil, nil
		}
		if s.runtimeTools == nil {
			return nil, errors.New("agent runtime tool catalog is unavailable")
		}
		available, err = s.runtimeTools.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list current runtime tools: %w", err)
		}
	}
	filtered := selected.FilterTools(available)
	if !approvalResume {
		return readOnlyResumeTools(filtered), nil
	}
	if err := s.validateApprovedRuntimeAction(ctx, run, filtered); err != nil {
		return nil, err
	}
	return filtered, nil
}

func readOnlyResumeTools(tools []agentRuntime.ToolDefinition) []agentRuntime.ToolDefinition {
	filtered := make([]agentRuntime.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if tool.Category == agentRuntime.ToolCategoryRead && !tool.ApprovalRequired() {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func (s *AgentService) validateApprovedRuntimeAction(
	ctx context.Context,
	run *repository.AgentExecutionRun,
	tools []agentRuntime.ToolDefinition,
) error {
	if run == nil || run.PendingActionType != string(agentRuntime.ActionToolCall) ||
		strings.TrimSpace(run.ApprovalRequestID) == "" {
		return ErrAgentExecutionRunNotResumable
	}
	approvalOID, err := primitive.ObjectIDFromHex(run.ApprovalRequestID)
	if err != nil {
		return ErrAgentExecutionRunNotResumable
	}
	approvalRepo, err := s.toolApprovalRepository()
	if err != nil {
		return err
	}
	approval, err := approvalRepo.GetToolApproval(ctx, approvalOID, run.UserID)
	if err != nil {
		return err
	}
	now := time.Now()
	if approval.Status != repository.ToolApprovalStatusApproved || approval.Source != string(workflowTool.SourceRuntime) ||
		approval.RunID != run.ID || approval.StepID != run.PendingActionID || approval.ToolName != run.PendingActionName ||
		approval.InputDigest != run.ApprovalInputDigest || approval.IdempotencyKey != run.ApprovalIdempotencyKey ||
		approval.ExpiresAt.IsZero() || !approval.ExpiresAt.After(now) ||
		(!run.ApprovalExpiresAt.IsZero() && !run.ApprovalExpiresAt.After(now)) {
		return ErrAgentExecutionRunNotResumable
	}
	for _, tool := range tools {
		if tool.Name != run.PendingActionName {
			continue
		}
		if !tool.ApprovalRequired() || string(tool.Category) != approval.Category {
			return ErrAgentExecutionProfileDrift
		}
		return nil
	}
	return ErrAgentExecutionProfileDrift
}

func (s *AgentService) IssueAgentResumeGrant(
	ctx context.Context,
	userID uint64,
	approvalID string,
	expectedRunRevision int64,
) (*AgentResumeGrantView, error) {
	if s == nil || !s.recoverableAgentRuns || !s.unifiedAgentApprovalRecovery || s.agentExecutionRunStore == nil {
		return nil, ErrAgentExecutionRunNotResumable
	}
	if userID == 0 || expectedRunRevision <= 0 {
		return nil, ErrInvalidUnifiedAgentRequest
	}
	approvalOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(approvalID))
	if err != nil {
		return nil, fmt.Errorf("invalid approval_id: %w", err)
	}
	approvalRepo, err := s.toolApprovalRepository()
	if err != nil {
		return nil, err
	}
	approval, err := approvalRepo.GetToolApproval(ctx, approvalOID, userID)
	if err != nil {
		return nil, err
	}
	if approval.Status != repository.ToolApprovalStatusApproved || approval.Source != string(workflowTool.SourceRuntime) {
		return nil, ErrAgentExecutionRunNotResumable
	}
	run, err := s.agentExecutionRunStore.GetAgentExecutionRun(ctx, approval.RunID, userID)
	if err != nil {
		return nil, err
	}
	if run.Revision != expectedRunRevision || run.ApprovalRequestID != approvalID ||
		agentExecutionRunViewAt(run, time.Now()).Status != string(repository.AgentExecutionRunApprovalRequired) {
		return nil, repository.ErrAgentExecutionRunConflict
	}
	checkpoint, err := s.openAgentRunCheckpoint(run)
	if err != nil {
		return nil, err
	}
	if checkpoint.PendingAction.Type != agentRuntime.ActionToolCall || checkpoint.PendingApprovalID != approvalID ||
		checkpoint.PendingAction.ID != run.PendingActionID || checkpoint.PendingAction.Name != run.PendingActionName {
		return nil, ErrAgentExecutionRunNotResumable
	}
	if _, err := s.resolveAgentResumeTools(ctx, run, true); err != nil {
		return nil, err
	}
	resumeToken, err := newWorkflowResumeToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	expiresAt := now.Add(defaultWorkflowResumeGrantTTL)
	if approval.ExpiresAt.Before(expiresAt) {
		expiresAt = approval.ExpiresAt
	}
	if !expiresAt.After(now) {
		return nil, ErrAgentExecutionRunNotResumable
	}
	grantStore, ok := s.agentExecutionRunStore.(repository.AgentExecutionApprovalRunStore)
	if !ok {
		return nil, ErrAgentExecutionRunStoreUnavailable
	}
	updated, err := grantStore.IssueAgentExecutionResumeGrant(ctx, repository.AgentExecutionResumeGrant{
		RunID: run.ID, UserID: userID, ApprovalRequestID: approvalID,
		ExpectedRevision: expectedRunRevision, TokenHash: hashWorkflowResumeToken(resumeToken),
		IssuedAt: now, ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, err
	}
	return &AgentResumeGrantView{Run: updated, ApprovalID: approvalID, ResumeToken: resumeToken, ExpiresAt: expiresAt}, nil
}

func (s *AgentService) failClaimedAgentExecutionRun(
	ctx context.Context,
	run *repository.AgentExecutionRun,
	attemptID string,
	failureCode string,
	failure error,
) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), agentExecutionStateCommitTimeout)
	defer cancel()
	updated, err := s.agentExecutionRunStore.CommitAgentExecutionRun(commitCtx, repository.AgentExecutionRunCommit{
		RunID: run.ID, UserID: run.UserID, ExpectedRevision: run.Revision,
		ExpectedResumeAttemptID: attemptID,
		DialogueID:              run.DialogueID, Status: repository.AgentExecutionRunFailed,
		Mode: run.Mode, Model: run.Model, AgentProfileID: run.AgentProfileID,
		AgentProfileVersion: run.AgentProfileVersion, PromptTemplateID: run.PromptTemplateID,
		PromptTemplateVersion: run.PromptTemplateVersion,
		FailureCode:           failureCode, FailureDigest: agentExecutionErrorDigest(run.ID, failure),
		StepCount: run.StepCount, InputTokens: run.InputTokens, OutputTokens: run.OutputTokens,
		TotalTokens: run.TotalTokens, EstimatedCostMicros: run.EstimatedCostMicros,
		UsageEstimated: run.UsageEstimated, CostEstimated: run.CostEstimated,
		PricingVersion: run.PricingVersion, MaxSteps: run.MaxSteps,
		MaxTotalTokens: run.MaxTotalTokens, MaxEstimatedCostMicros: run.MaxEstimatedCostMicros,
		AccountingVersion: run.AccountingVersion, UpdatedAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("commit failed agent execution resume: %w", err)
	}
	*run = *updated
	return nil
}

func (s *AgentService) releaseClaimedAgentExecutionRun(
	ctx context.Context,
	run *repository.AgentExecutionRun,
	attemptID string,
	cause error,
) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), agentExecutionStateCommitTimeout)
	defer cancel()
	status := repository.AgentExecutionRunAwaitingHuman
	if run.PendingResumeKind == repository.AgentExecutionResumeApproval ||
		run.PendingResumeKind == repository.AgentExecutionResumeDelegatedApproval ||
		(run.PendingResumeKind == "" && run.PendingActionType == string(agentRuntime.ActionToolCall)) {
		status = repository.AgentExecutionRunApprovalRequired
	}
	updated, err := s.agentExecutionRunStore.CommitAgentExecutionRun(commitCtx, repository.AgentExecutionRunCommit{
		RunID: run.ID, UserID: run.UserID, ExpectedRevision: run.Revision,
		ExpectedResumeAttemptID: attemptID,
		DialogueID:              run.DialogueID, Status: status,
		Mode: run.Mode, Model: run.Model, AgentProfileID: run.AgentProfileID,
		AgentProfileVersion: run.AgentProfileVersion, PromptTemplateID: run.PromptTemplateID,
		PromptTemplateVersion: run.PromptTemplateVersion,
		PendingActionType:     run.PendingActionType, PendingActionName: run.PendingActionName,
		PendingActionID: run.PendingActionID, PendingResumeKind: run.PendingResumeKind,
		ApprovalRequestID:   run.ApprovalRequestID,
		ApprovalInputDigest: run.ApprovalInputDigest, ApprovalIdempotencyKey: run.ApprovalIdempotencyKey,
		ApprovalExpiresAt: run.ApprovalExpiresAt,
		StepCount:         run.StepCount, InputTokens: run.InputTokens, OutputTokens: run.OutputTokens,
		TotalTokens: run.TotalTokens, EstimatedCostMicros: run.EstimatedCostMicros,
		UsageEstimated: run.UsageEstimated, CostEstimated: run.CostEstimated,
		PricingVersion: run.PricingVersion, MaxSteps: run.MaxSteps,
		MaxTotalTokens: run.MaxTotalTokens, MaxEstimatedCostMicros: run.MaxEstimatedCostMicros,
		AccountingVersion: run.AccountingVersion, ResumeSupported: true,
		CheckpointVersion: run.CheckpointVersion, CheckpointKeyID: run.CheckpointKeyID,
		CheckpointNonce: run.CheckpointNonce, CheckpointCiphertext: run.CheckpointCiphertext,
		CheckpointDigest: run.CheckpointDigest, CheckpointSizeBytes: run.CheckpointSizeBytes,
		FailureCode:   "resume_precondition_failed",
		FailureDigest: agentExecutionErrorDigest(run.ID, cause), UpdatedAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("release agent execution resume claim: %w", err)
	}
	*run = *updated
	return nil
}

func agentExecutionRunViewAt(run *repository.AgentExecutionRun, now time.Time) *AgentExecutionRunView {
	if run == nil {
		return nil
	}
	status := run.Status
	resumeSupported := run.ResumeSupported && isSuspendedAgentExecutionStatusView(status)
	if run.ResumeSupported && status == repository.AgentExecutionRunRunning &&
		strings.TrimSpace(run.ResumeAttemptID) != "" && !run.ResumeLeaseUntil.IsZero() &&
		!run.ResumeLeaseUntil.After(now) {
		status = repository.AgentExecutionRunAwaitingHuman
		if run.PendingResumeKind == repository.AgentExecutionResumeApproval ||
			run.PendingResumeKind == repository.AgentExecutionResumeDelegatedApproval ||
			(run.PendingResumeKind == "" && run.PendingActionType == string(agentRuntime.ActionToolCall)) {
			status = repository.AgentExecutionRunApprovalRequired
		}
		resumeSupported = true
	}
	pendingActionType := run.PendingActionType
	if status == repository.AgentExecutionRunAwaitingHuman &&
		run.PendingResumeKind == repository.AgentExecutionResumeHuman {
		pendingActionType = string(agentRuntime.ActionAskHuman)
	}
	view := &AgentExecutionRunView{
		RunID: run.ID, DialogueID: run.DialogueID, ExecutionProfile: run.ExecutionProfile,
		CapabilityIDs: append([]string(nil), run.CapabilityIDs...), Status: string(status),
		SkillID: run.SkillID, SkillVersion: run.SkillVersion,
		TaskTemplateID: run.TaskTemplateID, TaskTemplateRevision: run.TaskTemplateRevision,
		Revision: run.Revision, ResumeSupported: resumeSupported,
		PendingActionType: pendingActionType, PendingActionName: run.PendingActionName,
		PendingActionID: run.PendingActionID, ApprovalID: run.ApprovalRequestID,
		ApprovalExpiresAt: run.ApprovalExpiresAt,
		StepCount:         run.StepCount, InputTokens: run.InputTokens, OutputTokens: run.OutputTokens,
		TotalTokens: run.TotalTokens, EstimatedCostMicros: run.EstimatedCostMicros,
		PricingVersion: run.PricingVersion, FailureCode: run.FailureCode,
		StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt, SuspendedAt: run.SuspendedAt,
		FinishedAt: run.FinishedAt,
	}
	if run.ExecutionStrategyPlan != nil {
		cloned := agentStrategy.ClonePlan(*run.ExecutionStrategyPlan)
		view.ExecutionStrategyPlan = &cloned
	}
	return view
}

func isSuspendedAgentExecutionStatusView(status repository.AgentExecutionRunStatus) bool {
	return status == repository.AgentExecutionRunAwaitingHuman || status == repository.AgentExecutionRunApprovalRequired
}
