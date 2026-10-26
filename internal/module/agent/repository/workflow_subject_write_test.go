package repository

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestWorkflowStateEventOwnerRejectsMixedOrMissingIdentity(t *testing.T) {
	runID := primitive.NewObjectID()
	cases := []struct {
		name   string
		events []*WorkflowStateEvent
		want   uint64
		bad    bool
	}{
		{name: "one owner", events: []*WorkflowStateEvent{nil, {RunID: runID, UserID: 7}, {RunID: runID, UserID: 7}}, want: 7},
		{name: "mixed owners", events: []*WorkflowStateEvent{{RunID: runID, UserID: 7}, {RunID: runID, UserID: 8}}, bad: true},
		{name: "missing run", events: []*WorkflowStateEvent{{UserID: 7}}, bad: true},
		{name: "missing owner", events: []*WorkflowStateEvent{{RunID: runID}}, bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := workflowStateEventOwner(tc.events)
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("owner=%d err=%v, want owner=%d bad=%v", got, err, tc.want, tc.bad)
			}
		})
	}
}

func TestWorkflowCompensationOwnerRejectsMixedOrMissingIdentity(t *testing.T) {
	runID := primitive.NewObjectID()
	valid := func(userID uint64) *WorkflowCompensationRecord {
		return &WorkflowCompensationRecord{RunID: runID, UserID: userID, Sequence: 1, PlanHash: "plan"}
	}
	cases := []struct {
		name    string
		records []*WorkflowCompensationRecord
		want    uint64
		bad     bool
	}{
		{name: "one owner", records: []*WorkflowCompensationRecord{nil, valid(7), valid(7)}, want: 7},
		{name: "mixed owners", records: []*WorkflowCompensationRecord{valid(7), valid(8)}, bad: true},
		{name: "missing owner", records: []*WorkflowCompensationRecord{valid(0)}, bad: true},
		{name: "missing plan", records: []*WorkflowCompensationRecord{{RunID: runID, UserID: 7, Sequence: 1}}, bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := workflowCompensationOwner(tc.records)
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("owner=%d err=%v, want owner=%d bad=%v", got, err, tc.want, tc.bad)
			}
		})
	}
}

func TestWorkflowStateWritesRejectIncompleteIdentityBeforeMongo(t *testing.T) {
	repo := &MongoAgentRepository{}
	ctx := context.Background()
	if err := repo.AppendWorkflowStateEvents(ctx, []*WorkflowStateEvent{{UserID: 7}}); err == nil {
		t.Fatal("event without run was accepted")
	}
	if err := repo.SaveWorkflowStateSnapshot(ctx, &WorkflowStateSnapshot{UserID: 7}); err == nil {
		t.Fatal("snapshot without run was accepted")
	}
	if err := repo.AdvanceWorkflowRunStateVersion(ctx, primitive.NewObjectID(), 0, 1); err == nil {
		t.Fatal("cursor without owner was accepted")
	}
	if err := repo.SaveWorkflowCompensationPlan(ctx, []*WorkflowCompensationRecord{{UserID: 7}}); err == nil {
		t.Fatal("compensation without run was accepted")
	}
}

func TestWorkflowControlWritesRejectIncompleteIdentityBeforeMongo(t *testing.T) {
	repo := &MongoAgentRepository{}
	ctx := context.Background()
	runID := primitive.NewObjectID()
	if _, err := repo.RequestWorkflowRunCancellation(ctx, runID, 0, "stop"); err == nil {
		t.Fatal("cancellation without owner was accepted")
	}
	if _, err := repo.CommitWorkflowRunExecutionState(ctx, &WorkflowRunRecord{UserID: 7}); err == nil {
		t.Fatal("commit without run id was accepted")
	}
	if _, err := repo.ClaimWorkflowCompensation(ctx, runID, 0, 1, "attempt", time.Now(), primitive.NilObjectID, false); err == nil {
		t.Fatal("claim without owner was accepted")
	}
	if err := repo.CompleteWorkflowCompensation(ctx, primitive.NilObjectID, "attempt", ""); err == nil {
		t.Fatal("completion without compensation id was accepted")
	}
	if err := repo.SuspendWorkflowCompensation(ctx, primitive.NilObjectID, "attempt", primitive.NilObjectID); err == nil {
		t.Fatal("suspension without approval id was accepted")
	}
	if err := repo.FailWorkflowCompensation(ctx, primitive.NilObjectID, "attempt", "failed"); err == nil {
		t.Fatal("failure without compensation id was accepted")
	}
	if err := repo.RejectWorkflowCompensation(ctx, runID, 0, primitive.NewObjectID(), "rejected"); err == nil {
		t.Fatal("rejection without owner was accepted")
	}
}
