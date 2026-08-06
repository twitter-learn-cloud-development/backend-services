package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/repository"
)

type traceOwnershipRepositoryFake struct {
	repository.AgentRepository
	run *repository.WorkflowRunRecord
}

func (r *traceOwnershipRepositoryFake) GetWorkflowRun(_ context.Context, runID primitive.ObjectID, userID uint64) (*repository.WorkflowRunRecord, error) {
	if r.run == nil || r.run.ID != runID || r.run.UserID != userID {
		return nil, errors.New("workflow run not found")
	}
	return r.run, nil
}

func TestGetWorkflowRunTraceEnforcesRunOwnershipBeforeReadingTrace(t *testing.T) {
	run := &repository.WorkflowRunRecord{ID: primitive.NewObjectID(), UserID: 41}
	traces := agentObservability.NewInMemoryRecorder()
	require.NoError(t, traces.RecordRun(context.Background(), agentObservability.RunRecord{
		RecordID: run.ID.Hex(), RunID: run.ID.Hex(), UserID: 41, Status: "success",
	}))
	svc := &AgentService{repo: &traceOwnershipRepositoryFake{run: run}, traceReader: traces}

	bundle, err := svc.GetWorkflowRunTrace(context.Background(), 41, run.ID.Hex())
	require.NoError(t, err)
	require.Equal(t, "success", bundle.Run.Status)

	_, err = svc.GetWorkflowRunTrace(context.Background(), 42, run.ID.Hex())
	require.ErrorContains(t, err, "workflow run not found")
}

func TestGetWorkflowRunTraceRejectsInvalidRunIDBeforeTraceRead(t *testing.T) {
	svc := &AgentService{
		repo: &traceOwnershipRepositoryFake{}, traceReader: agentObservability.NewInMemoryRecorder(),
	}
	_, err := svc.GetWorkflowRunTrace(context.Background(), 41, "invalid")
	require.ErrorContains(t, err, "invalid run_id")
}
