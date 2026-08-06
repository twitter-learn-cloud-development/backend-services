package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/engine"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	workflowRuntimeToolPrefix       = "workflow_"
	workflowToolResultSchema        = "workflow.run.v1"
	workflowToolContinuationVersion = "workflow.tool.continuation.v1"
	workflowToolWaitModeHumanInput  = "human_input"
	defaultWorkflowToolCatalogLimit = 20
	defaultWorkflowToolTimeout      = 60 * time.Second
	maxWorkflowToolDescriptionRunes = 512
	maxWorkflowToolWaitPromptRunes  = 512
	maxWorkflowToolInputSchemaBytes = 16 << 10
	defaultWorkflowToolInputSchema  = `{"type":"object","properties":{"user_input":{"type":"string","minLength":1,"maxLength":12000}},"required":["user_input"],"additionalProperties":false}`
)

var (
	ErrWorkflowAsToolDisabled = errors.New("workflow-as-tool runtime is disabled")
	ErrWorkflowNotPublishable = errors.New("workflow is not eligible for tool publication")
)

var reservedWorkflowToolInputs = map[string]struct{}{
	"user_id":              {},
	"run_id":               {},
	"action_id":            {},
	"parent_run_id":        {},
	"parent_action_id":     {},
	"invocation_source":    {},
	"dialogue_key":         {},
	"persist_dialogue":     {},
	"workflow_id":          {},
	"workflow_revision_id": {},
}

type PublishWorkflowToolInput struct {
	WorkflowRevisionID string
	Description        string
	InputSchemaJSON    string
	ExpectedRevision   int64
}

type workflowToolContinuation struct {
	Version                string `json:"version"`
	ResumeKind             string `json:"resume_kind"`
	ToolName               string `json:"tool_name"`
	PublicationRevision    int64  `json:"publication_revision"`
	WorkflowID             string `json:"workflow_id"`
	WorkflowRevisionID     string `json:"workflow_revision_id"`
	WorkflowRevisionNumber int64  `json:"workflow_revision_number"`
	WorkflowDSLHash        string `json:"workflow_dsl_hash"`
	WorkflowRunID          string `json:"workflow_run_id"`
	ParentRunID            string `json:"parent_run_id"`
	ParentActionID         string `json:"parent_action_id"`
	ApprovalRequestID      string `json:"approval_request_id,omitempty"`
	WorkflowResumeToken    string `json:"workflow_resume_token,omitempty"`
}

func (s *AgentService) PublishWorkflowTool(
	ctx context.Context,
	userID uint64,
	workflowID string,
	input PublishWorkflowToolInput,
) (*repository.WorkflowToolPublication, error) {
	if s == nil || !s.workflowAsToolEnabled {
		return nil, ErrWorkflowAsToolDisabled
	}
	store, err := s.workflowToolPublications()
	if err != nil {
		return nil, err
	}
	if userID == 0 {
		return nil, errors.New("workflow tool publication user is required")
	}
	workflowOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(workflowID))
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_id: %w", err)
	}
	workflow, err := s.repo.GetWorkflow(ctx, workflowOID, userID)
	if err != nil {
		return nil, err
	}
	revision, err := s.resolveRequestedWorkflowRevision(
		ctx,
		workflow,
		strings.TrimSpace(input.WorkflowRevisionID),
	)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow tool revision: %w", err)
	}
	if _, err := s.validateWorkflowToolRevision(ctx, userID, workflow, revision); err != nil {
		return nil, err
	}

	description := normalizeWorkflowToolDescription(input.Description)
	if description == "" {
		description = normalizeWorkflowToolDescription(
			fmt.Sprintf("Run the published governed workflow %q.", workflow.Name),
		)
	}
	if description == "" {
		return nil, errors.New("workflow tool description is required")
	}
	inputSchema := strings.TrimSpace(input.InputSchemaJSON)
	if inputSchema == "" {
		inputSchema = defaultWorkflowToolInputSchema
	}
	if err := validateWorkflowToolInputSchema(inputSchema); err != nil {
		return nil, err
	}

	publication := &repository.WorkflowToolPublication{
		UserID:                 userID,
		WorkflowID:             workflow.ID,
		WorkflowRevisionID:     revision.ID,
		WorkflowRevisionNumber: revision.RevisionNumber,
		WorkflowDSLHash:        revision.DSLHash,
		ToolName:               workflowRuntimeToolName(workflow.ID),
		DisplayName:            workflowToolBoundedText(workflow.Name, 120),
		Description:            description,
		InputSchemaJSON:        inputSchema,
		Status:                 repository.WorkflowToolPublicationActive,
	}
	if publication.DisplayName == "" {
		publication.DisplayName = publication.ToolName
	}
	if err := store.SaveWorkflowToolPublication(ctx, publication, input.ExpectedRevision); err != nil {
		return nil, err
	}
	return publication, nil
}

func (s *AgentService) GetWorkflowToolPublication(
	ctx context.Context,
	userID uint64,
	workflowID string,
) (*repository.WorkflowToolPublication, error) {
	store, err := s.workflowToolPublications()
	if err != nil {
		return nil, err
	}
	workflowOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(workflowID))
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_id: %w", err)
	}
	return store.GetWorkflowToolPublication(ctx, userID, workflowOID)
}

func (s *AgentService) UnpublishWorkflowTool(
	ctx context.Context,
	userID uint64,
	workflowID string,
	expectedRevision int64,
) (*repository.WorkflowToolPublication, error) {
	if expectedRevision < 1 {
		return nil, errors.New("expected_revision is required when unpublishing a workflow tool")
	}
	store, err := s.workflowToolPublications()
	if err != nil {
		return nil, err
	}
	workflowOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(workflowID))
	if err != nil {
		return nil, fmt.Errorf("invalid workflow_id: %w", err)
	}
	publication, err := store.GetWorkflowToolPublication(ctx, userID, workflowOID)
	if err != nil {
		return nil, err
	}
	publication.Status = repository.WorkflowToolPublicationDisabled
	if err := store.SaveWorkflowToolPublication(ctx, publication, expectedRevision); err != nil {
		return nil, err
	}
	return publication, nil
}

func (s *AgentService) listPublishedWorkflowRuntimeTools(
	ctx context.Context,
	userID uint64,
) ([]agentRuntime.ToolDefinition, error) {
	if s == nil || !s.workflowAsToolEnabled {
		return nil, ErrWorkflowAsToolDisabled
	}
	store, err := s.workflowToolPublications()
	if err != nil {
		return nil, err
	}
	publications, err := store.ListActiveWorkflowToolPublications(
		ctx,
		userID,
		s.workflowToolCatalogLimit,
	)
	if err != nil {
		return nil, err
	}
	definitions := make([]agentRuntime.ToolDefinition, 0, len(publications))
	for _, publication := range publications {
		if _, _, err := s.validateWorkflowToolPublicationBinding(ctx, userID, publication); err != nil {
			slog.WarnContext(
				ctx,
				"published workflow tool excluded from runtime catalog",
				"tool", workflowToolPublicationName(publication),
				"error", err,
			)
			continue
		}
		definitions = append(definitions, workflowSkillToolDefinition(publication))
	}
	return definitions, nil
}

func (e *mcpRuntimeToolExecutor) executeWorkflowTool(
	ctx context.Context,
	call agentRuntime.ToolCall,
	arguments map[string]interface{},
) (agentRuntime.ToolResult, error) {
	if e == nil || e.service == nil || !e.service.workflowAsToolEnabled {
		return agentRuntime.ToolResult{}, ErrWorkflowAsToolDisabled
	}
	if e.service.workflowToolExecutor == nil {
		return agentRuntime.ToolResult{}, errors.New("workflow tool executor is not configured")
	}
	store, err := e.service.workflowToolPublications()
	if err != nil {
		return agentRuntime.ToolResult{}, err
	}
	publication, err := store.GetWorkflowToolPublicationByName(
		ctx,
		call.RunContext.UserID,
		call.Name,
	)
	if err != nil {
		return agentRuntime.ToolResult{}, err
	}
	if err := validateWorkflowSkillExecutionBinding(
		ctx,
		call.RunContext.UserID,
		publication,
	); err != nil {
		return agentRuntime.ToolResult{}, err
	}
	revision, workflowDSL, err := e.service.validateWorkflowToolPublicationBinding(
		ctx,
		call.RunContext.UserID,
		publication,
	)
	if err != nil {
		return agentRuntime.ToolResult{}, err
	}

	spec := workflowToolPublicationSpec(publication, e.service.workflowToolTimeout)
	outputs, err := e.service.workflowToolExecutor.ExecuteAdHoc(
		ctx,
		workflowTool.ExecutionRequest{
			ToolName:       call.Name,
			Inputs:         cloneWorkflowToolInputs(arguments),
			Identity:       workflowTool.CallerIdentity{UserID: call.RunContext.UserID},
			RunID:          call.RunContext.RunID,
			StepID:         call.ActionID,
			Source:         workflowTool.SourceRuntime,
			IdempotencyKey: toolIdempotencyKey(call.RunContext.RunID, call.ActionID, call.Name),
		},
		spec,
		workflowTool.HandlerFunc(func(
			handlerCtx context.Context,
			governedInputs map[string]interface{},
		) (map[string]interface{}, error) {
			childInputs := cloneWorkflowToolInputs(governedInputs)
			for reserved := range reservedWorkflowToolInputs {
				delete(childInputs, reserved)
			}
			inputJSON, marshalErr := json.Marshal(childInputs)
			if marshalErr != nil {
				return nil, fmt.Errorf("encode workflow tool input: %w", marshalErr)
			}
			result, runErr := e.service.runWorkflowRevision(
				handlerCtx,
				call.RunContext.UserID,
				publication.WorkflowID.Hex(),
				publication.WorkflowRevisionID.Hex(),
				string(inputJSON),
				workflowInvocation{
					Source:         string(workflowTool.SourceRuntime),
					ParentRunID:    call.RunContext.RunID,
					ParentActionID: call.ActionID,
					NestedRuntime:  true,
				},
			)
			if runErr != nil {
				return nil, runErr
			}
			if result != nil && result.Run != nil &&
				result.Run.Status == WorkflowRunStatusSuspended {
				return nil, newWorkflowRuntimeToolSuspension(call, publication, result)
			}
			if result == nil || result.Run == nil || result.Run.Status != WorkflowRunStatusSuccess {
				return nil, fmt.Errorf("published workflow did not complete successfully")
			}
			return workflowRuntimeToolOutputs(publication, revision, workflowDSL, result)
		}),
	)
	if err != nil {
		var suspended *agentRuntime.ToolSuspensionError
		if errors.As(err, &suspended) {
			return agentRuntime.ToolResult{}, suspended
		}
		return agentRuntime.ToolResult{}, fmt.Errorf("execute published workflow tool %s: %w", call.Name, err)
	}
	content, _ := outputs["content"].(string)
	structured, err := encodeMCPStructuredContent(outputs["structured_content"])
	if err != nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("encode published workflow result: %w", err)
	}
	return agentRuntime.ToolResult{
		Content:           content,
		StructuredContent: structured,
	}, nil
}

func (e *mcpRuntimeToolExecutor) ResumeTool(
	ctx context.Context,
	request agentRuntime.ToolResumeRequest,
) (agentRuntime.ToolResult, error) {
	if e == nil || e.service == nil || !e.service.workflowAsToolEnabled {
		return agentRuntime.ToolResult{}, ErrWorkflowAsToolDisabled
	}
	if !isWorkflowRuntimeToolName(request.Call.Name) {
		return agentRuntime.ToolResult{}, fmt.Errorf("tool %s does not support continuation", request.Call.Name)
	}
	if e.service.workflowToolExecutor == nil {
		return agentRuntime.ToolResult{}, errors.New("workflow tool executor is not configured")
	}
	var arguments map[string]interface{}
	if err := json.Unmarshal(request.Call.Arguments, &arguments); err != nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("decode workflow tool continuation arguments: %w", err)
	}
	var continuation workflowToolContinuation
	if request.Continuation.Version != workflowToolContinuationVersion ||
		json.Unmarshal(request.Continuation.State, &continuation) != nil {
		return agentRuntime.ToolResult{}, errors.New("workflow tool continuation is invalid")
	}
	if continuation.ResumeKind != string(request.Continuation.ResumeKind) ||
		continuation.ApprovalRequestID != strings.TrimSpace(request.Continuation.ApprovalID) {
		return agentRuntime.ToolResult{}, errors.New("workflow tool continuation envelope binding changed")
	}

	store, err := e.service.workflowToolPublications()
	if err != nil {
		return agentRuntime.ToolResult{}, err
	}
	publication, err := store.GetWorkflowToolPublicationByName(
		ctx,
		request.Call.RunContext.UserID,
		request.Call.Name,
	)
	if err != nil {
		return agentRuntime.ToolResult{}, err
	}
	if err := validateWorkflowSkillExecutionBinding(
		ctx,
		request.Call.RunContext.UserID,
		publication,
	); err != nil {
		return agentRuntime.ToolResult{}, err
	}
	revision, workflowDSL, err := e.service.validateWorkflowToolPublicationBinding(
		ctx,
		request.Call.RunContext.UserID,
		publication,
	)
	if err != nil {
		return agentRuntime.ToolResult{}, err
	}
	childRun, err := e.service.validateWorkflowToolContinuation(
		ctx,
		request.Call,
		publication,
		revision,
		continuation,
	)
	if err != nil {
		return agentRuntime.ToolResult{}, err
	}

	spec := workflowToolPublicationSpec(publication, e.service.workflowToolTimeout)
	outputs, err := e.service.workflowToolExecutor.ExecuteAdHoc(
		ctx,
		workflowTool.ExecutionRequest{
			ToolName:       request.Call.Name,
			Inputs:         cloneWorkflowToolInputs(arguments),
			Identity:       workflowTool.CallerIdentity{UserID: request.Call.RunContext.UserID},
			RunID:          request.Call.RunContext.RunID,
			StepID:         request.Call.ActionID,
			Source:         workflowTool.SourceRuntime,
			IdempotencyKey: toolIdempotencyKey(request.Call.RunContext.RunID, request.Call.ActionID, request.Call.Name),
		},
		spec,
		workflowTool.HandlerFunc(func(
			handlerCtx context.Context,
			_ map[string]interface{},
		) (map[string]interface{}, error) {
			result := &WorkflowExecutionResult{
				Run:      childRun,
				Snapshot: workflowSnapshotFromRun(childRun),
			}
			if childRun.Status == WorkflowRunStatusSuspended {
				resumeInputs := map[string]interface{}{}
				approvalID := ""
				resumeToken := continuation.WorkflowResumeToken
				switch continuation.ResumeKind {
				case "", string(agentRuntime.ResumeKindHumanResponse):
					resumeInputs["human_response"] = strings.TrimSpace(request.HumanResponse)
				case string(agentRuntime.ResumeKindDelegatedToolApproval):
					approvalID = strings.TrimSpace(request.ApprovalID)
					resumeToken = strings.TrimSpace(request.ResumeToken)
				default:
					return nil, errors.New("workflow tool continuation resume kind is invalid")
				}
				resumeInput, marshalErr := json.Marshal(resumeInputs)
				if marshalErr != nil {
					return nil, fmt.Errorf("encode workflow continuation input: %w", marshalErr)
				}
				result, err = e.service.ResumeWorkflowRun(
					handlerCtx,
					request.Call.RunContext.UserID,
					childRun.ID.Hex(),
					approvalID,
					resumeToken,
					string(resumeInput),
				)
				if err != nil {
					return nil, err
				}
			}
			if result != nil && result.Run != nil &&
				result.Run.Status == WorkflowRunStatusSuspended {
				return nil, newWorkflowRuntimeToolSuspension(
					request.Call,
					publication,
					result,
				)
			}
			if result == nil || result.Run == nil || result.Run.Status != WorkflowRunStatusSuccess {
				return nil, fmt.Errorf("published workflow continuation did not complete successfully")
			}
			return workflowRuntimeToolOutputs(publication, revision, workflowDSL, result)
		}),
	)
	if err != nil {
		var suspended *agentRuntime.ToolSuspensionError
		if errors.As(err, &suspended) {
			return agentRuntime.ToolResult{}, suspended
		}
		return agentRuntime.ToolResult{}, fmt.Errorf(
			"resume published workflow tool %s: %w",
			request.Call.Name,
			err,
		)
	}
	content, _ := outputs["content"].(string)
	structured, err := encodeMCPStructuredContent(outputs["structured_content"])
	if err != nil {
		return agentRuntime.ToolResult{}, fmt.Errorf("encode resumed workflow result: %w", err)
	}
	return agentRuntime.ToolResult{
		Content:           content,
		StructuredContent: structured,
	}, nil
}

func (s *AgentService) validateWorkflowToolContinuation(
	ctx context.Context,
	call agentRuntime.ToolCall,
	publication *repository.WorkflowToolPublication,
	revision *repository.WorkflowRevision,
	continuation workflowToolContinuation,
) (*repository.WorkflowRunRecord, error) {
	if continuation.Version != workflowToolContinuationVersion ||
		continuation.ToolName != call.Name ||
		continuation.PublicationRevision != publication.Revision ||
		continuation.WorkflowID != publication.WorkflowID.Hex() ||
		continuation.WorkflowRevisionID != revision.ID.Hex() ||
		continuation.WorkflowRevisionNumber != revision.RevisionNumber ||
		continuation.WorkflowDSLHash != revision.DSLHash ||
		continuation.ParentRunID != call.RunContext.RunID ||
		continuation.ParentActionID != call.ActionID {
		return nil, errors.New("workflow tool continuation binding changed")
	}
	switch continuation.ResumeKind {
	case "", string(agentRuntime.ResumeKindHumanResponse):
		if strings.TrimSpace(continuation.WorkflowResumeToken) == "" ||
			strings.TrimSpace(continuation.ApprovalRequestID) != "" {
			return nil, errors.New("workflow tool human continuation binding changed")
		}
	case string(agentRuntime.ResumeKindDelegatedToolApproval):
		if strings.TrimSpace(continuation.ApprovalRequestID) == "" ||
			strings.TrimSpace(continuation.WorkflowResumeToken) != "" {
			return nil, errors.New("workflow tool approval continuation binding changed")
		}
	default:
		return nil, errors.New("workflow tool continuation resume kind is unsupported")
	}
	runID, err := primitive.ObjectIDFromHex(continuation.WorkflowRunID)
	if err != nil {
		return nil, errors.New("workflow tool continuation run identity is invalid")
	}
	childRun, err := s.repo.GetWorkflowRun(ctx, runID, call.RunContext.UserID)
	if err != nil {
		return nil, err
	}
	if childRun.WorkflowID != publication.WorkflowID ||
		childRun.WorkflowRevisionID != revision.ID ||
		childRun.WorkflowRevisionNumber != revision.RevisionNumber ||
		childRun.InvocationSource != string(workflowTool.SourceRuntime) ||
		childRun.ParentRunID != call.RunContext.RunID ||
		childRun.ParentActionID != call.ActionID {
		return nil, errors.New("workflow tool child run lineage changed")
	}
	if continuation.ResumeKind == string(agentRuntime.ResumeKindDelegatedToolApproval) {
		approvalOID, parseErr := primitive.ObjectIDFromHex(continuation.ApprovalRequestID)
		if parseErr != nil {
			return nil, errors.New("workflow tool approval continuation identity is invalid")
		}
		if childRun.Status == WorkflowRunStatusSuspended && childRun.ApprovalRequestID != approvalOID {
			return nil, errors.New("workflow tool child approval binding changed")
		}
	} else if !childRun.ApprovalRequestID.IsZero() {
		return nil, errors.New("workflow tool human continuation cannot resume an approval-gated child")
	}
	switch childRun.Status {
	case WorkflowRunStatusSuspended, WorkflowRunStatusSuccess:
		return childRun, nil
	default:
		return nil, fmt.Errorf(
			"workflow tool child run cannot resume from status %s",
			childRun.Status,
		)
	}
}

func (s *AgentService) resolveWorkflowToolApprovalBinding(
	ctx context.Context,
	parentRun *repository.AgentExecutionRun,
	captured capturedAgentExecution,
) (*repository.ToolApprovalRequest, error) {
	if parentRun == nil || captured.result.PendingAction == nil ||
		captured.result.PendingToolContinuation == nil {
		return nil, errors.New("workflow tool approval continuation is unavailable")
	}
	action := captured.result.PendingAction
	continuation, err := decodeWorkflowToolContinuation(*captured.result.PendingToolContinuation)
	if err != nil {
		return nil, err
	}
	approvalID := strings.TrimSpace(captured.result.ApprovalID)
	if continuation.ResumeKind != string(agentRuntime.ResumeKindDelegatedToolApproval) ||
		continuation.ApprovalRequestID != approvalID ||
		continuation.ParentRunID != parentRun.ID ||
		continuation.ParentActionID != action.ID ||
		continuation.ToolName != action.Name {
		return nil, errors.New("workflow tool approval continuation does not match the parent action")
	}
	approvalOID, err := primitive.ObjectIDFromHex(approvalID)
	if err != nil {
		return nil, errors.New("workflow tool child approval identity is invalid")
	}
	approvalRepo, err := s.toolApprovalRepository()
	if err != nil {
		return nil, err
	}
	approval, err := approvalRepo.GetToolApproval(ctx, approvalOID, parentRun.UserID)
	if err != nil {
		return nil, err
	}
	childRunOID, err := primitive.ObjectIDFromHex(continuation.WorkflowRunID)
	if err != nil {
		return nil, errors.New("workflow tool child run identity is invalid")
	}
	childRun, err := s.repo.GetWorkflowRun(ctx, childRunOID, parentRun.UserID)
	if err != nil {
		return nil, err
	}
	if childRun.Status != WorkflowRunStatusSuspended ||
		childRun.InvocationSource != string(workflowTool.SourceRuntime) ||
		childRun.ParentRunID != parentRun.ID ||
		childRun.ParentActionID != action.ID ||
		childRun.ApprovalRequestID != approvalOID ||
		approval.RunID != childRun.ID.Hex() ||
		approval.Source != string(workflowTool.SourceWorkflow) ||
		strings.TrimSpace(approval.StepID) == "" ||
		strings.TrimSpace(approval.ToolName) == "" ||
		strings.TrimSpace(approval.InputDigest) == "" ||
		strings.TrimSpace(approval.IdempotencyKey) == "" {
		return nil, errors.New("workflow tool child approval binding does not match the suspended action")
	}
	if approval.Status != repository.ToolApprovalStatusPending &&
		approval.Status != repository.ToolApprovalStatusApproved {
		return nil, fmt.Errorf("workflow tool child approval status %q cannot suspend a run", approval.Status)
	}
	if approval.ExpiresAt.IsZero() || !approval.ExpiresAt.After(time.Now()) {
		return nil, errors.New("workflow tool child approval expired before checkpoint commit")
	}
	return approval, nil
}

func (s *AgentService) validateWorkflowToolApprovalResume(
	ctx context.Context,
	parentRun *repository.AgentExecutionRun,
	checkpoint agentRuntime.RunCheckpoint,
	approvalID string,
	resumeToken string,
) error {
	if parentRun == nil ||
		parentRun.PendingResumeKind != repository.AgentExecutionResumeDelegatedApproval ||
		checkpoint.PendingResumeKind != agentRuntime.ResumeKindDelegatedToolApproval ||
		checkpoint.PendingToolContinuation == nil ||
		checkpoint.PendingAction.Type != agentRuntime.ActionToolCall ||
		checkpoint.PendingAction.ID != parentRun.PendingActionID ||
		checkpoint.PendingAction.Name != parentRun.PendingActionName ||
		checkpoint.PendingApprovalID != approvalID {
		return ErrAgentExecutionRunNotResumable
	}
	continuation, err := decodeWorkflowToolContinuation(*checkpoint.PendingToolContinuation)
	if err != nil {
		return err
	}
	if continuation.ParentRunID != parentRun.ID ||
		continuation.ParentActionID != parentRun.PendingActionID ||
		continuation.ToolName != parentRun.PendingActionName ||
		continuation.ApprovalRequestID != approvalID {
		return ErrAgentExecutionRunNotResumable
	}
	approvalOID, err := primitive.ObjectIDFromHex(approvalID)
	if err != nil {
		return ErrAgentExecutionRunNotResumable
	}
	approvalRepo, err := s.toolApprovalRepository()
	if err != nil {
		return err
	}
	approval, err := approvalRepo.GetToolApproval(ctx, approvalOID, parentRun.UserID)
	if err != nil {
		return err
	}
	childRunOID, err := primitive.ObjectIDFromHex(continuation.WorkflowRunID)
	if err != nil {
		return ErrAgentExecutionRunNotResumable
	}
	childRun, err := s.repo.GetWorkflowRun(ctx, childRunOID, parentRun.UserID)
	if err != nil {
		return err
	}
	if childRun.WorkflowID.Hex() != continuation.WorkflowID ||
		childRun.WorkflowRevisionID.Hex() != continuation.WorkflowRevisionID ||
		childRun.WorkflowRevisionNumber != continuation.WorkflowRevisionNumber ||
		childRun.InvocationSource != string(workflowTool.SourceRuntime) ||
		childRun.ParentRunID != parentRun.ID ||
		childRun.ParentActionID != parentRun.PendingActionID ||
		approval.RunID != childRun.ID.Hex() ||
		approval.Source != string(workflowTool.SourceWorkflow) ||
		approval.InputDigest != parentRun.ApprovalInputDigest ||
		approval.IdempotencyKey != parentRun.ApprovalIdempotencyKey {
		return ErrAgentExecutionRunNotResumable
	}
	switch childRun.Status {
	case WorkflowRunStatusSuspended:
		now := time.Now()
		if childRun.ApprovalRequestID != approvalOID ||
			approval.Status != repository.ToolApprovalStatusApproved ||
			approval.ExpiresAt.IsZero() ||
			!approval.ExpiresAt.After(now) ||
			strings.TrimSpace(resumeToken) == "" ||
			childRun.ResumeTokenHash != hashWorkflowResumeToken(resumeToken) ||
			childRun.ResumeGrantExpiresAt.IsZero() ||
			!childRun.ResumeGrantExpiresAt.After(now) {
			return ErrAgentExecutionRunNotResumable
		}
	case WorkflowRunStatusSuccess:
		switch approval.Status {
		case repository.ToolApprovalStatusApproved,
			repository.ToolApprovalStatusExecuting,
			repository.ToolApprovalStatusConsumed:
		default:
			return ErrAgentExecutionRunNotResumable
		}
	default:
		return ErrAgentExecutionRunNotResumable
	}
	return nil
}

func decodeWorkflowToolContinuation(
	continuation agentRuntime.ToolContinuation,
) (workflowToolContinuation, error) {
	var decoded workflowToolContinuation
	if continuation.Version != workflowToolContinuationVersion ||
		json.Unmarshal(continuation.State, &decoded) != nil ||
		decoded.Version != workflowToolContinuationVersion ||
		decoded.ResumeKind != string(continuation.ResumeKind) ||
		decoded.ApprovalRequestID != strings.TrimSpace(continuation.ApprovalID) {
		return workflowToolContinuation{}, errors.New("workflow tool continuation is invalid")
	}
	return decoded, nil
}

func newWorkflowRuntimeToolSuspension(
	call agentRuntime.ToolCall,
	publication *repository.WorkflowToolPublication,
	result *WorkflowExecutionResult,
) error {
	if publication == nil || result == nil || result.Run == nil ||
		result.Run.Status != WorkflowRunStatusSuspended {
		return errors.New("workflow tool suspension is incomplete")
	}
	resumeKind := agentRuntime.ResumeKindHumanResponse
	approvalID := ""
	resumeToken := strings.TrimSpace(result.ResumeToken)
	if !result.Run.ApprovalRequestID.IsZero() {
		resumeKind = agentRuntime.ResumeKindDelegatedToolApproval
		approvalID = result.Run.ApprovalRequestID.Hex()
		// Approval runs receive a bootstrap token from the generic Workflow
		// suspension path. It is deliberately not copied into the parent
		// checkpoint; approval grant issuance rotates it before resume.
		resumeToken = ""
	} else if resumeToken == "" {
		return errors.New("workflow tool human suspension has no resume token")
	}
	prompt := workflowToolSuspensionPrompt(result.Run.CheckpointJSON)
	if prompt == "" {
		if resumeKind == agentRuntime.ResumeKindDelegatedToolApproval {
			prompt = "Approve the requested workflow action to continue."
		} else {
			return errors.New("workflow tool suspension prompt is unavailable")
		}
	}
	state, err := json.Marshal(workflowToolContinuation{
		Version:                workflowToolContinuationVersion,
		ResumeKind:             string(resumeKind),
		ToolName:               publication.ToolName,
		PublicationRevision:    publication.Revision,
		WorkflowID:             publication.WorkflowID.Hex(),
		WorkflowRevisionID:     publication.WorkflowRevisionID.Hex(),
		WorkflowRevisionNumber: publication.WorkflowRevisionNumber,
		WorkflowDSLHash:        publication.WorkflowDSLHash,
		WorkflowRunID:          result.Run.ID.Hex(),
		ParentRunID:            call.RunContext.RunID,
		ParentActionID:         call.ActionID,
		ApprovalRequestID:      approvalID,
		WorkflowResumeToken:    resumeToken,
	})
	if err != nil {
		return fmt.Errorf("encode workflow tool continuation: %w", err)
	}
	return &agentRuntime.ToolSuspensionError{Continuation: agentRuntime.ToolContinuation{
		Version:    workflowToolContinuationVersion,
		Prompt:     prompt,
		ResumeKind: resumeKind,
		ApprovalID: approvalID,
		State:      state,
	}}
}

func workflowToolSuspensionPrompt(checkpointJSON string) string {
	var checkpoint engine.WorkflowCheckpoint
	if err := json.Unmarshal([]byte(checkpointJSON), &checkpoint); err != nil {
		return ""
	}
	return workflowToolBoundedText(checkpoint.Reason, maxWorkflowToolWaitPromptRunes)
}

func workflowRuntimeToolOutputs(
	publication *repository.WorkflowToolPublication,
	revision *repository.WorkflowRevision,
	workflowDSL *dsl.WorkflowDSL,
	result *WorkflowExecutionResult,
) (map[string]interface{}, error) {
	if publication == nil || revision == nil || result == nil || result.Run == nil {
		return nil, errors.New("published workflow result is incomplete")
	}
	content := strings.TrimSpace(workflowAssistantContent(result.Snapshot, workflowDSL))
	if content == "" {
		content = "Published workflow completed successfully."
	}
	return map[string]interface{}{
		"content": content,
		"structured_content": map[string]interface{}{
			"schema":               workflowToolResultSchema,
			"workflow_id":          publication.WorkflowID.Hex(),
			"workflow_revision_id": revision.ID.Hex(),
			"workflow_run_id":      result.Run.ID.Hex(),
			"status":               result.Run.Status,
			"response":             content,
		},
	}, nil
}

func workflowSnapshotFromRun(run *repository.WorkflowRunRecord) map[string]map[string]interface{} {
	if run == nil || strings.TrimSpace(run.OutputJSON) == "" {
		return nil
	}
	var output struct {
		Blackboard map[string]map[string]interface{} `json:"blackboard"`
	}
	if err := json.Unmarshal([]byte(run.OutputJSON), &output); err != nil {
		return nil
	}
	return output.Blackboard
}

func (s *AgentService) validateWorkflowToolPublicationBinding(
	ctx context.Context,
	userID uint64,
	publication *repository.WorkflowToolPublication,
) (*repository.WorkflowRevision, *dsl.WorkflowDSL, error) {
	if publication == nil || publication.UserID != userID ||
		publication.Status != repository.WorkflowToolPublicationActive {
		return nil, nil, ErrWorkflowNotPublishable
	}
	if publication.ToolName != workflowRuntimeToolName(publication.WorkflowID) {
		return nil, nil, fmt.Errorf("%w: unstable tool identity", ErrWorkflowNotPublishable)
	}
	if err := validateWorkflowToolInputSchema(publication.InputSchemaJSON); err != nil {
		return nil, nil, err
	}
	workflow, err := s.repo.GetWorkflow(ctx, publication.WorkflowID, userID)
	if err != nil {
		return nil, nil, err
	}
	revisionRepo, err := s.workflowRevisionRepository()
	if err != nil {
		return nil, nil, err
	}
	revision, err := revisionRepo.GetWorkflowRevision(
		ctx,
		publication.WorkflowID,
		publication.WorkflowRevisionID,
		userID,
	)
	if err != nil {
		return nil, nil, err
	}
	revision, err = validateWorkflowRevisionIntegrity(revision)
	if err != nil {
		return nil, nil, err
	}
	if revision.RevisionNumber != publication.WorkflowRevisionNumber ||
		revision.DSLHash != publication.WorkflowDSLHash {
		return nil, nil, fmt.Errorf("%w: immutable revision binding changed", ErrWorkflowNotPublishable)
	}
	workflowDSL, err := s.validateWorkflowToolRevision(ctx, userID, workflow, revision)
	if err != nil {
		return nil, nil, err
	}
	return revision, workflowDSL, nil
}

func (s *AgentService) validateWorkflowToolRevision(
	ctx context.Context,
	userID uint64,
	workflow *repository.WorkflowDefinition,
	revision *repository.WorkflowRevision,
) (*dsl.WorkflowDSL, error) {
	if workflow == nil || revision == nil || workflow.UserID != userID ||
		revision.UserID != userID || revision.WorkflowID != workflow.ID {
		return nil, fmt.Errorf("%w: workflow revision ownership mismatch", ErrWorkflowNotPublishable)
	}
	if _, err := validateWorkflowRevisionIntegrity(revision); err != nil {
		return nil, err
	}
	var workflowDSL dsl.WorkflowDSL
	if err := json.Unmarshal([]byte(revision.DSLJSON), &workflowDSL); err != nil {
		return nil, fmt.Errorf("%w: invalid workflow DSL: %v", ErrWorkflowNotPublishable, err)
	}
	if err := validateWorkflowSecurity(&workflowDSL); err != nil {
		return nil, err
	}
	if s.workflowToolExecutor == nil || s.workflowToolExecutor.Registry() == nil {
		return nil, errors.New("workflow tool executor is not configured")
	}
	for _, node := range workflowDSL.Nodes {
		if node.Compensation != nil {
			return nil, fmt.Errorf(
				"%w: node %s declares compensation",
				ErrWorkflowNotPublishable,
				node.ID,
			)
		}
		switch node.Type {
		case "start", "end", "router", "llm":
		case "wait":
			if _, err := validateWorkflowToolWaitNode(node); err != nil {
				return nil, err
			}
		case "agent":
			return nil, fmt.Errorf(
				"%w: node %s starts a nested agent strategy",
				ErrWorkflowNotPublishable,
				node.ID,
			)
		case "tool":
			toolName, err := resolveWorkflowToolName(node.Properties)
			if err != nil {
				return nil, fmt.Errorf("%w: node %s: %v", ErrWorkflowNotPublishable, node.ID, err)
			}
			if isWorkflowRuntimeToolName(toolName) {
				return nil, fmt.Errorf(
					"%w: node %s recursively invokes a published workflow",
					ErrWorkflowNotPublishable,
					node.ID,
				)
			}
			if registered, ok := s.workflowToolExecutor.Registry().Get(toolName); ok {
				requiresApproval, governanceErr := validatePublishedWorkflowToolSpec(registered.Spec)
				if governanceErr != nil {
					return nil, fmt.Errorf(
						"%w: node %s tool %s: %v",
						ErrWorkflowNotPublishable,
						node.ID,
						toolName,
						governanceErr,
					)
				}
				if requiresApproval && !s.workflowToolApprovalBridgeAvailable() {
					return nil, fmt.Errorf(
						"%w: node %s tool %s requires the recoverable approval bridge",
						ErrWorkflowNotPublishable,
						node.ID,
						toolName,
					)
				}
				continue
			}
			if !externalmcp.IsQualifiedToolName(toolName) {
				return nil, fmt.Errorf(
					"%w: node %s references unknown tool %s",
					ErrWorkflowNotPublishable,
					node.ID,
					toolName,
				)
			}
			manager, err := s.externalMCP()
			if err != nil {
				return nil, fmt.Errorf("%w: resolve node %s external MCP: %v", ErrWorkflowNotPublishable, node.ID, err)
			}
			definition, err := manager.GetGovernedTool(ctx, userID, toolName)
			if err != nil {
				return nil, fmt.Errorf("%w: resolve node %s external MCP: %v", ErrWorkflowNotPublishable, node.ID, err)
			}
			if !definition.Policy.Enabled {
				return nil, fmt.Errorf(
					"%w: node %s external MCP tool %s is disabled",
					ErrWorkflowNotPublishable,
					node.ID,
					toolName,
				)
			}
			if definition.Policy.Category == externalmcp.ToolCategoryRead &&
				!definition.Schema.DeclaredReadOnly {
				return nil, fmt.Errorf(
					"%w: node %s external MCP tool %s is not reviewed read-only",
					ErrWorkflowNotPublishable,
					node.ID,
					toolName,
				)
			}
			requiresApproval, governanceErr := validatePublishedWorkflowToolSpec(
				externalMCPToolSpec(definition),
			)
			if governanceErr != nil {
				return nil, fmt.Errorf(
					"%w: node %s external MCP tool %s: %v",
					ErrWorkflowNotPublishable,
					node.ID,
					toolName,
					governanceErr,
				)
			}
			if requiresApproval && !s.workflowToolApprovalBridgeAvailable() {
				return nil, fmt.Errorf(
					"%w: node %s external MCP tool %s requires the recoverable approval bridge",
					ErrWorkflowNotPublishable,
					node.ID,
					toolName,
				)
			}
		default:
			return nil, fmt.Errorf(
				"%w: node %s has unsupported type %s",
				ErrWorkflowNotPublishable,
				node.ID,
				node.Type,
			)
		}
	}
	nodes, err := s.buildWorkflowNodes(&workflowDSL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWorkflowNotPublishable, err)
	}
	if _, err := engine.NewScheduler(&workflowDSL, nodes); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWorkflowNotPublishable, err)
	}
	return &workflowDSL, nil
}

func validatePublishedWorkflowToolSpec(spec workflowTool.ToolSpec) (bool, error) {
	normalized, err := spec.Normalize()
	if err != nil {
		return false, err
	}
	if normalized.Permission != workflowTool.PermissionAuthenticated {
		return false, errors.New("internal-only tools cannot be published through a user workflow")
	}
	switch normalized.Category {
	case workflowTool.CategoryRead:
		if normalized.RequiresApproval() {
			return false, errors.New("read tools cannot declare an approval gate")
		}
		return false, nil
	case workflowTool.CategoryWrite:
		if !normalized.RequiresApproval() || !normalized.Idempotency.Required {
			return false, errors.New("write tools must require approval and idempotency")
		}
		return true, nil
	case workflowTool.CategoryRisky:
		if !normalized.RequiresApproval() {
			return false, errors.New("risky tools must require approval")
		}
		return true, nil
	default:
		return false, fmt.Errorf("category %s cannot be published", normalized.Category)
	}
}

func (s *AgentService) workflowToolApprovalBridgeAvailable() bool {
	return s != nil &&
		s.recoverableAgentRuns &&
		s.unifiedAgentApprovalRecovery &&
		s.agentExecutionRunStore != nil &&
		s.agentCheckpointCipher != nil
}

func validateWorkflowToolWaitNode(node dsl.NodeDSL) (string, error) {
	var properties map[string]interface{}
	if len(node.Properties) > 0 {
		if err := json.Unmarshal(node.Properties, &properties); err != nil {
			return "", fmt.Errorf(
				"%w: node %s has invalid wait properties",
				ErrWorkflowNotPublishable,
				node.ID,
			)
		}
	}
	mode, _ := properties["resume_mode"].(string)
	if strings.TrimSpace(mode) != workflowToolWaitModeHumanInput {
		return "", fmt.Errorf(
			"%w: node %s wait resume_mode must be %s",
			ErrWorkflowNotPublishable,
			node.ID,
			workflowToolWaitModeHumanInput,
		)
	}
	if _, reserved := properties["approval_request_id"]; reserved {
		return "", fmt.Errorf(
			"%w: node %s cannot declare approval metadata",
			ErrWorkflowNotPublishable,
			node.ID,
		)
	}
	if _, reserved := properties["resume_token"]; reserved {
		return "", fmt.Errorf(
			"%w: node %s cannot declare a resume token",
			ErrWorkflowNotPublishable,
			node.ID,
		)
	}
	reason, _ := properties["reason"].(string)
	reason = workflowToolBoundedText(reason, maxWorkflowToolWaitPromptRunes)
	if reason == "" {
		return "", fmt.Errorf(
			"%w: node %s requires a user-facing wait prompt",
			ErrWorkflowNotPublishable,
			node.ID,
		)
	}
	return reason, nil
}

func validateWorkflowToolInputSchema(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("workflow tool input_schema_json is required")
	}
	if len(raw) > maxWorkflowToolInputSchemaBytes {
		return fmt.Errorf(
			"workflow tool input schema exceeds %d bytes",
			maxWorkflowToolInputSchemaBytes,
		)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return fmt.Errorf("invalid workflow tool input schema: %w", err)
	}
	if schemaType, _ := schema["type"].(string); schemaType != "object" {
		return errors.New("workflow tool input schema root type must be object")
	}
	if path, found := workflowToolSchemaReference(schema, "$"); found {
		return fmt.Errorf("workflow tool input schema references are not supported: %s", path)
	}
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		for name := range properties {
			if _, reserved := reservedWorkflowToolInputs[strings.ToLower(strings.TrimSpace(name))]; reserved {
				return fmt.Errorf("workflow tool input schema property %q is reserved", name)
			}
		}
	}
	if required, ok := schema["required"].([]interface{}); ok {
		for _, value := range required {
			name, _ := value.(string)
			if _, reserved := reservedWorkflowToolInputs[strings.ToLower(strings.TrimSpace(name))]; reserved {
				return fmt.Errorf("workflow tool input schema required field %q is reserved", name)
			}
		}
	}

	registry := workflowTool.NewRegistry()
	err := registry.RegisterHandler(
		workflowTool.ToolSpec{
			Name:        "workflow_schema_validator",
			Description: "Validate a published workflow input contract.",
			InputSchema: json.RawMessage(raw),
			Category:    workflowTool.CategoryRead,
			Permission:  workflowTool.PermissionAuthenticated,
			Timeout:     time.Second,
			Retry:       workflowTool.RetryPolicy{MaxAttempts: 1},
			Approval:    workflowTool.ApprovalNever,
		},
		workflowTool.HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("invalid workflow tool input schema: %w", err)
	}
	return nil
}

func workflowToolSchemaReference(value interface{}, path string) (string, bool) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, child := range typed {
			childPath := path + "." + key
			if key == "$ref" {
				return childPath, true
			}
			if foundPath, found := workflowToolSchemaReference(child, childPath); found {
				return foundPath, true
			}
		}
	case []interface{}:
		for index, child := range typed {
			if foundPath, found := workflowToolSchemaReference(
				child,
				fmt.Sprintf("%s[%d]", path, index),
			); found {
				return foundPath, true
			}
		}
	}
	return "", false
}

func workflowToolPublicationSpec(
	publication *repository.WorkflowToolPublication,
	timeout time.Duration,
) workflowTool.ToolSpec {
	if timeout <= 0 {
		timeout = defaultWorkflowToolTimeout
	}
	return workflowTool.ToolSpec{
		Name:            publication.ToolName,
		Description:     publication.Description,
		InputSchema:     json.RawMessage(publication.InputSchemaJSON),
		Category:        workflowTool.CategoryRead,
		Permission:      workflowTool.PermissionAuthenticated,
		Timeout:         timeout,
		Retry:           workflowTool.RetryPolicy{MaxAttempts: 1},
		Approval:        workflowTool.ApprovalNever,
		SensitiveFields: workflowToolInputFields(publication.InputSchemaJSON),
	}
}

func workflowToolInputFields(schemaJSON string) []string {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return nil
	}
	fields := make([]string, 0, len(schema.Properties))
	for field := range schema.Properties {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func (s *AgentService) workflowToolPublications() (repository.WorkflowToolPublicationStore, error) {
	if s == nil || s.workflowToolPublicationStore == nil {
		return nil, errors.New("workflow tool publication store is not configured")
	}
	if s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	return s.workflowToolPublicationStore, nil
}

func workflowRuntimeToolName(workflowID primitive.ObjectID) string {
	return workflowRuntimeToolPrefix + workflowID.Hex()
}

func isWorkflowRuntimeToolName(name string) bool {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(name, workflowRuntimeToolPrefix) {
		return false
	}
	_, err := primitive.ObjectIDFromHex(strings.TrimPrefix(name, workflowRuntimeToolPrefix))
	return err == nil
}

func cloneWorkflowToolInputs(inputs map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(inputs))
	for key, value := range inputs {
		cloned[key] = cloneExternalMCPValue(value)
	}
	return cloned
}

func normalizeWorkflowToolDescription(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	return workflowToolBoundedText(value, maxWorkflowToolDescriptionRunes)
}

func workflowToolBoundedText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes < 1 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func workflowToolPublicationName(publication *repository.WorkflowToolPublication) string {
	if publication == nil {
		return ""
	}
	return publication.ToolName
}
