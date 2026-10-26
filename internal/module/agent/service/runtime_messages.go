package service

import (
	"context"

	agentMessage "twitter-clone/internal/module/agent/message"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/sashabaranov/go-openai"
)

const defaultRuntimeMaxInputTokens = 12000

func (s *AgentService) buildRuntimeMessages(
	systemPrompt string,
	currentInput string,
	history []openai.ChatCompletionMessage,
	runBudget agentRuntime.Budget,
) (agentMessage.BuildResult, error) {
	builder := s.runtimeMessages
	if builder == nil {
		builder = agentMessage.NewBuilder(nil, nil)
	}
	maxInputTokens := runBudget.MaxInputTokens
	if maxInputTokens <= 0 {
		maxInputTokens = defaultRuntimeMaxInputTokens
	}
	return builder.Build(agentMessage.BuildRequest{
		System: []agentRuntime.Message{{
			Role: agentRuntime.RoleSystem, Content: systemPrompt,
		}},
		Current: agentRuntime.Message{Role: agentRuntime.RoleUser, Content: currentInput},
		History: openAIMessagesToRuntime(history),
		Budget: agentMessage.Budget{
			MaxInputTokens:   maxInputTokens,
			HistoryTokens:    maxInputTokens * 60 / 100,
			MemoryTokens:     maxInputTokens * 15 / 100,
			RAGTokens:        maxInputTokens * 20 / 100,
			ToolResultTokens: maxInputTokens * 20 / 100,
			BlackboardTokens: maxInputTokens * 10 / 100,
		},
	})
}

// buildRuntimeMessagesWithCognitiveContext injects retrieved semantic session
// summaries and long-term memory into every governed Runtime path. Raw recent
// history still follows the token-aware builder rules; older durable context
// is recovered through the bounded cognitive context block instead of relying
// on lossy prefix/suffix truncation alone.
func (s *AgentService) buildRuntimeMessagesWithCognitiveContext(
	ctx context.Context,
	userID uint64,
	systemPrompt string,
	currentInput string,
	history []openai.ChatCompletionMessage,
	runBudget agentRuntime.Budget,
) (agentMessage.BuildResult, cognitiveContextResult, error) {
	cognitive := s.buildCognitiveContext(ctx, userID, currentInput)
	result, err := s.buildRuntimeMessages(
		s.decorateSystemPromptWithCognitiveContext(systemPrompt, cognitive),
		currentInput,
		history,
		runBudget,
	)
	return result, cognitive, err
}

func addCognitiveContextMetadata(metadata map[string]interface{}, cognitive cognitiveContextResult) {
	if metadata == nil {
		return
	}
	metadata["cognitive_intent"] = string(cognitive.Intent)
	metadata["cognitive_rewritten_query"] = cognitive.RewrittenQuery
	metadata["cognitive_chunk_count"] = cognitive.ChunkCount
}

func stringifyDroppedSources(dropped map[agentMessage.Source]int) map[string]int {
	result := make(map[string]int, len(dropped))
	for source, count := range dropped {
		if count > 0 {
			result[string(source)] = count
		}
	}
	return result
}
