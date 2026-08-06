package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/repository"
)

func TestWatchWorkflowRunEventsStreamsOwnedRunUntilTerminal(t *testing.T) {
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), UserID: 41, Status: WorkflowRunStatusRunning,
	}
	events := agentObservability.NewInMemoryEventStore(10)
	repo := &traceOwnershipRepositoryFake{run: run}
	svc := &AgentService{
		repo: repo, traceEventReader: events,
		workflowEventHeartbeat: time.Second, workflowEventWindow: time.Second,
	}
	received := make([]agentObservability.TraceEvent, 0, 2)

	require.NoError(t, events.RecordRun(context.Background(), agentObservability.RunRecord{
		RecordID: run.ID.Hex(), RunID: run.ID.Hex(), UserID: run.UserID, Status: WorkflowRunStatusRunning,
	}))
	require.NoError(t, events.RecordRun(context.Background(), agentObservability.RunRecord{
		RecordID: run.ID.Hex(), RunID: run.ID.Hex(), UserID: run.UserID, Status: WorkflowRunStatusSuccess,
	}))
	err := svc.WatchWorkflowRunEvents(context.Background(), run.UserID, run.ID.Hex(), "", func(event agentObservability.TraceEvent) error {
		received = append(received, event)
		return nil
	})

	require.NoError(t, err)
	require.Len(t, received, 2)
	require.False(t, received[0].Terminal)
	require.True(t, received[1].Terminal)
	require.Equal(t, WorkflowRunStatusSuccess, received[1].Run.Status)

	err = svc.WatchWorkflowRunEvents(context.Background(), 42, run.ID.Hex(), "", func(agentObservability.TraceEvent) error { return nil })
	require.ErrorContains(t, err, "workflow run not found")
}

func TestWatchWorkflowRunEventsEmitsHeartbeatAndWindowBoundary(t *testing.T) {
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), UserID: 7, Status: WorkflowRunStatusRunning,
	}
	events := agentObservability.NewInMemoryEventStore(10)
	svc := &AgentService{
		repo:                   &traceOwnershipRepositoryFake{run: run},
		traceEventReader:       &immediateFirstEventReader{delegate: events},
		workflowEventHeartbeat: 10 * time.Millisecond, workflowEventWindow: 35 * time.Millisecond,
	}
	received := make([]agentObservability.TraceEvent, 0, 4)
	err := svc.WatchWorkflowRunEvents(context.Background(), run.UserID, run.ID.Hex(), "", func(event agentObservability.TraceEvent) error {
		received = append(received, event)
		return nil
	})

	require.NoError(t, err)
	require.GreaterOrEqual(t, len(received), 2)
	require.True(t, received[0].Heartbeat)
	require.Equal(t, "window_expired", received[len(received)-1].Reason)
}

func TestWatchWorkflowRunEventsHonorsCancellation(t *testing.T) {
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), UserID: 9, Status: WorkflowRunStatusRunning,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := &AgentService{
		repo:             &traceOwnershipRepositoryFake{run: run},
		traceEventReader: agentObservability.NewInMemoryEventStore(10),
	}
	err := svc.WatchWorkflowRunEvents(ctx, run.UserID, run.ID.Hex(), "", func(agentObservability.TraceEvent) error { return nil })
	require.ErrorIs(t, err, context.Canceled)
}

type immediateFirstEventReader struct {
	delegate agentObservability.EventReader
	called   bool
}

func (r *immediateFirstEventReader) ReadTraceEvents(
	ctx context.Context,
	userID uint64,
	runID string,
	afterCursor string,
	limit int,
	block time.Duration,
) (agentObservability.TraceEventBatch, error) {
	if !r.called {
		r.called = true
		return agentObservability.TraceEventBatch{}, nil
	}
	return r.delegate.ReadTraceEvents(ctx, userID, runID, afterCursor, limit, block)
}
