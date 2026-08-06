package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/engine"
)

func TestGetWorkflowRunReplayVerifiesAndSanitizesEvidence(t *testing.T) {
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 51, Name: "replay", DSLJSON: `{"nodes":[],"edges":[]}`,
	}
	repo := newCompensationWorkflowRepositoryFake(workflow)
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: workflow.ID,
		WorkflowRevisionID: repo.currentRevision.ID, WorkflowRevisionNumber: 1,
		UserID: workflow.UserID, Status: WorkflowRunStatusCompensated, StateVersion: 2,
		StartedAt: time.Now().Add(-time.Minute), FinishedAt: time.Now(),
	}
	repo.runs[run.ID] = run

	first := mustWorkflowReplayEvent(t, run, engine.StateEvent{
		Sequence: 1, NodeID: "start", Delta: map[string]interface{}{"user_input": "hello"},
		AppliedAt: time.Now().Add(-50 * time.Second).UnixMilli(),
	})
	second := mustWorkflowReplayEvent(t, run, engine.StateEvent{
		Sequence: 2, NodeID: "reserve", Delta: map[string]interface{}{"reservation_id": "r-1"},
		AppliedAt: time.Now().Add(-40 * time.Second).UnixMilli(),
	})
	repo.stateEvents[1] = first
	repo.stateEvents[2] = second

	blackboard := engine.NewBlackboard()
	blackboard.ApplyDelta("start", map[string]interface{}{"user_input": "hello"})
	snapshot, err := workflowStateSnapshotRecord(run, blackboard.Commit())
	require.NoError(t, err)
	repo.stateSnapshots[1] = snapshot

	repo.compensations[1] = &repository.WorkflowCompensationRecord{
		ID: primitive.NewObjectID(), RunID: run.ID, WorkflowID: workflow.ID,
		WorkflowRevisionID: repo.currentRevision.ID, UserID: workflow.UserID,
		Sequence: 1, SourceNodeID: "reserve", StepID: "reserve$compensate",
		ToolName: "Release", InputJSON: `{"api_key":"must-not-leak"}`,
		OutputJSON: `{"secret":"must-not-leak"}`, InputHash: "input-hash", PlanHash: "plan-hash",
		Status: repository.WorkflowCompensationStatusSucceeded, Attempt: 1,
		CreatedAt: time.Now().Add(-30 * time.Second), UpdatedAt: time.Now().Add(-20 * time.Second),
		FinishedAt: time.Now().Add(-20 * time.Second),
	}

	svc := &AgentService{repo: repo}
	replay, err := svc.GetWorkflowRunReplay(context.Background(), workflow.UserID, run.ID.Hex())
	require.NoError(t, err)
	require.True(t, replay.Integrity.Verified)
	require.Equal(t, int64(2), replay.Integrity.EventCount)
	require.Equal(t, int64(2), replay.Integrity.LastSequence)
	require.Equal(t, int64(1), replay.Integrity.SnapshotVersion)
	require.Len(t, replay.Events, 2)
	require.Equal(t, "reserve", replay.Events[1].NodeID)
	require.NotNil(t, replay.Revision)
	require.Equal(t, repo.currentRevision.ID, replay.Revision.ID)
	require.Len(t, replay.Compensations, 1)
	require.Equal(t, "input-hash", replay.Compensations[0].InputHash)

	payload, err := json.Marshal(replay)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "must-not-leak")

	_, err = svc.GetWorkflowRunReplay(context.Background(), workflow.UserID+1, run.ID.Hex())
	require.ErrorContains(t, err, "workflow run not found")
}

func TestGetWorkflowRunReplayRejectsTamperedEvent(t *testing.T) {
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 77, Name: "tampered replay", DSLJSON: `{"nodes":[],"edges":[]}`,
	}
	repo := newCompensationWorkflowRepositoryFake(workflow)
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: workflow.ID,
		WorkflowRevisionID: repo.currentRevision.ID, WorkflowRevisionNumber: 1,
		UserID: workflow.UserID, Status: WorkflowRunStatusFailed, StateVersion: 1,
	}
	repo.runs[run.ID] = run
	record := mustWorkflowReplayEvent(t, run, engine.StateEvent{
		Sequence: 1, NodeID: "start", Delta: map[string]interface{}{"user_input": "hello"},
		AppliedAt: time.Now().UnixMilli(),
	})
	record.EventHash = "tampered"
	repo.stateEvents[1] = record

	svc := &AgentService{repo: repo}
	_, err := svc.GetWorkflowRunReplay(context.Background(), workflow.UserID, run.ID.Hex())
	require.ErrorContains(t, err, "failed integrity validation")
}

func mustWorkflowReplayEvent(t *testing.T, run *repository.WorkflowRunRecord, event engine.StateEvent) *repository.WorkflowStateEvent {
	t.Helper()
	record, err := workflowStateEventRecord(run, event)
	require.NoError(t, err)
	return record
}
