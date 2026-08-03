package service

import (
	"context"
	"fmt"
	"strings"
)

type selectedModelContextKey struct{}

// ContextWithModelKind resolves the public model identifier once at the gRPC
// boundary. Downstream execution receives only the concrete model name.
func (s *AgentService) ContextWithModelKind(ctx context.Context, modelKindID uint64) (context.Context, error) {
	if modelKindID == 0 {
		return ctx, nil
	}
	for _, model := range s.GetModelInfo() {
		if model.ID == modelKindID && strings.TrimSpace(model.Name) != "" {
			return context.WithValue(ctx, selectedModelContextKey{}, strings.TrimSpace(model.Name)), nil
		}
	}
	return nil, fmt.Errorf("unknown chat model_kind_id %d", modelKindID)
}

func (s *AgentService) selectedModel(ctx context.Context) string {
	if ctx != nil {
		if model, ok := ctx.Value(selectedModelContextKey{}).(string); ok && strings.TrimSpace(model) != "" {
			return strings.TrimSpace(model)
		}
	}
	return strings.TrimSpace(s.chatModel)
}
