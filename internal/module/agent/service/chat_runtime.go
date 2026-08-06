package service

import (
	"context"
	"errors"
	"fmt"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/pkg/logger"

	"go.uber.org/zap"
)

// callApiOfAiRuntime executes tool-free dialogue through the governed Runtime.
// Tool-oriented capabilities must be selected explicitly by the capability
// planner instead of being silently available to an ordinary conversation.
func (s *AgentService) callApiOfAiRuntime(
	ctx context.Context,
	userID uint64,
	dialogueID uint64,
	dialogueKey string,
	content string,
) (*ChatResult, error) {
	dialogue, err := s.getOrCreateDialogue(
		ctx,
		userID,
		resolveDialogueKey(dialogueID, dialogueKey),
		content,
		repository.ModeChat,
	)
	if err != nil {
		return nil, err
	}
	noteAgentExecutionDialogue(ctx, dialogue.ID.Hex())
	if s.runtimeRunner == nil {
		return nil, errors.New("agent runtime runner is not configured")
	}

	selectedProfile, err := s.resolveAgentProfile(ctx, profileConversationReply, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve conversation profile failed: %w", err)
	}
	cognitive := s.buildCognitiveContext(ctx, userID, content)
	systemPrompt := s.decorateSystemPromptWithCognitiveContext(
		selectedProfile.Prompt.SystemPrompt,
		cognitive,
	)

	contextMessages, err := s.loadContextMessages(ctx, dialogue.ID)
	if err != nil {
		logger.Warn(ctx, "load chat runtime context failed", zap.Error(err))
		contextMessages = nil
	}
	messageBuild, err := s.buildRuntimeMessages(
		systemPrompt,
		content,
		contextMessages,
		selectedProfile.Budget,
	)
	if err != nil {
		return nil, fmt.Errorf("build chat runtime messages failed: %w", err)
	}

	result, err := s.runRuntime(ctx, agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID:                 agentExecutionRunID(ctx),
			UserID:                userID,
			Mode:                  agentRuntime.ModeChat,
			Budget:                selectedProfile.Budget,
			AgentProfileID:        selectedProfile.ID,
			AgentProfileVersion:   selectedProfile.Version,
			PromptTemplateID:      selectedProfile.Prompt.ID,
			PromptTemplateVersion: selectedProfile.Prompt.Version,
		},
		Model:    s.selectedModel(ctx),
		Messages: messageBuild.Messages,
		Tools:    nil,
	})
	s.recordRuntimeResult(ctx, result, err, profileConversationReply)
	if err != nil {
		return nil, fmt.Errorf("chat runtime failed: %w", err)
	}
	response, err := runtimeUserVisibleResponse(result)
	if err != nil {
		return nil, fmt.Errorf("chat runtime response: %w", err)
	}

	metadata := runtimeResultMetadata(
		result,
		selectedProfile.ID,
		selectedProfile.Version,
		selectedProfile.Prompt.Version,
	)
	metadata["execution_profile"] = ExecutionProfileRuntimeChat
	metadata["capability_ids"] = []string{CapabilityConversationReply}
	metadata["runtime_context_tokens"] = messageBuild.EstimatedTokens
	metadata["runtime_context_dropped"] = stringifyDroppedSources(messageBuild.Dropped)
	metadata["cognitive_intent"] = string(cognitive.Intent)
	metadata["cognitive_rewritten_query"] = cognitive.RewrittenQuery
	metadata["cognitive_chunk_count"] = cognitive.ChunkCount
	if err := s.saveUserAndAssistantMessages(
		ctx,
		dialogue.ID,
		userID,
		content,
		response,
		metadata,
	); err != nil {
		logger.Error(ctx, "save chat runtime messages failed", zap.Error(err))
		return nil, fmt.Errorf("persist chat conversation failed: %w", err)
	}

	return &ChatResult{
		DialogueID: dialogue.ID.Hex(),
		RunID:      result.Context.RunID,
		RunStatus:  string(result.Status),
		Response:   response,
	}, nil
}
