package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

const (
	CapabilityConversationReply = "conversation.reply"
	CapabilityPlatformSearch    = "platform.search"
	CapabilityWebSearch         = "web.search"
	CapabilityContentDraft      = "content.draft"
	CapabilityExternalMCP       = "connector.mcp"
	CapabilityWorkflowRun       = "workflow.run"
	CapabilitySkillRun          = "skill.run"

	ExecutionProfileRuntimeChat           = "runtime.chat"
	ExecutionProfileRuntimePlatformSearch = "runtime.platform_search"
	ExecutionProfileRuntimeDraft          = "runtime.draft"
	ExecutionProfileCompatChat            = "compat.chat"
	ExecutionProfileCompatConsult         = "compat.consult"
	ExecutionProfileCompatAssist          = "compat.assist"
	ExecutionProfileRuntimeResearchDraft  = "runtime.research_draft"
	ExecutionProfileRuntimeWebSearch      = "runtime.web_search"
	ExecutionProfileRuntimeWebDraft       = "runtime.web_research_draft"
	ExecutionProfileRuntimeExternalMCP    = "runtime.external_mcp"
	ExecutionProfileRuntimeWorkflow       = "runtime.workflow"
	ExecutionProfileRuntimeSkill          = "runtime.skill"

	UnifiedAgentRunStatusCompleted     = "completed"
	UnifiedAgentRunStatusAwaitingHuman = "awaiting_human"

	AgentArtifactTypeContentDraft = "content.draft"
	AgentArtifactStatusReady      = "ready"
	AgentArtifactContentMarkdown  = "text/markdown"

	AgentApprovalStatusNotRequired   = "not_required"
	AgentApprovalStatusInputRequired = "input_required"
)

var (
	ErrUnsupportedCapability        = errors.New("unsupported agent capability")
	ErrCapabilityCompositionPending = errors.New("compound capability execution is not available")
	ErrInvalidUnifiedAgentRequest   = errors.New("invalid unified agent request")
)

// AgentCapabilityPlan is an internal routing decision. ExecutionProfile is not
// a permission boundary; profiles and the governed tool executor still enforce
// tool access.
type AgentCapabilityPlan struct {
	ExecutionProfile string
	CapabilityIDs    []string
}

type AgentCapabilityPlanRequest struct {
	Query                  string
	PreferredCapabilityIDs []string
}

type AgentCapabilityPlanner interface {
	Plan(context.Context, AgentCapabilityPlanRequest) (AgentCapabilityPlan, error)
}

type UnifiedAgentRequest struct {
	UserID                    uint64
	DialogueID                uint64
	DialogueKey               string
	Content                   string
	PreferredCapabilityIDs    []string
	WebSearchProviderConfigID string
	SkillID                   string
	SkillVersion              string
	TaskTemplateID            string
	TaskTemplateRevision      int64
	ExpectedExecutionProfile  string
}

type UnifiedAgentResult struct {
	ChatResult
	ExecutionProfile             string
	CapabilityIDs                []string
	RunStatus                    string
	PublishableDraft             bool
	ToolActivities               []AgentToolActivity
	Citations                    []AgentCitation
	Artifacts                    []AgentArtifact
	ApprovalState                AgentApprovalState
	SelectedSkillID              string
	SelectedSkillVersion         string
	SelectedTaskTemplateID       string
	SelectedTaskTemplateRevision int64
	ExecutionStrategyPlan        agentStrategy.Plan
}

// AgentArtifact is a typed, user-visible execution output. It is a projection
// over a persisted Run, not a second persistence model.
type AgentArtifact struct {
	ArtifactID           string
	Type                 string
	Status               string
	ContentType          string
	Content              string
	SourceRunID          string
	RequiresConfirmation bool
}

// AgentApprovalState is intentionally sanitized. Approval inputs, credentials
// and one-time resume tokens are never part of the ordinary RunAgent response.
type AgentApprovalState struct {
	Status          string
	ApprovalID      string
	RunID           string
	Action          string
	Revision        int64
	ExpiresAt       int64
	ResumeSupported bool
}

// ConservativeCapabilityPlanner recognizes only capabilities in the injected
// catalog. It must not advertise web.search or external MCP until those
// adapters and their governance are implemented.
type ConservativeCapabilityPlanner struct {
	catalog AgentCapabilityCatalog
}

func NewConservativeCapabilityPlanner(catalogs ...AgentCapabilityCatalog) AgentCapabilityPlanner {
	var catalog AgentCapabilityCatalog
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}
	if catalog == nil {
		builtIn, err := NewBuiltInAgentCapabilityCatalog()
		if err != nil {
			panic(fmt.Sprintf("invalid built-in agent capability catalog: %v", err))
		}
		catalog = builtIn
	}
	return ConservativeCapabilityPlanner{catalog: catalog}
}

func (p ConservativeCapabilityPlanner) Plan(
	ctx context.Context,
	request AgentCapabilityPlanRequest,
) (AgentCapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return AgentCapabilityPlan{}, err
	}
	if p.catalog == nil {
		return AgentCapabilityPlan{}, errors.New("agent capability catalog is not configured")
	}

	preferred := uniqueCapabilityIDs(request.PreferredCapabilityIDs)
	if len(preferred) > 0 {
		return p.catalog.ResolvePlan(ctx, preferred)
	}

	query := strings.ToLower(strings.TrimSpace(request.Query))
	if query == "" {
		return AgentCapabilityPlan{}, fmt.Errorf("%w: content is required", ErrInvalidUnifiedAgentRequest)
	}

	wantsSearch := containsAny(query, searchIntentTerms)
	wantsWebSearch := containsAny(query, webSearchIntentTerms)
	wantsDraft := containsAny(query, draftIntentTerms)
	wantsWorkflow := containsAny(query, workflowIntentTerms)
	if wantsWorkflow {
		return p.catalog.ResolvePlan(ctx, []string{CapabilityWorkflowRun})
	}
	if wantsWebSearch && wantsDraft {
		return p.catalog.ResolvePlan(ctx, []string{CapabilityWebSearch, CapabilityContentDraft})
	}
	if wantsWebSearch {
		return p.catalog.ResolvePlan(ctx, []string{CapabilityWebSearch})
	}
	if wantsSearch && wantsDraft {
		return p.catalog.ResolvePlan(ctx, []string{CapabilityPlatformSearch, CapabilityContentDraft})
	}
	if wantsDraft {
		return p.catalog.ResolvePlan(ctx, []string{CapabilityContentDraft})
	}
	if wantsSearch {
		return p.catalog.ResolvePlan(ctx, []string{CapabilityPlatformSearch})
	}
	return p.catalog.ResolvePlan(ctx, []string{CapabilityConversationReply})
}

func (s *AgentService) RunAgent(ctx context.Context, request UnifiedAgentRequest) (*UnifiedAgentResult, error) {
	if s == nil || s.capabilityPlanner == nil {
		return nil, errors.New("agent capability planner is not configured")
	}
	if request.UserID == 0 || strings.TrimSpace(request.Content) == "" {
		return nil, fmt.Errorf("%w: user_id and content are required", ErrInvalidUnifiedAgentRequest)
	}
	request.SkillID = strings.TrimSpace(request.SkillID)
	request.SkillVersion = strings.TrimSpace(request.SkillVersion)
	request.TaskTemplateID = strings.TrimSpace(request.TaskTemplateID)
	request.ExpectedExecutionProfile = strings.TrimSpace(request.ExpectedExecutionProfile)
	if (request.SkillID == "") != (request.SkillVersion == "") {
		return nil, fmt.Errorf(
			"%w: skill_id and skill_version must be supplied together",
			ErrInvalidUnifiedAgentRequest,
		)
	}
	if (request.TaskTemplateID == "") != (request.TaskTemplateRevision == 0) ||
		(request.TaskTemplateID != "" && request.ExpectedExecutionProfile == "") {
		return nil, fmt.Errorf(
			"%w: task template identity and expected execution profile are incomplete",
			ErrInvalidUnifiedAgentRequest,
		)
	}
	ctx = withWebSearchProviderConfig(ctx, request.WebSearchProviderConfigID)

	var selectedSkill *resolvedWorkflowSkill
	if request.SkillID != "" {
		preferred := uniqueCapabilityIDs(request.PreferredCapabilityIDs)
		if len(preferred) > 0 &&
			(len(preferred) != 1 || preferred[0] != CapabilitySkillRun) {
			return nil, fmt.Errorf(
				"%w: an explicit Skill can only request %s",
				ErrInvalidUnifiedAgentRequest,
				CapabilitySkillRun,
			)
		}
		var resolveErr error
		selectedSkill, resolveErr = s.resolveWorkflowSkill(
			ctx,
			request.UserID,
			request.SkillID,
			request.SkillVersion,
		)
		if resolveErr != nil {
			return nil, resolveErr
		}
		request.PreferredCapabilityIDs = []string{CapabilitySkillRun}
	}
	if len(uniqueCapabilityIDs(request.PreferredCapabilityIDs)) == 0 {
		request.PreferredCapabilityIDs = s.inferDialogueCapabilityHints(ctx, request)
	}
	plan, err := s.capabilityPlanner.Plan(ctx, AgentCapabilityPlanRequest{
		Query:                  request.Content,
		PreferredCapabilityIDs: request.PreferredCapabilityIDs,
	})
	if err != nil {
		return nil, err
	}
	if plan.ExecutionProfile == ExecutionProfileRuntimeSkill && selectedSkill == nil {
		return nil, fmt.Errorf(
			"%w: %s requires an exact skill_id and skill_version",
			ErrInvalidUnifiedAgentRequest,
			CapabilitySkillRun,
		)
	}
	if request.TaskTemplateID != "" &&
		(plan.ExecutionProfile != request.ExpectedExecutionProfile ||
			!sameCapabilityIDs(plan.CapabilityIDs, request.PreferredCapabilityIDs)) {
		return nil, ErrAgentTaskTemplateRouteDrift
	}
	executionStrategyPlan, err := s.planUnifiedAgentExecutionStrategy(ctx, request, plan)
	if err != nil {
		return nil, err
	}
	ctx, executionRun, err := s.beginUnifiedAgentExecutionRun(ctx, request, plan, executionStrategyPlan)
	if err != nil {
		return nil, err
	}

	var result *ChatResult
	var toolActivities []AgentToolActivity
	var citations []AgentCitation
	if executionStrategyPlan.SelectedStrategy == agentStrategy.KindMultiAgent {
		var execution *unifiedAgentExecution
		execution, err = s.executeMultiAgentStrategy(ctx, request, plan, executionStrategyPlan)
		if execution != nil {
			result = execution.ChatResult
			toolActivities = execution.ToolActivities
			citations = execution.Citations
		}
	} else {
		switch plan.ExecutionProfile {
		case ExecutionProfileRuntimeChat:
			result, err = s.callApiOfAiRuntime(
				ctx,
				request.UserID,
				request.DialogueID,
				request.DialogueKey,
				request.Content,
			)
		case ExecutionProfileCompatChat:
			result, err = s.callApiOfAiLegacy(ctx, request.UserID, request.DialogueID, request.DialogueKey, request.Content)
		case ExecutionProfileCompatConsult:
			result, err = s.ConsultContent(ctx, request.UserID, request.DialogueID, request.DialogueKey, request.Content)
		case ExecutionProfileRuntimePlatformSearch:
			var execution *unifiedAgentExecution
			execution, err = s.executePlatformSearchRuntime(
				ctx,
				request.UserID,
				request.DialogueID,
				request.DialogueKey,
				request.Content,
				plan,
			)
			if execution != nil {
				result = execution.ChatResult
				toolActivities = execution.ToolActivities
				citations = execution.Citations
			}
		case ExecutionProfileRuntimeDraft:
			result, err = s.assistPublishTwitterRuntime(
				ctx,
				request.UserID,
				request.DialogueID,
				request.DialogueKey,
				request.Content,
			)
		case ExecutionProfileCompatAssist:
			result, err = s.assistPublishTwitterLegacy(
				ctx,
				request.UserID,
				request.DialogueID,
				request.DialogueKey,
				request.Content,
			)
		case ExecutionProfileRuntimeResearchDraft:
			var execution *unifiedAgentExecution
			execution, err = s.executeResearchDraftRuntime(
				ctx,
				request.UserID,
				request.DialogueID,
				request.DialogueKey,
				request.Content,
				plan,
			)
			if execution != nil {
				result = execution.ChatResult
				toolActivities = execution.ToolActivities
				citations = execution.Citations
			}
		case ExecutionProfileRuntimeWebSearch, ExecutionProfileRuntimeWebDraft:
			var execution *unifiedAgentExecution
			execution, err = s.executeWebSearchRuntime(
				ctx,
				request.UserID,
				request.DialogueID,
				request.DialogueKey,
				request.Content,
				plan,
			)
			if execution != nil {
				result = execution.ChatResult
				toolActivities = execution.ToolActivities
				citations = execution.Citations
			}
		case ExecutionProfileRuntimeExternalMCP:
			var execution *unifiedAgentExecution
			execution, err = s.executeExternalMCPRuntime(
				ctx,
				request.UserID,
				request.DialogueID,
				request.DialogueKey,
				request.Content,
				plan,
			)
			if execution != nil {
				result = execution.ChatResult
				toolActivities = execution.ToolActivities
			}
		case ExecutionProfileRuntimeWorkflow:
			var execution *unifiedAgentExecution
			execution, err = s.executeWorkflowRuntime(
				ctx,
				request.UserID,
				request.DialogueID,
				request.DialogueKey,
				request.Content,
				plan,
			)
			if execution != nil {
				result = execution.ChatResult
				toolActivities = execution.ToolActivities
			}
		case ExecutionProfileRuntimeSkill:
			var execution *unifiedAgentExecution
			execution, err = s.executeSkillRuntime(
				ctx,
				request.UserID,
				request.DialogueID,
				request.DialogueKey,
				request.Content,
				plan,
				selectedSkill,
			)
			if execution != nil {
				result = execution.ChatResult
				toolActivities = execution.ToolActivities
			}
		default:
			err = fmt.Errorf("%w: execution profile %q", ErrUnsupportedCapability, plan.ExecutionProfile)
		}
	}
	if err != nil {
		stateErr := s.finishUnifiedAgentExecutionRun(ctx, executionRun, nil, err)
		return nil, errors.Join(err, stateErr)
	}
	if result == nil {
		err = errors.New("agent execution returned no result")
		stateErr := s.finishUnifiedAgentExecutionRun(ctx, executionRun, nil, err)
		return nil, errors.Join(err, stateErr)
	}
	if len(citations) == 0 && len(result.Tweets) > 0 {
		citations = citationsFromTweetResults(result.Tweets)
	}
	runStatus := strings.TrimSpace(result.RunStatus)
	if runStatus == "" {
		runStatus = UnifiedAgentRunStatusCompleted
	}
	publishableDraft := runStatus == UnifiedAgentRunStatusCompleted &&
		containsCapability(plan.CapabilityIDs, CapabilityContentDraft) &&
		strings.TrimSpace(result.RunID) != ""
	artifacts := buildUnifiedAgentArtifacts(result, publishableDraft)

	unifiedResult := &UnifiedAgentResult{
		ChatResult:       *result,
		ExecutionProfile: plan.ExecutionProfile,
		CapabilityIDs:    append([]string(nil), plan.CapabilityIDs...),
		RunStatus:        runStatus,
		PublishableDraft: publishableDraft,
		ToolActivities:   append([]AgentToolActivity(nil), toolActivities...),
		Citations:        append([]AgentCitation(nil), citations...),
		Artifacts:        artifacts,
		ApprovalState: AgentApprovalState{
			Status: AgentApprovalStatusNotRequired,
			RunID:  result.RunID,
		},
		SelectedSkillID:              request.SkillID,
		SelectedSkillVersion:         request.SkillVersion,
		SelectedTaskTemplateID:       request.TaskTemplateID,
		SelectedTaskTemplateRevision: request.TaskTemplateRevision,
		ExecutionStrategyPlan:        agentStrategy.ClonePlan(executionStrategyPlan),
	}
	if err := s.finishUnifiedAgentExecutionRun(ctx, executionRun, unifiedResult, nil); err != nil {
		return nil, err
	}
	if executionRun != nil {
		unifiedResult.RunStatus = string(executionRun.Status)
		unifiedResult.ApprovalState.RunID = executionRun.ID
		unifiedResult.ApprovalState.Revision = executionRun.Revision
		unifiedResult.ApprovalState.ResumeSupported = executionRun.ResumeSupported
		if executionRun.Status == repository.AgentExecutionRunAwaitingHuman {
			unifiedResult.ApprovalState.Status = AgentApprovalStatusInputRequired
			unifiedResult.ApprovalState.Action = string(agentRuntime.ActionAskHuman)
			unifiedResult.PublishableDraft = false
			unifiedResult.Artifacts = nil
		}
		if executionRun.Status == repository.AgentExecutionRunApprovalRequired {
			unifiedResult.ApprovalState.Status = AgentApprovalStatusInputRequired
			unifiedResult.ApprovalState.ApprovalID = executionRun.ApprovalRequestID
			unifiedResult.ApprovalState.Action = string(agentRuntime.ActionToolCall)
			unifiedResult.ApprovalState.ExpiresAt = executionRun.ApprovalExpiresAt.Unix()
			unifiedResult.PublishableDraft = false
			unifiedResult.Artifacts = nil
		}
	}
	return unifiedResult, nil
}

func buildUnifiedAgentArtifacts(result *ChatResult, publishableDraft bool) []AgentArtifact {
	if result == nil || !publishableDraft {
		return nil
	}
	runID := strings.TrimSpace(result.RunID)
	content := strings.TrimSpace(result.Response)
	if runID == "" || content == "" {
		return nil
	}
	return []AgentArtifact{{
		ArtifactID:           AgentArtifactTypeContentDraft + ":" + runID,
		Type:                 AgentArtifactTypeContentDraft,
		Status:               AgentArtifactStatusReady,
		ContentType:          AgentArtifactContentMarkdown,
		Content:              content,
		SourceRunID:          runID,
		RequiresConfirmation: true,
	}}
}

func uniqueCapabilityIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsCapability(capabilityIDs []string, expected string) bool {
	for _, capabilityID := range capabilityIDs {
		if capabilityID == expected {
			return true
		}
	}
	return false
}

func containsAny(value string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

var searchIntentTerms = []string{
	"搜索", "查询", "查找", "找一下", "帮我查", "最新", "资料",
	"search", "find", "look up", "latest",
}

var webSearchIntentTerms = []string{
	"联网搜索", "网页搜索", "搜索网页", "全网搜索", "互联网上", "网上查", "查一下网页",
	"web search", "search the web", "search online", "internet search", "look up online",
}

var draftIntentTerms = []string{
	"写推文", "写一条", "草稿", "改写", "润色", "文案", "帮我写",
	"write a tweet", "draft", "rewrite", "polish",
}

var workflowIntentTerms = []string{
	"运行我的工作流", "执行我的工作流", "运行我的自动化", "执行我的自动化",
	"run my workflow", "execute my workflow", "run my automation", "execute my automation",
}
