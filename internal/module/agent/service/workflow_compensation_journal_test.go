package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

func TestGetWorkflowCompensationJournalIsTenantScopedAndRedacted(t *testing.T) {
	repo := newCompensationWorkflowRepositoryFake(nil)
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(),
		WorkflowRevisionID: primitive.NewObjectID(), UserID: 81,
		Status: WorkflowRunStatusCompensationFailed,
	}
	repo.runs[run.ID] = run
	now := time.Now()
	repo.compensations[1] = &repository.WorkflowCompensationRecord{
		ID: primitive.NewObjectID(), RunID: run.ID, UserID: run.UserID,
		Sequence: 1, SourceNodeID: "reserve", StepID: "reserve$compensate", ToolName: "Release",
		InputJSON: `{"secret":"must-not-leak"}`, OutputJSON: `{"token":"must-not-leak"}`,
		IdempotencyKey: "must-not-leak", InputHash: "input-hash", PlanHash: "plan-hash",
		Status: repository.WorkflowCompensationStatusFailed, ErrorMessage: "release failed",
		CreatedAt: now, UpdatedAt: now,
	}
	svc := &AgentService{repo: repo}

	journal, err := svc.GetWorkflowCompensationJournal(context.Background(), 81, run.ID.Hex())
	require.NoError(t, err)
	require.Equal(t, run.ID, journal.Run.ID)
	require.Equal(t, 1, journal.NextSequence)
	require.True(t, journal.RetryAvailable)
	require.Len(t, journal.Entries, 1)
	require.True(t, journal.Entries[0].IsNext)
	require.Equal(t, "input-hash", journal.Entries[0].InputHash)
	require.Equal(t, "plan-hash", journal.Entries[0].PlanHash)

	_, err = svc.GetWorkflowCompensationJournal(context.Background(), 82, run.ID.Hex())
	require.Error(t, err)
}

func TestRetryWorkflowCompensationExecutesOnlyPersistedNextEntry(t *testing.T) {
	repo := newCompensationWorkflowRepositoryFake(nil)
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(),
		WorkflowRevisionID: primitive.NewObjectID(), UserID: 83,
		Status: WorkflowRunStatusCompensationFailed, ErrorMessage: "main workflow failed",
	}
	repo.runs[run.ID] = run
	repo.compensations[1] = &repository.WorkflowCompensationRecord{
		ID: primitive.NewObjectID(), RunID: run.ID, WorkflowID: run.WorkflowID,
		WorkflowRevisionID: run.WorkflowRevisionID, UserID: run.UserID,
		Sequence: 1, SourceNodeID: "reserve", StepID: "reserve$compensate", ToolName: "Release",
		InputJSON: `{}`, InputHash: "input-hash", PlanHash: "plan-hash",
		IdempotencyKey: run.ID.Hex() + ":reserve$compensate:Release",
		Status:         repository.WorkflowCompensationStatusFailed, ErrorMessage: "temporary failure",
	}
	registry := workflowTool.NewRegistry()
	calls := 0
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"Release", "release resource", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			calls++
			return map[string]interface{}{"released": true}, nil
		},
	)))
	svc := &AgentService{repo: repo, workflowToolExecutor: workflowTool.NewExecutor(registry)}

	result, err := svc.RetryWorkflowCompensation(context.Background(), 83, run.ID.Hex())
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCompensated, result.Run.Status)
	require.Equal(t, 1, calls)
	require.Equal(t, "main workflow failed", result.Run.ErrorMessage)

	records, err := repo.ListWorkflowCompensations(context.Background(), run.ID, 83)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowCompensationStatusSucceeded, records[0].Status)
}

func TestRetryWorkflowCompensationRejectsNonRecoverableRun(t *testing.T) {
	repo := newCompensationWorkflowRepositoryFake(nil)
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(),
		WorkflowRevisionID: primitive.NewObjectID(), UserID: 84,
		Status: WorkflowRunStatusSuccess,
	}
	repo.runs[run.ID] = run
	repo.compensations[1] = &repository.WorkflowCompensationRecord{
		ID: primitive.NewObjectID(), RunID: run.ID, UserID: run.UserID,
		Sequence: 1, Status: repository.WorkflowCompensationStatusFailed,
	}
	svc := &AgentService{repo: repo}

	_, err := svc.RetryWorkflowCompensation(context.Background(), 84, run.ID.Hex())
	require.ErrorContains(t, err, "cannot be retried")
}

func TestGetWorkflowCompensationJournalAllowsPlannedCrashWindowButNotActiveLease(t *testing.T) {
	repo := newCompensationWorkflowRepositoryFake(nil)
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(),
		WorkflowRevisionID: primitive.NewObjectID(), UserID: 85,
		Status: WorkflowRunStatusCompensating,
	}
	repo.runs[run.ID] = run
	record := &repository.WorkflowCompensationRecord{
		ID: primitive.NewObjectID(), RunID: run.ID, UserID: run.UserID,
		Sequence: 1, Status: repository.WorkflowCompensationStatusPlanned,
	}
	repo.compensations[1] = record
	svc := &AgentService{repo: repo}

	journal, err := svc.GetWorkflowCompensationJournal(context.Background(), 85, run.ID.Hex())
	require.NoError(t, err)
	require.True(t, journal.RetryAvailable)

	record.Status = repository.WorkflowCompensationStatusExecuting
	record.LeaseUntil = time.Now().Add(time.Minute)
	journal, err = svc.GetWorkflowCompensationJournal(context.Background(), 85, run.ID.Hex())
	require.NoError(t, err)
	require.False(t, journal.RetryAvailable)
}
