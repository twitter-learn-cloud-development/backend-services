package consumer

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestPrometheusTweetCreatedObserverBoundsLabels(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	observer, err := NewPrometheusTweetCreatedObserver(registry)
	require.NoError(t, err)

	observer.ObserveStage(tweetCreatedStageTrends, "duplicate")
	observer.ObserveStage("tweet-123", "database timeout")

	families, err := registry.Gather()
	require.NoError(t, err)
	require.Len(t, families, 1)
	require.Equal(t, "timeline_tweet_created_stage_total", families[0].GetName())
	require.Len(t, families[0].Metric, 2)

	observed := make(map[string]float64, 2)
	for _, metric := range families[0].Metric {
		labels := make(map[string]string, 2)
		for _, label := range metric.Label {
			labels[label.GetName()] = label.GetValue()
		}
		observed[labels["stage"]+":"+labels["result"]] = metric.GetCounter().GetValue()
	}
	require.Equal(t, map[string]float64{
		"trends:duplicate": 1,
		"unknown:unknown":  1,
	}, observed)
}
