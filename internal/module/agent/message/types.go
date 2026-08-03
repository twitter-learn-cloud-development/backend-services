package message

import (
	"errors"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type Source string

const (
	SourceSystem     Source = "system"
	SourceDeveloper  Source = "developer"
	SourcePolicy     Source = "policy"
	SourceCurrent    Source = "current_input"
	SourceHistory    Source = "history"
	SourceToolResult Source = "tool_result"
	SourcePersona    Source = "persona"
	SourceMemory     Source = "episodic_memory"
	SourceRAG        Source = "rag"
	SourceBlackboard Source = "blackboard"
)

var ErrMandatoryContextTooLarge = errors.New("mandatory context exceeds input token budget")

type Fragment struct {
	Source  Source
	Name    string
	Content string
	Score   float64
}

type Budget struct {
	MaxInputTokens   int
	HistoryTokens    int
	MemoryTokens     int
	RAGTokens        int
	ToolResultTokens int
	BlackboardTokens int
}

type BuildRequest struct {
	System     []agentRuntime.Message
	Developer  []agentRuntime.Message
	Policy     []agentRuntime.Message
	Current    agentRuntime.Message
	History    []agentRuntime.Message
	Persona    []Fragment
	Memory     []Fragment
	RAG        []Fragment
	Blackboard []Fragment
	Budget     Budget
}

type BuildResult struct {
	Messages        []agentRuntime.Message
	EstimatedTokens int
	Dropped         map[Source]int
}

type Builder interface {
	Build(request BuildRequest) (BuildResult, error)
}

type Compressor interface {
	Compress(content string, maxTokens int) string
}
