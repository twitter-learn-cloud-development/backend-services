package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/engine"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type compensationWorkflowRepositoryFake struct {
	*approvalWorkflowRepositoryFake
	compensations map[int]*repository.WorkflowCompensationRecord
}

func newCompensationWorkflowRepositoryFake(workflow *repository.WorkflowDefinition) *compensationWorkflowRepositoryFake {
	return &compensationWorkflowRepositoryFake{
		approvalWorkflowRepositoryFake: newApprovalWorkflowRepositoryFake(workflow),
		compensations:                  make(map[int]*repository.WorkflowCompensationRecord),
	}
}

func (r *compensationWorkflowRepositoryFake) SaveWorkflowCompensationPlan(_ context.Context, records []*repository.WorkflowCompensationRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range records {
		if existing := r.compensations[record.Sequence]; existing != nil {
			if existing.RunID != record.RunID || existing.UserID != record.UserID || existing.PlanHash != record.PlanHash {
				return repository.ErrWorkflowCompensationConflict
			}
			continue
		}
		copy := *record
		if copy.ID.IsZero() {
			copy.ID = primitive.NewObjectID()
		}
		r.compensations[copy.Sequence] = &copy
	}
	return nil
}

func (r *compensationWorkflowRepositoryFake) ListWorkflowCompensations(_ context.Context, runID primitive.ObjectID, userID uint64) ([]*repository.WorkflowCompensationRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*repository.WorkflowCompensationRecord, 0, len(r.compensations))
	for _, record := range r.compensations {
		if record.RunID == runID && record.UserID == userID {
			copy := *record
			result = append(result, &copy)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
	return result, nil
}

func (r *compensationWorkflowRepositoryFake) ClaimWorkflowCompensation(
	_ context.Context,
	runID primitive.ObjectID,
	userID uint64,
	sequence int,
	attemptID string,
	leaseUntil time.Time,
	approvalID primitive.ObjectID,
	retryFailed bool,
) (*repository.WorkflowCompensationRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.compensations[sequence]
	if record == nil || record.RunID != runID || record.UserID != userID {
		return nil, repository.ErrWorkflowCompensationUnavailable
	}
	claimable := record.Status == repository.WorkflowCompensationStatusPlanned ||
		(record.Status == repository.WorkflowCompensationStatusExecuting && !record.LeaseUntil.After(time.Now())) ||
		(retryFailed && record.Status == repository.WorkflowCompensationStatusFailed) ||
		(!approvalID.IsZero() && record.Status == repository.WorkflowCompensationStatusSuspended && record.ApprovalRequestID == approvalID)
	if !claimable {
		return nil, repository.ErrWorkflowCompensationUnavailable
	}
	record.Status = repository.WorkflowCompensationStatusExecuting
	record.AttemptID = attemptID
	record.LeaseUntil = leaseUntil
	record.ApprovalRequestID = primitive.NilObjectID
	record.Attempt++
	copy := *record
	return &copy, nil
}

func (r *compensationWorkflowRepositoryFake) CompleteWorkflowCompensation(_ context.Context, compensationID primitive.ObjectID, attemptID, outputJSON string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.compensations {
		if record.ID == compensationID && record.Status == repository.WorkflowCompensationStatusExecuting && record.AttemptID == attemptID {
			record.Status = repository.WorkflowCompensationStatusSucceeded
			record.OutputJSON = outputJSON
			record.AttemptID = ""
			record.LeaseUntil = time.Time{}
			return nil
		}
	}
	return repository.ErrWorkflowCompensationClaimInvalid
}

func (r *compensationWorkflowRepositoryFake) SuspendWorkflowCompensation(_ context.Context, compensationID primitive.ObjectID, attemptID string, approvalID primitive.ObjectID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.compensations {
		if record.ID == compensationID && record.Status == repository.WorkflowCompensationStatusExecuting && record.AttemptID == attemptID {
			record.Status = repository.WorkflowCompensationStatusSuspended
			record.ApprovalRequestID = approvalID
			record.AttemptID = ""
			record.LeaseUntil = time.Time{}
			return nil
		}
	}
	return repository.ErrWorkflowCompensationClaimInvalid
}

func (r *compensationWorkflowRepositoryFake) FailWorkflowCompensation(_ context.Context, compensationID primitive.ObjectID, attemptID, errorMessage string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.compensations {
		if record.ID == compensationID && record.Status == repository.WorkflowCompensationStatusExecuting && record.AttemptID == attemptID {
			record.Status = repository.WorkflowCompensationStatusFailed
			record.ErrorMessage = errorMessage
			record.AttemptID = ""
			record.LeaseUntil = time.Time{}
			return nil
		}
	}
	return repository.ErrWorkflowCompensationClaimInvalid
}

func (r *compensationWorkflowRepositoryFake) RejectWorkflowCompensation(_ context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.compensations {
		if record.RunID == runID && record.UserID == userID && record.Status == repository.WorkflowCompensationStatusSuspended && record.ApprovalRequestID == approvalID {
			record.Status = repository.WorkflowCompensationStatusFailed
			record.ErrorMessage = reason
			record.ApprovalRequestID = primitive.NilObjectID
			return nil
		}
	}
	return repository.ErrWorkflowCompensationUnavailable
}

func TestFailedWorkflowPersistsDeterministicCompensationPlan(t *testing.T) {
	definition := dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{
				ID: "reserve", Type: "tool", Properties: json.RawMessage(`{"tool_name":"Reserve"}`),
				Compensation: &dsl.CompensationDSL{
					ToolName: "Release", Properties: json.RawMessage(`{"reservation_id":"{{reserve.id}}"}`),
				},
			},
			{ID: "fail", Type: "tool", Properties: json.RawMessage(`{"tool_name":"Fail"}`)},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-reserve", Source: "start", Target: "reserve"},
			{ID: "reserve-fail", Source: "reserve", Target: "fail"},
		},
	}
	dslJSON, err := json.Marshal(definition)
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 51, Name: "compensation flow", DSLJSON: string(dslJSON),
	}
	repo := newCompensationWorkflowRepositoryFake(workflow)
	registry := workflowTool.NewRegistry()
	releaseCalls := 0
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"Reserve", "reserve resource", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"id": "reservation-51"}, nil
		},
	)))
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"Fail", "fail after side effect", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			return nil, errors.New("downstream failed")
		},
	)))
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"Release", "release resource", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			releaseCalls++
			return map[string]interface{}{"released": true}, nil
		},
	)))
	svc := &AgentService{repo: repo, workflowToolExecutor: workflowTool.NewExecutor(registry)}

	result, err := svc.RunWorkflow(context.Background(), 51, workflow.ID.Hex(), `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCompensated, result.Run.Status)
	require.ErrorContains(t, errors.New(result.Run.ErrorMessage), "downstream failed")
	require.Equal(t, 1, releaseCalls)

	records, err := repo.ListWorkflowCompensations(context.Background(), result.Run.ID, 51)
	require.NoError(t, err)
	require.Len(t, records, 1)
	record := records[0]
	require.Equal(t, repository.WorkflowCompensationStatusSucceeded, record.Status)
	require.Equal(t, "reserve", record.SourceNodeID)
	require.Equal(t, "reserve$compensate", record.StepID)
	require.Equal(t, "Release", record.ToolName)
	require.Equal(t, result.Run.ID.Hex()+":reserve$compensate:Release", record.IdempotencyKey)
	require.NotEmpty(t, record.InputHash)
	require.NotEmpty(t, record.PlanHash)
	require.JSONEq(t, `{"released":true}`, record.OutputJSON)
	var inputs map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(record.InputJSON), &inputs))
	require.Equal(t, "reservation-51", inputs["reservation_id"])
}

func TestWriteCompensationSuspendsForApprovalAndResumesOnce(t *testing.T) {
	definition := compensationWorkflowDefinition(&dsl.CompensationDSL{
		ToolName: "ReleaseWrite", Properties: json.RawMessage(`{"reservation_id":"{{reserve.id}}"}`),
	})
	workflow := compensationWorkflowRecord(t, 63, definition)
	repo := newCompensationWorkflowRepositoryFake(workflow)
	registry := workflowTool.NewRegistry()
	releaseCalls := 0
	reserveCalls := 0
	registerCompensationTestBaseTools(t, registry, &reserveCalls)
	require.NoError(t, registry.Register(workflowTool.NewDelegatedToolWithSpec(workflowTool.ToolSpec{
		Name: "ReleaseWrite", Description: "release reserved resource", InputSchema: json.RawMessage(`{"type":"object"}`),
		Category: workflowTool.CategoryWrite, Permission: workflowTool.PermissionAuthenticated,
		Timeout: time.Second, Retry: workflowTool.RetryPolicy{MaxAttempts: 1},
		Idempotency: workflowTool.IdempotencyPolicy{Required: true}, Approval: workflowTool.ApprovalRequired,
	}, func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		releaseCalls++
		return map[string]interface{}{"released": true}, nil
	})))
	executor := workflowTool.NewExecutor(
		registry,
		workflowTool.WithApprovalGate(NewPersistentApprovalGate(repo, time.Hour)),
		workflowTool.WithIdempotencyStore(NewPersistentToolResultStore(repo)),
	)
	svc := &AgentService{repo: repo, workflowToolExecutor: executor}

	result, err := svc.RunWorkflow(context.Background(), 63, workflow.ID.Hex(), `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuspended, result.Run.Status)
	require.NotEmpty(t, result.ResumeToken)
	require.True(t, strings.HasSuffix(result.Run.WaitingNodeID, "$compensate"))
	require.Zero(t, releaseCalls)
	records, err := repo.ListWorkflowCompensations(context.Background(), result.Run.ID, 63)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowCompensationStatusSuspended, records[0].Status)
	require.Equal(t, result.Run.ApprovalRequestID, records[0].ApprovalRequestID)

	approvals, _, err := svc.ListToolApprovals(context.Background(), 63, repository.ToolApprovalStatusPending, 1, 20)
	require.NoError(t, err)
	require.Len(t, approvals, 1)
	approved, err := svc.DecideToolApproval(context.Background(), 63, approvals[0].ID, repository.ToolApprovalStatusApproved, "", approvals[0].Revision)
	require.NoError(t, err)
	resumed, err := svc.ResumeWorkflowRun(context.Background(), 63, result.Run.ID.Hex(), approved.ID, result.ResumeToken, `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCompensated, resumed.Run.Status)
	require.Equal(t, 1, releaseCalls)
	require.Contains(t, resumed.Run.ErrorMessage, "downstream failed")

	_, err = svc.ResumeWorkflowRun(context.Background(), 63, result.Run.ID.Hex(), approved.ID, result.ResumeToken, `{}`)
	require.Error(t, err)
	require.Equal(t, 1, releaseCalls)
}

func TestRejectingCompensationApprovalTerminatesRunAndJournal(t *testing.T) {
	definition := compensationWorkflowDefinition(&dsl.CompensationDSL{
		ToolName: "ReleaseWrite", Properties: json.RawMessage(`{"reservation_id":"{{reserve.id}}"}`),
	})
	workflow := compensationWorkflowRecord(t, 64, definition)
	repo := newCompensationWorkflowRepositoryFake(workflow)
	registry := workflowTool.NewRegistry()
	reserveCalls := 0
	registerCompensationTestBaseTools(t, registry, &reserveCalls)
	require.NoError(t, registry.Register(workflowTool.NewDelegatedToolWithSpec(workflowTool.ToolSpec{
		Name: "ReleaseWrite", InputSchema: json.RawMessage(`{"type":"object"}`),
		Category: workflowTool.CategoryWrite, Permission: workflowTool.PermissionAuthenticated,
		Timeout: time.Second, Retry: workflowTool.RetryPolicy{MaxAttempts: 1},
		Idempotency: workflowTool.IdempotencyPolicy{Required: true}, Approval: workflowTool.ApprovalRequired,
	}, func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"released": true}, nil
	})))
	executor := workflowTool.NewExecutor(
		registry,
		workflowTool.WithApprovalGate(NewPersistentApprovalGate(repo, time.Hour)),
		workflowTool.WithIdempotencyStore(NewPersistentToolResultStore(repo)),
	)
	svc := &AgentService{repo: repo, workflowToolExecutor: executor}

	result, err := svc.RunWorkflow(context.Background(), 64, workflow.ID.Hex(), `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuspended, result.Run.Status)
	approvals, _, err := svc.ListToolApprovals(context.Background(), 64, repository.ToolApprovalStatusPending, 1, 20)
	require.NoError(t, err)
	require.Len(t, approvals, 1)
	_, err = svc.DecideToolApproval(context.Background(), 64, approvals[0].ID, repository.ToolApprovalStatusRejected, "do not undo", approvals[0].Revision)
	require.NoError(t, err)

	run, err := repo.GetWorkflowRun(context.Background(), result.Run.ID, 64)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusRejected, run.Status)
	records, err := repo.ListWorkflowCompensations(context.Background(), result.Run.ID, 64)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowCompensationStatusFailed, records[0].Status)
	require.Equal(t, "do not undo", records[0].ErrorMessage)
}

func TestFailedCompensationCanBeRecoveredWithoutReplayingWorkflow(t *testing.T) {
	definition := compensationWorkflowDefinition(&dsl.CompensationDSL{
		ToolName: "ReleaseEventually", Properties: json.RawMessage(`{"reservation_id":"{{reserve.id}}"}`),
	})
	workflow := compensationWorkflowRecord(t, 71, definition)
	repo := newCompensationWorkflowRepositoryFake(workflow)
	registry := workflowTool.NewRegistry()
	releaseCalls := 0
	reserveCalls := 0
	registerCompensationTestBaseTools(t, registry, &reserveCalls)
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"ReleaseEventually", "compensate", `{"type":"object"}`, func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			releaseCalls++
			if releaseCalls == 1 {
				return nil, errors.New("release backend unavailable")
			}
			return map[string]interface{}{"released": true}, nil
		})))
	svc := &AgentService{repo: repo, workflowToolExecutor: workflowTool.NewExecutor(registry)}

	result, err := svc.RunWorkflow(context.Background(), 71, workflow.ID.Hex(), `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCompensationFailed, result.Run.Status)
	require.Equal(t, 1, reserveCalls)
	require.Equal(t, 1, releaseCalls)

	recovered, err := svc.ResumeWorkflowRun(context.Background(), 71, result.Run.ID.Hex(), "", "", `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusCompensated, recovered.Run.Status)
	require.Equal(t, 1, reserveCalls, "main workflow side effect must not be replayed")
	require.Equal(t, 2, releaseCalls)
}

func TestPersistWorkflowCompensationPlanIsIdempotentAndDetectsDrift(t *testing.T) {
	repo := newCompensationWorkflowRepositoryFake(nil)
	svc := &AgentService{repo: repo}
	run := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(),
		WorkflowRevisionID: primitive.NewObjectID(), UserID: 9,
	}
	tasks := []engine.CompensationTask{{
		Sequence: 1, SourceNodeID: "write", StepID: "write$compensate",
		ToolName: "Undo", Inputs: map[string]interface{}{"id": "resource-1"},
	}}

	require.NoError(t, svc.persistWorkflowCompensationPlan(context.Background(), run, tasks))
	require.NoError(t, svc.persistWorkflowCompensationPlan(context.Background(), run, tasks))
	require.Len(t, repo.compensations, 1)

	drifted := append([]engine.CompensationTask(nil), tasks...)
	drifted[0].ToolName = "DifferentUndo"
	require.ErrorIs(t, svc.persistWorkflowCompensationPlan(context.Background(), run, drifted), repository.ErrWorkflowCompensationConflict)
}

func TestBuildWorkflowNodesRejectsUnregisteredCompensationTool(t *testing.T) {
	registry := workflowTool.NewRegistry()
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"Reserve", "reserve", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) { return nil, nil },
	)))
	definition := &dsl.WorkflowDSL{Nodes: []dsl.NodeDSL{{
		ID: "reserve", Type: "tool", Properties: json.RawMessage(`{"tool_name":"Reserve"}`),
		Compensation: &dsl.CompensationDSL{ToolName: "MissingUndo"},
	}}}

	_, err := buildWorkflowNodes(definition, workflowTool.NewExecutor(registry))
	require.ErrorContains(t, err, "unregistered compensation tool MissingUndo")
}

func compensationWorkflowDefinition(compensation *dsl.CompensationDSL) dsl.WorkflowDSL {
	return dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "reserve", Type: "tool", Properties: json.RawMessage(`{"tool_name":"Reserve"}`), Compensation: compensation},
			{ID: "fail", Type: "tool", Properties: json.RawMessage(`{"tool_name":"Fail"}`)},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-reserve", Source: "start", Target: "reserve"},
			{ID: "reserve-fail", Source: "reserve", Target: "fail"},
		},
	}
}

func compensationWorkflowRecord(t *testing.T, userID uint64, definition dsl.WorkflowDSL) *repository.WorkflowDefinition {
	t.Helper()
	dslJSON, err := json.Marshal(definition)
	require.NoError(t, err)
	return &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: userID, Name: "compensation flow", DSLJSON: string(dslJSON),
	}
}

func registerCompensationTestBaseTools(t *testing.T, registry *workflowTool.ToolRegistry, reserveCalls *int) {
	t.Helper()
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"Reserve", "reserve", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			(*reserveCalls)++
			return map[string]interface{}{"id": "reservation"}, nil
		},
	)))
	require.NoError(t, registry.Register(workflowTool.NewDelegatedTool(
		"Fail", "fail", `{"type":"object"}`,
		func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			return nil, errors.New("downstream failed")
		},
	)))
}
