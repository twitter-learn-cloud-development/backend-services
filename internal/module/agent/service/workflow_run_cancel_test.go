package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/dsl"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type cancellationWorkflowRepositoryFake struct {
	*approvalWorkflowRepositoryFake
	controlMu sync.Mutex
}

func newCancellationWorkflowRepositoryFake(workflow *repository.WorkflowDefinition) *cancellationWorkflowRepositoryFake {
	return &cancellationWorkflowRepositoryFake{
		approvalWorkflowRepositoryFake: newApprovalWorkflowRepositoryFake(workflow),
	}
}

func (r *cancellationWorkflowRepositoryFake) CreateWorkflowRun(_ context.Context, run *repository.WorkflowRunRecord) error {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	if run.ID.IsZero() {
		run.ID = primitive.NewObjectID()
	}
	if run.Revision <= 0 {
		run.Revision = 1
	}
	copy := *run
	r.runs[run.ID] = &copy
	return nil
}

func (r *cancellationWorkflowRepositoryFake) UpdateWorkflowRun(_ context.Context, run *repository.WorkflowRunRecord) error {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	copy := *run
	copy.Revision++
	*run = copy
	r.runs[run.ID] = &copy
	return nil
}

func (r *cancellationWorkflowRepositoryFake) AdvanceWorkflowRunStateVersion(
	_ context.Context,
	runID primitive.ObjectID,
	userID uint64,
	stateVersion int64,
) error {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	run := r.runs[runID]
	if run == nil || run.UserID != userID {
		return errors.New("workflow run not found")
	}
	if stateVersion > run.StateVersion {
		run.StateVersion = stateVersion
		run.Revision++
	}
	return nil
}

func (r *cancellationWorkflowRepositoryFake) GetWorkflowRun(_ context.Context, runID primitive.ObjectID, userID uint64) (*repository.WorkflowRunRecord, error) {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	run := r.runs[runID]
	if run == nil || run.UserID != userID {
		return nil, errors.New("workflow run not found")
	}
	copy := *run
	return &copy, nil
}

func (r *cancellationWorkflowRepositoryFake) RequestWorkflowRunCancellation(
	_ context.Context,
	runID primitive.ObjectID,
	userID uint64,
	reason string,
) (*repository.WorkflowRunRecord, error) {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	run := r.runs[runID]
	if run == nil || run.UserID != userID || run.Status != WorkflowRunStatusRunning {
		return nil, repository.ErrWorkflowRunCancellationUnavailable
	}
	run.Status = WorkflowRunStatusCanceling
	run.CancelReason = reason
	run.CancelRequestedAt = time.Now()
	run.Revision++
	copy := *run
	return &copy, nil
}

func (r *cancellationWorkflowRepositoryFake) IsWorkflowRunCancellationRequested(
	_ context.Context,
	runID primitive.ObjectID,
	userID uint64,
) (bool, error) {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	run := r.runs[runID]
	if run == nil || run.UserID != userID {
		return false, errors.New("workflow run not found")
	}
	return run.Status == WorkflowRunStatusCanceling, nil
}

func (r *cancellationWorkflowRepositoryFake) CommitWorkflowRunExecutionState(
	_ context.Context,
	run *repository.WorkflowRunRecord,
) (*repository.WorkflowRunRecord, error) {
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	current := r.runs[run.ID]
	if current == nil || current.UserID != run.UserID {
		return nil, errors.New("workflow run not found")
	}
	desired := *run
	if current.Status == WorkflowRunStatusCanceling {
		desired.Status = WorkflowRunStatusCanceled
		desired.CancelReason = current.CancelReason
		desired.CancelRequestedAt = current.CancelRequestedAt
		desired.CanceledAt = time.Now()
		desired.FinishedAt = desired.CanceledAt
		desired.ErrorMessage = "workflow canceled by user: " + current.CancelReason
	}
	desired.Revision = current.Revision + 1
	r.runs[run.ID] = &desired
	copy := desired
	return &copy, nil
}

func TestCancelWorkflowRunPropagatesAcrossExecutionContext(t *testing.T) {
	definition := dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "block", Type: "tool", Properties: json.RawMessage(`{"tool_name":"Block"}`)},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-block", Source: "start", Target: "block"},
			{ID: "block-end", Source: "block", Target: "end"},
		},
	}
	dslJSON, err := json.Marshal(definition)
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 111, Name: "cancel flow", DSLJSON: string(dslJSON),
	}
	repo := newCancellationWorkflowRepositoryFake(workflow)
	registry := workflowTool.NewRegistry()
	started := make(chan struct{})
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"Block", "wait for cancellation", `{"type":"object"}`,
		func(ctx context.Context, _ map[string]interface{}) (map[string]interface{}, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	)))
	svc := &AgentService{
		repo: repo, workflowToolExecutor: workflowTool.NewExecutor(registry),
		workflowCancelPoll: 5 * time.Millisecond,
	}
	type outcome struct {
		result *WorkflowExecutionResult
		err    error
	}
	resultCh := make(chan outcome, 1)
	go func() {
		result, runErr := svc.RunWorkflow(context.Background(), 111, workflow.ID.Hex(), `{}`)
		resultCh <- outcome{result: result, err: runErr}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("blocking node did not start")
	}
	var runID primitive.ObjectID
	require.Eventually(t, func() bool {
		repo.controlMu.Lock()
		defer repo.controlMu.Unlock()
		for id := range repo.runs {
			runID = id
			return true
		}
		return false
	}, time.Second, 5*time.Millisecond)

	canceling, err := svc.CancelWorkflowRun(context.Background(), 111, runID.Hex(), "operator requested stop")
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCanceling, canceling.Status)

	select {
	case outcome := <-resultCh:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.Equal(t, WorkflowRunStatusCanceled, outcome.result.Run.Status)
		require.Equal(t, "operator requested stop", outcome.result.Run.CancelReason)
		require.ErrorContains(t, errors.New(outcome.result.Run.ErrorMessage), "canceled")
	case <-time.After(2 * time.Second):
		t.Fatal("workflow did not stop after cancellation request")
	}
}

func TestCancelWorkflowRunRejectsCrossTenantAndLongReason(t *testing.T) {
	repo := newCancellationWorkflowRepositoryFake(nil)
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(),
		UserID: 112, Status: WorkflowRunStatusRunning, Revision: 1,
	}
	repo.runs[run.ID] = run
	svc := &AgentService{repo: repo}

	_, err := svc.CancelWorkflowRun(context.Background(), 113, run.ID.Hex(), "stop")
	require.Error(t, err)
	_, err = svc.CancelWorkflowRun(context.Background(), 112, run.ID.Hex(), string(make([]rune, 501)))
	require.ErrorContains(t, err, "exceeds 500")
}

func TestWorkflowStateCursorDoesNotOverwriteCancellationState(t *testing.T) {
	repo := newCancellationWorkflowRepositoryFake(nil)
	stored := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(),
		UserID: 114, Status: WorkflowRunStatusCanceling, StateVersion: 1, Revision: 2,
	}
	repo.runs[stored.ID] = stored
	staleExecutionView := *stored
	staleExecutionView.Status = WorkflowRunStatusRunning
	staleExecutionView.StateVersion = 3
	svc := &AgentService{repo: repo}

	require.NoError(t, svc.advanceWorkflowRunStateVersion(context.Background(), &staleExecutionView))

	persisted, err := repo.GetWorkflowRun(context.Background(), stored.ID, stored.UserID)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCanceling, persisted.Status)
	require.EqualValues(t, 3, persisted.StateVersion)
	require.EqualValues(t, 3, persisted.Revision)
}
