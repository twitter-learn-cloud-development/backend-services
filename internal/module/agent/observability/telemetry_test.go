package observability

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestPrometheusRecorderUsesOnlyBoundedLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	recorder, err := NewPrometheusRecorder(registry)
	require.NoError(t, err)
	require.NoError(t, recorder.RecordRun(context.Background(), RunRecord{
		RunID: "tenant-run-high-cardinality", UserID: 991, Source: "tenant-source",
		Strategy: "tenant-strategy-991", Status: "tenant-status", DurationMS: 125,
	}))
	require.NoError(t, recorder.RecordStep(context.Background(), StepRecord{
		RunID: "tenant-run-high-cardinality", UserID: 991, Source: SourceWorkflow,
		StepID: "tenant-step-high-cardinality", StepType: "tenant-step", Status: "success", DurationMS: 25,
	}))
	require.NoError(t, recorder.RecordLLMCall(context.Background(), LLMCallRecord{
		RunID: "tenant-run-high-cardinality", UserID: 991, Source: SourceRuntime,
		StepID: "tenant-step-high-cardinality", Provider: "tenant-provider-991", Model: "tenant-model-991",
		Status: "success", Usage: TokenUsage{
			InputTokens: 10, OutputTokens: 4, TotalTokens: 14, Estimated: true,
			EstimatedCostMicros: 7, CostEstimated: true,
		},
	}))

	families, err := registry.Gather()
	require.NoError(t, err)
	require.NotEmpty(t, families)
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				require.NotContains(t, []string{"user_id", "run_id", "step_id", "model", "error"}, label.GetName())
				require.NotContains(t, label.GetValue(), "tenant-")
			}
		}
	}
}

func TestOTelRecorderCreatesChildSpanWithoutSensitiveContent(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	ctx, parent := provider.Tracer("test").Start(context.Background(), "request")
	parentSpanID := parent.SpanContext().SpanID()
	startedAt := time.Now()
	recorder := NewOTelRecorder(provider)
	require.NoError(t, recorder.RecordLLMCall(ctx, LLMCallRecord{
		RunID: "run-1", WorkflowID: "workflow-1", Source: SourceWorkflow, StepID: "llm-1",
		Provider: "dashscope", Model: "qwen", Status: "failed", ErrorClass: "provider_error",
		PromptHash: "private-prompt-hash", CompletionHash: "private-completion-hash",
		PromptTemplateID: "assist.draft.system", PromptTemplateVersion: "v1",
		PromptSample: "private prompt preview", CompletionSample: "private completion preview",
		PromptLength: 50, CompletionLength: 20, Usage: TokenUsage{InputTokens: 8, OutputTokens: 3},
		StartedAt: startedAt, FinishedAt: startedAt.Add(20 * time.Millisecond),
	}))
	parent.End()

	var llmSpan sdktrace.ReadOnlySpan
	for _, span := range spanRecorder.Ended() {
		if span.Name() == "agent.llm" {
			llmSpan = span
			break
		}
	}
	require.NotNil(t, llmSpan)
	require.Equal(t, parentSpanID, llmSpan.Parent().SpanID())
	attributes := traceAttributes(llmSpan.Attributes())
	require.Equal(t, "run-1", attributes["agent.run.id"])
	require.Equal(t, "50", attributes["agent.prompt.length"])
	require.Equal(t, "assist.draft.system", attributes["agent.prompt.template_id"])
	require.Equal(t, "v1", attributes["agent.prompt.template_version"])
	for _, value := range attributes {
		require.NotContains(t, value, "private-prompt-hash")
		require.NotContains(t, value, "private-completion-hash")
		require.NotContains(t, value, "private prompt preview")
		require.NotContains(t, value, "private completion preview")
	}
}

func TestFanoutRecorderWritesPersistenceAndTelemetry(t *testing.T) {
	memory := NewInMemoryRecorder()
	registry := prometheus.NewRegistry()
	metrics, err := NewPrometheusRecorder(registry)
	require.NoError(t, err)
	recorder := NewFanoutRecorder(memory, metrics)
	require.NoError(t, recorder.RecordRun(context.Background(), RunRecord{
		RecordID: "run-1", RunID: "run-1", UserID: 1, Source: SourceRuntime,
		Strategy: "consult", Status: "completed",
	}))
	bundle, err := memory.GetTraceBundle(context.Background(), 1, "run-1")
	require.NoError(t, err)
	require.Equal(t, "completed", bundle.Run.Status)
	families, err := registry.Gather()
	require.NoError(t, err)
	require.NotEmpty(t, families)
}

func traceAttributes(values []attribute.KeyValue) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		result[string(value.Key)] = fmt.Sprint(value.Value.AsInterface())
	}
	return result
}
