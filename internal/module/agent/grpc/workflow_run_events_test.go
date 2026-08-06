package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	agentObservability "twitter-clone/internal/module/agent/observability"
)

func TestWorkflowRunEventToProtoPreservesRedactedTraceFields(t *testing.T) {
	now := time.Unix(100, 123000000)
	event := workflowRunEventToProto(agentObservability.TraceEvent{
		Cursor: "11-0", Kind: agentObservability.TraceEventLLMCall, Terminal: true, CreatedAt: now,
		LLMCall: &agentObservability.LLMCallRecord{
			RecordID: "llm-1", RunID: "run-1", UserID: 7,
			PromptHash: "prompt-digest", PromptLength: 321,
			CompletionHash: "completion-digest", CompletionLength: 456,
		},
	})

	require.Equal(t, "11-0", event.Cursor)
	require.True(t, event.Terminal)
	require.Equal(t, now.UnixMilli(), event.CreatedAtMs)
	require.Equal(t, "prompt-digest", event.LlmCall.PromptHash)
	require.Equal(t, int32(321), event.LlmCall.PromptLength)
	require.Equal(t, "completion-digest", event.LlmCall.CompletionHash)
}

func TestExecutionToolCallTraceToProtoPreservesObjectReferenceMetadata(t *testing.T) {
	record := executionToolCallTraceToProto(&agentObservability.ToolCallRecord{
		RecordID: "tool-1", RunID: "run-1", UserID: 7, ToolName: "SearchTweets",
		OutputHash: "digest", OutputLength: 2048, OutputStorage: "minio",
		OutputReference:   "minio://agent-tool-results/tool-results/a/b/c.json",
		OutputContentType: "application/json",
	})

	require.Equal(t, "digest", record.OutputHash)
	require.Equal(t, int32(2048), record.OutputLength)
	require.Equal(t, "minio", record.OutputStorage)
	require.Equal(t, "minio://agent-tool-results/tool-results/a/b/c.json", record.OutputReference)
	require.Equal(t, "application/json", record.OutputContentType)
}
