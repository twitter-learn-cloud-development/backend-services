package consumer

import (
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"twitter-clone/internal/module/agent/attribution"
)

type PrometheusContentEngagementObserver struct {
	events *prometheus.CounterVec
}

func NewPrometheusContentEngagementObserver(registerer prometheus.Registerer) (*PrometheusContentEngagementObserver, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	observer := &PrometheusContentEngagementObserver{events: prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agent_profile_content_engagement_events_total",
		Help: "Content engagement attribution events by bounded kind and processing result.",
	}, []string{"kind", "result"})}
	if err := registerer.Register(observer.events); err != nil {
		return nil, fmt.Errorf("register content engagement metrics: %w", err)
	}
	return observer, nil
}

func (o *PrometheusContentEngagementObserver) Observe(kind, result string) {
	if o == nil || o.events == nil {
		return
	}
	o.events.WithLabelValues(boundedContentEngagementKind(kind), boundedContentEngagementResult(result)).Inc()
}

func boundedContentEngagementKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case attribution.EngagementKindLike, attribution.EngagementKindComment:
		return strings.TrimSpace(kind)
	default:
		return "unknown"
	}
}

func boundedContentEngagementResult(result string) string {
	switch strings.TrimSpace(result) {
	case "attributed", "replayed", "ignored_non_agent", "ignored_self", "ignored_expired",
		"failed", "malformed", "retried", "dlq", "publish_failed", "requeued", "acknowledgement_uncertain":
		return strings.TrimSpace(result)
	default:
		return "unknown"
	}
}
