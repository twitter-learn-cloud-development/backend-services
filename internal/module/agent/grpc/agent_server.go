package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/module/agent/extension"
	"twitter-clone/internal/module/agent/marketplace"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	agentObservability "twitter-clone/internal/module/agent/observability"
	agentproject "twitter-clone/internal/module/agent/project"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/service"
	"twitter-clone/internal/module/agent/skill"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

// AgentServer gRPC 服务器
type AgentServer struct {
	aiAgentv1.UnimplementedAiAgentServiceServer
	svc                            *service.AgentService
	profileManager                 *service.ProfileCatalogManager
	profileExperimentManager       *service.ProfileExperimentManager
	profileAccessManager           *service.ProfileAccessManager
	profileAdminToken              string
	profileDirectPublishEnabled    bool
	extensionMarketplaceManager    *service.ExtensionMarketplaceManager
	extensionMarketplaceAdminToken string
}

func WithProfileExperimentManager(manager *service.ProfileExperimentManager) AgentServerOption {
	return func(server *AgentServer) {
		server.profileExperimentManager = manager
	}
}

type AgentServerOption func(*AgentServer)

func WithProfileAdministration(manager *service.ProfileCatalogManager, token string) AgentServerOption {
	return func(server *AgentServer) {
		server.profileManager = manager
		server.profileAdminToken = token
	}
}

func WithProfileDirectPublish(enabled bool) AgentServerOption {
	return func(server *AgentServer) {
		server.profileDirectPublishEnabled = enabled
	}
}

func WithProfileAccessManager(manager *service.ProfileAccessManager) AgentServerOption {
	return func(server *AgentServer) {
		server.profileAccessManager = manager
	}
}

func WithExtensionMarketplaceAdministration(
	manager *service.ExtensionMarketplaceManager,
	token string,
) AgentServerOption {
	return func(server *AgentServer) {
		server.extensionMarketplaceManager = manager
		server.extensionMarketplaceAdminToken = strings.TrimSpace(token)
	}
}

// NewAgentServer 创建 Agent gRPC 服务器
func NewAgentServer(svc *service.AgentService, options ...AgentServerOption) *AgentServer {
	server := &AgentServer{svc: svc}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	return server
}

// CallApiOfAi 模式一：直接调用 AI 对话
func (s *AgentServer) CallApiOfAi(ctx context.Context, req *aiAgentv1.CallApiOfAiRequest) (*aiAgentv1.CallApiOfAiResponse, error) {
	log.Printf("gRPC: CallApiOfAi - user_id=%d, dialogue_id=%d", req.UserId, req.MainContent.DialogueId)

	modelCtx, err := s.svc.ContextWithModelKind(ctx, req.ModelKindId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.svc.CallApiOfAi(modelCtx, req.UserId, req.MainContent.DialogueId, req.MainContent.DialogueKey, req.MainContent.Content)
	if err != nil {
		log.Printf("❌ CallApiOfAi error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to call ai: %v", err)
	}

	return &aiAgentv1.CallApiOfAiResponse{
		Code:        200,
		Msg:         "success",
		Response:    result.Response,
		DialogueKey: result.DialogueID,
	}, nil
}

// RunAgent is the P8 unified entry point. Capability hints never bypass
// server-side catalog, policy, budget or approval enforcement.
func (s *AgentServer) RunAgent(ctx context.Context, req *aiAgentv1.RunAgentRequest) (*aiAgentv1.RunAgentResponse, error) {
	if req == nil || req.MainContent == nil {
		return nil, status.Error(codes.InvalidArgument, "main_content is required")
	}

	modelCtx, err := s.svc.ContextWithModelKind(ctx, req.ModelKindId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.svc.RunAgent(modelCtx, service.UnifiedAgentRequest{
		UserID:                    req.UserId,
		DialogueID:                req.MainContent.DialogueId,
		DialogueKey:               req.MainContent.DialogueKey,
		Content:                   req.MainContent.Content,
		PreferredCapabilityIDs:    req.PreferredCapabilityIds,
		WebSearchProviderConfigID: req.WebSearchProviderConfigId,
		SkillID:                   req.SkillId,
		SkillVersion:              req.SkillVersion,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidUnifiedAgentRequest),
			errors.Is(err, service.ErrUnsupportedCapability):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, service.ErrCapabilityCompositionPending),
			errors.Is(err, service.ErrCapabilityUnavailable),
			errors.Is(err, service.ErrRequiredCapabilityEvidence),
			errors.Is(err, skill.ErrCatalogDisabled),
			errors.Is(err, service.ErrWorkflowNotPublishable),
			errors.Is(err, service.ErrAgentTaskTemplateRouteDrift):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		case errors.Is(err, skill.ErrSkillNotFound),
			errors.Is(err, skill.ErrVersionNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		default:
			return nil, status.Errorf(codes.Internal, "run agent failed: %v", err)
		}
	}

	return unifiedAgentResultToProto(result), nil
}

func (s *AgentServer) CreateAgentTaskTemplate(
	ctx context.Context,
	req *aiAgentv1.CreateAgentTaskTemplateRequest,
) (*aiAgentv1.CreateAgentTaskTemplateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	template, err := s.svc.CreateAgentTaskTemplate(ctx, service.CreateAgentTaskTemplateRequest{
		UserID: req.UserId, SourceRunID: req.SourceRunId,
		ExpectedSourceRunRevision: req.ExpectedSourceRunRevision,
		Name:                      req.Name, Description: req.Description,
		InstructionTemplate: req.InstructionTemplate,
		IdempotencyKey:      req.IdempotencyKey,
	})
	if err != nil {
		return nil, agentTaskTemplateGRPCError(err)
	}
	return &aiAgentv1.CreateAgentTaskTemplateResponse{
		Code: 200, Msg: "success", TaskTemplate: agentTaskTemplateToProto(template),
	}, nil
}

func (s *AgentServer) ListAgentTaskTemplates(
	ctx context.Context,
	req *aiAgentv1.ListAgentTaskTemplatesRequest,
) (*aiAgentv1.ListAgentTaskTemplatesResponse, error) {
	if req == nil || req.UserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	templates, err := s.svc.ListAgentTaskTemplates(ctx, req.UserId, int(req.Limit))
	if err != nil {
		return nil, agentTaskTemplateGRPCError(err)
	}
	items := make([]*aiAgentv1.AgentTaskTemplate, 0, len(templates))
	for _, template := range templates {
		items = append(items, agentTaskTemplateToProto(template))
	}
	return &aiAgentv1.ListAgentTaskTemplatesResponse{
		Code: 200, Msg: "success",
		ExecutionEnabled: s.svc.AgentTaskTemplatesEnabled(),
		TaskTemplates:    items,
	}, nil
}

func (s *AgentServer) ArchiveAgentTaskTemplate(
	ctx context.Context,
	req *aiAgentv1.ArchiveAgentTaskTemplateRequest,
) (*aiAgentv1.ArchiveAgentTaskTemplateResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	template, err := s.svc.ArchiveAgentTaskTemplate(
		ctx,
		req.UserId,
		req.TemplateId,
		req.ExpectedRevision,
	)
	if err != nil {
		return nil, agentTaskTemplateGRPCError(err)
	}
	return &aiAgentv1.ArchiveAgentTaskTemplateResponse{
		Code: 200, Msg: "success", TaskTemplate: agentTaskTemplateToProto(template),
	}, nil
}

func (s *AgentServer) RunAgentTaskTemplate(
	ctx context.Context,
	req *aiAgentv1.RunAgentTaskTemplateRequest,
) (*aiAgentv1.RunAgentResponse, error) {
	if req == nil || req.MainContent == nil {
		return nil, status.Error(codes.InvalidArgument, "main_content is required")
	}
	modelCtx, err := s.svc.ContextWithModelKind(ctx, req.ModelKindId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.svc.RunAgentTaskTemplate(modelCtx, service.RunAgentTaskTemplateRequest{
		UserID: req.UserId, TemplateID: req.TemplateId,
		ExpectedTemplateRevision:  req.ExpectedRevision,
		DialogueID:                req.MainContent.DialogueId,
		DialogueKey:               req.MainContent.DialogueKey,
		Input:                     req.MainContent.Content,
		WebSearchProviderConfigID: req.WebSearchProviderConfigId,
	})
	if err != nil {
		return nil, agentTaskTemplateGRPCError(err)
	}
	return unifiedAgentResultToProto(result), nil
}

func agentTaskTemplateGRPCError(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidAgentTaskTemplate),
		errors.Is(err, service.ErrInvalidUnifiedAgentRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, repository.ErrAgentTaskTemplateNotFound),
		errors.Is(err, repository.ErrAgentExecutionRunNotFound),
		errors.Is(err, skill.ErrSkillNotFound),
		errors.Is(err, skill.ErrVersionNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, repository.ErrAgentTaskTemplateConflict),
		errors.Is(err, repository.ErrAgentExecutionRunConflict):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, service.ErrAgentTaskTemplateIdempotency):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, service.ErrAgentTaskTemplatesDisabled),
		errors.Is(err, service.ErrAgentTaskTemplateStoreUnavailable),
		errors.Is(err, service.ErrAgentExecutionRunStoreUnavailable),
		errors.Is(err, service.ErrAgentTaskTemplateSourceIncomplete),
		errors.Is(err, service.ErrAgentTaskTemplateRouteDrift),
		errors.Is(err, service.ErrCapabilityUnavailable),
		errors.Is(err, skill.ErrCatalogDisabled):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Errorf(codes.Internal, "agent task template operation failed: %v", err)
	}
}

func (s *AgentServer) ListAgentSkills(
	ctx context.Context,
	req *aiAgentv1.ListAgentSkillsRequest,
) (*aiAgentv1.ListAgentSkillsResponse, error) {
	if req == nil || req.UserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	versions, err := s.svc.ListAgentSkills(ctx, req.UserId, int(req.Limit))
	if err != nil {
		return nil, agentSkillGRPCError(err)
	}
	items := make([]*aiAgentv1.AgentSkill, 0, len(versions))
	for _, version := range versions {
		items = append(items, agentSkillToProto(version))
	}
	return &aiAgentv1.ListAgentSkillsResponse{
		Code: 200, Msg: "success", Skills: items,
	}, nil
}

func (s *AgentServer) GetAgentSkill(
	ctx context.Context,
	req *aiAgentv1.GetAgentSkillRequest,
) (*aiAgentv1.GetAgentSkillResponse, error) {
	if req == nil || req.UserId == 0 || strings.TrimSpace(req.SkillId) == "" ||
		strings.TrimSpace(req.Version) == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"user_id, skill_id and version are required",
		)
	}
	version, err := s.svc.GetAgentSkill(
		ctx,
		req.UserId,
		req.SkillId,
		req.Version,
	)
	if err != nil {
		return nil, agentSkillGRPCError(err)
	}
	return &aiAgentv1.GetAgentSkillResponse{
		Code: 200, Msg: "success", Skill: agentSkillToProto(version),
	}, nil
}

func (s *AgentServer) ListAgentExtensions(
	ctx context.Context,
	req *aiAgentv1.ListAgentExtensionsRequest,
) (*aiAgentv1.ListAgentExtensionsResponse, error) {
	if req == nil || req.UserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	page, err := s.svc.ListAgentExtensions(ctx, req.UserId, extension.Query{
		Kind: req.Kind, Category: req.Category, Scope: req.Scope, Status: req.Status,
		Search: req.Search, AfterCursor: req.AfterCursor, PageSize: int(req.PageSize),
	})
	if err != nil {
		return nil, agentExtensionGRPCError(err)
	}
	items := make([]*aiAgentv1.AgentExtension, 0, len(page.Entries))
	for _, entry := range page.Entries {
		items = append(items, agentExtensionToProto(entry))
	}
	sources := make([]*aiAgentv1.AgentExtensionSourceStatus, 0, len(page.Sources))
	for _, source := range page.Sources {
		sources = append(sources, &aiAgentv1.AgentExtensionSourceStatus{
			Source: source.Source, State: source.State, EntryCount: int32(source.EntryCount),
		})
	}
	return &aiAgentv1.ListAgentExtensionsResponse{
		Code: 200, Msg: "success", ContractVersion: page.ContractVersion,
		Extensions: items, Sources: sources, NextCursor: page.NextCursor, HasMore: page.HasMore,
	}, nil
}

func (s *AgentServer) ListAgentMarketplaceExtensions(
	ctx context.Context,
	req *aiAgentv1.ListAgentMarketplaceExtensionsRequest,
) (*aiAgentv1.ListAgentMarketplaceExtensionsResponse, error) {
	if req == nil || req.UserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	page, err := s.svc.ListAgentMarketplaceExtensions(ctx, req.UserId, marketplace.Query{
		Kind: req.Kind, PublisherID: req.PublisherId, Search: req.Search,
		AfterCursor: req.AfterCursor, PageSize: int(req.PageSize),
	})
	if err != nil {
		return nil, agentMarketplaceGRPCError(err)
	}
	releases := make([]*aiAgentv1.AgentMarketplaceExtension, 0, len(page.Releases))
	for _, release := range page.Releases {
		releases = append(releases, agentMarketplaceExtensionToProto(release))
	}
	return &aiAgentv1.ListAgentMarketplaceExtensionsResponse{
		Code: 200, Msg: "success", ContractVersion: page.ContractVersion,
		Releases: releases, NextCursor: page.NextCursor, HasMore: page.HasMore,
	}, nil
}

func (s *AgentServer) GetAgentRun(
	ctx context.Context,
	req *aiAgentv1.GetAgentRunRequest,
) (*aiAgentv1.GetAgentRunResponse, error) {
	if req == nil || req.UserId == 0 || strings.TrimSpace(req.RunId) == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and run_id are required")
	}
	run, err := s.svc.GetAgentExecutionRunView(ctx, req.UserId, req.RunId)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrAgentExecutionRunNotFound):
			return nil, status.Error(codes.NotFound, "agent run not found")
		case errors.Is(err, service.ErrAgentExecutionRunStoreUnavailable):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		default:
			return nil, status.Errorf(codes.Internal, "get agent run failed: %v", err)
		}
	}
	return &aiAgentv1.GetAgentRunResponse{
		Code: 200, Msg: "success", Run: agentExecutionRunViewToProto(run),
	}, nil
}

func (s *AgentServer) GetAgentRunAccounting(
	ctx context.Context,
	req *aiAgentv1.GetAgentRunAccountingRequest,
) (*aiAgentv1.GetAgentRunAccountingResponse, error) {
	if req == nil || req.UserId == 0 || strings.TrimSpace(req.RunId) == "" || req.ChildLimit < 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id, run_id and a non-negative child_limit are required")
	}
	accounting, err := s.svc.GetAgentRunAccounting(ctx, req.UserId, req.RunId, int(req.ChildLimit))
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrAgentExecutionRunNotFound):
			return nil, status.Error(codes.NotFound, "agent run not found")
		case errors.Is(err, service.ErrAgentExecutionRunStoreUnavailable),
			errors.Is(err, service.ErrAgentRunAccountingStoreUnavailable):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		default:
			return nil, status.Errorf(codes.Internal, "get agent run accounting failed: %v", err)
		}
	}
	return &aiAgentv1.GetAgentRunAccountingResponse{
		Code: 200, Msg: "success", Accounting: agentRunAccountingToProto(accounting),
	}, nil
}

func (s *AgentServer) ResumeAgentRun(
	ctx context.Context,
	req *aiAgentv1.ResumeAgentRunRequest,
) (*aiAgentv1.RunAgentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	result, err := s.svc.ResumeAgentExecutionRun(ctx, service.ResumeAgentExecutionRequest{
		UserID: req.UserId, RunID: req.RunId, ExpectedRevision: req.ExpectedRevision,
		HumanResponse: req.HumanResponse, ApprovalID: req.ApprovalId, ResumeToken: req.ResumeToken,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidUnifiedAgentRequest):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, repository.ErrAgentExecutionRunNotFound):
			return nil, status.Error(codes.NotFound, "agent run not found")
		case errors.Is(err, repository.ErrAgentExecutionRunConflict):
			return nil, status.Error(codes.Aborted, "agent run revision or resume lease changed")
		case errors.Is(err, service.ErrAgentExecutionRunNotResumable),
			errors.Is(err, service.ErrAgentExecutionProfileDrift),
			errors.Is(err, service.ErrAgentExecutionRunStoreUnavailable):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		case errors.Is(err, service.ErrAgentRunCheckpointInvalid),
			errors.Is(err, service.ErrAgentRunCheckpointUnavailable):
			return nil, status.Error(codes.FailedPrecondition, "agent run checkpoint is unavailable")
		default:
			return nil, status.Errorf(codes.Internal, "resume agent run failed: %v", err)
		}
	}
	return unifiedAgentResultToProto(result), nil
}

func (s *AgentServer) IssueAgentResumeGrant(
	ctx context.Context,
	req *aiAgentv1.IssueAgentResumeGrantRequest,
) (*aiAgentv1.IssueAgentResumeGrantResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	grant, err := s.svc.IssueAgentResumeGrant(ctx, req.UserId, req.ApprovalId, req.ExpectedRunRevision)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrAgentExecutionRunNotFound):
			return nil, status.Error(codes.NotFound, "agent run not found")
		case errors.Is(err, repository.ErrAgentExecutionRunConflict):
			return nil, status.Error(codes.Aborted, "agent run revision changed")
		case errors.Is(err, service.ErrAgentExecutionRunNotResumable),
			errors.Is(err, service.ErrAgentExecutionProfileDrift):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		default:
			return nil, status.Errorf(codes.Internal, "issue agent resume grant failed: %v", err)
		}
	}
	return &aiAgentv1.IssueAgentResumeGrantResponse{
		Code: 200, Msg: "success", Run: agentExecutionRunToProto(grant.Run),
		ResumeToken: grant.ResumeToken, ExpiresAt: unixOrZero(grant.ExpiresAt),
	}, nil
}

func unifiedAgentResultToProto(result *service.UnifiedAgentResult) *aiAgentv1.RunAgentResponse {
	if result == nil {
		return nil
	}
	tweets := make([]*aiAgentv1.TweetResult, 0, len(result.Tweets))
	for _, tweet := range result.Tweets {
		tweets = append(tweets, &aiAgentv1.TweetResult{
			TweetId: tweet.TweetID,
			Url:     tweet.URL,
			Summary: tweet.Summary,
		})
	}
	toolActivities := make([]*aiAgentv1.AgentToolActivity, 0, len(result.ToolActivities))
	for _, activity := range result.ToolActivities {
		toolActivities = append(toolActivities, &aiAgentv1.AgentToolActivity{
			StepIndex:   int32(activity.StepIndex),
			ToolName:    activity.ToolName,
			Status:      activity.Status,
			ResultCount: int32(activity.ResultCount),
		})
	}
	citations := make([]*aiAgentv1.AgentCitation, 0, len(result.Citations))
	for _, citation := range result.Citations {
		citations = append(citations, &aiAgentv1.AgentCitation{
			CitationId: citation.CitationID,
			SourceType: citation.SourceType,
			SourceId:   citation.SourceID,
			Url:        citation.URL,
			Title:      citation.Title,
			Snippet:    citation.Snippet,
		})
	}
	artifacts := make([]*aiAgentv1.AgentArtifact, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		artifacts = append(artifacts, &aiAgentv1.AgentArtifact{
			ArtifactId:           artifact.ArtifactID,
			Type:                 artifact.Type,
			Status:               artifact.Status,
			ContentType:          artifact.ContentType,
			Content:              artifact.Content,
			SourceRunId:          artifact.SourceRunID,
			RequiresConfirmation: artifact.RequiresConfirmation,
		})
	}
	approvalState := &aiAgentv1.AgentApprovalState{
		Status:          result.ApprovalState.Status,
		ApprovalId:      result.ApprovalState.ApprovalID,
		RunId:           result.ApprovalState.RunID,
		Action:          result.ApprovalState.Action,
		Revision:        result.ApprovalState.Revision,
		ExpiresAt:       result.ApprovalState.ExpiresAt,
		ResumeSupported: result.ApprovalState.ResumeSupported,
	}
	return &aiAgentv1.RunAgentResponse{
		Code:                         200,
		Msg:                          "success",
		Response:                     result.Response,
		DialogueKey:                  result.DialogueID,
		RunId:                        result.RunID,
		ExecutionProfile:             result.ExecutionProfile,
		CapabilityIds:                result.CapabilityIDs,
		TweetList:                    tweets,
		PublishableDraft:             result.PublishableDraft,
		ToolActivities:               toolActivities,
		Citations:                    citations,
		Artifacts:                    artifacts,
		ApprovalState:                approvalState,
		RunStatus:                    result.RunStatus,
		SelectedSkillId:              result.SelectedSkillID,
		SelectedSkillVersion:         result.SelectedSkillVersion,
		SelectedTaskTemplateId:       result.SelectedTaskTemplateID,
		SelectedTaskTemplateRevision: result.SelectedTaskTemplateRevision,
		ExecutionStrategyPlan:        executionStrategyPlanToProto(&result.ExecutionStrategyPlan),
	}
}

func agentExecutionRunViewToProto(run *service.AgentExecutionRunView) *aiAgentv1.AgentExecutionRun {
	if run == nil {
		return nil
	}
	return &aiAgentv1.AgentExecutionRun{
		RunId: run.RunID, DialogueKey: run.DialogueID, ExecutionProfile: run.ExecutionProfile,
		CapabilityIds: append([]string(nil), run.CapabilityIDs...), Status: run.Status,
		SkillId: run.SkillID, SkillVersion: run.SkillVersion,
		TaskTemplateId: run.TaskTemplateID, TaskTemplateRevision: run.TaskTemplateRevision,
		ExecutionStrategyPlan: executionStrategyPlanToProto(run.ExecutionStrategyPlan),
		Revision:              run.Revision, ResumeSupported: run.ResumeSupported,
		PendingActionType: run.PendingActionType, PendingActionName: run.PendingActionName,
		PendingActionId: run.PendingActionID, ApprovalId: run.ApprovalID,
		ApprovalExpiresAt: unixOrZero(run.ApprovalExpiresAt),
		StepCount:         int32(run.StepCount), InputTokens: int32(run.InputTokens),
		OutputTokens: int32(run.OutputTokens), TotalTokens: int32(run.TotalTokens),
		EstimatedCostMicros: run.EstimatedCostMicros, PricingVersion: run.PricingVersion,
		FailureCode: run.FailureCode, StartedAt: unixMilliOrZero(run.StartedAt),
		UpdatedAt: unixMilliOrZero(run.UpdatedAt), SuspendedAt: unixMilliOrZero(run.SuspendedAt),
		FinishedAt: unixMilliOrZero(run.FinishedAt),
	}
}

func agentExecutionRunToProto(run *repository.AgentExecutionRun) *aiAgentv1.AgentExecutionRun {
	if run == nil {
		return nil
	}
	return &aiAgentv1.AgentExecutionRun{
		RunId: run.ID, DialogueKey: run.DialogueID, ExecutionProfile: run.ExecutionProfile,
		CapabilityIds: append([]string(nil), run.CapabilityIDs...), Status: string(run.Status),
		SkillId: run.SkillID, SkillVersion: run.SkillVersion,
		TaskTemplateId: run.TaskTemplateID, TaskTemplateRevision: run.TaskTemplateRevision,
		ExecutionStrategyPlan: executionStrategyPlanToProto(run.ExecutionStrategyPlan),
		Revision:              run.Revision, ResumeSupported: run.ResumeSupported,
		PendingActionType: run.PendingActionType, PendingActionName: run.PendingActionName,
		PendingActionId: run.PendingActionID, ApprovalId: run.ApprovalRequestID,
		ApprovalExpiresAt: unixOrZero(run.ApprovalExpiresAt),
		StepCount:         int32(run.StepCount), InputTokens: int32(run.InputTokens),
		OutputTokens: int32(run.OutputTokens), TotalTokens: int32(run.TotalTokens),
		EstimatedCostMicros: run.EstimatedCostMicros, PricingVersion: run.PricingVersion,
		FailureCode: run.FailureCode, StartedAt: unixMilliOrZero(run.StartedAt),
		UpdatedAt: unixMilliOrZero(run.UpdatedAt), SuspendedAt: unixMilliOrZero(run.SuspendedAt),
		FinishedAt: unixMilliOrZero(run.FinishedAt),
	}
}

func executionStrategyPlanToProto(plan *agentStrategy.Plan) *aiAgentv1.AgentExecutionStrategyPlan {
	if plan == nil || plan.Version == "" {
		return nil
	}
	roles := make([]*aiAgentv1.AgentExecutionStrategyRole, 0, len(plan.Roles))
	for _, role := range plan.Roles {
		roles = append(roles, &aiAgentv1.AgentExecutionStrategyRole{
			RoleId: role.RoleID, CapabilityIds: append([]string(nil), role.CapabilityIDs...),
			AllowedTools: append([]string(nil), role.AllowedTools...), MaxSteps: int32(role.MaxSteps),
			MaxTotalTokens:         int32(role.MaxTotalTokens),
			MaxEstimatedCostMicros: role.MaxEstimatedCostMicros, TimeoutMillis: role.TimeoutMillis,
		})
	}
	return &aiAgentv1.AgentExecutionStrategyPlan{
		Version: plan.Version, TemplateId: plan.TemplateID,
		CandidateStrategy: string(plan.CandidateStrategy), SelectedStrategy: string(plan.SelectedStrategy),
		Decision: string(plan.Decision), ReasonCode: string(plan.ReasonCode),
		ComplexityScore: int32(plan.ComplexityScore), ComplexityClass: string(plan.ComplexityClass),
		ComplexitySignals:      append([]string(nil), plan.ComplexitySignals...),
		EstimatedLatencyMillis: plan.EstimatedLatencyMillis,
		EstimatedTotalTokens:   int32(plan.EstimatedTotalTokens), EstimatedCostMicros: plan.EstimatedCostMicros,
		MaxParallelRoles: int32(plan.MaxParallelRoles), Roles: roles, PlanDigest: plan.PlanDigest,
	}
}

func agentRunAccountingToProto(view *service.AgentRunAccountingView) *aiAgentv1.AgentRunAccounting {
	if view == nil {
		return nil
	}
	children := make([]*aiAgentv1.WorkflowRunAccounting, 0, len(view.Children))
	for _, child := range view.Children {
		children = append(children, &aiAgentv1.WorkflowRunAccounting{
			RunId: child.RunID, WorkflowId: child.WorkflowID, ParentActionId: child.ParentActionID,
			Status: child.Status, State: child.State, AccountingVersion: child.AccountingVersion,
			Usage:         executionRuntimeTokenUsageToProto(child.Usage),
			Budget:        executionBudgetViewToProto(child.Budget),
			StartedAtMs:   unixMilliOrZero(child.StartedAt),
			SuspendedAtMs: unixMilliOrZero(child.SuspendedAt),
			FinishedAtMs:  unixMilliOrZero(child.FinishedAt),
		})
	}
	return &aiAgentv1.AgentRunAccounting{
		RunId: view.RunID, RunStatus: view.RunStatus, Scope: view.Scope, State: view.State,
		Complete: view.Complete, Truncated: view.Truncated, ChildRunCount: view.ChildRunCount,
		IncludedChildRunCount: int32(view.IncludedChildRunCount),
		AccountingVersion:     view.AccountingVersion,
		ParentUsage:           executionRuntimeTokenUsageToProto(view.ParentUsage),
		ParentBudget:          executionBudgetViewToProto(view.ParentBudget),
		ChildUsage:            executionRuntimeTokenUsageToProto(view.ChildUsage),
		TotalUsage:            executionRuntimeTokenUsageToProto(view.TotalUsage),
		Children:              children,
	}
}

func executionRuntimeTokenUsageToProto(usage agentRuntime.TokenUsage) *aiAgentv1.ExecutionTokenUsage {
	return &aiAgentv1.ExecutionTokenUsage{
		InputTokens: int32(usage.InputTokens), OutputTokens: int32(usage.OutputTokens),
		TotalTokens: int32(usage.TotalTokens), Estimated: usage.Estimated,
		EstimatedCostMicros: usage.EstimatedCostMicros, CostEstimated: usage.CostEstimated,
		PricingVersion: usage.PricingVersion,
	}
}

func executionBudgetViewToProto(budget service.ExecutionBudgetView) *aiAgentv1.ExecutionBudgetSnapshot {
	return &aiAgentv1.ExecutionBudgetSnapshot{
		MaxSteps: int32(budget.MaxSteps), MaxTotalTokens: int32(budget.MaxTotalTokens),
		MaxEstimatedCostMicros: budget.MaxEstimatedCostMicros,
		ConsumedSteps:          int32(budget.ConsumedSteps), ConsumedTokens: int32(budget.ConsumedTokens),
		ConsumedCostMicros: budget.ConsumedCostMicros,
	}
}

func agentTaskTemplateToProto(template *service.AgentTaskTemplateView) *aiAgentv1.AgentTaskTemplate {
	if template == nil {
		return nil
	}
	return &aiAgentv1.AgentTaskTemplate{
		ContractVersion: template.ContractVersion, TemplateId: template.TemplateID,
		Name: template.Name, Description: template.Description,
		InstructionTemplate: template.InstructionTemplate, Status: template.Status,
		Revision: template.Revision, SourceRunId: template.SourceRunID,
		SourceRunRevision:      template.SourceRunRevision,
		SourceResultDigest:     template.SourceResultDigest,
		SourceExecutionProfile: template.SourceExecutionProfile,
		CapabilityIds:          append([]string(nil), template.CapabilityIDs...),
		SkillId:                template.SkillID, SkillVersion: template.SkillVersion,
		SourceModel: template.SourceModel, AgentProfileId: template.AgentProfileID,
		AgentProfileVersion:   template.AgentProfileVersion,
		PromptTemplateId:      template.PromptTemplateID,
		PromptTemplateVersion: template.PromptTemplateVersion,
		CreatedAt:             unixMilliOrZero(template.CreatedAt),
		UpdatedAt:             unixMilliOrZero(template.UpdatedAt),
		ArchivedAt:            unixMilliOrZero(template.ArchivedAt),
	}
}

// ConsultContent 模式二：通过对话查询相关推文和作者
func (s *AgentServer) ConsultContent(ctx context.Context, req *aiAgentv1.ConsultContentRequest) (*aiAgentv1.ConsultContentResponse, error) {
	log.Printf("gRPC: ConsultContent - user_id=%d, dialogue_id=%d", req.UserId, req.MainContent.DialogueId)

	modelCtx, err := s.svc.ContextWithModelKind(ctx, req.ModelKindId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.svc.ConsultContent(modelCtx, req.UserId, req.MainContent.DialogueId, req.MainContent.DialogueKey, req.MainContent.Content)
	if err != nil {
		log.Printf("❌ ConsultContent error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to consult content: %v", err)
	}

	protoTweetList := make([]*aiAgentv1.TweetResult, len(result.Tweets))
	for i, t := range result.Tweets {
		protoTweetList[i] = &aiAgentv1.TweetResult{
			TweetId: t.TweetID,
			Url:     t.URL,
			Summary: t.Summary,
		}
	}

	return &aiAgentv1.ConsultContentResponse{
		Code:        200,
		Msg:         "success",
		Response:    result.Response,
		TweetList:   protoTweetList,
		DialogueKey: result.DialogueID,
	}, nil
}

// AssistPublishTwitter 模式三：协助构建推文
func (s *AgentServer) AssistPublishTwitter(ctx context.Context, req *aiAgentv1.AssistPublishTwitterRequest) (*aiAgentv1.AssistPublishTwitterResponse, error) {
	log.Printf("gRPC: AssistPublishTwitter - user_id=%d, dialogue_id=%d", req.UserId, req.MainContent.DialogueId)

	modelCtx, err := s.svc.ContextWithModelKind(ctx, req.ModelKindId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.svc.AssistPublishTwitter(modelCtx, req.UserId, req.MainContent.DialogueId, req.MainContent.DialogueKey, req.MainContent.Content)
	if err != nil {
		log.Printf("❌ AssistPublishTwitter error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to assist publish twitter: %v", err)
	}
	return &aiAgentv1.AssistPublishTwitterResponse{
		Code:        200,
		Msg:         "success",
		Response:    result.Response,
		DialogueKey: result.DialogueID,
		RunId:       result.RunID,
	}, nil
}

// ConfirmPublishTwitter 模式三第二阶段：确认发布推文
func (s *AgentServer) ConfirmPublishTwitter(ctx context.Context, req *aiAgentv1.ConfirmPublishTwitterRequest) (*aiAgentv1.ConfirmPublishTwitterResponse, error) {
	log.Printf("gRPC: ConfirmPublishTwitter - user_id=%d", req.UserId)

	result, err := s.svc.ConfirmPublishTwitter(ctx, req.UserId, req.SourceRunId, req.Content)
	if err != nil {
		log.Printf("❌ ConfirmPublishTwitter error: %v", err)
		if errors.Is(err, service.ErrInvalidDraftSourceRun) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "failed to confirm publish twitter: %v", err)
	}

	return &aiAgentv1.ConfirmPublishTwitterResponse{
		Code:     200,
		Msg:      "success",
		Response: "发布成功",
		TweetId:  result.TweetID,
	}, nil
}

// GetRepositoryDialogue 获取历史对话列表
func (s *AgentServer) GetRepositoryDialogue(ctx context.Context, req *aiAgentv1.GetRepositoryDialogueRequest) (*aiAgentv1.GetRepositoryDialogueResponse, error) {
	log.Printf("gRPC: GetRepositoryDialogue - user_id=%d", req.UserId)

	dialogues, _, err := s.svc.ListDialogues(ctx, req.UserId, 1, 50)
	if err != nil {
		log.Printf("❌ GetRepositoryDialogue error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get dialogues: %v", err)
	}

	protoDialogues := make([]*aiAgentv1.RepositoryDialogue, len(dialogues))
	for i, d := range dialogues {
		protoDialogues[i] = &aiAgentv1.RepositoryDialogue{
			Id:          dialogueObjectIDToUint64(d.ID),
			UserId:      d.UserID,
			Title:       d.Title,
			DialogueKey: d.ID.Hex(),
		}
	}

	return &aiAgentv1.GetRepositoryDialogueResponse{
		Code:                   200,
		Msg:                    "success",
		RepositoryDialogueList: protoDialogues,
	}, nil
}

// GetDialogueDetail 获取某个历史对话的详细消息记录
func (s *AgentServer) GetDialogueDetail(ctx context.Context, req *aiAgentv1.GetDialogueDetailRequest) (*aiAgentv1.GetDialogueDetailResponse, error) {
	log.Printf("gRPC: GetDialogueDetail - user_id=%d, dialogue_id=%d", req.UserId, req.DialogueId)

	// 将 uint64 dialogue_id 转回 hex 格式
	dialogueIDHex := req.DialogueKey
	if dialogueIDHex == "" {
		dialogueIDHex = uint64ToObjectIDHex(req.DialogueId)
	}

	messages, err := s.svc.GetDialogueMessages(ctx, req.UserId, dialogueIDHex)
	if err != nil {
		log.Printf("❌ GetDialogueDetail error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get dialogue detail: %v", err)
	}

	protoMessages := make([]*aiAgentv1.RepositoryContentList, len(messages))
	for i, m := range messages {
		question := ""
		response := ""
		if m.Role == "user" {
			question = m.Content
		} else if m.Role == "assistant" {
			response = m.Content
		}

		protoMessages[i] = &aiAgentv1.RepositoryContentList{
			Id:          dialogueObjectIDToUint64(m.ID),
			UserId:      m.UserID,
			DialogueId:  dialogueObjectIDToUint64(m.DialogueID),
			Question:    question,
			Response:    response,
			DialogueKey: m.DialogueID.Hex(),
			Role:        string(m.Role),
			Content:     m.Content,
			RunId:       dialogueMessageMetadataString(m.Metadata, "runtime_run_id"),
			PublishableDraft: m.Role == repository.RoleAssistant &&
				dialogueMessageMetadataString(m.Metadata, "runtime_mode") == "assist",
		}
	}

	return &aiAgentv1.GetDialogueDetailResponse{
		Code:     200,
		Msg:      "success",
		Messages: protoMessages,
	}, nil
}

func dialogueMessageMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}

// GetModelDetailedInformation 获取模型初始化详细信息
// EndDialogueSession finalizes pending episodic memory without deleting the
// dialogue. The service layer enforces ownership and concurrent idempotency.
func (s *AgentServer) EndDialogueSession(ctx context.Context, req *aiAgentv1.EndDialogueSessionRequest) (*aiAgentv1.EndDialogueSessionResponse, error) {
	dialogueIDHex := req.DialogueKey
	if dialogueIDHex == "" {
		dialogueIDHex = uint64ToObjectIDHex(req.DialogueId)
	}
	if err := s.svc.EndDialogueSession(ctx, req.UserId, dialogueIDHex); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to end dialogue session: %v", err)
	}
	return &aiAgentv1.EndDialogueSessionResponse{Code: 200, Msg: "success"}, nil
}

func (s *AgentServer) GetModelDetailedInformation(ctx context.Context, req *aiAgentv1.GetModelDetailedInformationRequest) (*aiAgentv1.GetModelDetailedInformationResponse, error) {
	log.Printf("gRPC: GetModelDetailedInformation - user_id=%d", req.UserId)

	models := s.svc.GetModelInfo()
	protoModels := make([]*aiAgentv1.ModelKind, len(models))
	for i, m := range models {
		fileKinds := make([]*aiAgentv1.FileKind, len(m.FileKinds))
		for j, fk := range m.FileKinds {
			fileKinds[j] = &aiAgentv1.FileKind{
				Id:   fk.ID,
				Name: fk.Name,
			}
		}
		protoModels[i] = &aiAgentv1.ModelKind{
			Id:           m.ID,
			Name:         m.Name,
			Description:  m.Description,
			MaxTokens:    m.MaxTokens,
			FileKindList: fileKinds,
		}
	}

	return &aiAgentv1.GetModelDetailedInformationResponse{
		Code:          200,
		Msg:           "success",
		ModelKindList: protoModels,
	}, nil
}

// AnalysisFiles 解析前端文件（PDF/TXT/图片）
func (s *AgentServer) AnalysisFiles(ctx context.Context, req *aiAgentv1.AnalysisFilesRequest) (*aiAgentv1.AnalysisFilesResponse, error) {
	log.Printf("gRPC: AnalysisFiles - user_id=%d, file_kind_id=%d, file_size=%d", req.UserId, req.FileKindId, len(req.FileContent))

	result, err := s.svc.AnalysisFile(ctx, req.UserId, req.FileKindId, req.FileContent)
	if err != nil {
		log.Printf("❌ AnalysisFiles error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to analysis file: %v", err)
	}

	return &aiAgentv1.AnalysisFilesResponse{
		Code:          200,
		Msg:           "success",
		ParsedContent: result.ParsedContent,
		FileKey:       result.FileKey,
	}, nil
}

// MultiAgentPublishTwitter 模式四：多 Agent 协作写推文
func (s *AgentServer) MultiAgentPublishTwitter(ctx context.Context, req *aiAgentv1.MultiAgentPublishTwitterRequest) (*aiAgentv1.MultiAgentPublishTwitterResponse, error) {
	log.Printf("gRPC: MultiAgentPublishTwitter - user_id=%d, domain=%s", req.UserId, req.Domain)

	result, err := s.svc.MultiAgentPublishTwitter(ctx, req.UserId, req.Domain, req.AuthorUserId, req.StyleRatio, req.ReferenceTweetIds, req.DialogueKey, req.Content)
	if err != nil {
		log.Printf("❌ MultiAgentPublishTwitter error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to multi agent publish twitter: %v", err)
	}

	return &aiAgentv1.MultiAgentPublishTwitterResponse{
		Code:        200,
		Msg:         "success",
		Response:    result.Response,
		DialogueKey: result.DialogueID,
	}, nil
}

// AnalyzeAlert 告警分析根因诊断
func (s *AgentServer) AnalyzeAlert(ctx context.Context, req *aiAgentv1.AnalyzeAlertRequest) (*aiAgentv1.AnalyzeAlertResponse, error) {
	log.Printf("gRPC: AnalyzeAlert received")

	report, structuredRca, err := s.svc.AnalyzeAlert(ctx, req.AlertPayload, req.ErrorLogs)
	if err != nil {
		log.Printf("❌ AnalyzeAlert error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to analyze alert: %v", err)
	}

	return &aiAgentv1.AnalyzeAlertResponse{
		Code:           200,
		Msg:            "success",
		AnalysisReport: report,
		StructuredRca:  structuredRca,
	}, nil
}

func (s *AgentServer) CreateWorkflow(ctx context.Context, req *aiAgentv1.CreateWorkflowRequest) (*aiAgentv1.CreateWorkflowResponse, error) {
	log.Printf("gRPC: CreateWorkflow - user_id=%d, name=%s", req.UserId, req.Name)

	workflow, err := s.svc.CreateWorkflow(ctx, req.UserId, req.Name, req.DslJson)
	if err != nil {
		log.Printf("❌ CreateWorkflow error: %v", err)
		return nil, status.Errorf(codes.InvalidArgument, "failed to create workflow: %v", err)
	}

	return &aiAgentv1.CreateWorkflowResponse{
		Code:     200,
		Msg:      "success",
		Workflow: workflowToProto(workflow),
	}, nil
}

func (s *AgentServer) UpdateWorkflow(ctx context.Context, req *aiAgentv1.UpdateWorkflowRequest) (*aiAgentv1.UpdateWorkflowResponse, error) {
	log.Printf("gRPC: UpdateWorkflow - user_id=%d, workflow_id=%s", req.UserId, req.WorkflowId)

	workflow, err := s.svc.UpdateWorkflow(ctx, req.UserId, req.WorkflowId, req.Name, req.DslJson)
	if err != nil {
		log.Printf("❌ UpdateWorkflow error: %v", err)
		return nil, status.Errorf(codes.InvalidArgument, "failed to update workflow: %v", err)
	}

	return &aiAgentv1.UpdateWorkflowResponse{
		Code:     200,
		Msg:      "success",
		Workflow: workflowToProto(workflow),
	}, nil
}

func (s *AgentServer) ListWorkflows(ctx context.Context, req *aiAgentv1.ListWorkflowsRequest) (*aiAgentv1.ListWorkflowsResponse, error) {
	log.Printf("gRPC: ListWorkflows - user_id=%d", req.UserId)

	workflows, total, err := s.svc.ListWorkflows(ctx, req.UserId, int(req.Page), int(req.PageSize))
	if err != nil {
		log.Printf("❌ ListWorkflows error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to list workflows: %v", err)
	}

	protoWorkflows := make([]*aiAgentv1.WorkflowSummary, 0, len(workflows))
	for _, workflow := range workflows {
		protoWorkflows = append(protoWorkflows, workflowSummaryToProto(workflow))
	}

	return &aiAgentv1.ListWorkflowsResponse{
		Code:      200,
		Msg:       "success",
		Workflows: protoWorkflows,
		Total:     total,
	}, nil
}

func (s *AgentServer) GetWorkflow(ctx context.Context, req *aiAgentv1.GetWorkflowRequest) (*aiAgentv1.GetWorkflowResponse, error) {
	log.Printf("gRPC: GetWorkflow - user_id=%d, workflow_id=%s", req.UserId, req.WorkflowId)

	workflow, err := s.svc.GetWorkflow(ctx, req.UserId, req.WorkflowId)
	if err != nil {
		log.Printf("❌ GetWorkflow error: %v", err)
		return nil, status.Errorf(codes.NotFound, "failed to get workflow: %v", err)
	}

	return &aiAgentv1.GetWorkflowResponse{
		Code:     200,
		Msg:      "success",
		Workflow: workflowToProto(workflow),
	}, nil
}

func (s *AgentServer) ListWorkflowRevisions(ctx context.Context, req *aiAgentv1.ListWorkflowRevisionsRequest) (*aiAgentv1.ListWorkflowRevisionsResponse, error) {
	revisions, total, err := s.svc.ListWorkflowRevisions(ctx, req.UserId, req.WorkflowId, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list workflow revisions: %v", err)
	}
	items := make([]*aiAgentv1.WorkflowRevisionSummary, 0, len(revisions))
	for _, revision := range revisions {
		items = append(items, workflowRevisionSummaryToProto(revision))
	}
	return &aiAgentv1.ListWorkflowRevisionsResponse{
		Code: 200, Msg: "success", Revisions: items, Total: total,
	}, nil
}

func (s *AgentServer) GetWorkflowRevision(ctx context.Context, req *aiAgentv1.GetWorkflowRevisionRequest) (*aiAgentv1.GetWorkflowRevisionResponse, error) {
	revision, err := s.svc.GetWorkflowRevision(ctx, req.UserId, req.WorkflowId, req.RevisionId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "failed to get workflow revision: %v", err)
	}
	return &aiAgentv1.GetWorkflowRevisionResponse{
		Code: 200, Msg: "success", Revision: workflowRevisionToProto(revision),
	}, nil
}

func (s *AgentServer) PublishWorkflowTool(
	ctx context.Context,
	req *aiAgentv1.PublishWorkflowToolRequest,
) (*aiAgentv1.PublishWorkflowToolResponse, error) {
	if req == nil || req.UserId == 0 || strings.TrimSpace(req.WorkflowId) == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and workflow_id are required")
	}
	publication, err := s.svc.PublishWorkflowTool(
		ctx,
		req.UserId,
		req.WorkflowId,
		service.PublishWorkflowToolInput{
			WorkflowRevisionID: req.WorkflowRevisionId,
			Description:        req.Description,
			InputSchemaJSON:    req.InputSchemaJson,
			ExpectedRevision:   req.ExpectedRevision,
		},
	)
	if err != nil {
		return nil, workflowToolPublicationGRPCError(err, codes.InvalidArgument)
	}
	return &aiAgentv1.PublishWorkflowToolResponse{
		Code: 200, Msg: "success", Publication: workflowToolPublicationToProto(publication),
	}, nil
}

func (s *AgentServer) GetWorkflowToolPublication(
	ctx context.Context,
	req *aiAgentv1.GetWorkflowToolPublicationRequest,
) (*aiAgentv1.GetWorkflowToolPublicationResponse, error) {
	if req == nil || req.UserId == 0 || strings.TrimSpace(req.WorkflowId) == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and workflow_id are required")
	}
	publication, err := s.svc.GetWorkflowToolPublication(ctx, req.UserId, req.WorkflowId)
	if err != nil {
		return nil, workflowToolPublicationGRPCError(err, codes.Internal)
	}
	return &aiAgentv1.GetWorkflowToolPublicationResponse{
		Code: 200, Msg: "success", Publication: workflowToolPublicationToProto(publication),
	}, nil
}

func (s *AgentServer) UnpublishWorkflowTool(
	ctx context.Context,
	req *aiAgentv1.UnpublishWorkflowToolRequest,
) (*aiAgentv1.UnpublishWorkflowToolResponse, error) {
	if req == nil || req.UserId == 0 || strings.TrimSpace(req.WorkflowId) == "" ||
		req.ExpectedRevision < 1 {
		return nil, status.Error(
			codes.InvalidArgument,
			"user_id, workflow_id and expected_revision are required",
		)
	}
	publication, err := s.svc.UnpublishWorkflowTool(
		ctx,
		req.UserId,
		req.WorkflowId,
		req.ExpectedRevision,
	)
	if err != nil {
		return nil, workflowToolPublicationGRPCError(err, codes.Internal)
	}
	return &aiAgentv1.UnpublishWorkflowToolResponse{
		Code: 200, Msg: "success", Publication: workflowToolPublicationToProto(publication),
	}, nil
}

func (s *AgentServer) RunWorkflow(ctx context.Context, req *aiAgentv1.RunWorkflowRequest) (*aiAgentv1.RunWorkflowResponse, error) {
	log.Printf("gRPC: RunWorkflow - user_id=%d, workflow_id=%s", req.UserId, req.WorkflowId)

	result, err := s.svc.RunWorkflowRevision(ctx, req.UserId, req.WorkflowId, req.WorkflowRevisionId, req.InputJson)
	if err != nil {
		log.Printf("❌ RunWorkflow error: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to run workflow: %v", err)
	}

	return &aiAgentv1.RunWorkflowResponse{
		Code:        200,
		Msg:         "success",
		Run:         workflowRunToProto(result.Run),
		DialogueKey: result.DialogueKey,
		Response:    result.Response,
		ResumeToken: result.ResumeToken,
	}, nil
}

func (s *AgentServer) GetWorkflowRun(ctx context.Context, req *aiAgentv1.GetWorkflowRunRequest) (*aiAgentv1.GetWorkflowRunResponse, error) {
	log.Printf("gRPC: GetWorkflowRun - user_id=%d, run_id=%s", req.UserId, req.RunId)

	run, err := s.svc.GetWorkflowRun(ctx, req.UserId, req.RunId)
	if err != nil {
		log.Printf("❌ GetWorkflowRun error: %v", err)
		return nil, status.Errorf(codes.NotFound, "failed to get workflow run: %v", err)
	}

	return &aiAgentv1.GetWorkflowRunResponse{
		Code: 200,
		Msg:  "success",
		Run:  workflowRunToProto(run),
	}, nil
}

func (s *AgentServer) GetWorkflowRunTrace(ctx context.Context, req *aiAgentv1.GetWorkflowRunTraceRequest) (*aiAgentv1.GetWorkflowRunTraceResponse, error) {
	bundle, err := s.svc.GetWorkflowRunTrace(ctx, req.UserId, req.RunId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "failed to get workflow run trace: %v", err)
	}
	response := &aiAgentv1.GetWorkflowRunTraceResponse{Code: 200, Msg: "success"}
	if bundle.Run != nil {
		response.Run = executionRunTraceToProto(bundle.Run)
	}
	for index := range bundle.Steps {
		response.Steps = append(response.Steps, executionStepTraceToProto(&bundle.Steps[index]))
	}
	for index := range bundle.LLMCalls {
		response.LlmCalls = append(response.LlmCalls, executionLLMCallTraceToProto(&bundle.LLMCalls[index]))
	}
	for index := range bundle.ToolCalls {
		response.ToolCalls = append(response.ToolCalls, executionToolCallTraceToProto(&bundle.ToolCalls[index]))
	}
	return response, nil
}

func (s *AgentServer) SearchWorkflowBlackboard(ctx context.Context, req *aiAgentv1.SearchWorkflowBlackboardRequest) (*aiAgentv1.SearchWorkflowBlackboardResponse, error) {
	result, err := s.svc.SearchWorkflowBlackboard(ctx, req.UserId, req.RunId, service.WorkflowBlackboardSearchRequest{
		StateVersion: req.StateVersion,
		Query:        req.Query,
		PathPrefix:   req.PathPrefix,
		AfterCursor:  req.AfterCursor,
		PageSize:     int(req.PageSize),
	})
	if err != nil {
		if errors.Is(err, service.ErrWorkflowBlackboardInvalidQuery) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Errorf(codes.FailedPrecondition, "failed to search workflow blackboard: %v", err)
	}
	entries := make([]*aiAgentv1.WorkflowBlackboardEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, &aiAgentv1.WorkflowBlackboardEntry{
			Path: entry.Path, ValueJson: entry.ValueJSON, ValueType: entry.ValueType,
			ValueHash: entry.ValueHash, ValueLength: entry.ValueLength, Truncated: entry.Truncated,
		})
	}
	return &aiAgentv1.SearchWorkflowBlackboardResponse{
		Code: 200, Msg: "success", RunId: result.RunID, StateVersion: result.StateVersion,
		BaseSnapshotVersion: result.BaseSnapshotVersion, BaseSnapshotHash: result.BaseSnapshotHash,
		StateHash: result.StateHash, Verified: result.Verified, Entries: entries,
		MatchedTotal: result.MatchedTotal, NextCursor: result.NextCursor, HasMore: result.HasMore,
	}, nil
}

func (s *AgentServer) WatchWorkflowRunEvents(
	req *aiAgentv1.WatchWorkflowRunEventsRequest,
	stream grpc.ServerStreamingServer[aiAgentv1.WorkflowRunEvent],
) error {
	err := s.svc.WatchWorkflowRunEvents(
		stream.Context(), req.UserId, req.RunId, req.AfterCursor,
		func(event agentObservability.TraceEvent) error {
			return stream.Send(workflowRunEventToProto(event))
		},
	)
	if err != nil {
		return status.Error(codes.FailedPrecondition, "workflow run event stream unavailable")
	}
	return nil
}

func (s *AgentServer) ListWorkflowRuns(ctx context.Context, req *aiAgentv1.ListWorkflowRunsRequest) (*aiAgentv1.ListWorkflowRunsResponse, error) {
	runs, total, err := s.svc.ListWorkflowRuns(
		ctx, req.UserId, req.WorkflowId, req.Status, int(req.Page), int(req.PageSize),
	)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to list workflow runs: %v", err)
	}
	items := make([]*aiAgentv1.WorkflowRunSummary, 0, len(runs))
	for _, run := range runs {
		items = append(items, workflowRunSummaryToProto(run))
	}
	return &aiAgentv1.ListWorkflowRunsResponse{Code: 200, Msg: "success", Runs: items, Total: total}, nil
}

func (s *AgentServer) CancelWorkflowRun(ctx context.Context, req *aiAgentv1.CancelWorkflowRunRequest) (*aiAgentv1.CancelWorkflowRunResponse, error) {
	run, err := s.svc.CancelWorkflowRun(ctx, req.UserId, req.RunId, req.Reason)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to cancel workflow run: %v", err)
	}
	return &aiAgentv1.CancelWorkflowRunResponse{
		Code: 200, Msg: "success", Run: workflowRunSummaryToProto(run),
	}, nil
}

func (s *AgentServer) GetWorkflowRunReplay(ctx context.Context, req *aiAgentv1.GetWorkflowRunReplayRequest) (*aiAgentv1.GetWorkflowRunReplayResponse, error) {
	replay, err := s.svc.GetWorkflowRunReplay(ctx, req.UserId, req.RunId)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to get workflow run replay: %v", err)
	}
	events := make([]*aiAgentv1.WorkflowReplayStateEvent, 0, len(replay.Events))
	for _, event := range replay.Events {
		events = append(events, &aiAgentv1.WorkflowReplayStateEvent{
			Sequence: event.Sequence, NodeId: event.NodeID, DeltaJson: event.DeltaJSON,
			EventHash: event.EventHash, AppliedAt: event.AppliedAt,
		})
	}
	compensations := make([]*aiAgentv1.WorkflowReplayCompensation, 0, len(replay.Compensations))
	for _, compensation := range replay.Compensations {
		compensations = append(compensations, &aiAgentv1.WorkflowReplayCompensation{
			Sequence: int32(compensation.Sequence), SourceNodeId: compensation.SourceNodeID,
			StepId: compensation.StepID, ToolName: compensation.ToolName,
			InputHash: compensation.InputHash, PlanHash: compensation.PlanHash,
			Status: compensation.Status, Attempt: int32(compensation.Attempt),
			ErrorMessage: compensation.ErrorMessage, ApprovalRequestId: compensation.ApprovalRequestID,
			LeaseUntil: compensation.LeaseUntil, CreatedAt: compensation.CreatedAt,
			UpdatedAt: compensation.UpdatedAt, FinishedAt: compensation.FinishedAt,
		})
	}
	response := &aiAgentv1.GetWorkflowRunReplayResponse{
		Code: 200, Msg: "success", Run: workflowRunToProto(replay.Run), Events: events,
		Compensations: compensations,
		Integrity: &aiAgentv1.WorkflowReplayIntegrity{
			Verified: replay.Integrity.Verified, StateVersion: replay.Integrity.StateVersion,
			EventCount: replay.Integrity.EventCount, LastSequence: replay.Integrity.LastSequence,
			SnapshotVersion: replay.Integrity.SnapshotVersion,
		},
	}
	if replay.Revision != nil {
		response.Revision = workflowRevisionSummaryToProto(replay.Revision)
	}
	if replay.Snapshot != nil {
		response.Snapshot = &aiAgentv1.WorkflowReplaySnapshot{
			StateVersion: replay.Snapshot.StateVersion, SnapshotHash: replay.Snapshot.SnapshotHash,
			CreatedAt: replay.Snapshot.CreatedAt,
		}
	}
	return response, nil
}

func (s *AgentServer) GetWorkflowCompensationJournal(ctx context.Context, req *aiAgentv1.GetWorkflowCompensationJournalRequest) (*aiAgentv1.GetWorkflowCompensationJournalResponse, error) {
	journal, err := s.svc.GetWorkflowCompensationJournal(ctx, req.UserId, req.RunId)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to get workflow compensation journal: %v", err)
	}
	entries := make([]*aiAgentv1.WorkflowCompensationJournalEntry, 0, len(journal.Entries))
	for _, entry := range journal.Entries {
		entries = append(entries, &aiAgentv1.WorkflowCompensationJournalEntry{
			Sequence: int32(entry.Sequence), SourceNodeId: entry.SourceNodeID,
			StepId: entry.StepID, ToolName: entry.ToolName,
			InputHash: entry.InputHash, PlanHash: entry.PlanHash,
			Status: entry.Status, Attempt: int32(entry.Attempt), ErrorMessage: entry.ErrorMessage,
			ApprovalRequestId: entry.ApprovalRequestID, LeaseUntil: entry.LeaseUntil,
			CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt, FinishedAt: entry.FinishedAt,
			IsNext: entry.IsNext,
		})
	}
	return &aiAgentv1.GetWorkflowCompensationJournalResponse{
		Code: 200, Msg: "success", Run: workflowCompensationRunSummaryToProto(journal.Run), Entries: entries,
		NextSequence: int32(journal.NextSequence), RetryAvailable: journal.RetryAvailable,
	}, nil
}

func (s *AgentServer) RetryWorkflowCompensation(ctx context.Context, req *aiAgentv1.RetryWorkflowCompensationRequest) (*aiAgentv1.RetryWorkflowCompensationResponse, error) {
	result, err := s.svc.RetryWorkflowCompensation(ctx, req.UserId, req.RunId)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "failed to retry workflow compensation: %v", err)
	}
	return &aiAgentv1.RetryWorkflowCompensationResponse{
		Code: 200, Msg: "success", Run: workflowRunToProto(result.Run),
		Response: result.Response, ResumeToken: result.ResumeToken,
	}, nil
}

func (s *AgentServer) ListToolApprovals(ctx context.Context, req *aiAgentv1.ListToolApprovalsRequest) (*aiAgentv1.ListToolApprovalsResponse, error) {
	approvals, total, err := s.svc.ListToolApprovals(ctx, req.UserId, req.Status, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to list tool approvals: %v", err)
	}
	items := make([]*aiAgentv1.ToolApprovalRequest, 0, len(approvals))
	for _, approval := range approvals {
		items = append(items, toolApprovalToProto(approval))
	}
	return &aiAgentv1.ListToolApprovalsResponse{Code: 200, Msg: "success", Approvals: items, Total: total}, nil
}

func (s *AgentServer) DecideToolApproval(ctx context.Context, req *aiAgentv1.DecideToolApprovalRequest) (*aiAgentv1.DecideToolApprovalResponse, error) {
	approval, err := s.svc.DecideToolApproval(
		ctx, req.UserId, req.ApprovalId, req.Decision, req.Reason, req.ExpectedRevision,
	)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "failed to decide tool approval: %v", err)
	}
	return &aiAgentv1.DecideToolApprovalResponse{Code: 200, Msg: "success", Approval: toolApprovalToProto(approval)}, nil
}

func (s *AgentServer) IssueWorkflowResumeGrant(ctx context.Context, req *aiAgentv1.IssueWorkflowResumeGrantRequest) (*aiAgentv1.IssueWorkflowResumeGrantResponse, error) {
	grant, err := s.svc.IssueWorkflowResumeGrant(ctx, req.UserId, req.ApprovalId, req.ExpectedRunRevision)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "failed to issue workflow resume grant: %v", err)
	}
	return &aiAgentv1.IssueWorkflowResumeGrantResponse{
		Code: 200, Msg: "success", Run: workflowRunToProto(grant.Run),
		ResumeToken: grant.ResumeToken, ExpiresAt: unixOrZero(grant.ExpiresAt),
	}, nil
}

func (s *AgentServer) ResumeWorkflowRun(ctx context.Context, req *aiAgentv1.ResumeWorkflowRunRequest) (*aiAgentv1.ResumeWorkflowRunResponse, error) {
	result, err := s.svc.ResumeWorkflowRun(
		ctx, req.UserId, req.RunId, req.ApprovalId, req.ResumeToken, req.InputJson,
	)
	if err != nil {
		return nil, status.Errorf(codes.Aborted, "failed to resume workflow run: %v", err)
	}
	return &aiAgentv1.ResumeWorkflowRunResponse{
		Code: 200, Msg: "success", Run: workflowRunToProto(result.Run),
		Response: result.Response, ResumeToken: result.ResumeToken,
	}, nil
}

func (s *AgentServer) CreateProviderConfig(ctx context.Context, req *aiAgentv1.CreateProviderConfigRequest) (*aiAgentv1.CreateProviderConfigResponse, error) {
	config, err := s.svc.CreateProviderConfig(ctx, req.UserId, service.ProviderConfigInput{
		Kind: req.Kind, Name: req.Name, Provider: req.Provider, BaseURL: req.BaseUrl, Model: req.Model, APIKey: req.ApiKey,
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to create provider config: %v", err)
	}
	return &aiAgentv1.CreateProviderConfigResponse{Code: 200, Msg: "success", ProviderConfig: providerConfigToProto(config)}, nil
}

func (s *AgentServer) UpdateProviderConfig(ctx context.Context, req *aiAgentv1.UpdateProviderConfigRequest) (*aiAgentv1.UpdateProviderConfigResponse, error) {
	config, err := s.svc.UpdateProviderConfig(ctx, req.UserId, req.ProviderConfigId, service.ProviderConfigInput{
		Kind: req.Kind, Name: req.Name, Provider: req.Provider, BaseURL: req.BaseUrl, Model: req.Model,
		APIKey: req.ApiKey, Revision: req.Revision,
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to update provider config: %v", err)
	}
	return &aiAgentv1.UpdateProviderConfigResponse{Code: 200, Msg: "success", ProviderConfig: providerConfigToProto(config)}, nil
}

func (s *AgentServer) ListProviderConfigs(ctx context.Context, req *aiAgentv1.ListProviderConfigsRequest) (*aiAgentv1.ListProviderConfigsResponse, error) {
	configs, total, err := s.svc.ListProviderConfigsByKind(ctx, req.UserId, req.Kind, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list provider configs: %v", err)
	}
	items := make([]*aiAgentv1.ProviderConfig, 0, len(configs))
	for _, config := range configs {
		items = append(items, providerConfigToProto(config))
	}
	return &aiAgentv1.ListProviderConfigsResponse{Code: 200, Msg: "success", ProviderConfigs: items, Total: total}, nil
}

func (s *AgentServer) GetProviderConfig(ctx context.Context, req *aiAgentv1.GetProviderConfigRequest) (*aiAgentv1.GetProviderConfigResponse, error) {
	config, err := s.svc.GetProviderConfig(ctx, req.UserId, req.ProviderConfigId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "failed to get provider config: %v", err)
	}
	return &aiAgentv1.GetProviderConfigResponse{Code: 200, Msg: "success", ProviderConfig: providerConfigToProto(config)}, nil
}

func (s *AgentServer) RevokeProviderConfig(ctx context.Context, req *aiAgentv1.RevokeProviderConfigRequest) (*aiAgentv1.RevokeProviderConfigResponse, error) {
	if err := s.svc.RevokeProviderConfig(ctx, req.UserId, req.ProviderConfigId, req.Revision); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "failed to revoke provider config: %v", err)
	}
	return &aiAgentv1.RevokeProviderConfigResponse{Code: 200, Msg: "success"}, nil
}

func (s *AgentServer) CreateExternalMCPConnection(
	ctx context.Context,
	req *aiAgentv1.CreateExternalMCPConnectionRequest,
) (*aiAgentv1.CreateExternalMCPConnectionResponse, error) {
	connection, err := s.svc.CreateExternalMCPConnection(ctx, req.UserId, externalMCPInput(
		req.Scope, req.ProjectId, req.Name, req.Transport, req.Endpoint, req.AuthType,
		req.CredentialSource, req.ManagedCredentialRef, req.BearerToken,
	))
	if err != nil {
		return nil, externalMCPControlError(err, codes.InvalidArgument)
	}
	return &aiAgentv1.CreateExternalMCPConnectionResponse{
		Code: 200, Msg: "success", Connection: externalMCPConnectionToProto(connection),
	}, nil
}

func (s *AgentServer) UpdateExternalMCPConnection(
	ctx context.Context,
	req *aiAgentv1.UpdateExternalMCPConnectionRequest,
) (*aiAgentv1.UpdateExternalMCPConnectionResponse, error) {
	connection, err := s.svc.UpdateExternalMCPConnection(
		ctx, req.UserId, req.ConnectionId, req.ExpectedRevision,
		externalMCPInput(
			req.Scope, req.ProjectId, req.Name, req.Transport, req.Endpoint, req.AuthType,
			req.CredentialSource, req.ManagedCredentialRef, req.BearerToken,
		),
	)
	if err != nil {
		return nil, externalMCPControlError(err, codes.InvalidArgument)
	}
	return &aiAgentv1.UpdateExternalMCPConnectionResponse{
		Code: 200, Msg: "success", Connection: externalMCPConnectionToProto(connection),
	}, nil
}

func (s *AgentServer) ListExternalMCPConnections(
	ctx context.Context,
	req *aiAgentv1.ListExternalMCPConnectionsRequest,
) (*aiAgentv1.ListExternalMCPConnectionsResponse, error) {
	connections, total, err := s.svc.ListExternalMCPConnections(ctx, req.UserId, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, externalMCPControlError(err, codes.Internal)
	}
	items := make([]*aiAgentv1.ExternalMCPConnection, 0, len(connections))
	for _, connection := range connections {
		items = append(items, externalMCPConnectionToProto(connection))
	}
	return &aiAgentv1.ListExternalMCPConnectionsResponse{
		Code: 200, Msg: "success", Connections: items, Total: total,
	}, nil
}

func (s *AgentServer) GetExternalMCPConnection(
	ctx context.Context,
	req *aiAgentv1.GetExternalMCPConnectionRequest,
) (*aiAgentv1.GetExternalMCPConnectionResponse, error) {
	connection, err := s.svc.GetExternalMCPConnection(ctx, req.UserId, req.ConnectionId)
	if err != nil {
		return nil, externalMCPControlError(err, codes.NotFound)
	}
	return &aiAgentv1.GetExternalMCPConnectionResponse{
		Code: 200, Msg: "success", Connection: externalMCPConnectionToProto(connection),
	}, nil
}

func (s *AgentServer) RevokeExternalMCPConnection(
	ctx context.Context,
	req *aiAgentv1.RevokeExternalMCPConnectionRequest,
) (*aiAgentv1.RevokeExternalMCPConnectionResponse, error) {
	if err := s.svc.RevokeExternalMCPConnection(ctx, req.UserId, req.ConnectionId, req.ExpectedRevision); err != nil {
		return nil, externalMCPControlError(err, codes.InvalidArgument)
	}
	return &aiAgentv1.RevokeExternalMCPConnectionResponse{Code: 200, Msg: "success"}, nil
}

func (s *AgentServer) DiscoverExternalMCPTools(
	ctx context.Context,
	req *aiAgentv1.DiscoverExternalMCPToolsRequest,
) (*aiAgentv1.DiscoverExternalMCPToolsResponse, error) {
	connection, snapshot, err := s.svc.DiscoverExternalMCPTools(ctx, req.UserId, req.ConnectionId, req.ExpectedRevision)
	if err != nil {
		return nil, externalMCPDiscoveryError(err)
	}
	return &aiAgentv1.DiscoverExternalMCPToolsResponse{
		Code: 200, Msg: "success", Connection: externalMCPConnectionToProto(connection),
		Snapshot: externalMCPSnapshotToProto(snapshot),
	}, nil
}

func (s *AgentServer) ApproveExternalMCPSnapshot(
	ctx context.Context,
	req *aiAgentv1.ApproveExternalMCPSnapshotRequest,
) (*aiAgentv1.ApproveExternalMCPSnapshotResponse, error) {
	connection, snapshot, err := s.svc.ApproveExternalMCPSnapshot(
		ctx, req.UserId, req.ConnectionId, req.SnapshotId, req.ExpectedRevision,
	)
	if err != nil {
		return nil, externalMCPControlError(err, codes.InvalidArgument)
	}
	return &aiAgentv1.ApproveExternalMCPSnapshotResponse{
		Code: 200, Msg: "success", Connection: externalMCPConnectionToProto(connection),
		Snapshot: externalMCPSnapshotToProto(snapshot),
	}, nil
}

func (s *AgentServer) ListExternalMCPTools(
	ctx context.Context,
	req *aiAgentv1.ListExternalMCPToolsRequest,
) (*aiAgentv1.ListExternalMCPToolsResponse, error) {
	connection, snapshot, tools, err := s.svc.ListExternalMCPTools(ctx, req.UserId, req.ConnectionId)
	if err != nil {
		return nil, externalMCPControlError(err, codes.FailedPrecondition)
	}
	items := make([]*aiAgentv1.ExternalMCPToolView, 0, len(tools))
	for _, tool := range tools {
		items = append(items, externalMCPToolViewToProto(tool))
	}
	return &aiAgentv1.ListExternalMCPToolsResponse{
		Code: 200, Msg: "success", Connection: externalMCPConnectionToProto(connection),
		Snapshot: externalMCPSnapshotToProto(snapshot), Tools: items,
	}, nil
}

func (s *AgentServer) ConfigureExternalMCPTool(
	ctx context.Context,
	req *aiAgentv1.ConfigureExternalMCPToolRequest,
) (*aiAgentv1.ConfigureExternalMCPToolResponse, error) {
	connection, tool, err := s.svc.ConfigureExternalMCPTool(
		ctx,
		req.UserId,
		req.ConnectionId,
		req.ExpectedRevision,
		externalmcp.ToolPolicyInput{
			SnapshotID: req.SnapshotId,
			ToolName:   req.ToolName,
			Category:   req.Category,
			Enabled:    req.Enabled,
		},
	)
	if err != nil {
		return nil, externalMCPControlError(err, codes.InvalidArgument)
	}
	return &aiAgentv1.ConfigureExternalMCPToolResponse{
		Code: 200, Msg: "success", Connection: externalMCPConnectionToProto(connection),
		Tool: externalMCPToolViewToProto(tool),
	}, nil
}

// ========================== 辅助函数 ==========================

func workflowSummaryToProto(workflow *repository.WorkflowDefinition) *aiAgentv1.WorkflowSummary {
	currentRevisionID := ""
	if !workflow.CurrentRevisionID.IsZero() {
		currentRevisionID = workflow.CurrentRevisionID.Hex()
	}
	return &aiAgentv1.WorkflowSummary{
		WorkflowId:            workflow.ID.Hex(),
		UserId:                workflow.UserID,
		Name:                  workflow.Name,
		CreatedAt:             workflow.CreatedAt.Unix(),
		UpdatedAt:             workflow.UpdatedAt.Unix(),
		CurrentRevisionId:     currentRevisionID,
		CurrentRevisionNumber: workflow.CurrentRevisionNumber,
		CurrentDslHash:        workflow.CurrentDSLHash,
	}
}

func workflowToProto(workflow *repository.WorkflowDefinition) *aiAgentv1.WorkflowDetail {
	currentRevisionID := ""
	if !workflow.CurrentRevisionID.IsZero() {
		currentRevisionID = workflow.CurrentRevisionID.Hex()
	}
	return &aiAgentv1.WorkflowDetail{
		WorkflowId:            workflow.ID.Hex(),
		UserId:                workflow.UserID,
		Name:                  workflow.Name,
		DslJson:               workflow.DSLJSON,
		CreatedAt:             workflow.CreatedAt.Unix(),
		UpdatedAt:             workflow.UpdatedAt.Unix(),
		CurrentRevisionId:     currentRevisionID,
		CurrentRevisionNumber: workflow.CurrentRevisionNumber,
		CurrentDslHash:        workflow.CurrentDSLHash,
	}
}

func workflowRevisionSummaryToProto(revision *repository.WorkflowRevision) *aiAgentv1.WorkflowRevisionSummary {
	return &aiAgentv1.WorkflowRevisionSummary{
		RevisionId: revision.ID.Hex(), WorkflowId: revision.WorkflowID.Hex(), UserId: revision.UserID,
		RevisionNumber: revision.RevisionNumber, DslHash: revision.DSLHash,
		CreatedAt: unixOrZero(revision.CreatedAt),
	}
}

func workflowRevisionToProto(revision *repository.WorkflowRevision) *aiAgentv1.WorkflowRevisionDetail {
	return &aiAgentv1.WorkflowRevisionDetail{
		RevisionId: revision.ID.Hex(), WorkflowId: revision.WorkflowID.Hex(), UserId: revision.UserID,
		RevisionNumber: revision.RevisionNumber, DslJson: revision.DSLJSON, DslHash: revision.DSLHash,
		CreatedAt: unixOrZero(revision.CreatedAt),
	}
}

func workflowRunToProto(run *repository.WorkflowRunRecord) *aiAgentv1.WorkflowRun {
	approvalRequestID := ""
	if !run.ApprovalRequestID.IsZero() {
		approvalRequestID = run.ApprovalRequestID.Hex()
	}
	workflowRevisionID := ""
	if !run.WorkflowRevisionID.IsZero() {
		workflowRevisionID = run.WorkflowRevisionID.Hex()
	}
	return &aiAgentv1.WorkflowRun{
		RunId:                  run.ID.Hex(),
		WorkflowId:             run.WorkflowID.Hex(),
		UserId:                 run.UserID,
		Status:                 run.Status,
		InputJson:              run.InputJSON,
		OutputJson:             run.OutputJSON,
		ErrorMessage:           run.ErrorMessage,
		StartedAt:              unixOrZero(run.StartedAt),
		FinishedAt:             unixOrZero(run.FinishedAt),
		WaitingNodeId:          run.WaitingNodeID,
		SuspendedAt:            unixOrZero(run.SuspendedAt),
		ApprovalRequestId:      approvalRequestID,
		Revision:               run.Revision,
		WorkflowRevisionId:     workflowRevisionID,
		WorkflowRevisionNumber: run.WorkflowRevisionNumber,
		StateVersion:           run.StateVersion,
		CancelRequestedAt:      unixOrZero(run.CancelRequestedAt),
		CancelReason:           run.CancelReason,
		CanceledAt:             unixOrZero(run.CanceledAt),
		ResumeGrantIssuedAt:    unixOrZero(run.ResumeGrantIssuedAt),
		ResumeGrantExpiresAt:   unixOrZero(run.ResumeGrantExpiresAt),
		InvocationSource:       run.InvocationSource,
		ParentRunId:            run.ParentRunID,
		ParentActionId:         run.ParentActionID,
	}
}

func workflowToolPublicationToProto(
	publication *repository.WorkflowToolPublication,
) *aiAgentv1.WorkflowToolPublication {
	if publication == nil {
		return nil
	}
	return &aiAgentv1.WorkflowToolPublication{
		PublicationId:          publication.ID.Hex(),
		UserId:                 publication.UserID,
		WorkflowId:             publication.WorkflowID.Hex(),
		WorkflowRevisionId:     publication.WorkflowRevisionID.Hex(),
		WorkflowRevisionNumber: publication.WorkflowRevisionNumber,
		WorkflowDslHash:        publication.WorkflowDSLHash,
		ToolName:               publication.ToolName,
		DisplayName:            publication.DisplayName,
		Description:            publication.Description,
		InputSchemaJson:        publication.InputSchemaJSON,
		Status:                 publication.Status,
		Revision:               publication.Revision,
		CreatedAt:              unixOrZero(publication.CreatedAt),
		UpdatedAt:              unixOrZero(publication.UpdatedAt),
	}
}

func agentSkillToProto(version skill.Version) *aiAgentv1.AgentSkill {
	knowledge := make([]*aiAgentv1.AgentSkillKnowledgeBinding, 0, len(version.Knowledge))
	for _, binding := range version.Knowledge {
		knowledge = append(knowledge, &aiAgentv1.AgentSkillKnowledgeBinding{
			Kind: binding.Kind, Reference: binding.Reference, Version: binding.Version,
		})
	}
	result := &aiAgentv1.AgentSkill{
		ContractVersion: version.ContractVersion,
		SkillId:         version.ID,
		Version:         version.Version,
		DisplayName:     version.DisplayName,
		Description:     version.Description,
		Instructions:    version.Instructions,
		Source:          version.Source,
		AllowedTools:    append([]string(nil), version.AllowedTools...),
		Knowledge:       knowledge,
		Profile: &aiAgentv1.AgentSkillProfileBinding{
			ProfileId:      version.Profile.ID,
			ProfileVersion: version.Profile.Version,
			PromptId:       version.Profile.PromptID,
			PromptVersion:  version.Profile.PromptVersion,
		},
		Budget: &aiAgentv1.AgentSkillBudget{
			MaxSteps:               int32(version.Budget.MaxSteps),
			MaxInputTokens:         int32(version.Budget.MaxInputTokens),
			MaxOutputTokens:        int32(version.Budget.MaxOutputTokens),
			MaxTotalTokens:         int32(version.Budget.MaxTotalTokens),
			MaxEstimatedCostMicros: version.Budget.MaxEstimatedCostMicros,
			TimeoutSeconds:         int64(version.Budget.Timeout / time.Second),
		},
		Output: &aiAgentv1.AgentSkillOutputContract{
			SchemaId:    version.Output.SchemaID,
			ContentType: version.Output.ContentType,
			SchemaJson:  string(version.Output.SchemaJSON),
		},
	}
	if version.Workflow != nil {
		result.Workflow = &aiAgentv1.AgentSkillWorkflowBinding{
			PublicationId:          version.Workflow.PublicationID,
			PublicationRevision:    version.Workflow.PublicationRevision,
			WorkflowId:             version.Workflow.WorkflowID,
			WorkflowRevisionId:     version.Workflow.WorkflowRevisionID,
			WorkflowRevisionNumber: version.Workflow.WorkflowRevisionNumber,
			WorkflowDslHash:        version.Workflow.WorkflowDSLHash,
			ToolName:               version.Workflow.ToolName,
			InputSchemaJson:        string(version.Workflow.InputSchemaJSON),
		}
	}
	return result
}

func agentExtensionToProto(entry extension.Entry) *aiAgentv1.AgentExtension {
	result := &aiAgentv1.AgentExtension{
		ContractVersion: entry.ContractVersion,
		ExtensionId:     entry.ID,
		Kind:            entry.Kind,
		Name:            entry.Name,
		DisplayName:     entry.DisplayName,
		Description:     entry.Description,
		Version:         entry.Version,
		Source:          entry.Source,
		CapabilityId:    entry.CapabilityID,
		Category:        entry.Category,
		Scope:           entry.Scope,
		Status:          entry.Status,
		ApprovalMode:    entry.ApprovalMode,
		HealthStatus:    entry.HealthStatus,
	}
	if entry.Skill != nil {
		result.Skill = &aiAgentv1.AgentExtensionSkillReference{
			SkillId: entry.Skill.SkillID, Version: entry.Skill.Version,
		}
	}
	if entry.MCP != nil {
		result.Mcp = &aiAgentv1.AgentExtensionMCPReference{
			ConnectionId:      entry.MCP.ConnectionID,
			ServerId:          entry.MCP.ServerID,
			SnapshotId:        entry.MCP.SnapshotID,
			QualifiedToolName: entry.MCP.QualifiedToolName,
		}
	}
	return result
}

func agentMarketplaceExtensionToProto(listing marketplace.Listing) *aiAgentv1.AgentMarketplaceExtension {
	return &aiAgentv1.AgentMarketplaceExtension{
		ContractVersion: listing.ContractVersion,
		ReleaseId:       listing.ReleaseID,
		PackageId:       listing.PackageID,
		Kind:            listing.Kind,
		Version:         listing.Version,
		DisplayName:     listing.DisplayName,
		Description:     listing.Description,
		Publisher: &aiAgentv1.AgentMarketplacePublisher{
			PublisherId:  listing.Publisher.PublisherID,
			DisplayName:  listing.Publisher.DisplayName,
			Verification: listing.Publisher.Verification,
		},
		ArtifactDigestSha256: listing.ArtifactDigestSHA256,
		SignatureKeyId:       listing.SignatureKeyID,
		CapabilityIds:        append([]string(nil), listing.CapabilityIDs...),
		RequestedPermissions: append([]string(nil), listing.RequestedPermissions...),
		PublishedAtUnixMs:    listing.PublishedAt.UnixMilli(),
		SignatureVerified:    listing.SignatureVerified,
	}
}

func agentMarketplaceGRPCError(err error) error {
	switch {
	case errors.Is(err, marketplace.ErrCatalogDisabled):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, marketplace.ErrInvalidQuery),
		errors.Is(err, marketplace.ErrInvalidCursor),
		errors.Is(err, service.ErrInvalidUnifiedAgentRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Errorf(codes.Internal, "agent extension marketplace request failed: %v", err)
	}
}

func agentExtensionGRPCError(err error) error {
	switch {
	case errors.Is(err, extension.ErrCatalogDisabled):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, extension.ErrInvalidQuery),
		errors.Is(err, extension.ErrInvalidCursor),
		errors.Is(err, service.ErrInvalidUnifiedAgentRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Errorf(codes.Internal, "agent extension catalog request failed: %v", err)
	}
}

func agentSkillGRPCError(err error) error {
	switch {
	case errors.Is(err, skill.ErrSkillNotFound),
		errors.Is(err, skill.ErrVersionNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, skill.ErrCatalogDisabled),
		errors.Is(err, service.ErrWorkflowNotPublishable),
		errors.Is(err, service.ErrWorkflowAsToolDisabled):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, service.ErrInvalidUnifiedAgentRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Errorf(codes.Internal, "agent Skill request failed: %v", err)
	}
}

func workflowToolPublicationGRPCError(err error, fallback codes.Code) error {
	switch {
	case errors.Is(err, repository.ErrWorkflowToolPublicationNotFound):
		return status.Error(codes.NotFound, repository.ErrWorkflowToolPublicationNotFound.Error())
	case errors.Is(err, repository.ErrWorkflowToolPublicationConflict):
		return status.Error(codes.Aborted, repository.ErrWorkflowToolPublicationConflict.Error())
	case errors.Is(err, service.ErrWorkflowNotPublishable):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, service.ErrWorkflowAsToolDisabled):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Errorf(fallback, "workflow tool publication request failed: %v", err)
	}
}

func workflowRunSummaryToProto(run *repository.WorkflowRunRecord) *aiAgentv1.WorkflowRunSummary {
	if run == nil {
		return nil
	}
	approvalRequestID := ""
	if !run.ApprovalRequestID.IsZero() {
		approvalRequestID = run.ApprovalRequestID.Hex()
	}
	return &aiAgentv1.WorkflowRunSummary{
		RunId: run.ID.Hex(), WorkflowId: run.WorkflowID.Hex(), Status: run.Status,
		ErrorMessage: run.ErrorMessage, StartedAt: unixOrZero(run.StartedAt),
		FinishedAt: unixOrZero(run.FinishedAt), WaitingNodeId: run.WaitingNodeID,
		ApprovalRequestId: approvalRequestID, WorkflowRevisionNumber: run.WorkflowRevisionNumber,
		StateVersion: run.StateVersion, CancelRequestedAt: unixOrZero(run.CancelRequestedAt),
		CancelReason: run.CancelReason, CanceledAt: unixOrZero(run.CanceledAt),
	}
}

func executionRunTraceToProto(record *agentObservability.RunRecord) *aiAgentv1.AgentRunTrace {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentRunTrace{
		RecordId: record.RecordID, RunId: record.RunID, WorkflowId: record.WorkflowID, UserId: record.UserID,
		Source: record.Source, Mode: record.Mode, Strategy: record.Strategy, Status: record.Status,
		ErrorClass: record.ErrorClass, Usage: executionTokenUsageToProto(record.Usage),
		Budget: &aiAgentv1.ExecutionBudgetSnapshot{
			MaxSteps: int32(record.Budget.MaxSteps), MaxTotalTokens: int32(record.Budget.MaxTotalTokens),
			MaxEstimatedCostMicros: record.Budget.MaxEstimatedCostMicros,
			ConsumedSteps:          int32(record.Budget.ConsumedSteps), ConsumedTokens: int32(record.Budget.ConsumedTokens),
			ConsumedCostMicros: record.Budget.ConsumedCostMicros,
		},
		StartedAtMs: unixMilliOrZero(record.StartedAt), FinishedAtMs: unixMilliOrZero(record.FinishedAt),
		DurationMs: record.DurationMS, UpdatedAtMs: unixMilliOrZero(record.UpdatedAt),
	}
}

func executionStepTraceToProto(record *agentObservability.StepRecord) *aiAgentv1.AgentStepTrace {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentStepTrace{
		RecordId: record.RecordID, RunId: record.RunID, WorkflowId: record.WorkflowID, UserId: record.UserID,
		Source: record.Source, StepId: record.StepID, ParentStepId: record.ParentStepID,
		Sequence: int32(record.Sequence), StepType: record.StepType, Name: record.Name, Status: record.Status,
		Attempt: int32(record.Attempt), MaxAttempts: int32(record.MaxAttempts), ErrorClass: record.ErrorClass,
		StartedAtMs: unixMilliOrZero(record.StartedAt), FinishedAtMs: unixMilliOrZero(record.FinishedAt),
		DurationMs: record.DurationMS, UpdatedAtMs: unixMilliOrZero(record.UpdatedAt),
	}
}

func executionLLMCallTraceToProto(record *agentObservability.LLMCallRecord) *aiAgentv1.AgentLLMCallTrace {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentLLMCallTrace{
		RecordId: record.RecordID, RunId: record.RunID, WorkflowId: record.WorkflowID, UserId: record.UserID,
		Source: record.Source, StepId: record.StepID, Sequence: int32(record.Sequence),
		Model: record.Model, Provider: record.Provider, Status: record.Status, ErrorClass: record.ErrorClass,
		PromptHash: record.PromptHash, PromptLength: int32(record.PromptLength),
		CompletionHash: record.CompletionHash, CompletionLength: int32(record.CompletionLength),
		PromptTemplateId: record.PromptTemplateID, PromptTemplateVersion: record.PromptTemplateVersion,
		PromptSample: record.PromptSample, CompletionSample: record.CompletionSample,
		PromptSampleStatus: record.PromptSampleStatus, CompletionSampleStatus: record.CompletionSampleStatus,
		ContentSamplePolicy: record.ContentSamplePolicy,
		Usage:               executionTokenUsageToProto(record.Usage), StartedAtMs: unixMilliOrZero(record.StartedAt),
		FinishedAtMs: unixMilliOrZero(record.FinishedAt), DurationMs: record.DurationMS,
		UpdatedAtMs: unixMilliOrZero(record.UpdatedAt),
	}
}

func executionToolCallTraceToProto(record *agentObservability.ToolCallRecord) *aiAgentv1.AgentToolCallTrace {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentToolCallTrace{
		RecordId: record.RecordID, RunId: record.RunID, WorkflowId: record.WorkflowID, UserId: record.UserID,
		Source: record.Source, StepId: record.StepID, Sequence: int32(record.Sequence),
		ToolName: record.ToolName, Category: record.Category, Status: record.Status, ErrorClass: record.ErrorClass,
		Attempts: int32(record.Attempts), ArgumentsHash: record.ArgumentsHash,
		ArgumentsLength: int32(record.ArgumentsLength), OutputHash: record.OutputHash, OutputLength: int32(record.OutputLength),
		OutputStorage: record.OutputStorage, OutputReference: record.OutputReference, OutputContentType: record.OutputContentType,
		StartedAtMs: unixMilliOrZero(record.StartedAt), FinishedAtMs: unixMilliOrZero(record.FinishedAt),
		DurationMs: record.DurationMS, UpdatedAtMs: unixMilliOrZero(record.UpdatedAt),
	}
}

func workflowRunEventToProto(event agentObservability.TraceEvent) *aiAgentv1.WorkflowRunEvent {
	return &aiAgentv1.WorkflowRunEvent{
		Cursor: event.Cursor, Kind: string(event.Kind), Reset_: event.Reset,
		Heartbeat: event.Heartbeat, Terminal: event.Terminal, Reason: event.Reason,
		CreatedAtMs: unixMilliOrZero(event.CreatedAt),
		Run:         executionRunTraceToProto(event.Run),
		Step:        executionStepTraceToProto(event.Step),
		LlmCall:     executionLLMCallTraceToProto(event.LLMCall),
		ToolCall:    executionToolCallTraceToProto(event.ToolCall),
	}
}

func executionTokenUsageToProto(usage agentObservability.TokenUsage) *aiAgentv1.ExecutionTokenUsage {
	return &aiAgentv1.ExecutionTokenUsage{
		InputTokens: int32(usage.InputTokens), OutputTokens: int32(usage.OutputTokens), TotalTokens: int32(usage.TotalTokens),
		Estimated: usage.Estimated, EstimatedCostMicros: usage.EstimatedCostMicros,
		CostEstimated: usage.CostEstimated, PricingVersion: usage.PricingVersion,
	}
}

func workflowCompensationRunSummaryToProto(run *repository.WorkflowRunRecord) *aiAgentv1.WorkflowCompensationRunSummary {
	if run == nil {
		return nil
	}
	approvalRequestID := ""
	if !run.ApprovalRequestID.IsZero() {
		approvalRequestID = run.ApprovalRequestID.Hex()
	}
	return &aiAgentv1.WorkflowCompensationRunSummary{
		RunId: run.ID.Hex(), WorkflowId: run.WorkflowID.Hex(), Status: run.Status,
		ErrorMessage: run.ErrorMessage, StartedAt: unixOrZero(run.StartedAt),
		FinishedAt: unixOrZero(run.FinishedAt), WaitingNodeId: run.WaitingNodeID,
		ApprovalRequestId: approvalRequestID,
	}
}

func toolApprovalToProto(approval *service.ToolApprovalView) *aiAgentv1.ToolApprovalRequest {
	if approval == nil {
		return nil
	}
	inputs, _ := json.Marshal(approval.RedactedInputs)
	return &aiAgentv1.ToolApprovalRequest{
		ApprovalId: approval.ID, UserId: approval.UserID, RunId: approval.RunID,
		StepId: approval.StepID, ToolName: approval.ToolName, Source: approval.Source,
		Category: approval.Category, Status: approval.Status, RedactedInputsJson: string(inputs),
		IdempotencyKey: approval.IdempotencyKey, Reason: approval.Reason, Revision: approval.Revision,
		CreatedAt: unixOrZero(approval.CreatedAt), ExpiresAt: unixOrZero(approval.ExpiresAt),
		DecidedAt: unixOrZero(approval.DecidedAt),
	}
}

func unixOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func unixMilliOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

func providerConfigToProto(config *service.ProviderConfigView) *aiAgentv1.ProviderConfig {
	if config == nil {
		return nil
	}
	return &aiAgentv1.ProviderConfig{
		ProviderConfigId: config.ID,
		UserId:           config.UserID,
		Kind:             config.Kind,
		Name:             config.Name, Provider: config.Provider, BaseUrl: config.BaseURL, Model: config.Model,
		Status: config.Status, HasSecret: config.HasSecret,
		CredentialVersion: config.CredentialVersion, Revision: config.Revision,
		CreatedAt: config.CreatedAt.Unix(), UpdatedAt: config.UpdatedAt.Unix(),
	}
}

func externalMCPInput(
	scope, projectID, name, transport, endpoint, authType,
	credentialSource, managedCredentialRef, bearerToken string,
) externalmcp.ConnectionInput {
	return externalmcp.ConnectionInput{
		Scope: scope, ProjectID: projectID, Name: name, Transport: transport,
		Endpoint: endpoint, AuthType: authType, CredentialSource: credentialSource,
		ManagedCredentialRef: managedCredentialRef, BearerToken: bearerToken,
	}
}

func externalMCPConnectionToProto(connection *externalmcp.Connection) *aiAgentv1.ExternalMCPConnection {
	if connection == nil {
		return nil
	}
	return &aiAgentv1.ExternalMCPConnection{
		ConnectionId: connection.ID, UserId: connection.UserID, Scope: connection.Scope,
		ProjectId: connection.ProjectID, ServerId: connection.ServerID, Name: connection.Name,
		Transport: connection.Transport, Endpoint: connection.Endpoint, AuthType: connection.AuthType,
		CredentialSource: connection.CredentialSource, Status: connection.Status, HasSecret: connection.HasSecret,
		ManagedCredentialRef:     connection.ManagedCredentialRef,
		ManagedCredentialVersion: connection.ManagedCredentialVersion,
		CredentialVersion:        connection.CredentialVersion, LatestSnapshotId: connection.LatestSnapshotID,
		PendingSnapshotId: connection.PendingSnapshotID, ActiveSnapshotId: connection.ActiveSnapshotID,
		DiscoveryStatus: connection.DiscoveryStatus, LastErrorCode: connection.LastErrorCode,
		LastCheckedAt: unixOrZero(connection.LastCheckedAt), Revision: connection.Revision,
		CreatedAt: unixOrZero(connection.CreatedAt), UpdatedAt: unixOrZero(connection.UpdatedAt),
		HealthStatus:    normalizedExternalMCPHealthStatus(connection.HealthStatus),
		HealthErrorCode: connection.HealthErrorCode, HealthFailureCount: connection.HealthFailureCount,
		LastHealthCheckedAt: unixOrZero(connection.LastHealthCheckedAt),
		LastHealthyAt:       unixOrZero(connection.LastHealthyAt),
		NextHealthCheckAt:   unixOrZero(connection.NextHealthCheckAt),
	}
}

func normalizedExternalMCPHealthStatus(status string) string {
	switch status {
	case externalmcp.HealthStatusHealthy, externalmcp.HealthStatusDegraded, externalmcp.HealthStatusUnhealthy:
		return status
	default:
		return externalmcp.HealthStatusUnknown
	}
}

func externalMCPSnapshotToProto(snapshot *externalmcp.ToolSchemaSnapshot) *aiAgentv1.ExternalMCPToolSnapshot {
	if snapshot == nil {
		return nil
	}
	tools := make([]*aiAgentv1.ExternalMCPToolSchema, 0, len(snapshot.Tools))
	for _, tool := range snapshot.Tools {
		tools = append(tools, externalMCPToolSchemaToProto(tool))
	}
	return &aiAgentv1.ExternalMCPToolSnapshot{
		SnapshotId: snapshot.ID, ConnectionId: snapshot.ConnectionID, UserId: snapshot.UserID,
		ServerId: snapshot.ServerID, SchemaHash: snapshot.SchemaHash, Version: snapshot.Version,
		Tools: tools, CreatedAt: unixOrZero(snapshot.CreatedAt),
	}
}

func externalMCPToolSchemaToProto(tool externalmcp.ToolSchema) *aiAgentv1.ExternalMCPToolSchema {
	return &aiAgentv1.ExternalMCPToolSchema{
		Name: tool.Name, QualifiedName: tool.QualifiedName, Description: tool.Description,
		InputSchemaJson: tool.InputSchemaJSON, OutputSchemaJson: tool.OutputSchemaJSON,
		DeclaredReadOnly: tool.DeclaredReadOnly, DeclaredIdempotent: tool.DeclaredIdempotent,
		IdempotencyKeyArgument:   tool.IdempotencyKeyArgument,
		SupportsWriteIdempotency: tool.SupportsWriteIdempotency(),
	}
}

func externalMCPToolPolicyToProto(policy externalmcp.ToolPolicy) *aiAgentv1.ExternalMCPToolPolicy {
	return &aiAgentv1.ExternalMCPToolPolicy{
		SnapshotId: policy.SnapshotID, ToolName: policy.ToolName,
		QualifiedName: policy.QualifiedName, Category: policy.Category,
		Enabled: policy.Enabled, UpdatedAt: unixOrZero(policy.UpdatedAt),
	}
}

func externalMCPToolViewToProto(tool externalmcp.ToolView) *aiAgentv1.ExternalMCPToolView {
	return &aiAgentv1.ExternalMCPToolView{
		Schema: externalMCPToolSchemaToProto(tool.Schema),
		Policy: externalMCPToolPolicyToProto(tool.Policy),
	}
}

func externalMCPControlError(err error, fallback codes.Code) error {
	switch {
	case errors.Is(err, externalmcp.ErrDisabled):
		return status.Error(codes.FailedPrecondition, externalmcp.ErrDisabled.Error())
	case errors.Is(err, externalmcp.ErrConnectionNotFound), errors.Is(err, externalmcp.ErrSnapshotNotFound):
		return status.Error(codes.NotFound, "external MCP resource not found")
	case errors.Is(err, externalmcp.ErrToolNotFound):
		return status.Error(codes.NotFound, externalmcp.ErrToolNotFound.Error())
	case errors.Is(err, externalmcp.ErrRevisionConflict):
		return status.Error(codes.Aborted, externalmcp.ErrRevisionConflict.Error())
	case errors.Is(err, externalmcp.ErrSnapshotMismatch):
		return status.Error(codes.Aborted, externalmcp.ErrSnapshotMismatch.Error())
	case errors.Is(err, externalmcp.ErrToolDisabled), errors.Is(err, externalmcp.ErrToolRiskBlocked), errors.Is(err, externalmcp.ErrToolWriteBlocked):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, externalmcp.ErrProjectScopeDisabled):
		return status.Error(codes.FailedPrecondition, externalmcp.ErrProjectScopeDisabled.Error())
	case errors.Is(err, externalmcp.ErrProjectStoreUnavailable):
		return status.Error(codes.FailedPrecondition, externalmcp.ErrProjectStoreUnavailable.Error())
	case errors.Is(err, externalmcp.ErrManagedCredentialsDisabled):
		return status.Error(codes.FailedPrecondition, externalmcp.ErrManagedCredentialsDisabled.Error())
	case errors.Is(err, externalmcp.ErrManagedCredentialUnavailable):
		return status.Error(codes.Unavailable, externalmcp.ErrManagedCredentialUnavailable.Error())
	case errors.Is(err, externalmcp.ErrManagedCredentialNotFound),
		errors.Is(err, externalmcp.ErrManagedCredentialBinding):
		return status.Error(codes.InvalidArgument, "managed external MCP credential binding is invalid")
	case errors.Is(err, agentproject.ErrAccessDenied):
		return status.Error(codes.PermissionDenied, agentproject.ErrAccessDenied.Error())
	case errors.Is(err, agentproject.ErrNotFound):
		return status.Error(codes.NotFound, "Agent project resource not found")
	default:
		return status.Errorf(fallback, "external MCP request failed: %v", err)
	}
}

func externalMCPDiscoveryError(err error) error {
	if errors.Is(err, externalmcp.ErrDisabled) || errors.Is(err, externalmcp.ErrRevisionConflict) ||
		errors.Is(err, externalmcp.ErrConnectionNotFound) {
		return externalMCPControlError(err, codes.FailedPrecondition)
	}
	return status.Error(codes.Unavailable, "external MCP discovery failed")
}

// dialogueObjectIDToUint64 将 MongoDB ObjectID 转为 uint64
// 由于 proto 中 dialogue_id 定义为 uint64，这里取 ObjectID 的后 8 字节作为 uint64
// 注意：这是一个有损转换，仅用于 gRPC 层的兼容适配
// 后续前端完善后建议改为直接传 string 类型的 hex ID
func dialogueObjectIDToUint64(oid interface{ Hex() string }) uint64 {
	hex := oid.Hex()
	if len(hex) < 16 {
		return 0
	}
	// 取 ObjectID hex 的后 16 位字符（8字节），转为 uint64
	var result uint64
	for _, c := range hex[len(hex)-16:] {
		result <<= 4
		switch {
		case c >= '0' && c <= '9':
			result |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			result |= uint64(c - 'a' + 10)
		}
	}
	return result
}

// uint64ToObjectIDHex 将 uint64 dialogue_id 转回 ObjectID hex 格式
// 这是 dialogueObjectIDToUint64 的逆操作（有损，前 8 字节补零）
func uint64ToObjectIDHex(id uint64) string {
	return fmt.Sprintf("%08x%016x", 0, id)
}
