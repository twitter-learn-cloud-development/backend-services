package runtime

import (
	"bytes"
	"context"
	"reflect"
	"time"
)

type VerifiedRepairReason string

const (
	VerifiedRepairExecutionFailed VerifiedRepairReason = "execution_failed"
	VerifiedRepairEvidenceMissing VerifiedRepairReason = "evidence_missing"
)

// VerifiedRepairSignal is deliberately bounded. Raw provider errors, tool
// output and rejected model proposals must never be fed back to a planner.
type VerifiedRepairSignal struct {
	Reason              VerifiedRepairReason
	ErrorCode           ErrorCode
	MissingCriterionIDs []string
}

type VerifiedRepairRequest struct {
	Task         TaskSpec
	Base         RunRequest
	Previous     RunResult
	Verification VerificationResult
	Evidence     EvidenceLedger
	Attempt      int
	Signal       VerifiedRepairSignal
}

// VerifiedRepairBuilder optionally replaces the legacy verifier-only repair
// prompt. VerifiedRunner still validates identity, budget and tool authority
// before executing the returned request.
type VerifiedRepairBuilder interface {
	BuildRepair(context.Context, VerifiedRepairRequest) (RunRequest, error)
}

func validateVerifiedRepairRequest(base RunRequest, previous RunResult, candidate RunRequest) error {
	if !sameRepairIdentity(base.Context, candidate.Context) || candidate.Model != base.Model {
		return &RunError{Code: ErrorInvalidRequest, Message: "repair request changed run identity or model"}
	}
	if !candidate.InitialToolChoice.Valid() {
		return &RunError{Code: ErrorInvalidRequest, Message: "repair request has invalid tool choice"}
	}
	if err := validateRepairTools(base.Tools, candidate.Tools); err != nil {
		return err
	}
	remaining, err := remainingGoalBudget(base.Context.Budget, previous)
	if err != nil {
		return err
	}
	if !budgetWithin(candidate.Context.Budget, remaining) {
		return &RunError{Code: ErrorInvalidRequest, Message: "repair request expanded the remaining run budget"}
	}
	if !messagePrefix(previous.Messages, candidate.Messages) {
		return &RunError{Code: ErrorInvalidRequest, Message: "repair request discarded or rewrote execution history"}
	}
	return nil
}

func sameRepairIdentity(base, candidate RunContext) bool {
	base.Budget = Budget{}
	candidate.Budget = Budget{}
	// A recovery plan has its own digest. The cumulative run remains bound to
	// the root plan while recovery plans are returned as separate evidence.
	base.StrategyPlanDigest = ""
	candidate.StrategyPlanDigest = ""
	return reflect.DeepEqual(base, candidate)
}

func validateRepairTools(authorized, candidate []ToolDefinition) error {
	catalog, err := buildToolCatalog(authorized)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(candidate))
	for _, tool := range candidate {
		original, ok := catalog[tool.Name]
		if !ok {
			return &RunError{Code: ErrorUnknownTool, Message: "repair request introduced an unauthorized tool"}
		}
		if _, duplicate := seen[tool.Name]; duplicate {
			return &RunError{Code: ErrorInvalidRequest, Message: "repair request contains duplicate tools"}
		}
		seen[tool.Name] = struct{}{}
		if tool.Category != original.Category || tool.ApprovalRequired() != original.ApprovalRequired() ||
			!bytes.Equal(tool.InputSchema, original.InputSchema) {
			return &RunError{Code: ErrorInvalidRequest, Message: "repair request changed tool authority"}
		}
	}
	return nil
}

func remainingGoalBudget(base Budget, previous RunResult) (Budget, error) {
	remaining := base
	maxSteps := base.MaxSteps
	if maxSteps == 0 {
		maxSteps = DefaultMaxSteps
	}
	remaining.MaxSteps = maxSteps - len(previous.Steps)
	if remaining.MaxSteps <= 0 {
		return Budget{}, &RunError{Code: ErrorBudgetExceeded, Message: "repair step budget is exhausted"}
	}
	if base.MaxTotalTokens > 0 {
		remaining.MaxTotalTokens = base.MaxTotalTokens - previous.Usage.TotalTokens
		if remaining.MaxTotalTokens <= 0 {
			return Budget{}, &RunError{Code: ErrorBudgetExceeded, Message: "repair token budget is exhausted"}
		}
	}
	if base.MaxEstimatedCostMicros > 0 {
		remaining.MaxEstimatedCostMicros = base.MaxEstimatedCostMicros - previous.Usage.EstimatedCostMicros
		if remaining.MaxEstimatedCostMicros <= 0 {
			return Budget{}, &RunError{Code: ErrorBudgetExceeded, Message: "repair cost budget is exhausted"}
		}
	}
	return remaining, nil
}

func budgetWithin(candidate, allowed Budget) bool {
	return limitWithin(candidate.MaxSteps, allowed.MaxSteps) &&
		limitWithin(candidate.MaxInputTokens, allowed.MaxInputTokens) &&
		limitWithin(candidate.MaxOutputTokens, allowed.MaxOutputTokens) &&
		limitWithin(candidate.MaxTotalTokens, allowed.MaxTotalTokens) &&
		costLimitWithin(candidate.MaxEstimatedCostMicros, allowed.MaxEstimatedCostMicros) &&
		durationWithin(candidate.Timeout, allowed.Timeout) && deadlineWithin(candidate.Deadline, allowed.Deadline)
}

func limitWithin(candidate, allowed int) bool {
	if allowed == 0 {
		return true
	}
	return candidate > 0 && candidate <= allowed
}

func costLimitWithin(candidate, allowed int64) bool {
	if allowed == 0 {
		return true
	}
	return candidate > 0 && candidate <= allowed
}

func durationWithin(candidate, allowed time.Duration) bool {
	if allowed == 0 {
		return true
	}
	return candidate > 0 && candidate <= allowed
}

func deadlineWithin(candidate, allowed time.Time) bool {
	if allowed.IsZero() {
		return true
	}
	return !candidate.IsZero() && !candidate.After(allowed)
}

func messagePrefix(prefix, messages []Message) bool {
	if len(messages) < len(prefix) {
		return false
	}
	normalizedPrefix := cloneMessages(prefix)
	normalizedMessages := cloneMessages(messages[:len(prefix)])
	return reflect.DeepEqual(normalizedPrefix, normalizedMessages)
}
