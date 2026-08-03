package observability

import (
	"context"
	"errors"
)

type fanoutRecorder struct {
	recorders []Recorder
}

func NewFanoutRecorder(recorders ...Recorder) Recorder {
	filtered := make([]Recorder, 0, len(recorders))
	for _, recorder := range recorders {
		if recorder != nil {
			filtered = append(filtered, recorder)
		}
	}
	switch len(filtered) {
	case 0:
		return NoopRecorder{}
	case 1:
		return filtered[0]
	default:
		return &fanoutRecorder{recorders: filtered}
	}
}

func (r *fanoutRecorder) RecordRun(ctx context.Context, record RunRecord) error {
	return r.record(func(recorder Recorder) error { return recorder.RecordRun(ctx, record) })
}

func (r *fanoutRecorder) RecordStep(ctx context.Context, record StepRecord) error {
	return r.record(func(recorder Recorder) error { return recorder.RecordStep(ctx, record) })
}

func (r *fanoutRecorder) RecordLLMCall(ctx context.Context, record LLMCallRecord) error {
	return r.record(func(recorder Recorder) error { return recorder.RecordLLMCall(ctx, record) })
}

func (r *fanoutRecorder) RecordToolCall(ctx context.Context, record ToolCallRecord) error {
	return r.record(func(recorder Recorder) error { return recorder.RecordToolCall(ctx, record) })
}

func (r *fanoutRecorder) record(call func(Recorder) error) error {
	var joined error
	for _, recorder := range r.recorders {
		joined = errors.Join(joined, call(recorder))
	}
	return joined
}
