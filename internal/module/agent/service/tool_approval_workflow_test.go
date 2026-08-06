package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"google.golang.org/grpc"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/module/agent/repository"
	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/engine"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type approvalWorkflowRepositoryFake struct {
	repository.AgentRepository
	mu              sync.Mutex
	workflow        *repository.WorkflowDefinition
	currentRevision *repository.WorkflowRevision
	revisions       map[primitive.ObjectID]*repository.WorkflowRevision
	stateEvents     map[int64]*repository.WorkflowStateEvent
	stateSnapshots  map[int64]*repository.WorkflowStateSnapshot
	runs            map[primitive.ObjectID]*repository.WorkflowRunRecord
	approvals       map[primitive.ObjectID]*repository.ToolApprovalRequest
	executions      map[string]*repository.ToolExecutionRecord
}

func newApprovalWorkflowRepositoryFake(workflow *repository.WorkflowDefinition) *approvalWorkflowRepositoryFake {
	repo := &approvalWorkflowRepositoryFake{
		workflow: workflow, runs: make(map[primitive.ObjectID]*repository.WorkflowRunRecord),
		approvals:      make(map[primitive.ObjectID]*repository.ToolApprovalRequest),
		executions:     make(map[string]*repository.ToolExecutionRecord),
		revisions:      make(map[primitive.ObjectID]*repository.WorkflowRevision),
		stateEvents:    make(map[int64]*repository.WorkflowStateEvent),
		stateSnapshots: make(map[int64]*repository.WorkflowStateSnapshot),
	}
	if workflow != nil {
		revision := &repository.WorkflowRevision{
			ID: primitive.NewObjectID(), WorkflowID: workflow.ID, UserID: workflow.UserID,
			RevisionNumber: 1, DSLJSON: workflow.DSLJSON, CreatedAt: time.Now(),
		}
		repo.currentRevision = revision
		repo.revisions[revision.ID] = revision
	}
	return repo
}

func (r *approvalWorkflowRepositoryFake) GetWorkflow(_ context.Context, workflowID primitive.ObjectID, userID uint64) (*repository.WorkflowDefinition, error) {
	if r.workflow == nil || r.workflow.ID != workflowID || r.workflow.UserID != userID {
		return nil, errors.New("workflow not found")
	}
	return r.workflow, nil
}

func (r *approvalWorkflowRepositoryFake) ResolveCurrentWorkflowRevision(_ context.Context, workflowID primitive.ObjectID, userID uint64) (*repository.WorkflowRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentRevision == nil || r.currentRevision.WorkflowID != workflowID || r.currentRevision.UserID != userID {
		return nil, errors.New("workflow revision not found")
	}
	copy := *r.currentRevision
	return &copy, nil
}

func (r *approvalWorkflowRepositoryFake) GetWorkflowRevision(_ context.Context, workflowID, revisionID primitive.ObjectID, userID uint64) (*repository.WorkflowRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	revision := r.revisions[revisionID]
	if revision == nil || revision.WorkflowID != workflowID || revision.UserID != userID {
		return nil, errors.New("workflow revision not found")
	}
	copy := *revision
	return &copy, nil
}

func (r *approvalWorkflowRepositoryFake) ListWorkflowRevisions(_ context.Context, workflowID primitive.ObjectID, userID uint64, page, pageSize int) ([]*repository.WorkflowRevision, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	revisions := make([]*repository.WorkflowRevision, 0, len(r.revisions))
	for _, revision := range r.revisions {
		if revision.WorkflowID == workflowID && revision.UserID == userID {
			copy := *revision
			revisions = append(revisions, &copy)
		}
	}
	sort.Slice(revisions, func(i, j int) bool {
		return revisions[i].RevisionNumber > revisions[j].RevisionNumber
	})
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	total := int64(len(revisions))
	start := (page - 1) * pageSize
	if start >= len(revisions) {
		return []*repository.WorkflowRevision{}, total, nil
	}
	end := start + pageSize
	if end > len(revisions) {
		end = len(revisions)
	}
	return revisions[start:end], total, nil
}

func (r *approvalWorkflowRepositoryFake) AppendWorkflowStateEvents(_ context.Context, events []*repository.WorkflowStateEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range events {
		if existing := r.stateEvents[event.Sequence]; existing != nil {
			if existing.EventHash != event.EventHash {
				return repository.ErrWorkflowStateEventConflict
			}
			continue
		}
		copy := *event
		r.stateEvents[event.Sequence] = &copy
	}
	return nil
}

func (r *approvalWorkflowRepositoryFake) ListWorkflowStateEvents(_ context.Context, _ primitive.ObjectID, _ uint64, afterSequence int64) ([]*repository.WorkflowStateEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := make([]*repository.WorkflowStateEvent, 0, len(r.stateEvents))
	for sequence := afterSequence + 1; sequence <= int64(len(r.stateEvents)); sequence++ {
		if event := r.stateEvents[sequence]; event != nil {
			copy := *event
			events = append(events, &copy)
		}
	}
	return events, nil
}

func (r *approvalWorkflowRepositoryFake) SaveWorkflowStateSnapshot(_ context.Context, snapshot *repository.WorkflowStateSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.stateSnapshots[snapshot.StateVersion]; existing != nil {
		if existing.SnapshotHash != snapshot.SnapshotHash {
			return repository.ErrWorkflowSnapshotConflict
		}
		return nil
	}
	copy := *snapshot
	r.stateSnapshots[snapshot.StateVersion] = &copy
	return nil
}

func (r *approvalWorkflowRepositoryFake) GetLatestWorkflowStateSnapshot(_ context.Context, runID primitive.ObjectID, userID uint64, atOrBeforeVersion int64) (*repository.WorkflowStateSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest *repository.WorkflowStateSnapshot
	for version, snapshot := range r.stateSnapshots {
		if version > atOrBeforeVersion || snapshot.RunID != runID || snapshot.UserID != userID {
			continue
		}
		if latest == nil || snapshot.StateVersion > latest.StateVersion {
			copy := *snapshot
			latest = &copy
		}
	}
	return latest, nil
}

func (r *approvalWorkflowRepositoryFake) CreateWorkflowRun(_ context.Context, run *repository.WorkflowRunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run.ID.IsZero() {
		run.ID = primitive.NewObjectID()
	}
	run.Revision = 1
	r.runs[run.ID] = run
	return nil
}

func (r *approvalWorkflowRepositoryFake) UpdateWorkflowRun(_ context.Context, run *repository.WorkflowRunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.Revision++
	r.runs[run.ID] = run
	return nil
}

func (r *approvalWorkflowRepositoryFake) GetWorkflowRun(_ context.Context, runID primitive.ObjectID, userID uint64) (*repository.WorkflowRunRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[runID]
	if run == nil || run.UserID != userID {
		return nil, errors.New("workflow run not found")
	}
	copy := *run
	return &copy, nil
}

func (r *approvalWorkflowRepositoryFake) CreateOrGetToolApproval(_ context.Context, request *repository.ToolApprovalRequest) (*repository.ToolApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.approvals {
		if sameApprovalInvocation(existing, request) {
			copy := *existing
			return &copy, nil
		}
	}
	request.ID = primitive.NewObjectID()
	request.Status = repository.ToolApprovalStatusPending
	request.Revision = 1
	request.CreatedAt = time.Now()
	request.UpdatedAt = request.CreatedAt
	r.approvals[request.ID] = request
	copy := *request
	return &copy, nil
}

func (r *approvalWorkflowRepositoryFake) GetToolApproval(_ context.Context, approvalID primitive.ObjectID, userID uint64) (*repository.ToolApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	approval := r.approvals[approvalID]
	if approval == nil || approval.UserID != userID {
		return nil, repository.ErrToolApprovalNotFound
	}
	copy := *approval
	return &copy, nil
}

func (r *approvalWorkflowRepositoryFake) ListToolApprovals(_ context.Context, userID uint64, status string, _, _ int) ([]*repository.ToolApprovalRequest, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*repository.ToolApprovalRequest
	for _, approval := range r.approvals {
		if approval.UserID == userID && (status == "" || approval.Status == status) {
			copy := *approval
			result = append(result, &copy)
		}
	}
	return result, int64(len(result)), nil
}

func (r *approvalWorkflowRepositoryFake) DecideToolApproval(_ context.Context, approvalID primitive.ObjectID, userID uint64, decision, reason string, expectedRevision int64) (*repository.ToolApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	approval := r.approvals[approvalID]
	if approval == nil || approval.UserID != userID || approval.Status != repository.ToolApprovalStatusPending || approval.Revision != expectedRevision {
		return nil, repository.ErrToolApprovalConflict
	}
	approval.Status = decision
	approval.Reason = reason
	approval.Revision++
	approval.DecidedAt = time.Now()
	copy := *approval
	return &copy, nil
}

func (r *approvalWorkflowRepositoryFake) ClaimApprovedToolApproval(_ context.Context, match repository.ToolApprovalMatch, attemptID string, leaseUntil time.Time) (*repository.ToolApprovalRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, approval := range r.approvals {
		if approval.UserID == match.UserID && approval.RunID == match.RunID && approval.StepID == match.StepID &&
			approval.ToolName == match.ToolName && approval.InputDigest == match.InputDigest &&
			approval.IdempotencyKey == match.IdempotencyKey && approval.Status == repository.ToolApprovalStatusApproved {
			approval.Status = repository.ToolApprovalStatusExecuting
			approval.ExecutionAttemptID = attemptID
			approval.ExecutionLeaseUntil = leaseUntil
			approval.Revision++
			copy := *approval
			return &copy, nil
		}
	}
	return nil, repository.ErrToolApprovalUnavailable
}

func (r *approvalWorkflowRepositoryFake) CompleteToolApproval(_ context.Context, approvalID primitive.ObjectID, attemptID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	approval := r.approvals[approvalID]
	if approval == nil || approval.ExecutionAttemptID != attemptID {
		return repository.ErrToolApprovalConflict
	}
	approval.Status = repository.ToolApprovalStatusConsumed
	approval.Revision++
	return nil
}

func (r *approvalWorkflowRepositoryFake) ReleaseToolApproval(_ context.Context, approvalID primitive.ObjectID, attemptID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	approval := r.approvals[approvalID]
	if approval != nil && approval.ExecutionAttemptID == attemptID {
		approval.Status = repository.ToolApprovalStatusApproved
		approval.ExecutionAttemptID = ""
	}
	return nil
}

func (r *approvalWorkflowRepositoryFake) ClaimToolExecution(_ context.Context, record *repository.ToolExecutionRecord) (*repository.ToolExecutionRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := record.ToolName + ":" + record.IdempotencyKey
	if existing := r.executions[key]; existing != nil {
		if existing.InputDigest != record.InputDigest {
			return nil, false, repository.ErrToolExecutionConflict
		}
		if existing.Status == repository.ToolExecutionStatusSucceeded {
			copy := *existing
			return &copy, false, nil
		}
		return nil, false, repository.ErrToolExecutionInProgress
	}
	record.ID = primitive.NewObjectID()
	record.Status = repository.ToolExecutionStatusExecuting
	r.executions[key] = record
	copy := *record
	return &copy, true, nil
}

func (r *approvalWorkflowRepositoryFake) CompleteToolExecution(_ context.Context, executionID primitive.ObjectID, attemptID string, result repository.ToolExecutionResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, execution := range r.executions {
		if execution.ID == executionID && execution.AttemptID == attemptID {
			execution.Status = repository.ToolExecutionStatusSucceeded
			execution.Output = result.Output
			execution.OutputReference = result.OutputReference
			execution.OutputDigest = result.Digest
			execution.OutputLength = result.Length
			return nil
		}
	}
	return repository.ErrToolExecutionClaimInvalid
}

func (r *approvalWorkflowRepositoryFake) FailToolExecution(_ context.Context, executionID primitive.ObjectID, attemptID, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, execution := range r.executions {
		if execution.ID == executionID && execution.AttemptID == attemptID {
			execution.Status = repository.ToolExecutionStatusFailed
		}
	}
	return nil
}

func (r *approvalWorkflowRepositoryFake) ClaimWorkflowRunResume(_ context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, tokenHash, attemptID string) (*repository.WorkflowRunRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[runID]
	if run == nil || run.UserID != userID || run.Status != WorkflowRunStatusSuspended ||
		run.ResumeTokenHash != tokenHash || run.ApprovalRequestID != approvalID ||
		(!run.ResumeGrantExpiresAt.IsZero() && !run.ResumeGrantExpiresAt.After(time.Now())) {
		return nil, repository.ErrWorkflowResumeConflict
	}
	run.Status = WorkflowRunStatusRunning
	run.ResumeTokenHash = ""
	run.ResumeAttemptID = attemptID
	run.ResumeGrantIssuedAt = time.Time{}
	run.ResumeGrantExpiresAt = time.Time{}
	run.Revision++
	copy := *run
	return &copy, nil
}

func (r *approvalWorkflowRepositoryFake) IssueWorkflowResumeGrant(
	_ context.Context,
	runID primitive.ObjectID,
	userID uint64,
	approvalID primitive.ObjectID,
	expectedRevision int64,
	tokenHash string,
	issuedAt, expiresAt time.Time,
) (*repository.WorkflowRunRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[runID]
	if run == nil || run.UserID != userID || run.Status != WorkflowRunStatusSuspended ||
		run.ApprovalRequestID != approvalID || run.Revision != expectedRevision {
		return nil, repository.ErrWorkflowResumeGrantConflict
	}
	run.ResumeToken = ""
	run.ResumeTokenHash = tokenHash
	run.ResumeAttemptID = ""
	run.ResumeGrantIssuedAt = issuedAt
	run.ResumeGrantExpiresAt = expiresAt
	run.Revision++
	copy := *run
	return &copy, nil
}

func (r *approvalWorkflowRepositoryFake) RejectWorkflowRunForApproval(_ context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[runID]
	if run == nil || run.UserID != userID || run.ApprovalRequestID != approvalID || run.Status != WorkflowRunStatusSuspended {
		return repository.ErrWorkflowResumeConflict
	}
	run.Status = WorkflowRunStatusRejected
	run.ErrorMessage = reason
	return nil
}

func sameApprovalInvocation(left, right *repository.ToolApprovalRequest) bool {
	return left.UserID == right.UserID && left.RunID == right.RunID && left.StepID == right.StepID &&
		left.ToolName == right.ToolName && left.InputDigest == right.InputDigest && left.IdempotencyKey == right.IdempotencyKey
}

type approvalTweetClient struct {
	tweetv1.TweetServiceClient
	mu    sync.Mutex
	calls int
}

func (c *approvalTweetClient) CreateTweet(context.Context, *tweetv1.CreateTweetRequest, ...grpc.CallOption) (*tweetv1.CreateTweetResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return &tweetv1.CreateTweetResponse{Tweet: &tweetv1.Tweet{Id: 9001}}, nil
}

func (c *approvalTweetClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestWorkflowToolApprovalPersistsAndResumeTokenIsSingleUse(t *testing.T) {
	properties, err := json.Marshal(map[string]interface{}{
		"tool_name": "PublishTweet", "content": "sensitive draft",
	})
	require.NoError(t, err)
	definition := dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "publish", Type: "tool", Properties: properties},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-publish", Source: "start", Target: "publish"},
			{ID: "publish-end", Source: "publish", Target: "end"},
		},
	}
	dslJSON, err := json.Marshal(definition)
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 42, Name: "approval flow", DSLJSON: string(dslJSON),
	}
	repo := newApprovalWorkflowRepositoryFake(workflow)
	tweetClient := &approvalTweetClient{}
	registry := workflowTool.NewRegistry()
	require.NoError(t, registry.Register(workflowTool.NewPublishTweetTool(tweetClient)))
	executor := workflowTool.NewExecutor(
		registry,
		workflowTool.WithApprovalGate(NewPersistentApprovalGate(repo, time.Hour)),
		workflowTool.WithIdempotencyStore(NewPersistentToolResultStore(repo)),
	)
	svc := &AgentService{repo: repo, workflowToolExecutor: executor}

	result, err := svc.RunWorkflow(context.Background(), 42, workflow.ID.Hex(), `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuspended, result.Run.Status)
	require.NotEmpty(t, result.ResumeToken)
	require.Empty(t, result.Run.ResumeToken)
	require.NotEmpty(t, result.Run.ResumeTokenHash)
	require.False(t, result.Run.ApprovalRequestID.IsZero())
	require.Equal(t, repo.currentRevision.ID, result.Run.WorkflowRevisionID)
	require.EqualValues(t, 1, result.Run.WorkflowRevisionNumber)
	require.Positive(t, result.Run.StateVersion)
	require.Contains(t, repo.stateSnapshots, result.Run.StateVersion)
	require.Zero(t, tweetClient.count())

	// Editing the mutable workflow view after suspension must not alter the
	// immutable DSL selected by this run.
	repo.workflow.DSLJSON = `{"nodes":[]}`

	approvals, total, err := svc.ListToolApprovals(context.Background(), 42, repository.ToolApprovalStatusPending, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "[REDACTED]", approvals[0].RedactedInputs["content"])
	_, err = svc.DecideToolApproval(context.Background(), 99, approvals[0].ID, repository.ToolApprovalStatusApproved, "", approvals[0].Revision)
	require.Error(t, err)

	approved, err := svc.DecideToolApproval(context.Background(), 42, approvals[0].ID, repository.ToolApprovalStatusApproved, "", approvals[0].Revision)
	require.NoError(t, err)
	require.Equal(t, repository.ToolApprovalStatusApproved, approved.Status)
	_, err = svc.DecideToolApproval(context.Background(), 42, approvals[0].ID, repository.ToolApprovalStatusApproved, "", approvals[0].Revision)
	require.ErrorIs(t, err, repository.ErrToolApprovalConflict)

	_, err = svc.ResumeWorkflowRun(context.Background(), 42, result.Run.ID.Hex(), approved.ID, "wrong-token", `{}`)
	require.ErrorIs(t, err, repository.ErrWorkflowResumeConflict)
	require.Zero(t, tweetClient.count())

	type resumeOutcome struct {
		result *WorkflowExecutionResult
		err    error
	}
	outcomes := make(chan resumeOutcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			resumed, resumeErr := svc.ResumeWorkflowRun(context.Background(), 42, result.Run.ID.Hex(), approved.ID, result.ResumeToken, `{}`)
			outcomes <- resumeOutcome{result: resumed, err: resumeErr}
		}()
	}
	successes := 0
	for i := 0; i < 2; i++ {
		outcome := <-outcomes
		if outcome.err == nil {
			successes++
			require.Equal(t, WorkflowRunStatusSuccess, outcome.result.Run.Status)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, tweetClient.count())
	resumedRun, err := repo.GetWorkflowRun(context.Background(), result.Run.ID, 42)
	require.NoError(t, err)
	require.Greater(t, resumedRun.StateVersion, result.Run.StateVersion)
	events, err := repo.ListWorkflowStateEvents(context.Background(), result.Run.ID, 42, 0)
	require.NoError(t, err)
	require.Len(t, events, int(resumedRun.StateVersion))
	for index, event := range events {
		require.EqualValues(t, index+1, event.Sequence)
	}
	require.Contains(t, repo.stateSnapshots, resumedRun.StateVersion)

	_, err = svc.ResumeWorkflowRun(context.Background(), 42, result.Run.ID.Hex(), approved.ID, result.ResumeToken, `{}`)
	require.Error(t, err)
	require.Equal(t, 1, tweetClient.count())

	storedApprovalID, err := primitive.ObjectIDFromHex(approved.ID)
	require.NoError(t, err)
	storedApproval, err := repo.GetToolApproval(context.Background(), storedApprovalID, 42)
	require.NoError(t, err)
	require.Equal(t, repository.ToolApprovalStatusConsumed, storedApproval.Status)
}

func TestApprovedWorkflowIssuesRotatingCrossDeviceResumeGrant(t *testing.T) {
	properties, err := json.Marshal(map[string]interface{}{
		"tool_name": "PublishTweet", "content": "cross-device approved draft",
	})
	require.NoError(t, err)
	dslJSON, err := json.Marshal(dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "publish", Type: "tool", Properties: properties},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-publish", Source: "start", Target: "publish"},
			{ID: "publish-end", Source: "publish", Target: "end"},
		},
	})
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 52, Name: "cross-device approval", DSLJSON: string(dslJSON),
	}
	repo := newApprovalWorkflowRepositoryFake(workflow)
	tweetClient := &approvalTweetClient{}
	registry := workflowTool.NewRegistry()
	require.NoError(t, registry.Register(workflowTool.NewPublishTweetTool(tweetClient)))
	svc := &AgentService{
		repo: repo,
		workflowToolExecutor: workflowTool.NewExecutor(
			registry,
			workflowTool.WithApprovalGate(NewPersistentApprovalGate(repo, time.Hour)),
			workflowTool.WithIdempotencyStore(NewPersistentToolResultStore(repo)),
		),
	}

	suspended, err := svc.RunWorkflow(context.Background(), 52, workflow.ID.Hex(), `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuspended, suspended.Run.Status)
	initialRevision := suspended.Run.Revision
	originalToken := suspended.ResumeToken
	require.NotEmpty(t, originalToken)

	approvals, total, err := svc.ListToolApprovals(context.Background(), 52, repository.ToolApprovalStatusPending, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	_, err = svc.IssueWorkflowResumeGrant(context.Background(), 52, approvals[0].ID, initialRevision)
	require.ErrorContains(t, err, "not approved")
	_, err = svc.IssueWorkflowResumeGrant(context.Background(), 53, approvals[0].ID, initialRevision)
	require.Error(t, err)
	approved, err := svc.DecideToolApproval(
		context.Background(), 52, approvals[0].ID, repository.ToolApprovalStatusApproved, "", approvals[0].Revision,
	)
	require.NoError(t, err)
	approvalOID, err := primitive.ObjectIDFromHex(approved.ID)
	require.NoError(t, err)
	repo.mu.Lock()
	approvalExpiry := repo.approvals[approvalOID].ExpiresAt
	repo.approvals[approvalOID].ExpiresAt = time.Now().Add(-time.Second)
	repo.mu.Unlock()
	_, err = svc.IssueWorkflowResumeGrant(context.Background(), 52, approved.ID, initialRevision)
	require.ErrorContains(t, err, "expired")
	repo.mu.Lock()
	repo.approvals[approvalOID].ExpiresAt = approvalExpiry
	repo.mu.Unlock()

	firstGrant, err := svc.IssueWorkflowResumeGrant(context.Background(), 52, approved.ID, initialRevision)
	require.NoError(t, err)
	require.NotEmpty(t, firstGrant.ResumeToken)
	require.NotEqual(t, originalToken, firstGrant.ResumeToken)
	require.True(t, firstGrant.ExpiresAt.After(time.Now()))
	require.LessOrEqual(t, firstGrant.ExpiresAt.Unix(), approvals[0].ExpiresAt.Unix())
	require.Empty(t, firstGrant.Run.ResumeToken)
	require.Equal(t, hashWorkflowResumeToken(firstGrant.ResumeToken), firstGrant.Run.ResumeTokenHash)

	_, err = svc.IssueWorkflowResumeGrant(context.Background(), 52, approved.ID, initialRevision)
	require.ErrorIs(t, err, repository.ErrWorkflowResumeGrantConflict)
	secondGrant, err := svc.IssueWorkflowResumeGrant(context.Background(), 52, approved.ID, firstGrant.Run.Revision)
	require.NoError(t, err)
	require.NotEqual(t, firstGrant.ResumeToken, secondGrant.ResumeToken)

	_, err = svc.ResumeWorkflowRun(context.Background(), 52, suspended.Run.ID.Hex(), approved.ID, originalToken, `{}`)
	require.ErrorIs(t, err, repository.ErrWorkflowResumeConflict)
	_, err = svc.ResumeWorkflowRun(context.Background(), 52, suspended.Run.ID.Hex(), approved.ID, firstGrant.ResumeToken, `{}`)
	require.ErrorIs(t, err, repository.ErrWorkflowResumeConflict)

	repo.mu.Lock()
	repo.runs[suspended.Run.ID].ResumeGrantExpiresAt = time.Now().Add(-time.Second)
	repo.mu.Unlock()
	_, err = svc.ResumeWorkflowRun(context.Background(), 52, suspended.Run.ID.Hex(), approved.ID, secondGrant.ResumeToken, `{}`)
	require.ErrorIs(t, err, repository.ErrWorkflowResumeConflict)

	latestRun, err := repo.GetWorkflowRun(context.Background(), suspended.Run.ID, 52)
	require.NoError(t, err)
	finalGrant, err := svc.IssueWorkflowResumeGrant(context.Background(), 52, approved.ID, latestRun.Revision)
	require.NoError(t, err)
	resumed, err := svc.ResumeWorkflowRun(
		context.Background(), 52, suspended.Run.ID.Hex(), approved.ID, finalGrant.ResumeToken, `{}`,
	)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuccess, resumed.Run.Status)
	require.Equal(t, 1, tweetClient.count())
	require.Empty(t, resumed.Run.ResumeTokenHash)
	require.True(t, resumed.Run.ResumeGrantIssuedAt.IsZero())
	require.True(t, resumed.Run.ResumeGrantExpiresAt.IsZero())
}

func TestWorkflowRevisionAPIAndSpecifiedRunUseImmutableDSL(t *testing.T) {
	validDSL, err := json.Marshal(dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{{ID: "start", Type: "start"}, {ID: "end", Type: "end"}},
		Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
	})
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 77, Name: "revision flow", DSLJSON: `{"nodes":[]}`,
	}
	repo := newApprovalWorkflowRepositoryFake(workflow)
	repo.currentRevision.RevisionNumber = 2
	repo.currentRevision.DSLJSON = workflow.DSLJSON
	oldRevision := &repository.WorkflowRevision{
		ID: primitive.NewObjectID(), WorkflowID: workflow.ID, UserID: workflow.UserID,
		RevisionNumber: 1, DSLJSON: string(validDSL), CreatedAt: time.Now().Add(-time.Minute),
	}
	repo.revisions[oldRevision.ID] = oldRevision
	svc := &AgentService{
		repo: repo, workflowToolExecutor: workflowTool.NewExecutor(workflowTool.NewRegistry()),
	}

	revisions, total, err := svc.ListWorkflowRevisions(context.Background(), 77, workflow.ID.Hex(), 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.EqualValues(t, 2, revisions[0].RevisionNumber)

	detail, err := svc.GetWorkflowRevision(context.Background(), 77, workflow.ID.Hex(), oldRevision.ID.Hex())
	require.NoError(t, err)
	require.Equal(t, oldRevision.ID, detail.ID)

	result, err := svc.RunWorkflowRevision(context.Background(), 77, workflow.ID.Hex(), oldRevision.ID.Hex(), `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuccess, result.Run.Status)
	require.Equal(t, oldRevision.ID, result.Run.WorkflowRevisionID)
	require.EqualValues(t, 1, result.Run.WorkflowRevisionNumber)
}

func TestWorkflowCheckpointReplayRejectsTamperedState(t *testing.T) {
	repo := newApprovalWorkflowRepositoryFake(nil)
	svc := &AgentService{repo: repo}
	run := &repository.WorkflowRunRecord{ID: primitive.NewObjectID(), UserID: 88}
	blackboard := engine.NewBlackboard()
	blackboard.ApplyDelta("start", map[string]interface{}{"user_input": "trusted"})
	blackboard.ApplyDelta("step", map[string]interface{}{"value": "persisted"})
	require.NoError(t, svc.persistWorkflowState(context.Background(), run, blackboard, true))

	checkpoint := engine.WorkflowCheckpoint{
		Blackboard:   blackboard.GetSnapshot(),
		StateVersion: blackboard.Version(),
	}
	rehydrated, err := svc.rehydrateWorkflowCheckpoint(context.Background(), run, checkpoint)
	require.NoError(t, err)
	require.Equal(t, checkpoint.Blackboard, rehydrated.Blackboard)

	checkpoint.Blackboard["step"]["value"] = "tampered"
	_, err = svc.rehydrateWorkflowCheckpoint(context.Background(), run, checkpoint)
	require.ErrorContains(t, err, "failed persisted event replay validation")

	checkpoint.Blackboard = blackboard.GetSnapshot()
	repo.mu.Lock()
	repo.stateSnapshots = make(map[int64]*repository.WorkflowStateSnapshot)
	repo.stateEvents[1].EventHash = "tampered"
	repo.mu.Unlock()
	_, err = svc.rehydrateWorkflowCheckpoint(context.Background(), run, checkpoint)
	require.ErrorContains(t, err, "event 1 failed integrity validation")
}

func TestWorkflowPersistsPeriodicAndFinalStateSnapshots(t *testing.T) {
	validDSL, err := json.Marshal(dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{{ID: "start", Type: "start"}, {ID: "end", Type: "end"}},
		Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
	})
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 91, Name: "snapshot flow", DSLJSON: string(validDSL),
	}
	repo := newApprovalWorkflowRepositoryFake(workflow)
	svc := &AgentService{
		repo: repo, workflowToolExecutor: workflowTool.NewExecutor(workflowTool.NewRegistry()),
		workflowSnapshotInterval: 2,
	}

	result, err := svc.RunWorkflow(context.Background(), 91, workflow.ID.Hex(), `{}`)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusSuccess, result.Run.Status)
	require.Contains(t, repo.stateSnapshots, int64(2))
	require.Contains(t, repo.stateSnapshots, result.Run.StateVersion)
	require.GreaterOrEqual(t, result.Run.Revision, int64(3))
}

func TestRejectToolApprovalTerminatesSuspendedRun(t *testing.T) {
	runID := primitive.NewObjectID()
	approvalID := primitive.NewObjectID()
	parentRunID := primitive.NewObjectID().Hex()
	repo := newApprovalWorkflowRepositoryFake(nil)
	repo.runs[runID] = &repository.WorkflowRunRecord{
		ID: runID, UserID: 7, Status: WorkflowRunStatusSuspended, ApprovalRequestID: approvalID,
		InvocationSource: string(workflowTool.SourceRuntime), ParentRunID: parentRunID,
	}
	repo.approvals[approvalID] = &repository.ToolApprovalRequest{
		ID: approvalID, UserID: 7, RunID: runID.Hex(), Source: string(workflowTool.SourceWorkflow),
		Status: repository.ToolApprovalStatusPending, Revision: 3,
	}
	parentRuns := &memoryAgentExecutionRunStore{run: &repository.AgentExecutionRun{
		ID: parentRunID, UserID: 7, Status: repository.AgentExecutionRunApprovalRequired,
		PendingResumeKind: repository.AgentExecutionResumeDelegatedApproval,
		ApprovalRequestID: approvalID.Hex(), ResumeSupported: true,
		CheckpointCiphertext: "encrypted", Revision: 2,
	}}
	svc := &AgentService{repo: repo, agentExecutionRunStore: parentRuns}

	_, err := svc.DecideToolApproval(context.Background(), 7, approvalID.Hex(), repository.ToolApprovalStatusRejected, "not allowed", 3)
	require.NoError(t, err)
	run, err := repo.GetWorkflowRun(context.Background(), runID, 7)
	require.NoError(t, err)
	require.Equal(t, WorkflowRunStatusRejected, run.Status)
	require.Equal(t, "not allowed", run.ErrorMessage)
	parent, err := parentRuns.GetAgentExecutionRun(context.Background(), parentRunID, 7)
	require.NoError(t, err)
	require.Equal(t, repository.AgentExecutionRunFailed, parent.Status)
	require.Equal(t, "approval_rejected", parent.FailureCode)
	require.False(t, parent.ResumeSupported)
	require.Empty(t, parent.PendingResumeKind)
}
