package service

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

func (r *compensationWorkflowRepositoryFake) ListExpiredWorkflowCompensationCandidates(
	_ context.Context,
	now time.Time,
	limit int,
) ([]repository.WorkflowCompensationRecoveryCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	records := make([]*repository.WorkflowCompensationRecord, 0, len(r.compensations))
	for _, record := range r.compensations {
		copy := *record
		records = append(records, &copy)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Sequence < records[j].Sequence })
	for _, record := range records {
		if record.Status == repository.WorkflowCompensationStatusSucceeded {
			continue
		}
		if record.Status != repository.WorkflowCompensationStatusExecuting || record.LeaseUntil.After(now) {
			return nil, nil
		}
		return []repository.WorkflowCompensationRecoveryCandidate{{
			RunID: record.RunID, UserID: record.UserID, Sequence: record.Sequence,
		}}, nil
	}
	return nil, nil
}

func TestWorkflowCompensationReconcilerRecoversExpiredSafeTool(t *testing.T) {
	repo, run := expiredCompensationRecoveryFixture(t, 81, "ReleaseRead")
	registry := workflowTool.NewRegistry()
	var calls atomic.Int32
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"ReleaseRead", "safe recovery", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			calls.Add(1)
			return map[string]interface{}{"released": true}, nil
		},
	)))
	service := &AgentService{repo: repo, workflowToolExecutor: workflowTool.NewExecutor(registry)}
	reconciler := NewWorkflowCompensationReconciler(service, repo, time.Minute, 10, nil)

	result, err := reconciler.Reconcile(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Scanned)
	require.EqualValues(t, 1, result.Recovered)
	require.EqualValues(t, 0, result.DeferredManualRetry)
	require.EqualValues(t, 1, calls.Load())

	storedRun, err := repo.GetWorkflowRun(context.Background(), run.ID, run.UserID)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCompensated, storedRun.Status)
	records, err := repo.ListWorkflowCompensations(context.Background(), run.ID, run.UserID)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowCompensationStatusSucceeded, records[0].Status)
	require.Equal(t, 2, records[0].Attempt)
}

func TestWorkflowCompensationReconcilerDefersApprovalToolToExplicitRetry(t *testing.T) {
	repo, run := expiredCompensationRecoveryFixture(t, 82, "ReleaseWrite")
	registry := workflowTool.NewRegistry()
	var calls atomic.Int32
	require.NoError(t, registry.Register(workflowTool.NewDelegatedToolWithSpec(workflowTool.ToolSpec{
		Name: "ReleaseWrite", InputSchema: json.RawMessage(`{"type":"object"}`),
		Category: workflowTool.CategoryWrite, Permission: workflowTool.PermissionAuthenticated,
		Approval: workflowTool.ApprovalRequired, Idempotency: workflowTool.IdempotencyPolicy{Required: true},
	}, func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		calls.Add(1)
		return map[string]interface{}{"released": true}, nil
	})))
	service := &AgentService{repo: repo, workflowToolExecutor: workflowTool.NewExecutor(registry)}
	reconciler := NewWorkflowCompensationReconciler(service, repo, time.Minute, 10, nil)

	result, err := reconciler.Reconcile(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, result.DeferredManualRetry)
	require.EqualValues(t, 0, result.Recovered)
	require.Zero(t, calls.Load(), "background recovery must not execute approval-protected tools")

	storedRun, err := repo.GetWorkflowRun(context.Background(), run.ID, run.UserID)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCompensationFailed, storedRun.Status)
	records, err := repo.ListWorkflowCompensations(context.Background(), run.ID, run.UserID)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowCompensationStatusFailed, records[0].Status)
	require.Contains(t, records[0].ErrorMessage, "requires approval")
}

func TestConcurrentWorkflowCompensationReconcilersExecuteExpiredLeaseOnce(t *testing.T) {
	repo, run := expiredCompensationRecoveryFixture(t, 83, "ReleaseOnce")
	registry := workflowTool.NewRegistry()
	var calls atomic.Int32
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"ReleaseOnce", "safe recovery", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			calls.Add(1)
			time.Sleep(20 * time.Millisecond)
			return map[string]interface{}{"released": true}, nil
		},
	)))
	service := &AgentService{repo: repo, workflowToolExecutor: workflowTool.NewExecutor(registry)}
	first := NewWorkflowCompensationReconciler(service, repo, time.Minute, 10, nil)
	second := NewWorkflowCompensationReconciler(service, repo, time.Minute, 10, nil)

	var wait sync.WaitGroup
	wait.Add(2)
	errorsSeen := make(chan error, 2)
	for _, reconciler := range []*WorkflowCompensationReconciler{first, second} {
		go func(current *WorkflowCompensationReconciler) {
			defer wait.Done()
			_, err := current.Reconcile(context.Background())
			errorsSeen <- err
		}(reconciler)
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, calls.Load())
	storedRun, err := repo.GetWorkflowRun(context.Background(), run.ID, run.UserID)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCompensated, storedRun.Status)
}

func expiredCompensationRecoveryFixture(
	t *testing.T,
	userID uint64,
	toolName string,
) (*compensationWorkflowRepositoryFake, *repository.WorkflowRunRecord) {
	t.Helper()
	repo := newCompensationWorkflowRepositoryFake(nil)
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(), WorkflowRevisionID: primitive.NewObjectID(),
		UserID: userID, Status: WorkflowRunStatusCompensating, ErrorMessage: "main workflow failed",
	}
	require.NoError(t, repo.CreateWorkflowRun(context.Background(), run))
	repo.compensations[1] = &repository.WorkflowCompensationRecord{
		ID: primitive.NewObjectID(), RunID: run.ID, WorkflowID: run.WorkflowID,
		WorkflowRevisionID: run.WorkflowRevisionID, UserID: userID, Sequence: 1,
		SourceNodeID: "write", StepID: "write$compensate", ToolName: toolName,
		InputJSON: `{}`, InputHash: "input-hash", PlanHash: "plan-hash",
		IdempotencyKey: run.ID.Hex() + ":write$compensate:" + toolName,
		Status:         repository.WorkflowCompensationStatusExecuting, Attempt: 1,
		AttemptID: "expired-attempt", LeaseUntil: time.Now().Add(-time.Minute),
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Minute),
	}
	return repo, run
}
