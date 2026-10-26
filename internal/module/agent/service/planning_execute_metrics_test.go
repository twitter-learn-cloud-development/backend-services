package service

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestPrometheusPlanningExecuteObserverBoundsLabelsAndCountsRepairs(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := NewPrometheusPlanningExecuteObserver(registry)
	if err != nil {
		t.Fatalf("NewPrometheusPlanningExecuteObserver() error = %v", err)
	}
	observer.ObservePlanningExecute(PlanningExecuteObservation{
		Phase: "tenant-controlled", ExecutionProfile: "tenant-controlled",
		Outcome: "tenant-controlled", Verification: "tenant-controlled",
		PlanningCalls: 2, AdmissionRepairs: 1, RecoveryAttempts: 3,
		Duration: time.Second,
	})

	if got := prometheusCounterValue(t, observer.runs.WithLabelValues(
		"unknown", "other", "error", "unknown",
	)); got != 1 {
		t.Fatalf("bounded run counter = %v, want 1", got)
	}
	if got := prometheusCounterValue(t, observer.calls.WithLabelValues("unknown", "other")); got != 2 {
		t.Fatalf("planning calls = %v, want 2", got)
	}
	if got := prometheusCounterValue(t, observer.repairs.WithLabelValues("other", "admission")); got != 1 {
		t.Fatalf("admission repairs = %v, want 1", got)
	}
	if got := prometheusCounterValue(t, observer.repairs.WithLabelValues("other", "recovery")); got != 3 {
		t.Fatalf("recovery attempts = %v, want 3", got)
	}
	observer.ObservePlanningExecute(PlanningExecuteObservation{
		Phase: planningExecutePhaseInitial, ExecutionProfile: ExecutionProfileRuntimeWebSearch,
		Outcome: "verified", Verification: "passed",
		PlanningCalls: 1, Duration: time.Second,
	})
	if got := prometheusCounterValue(t, observer.runs.WithLabelValues(
		planningExecutePhaseInitial, ExecutionProfileRuntimeWebSearch, "verified", "passed",
	)); got != 1 {
		t.Fatalf("web research run counter = %v, want 1", got)
	}
}
