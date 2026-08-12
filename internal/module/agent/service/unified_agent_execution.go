package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/skill"
	"twitter-clone/pkg/logger"

	"go.uber.org/zap"
)

var ErrRequiredCapabilityEvidence = errors.New("required capability evidence is missing")

type requiredToolRuntimeConfig struct {
	profileID          string
	requiredTool       string
	label              string
	allowedTools       []string
	dialogueMode       repository.DialogueMode
	runtimeMode        agentRuntime.Mode
	selectedTweetID    string
	systemPromptSuffix string
}

func (s *AgentService) executePlatformSearchRuntime(
	ctx context.Context,
	userID uint64,
	dialogueID uint64,
	dialogueKey string,
	content string,
	plan AgentCapabilityPlan,
) (*unifiedAgentExecution, error) {
	return s.executeRequiredToolRuntime(
		ctx,
		userID,
		dialogueID,
		dialogueKey,
		content,
		plan,
		requiredToolRuntimeConfig{
			profileID:    profileUnifiedPlatformSearch,
			requiredTool: "hybrid_search_tweets",
			label:        "platform search",
			allowedTools: []string{"hybrid_search_tweets"},
			dialogueMode: repository.ModeConsult,
			runtimeMode:  agentRuntime.ModeConsult,
		},
	)
}

func (s *AgentService) executeResearchDraftRuntime(
	ctx context.Context,
	userID uint64,
	dialogueID uint64,
	dialogueKey string,
	content string,
	plan AgentCapabilityPlan,
) (*unifiedAgentExecution, error) {
	return s.executeRequiredToolRuntime(
		ctx,
		userID,
		dialogueID,
		dialogueKey,
		content,
		plan,
		requiredToolRuntimeConfig{
			profileID:    profileUnifiedResearchDraft,
			requiredTool: "hybrid_search_tweets",
			label:        "research draft",
			dialogueMode: repository.ModeAssist,
			runtimeMode:  agentRuntime.ModeAssist,
		},
	)
}

func (s *AgentService) executeWebSearchRuntime(
	ctx context.Context,
	userID uint64,
	dialogueID uint64,
	dialogueKey string,
	content string,
	plan AgentCapabilityPlan,
) (*unifiedAgentExecution, error) {
	config := requiredToolRuntimeConfig{
		profileID:    profileUnifiedWebSearch,
		requiredTool: "web_search",
		label:        "web search",
		dialogueMode: repository.ModeConsult,
		runtimeMode:  agentRuntime.ModeConsult,
	}
	if containsCapability(plan.CapabilityIDs, CapabilityContentDraft) {
		config.profileID = profileUnifiedWebDraft
		config.label = "web research draft"
		config.dialogueMode = repository.ModeAssist
		config.runtimeMode = agentRuntime.ModeAssist
	}
	return s.executeRequiredToolRuntime(
		ctx,
		userID,
		dialogueID,
		dialogueKey,
		content,
		plan,
		config,
	)
}

func (s *AgentService) executeExternalMCPRuntime(
	ctx context.Context,
	userID uint64,
	dialogueID uint64,
	dialogueKey string,
	content string,
	plan AgentCapabilityPlan,
) (*unifiedAgentExecution, error) {
	dialogue, err := s.getOrCreateDialogue(
		ctx,
		userID,
		resolveDialogueKey(dialogueID, dialogueKey),
		content,
		repository.ModeConsult,
	)
	if err != nil {
		return nil, err
	}
	noteAgentExecutionDialogue(ctx, dialogue.ID.Hex())
	if s.runtimeRunner == nil {
		return nil, errors.New("agent runtime runner is not configured")
	}
	manager, err := s.externalMCP()
	if err != nil {
		return nil, err
	}
	governedCatalog := s.unifiedAgentApprovalRecovery
	var executable []externalmcp.ExecutableTool
	if governedCatalog {
		executable, err = manager.ListGovernedTools(ctx, userID)
	} else {
		executable, err = manager.ListExecutableTools(ctx, userID)
	}
	if err != nil {
		return nil, fmt.Errorf("list executable external MCP tools failed: %w", err)
	}
	availableTools := externalMCPRuntimeTools(executable)
	if len(availableTools) == 0 {
		return nil, errors.New("no governed external MCP tools are enabled")
	}
	profileID := profileUnifiedExternalMCP
	if governedCatalog {
		profileID = profileUnifiedExternalMCPGoverned
	}
	profile, err := s.resolveAgentProfile(ctx, profileID, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve external MCP profile failed: %w", err)
	}
	profile.AllowedTools = make([]string, 0, len(availableTools))
	allowedNames := make(map[string]struct{}, len(availableTools))
	for _, tool := range availableTools {
		if !governedCatalog {
			if err := validateRequiredReadTool(availableTools, tool.Name); err != nil {
				return nil, err
			}
		}
		profile.AllowedTools = append(profile.AllowedTools, tool.Name)
		allowedNames[tool.Name] = struct{}{}
	}
	tools := profile.FilterTools(availableTools)

	contextMessages, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load external MCP context failed", zap.Error(err))
		contextMessages = nil
	}
	messageBuild, err := s.buildRuntimeMessages(profile.Prompt.SystemPrompt, content, contextMessages, profile.Budget)
	if err != nil {
		return nil, fmt.Errorf("build external MCP runtime messages failed: %w", err)
	}

	runID := agentExecutionRunID(ctx)
	result, err := s.runRuntime(ctx, agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID: runID, UserID: userID, Mode: agentRuntime.ModeConsult,
			Budget: profile.Budget, AgentProfileID: profile.ID,
			AgentProfileVersion: profile.Version, PromptTemplateID: profile.Prompt.ID,
			PromptTemplateVersion: profile.Prompt.Version,
		},
		Model: s.selectedModel(ctx), Messages: messageBuild.Messages, Tools: tools,
	})
	s.recordRuntimeResult(ctx, result, err, profile.ID)
	approvalPending := result.Status == agentRuntime.RunStatusApprovalRequired &&
		agentRuntime.HasErrorCode(err, agentRuntime.ErrorApprovalRequired)
	if err != nil && !approvalPending {
		return nil, fmt.Errorf("external MCP runtime failed: %w", err)
	}
	response, responseErr := runtimeUserVisibleResponse(result)
	if responseErr != nil {
		return nil, fmt.Errorf("external MCP runtime response: %w", responseErr)
	}
	if result.Status == agentRuntime.RunStatusCompleted && !runtimeHasSuccessfulToolEvidenceAny(result, allowedNames) {
		return nil, fmt.Errorf(
			"%w: external MCP run %s did not complete an approved tool",
			ErrRequiredCapabilityEvidence,
			result.Context.RunID,
		)
	}
	toolActivities, _ := collectRuntimeResultEvidence(result)
	metadata := runtimeResultMetadata(result, profile.ID, profile.Version, profile.Prompt.Version)
	metadata["execution_profile"] = plan.ExecutionProfile
	metadata["capability_ids"] = append([]string(nil), plan.CapabilityIDs...)
	metadata["runtime_context_tokens"] = messageBuild.EstimatedTokens
	metadata["runtime_context_dropped"] = stringifyDroppedSources(messageBuild.Dropped)
	metadata["tool_activity_count"] = len(toolActivities)
	if err := s.saveUserAndAssistantMessages(
		ctx,
		dialogue.ID,
		userID,
		content,
		response,
		metadata,
	); err != nil {
		logger.Error(ctx, "save external MCP messages failed", zap.Error(err))
		return nil, fmt.Errorf("persist external MCP conversation failed: %w", err)
	}
	return &unifiedAgentExecution{
		ChatResult: &ChatResult{
			DialogueID: dialogue.ID.Hex(), RunID: result.Context.RunID,
			RunStatus: string(result.Status), Response: response,
		},
		ToolActivities: toolActivities,
	}, nil
}

func (s *AgentService) executeWorkflowRuntime(
	ctx context.Context,
	userID uint64,
	dialogueID uint64,
	dialogueKey string,
	content string,
	plan AgentCapabilityPlan,
) (*unifiedAgentExecution, error) {
	dialogue, err := s.getOrCreateDialogue(
		ctx,
		userID,
		resolveDialogueKey(dialogueID, dialogueKey),
		content,
		repository.ModeWorkflow,
	)
	if err != nil {
		return nil, err
	}
	noteAgentExecutionDialogue(ctx, dialogue.ID.Hex())
	if s.runtimeRunner == nil {
		return nil, errors.New("agent runtime runner is not configured")
	}
	availableTools, err := s.listPublishedWorkflowRuntimeTools(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list published workflow tools failed: %w", err)
	}
	if len(availableTools) == 0 {
		return nil, errors.New("no eligible published workflow tools are available")
	}
	selected, err := s.resolveAgentProfile(ctx, profileUnifiedWorkflow, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow runtime profile failed: %w", err)
	}
	selected.AllowedTools = make([]string, 0, len(availableTools))
	allowedNames := make(map[string]struct{}, len(availableTools))
	for _, definition := range availableTools {
		if err := validateRequiredReadTool(availableTools, definition.Name); err != nil {
			return nil, err
		}
		selected.AllowedTools = append(selected.AllowedTools, definition.Name)
		allowedNames[definition.Name] = struct{}{}
	}
	tools := selected.FilterTools(availableTools)

	contextMessages, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load workflow runtime context failed", zap.Error(err))
		contextMessages = nil
	}
	messageBuild, err := s.buildRuntimeMessages(
		selected.Prompt.SystemPrompt,
		content,
		contextMessages,
		selected.Budget,
	)
	if err != nil {
		return nil, fmt.Errorf("build workflow runtime messages failed: %w", err)
	}

	runID := agentExecutionRunID(ctx)
	result, err := s.runRuntime(ctx, agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID:                 runID,
			UserID:                userID,
			Mode:                  agentRuntime.ModeWorkflow,
			Budget:                selected.Budget,
			AgentProfileID:        selected.ID,
			AgentProfileVersion:   selected.Version,
			PromptTemplateID:      selected.Prompt.ID,
			PromptTemplateVersion: selected.Prompt.Version,
		},
		Model:    s.selectedModel(ctx),
		Messages: messageBuild.Messages,
		Tools:    tools,
	})
	s.recordRuntimeResult(ctx, result, err, selected.ID)
	approvalPending := result.Status == agentRuntime.RunStatusApprovalRequired &&
		agentRuntime.HasErrorCode(err, agentRuntime.ErrorApprovalRequired)
	if err != nil && !approvalPending {
		return nil, fmt.Errorf("workflow runtime failed: %w", err)
	}
	response, err := runtimeUserVisibleResponse(result)
	if err != nil {
		return nil, fmt.Errorf("workflow runtime response: %w", err)
	}
	if result.Status == agentRuntime.RunStatusCompleted &&
		!runtimeHasSuccessfulToolEvidenceAny(result, allowedNames) {
		return nil, fmt.Errorf(
			"%w: workflow run %s did not complete a published workflow tool",
			ErrRequiredCapabilityEvidence,
			result.Context.RunID,
		)
	}
	toolActivities, _ := collectRuntimeResultEvidence(result)
	metadata := runtimeResultMetadata(
		result,
		selected.ID,
		selected.Version,
		selected.Prompt.Version,
	)
	metadata["execution_profile"] = plan.ExecutionProfile
	metadata["capability_ids"] = append([]string(nil), plan.CapabilityIDs...)
	metadata["runtime_context_tokens"] = messageBuild.EstimatedTokens
	metadata["runtime_context_dropped"] = stringifyDroppedSources(messageBuild.Dropped)
	metadata["tool_activity_count"] = len(toolActivities)
	if err := s.saveUserAndAssistantMessages(
		ctx,
		dialogue.ID,
		userID,
		content,
		response,
		metadata,
	); err != nil {
		logger.Error(ctx, "save workflow runtime messages failed", zap.Error(err))
		return nil, fmt.Errorf("persist workflow runtime conversation failed: %w", err)
	}
	return &unifiedAgentExecution{
		ChatResult: &ChatResult{
			DialogueID: dialogue.ID.Hex(),
			RunID:      result.Context.RunID,
			RunStatus:  string(result.Status),
			Response:   response,
		},
		ToolActivities: toolActivities,
	}, nil
}

func (s *AgentService) executeSkillRuntime(
	ctx context.Context,
	userID uint64,
	dialogueID uint64,
	dialogueKey string,
	content string,
	plan AgentCapabilityPlan,
	selected *resolvedWorkflowSkill,
) (*unifiedAgentExecution, error) {
	if selected == nil {
		return nil, fmt.Errorf(
			"%w: an exact skill_id and skill_version are required",
			ErrInvalidUnifiedAgentRequest,
		)
	}
	// Re-resolve immediately before entering Runtime. The governed tool
	// executor repeats the publication binding check at call time.
	current, err := s.resolveWorkflowSkill(
		ctx,
		userID,
		selected.Version.ID,
		selected.Version.Version,
	)
	if err != nil {
		return nil, err
	}
	dialogue, err := s.getOrCreateDialogue(
		ctx,
		userID,
		resolveDialogueKey(dialogueID, dialogueKey),
		content,
		repository.ModeWorkflow,
	)
	if err != nil {
		return nil, err
	}
	noteAgentExecutionDialogue(ctx, dialogue.ID.Hex())
	if s.runtimeRunner == nil {
		return nil, errors.New("agent runtime runner is not configured")
	}

	selectedProfile := cloneServiceProfile(current.Profile)
	selectedProfile.AllowedTools = append([]string(nil), current.Version.AllowedTools...)
	availableTools := []agentRuntime.ToolDefinition{
		workflowSkillToolDefinition(current.Publication),
	}
	tools := selectedProfile.FilterTools(availableTools)
	if len(tools) != 1 || tools[0].Name != current.Publication.ToolName {
		return nil, errors.New("selected Skill tool binding is unavailable")
	}
	if err := validateRequiredReadTool(tools, current.Publication.ToolName); err != nil {
		return nil, err
	}

	contextMessages, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load skill runtime context failed", zap.Error(err))
		contextMessages = nil
	}
	systemPrompt := strings.TrimSpace(selectedProfile.Prompt.SystemPrompt) +
		"\n\nSelected immutable Skill contract:\n" +
		strings.TrimSpace(current.Version.Instructions)
	messageBuild, err := s.buildRuntimeMessages(
		systemPrompt,
		content,
		contextMessages,
		current.Version.Budget,
	)
	if err != nil {
		return nil, fmt.Errorf("build Skill runtime messages failed: %w", err)
	}

	runID := agentExecutionRunID(ctx)
	runCtx := withWorkflowSkillExecution(ctx, userID, current.Version)
	result, err := s.runRuntime(runCtx, agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID:                 runID,
			UserID:                userID,
			Mode:                  agentRuntime.ModeWorkflow,
			Budget:                current.Version.Budget,
			AgentProfileID:        selectedProfile.ID,
			AgentProfileVersion:   selectedProfile.Version,
			PromptTemplateID:      selectedProfile.Prompt.ID,
			PromptTemplateVersion: selectedProfile.Prompt.Version,
		},
		Model:    s.selectedModel(ctx),
		Messages: messageBuild.Messages,
		Tools:    tools,
		// Explicit Skill selection means its single bound workflow must execute.
		InitialToolChoice: agentRuntime.ToolChoiceRequired,
	})
	s.recordRuntimeResult(ctx, result, err, selectedProfile.ID)
	if err != nil {
		return nil, fmt.Errorf("Skill runtime failed: %w", err)
	}
	response, err := runtimeUserVisibleResponse(result)
	if err != nil {
		return nil, fmt.Errorf("Skill runtime response: %w", err)
	}
	if result.Status == agentRuntime.RunStatusCompleted {
		observation, ok := runtimeSuccessfulToolObservation(
			result,
			current.Publication.ToolName,
		)
		if !ok {
			return nil, fmt.Errorf(
				"%w: Skill run %s did not complete its bound workflow tool",
				ErrRequiredCapabilityEvidence,
				result.Context.RunID,
			)
		}
		if err := skill.ValidateOutput(
			current.Version.Output,
			observation.StructuredContent,
		); err != nil {
			return nil, fmt.Errorf("validate Skill output: %w", err)
		}
	}

	toolActivities, _ := collectRuntimeResultEvidence(result)
	metadata := runtimeResultMetadata(
		result,
		selectedProfile.ID,
		selectedProfile.Version,
		selectedProfile.Prompt.Version,
	)
	metadata["execution_profile"] = plan.ExecutionProfile
	metadata["capability_ids"] = append([]string(nil), plan.CapabilityIDs...)
	metadata["skill_id"] = current.Version.ID
	metadata["skill_version"] = current.Version.Version
	metadata["runtime_context_tokens"] = messageBuild.EstimatedTokens
	metadata["runtime_context_dropped"] = stringifyDroppedSources(messageBuild.Dropped)
	metadata["tool_activity_count"] = len(toolActivities)
	if err := s.saveUserAndAssistantMessages(
		ctx,
		dialogue.ID,
		userID,
		content,
		response,
		metadata,
	); err != nil {
		logger.Error(ctx, "save Skill runtime messages failed", zap.Error(err))
		return nil, fmt.Errorf("persist Skill conversation failed: %w", err)
	}
	return &unifiedAgentExecution{
		ChatResult: &ChatResult{
			DialogueID: dialogue.ID.Hex(),
			RunID:      result.Context.RunID,
			RunStatus:  string(result.Status),
			Response:   response,
		},
		ToolActivities: toolActivities,
	}, nil
}

func (s *AgentService) executeRequiredToolRuntime(
	ctx context.Context,
	userID uint64,
	dialogueID uint64,
	dialogueKey string,
	content string,
	plan AgentCapabilityPlan,
	config requiredToolRuntimeConfig,
) (*unifiedAgentExecution, error) {
	dialogue, err := s.getOrCreateDialogue(
		ctx,
		userID,
		resolveDialogueKey(dialogueID, dialogueKey),
		content,
		config.dialogueMode,
	)
	if err != nil {
		return nil, err
	}
	noteAgentExecutionDialogue(ctx, dialogue.ID.Hex())
	if s.runtimeRunner == nil {
		return nil, errors.New("agent runtime runner is not configured")
	}
	if s.runtimeTools == nil {
		return nil, errors.New("agent runtime tool catalog is not configured")
	}
	if config.profileID == profileUnifiedPlatformSearch {
		selection, selected, resolveErr := s.resolvePlatformTweetFollowUp(ctx, dialogue, content)
		if resolveErr != nil {
			return nil, fmt.Errorf("%s follow-up: %w", config.label, resolveErr)
		}
		if selected {
			config.requiredTool = "get_tweets_by_ids"
			config.label = "platform tweet detail"
			config.allowedTools = []string{"get_tweets_by_ids"}
			config.selectedTweetID = selection.TweetID
			config.systemPromptSuffix = platformTweetFollowUpSystemPrompt(selection)
		}
	}

	availableTools, err := s.runtimeTools.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runtime tools failed: %w", err)
	}
	profile, err := s.resolveAgentProfile(ctx, config.profileID, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve %s profile failed: %w", config.label, err)
	}
	if len(config.allowedTools) > 0 {
		profile.AllowedTools = append([]string(nil), config.allowedTools...)
	}
	tools := profile.FilterTools(availableTools)
	if err := validateRequiredReadTool(tools, config.requiredTool); err != nil {
		return nil, err
	}

	contextMessages, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load unified tool context failed", zap.String("profile", config.profileID), zap.Error(err))
		contextMessages = nil
	}
	systemPrompt := profile.Prompt.SystemPrompt + config.systemPromptSuffix
	messageBuild, err := s.buildRuntimeMessages(systemPrompt, content, contextMessages, profile.Budget)
	if err != nil {
		return nil, fmt.Errorf("build %s runtime messages failed: %w", config.label, err)
	}

	runID := agentExecutionRunID(ctx)
	result, err := s.runRuntime(ctx, agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID:                 runID,
			UserID:                userID,
			Mode:                  config.runtimeMode,
			Budget:                profile.Budget,
			AgentProfileID:        profile.ID,
			AgentProfileVersion:   profile.Version,
			PromptTemplateID:      profile.Prompt.ID,
			PromptTemplateVersion: profile.Prompt.Version,
		},
		Model:    s.selectedModel(ctx),
		Messages: messageBuild.Messages,
		Tools:    tools,
	})
	s.recordRuntimeResult(ctx, result, err, profile.ID)
	if config.profileID == profileUnifiedPlatformSearch {
		if config.requiredTool == "hybrid_search_tweets" {
			s.observePlatformSearchGoalShadow(ctx, content, result, err)
		} else if config.selectedTweetID != "" {
			s.observePlatformTweetFollowUpGoalShadow(
				ctx, content, config.selectedTweetID,
				platformTweetCitationURL(config.selectedTweetID), result, err,
			)
		}
	}
	if config.profileID == profileUnifiedResearchDraft {
		s.observeGroundedDraftGoalShadow(
			ctx, content, agentEvidence.GroundedDraftSourcePlatform, result, err,
		)
		s.observeResearchThenDraftGoalShadow(
			ctx, content, agentEvidence.GroundedDraftSourcePlatform, result, err,
		)
	}
	if config.profileID == profileUnifiedWebSearch || config.profileID == profileUnifiedWebDraft {
		s.observeWebResearchGoalShadow(ctx, content, result, err)
	}
	if config.profileID == profileUnifiedWebDraft {
		s.observeGroundedDraftGoalShadow(
			ctx, content, agentEvidence.GroundedDraftSourceWeb, result, err,
		)
		s.observeResearchThenDraftGoalShadow(
			ctx, content, agentEvidence.GroundedDraftSourceWeb, result, err,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("%s runtime failed: %w", config.label, err)
	}
	response, err := runtimeUserVisibleResponse(result)
	if err != nil {
		return nil, fmt.Errorf("%s runtime response: %w", config.label, err)
	}
	hasRequiredEvidence := runtimeHasSuccessfulToolEvidence(result, config.requiredTool)
	if config.profileID == profileUnifiedWebSearch || config.profileID == profileUnifiedWebDraft {
		hasRequiredEvidence = runtimeHasCitableWebSearchEvidence(result)
	}
	if config.selectedTweetID != "" {
		hasRequiredEvidence = runtimeHasPlatformTweetDetailEvidence(result, config.selectedTweetID)
	}
	if result.Status == agentRuntime.RunStatusCompleted && !hasRequiredEvidence {
		return nil, fmt.Errorf(
			"%w: %s run %s did not complete %s",
			ErrRequiredCapabilityEvidence,
			config.label,
			result.Context.RunID,
			config.requiredTool,
		)
	}
	toolActivities, citations := collectRuntimeResultEvidence(result)

	metadata := runtimeResultMetadata(result, profile.ID, profile.Version, profile.Prompt.Version)
	metadata["execution_profile"] = plan.ExecutionProfile
	metadata["capability_ids"] = append([]string(nil), plan.CapabilityIDs...)
	metadata["runtime_context_tokens"] = messageBuild.EstimatedTokens
	metadata["runtime_context_dropped"] = stringifyDroppedSources(messageBuild.Dropped)
	metadata["tool_activity_count"] = len(toolActivities)
	metadata["citation_count"] = len(citations)
	if references := platformTweetReferences(citations); len(references) > 0 {
		metadata[platformTweetReferencesMetadataKey] = references
	}
	if err := s.saveUserAndAssistantMessages(
		ctx,
		dialogue.ID,
		userID,
		content,
		response,
		metadata,
	); err != nil {
		logger.Error(ctx, "save unified tool messages failed", zap.String("profile", config.profileID), zap.Error(err))
		return nil, fmt.Errorf("persist %s conversation failed: %w", config.label, err)
	}

	return &unifiedAgentExecution{
		ChatResult: &ChatResult{
			DialogueID: dialogue.ID.Hex(),
			RunID:      result.Context.RunID,
			RunStatus:  string(result.Status),
			Response:   response,
		},
		ToolActivities: toolActivities,
		Citations:      citations,
	}, nil
}

func validateRequiredReadTool(tools []agentRuntime.ToolDefinition, requiredName string) error {
	for _, tool := range tools {
		if tool.Name != requiredName {
			continue
		}
		if tool.Category != agentRuntime.ToolCategoryRead || tool.ApprovalRequired() {
			return fmt.Errorf(
				"required runtime tool %s is not configured as a non-approval read tool",
				requiredName,
			)
		}
		return nil
	}
	return fmt.Errorf("required runtime tool %s is unavailable", requiredName)
}

func runtimeHasSuccessfulToolEvidence(result agentRuntime.RunResult, toolName string) bool {
	_, ok := runtimeSuccessfulToolObservation(result, toolName)
	return ok
}

func runtimeSuccessfulToolObservation(
	result agentRuntime.RunResult,
	toolName string,
) (agentRuntime.Observation, bool) {
	for _, step := range result.Steps {
		for _, action := range step.Actions {
			if action.Type != agentRuntime.ActionToolCall || action.Name != toolName {
				continue
			}
			for _, observation := range step.Observations {
				if observation.ActionID != action.ID || observation.IsError {
					continue
				}
				if strings.TrimSpace(observation.Content) == "" &&
					len(observation.StructuredContent) == 0 {
					continue
				}
				observation.StructuredContent = append(
					json.RawMessage(nil),
					observation.StructuredContent...,
				)
				return observation, true
			}
		}
	}
	return agentRuntime.Observation{}, false
}

func runtimeHasSuccessfulToolEvidenceAny(
	result agentRuntime.RunResult,
	allowed map[string]struct{},
) bool {
	for _, step := range result.Steps {
		for _, action := range step.Actions {
			if action.Type != agentRuntime.ActionToolCall {
				continue
			}
			if _, ok := allowed[action.Name]; !ok {
				continue
			}
			for _, observation := range step.Observations {
				if observation.ActionID == action.ID && !observation.IsError &&
					(strings.TrimSpace(observation.Content) != "" || len(observation.StructuredContent) > 0) {
					return true
				}
			}
		}
	}
	return false
}
