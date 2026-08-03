package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"

	agentObservability "twitter-clone/internal/module/agent/observability"
)

func TestRedisExecutionEventStoreResumeIsolationAndTTL(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisExecutionEventStore(client, ExecutionEventStreamConfig{MaxLength: 10, TTL: time.Hour})
	ctx := context.Background()

	require.NoError(t, store.RecordRun(ctx, agentObservability.RunRecord{
		RecordID: "run-1", RunID: "run-1", UserID: 41, Status: "running",
	}))
	require.NoError(t, store.RecordLLMCall(ctx, agentObservability.LLMCallRecord{
		RecordID: "llm-1", RunID: "run-1", UserID: 41, StepID: "llm",
		PromptHash: "digest-only", PromptLength: 123, Status: "success",
	}))
	batch, err := store.ReadTraceEvents(ctx, 41, "run-1", "", 1000, 0)
	require.NoError(t, err)
	require.Len(t, batch.Events, 2)
	require.Equal(t, agentObservability.TraceEventRun, batch.Events[0].Kind)
	require.Equal(t, "digest-only", batch.Events[1].LLMCall.PromptHash)
	require.Equal(t, time.Hour, server.TTL(executionEventStreamKey(41, "run-1")))

	cursor := batch.Events[1].Cursor
	require.NoError(t, store.RecordRun(ctx, agentObservability.RunRecord{
		RecordID: "run-1", RunID: "run-1", UserID: 41, Status: "success",
	}))
	resumed, err := store.ReadTraceEvents(ctx, 41, "run-1", cursor, 10, 0)
	require.NoError(t, err)
	require.Len(t, resumed.Events, 1)
	require.Equal(t, "success", resumed.Events[0].Run.Status)

	otherTenant, err := store.ReadTraceEvents(ctx, 42, "run-1", "", 10, 0)
	require.NoError(t, err)
	require.Empty(t, otherTenant.Events)
	require.NotEqual(t, executionEventStreamKey(41, "run-1"), executionEventStreamKey(42, "run-1"))
}

func TestRedisExecutionEventStoreRejectsFutureAndInvalidCursors(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisExecutionEventStore(client, ExecutionEventStreamConfig{})
	require.NoError(t, store.RecordRun(context.Background(), agentObservability.RunRecord{
		RecordID: "run", RunID: "run", UserID: 7, Status: "running",
	}))
	_, err := store.ReadTraceEvents(context.Background(), 7, "run", "invalid", 10, 0)
	require.Error(t, err)
	future, err := store.ReadTraceEvents(context.Background(), 7, "run", "9999999999999-0", 10, 0)
	require.NoError(t, err)
	require.True(t, future.Reset)
	require.NotEmpty(t, future.Events)
}
