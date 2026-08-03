package remote

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusMetrics struct {
	healthChecks   *prometheus.CounterVec
	healthDuration *prometheus.HistogramVec
	healthCycles   *prometheus.CounterVec
	healthClaimed  prometheus.Counter
	poolEvents     *prometheus.CounterVec
	poolSessions   *prometheus.GaugeVec
	productEvents  *prometheus.CounterVec
}

type ProductObserver interface {
	RecordProductEvent(scope string, transport string, event string)
}

func NewPrometheusMetrics(registerer prometheus.Registerer) (*PrometheusMetrics, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	metrics := &PrometheusMetrics{
		healthChecks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_external_mcp_health_checks_total",
			Help: "External MCP health-check outcomes without tenant or connection labels.",
		}, []string{"transport", "result", "error_code"}),
		healthDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "agent_external_mcp_health_check_duration_seconds",
			Help:    "External MCP health-check duration.",
			Buckets: prometheus.DefBuckets,
		}, []string{"transport", "result"}),
		healthCycles: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_external_mcp_health_cycles_total",
			Help: "External MCP health reconciliation cycles.",
		}, []string{"result"}),
		healthClaimed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "agent_external_mcp_health_claimed_total",
			Help: "External MCP connections claimed for active health checks.",
		}),
		poolEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_external_mcp_pool_events_total",
			Help: "Bounded external MCP client-pool lifecycle events.",
		}, []string{"event"}),
		poolSessions: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "agent_external_mcp_pool_sessions",
			Help: "Current external MCP client-pool sessions by bounded state.",
		}, []string{"state"}),
		productEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_external_mcp_product_events_total",
			Help: "Durably attributed external MCP Connector funnel transitions.",
		}, []string{"scope", "transport", "event"}),
	}
	collectors := []prometheus.Collector{
		metrics.healthChecks,
		metrics.healthDuration,
		metrics.healthCycles,
		metrics.healthClaimed,
		metrics.poolEvents,
		metrics.poolSessions,
		metrics.productEvents,
	}
	registered := make([]prometheus.Collector, 0, len(collectors))
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			for _, previous := range registered {
				registerer.Unregister(previous)
			}
			return nil, fmt.Errorf("register external MCP metrics: %w", err)
		}
		registered = append(registered, collector)
	}
	return metrics, nil
}

func (metrics *PrometheusMetrics) RecordProductEvent(scope string, transport string, event string) {
	if metrics == nil {
		return
	}
	scope = boundedMetricValue(scope, "unknown", ScopeUser, ScopeProject)
	transport = boundedMetricValue(transport, "unknown", TransportStreamableHTTP, TransportSSE)
	event = boundedMetricValue(event, "other", "configured", "activated", "first_used", "reused")
	metrics.productEvents.WithLabelValues(scope, transport, event).Inc()
}

func (metrics *PrometheusMetrics) RecordHealthCheck(
	transport string,
	result string,
	errorCode string,
	duration time.Duration,
) {
	if metrics == nil {
		return
	}
	transport = boundedMetricValue(transport, "unknown", TransportStreamableHTTP, TransportSSE)
	result = boundedMetricValue(result, "unknown", HealthOutcomeHealthy, HealthOutcomeFailed, HealthOutcomeSkipped, "persist_failed")
	errorCode = boundedMetricValue(
		errorCode,
		"other",
		"none",
		"timeout",
		"canceled",
		"pool_saturated",
		"pool_closed",
		"session_invalidated",
		"credential_unavailable",
		"endpoint_not_allowed",
		"connection_failed",
		"store_error",
	)
	metrics.healthChecks.WithLabelValues(transport, result, errorCode).Inc()
	metrics.healthDuration.WithLabelValues(transport, result).Observe(duration.Seconds())
}

func (metrics *PrometheusMetrics) RecordHealthCycle(result string, claimed int) {
	if metrics == nil {
		return
	}
	result = boundedMetricValue(result, "other", "completed", "empty", "canceled", "claim_failed", "reset_failed")
	metrics.healthCycles.WithLabelValues(result).Inc()
	if claimed > 0 {
		metrics.healthClaimed.Add(float64(claimed))
	}
}

func (metrics *PrometheusMetrics) RecordPoolEvent(event string) {
	if metrics == nil {
		return
	}
	event = boundedMetricValue(
		event,
		"other",
		PoolEventOpened,
		PoolEventReused,
		PoolEventClosed,
		PoolEventOpenFailed,
		PoolEventSaturated,
		PoolEventInvalidated,
	)
	metrics.poolEvents.WithLabelValues(event).Inc()
}

func (metrics *PrometheusMetrics) SetPoolStats(stats PoolStats) {
	if metrics == nil {
		return
	}
	metrics.poolSessions.WithLabelValues("total").Set(float64(stats.Total))
	metrics.poolSessions.WithLabelValues("idle").Set(float64(stats.Idle))
	metrics.poolSessions.WithLabelValues("in_use").Set(float64(stats.InUse))
	metrics.poolSessions.WithLabelValues("opening").Set(float64(stats.Opening))
}

func boundedMetricValue(value, fallback string, allowed ...string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return fallback
}
