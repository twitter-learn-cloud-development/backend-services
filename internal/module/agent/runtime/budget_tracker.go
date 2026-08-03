package runtime

import (
	"context"
	"fmt"
	"sync"
)

type budgetTrackerContextKey struct{}

// BudgetSnapshot is a race-safe view of workflow-wide budget consumption.
type BudgetSnapshot struct {
	NodeExecutions int        `json:"node_executions"`
	Usage          TokenUsage `json:"usage"`
	Reserved       TokenUsage `json:"reserved"`
}

// BudgetTracker coordinates node, token and cost admission across concurrent
// workflow branches. Runtime owns the accounting semantics; the workflow
// engine only asks for admission before executing work.
type BudgetTracker struct {
	mu             sync.Mutex
	budget         Budget
	nodeExecutions int
	usage          TokenUsage
	reserved       TokenUsage
}

func NewBudgetTracker(budget Budget) (*BudgetTracker, error) {
	if budget.MaxSteps < 0 || budget.MaxInputTokens < 0 || budget.MaxOutputTokens < 0 ||
		budget.MaxTotalTokens < 0 || budget.MaxEstimatedCostMicros < 0 {
		return nil, fmt.Errorf("budget limits cannot be negative")
	}
	return &BudgetTracker{budget: budget}, nil
}

func NewBudgetTrackerFromSnapshot(budget Budget, snapshot BudgetSnapshot) (*BudgetTracker, error) {
	tracker, err := NewBudgetTracker(budget)
	if err != nil {
		return nil, err
	}
	if snapshot.NodeExecutions < 0 {
		return nil, fmt.Errorf("budget snapshot node executions cannot be negative")
	}
	if normalizedUsage(snapshot.Reserved).TotalTokens != 0 || snapshot.Reserved.EstimatedCostMicros != 0 {
		return nil, fmt.Errorf("budget snapshot cannot contain in-flight reservations")
	}
	tracker.nodeExecutions = snapshot.NodeExecutions
	tracker.usage = normalizedUsage(snapshot.Usage)
	return tracker, nil
}

func ContextWithBudgetTracker(ctx context.Context, tracker *BudgetTracker) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tracker == nil {
		return ctx
	}
	return context.WithValue(ctx, budgetTrackerContextKey{}, tracker)
}

func BudgetTrackerFromContext(ctx context.Context) (*BudgetTracker, bool) {
	if ctx == nil {
		return nil, false
	}
	tracker, ok := ctx.Value(budgetTrackerContextKey{}).(*BudgetTracker)
	return tracker, ok && tracker != nil
}

// AdmitStep counts actual node attempts, including retries. Skipped branches
// do not consume this budget because their handlers never run.
func (t *BudgetTracker) AdmitStep() error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.budget.MaxSteps > 0 && t.nodeExecutions >= t.budget.MaxSteps {
		return &RunError{
			Code:    ErrorBudgetExceeded,
			Step:    t.nodeExecutions + 1,
			Message: fmt.Sprintf("node execution budget exceeded: limit %d", t.budget.MaxSteps),
		}
	}
	t.nodeExecutions++
	return nil
}

// UsageReservation protects a shared workflow budget before an LLM request is
// sent. Exactly one of Commit or Release must be called.
type UsageReservation struct {
	tracker  *BudgetTracker
	estimate TokenUsage
	once     sync.Once
}

func ReserveBudgetUsage(ctx context.Context, estimate TokenUsage) (*UsageReservation, error) {
	tracker, ok := BudgetTrackerFromContext(ctx)
	if !ok {
		return &UsageReservation{}, nil
	}
	return tracker.ReserveUsage(estimate)
}

func RecordBudgetUsage(ctx context.Context, usage TokenUsage) error {
	tracker, ok := BudgetTrackerFromContext(ctx)
	if !ok {
		return nil
	}
	reservation, err := tracker.ReserveUsage(TokenUsage{})
	if err != nil {
		return err
	}
	return reservation.Commit(usage)
}

func (t *BudgetTracker) ReserveUsage(estimate TokenUsage) (*UsageReservation, error) {
	if t == nil {
		return &UsageReservation{}, nil
	}
	estimate = normalizedUsage(estimate)
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.admitUsageLocked(estimate, true); err != nil {
		return nil, err
	}
	t.reserved.Add(estimate)
	return &UsageReservation{tracker: t, estimate: estimate}, nil
}

func (r *UsageReservation) Commit(actual TokenUsage) error {
	if r == nil {
		return nil
	}
	var commitErr error
	r.once.Do(func() {
		if r.tracker == nil {
			return
		}
		actual = normalizedUsage(actual)
		r.tracker.mu.Lock()
		defer r.tracker.mu.Unlock()
		r.tracker.subtractReservedLocked(r.estimate)
		if err := r.tracker.admitUsageLocked(actual, false); err != nil {
			// Provider usage represents spend that already happened, so retain it
			// even when the completed call crossed the configured limit.
			r.tracker.usage.Add(actual)
			commitErr = err
			return
		}
		r.tracker.usage.Add(actual)
	})
	return commitErr
}

func (r *UsageReservation) Release() {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if r.tracker == nil {
			return
		}
		r.tracker.mu.Lock()
		defer r.tracker.mu.Unlock()
		r.tracker.subtractReservedLocked(r.estimate)
	})
}

func (t *BudgetTracker) Budget() Budget {
	if t == nil {
		return Budget{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.budget
}

func (t *BudgetTracker) Snapshot() BudgetSnapshot {
	if t == nil {
		return BudgetSnapshot{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return BudgetSnapshot{
		NodeExecutions: t.nodeExecutions,
		Usage:          t.usage,
		Reserved:       t.reserved,
	}
}

func (t *BudgetTracker) admitUsageLocked(incoming TokenUsage, includeReserved bool) error {
	input := incoming.InputTokens
	output := incoming.OutputTokens
	if t.budget.MaxInputTokens > 0 && input > t.budget.MaxInputTokens {
		return budgetExceededError(0, "input", input, t.budget.MaxInputTokens)
	}
	if t.budget.MaxOutputTokens > 0 && output > t.budget.MaxOutputTokens {
		return budgetExceededError(0, "output", output, t.budget.MaxOutputTokens)
	}
	total := t.usage.TotalTokens + incoming.TotalTokens
	cost := t.usage.EstimatedCostMicros + incoming.EstimatedCostMicros
	if includeReserved {
		total += t.reserved.TotalTokens
		cost += t.reserved.EstimatedCostMicros
	}
	if t.budget.MaxTotalTokens > 0 && total > t.budget.MaxTotalTokens {
		return budgetExceededError(0, "workflow total", total, t.budget.MaxTotalTokens)
	}
	if t.budget.MaxEstimatedCostMicros > 0 && cost > t.budget.MaxEstimatedCostMicros {
		return costBudgetExceededError(0, cost, t.budget.MaxEstimatedCostMicros)
	}
	return nil
}

func (t *BudgetTracker) subtractReservedLocked(usage TokenUsage) {
	t.reserved.InputTokens = maxZero(t.reserved.InputTokens - usage.InputTokens)
	t.reserved.OutputTokens = maxZero(t.reserved.OutputTokens - usage.OutputTokens)
	t.reserved.TotalTokens = maxZero(t.reserved.TotalTokens - usage.TotalTokens)
	t.reserved.EstimatedCostMicros -= usage.EstimatedCostMicros
	if t.reserved.EstimatedCostMicros < 0 {
		t.reserved.EstimatedCostMicros = 0
	}
}

func normalizedUsage(usage TokenUsage) TokenUsage {
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	if usage.OutputTokens < 0 {
		usage.OutputTokens = 0
	}
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if usage.EstimatedCostMicros < 0 {
		usage.EstimatedCostMicros = 0
	}
	return usage
}

func maxZero(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
