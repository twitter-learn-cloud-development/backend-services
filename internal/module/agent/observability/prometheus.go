package observability

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusRecorder struct {
	runs         *prometheus.CounterVec
	runDuration  *prometheus.HistogramVec
	steps        *prometheus.CounterVec
	stepDuration *prometheus.HistogramVec
	llmRequests  *prometheus.CounterVec
	llmDuration  *prometheus.HistogramVec
	llmTokens    *prometheus.CounterVec
	llmCost      *prometheus.CounterVec
}

func NewPrometheusRecorder(registerer prometheus.Registerer) (*PrometheusRecorder, error) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}
	recorder := &PrometheusRecorder{
		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_runs_total", Help: "Agent run state records by bounded execution dimensions.",
		}, []string{"source", "strategy", "status"}),
		runDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "agent_run_duration_seconds", Help: "Agent run duration by bounded execution dimensions.", Buckets: prometheus.DefBuckets,
		}, []string{"source", "strategy", "status"}),
		steps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_steps_total", Help: "Agent step records by bounded execution dimensions.",
		}, []string{"source", "step_type", "status"}),
		stepDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "agent_step_duration_seconds", Help: "Agent step duration by bounded execution dimensions.", Buckets: prometheus.DefBuckets,
		}, []string{"source", "step_type", "status"}),
		llmRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_llm_requests_total", Help: "Agent LLM calls without tenant, run, prompt, or model labels.",
		}, []string{"source", "provider", "status"}),
		llmDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "agent_llm_duration_seconds", Help: "Agent LLM call duration without tenant, run, prompt, or model labels.", Buckets: prometheus.DefBuckets,
		}, []string{"source", "provider", "status"}),
		llmTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_llm_tokens_total", Help: "Agent LLM token usage by direction and estimation state.",
		}, []string{"source", "provider", "direction", "estimated"}),
		llmCost: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "agent_llm_estimated_cost_micros_total", Help: "Agent LLM cost in integer micro-units.",
		}, []string{"source", "provider", "estimated"}),
	}
	collectors := []prometheus.Collector{
		recorder.runs, recorder.runDuration, recorder.steps, recorder.stepDuration,
		recorder.llmRequests, recorder.llmDuration, recorder.llmTokens, recorder.llmCost,
	}
	registered := make([]prometheus.Collector, 0, len(collectors))
	for _, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			for _, previous := range registered {
				registerer.Unregister(previous)
			}
			return nil, fmt.Errorf("register agent execution metrics: %w", err)
		}
		registered = append(registered, collector)
	}
	return recorder, nil
}

func (r *PrometheusRecorder) RecordRun(_ context.Context, record RunRecord) error {
	if r == nil {
		return nil
	}
	labels := []string{boundedSource(record.Source), boundedStrategy(record.Strategy), boundedRunStatus(record.Status)}
	r.runs.WithLabelValues(labels...).Inc()
	observeMilliseconds(r.runDuration.WithLabelValues(labels...), record.DurationMS)
	return nil
}

func (r *PrometheusRecorder) RecordStep(_ context.Context, record StepRecord) error {
	if r == nil {
		return nil
	}
	labels := []string{boundedSource(record.Source), boundedStepType(record.StepType), boundedStepStatus(record.Status)}
	r.steps.WithLabelValues(labels...).Inc()
	observeMilliseconds(r.stepDuration.WithLabelValues(labels...), record.DurationMS)
	return nil
}

func (r *PrometheusRecorder) RecordLLMCall(_ context.Context, record LLMCallRecord) error {
	if r == nil {
		return nil
	}
	source := boundedSource(record.Source)
	provider := boundedProvider(record.Provider)
	status := boundedCallStatus(record.Status)
	r.llmRequests.WithLabelValues(source, provider, status).Inc()
	observeMilliseconds(r.llmDuration.WithLabelValues(source, provider, status), record.DurationMS)
	estimated := strconv.FormatBool(record.Usage.Estimated)
	if record.Usage.InputTokens > 0 {
		r.llmTokens.WithLabelValues(source, provider, "input", estimated).Add(float64(record.Usage.InputTokens))
	}
	if record.Usage.OutputTokens > 0 {
		r.llmTokens.WithLabelValues(source, provider, "output", estimated).Add(float64(record.Usage.OutputTokens))
	}
	if record.Usage.EstimatedCostMicros > 0 {
		r.llmCost.WithLabelValues(source, provider, strconv.FormatBool(record.Usage.CostEstimated)).Add(float64(record.Usage.EstimatedCostMicros))
	}
	return nil
}

func (r *PrometheusRecorder) RecordToolCall(context.Context, ToolCallRecord) error {
	return nil
}

func observeMilliseconds(observer prometheus.Observer, durationMS int64) {
	if durationMS >= 0 {
		observer.Observe(float64(durationMS) / 1000)
	}
}

func boundedSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SourceRuntime:
		return SourceRuntime
	case SourceWorkflow:
		return SourceWorkflow
	case "legacy_agent", "legacy":
		return "legacy"
	default:
		return "unknown"
	}
}

func boundedStrategy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "dag":
		return "dag"
	case value == "consult" || strings.HasPrefix(value, "consult."):
		return "consult"
	case value == "assist" || strings.HasPrefix(value, "assist."):
		return "assist"
	case value == "workflow" || strings.HasPrefix(value, "workflow."):
		return "workflow"
	case value == "multi" || strings.HasPrefix(value, "multi."):
		return "multi"
	default:
		return "unknown"
	}
}

func boundedRunStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "running", "suspended", "success", "completed", "failed", "rejected", "compensating", "compensated", "compensation_failed", "canceling", "canceled", "awaiting_human", "approval_required":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func boundedStepType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "start", "end", "llm", "tool", "agent", "agent_step", "router", "wait", "condition", "join":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func boundedStepStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending", "running", "retrying", "success", "failed", "skipped", "suspended", "canceled", "timed_out", "awaiting_human", "approval_required":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func boundedProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dashscope":
		return "dashscope"
	case "lmstudio", "lm-studio":
		return "lmstudio"
	case "openai":
		return "openai"
	case "custom":
		return "custom"
	case "legacy":
		return "legacy"
	default:
		return "unknown"
	}
}

func boundedCallStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "success", "failed", "canceled", "timeout":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}
