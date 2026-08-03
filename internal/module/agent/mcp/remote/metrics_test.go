package remote

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestPrometheusMetricsCollapseUnboundedLabels(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	metrics, err := NewPrometheusMetrics(registry)
	if err != nil {
		t.Fatalf("NewPrometheusMetrics() error = %v", err)
	}
	metrics.RecordHealthCheck("user-controlled-transport", "arbitrary-result", "raw remote error", time.Second)
	metrics.RecordHealthCycle("arbitrary-cycle", 2)
	metrics.RecordPoolEvent("arbitrary-event")
	metrics.RecordProductEvent("tenant-scope", "tenant-transport", "tenant-product-event")
	metrics.SetPoolStats(PoolStats{Total: 2, Idle: 1, InUse: 1})

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	labels := make(map[string]bool)
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				labels[label.GetName()+"="+label.GetValue()] = true
			}
		}
	}
	for _, expected := range []string{"transport=unknown", "result=unknown", "error_code=other", "event=other", "scope=unknown"} {
		if !labels[expected] {
			t.Fatalf("missing bounded label %q in %+v", expected, labels)
		}
	}
	for _, forbidden := range []string{"transport=user-controlled-transport", "error_code=raw remote error"} {
		if labels[forbidden] {
			t.Fatalf("unbounded label was exported: %s", forbidden)
		}
	}
}
