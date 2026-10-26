package service

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"twitter-clone/internal/module/agent/workflow/rag"
)

func TestSessionSummaryMetricsBoundLabelsAndRecordChunks(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := NewPrometheusSessionSummaryObserver(registry)
	if err != nil {
		t.Fatalf("NewPrometheusSessionSummaryObserver() error = %v", err)
	}
	observer.ObserveSessionSummary(SessionSummaryObservation{
		Trigger: "tenant-value", Outcome: "tenant-value", Duration: time.Second,
	})
	observer.ObserveSemanticContext(SemanticContextObservation{
		Intent: rag.Intent("tenant-value"), Outcome: "tenant-value", ChunkCount: 4,
		Duration: time.Second,
	})
	if got := prometheusCounterValue(t, observer.summaryAttempts.WithLabelValues("unknown", "failed")); got != 1 {
		t.Fatalf("summary counter = %v, want 1", got)
	}
	if got := prometheusCounterValue(t, observer.retrievals.WithLabelValues("unknown", "failed")); got != 1 {
		t.Fatalf("retrieval counter = %v, want 1", got)
	}
}
