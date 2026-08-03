package consumer

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestModerationCleanupMetricsBoundLabels(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	observer, err := NewPrometheusModerationCleanupObserver(registry)
	require.NoError(t, err)

	observer.ObserveEvent("tenant-or-event-id")
	observer.ObservePage("unexpected", 5, 2)
	observer.ObserveDuration("unexpected", time.Second)

	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				require.NotEqual(t, "tenant-or-event-id", label.GetValue())
			}
		}
	}
}
