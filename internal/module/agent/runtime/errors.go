package runtime

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	ErrorInvalidRequest   ErrorCode = "invalid_request"
	ErrorInvalidAction    ErrorCode = "invalid_action"
	ErrorEmptyResponse    ErrorCode = "empty_response"
	ErrorModel            ErrorCode = "model_error"
	ErrorUnknownTool      ErrorCode = "unknown_tool"
	ErrorTool             ErrorCode = "tool_error"
	ErrorRAG              ErrorCode = "rag_error"
	ErrorUnsupported      ErrorCode = "unsupported_action"
	ErrorMaxSteps         ErrorCode = "max_steps_exceeded"
	ErrorBudgetExceeded   ErrorCode = "budget_exceeded"
	ErrorConcurrencyLimit ErrorCode = "concurrency_limit_exceeded"
	ErrorTimeout          ErrorCode = "timeout"
	ErrorCanceled         ErrorCode = "canceled"
	ErrorApprovalRequired ErrorCode = "approval_required"
)

var ErrApprovalRequired = errors.New("tool approval required")

type ToolSuspensionError struct {
	Continuation ToolContinuation
	Cause        error
}

func (e *ToolSuspensionError) Error() string {
	if e == nil {
		return "tool suspended"
	}
	if e.Continuation.Prompt != "" {
		return "tool suspended: " + e.Continuation.Prompt
	}
	return "tool suspended"
}

func (e *ToolSuspensionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ExecutionSuspended lets governance adapters distinguish an expected
// continuation boundary from a downstream failure without importing Runtime.
func (e *ToolSuspensionError) ExecutionSuspended() bool {
	return e != nil
}

func ToolContinuationFromError(err error) (ToolContinuation, bool) {
	var suspended *ToolSuspensionError
	if !errors.As(err, &suspended) || suspended == nil {
		return ToolContinuation{}, false
	}
	return cloneToolContinuation(suspended.Continuation), true
}

type RunError struct {
	Code       ErrorCode
	Step       int
	ActionID   string
	ApprovalID string
	Message    string
	Cause      error
}

func (e *RunError) Error() string {
	if e == nil {
		return ""
	}
	prefix := string(e.Code)
	if e.Step > 0 {
		prefix = fmt.Sprintf("%s at step %d", prefix, e.Step)
	}
	if e.ActionID != "" {
		prefix += fmt.Sprintf(" action %s", e.ActionID)
	}
	if e.Message != "" {
		return prefix + ": " + e.Message
	}
	if e.Cause != nil {
		return prefix + ": " + e.Cause.Error()
	}
	return prefix
}

func (e *RunError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func HasErrorCode(err error, code ErrorCode) bool {
	var runErr *RunError
	return errors.As(err, &runErr) && runErr.Code == code
}

func ApprovalIDFromError(err error) string {
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Code != ErrorApprovalRequired {
		return ""
	}
	return runErr.ApprovalID
}
