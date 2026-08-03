package consumer

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestPrometheusOutboxWorkerObserverBoundsLabelsAndAddsCounts(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	observer, err := NewPrometheusOutboxWorkerObserver(registry)
	require.NoError(t, err)

	observer.ObserveOutbox(outboxOperationClaim, "claimed", 3)
	observer.ObserveOutbox("task-123", "database timeout", 1)
	observer.ObserveOutbox(outboxOperationClaim, "claimed", 0)

	families, err := registry.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)
	require.Equal(t, "timeline_outbox_worker_operations_total", families[0].GetName())
	require.Len(t, families[0].Metric, 2)

	observed := make(map[string]float64, 2)
	for _, metric := range families[0].Metric {
		labels := make(map[string]string, 2)
		for _, label := range metric.Label {
			labels[label.GetName()] = label.GetValue()
		}
		observed[labels["operation"]+":"+labels["result"]] = metric.GetCounter().GetValue()
	}
	require.Equal(t, map[string]float64{
		"claim:claimed":   3,
		"unknown:unknown": 1,
	}, observed)
}
