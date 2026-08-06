package service

import (
	"fmt"
	"testing"

	agentMessage "twitter-clone/internal/module/agent/message"
	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/sashabaranov/go-openai"
)

func TestBuildRuntimeMessagesPreservesSystemAndCurrentInput(t *testing.T) {
	service := &AgentService{}
	history := make([]openai.ChatCompletionMessage, 0, 20)
	for index := 0; index < 20; index++ {
		history = append(history, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleUser, Content: fmt.Sprintf("old-%d-very-long-history-content", index),
		})
	}
	result, err := service.buildRuntimeMessages(
		"system policy",
		"current question",
		history,
		agentRuntime.Budget{MaxInputTokens: 80},
	)
	if err != nil {
		t.Fatalf("buildRuntimeMessages() error = %v", err)
	}
	if len(result.Messages) < 2 {
		t.Fatalf("buildRuntimeMessages() messages = %+v", result.Messages)
	}
	if result.Messages[0].Role != agentRuntime.RoleSystem || result.Messages[0].Content != "system policy" {
		t.Fatalf("system message = %+v", result.Messages[0])
	}
	last := result.Messages[len(result.Messages)-1]
	if last.Role != agentRuntime.RoleUser || last.Content != "current question" {
		t.Fatalf("current message = %+v", last)
	}
	if result.EstimatedTokens > 80 {
		t.Fatalf("estimated tokens = %d, want <= 80", result.EstimatedTokens)
	}
}

func TestStringifyDroppedSourcesOmitsZeroValues(t *testing.T) {
	dropped := stringifyDroppedSources(map[agentMessage.Source]int{
		agentMessage.SourceHistory: 2,
		agentMessage.SourceRAG:     0,
	})
	if len(dropped) != 1 || dropped["history"] != 2 {
		t.Fatalf("stringifyDroppedSources() = %+v", dropped)
	}
}
