package service

import (
	"context"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/repository"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type agentRunAccountingStoreFake struct {
	userID      uint64
	parentRunID string
	limit       int
	runs        []*repository.WorkflowRunRecord
	total       int64
	err         error
}

func (f *agentRunAccountingStoreFake) ListDirectChildWorkflowRuns(
	_ context.Context,
	userID uint64,
	parentRunID string,
	limit int,
) ([]*repository.WorkflowRunRecord, int64, error) {
	f.userID = userID
	f.parentRunID = parentRunID
	f.limit = limit
	return f.runs, f.total, f.err
}

func TestGetAgentRunAccountingAggregatesParentAndDirectChildren(t *testing.T) {
	parentStore := &memoryAgentExecutionRunStore{run: &repository.AgentExecutionRun{
		ID: "parent-run", UserID: 73, Status: repository.AgentExecutionRunCompleted,
		StepCount: 2, InputTokens: 10, OutputTokens: 5, TotalTokens: 15,
		EstimatedCostMicros: 30, PricingVersion: "pricing-a",
		MaxSteps: 8, MaxTotalTokens: 500, MaxEstimatedCostMicros: 1_000,
		AccountingVersion: repository.ExecutionAccountingVersion,
	}}
	childStore := &agentRunAccountingStoreFake{
		total: 2,
		runs: []*repository.WorkflowRunRecord{
			{
				ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(), UserID: 73,
				ParentRunID: "parent-run", ParentActionID: "action-1", Status: WorkflowRunStatusSuccess,
				NodeExecutions: 3, InputTokens: 20, OutputTokens: 7, TotalTokens: 27,
				EstimatedCostMicros: 40, PricingVersion: "pricing-a",
				MaxSteps: 10, MaxTotalTokens: 600, MaxEstimatedCostMicros: 2_000,
				AccountingVersion: repository.ExecutionAccountingVersion,
				StartedAt:         time.Now().Add(-time.Minute), FinishedAt: time.Now(),
			},
			{
				ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(), UserID: 73,
				ParentRunID: "parent-run", ParentActionID: "action-2", Status: WorkflowRunStatusFailed,
				NodeExecutions: 1, InputTokens: 4, OutputTokens: 2, TotalTokens: 6,
				UsageEstimated: true, EstimatedCostMicros: 9, CostEstimated: true,
				PricingVersion: "pricing-b", AccountingVersion: repository.ExecutionAccountingVersion,
				StartedAt: time.Now().Add(-time.Minute), FinishedAt: time.Now(),
			},
		},
	}
	svc := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "model", "127.0.0.1:1", nil, nil, nil,
		WithAgentExecutionRunStore(parentStore),
		WithAgentRunAccountingStore(childStore),
		WithRecoverableAgentRuns(true),
	)
	defer svc.Close()

	view, err := svc.GetAgentRunAccounting(context.Background(), 73, "parent-run", 20)
	if err != nil {
		t.Fatalf("GetAgentRunAccounting() error = %v", err)
	}
	if childStore.userID != 73 || childStore.parentRunID != "parent-run" || childStore.limit != 20 {
		t.Fatalf("child query = user %d parent %q limit %d", childStore.userID, childStore.parentRunID, childStore.limit)
	}
	if !view.Complete || view.State != AgentRunAccountingStateComplete || view.Truncated {
		t.Fatalf("accounting completeness = %+v", view)
	}
	if view.ChildUsage.TotalTokens != 33 || view.TotalUsage.TotalTokens != 48 ||
		view.TotalUsage.EstimatedCostMicros != 79 || view.TotalUsage.PricingVersion != "mixed" ||
		!view.TotalUsage.Estimated || !view.TotalUsage.CostEstimated {
		t.Fatalf("aggregate usage = %+v", view.TotalUsage)
	}
	if len(view.Children) != 2 || view.Children[0].Budget.ConsumedSteps != 3 {
		t.Fatalf("child accounting = %+v", view.Children)
	}
}

func TestGetAgentRunAccountingMarksLegacyOrTruncatedDataPartial(t *testing.T) {
	parentStore := &memoryAgentExecutionRunStore{run: &repository.AgentExecutionRun{
		ID: "legacy-parent", UserID: 91, Status: repository.AgentExecutionRunCompleted,
	}}
	childStore := &agentRunAccountingStoreFake{
		total: 2,
		runs: []*repository.WorkflowRunRecord{{
			ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(), UserID: 91,
			ParentRunID: "legacy-parent", Status: WorkflowRunStatusSuccess,
			TotalTokens: 12, AccountingVersion: repository.ExecutionAccountingVersion,
		}},
	}
	svc := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "model", "127.0.0.1:1", nil, nil, nil,
		WithAgentExecutionRunStore(parentStore),
		WithAgentRunAccountingStore(childStore),
		WithRecoverableAgentRuns(true),
	)
	defer svc.Close()

	view, err := svc.GetAgentRunAccounting(context.Background(), 91, "legacy-parent", 0)
	if err != nil {
		t.Fatalf("GetAgentRunAccounting() error = %v", err)
	}
	if view.Complete || view.State != AgentRunAccountingStatePartial || !view.Truncated ||
		view.TotalUsage.TotalTokens != 12 || childStore.limit != defaultAgentRunAccountingChildLimit {
		t.Fatalf("partial accounting = %+v", view)
	}
}

func TestGetAgentRunAccountingRejectsCrossTenantParent(t *testing.T) {
	parentStore := &memoryAgentExecutionRunStore{run: &repository.AgentExecutionRun{
		ID: "owned-run", UserID: 10, Status: repository.AgentExecutionRunCompleted,
	}}
	childStore := &agentRunAccountingStoreFake{}
	svc := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "model", "127.0.0.1:1", nil, nil, nil,
		WithAgentExecutionRunStore(parentStore),
		WithAgentRunAccountingStore(childStore),
		WithRecoverableAgentRuns(true),
	)
	defer svc.Close()

	if _, err := svc.GetAgentRunAccounting(context.Background(), 11, "owned-run", 10); err == nil {
		t.Fatal("GetAgentRunAccounting() error = nil, want ownership failure")
	}
	if childStore.parentRunID != "" {
		t.Fatalf("child store called for unauthorized parent %q", childStore.parentRunID)
	}
}
