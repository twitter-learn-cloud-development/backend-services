package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
)

type workflowRunQueryRepositoryFake struct {
	repository.AgentRepository
	userID     uint64
	workflowID primitive.ObjectID
	status     string
	page       int
	pageSize   int
	runs       []*repository.WorkflowRunRecord
	total      int64
	calls      int
}

func (r *workflowRunQueryRepositoryFake) ListWorkflowRuns(
	_ context.Context,
	userID uint64,
	workflowID primitive.ObjectID,
	status string,
	page int,
	pageSize int,
) ([]*repository.WorkflowRunRecord, int64, error) {
	r.calls++
	r.userID = userID
	r.workflowID = workflowID
	r.status = status
	r.page = page
	r.pageSize = pageSize
	return r.runs, r.total, nil
}

func TestListWorkflowRunsValidatesAndScopesQuery(t *testing.T) {
	workflowID := primitive.NewObjectID()
	repo := &workflowRunQueryRepositoryFake{
		runs:  []*repository.WorkflowRunRecord{{ID: primitive.NewObjectID(), UserID: 101}},
		total: 1,
	}
	svc := &AgentService{repo: repo}

	runs, total, err := svc.ListWorkflowRuns(
		context.Background(), 101, workflowID.Hex(), WorkflowRunStatusFailed, 0, 1000,
	)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, int64(1), total)
	require.Equal(t, uint64(101), repo.userID)
	require.Equal(t, workflowID, repo.workflowID)
	require.Equal(t, WorkflowRunStatusFailed, repo.status)
	require.Equal(t, 1, repo.page)
	require.Equal(t, 20, repo.pageSize)
}

func TestListWorkflowRunsRejectsInvalidFiltersBeforeRepositoryCall(t *testing.T) {
	repo := &workflowRunQueryRepositoryFake{}
	svc := &AgentService{repo: repo}

	_, _, err := svc.ListWorkflowRuns(context.Background(), 102, "bad-id", "", 1, 20)
	require.ErrorContains(t, err, "invalid workflow_id")
	_, _, err = svc.ListWorkflowRuns(context.Background(), 102, "", "unknown", 1, 20)
	require.ErrorContains(t, err, "invalid workflow run status")
	require.Zero(t, repo.calls)
}
