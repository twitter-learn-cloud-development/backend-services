package observability

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TraceEventKind string

const (
	TraceEventRun      TraceEventKind = "run"
	TraceEventStep     TraceEventKind = "step"
	TraceEventLLMCall  TraceEventKind = "llm_call"
	TraceEventToolCall TraceEventKind = "tool_call"
	TraceEventControl  TraceEventKind = "control"

	DefaultTraceEventBatchSize = 100
	MaxTraceEventBatchSize     = 100
)

type TraceEvent struct {
	Cursor    string          `json:"cursor"`
	Kind      TraceEventKind  `json:"kind"`
	Run       *RunRecord      `json:"run,omitempty"`
	Step      *StepRecord     `json:"step,omitempty"`
	LLMCall   *LLMCallRecord  `json:"llm_call,omitempty"`
	ToolCall  *ToolCallRecord `json:"tool_call,omitempty"`
	Reset     bool            `json:"reset,omitempty"`
	Heartbeat bool            `json:"heartbeat,omitempty"`
	Terminal  bool            `json:"terminal,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type TraceEventBatch struct {
	Events []TraceEvent `json:"events"`
	Reset  bool         `json:"reset,omitempty"`
}

type EventReader interface {
	ReadTraceEvents(
		ctx context.Context,
		userID uint64,
		runID string,
		afterCursor string,
		limit int,
		block time.Duration,
	) (TraceEventBatch, error)
}

func NormalizeTraceEventCursor(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "0-0", nil
	}
	parts := strings.Split(value, "-")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("event cursor must use Redis stream ID format")
	}
	for _, part := range parts {
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return "", errors.New("event cursor must use Redis stream ID format")
		}
	}
	if len(value) > 64 {
		return "", errors.New("event cursor is too long")
	}
	return value, nil
}

func CompareTraceEventCursor(left, right string) (int, error) {
	left, err := NormalizeTraceEventCursor(left)
	if err != nil {
		return 0, fmt.Errorf("invalid left event cursor: %w", err)
	}
	right, err = NormalizeTraceEventCursor(right)
	if err != nil {
		return 0, fmt.Errorf("invalid right event cursor: %w", err)
	}
	leftParts := strings.Split(left, "-")
	rightParts := strings.Split(right, "-")
	for index := 0; index < 2; index++ {
		leftValue, _ := strconv.ParseUint(leftParts[index], 10, 64)
		rightValue, _ := strconv.ParseUint(rightParts[index], 10, 64)
		if leftValue < rightValue {
			return -1, nil
		}
		if leftValue > rightValue {
			return 1, nil
		}
	}
	return 0, nil
}

func ClampTraceEventBatchSize(limit int) int {
	if limit <= 0 {
		return DefaultTraceEventBatchSize
	}
	if limit > MaxTraceEventBatchSize {
		return MaxTraceEventBatchSize
	}
	return limit
}
