package observability

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTraceEventCursorValidationAndOrdering(t *testing.T) {
	cursor, err := NormalizeTraceEventCursor("")
	require.NoError(t, err)
	require.Equal(t, "0-0", cursor)
	_, err = NormalizeTraceEventCursor("$ OR secret")
	require.Error(t, err)
	comparison, err := CompareTraceEventCursor("12-3", "12-4")
	require.NoError(t, err)
	require.Equal(t, -1, comparison)
	comparison, err = CompareTraceEventCursor("13-0", "12-999")
	require.NoError(t, err)
	require.Equal(t, 1, comparison)
}

func TestInMemoryEventStoreIsTenantScopedBoundedAndResumable(t *testing.T) {
	store := NewInMemoryEventStore(2)
	ctx := context.Background()
	require.NoError(t, store.RecordRun(ctx, RunRecord{RecordID: "run", RunID: "run", UserID: 7, Status: "running"}))
	require.NoError(t, store.RecordStep(ctx, StepRecord{RecordID: "step", RunID: "run", UserID: 7, StepID: "one"}))
	first, err := store.ReadTraceEvents(ctx, 7, "run", "", 1000, 0)
	require.NoError(t, err)
	require.Len(t, first.Events, 2)
	require.Equal(t, TraceEventRun, first.Events[0].Kind)

	lastCursor := first.Events[1].Cursor
	require.NoError(t, store.RecordRun(ctx, RunRecord{RecordID: "run", RunID: "run", UserID: 7, Status: "success"}))
	resumed, err := store.ReadTraceEvents(ctx, 7, "run", lastCursor, 10, 0)
	require.NoError(t, err)
	require.Len(t, resumed.Events, 1)
	require.Equal(t, "success", resumed.Events[0].Run.Status)

	trimmed, err := store.ReadTraceEvents(ctx, 7, "run", first.Events[0].Cursor, 10, 0)
	require.NoError(t, err)
	require.True(t, trimmed.Reset)
	otherTenant, err := store.ReadTraceEvents(ctx, 8, "run", "", 10, 0)
	require.NoError(t, err)
	require.Empty(t, otherTenant.Events)
}

func TestInMemoryEventStoreBlockingReadObservesCancellation(t *testing.T) {
	store := NewInMemoryEventStore(10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := store.ReadTraceEvents(ctx, 9, "run", "", 10, time.Second)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("blocking event read ignored context cancellation")
	}
}
