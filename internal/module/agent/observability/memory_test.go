package observability

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInMemoryRecorderUpsertsAndIsolatesTenant(t *testing.T) {
	recorder := NewInMemoryRecorder()
	ctx := context.Background()
	require.NoError(t, recorder.RecordRun(ctx, RunRecord{RecordID: "run-1", RunID: "run-1", UserID: 7, Status: "running"}))
	require.NoError(t, recorder.RecordRun(ctx, RunRecord{RecordID: "run-1", RunID: "run-1", UserID: 7, Status: "success"}))
	require.NoError(t, recorder.RecordRun(ctx, RunRecord{RecordID: "run-1", RunID: "run-1", UserID: 8, Status: "tenant-8"}))
	require.NoError(t, recorder.RecordStep(ctx, StepRecord{RecordID: "run-1:step:b", RunID: "run-1", UserID: 7, StepID: "b", Sequence: 2}))
	require.NoError(t, recorder.RecordStep(ctx, StepRecord{RecordID: "run-1:step:a", RunID: "run-1", UserID: 7, StepID: "a", Sequence: 1}))
	require.NoError(t, recorder.RecordStep(ctx, StepRecord{RecordID: "other:step:a", RunID: "run-1", UserID: 8, StepID: "a", Sequence: 1}))

	bundle, err := recorder.GetTraceBundle(ctx, 7, "run-1")
	require.NoError(t, err)
	require.Equal(t, "success", bundle.Run.Status)
	require.Equal(t, []string{"a", "b"}, []string{bundle.Steps[0].StepID, bundle.Steps[1].StepID})
	tenantEight, err := recorder.GetTraceBundle(ctx, 8, "run-1")
	require.NoError(t, err)
	require.Equal(t, "tenant-8", tenantEight.Run.Status)
	require.Len(t, tenantEight.Steps, 1)
	crossTenant, err := recorder.GetTraceBundle(ctx, 9, "run-1")
	require.NoError(t, err)
	require.Nil(t, crossTenant.Run)
	require.Empty(t, crossTenant.Steps)
}

func TestInMemoryRecorderSupportsConcurrentWriters(t *testing.T) {
	recorder := NewInMemoryRecorder()
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(sequence int) {
			defer wg.Done()
			errs <- recorder.RecordStep(context.Background(), StepRecord{
				RecordID: "run-2:step:" + string(rune('a'+sequence)), RunID: "run-2", UserID: 10, Sequence: sequence,
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	bundle, err := recorder.GetTraceBundle(context.Background(), 10, "run-2")
	require.NoError(t, err)
	require.Len(t, bundle.Steps, 32)
}
