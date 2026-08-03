package runtime

import (
	"context"
	"testing"
)

func TestBudgetTrackerRejectsRunawayNodeAttempt(t *testing.T) {
	tracker, err := NewBudgetTracker(Budget{MaxSteps: 2})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}
	if err := tracker.AdmitStep(); err != nil {
		t.Fatalf("first AdmitStep() error = %v", err)
	}
	if err := tracker.AdmitStep(); err != nil {
		t.Fatalf("second AdmitStep() error = %v", err)
	}
	if err := tracker.AdmitStep(); !HasErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("third AdmitStep() error = %v, want budget_exceeded", err)
	}
}

func TestBudgetTrackerIncludesInFlightReservationsInAdmission(t *testing.T) {
	tracker, err := NewBudgetTracker(Budget{MaxTotalTokens: 100})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}
	ctx := ContextWithBudgetTracker(context.Background(), tracker)
	first, err := ReserveBudgetUsage(ctx, TokenUsage{TotalTokens: 60})
	if err != nil {
		t.Fatalf("first ReserveBudgetUsage() error = %v", err)
	}
	defer first.Release()
	if _, err := ReserveBudgetUsage(ctx, TokenUsage{TotalTokens: 60}); !HasErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("second ReserveBudgetUsage() error = %v, want budget_exceeded", err)
	}
}

func TestBudgetTrackerRetainsUsageThatCrossesLimit(t *testing.T) {
	tracker, err := NewBudgetTracker(Budget{MaxTotalTokens: 100, MaxEstimatedCostMicros: 50})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}
	reservation, err := tracker.ReserveUsage(TokenUsage{TotalTokens: 80, EstimatedCostMicros: 40})
	if err != nil {
		t.Fatalf("ReserveUsage() error = %v", err)
	}
	err = reservation.Commit(TokenUsage{TotalTokens: 120, EstimatedCostMicros: 60})
	if !HasErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("Commit() error = %v, want budget_exceeded", err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Usage.TotalTokens != 120 || snapshot.Usage.EstimatedCostMicros != 60 {
		t.Fatalf("snapshot usage = %+v", snapshot.Usage)
	}
	if snapshot.Reserved.TotalTokens != 0 {
		t.Fatalf("reserved usage = %+v", snapshot.Reserved)
	}
}
