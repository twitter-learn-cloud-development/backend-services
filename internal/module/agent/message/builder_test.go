package message

import (
	"errors"
	"strings"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestBuilderKeepsMandatoryMessagesAndToolPairs(t *testing.T) {
	counter := agentRuntime.NewHeuristicTokenCounter()
	builder := NewBuilder(counter, nil)
	toolCall := agentRuntime.Message{
		Role: agentRuntime.RoleAssistant,
		Actions: []agentRuntime.Action{{
			ID: "call-1", Type: agentRuntime.ActionToolCall, Name: "search",
		}},
	}
	toolResult := agentRuntime.Message{
		Role: agentRuntime.RoleTool, ToolCallID: "call-1", Name: "search", Content: "relevant result",
	}
	result, err := builder.Build(BuildRequest{
		System:    []agentRuntime.Message{{Role: agentRuntime.RoleSystem, Content: "system"}},
		Developer: []agentRuntime.Message{{Role: agentRuntime.RoleDeveloper, Content: "developer"}},
		Policy:    []agentRuntime.Message{{Role: agentRuntime.RoleSystem, Content: "policy"}},
		Current:   agentRuntime.Message{Role: agentRuntime.RoleUser, Content: "current question"},
		History: []agentRuntime.Message{
			{Role: agentRuntime.RoleUser, Content: strings.Repeat("old history ", 20)},
			toolCall,
			toolResult,
		},
		Budget: Budget{MaxInputTokens: 300, HistoryTokens: 1, ToolResultTokens: 100},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Messages) != 6 {
		t.Fatalf("Build() messages = %+v", result.Messages)
	}
	if result.Messages[0].Role != agentRuntime.RoleSystem || result.Messages[1].Role != agentRuntime.RoleDeveloper {
		t.Fatalf("mandatory header order = %+v", result.Messages[:3])
	}
	if result.Messages[3].Actions[0].ID != "call-1" || result.Messages[4].ToolCallID != "call-1" {
		t.Fatalf("tool pair = %+v", result.Messages[3:5])
	}
	if result.Messages[len(result.Messages)-1].Content != "current question" {
		t.Fatalf("current input is not last = %+v", result.Messages)
	}
	if result.Dropped[SourceHistory] != 1 {
		t.Fatalf("dropped history = %+v", result.Dropped)
	}
}

func TestBuilderCompressesToolResultWithoutBreakingPair(t *testing.T) {
	counter := agentRuntime.NewHeuristicTokenCounter()
	builder := NewBuilder(counter, nil)
	assistant := agentRuntime.Message{
		Role:    agentRuntime.RoleAssistant,
		Actions: []agentRuntime.Action{{ID: "call-1", Type: agentRuntime.ActionToolCall, Name: "search"}},
	}
	tool := agentRuntime.Message{
		Role: agentRuntime.RoleTool, ToolCallID: "call-1", Name: "search",
		Content: strings.Repeat("long tool observation ", 80),
	}
	fixed := counter.CountMessages([]agentRuntime.Message{assistant, {
		Role: agentRuntime.RoleTool, ToolCallID: "call-1", Name: "search",
	}})
	result, err := builder.Build(BuildRequest{
		System:  []agentRuntime.Message{{Role: agentRuntime.RoleSystem, Content: "system"}},
		Current: agentRuntime.Message{Role: agentRuntime.RoleUser, Content: "question"},
		History: []agentRuntime.Message{assistant, tool},
		Budget:  Budget{MaxInputTokens: 500, ToolResultTokens: fixed + 20},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Messages) != 4 {
		t.Fatalf("Build() messages = %+v", result.Messages)
	}
	if result.Messages[1].Actions[0].ID != result.Messages[2].ToolCallID {
		t.Fatalf("compressed tool pair broken = %+v", result.Messages[1:3])
	}
	if !strings.Contains(result.Messages[2].Content, "[compressed]") {
		t.Fatalf("tool result was not compressed = %q", result.Messages[2].Content)
	}
}

func TestBuilderKeepsRAGChunksAtomic(t *testing.T) {
	counter := agentRuntime.NewHeuristicTokenCounter()
	builder := NewBuilder(counter, nil)
	result, err := builder.Build(BuildRequest{
		System:  []agentRuntime.Message{{Role: agentRuntime.RoleSystem, Content: "system"}},
		Current: agentRuntime.Message{Role: agentRuntime.RoleUser, Content: "question"},
		RAG: []Fragment{
			{Content: strings.Repeat("large-rag-chunk ", 100), Score: 1},
			{Content: "small relevant chunk", Score: 0.9},
		},
		Budget: Budget{MaxInputTokens: 300, RAGTokens: 30},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	joined := ""
	for _, message := range result.Messages {
		joined += message.Content
	}
	if strings.Contains(joined, "large-rag-chunk") || !strings.Contains(joined, "small relevant chunk") {
		t.Fatalf("RAG selection should skip whole oversized chunks = %q", joined)
	}
	if result.Dropped[SourceRAG] != 1 {
		t.Fatalf("dropped RAG = %+v", result.Dropped)
	}
}

func TestBuilderRejectsMandatoryContextOverBudget(t *testing.T) {
	builder := NewBuilder(nil, nil)
	_, err := builder.Build(BuildRequest{
		System:  []agentRuntime.Message{{Role: agentRuntime.RoleSystem, Content: strings.Repeat("system", 20)}},
		Current: agentRuntime.Message{Role: agentRuntime.RoleUser, Content: "question"},
		Budget:  Budget{MaxInputTokens: 5},
	})
	if !errors.Is(err, ErrMandatoryContextTooLarge) {
		t.Fatalf("Build() error = %v, want ErrMandatoryContextTooLarge", err)
	}
}

func TestBuilderDropsOrphanToolResults(t *testing.T) {
	builder := NewBuilder(nil, nil)
	result, err := builder.Build(BuildRequest{
		System:  []agentRuntime.Message{{Role: agentRuntime.RoleSystem, Content: "system"}},
		Current: agentRuntime.Message{Role: agentRuntime.RoleUser, Content: "question"},
		History: []agentRuntime.Message{{Role: agentRuntime.RoleTool, ToolCallID: "orphan", Content: "orphan"}},
		Budget:  Budget{MaxInputTokens: 100},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Messages) != 2 || result.Dropped[SourceToolResult] != 1 {
		t.Fatalf("orphan result was retained = messages %+v dropped %+v", result.Messages, result.Dropped)
	}
}
