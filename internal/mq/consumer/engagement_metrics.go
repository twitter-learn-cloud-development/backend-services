package consumer

import (
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

type EngagementObserver interface {
	Observe(string, string, time.Duration)
}
type EngagementMetrics struct {
	events   *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func NewEngagementMetrics(reg prometheus.Registerer) (*EngagementMetrics, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	m := &EngagementMetrics{events: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "engagement_projection_events_total", Help: "Bounded engagement projection outcomes."}, []string{"kind", "result"}), duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "engagement_projection_seconds", Help: "Engagement delivery projection latency."}, []string{"kind"})}
	if err := reg.Register(m.events); err != nil {
		return nil, err
	}
	if err := reg.Register(m.duration); err != nil {
		return nil, err
	}
	return m, nil
}
func (m *EngagementMetrics) Observe(kind, result string, d time.Duration) {
	if kind != RoutingKeyTweetLiked && kind != RoutingKeyCommentCreated {
		kind = "unknown"
	}
	switch result {
	case "applied", "duplicate", "no_topics", "limited", "expired", "malformed", "failed", "cancelled", "ack_uncertain", "consume_unavailable":
	default:
		result = "unknown"
	}
	m.events.WithLabelValues(kind, result).Inc()
	m.duration.WithLabelValues(kind).Observe(d.Seconds())
}
func (c *TimelineConsumer) SetEngagementObserver(observer EngagementObserver) {
	c.engagementObserver = observer
}
