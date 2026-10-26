package model

import (
	"context"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

// The adapter owns Provider selection; the policy never receives credentials.
type CallAdmission interface {
	AcquireCall(context.Context, string, *agentRuntime.ModelRequest) (context.Context, func(), error)
}
type OpenAIClientOption func(*OpenAICompatibleClient)

func WithCallAdmission(policy CallAdmission) OpenAIClientOption {
	return func(client *OpenAICompatibleClient) { client.admission = policy }
}
