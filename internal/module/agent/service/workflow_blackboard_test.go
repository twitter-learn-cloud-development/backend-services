package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/engine"
)

type workflowBlackboardRepositoryFake struct {
	repository.AgentRepository
	run      *repository.WorkflowRunRecord
	snapshot *repository.WorkflowStateSnapshot
	events   []*repository.WorkflowStateEvent
}

func (r *workflowBlackboardRepositoryFake) GetWorkflowRun(
	_ context.Context,
	runID primitive.ObjectID,
	userID uint64,
) (*repository.WorkflowRunRecord, error) {
	if r.run == nil || r.run.ID != runID || r.run.UserID != userID {
		return nil, errors.New("workflow run not found")
	}
	cloned := *r.run
	return &cloned, nil
}

func (r *workflowBlackboardRepositoryFake) GetLatestWorkflowStateSnapshot(
	_ context.Context,
	runID primitive.ObjectID,
	userID uint64,
	atOrBeforeVersion int64,
) (*repository.WorkflowStateSnapshot, error) {
	if r.snapshot == nil || r.snapshot.RunID != runID || r.snapshot.UserID != userID || r.snapshot.StateVersion > atOrBeforeVersion {
		return nil, nil
	}
	cloned := *r.snapshot
	return &cloned, nil
}

func (r *workflowBlackboardRepositoryFake) SaveWorkflowStateSnapshot(context.Context, *repository.WorkflowStateSnapshot) error {
	return nil
}

func (r *workflowBlackboardRepositoryFake) ListWorkflowStateEventsRange(
	_ context.Context,
	runID primitive.ObjectID,
	userID uint64,
	afterSequence int64,
	atOrBeforeSequence int64,
	limit int64,
) ([]*repository.WorkflowStateEvent, error) {
	result := make([]*repository.WorkflowStateEvent, 0)
	for _, event := range r.events {
		if event == nil || event.RunID != runID || event.UserID != userID || event.Sequence <= afterSequence || event.Sequence > atOrBeforeSequence {
			continue
		}
		cloned := *event
		result = append(result, &cloned)
		if int64(len(result)) >= limit {
			break
		}
	}
	return result, nil
}

func TestSearchWorkflowBlackboardMaterializesRedactsAndPaginatesStableVersion(t *testing.T) {
	run := &repository.WorkflowRunRecord{ID: primitive.NewObjectID(), UserID: 71, StateVersion: 3}
	blackboard := engine.NewBlackboard()
	blackboard.ApplyDelta("start", map[string]interface{}{"user_input": "云原生平台"})
	snapshot, err := workflowStateSnapshotRecord(run, blackboard.Commit())
	require.NoError(t, err)

	second := blackboard.ApplyDelta("llm", map[string]interface{}{
		"result": map[string]interface{}{"summary": "云原生分析", "api_key": "must-not-leak"},
	})
	third := blackboard.ApplyDelta("tool", map[string]interface{}{
		"items": []interface{}{"first", "second"}, "token": "must-not-leak",
	})
	secondRecord, err := workflowStateEventRecord(run, second)
	require.NoError(t, err)
	thirdRecord, err := workflowStateEventRecord(run, third)
	require.NoError(t, err)
	repo := &workflowBlackboardRepositoryFake{
		run: run, snapshot: snapshot, events: []*repository.WorkflowStateEvent{secondRecord, thirdRecord},
	}
	svc := &AgentService{repo: repo}

	firstPage, err := svc.SearchWorkflowBlackboard(context.Background(), run.UserID, run.ID.Hex(), WorkflowBlackboardSearchRequest{PageSize: 1})
	require.NoError(t, err)
	require.True(t, firstPage.Verified)
	require.Equal(t, int64(3), firstPage.StateVersion)
	require.Equal(t, int64(1), firstPage.BaseSnapshotVersion)
	require.Equal(t, int64(4), firstPage.MatchedTotal)
	require.Len(t, firstPage.Entries, 1)
	require.Equal(t, "llm.result", firstPage.Entries[0].Path)
	require.Contains(t, firstPage.Entries[0].ValueJSON, "[REDACTED]")
	require.NotContains(t, firstPage.Entries[0].ValueJSON, "must-not-leak")
	require.True(t, firstPage.HasMore)
	require.NotEmpty(t, firstPage.NextCursor)

	// A cursor pins the original state version even if the run advances before
	// the next page is requested.
	run.StateVersion = 4
	fourth := blackboard.ApplyDelta("writer", map[string]interface{}{"draft": "newer value"})
	fourthRecord, err := workflowStateEventRecord(run, fourth)
	require.NoError(t, err)
	repo.events = append(repo.events, fourthRecord)
	secondPage, err := svc.SearchWorkflowBlackboard(context.Background(), run.UserID, run.ID.Hex(), WorkflowBlackboardSearchRequest{
		AfterCursor: firstPage.NextCursor, PageSize: 1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), secondPage.StateVersion)
	require.Equal(t, "start.user_input", secondPage.Entries[0].Path)

	matched, err := svc.SearchWorkflowBlackboard(context.Background(), run.UserID, run.ID.Hex(), WorkflowBlackboardSearchRequest{
		StateVersion: 3, Query: "云原生", PathPrefix: "start.", PageSize: 25,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), matched.MatchedTotal)
	require.Equal(t, "\"云原生平台\"", matched.Entries[0].ValueJSON)
	secretSearch, err := svc.SearchWorkflowBlackboard(context.Background(), run.UserID, run.ID.Hex(), WorkflowBlackboardSearchRequest{
		StateVersion: 3, Query: "must-not-leak", PageSize: 25,
	})
	require.NoError(t, err)
	require.Zero(t, secretSearch.MatchedTotal)
}

func TestSearchWorkflowBlackboardRejectsCrossTenantAndTamperedSnapshot(t *testing.T) {
	run := &repository.WorkflowRunRecord{ID: primitive.NewObjectID(), UserID: 91, StateVersion: 1}
	blackboard := engine.NewBlackboard()
	blackboard.ApplyDelta("start", map[string]interface{}{"value": "trusted"})
	snapshot, err := workflowStateSnapshotRecord(run, blackboard.Commit())
	require.NoError(t, err)
	repo := &workflowBlackboardRepositoryFake{run: run, snapshot: snapshot}
	svc := &AgentService{repo: repo}

	_, err = svc.SearchWorkflowBlackboard(context.Background(), run.UserID+1, run.ID.Hex(), WorkflowBlackboardSearchRequest{})
	require.ErrorContains(t, err, "not found")

	repo.snapshot.SnapshotJSON = `{"start":{"value":"tampered"}}`
	_, err = svc.SearchWorkflowBlackboard(context.Background(), run.UserID, run.ID.Hex(), WorkflowBlackboardSearchRequest{})
	require.ErrorContains(t, err, "integrity validation")
}

func TestSearchWorkflowBlackboardRejectsCursorFilterMismatch(t *testing.T) {
	run := &repository.WorkflowRunRecord{ID: primitive.NewObjectID(), UserID: 81, StateVersion: 1}
	blackboard := engine.NewBlackboard()
	blackboard.ApplyDelta("start", map[string]interface{}{"one": 1, "two": 2})
	snapshot, err := workflowStateSnapshotRecord(run, blackboard.Commit())
	require.NoError(t, err)
	svc := &AgentService{repo: &workflowBlackboardRepositoryFake{run: run, snapshot: snapshot}}

	page, err := svc.SearchWorkflowBlackboard(context.Background(), run.UserID, run.ID.Hex(), WorkflowBlackboardSearchRequest{PageSize: 1})
	require.NoError(t, err)
	require.NotEmpty(t, page.NextCursor)
	_, err = svc.SearchWorkflowBlackboard(context.Background(), run.UserID, run.ID.Hex(), WorkflowBlackboardSearchRequest{
		AfterCursor: page.NextCursor, Query: "different", PageSize: 1,
	})
	require.ErrorIs(t, err, ErrWorkflowBlackboardInvalidQuery)
}

func TestSearchWorkflowBlackboardOmitsOversizedValuePreview(t *testing.T) {
	run := &repository.WorkflowRunRecord{ID: primitive.NewObjectID(), UserID: 82, StateVersion: 1}
	blackboard := engine.NewBlackboard()
	blackboard.ApplyDelta("tool", map[string]interface{}{"payload": string(make([]byte, maxWorkflowBlackboardValueBytes+1))})
	snapshot, err := workflowStateSnapshotRecord(run, blackboard.Commit())
	require.NoError(t, err)
	svc := &AgentService{repo: &workflowBlackboardRepositoryFake{run: run, snapshot: snapshot}}

	result, err := svc.SearchWorkflowBlackboard(context.Background(), run.UserID, run.ID.Hex(), WorkflowBlackboardSearchRequest{})
	require.NoError(t, err)
	require.Len(t, result.Entries, 1)
	require.True(t, result.Entries[0].Truncated)
	require.Empty(t, result.Entries[0].ValueJSON)
	require.Greater(t, result.Entries[0].ValueLength, int64(maxWorkflowBlackboardValueBytes))
	require.NotEmpty(t, result.Entries[0].ValueHash)
}
