package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type OTelRecorder struct {
	tracer oteltrace.Tracer
}

func NewOTelRecorder(provider oteltrace.TracerProvider) *OTelRecorder {
	if provider == nil {
		provider = otel.GetTracerProvider()
	}
	return &OTelRecorder{tracer: provider.Tracer("twitter-clone/agent-runtime")}
}

func (r *OTelRecorder) RecordRun(ctx context.Context, record RunRecord) error {
	r.recordSpan(ctx, "agent.run", record.StartedAt, record.FinishedAt, record.Status, record.ErrorClass,
		attribute.String("agent.run.id", record.RunID),
		attribute.String("agent.workflow.id", record.WorkflowID),
		attribute.String("agent.source", record.Source),
		attribute.String("agent.mode", record.Mode),
		attribute.String("agent.strategy", record.Strategy),
		attribute.Int("agent.usage.total_tokens", record.Usage.TotalTokens),
		attribute.Int64("agent.usage.cost_micros", record.Usage.EstimatedCostMicros),
		attribute.Int("agent.budget.max_steps", record.Budget.MaxSteps),
		attribute.Int("agent.budget.max_tokens", record.Budget.MaxTotalTokens),
	)
	return nil
}

func (r *OTelRecorder) RecordStep(ctx context.Context, record StepRecord) error {
	r.recordSpan(ctx, "agent.step", record.StartedAt, record.FinishedAt, record.Status, record.ErrorClass,
		attribute.String("agent.run.id", record.RunID),
		attribute.String("agent.workflow.id", record.WorkflowID),
		attribute.String("agent.source", record.Source),
		attribute.String("agent.step.id", record.StepID),
		attribute.String("agent.step.parent_id", record.ParentStepID),
		attribute.String("agent.step.type", record.StepType),
		attribute.Int("agent.step.sequence", record.Sequence),
		attribute.Int("agent.step.attempt", record.Attempt),
	)
	return nil
}

func (r *OTelRecorder) RecordLLMCall(ctx context.Context, record LLMCallRecord) error {
	r.recordSpan(ctx, "agent.llm", record.StartedAt, record.FinishedAt, record.Status, record.ErrorClass,
		attribute.String("agent.run.id", record.RunID),
		attribute.String("agent.workflow.id", record.WorkflowID),
		attribute.String("agent.source", record.Source),
		attribute.String("agent.step.id", record.StepID),
		attribute.String("gen_ai.system", record.Provider),
		attribute.String("gen_ai.request.model", record.Model),
		attribute.String("agent.prompt.template_id", record.PromptTemplateID),
		attribute.String("agent.prompt.template_version", record.PromptTemplateVersion),
		attribute.Int("gen_ai.usage.input_tokens", record.Usage.InputTokens),
		attribute.Int("gen_ai.usage.output_tokens", record.Usage.OutputTokens),
		attribute.Bool("agent.usage.estimated", record.Usage.Estimated),
		attribute.Int64("agent.usage.cost_micros", record.Usage.EstimatedCostMicros),
		attribute.Int("agent.prompt.length", record.PromptLength),
		attribute.Int("agent.completion.length", record.CompletionLength),
	)
	return nil
}

func (r *OTelRecorder) RecordToolCall(ctx context.Context, record ToolCallRecord) error {
	r.recordSpan(ctx, "agent.tool", record.StartedAt, record.FinishedAt, record.Status, record.ErrorClass,
		attribute.String("agent.run.id", record.RunID),
		attribute.String("agent.workflow.id", record.WorkflowID),
		attribute.String("agent.source", record.Source),
		attribute.String("agent.step.id", record.StepID),
		attribute.String("agent.tool.name", record.ToolName),
		attribute.String("agent.tool.category", record.Category),
		attribute.Int("agent.tool.attempts", record.Attempts),
		attribute.Int("agent.tool.arguments_length", record.ArgumentsLength),
		attribute.Int("agent.tool.output_length", record.OutputLength),
	)
	return nil
}

func (r *OTelRecorder) recordSpan(
	ctx context.Context,
	name string,
	startedAt, finishedAt time.Time,
	status, errorClass string,
	attributes ...attribute.KeyValue,
) {
	if r == nil || r.tracer == nil {
		return
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	if finishedAt.IsZero() || finishedAt.Before(startedAt) {
		finishedAt = startedAt
	}
	attributes = append(attributes,
		attribute.String("agent.status", status),
		attribute.String("error.type", errorClass),
	)
	_, span := r.tracer.Start(ctx, name, oteltrace.WithTimestamp(startedAt), oteltrace.WithAttributes(attributes...))
	if traceStatusIsError(status, errorClass) {
		span.SetStatus(codes.Error, errorClass)
	}
	span.End(oteltrace.WithTimestamp(finishedAt))
}

func traceStatusIsError(status, errorClass string) bool {
	if errorClass == "" || errorClass == "canceled" || errorClass == "suspended" {
		return false
	}
	switch status {
	case "failed", "rejected", "timed_out", "compensation_failed":
		return true
	default:
		return false
	}
}
