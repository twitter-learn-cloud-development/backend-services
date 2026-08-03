package service

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestRiskControlMetricsBoundLabels(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	observer, err := NewPrometheusRiskControlObserver(registry)
	require.NoError(t, err)

	observer.Observe("tweet-or-user-id")
	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				require.NotEqual(t, "tweet-or-user-id", label.GetValue())
				require.Equal(t, "unknown", label.GetValue())
			}
		}
	}
}
