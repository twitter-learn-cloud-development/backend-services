package tool

import (
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusMetrics struct {
	executions      *prometheus.CounterVec
	duration        *prometheus.HistogramVec
	attempts        *prometheus.CounterVec
	circuitState    *prometheus.GaugeVec
	reconciled      *prometheus.CounterVec
	reconciliations *prometheus.CounterVec
}

func NewPrometheusMetrics(registerer prometheus.Registerer) (*PrometheusMetrics, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	metrics := &PrometheusMetrics{
		executions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_tool_executions_total", Help: "Governed agent tool execution decisions.",
		}, []string{"tool", "category", "source", "decision", "error_code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "agent_tool_duration_seconds", Help: "Governed agent tool execution duration.", Buckets: prometheus.DefBuckets,
		}, []string{"tool", "category", "source", "decision"}),
		attempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_tool_attempts_total", Help: "Downstream agent tool handler attempts.",
		}, []string{"tool", "category", "source"}),
		circuitState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "agent_tool_circuit_state", Help: "One-hot state of each agent tool circuit breaker.",
		}, []string{"tool", "state"}),
		reconciled: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_tool_governance_reconciled_total", Help: "Governance records repaired by reconciliation.",
		}, []string{"action"}),
		reconciliations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_tool_governance_reconciliations_total", Help: "Governance reconciliation runs.",
		}, []string{"result"}),
	}
	collectors := []prometheus.Collector{metrics.executions, metrics.duration, metrics.attempts, metrics.circuitState, metrics.reconciled, metrics.reconciliations}
	registered := make([]prometheus.Collector, 0, len(collectors))
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			for _, previous := range registered {
				registerer.Unregister(previous)
			}
			return nil, fmt.Errorf("register agent tool metrics: %w", err)
		}
		registered = append(registered, collector)
	}
	return metrics, nil
}

func (m *PrometheusMetrics) RecordToolExecution(event AuditEvent) {
	if m == nil {
		return
	}
	toolName := metricToolName(event.ToolName)
	category := metricValue(string(event.Category), "unknown")
	source := metricValue(string(event.Source), "unknown")
	decision := metricValue(event.Decision, "unknown")
	errorCode := metricValue(string(event.ErrorCode), "none")
	m.executions.WithLabelValues(toolName, category, source, decision, errorCode).Inc()
	m.duration.WithLabelValues(toolName, category, source, decision).Observe(event.Duration.Seconds())
	if event.Attempts > 0 {
		m.attempts.WithLabelValues(toolName, category, source).Add(float64(event.Attempts))
	}
}

func (m *PrometheusMetrics) SetCircuitState(toolName string, state CircuitState) {
	if m == nil {
		return
	}
	toolName = metricToolName(toolName)
	for _, candidate := range []CircuitState{CircuitClosed, CircuitOpen, CircuitHalfOpen} {
		value := 0.0
		if candidate == state {
			value = 1
		}
		m.circuitState.WithLabelValues(toolName, string(candidate)).Set(value)
	}
}

func (m *PrometheusMetrics) RecordReconciliation(result string, actions map[string]int64) {
	if m == nil {
		return
	}
	m.reconciliations.WithLabelValues(metricValue(result, "unknown")).Inc()
	for action, count := range actions {
		if count > 0 {
			m.reconciled.WithLabelValues(metricValue(action, "unknown")).Add(float64(count))
		}
	}
}

func metricToolName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if len(value) > 96 {
		return "dynamic"
	}
	return value
}

func metricValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
