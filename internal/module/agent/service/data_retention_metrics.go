package service

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"twitter-clone/internal/module/agent/repository"
)

type PrometheusDataRetentionObserver struct {
	items  *prometheus.CounterVec
	cycles *prometheus.CounterVec
	timing prometheus.Histogram
}

func NewPrometheusDataRetentionObserver(registerer prometheus.Registerer) (*PrometheusDataRetentionObserver, error) {
	observer := &PrometheusDataRetentionObserver{
		items: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_data_retention_items_total",
			Help: "Agent application data retention items by bounded resource and outcome.",
		}, []string{"resource", "outcome"}),
		cycles: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_data_retention_cycles_total",
			Help: "Agent application data retention cycles by outcome.",
		}, []string{"outcome"}),
		timing: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "agent_data_retention_cycle_duration_seconds",
			Help:    "Agent application data retention cycle duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
	}
	if err := registerer.Register(observer.items); err != nil {
		return nil, err
	}
	if err := registerer.Register(observer.cycles); err != nil {
		return nil, err
	}
	if err := registerer.Register(observer.timing); err != nil {
		return nil, err
	}
	return observer, nil
}

func (o *PrometheusDataRetentionObserver) ObserveDataRetention(resource repository.DataRetentionResource, outcome string) {
	if o == nil {
		return
	}
	o.items.WithLabelValues(string(resource), outcome).Inc()
}

func (o *PrometheusDataRetentionObserver) ObserveDataRetentionCycle(outcome string, duration time.Duration) {
	if o == nil {
		return
	}
	o.cycles.WithLabelValues(outcome).Inc()
	o.timing.Observe(duration.Seconds())
}
