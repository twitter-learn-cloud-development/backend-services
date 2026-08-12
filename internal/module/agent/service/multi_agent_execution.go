package service

import (
	"context"
	"errors"
	"fmt"

	agentMultiRole "twitter-clone/internal/module/agent/multirole"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
	"twitter-clone/pkg/logger"

	"go.uber.org/zap"
)

var (
	ErrMultiAgentPlanUnsupported = agentMultiRole.ErrPlanUnsupported
	ErrMultiAgentRoleFailed      = agentMultiRole.ErrRoleExecutionFailed
)

const (
	multiAgentAggregatePromptID = "multi.runtime.aggregate"
	multiAgentRoleResearcher    = agentMultiRole.RoleResearcher
	multiAgentRoleDrafter       = agentMultiRole.RoleDrafter
	multiAgentRoleReviewer      = agentMultiRole.RoleReviewer
)

type multiAgentRoleExecutionError = agentMultiRole.RoleExecutionError

type multiAgentExecutionConfig struct {
	templateID          string
	parentProfileID     string
	researcherProfileID string
	requiredTool        string
	label               string
	dialogueMode        repository.DialogueMode
	runtimeMode         agentRuntime.Mode
}

func (s *AgentService) executeMultiAgentStrategy(
	ctx context.Context,
	request UnifiedAgentRequest,
	capabilityPlan AgentCapabilityPlan,
	strategyPlan agentStrategy.Plan,
) (*unifiedAgentExecution, error) {
	config, err := multiAgentConfig(capabilityPlan.ExecutionProfile, strategyPlan.TemplateID)
	if err != nil {
		return nil, err
	}
	if err := agentMultiRole.ValidateSequentialPlan(strategyPlan); err != nil {
		return nil, err
	}
	if s.runtimeRunner == nil {
		return nil, errors.New("agent runtime runner is not configured")
	}
	if s.runtimeTools == nil {
		return nil, errors.New("agent runtime tool catalog is not configured")
	}

	dialogue, err := s.getOrCreateDialogue(
		ctx,
		request.UserID,
		resolveDialogueKey(request.DialogueID, request.DialogueKey),
		request.Content,
		config.dialogueMode,
	)
	if err != nil {
		return nil, err
	}
	noteAgentExecutionDialogue(ctx, dialogue.ID.Hex())

	availableTools, err := s.runtimeTools.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runtime tools for multi-agent execution: %w", err)
	}
	if strategyPlan.ProfileSetAnchor != config.parentProfileID || strategyPlan.ProfileSetVersion == "" {
		return nil, fmt.Errorf(
			"%w: profile set binding %q@%q does not match %q",
			ErrMultiAgentPlanUnsupported,
			strategyPlan.ProfileSetAnchor,
			strategyPlan.ProfileSetVersion,
			config.parentProfileID,
		)
	}
	profileSet, err := s.resolveAgentProfileSetVersion(ctx, config.parentProfileID, []string{
		config.researcherProfileID,
		profileMultiDrafter,
		profileMultiReviewer,
	}, strategyPlan.ProfileSetVersion, request.UserID)
	if err != nil {
		return nil, fmt.Errorf("resolve atomic multi-agent profile set: %w", err)
	}
	parentProfile, ok := profileSet.Profile(config.parentProfileID)
	if !ok {
		return nil, fmt.Errorf("multi-agent profile set is missing parent %q", config.parentProfileID)
	}
	researcherProfile, ok := profileSet.Profile(config.researcherProfileID)
	if !ok {
		return nil, fmt.Errorf("multi-agent profile set is missing researcher %q", config.researcherProfileID)
	}
	drafterProfile, ok := profileSet.Profile(profileMultiDrafter)
	if !ok {
		return nil, fmt.Errorf("multi-agent profile set is missing drafter %q", profileMultiDrafter)
	}
	reviewerProfile, ok := profileSet.Profile(profileMultiReviewer)
	if !ok {
		return nil, fmt.Errorf("multi-agent profile set is missing reviewer %q", profileMultiReviewer)
	}

	history, historyErr := s.loadContextMessages(ctx, dialogue.ID)
	if historyErr != nil {
		logger.Warn(ctx, "load multi-agent research context failed", zap.Error(historyErr))
		history = nil
	}

	var toolActivities []AgentToolActivity
	var citations []AgentCitation
	handoff := agentMultiRole.EvidenceHandoffBuilderFunc(func(
		summary string,
		research agentRuntime.RunResult,
	) (string, error) {
		activities, collected := collectRuntimeResultEvidence(research)
		if len(collected) == 0 {
			return "", fmt.Errorf(
				"%w: no structured citation survived evidence validation",
				ErrRequiredCapabilityEvidence,
			)
		}
		mapped := make([]agentMultiRole.Citation, 0, len(collected))
		for _, citation := range collected {
			mapped = append(mapped, agentMultiRole.Citation{
				CitationID: citation.CitationID,
				SourceType: citation.SourceType,
				SourceID:   citation.SourceID,
				URL:        citation.URL,
				Title:      citation.Title,
				Snippet:    citation.Snippet,
			})
		}
		encoded, encodeErr := agentMultiRole.EncodeEvidenceHandoff(summary, mapped)
		if encodeErr != nil {
			return "", encodeErr
		}
		toolActivities = activities
		citations = collected
		return encoded, nil
	})

	parentRunID := agentExecutionRunID(ctx)
	selectedModel := s.selectedModel(ctx)
	executor := agentMultiRole.NewExecutor(s.runtimeRunner, s.runtimeMessages)
	multiResult, executionErr := executor.Execute(ctx, agentMultiRole.Request{
		ParentContext: agentRuntime.RunContext{
			RunID:                 parentRunID,
			UserID:                request.UserID,
			Mode:                  config.runtimeMode,
			AgentProfileID:        profileMultiAggregate,
			AgentProfileVersion:   profileSet.Version,
			PromptTemplateID:      multiAgentAggregatePromptID,
			PromptTemplateVersion: profileSet.Version,
		},
		Plan:         strategyPlan,
		Model:        selectedModel,
		Input:        request.Content,
		History:      openAIMessagesToRuntime(history),
		Tools:        availableTools,
		RequiredTool: config.requiredTool,
		Profiles: agentMultiRole.Profiles{
			Parent: parentProfile, Researcher: researcherProfile,
			Drafter: drafterProfile, Reviewer: reviewerProfile,
		},
		Handoff: handoff,
	})
	aggregate := multiResult.Aggregate
	if aggregate.Context.RunID != "" {
		parentRequest := agentRuntime.RunRequest{Context: aggregate.Context, Model: selectedModel}
		captureAgentRuntimeExecution(ctx, parentRequest, aggregate, executionErr)
		s.recordRuntimeResult(ctx, aggregate, executionErr, "multi_agent:"+strategyPlan.TemplateID)
	}
	if executionErr != nil {
		if errors.Is(executionErr, agentMultiRole.ErrRequiredToolEvidence) &&
			!errors.Is(executionErr, ErrRequiredCapabilityEvidence) {
			executionErr = fmt.Errorf("%w: %v", ErrRequiredCapabilityEvidence, executionErr)
		}
		return nil, executionErr
	}

	metadata := runtimeResultMetadata(
		aggregate,
		profileMultiAggregate,
		profileSet.Version,
		profileSet.Version,
	)
	metadata["profile_set_anchor"] = profileSet.AnchorID
	metadata["profile_set_version"] = profileSet.Version
	metadata["execution_profile"] = capabilityPlan.ExecutionProfile
	metadata["capability_ids"] = append([]string(nil), capabilityPlan.CapabilityIDs...)
	metadata["execution_strategy"] = string(strategyPlan.SelectedStrategy)
	metadata["execution_strategy_template"] = strategyPlan.TemplateID
	metadata["execution_strategy_plan_digest"] = strategyPlan.PlanDigest
	metadata["multi_agent_role_count"] = len(multiResult.Roles)
	metadata["runtime_context_tokens"] = multiResult.EstimatedContextTokens()
	if research, ok := multiResult.Role(agentMultiRole.RoleResearcher); ok {
		metadata["runtime_context_dropped"] = stringifyDroppedSources(research.Build.Dropped)
	}
	metadata["tool_activity_count"] = len(toolActivities)
	metadata["citation_count"] = len(citations)
	if err := s.saveUserAndAssistantMessages(
		ctx,
		dialogue.ID,
		request.UserID,
		request.Content,
		aggregate.FinalAnswer,
		metadata,
	); err != nil {
		logger.Error(ctx, "save multi-agent messages failed", zap.Error(err))
		return nil, fmt.Errorf("persist multi-agent conversation: %w", err)
	}

	return &unifiedAgentExecution{
		ChatResult: &ChatResult{
			DialogueID: dialogue.ID.Hex(),
			RunID:      parentRunID,
			RunStatus:  string(aggregate.Status),
			Response:   aggregate.FinalAnswer,
		},
		ToolActivities: toolActivities,
		Citations:      citations,
	}, nil
}
