package service

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusRiskControlObserver struct {
	events *prometheus.CounterVec
}

func NewPrometheusRiskControlObserver(registerer prometheus.Registerer) (*PrometheusRiskControlObserver, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	observer := &PrometheusRiskControlObserver{
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_risk_control_events_total",
			Help: "Risk control queue events by bounded processing result.",
		}, []string{"result"}),
	}
	if err := registerer.Register(observer.events); err != nil {
		return nil, fmt.Errorf("register risk control metrics: %w", err)
	}
	return observer, nil
}

func (o *PrometheusRiskControlObserver) Observe(result string) {
	if o == nil {
		return
	}
	o.events.WithLabelValues(boundedRiskControlResult(result)).Inc()
}

func boundedRiskControlResult(result string) string {
	switch result {
	case "received", "dispatched", "duplicate", "malformed", "retried", "dlq",
		"publish_failed", "requeued", "shutdown_requeued", "acknowledgement_uncertain":
		return result
	default:
		return "unknown"
	}
}
