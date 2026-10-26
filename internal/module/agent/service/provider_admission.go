package service

import (
	"context"
	"github.com/sashabaranov/go-openai"
	agentModel "twitter-clone/internal/module/agent/model"
)

func (s *AgentService) completeCompatibilityModel(ctx context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	return agentModel.CompleteWithAdmission(ctx, s.llmClient, providerDashScope, request, s.providerAdmission)
}
