package observability

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

type InMemoryRecorder struct {
	mu        sync.RWMutex
	runs      map[string]RunRecord
	steps     map[string]StepRecord
	llmCalls  map[string]LLMCallRecord
	toolCalls map[string]ToolCallRecord
}

func NewInMemoryRecorder() *InMemoryRecorder {
	return &InMemoryRecorder{
		runs: make(map[string]RunRecord), steps: make(map[string]StepRecord),
		llmCalls: make(map[string]LLMCallRecord), toolCalls: make(map[string]ToolCallRecord),
	}
}

func (r *InMemoryRecorder) RecordRun(_ context.Context, record RunRecord) error {
	if err := validateIdentity(record.RecordID, record.RunID, record.UserID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[memoryRecordKey(record.UserID, record.RecordID)] = record
	return nil
}

func (r *InMemoryRecorder) RecordStep(_ context.Context, record StepRecord) error {
	if err := validateIdentity(record.RecordID, record.RunID, record.UserID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps[memoryRecordKey(record.UserID, record.RecordID)] = record
	return nil
}

func (r *InMemoryRecorder) RecordLLMCall(_ context.Context, record LLMCallRecord) error {
	if err := validateIdentity(record.RecordID, record.RunID, record.UserID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.llmCalls[memoryRecordKey(record.UserID, record.RecordID)] = record
	return nil
}

func (r *InMemoryRecorder) RecordToolCall(_ context.Context, record ToolCallRecord) error {
	if err := validateIdentity(record.RecordID, record.RunID, record.UserID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolCalls[memoryRecordKey(record.UserID, record.RecordID)] = record
	return nil
}

func (r *InMemoryRecorder) GetTraceBundle(_ context.Context, userID uint64, runID string) (*TraceBundle, error) {
	if userID == 0 || runID == "" {
		return nil, errors.New("trace query identity is incomplete")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	bundle := &TraceBundle{Steps: []StepRecord{}, LLMCalls: []LLMCallRecord{}, ToolCalls: []ToolCallRecord{}}
	for _, record := range r.runs {
		if record.UserID == userID && record.RunID == runID {
			copy := record
			bundle.Run = &copy
			break
		}
	}
	for _, record := range r.steps {
		if record.UserID == userID && record.RunID == runID {
			bundle.Steps = append(bundle.Steps, record)
		}
	}
	for _, record := range r.llmCalls {
		if record.UserID == userID && record.RunID == runID {
			bundle.LLMCalls = append(bundle.LLMCalls, record)
		}
	}
	for _, record := range r.toolCalls {
		if record.UserID == userID && record.RunID == runID {
			bundle.ToolCalls = append(bundle.ToolCalls, record)
		}
	}
	sort.Slice(bundle.Steps, func(i, j int) bool {
		return lessSequence(bundle.Steps[i].Sequence, bundle.Steps[i].RecordID, bundle.Steps[j].Sequence, bundle.Steps[j].RecordID)
	})
	sort.Slice(bundle.LLMCalls, func(i, j int) bool {
		return lessSequence(bundle.LLMCalls[i].Sequence, bundle.LLMCalls[i].RecordID, bundle.LLMCalls[j].Sequence, bundle.LLMCalls[j].RecordID)
	})
	sort.Slice(bundle.ToolCalls, func(i, j int) bool {
		return lessSequence(bundle.ToolCalls[i].Sequence, bundle.ToolCalls[i].RecordID, bundle.ToolCalls[j].Sequence, bundle.ToolCalls[j].RecordID)
	})
	return bundle, nil
}

func validateIdentity(recordID, runID string, userID uint64) error {
	if recordID == "" || runID == "" || userID == 0 {
		return errors.New("trace record identity is incomplete")
	}
	return nil
}

func memoryRecordKey(userID uint64, recordID string) string {
	return fmt.Sprintf("%d\x00%s", userID, recordID)
}

func lessSequence(leftSequence int, leftID string, rightSequence int, rightID string) bool {
	if leftSequence != rightSequence {
		return leftSequence < rightSequence
	}
	return leftID < rightID
}
