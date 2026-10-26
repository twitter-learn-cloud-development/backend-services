package consumer

import (
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

type TrendsDecayObserver interface{ Observe(string, time.Duration) }
type TrendsDecayMetrics struct {
	events   *prometheus.CounterVec
	duration prometheus.Histogram
}

func NewTrendsDecayMetrics(reg prometheus.Registerer) (*TrendsDecayMetrics, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m := &TrendsDecayMetrics{events: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "trends_decay_bucket_total", Help: "Bounded trend decay bucket outcomes."}, []string{"result"}), duration: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "trends_decay_bucket_seconds", Help: "Bounded trend decay execution latency."})}
	if err := reg.Register(m.events); err != nil {
		return nil, err
	}
	if err := reg.Register(m.duration); err != nil {
		return nil, err
	}
	return m, nil
}
func (m *TrendsDecayMetrics) Observe(result string, d time.Duration) {
	switch result {
	case "applied", "duplicate", "empty", "oversized", "invalid_state", "clock_regression", "failed", "cancelled":
	default:
		result = "unknown"
	}
	m.events.WithLabelValues(result).Inc()
	m.duration.Observe(d.Seconds())
}
func (c *TimelineConsumer) SetTrendsDecayObserver(observer TrendsDecayObserver) {
	c.decayObserver = observer
}
