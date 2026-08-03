package service

import (
	"context"
	"testing"
)

func TestContextWithModelKindResolvesAdvertisedChatModel(t *testing.T) {
	service := &AgentService{chatModel: "fallback-model"}
	models := service.GetModelInfo()
	if len(models) == 0 {
		t.Fatal("GetModelInfo() returned no chat models")
	}
	ctx, err := service.ContextWithModelKind(context.Background(), models[0].ID)
	if err != nil {
		t.Fatalf("ContextWithModelKind() error = %v", err)
	}
	if got := service.selectedModel(ctx); got != models[0].Name {
		t.Fatalf("selectedModel() = %q, want %q", got, models[0].Name)
	}
}

func TestContextWithModelKindRejectsUnknownIDAndKeepsDefaultForZero(t *testing.T) {
	service := &AgentService{chatModel: "fallback-model"}
	ctx, err := service.ContextWithModelKind(context.Background(), 0)
	if err != nil || service.selectedModel(ctx) != "fallback-model" {
		t.Fatalf("zero model selection = %q/%v", service.selectedModel(ctx), err)
	}
	if _, err := service.ContextWithModelKind(context.Background(), 999999); err == nil {
		t.Fatal("ContextWithModelKind() error = nil, want unknown model error")
	}
}
