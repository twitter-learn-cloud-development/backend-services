package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type memoryWorkflowToolPublicationStore struct {
	mu           sync.Mutex
	byWorkflowID map[string]*repository.WorkflowToolPublication
}

type workflowAsToolAgentRepository struct {
	*approvalWorkflowRepositoryFake
	dialogues *assistRuntimeRepository
}

type workflowApprovalRuntimeModel struct {
	mu       sync.Mutex
	toolName string
	calls    int
}

func (m *workflowApprovalRuntimeModel) Complete(
	_ context.Context,
	_ agentRuntime.ModelRequest,
) (agentRuntime.ModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.calls == 1 {
		return agentRuntime.ModelResponse{Actions: []agentRuntime.Action{{
			ID: "workflow-write-action", Type: agentRuntime.ActionToolCall, Name: m.toolName,
			Arguments: json.RawMessage(`{"user_input":"publish the governed result"}`),
		}}}, nil
	}
	return agentRuntime.ModelResponse{
		Message: agentRuntime.Message{Content: "The governed workflow completed."},
	}, nil
}

func (r *workflowAsToolAgentRepository) CreateDialogue(
	ctx context.Context,
	userID uint64,
	title string,
	mode repository.DialogueMode,
) (*repository.Dialogue, error) {
	return r.dialogues.CreateDialogue(ctx, userID, title, mode)
}

func (r *workflowAsToolAgentRepository) GetDialogue(
	ctx context.Context,
	id primitive.ObjectID,
) (*repository.Dialogue, error) {
	return r.dialogues.GetDialogue(ctx, id)
}

func (r *workflowAsToolAgentRepository) GetRecentMessages(
	ctx context.Context,
	dialogueID primitive.ObjectID,
	limit int,
) ([]*repository.DialogueMessage, error) {
	return r.dialogues.GetRecentMessages(ctx, dialogueID, limit)
}

func (r *workflowAsToolAgentRepository) SaveMessages(
	ctx context.Context,
	messages []*repository.DialogueMessage,
) error {
	return r.dialogues.SaveMessages(ctx, messages)
}

func (r *workflowAsToolAgentRepository) SaveMessage(
	ctx context.Context,
	message *repository.DialogueMessage,
) error {
	return r.dialogues.SaveMessage(ctx, message)
}

func (r *workflowAsToolAgentRepository) TouchDialogue(
	ctx context.Context,
	dialogueID primitive.ObjectID,
) error {
	return r.dialogues.TouchDialogue(ctx, dialogueID)
}

func newMemoryWorkflowToolPublicationStore() *memoryWorkflowToolPublicationStore {
	return &memoryWorkflowToolPublicationStore{
		byWorkflowID: make(map[string]*repository.WorkflowToolPublication),
	}
}

func (s *memoryWorkflowToolPublicationStore) SaveWorkflowToolPublication(
	_ context.Context,
	publication *repository.WorkflowToolPublication,
	expectedRevision int64,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := workflowToolPublicationStoreKey(publication.UserID, publication.WorkflowID)
	existing := s.byWorkflowID[key]
	if existing == nil {
		if expectedRevision != 0 {
			return repository.ErrWorkflowToolPublicationConflict
		}
		copy := cloneWorkflowToolPublication(publication)
		copy.ID = primitive.NewObjectID()
		copy.Revision = 1
		copy.CreatedAt = time.Now()
		copy.UpdatedAt = copy.CreatedAt
		s.byWorkflowID[key] = copy
		*publication = *cloneWorkflowToolPublication(copy)
		return nil
	}
	if expectedRevision < 1 || existing.Revision != expectedRevision {
		return repository.ErrWorkflowToolPublicationConflict
	}
	copy := cloneWorkflowToolPublication(publication)
	copy.ID = existing.ID
	copy.CreatedAt = existing.CreatedAt
	copy.UpdatedAt = time.Now()
	copy.Revision = existing.Revision + 1
	s.byWorkflowID[key] = copy
	*publication = *cloneWorkflowToolPublication(copy)
	return nil
}

func (s *memoryWorkflowToolPublicationStore) GetWorkflowToolPublication(
	_ context.Context,
	userID uint64,
	workflowID primitive.ObjectID,
) (*repository.WorkflowToolPublication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	publication := s.byWorkflowID[workflowToolPublicationStoreKey(userID, workflowID)]
	if publication == nil {
		return nil, repository.ErrWorkflowToolPublicationNotFound
	}
	return cloneWorkflowToolPublication(publication), nil
}

func (s *memoryWorkflowToolPublicationStore) GetWorkflowToolPublicationByName(
	_ context.Context,
	userID uint64,
	toolName string,
) (*repository.WorkflowToolPublication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, publication := range s.byWorkflowID {
		if publication.UserID == userID && publication.ToolName == toolName {
			return cloneWorkflowToolPublication(publication), nil
		}
	}
	return nil, repository.ErrWorkflowToolPublicationNotFound
}

func (s *memoryWorkflowToolPublicationStore) ListActiveWorkflowToolPublications(
	_ context.Context,
	userID uint64,
	limit int,
) ([]*repository.WorkflowToolPublication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 {
		limit = defaultWorkflowToolCatalogLimit
	}
	publications := make([]*repository.WorkflowToolPublication, 0)
	for _, publication := range s.byWorkflowID {
		if publication.UserID == userID &&
			publication.Status == repository.WorkflowToolPublicationActive {
			publications = append(publications, cloneWorkflowToolPublication(publication))
		}
	}
	sort.Slice(publications, func(i, j int) bool {
		return publications[i].ToolName < publications[j].ToolName
	})
	if len(publications) > limit {
		publications = publications[:limit]
	}
	return publications, nil
}

func TestPublishedWorkflowToolUsesImmutableRevisionAndRecordsLineage(t *testing.T) {
	workflowDSL := dsl.WorkflowDSL{
		Name: "Read-only summary",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
	}
	service, repo, store := newWorkflowAsToolTestService(
		t,
		42,
		workflowDSL,
		workflowTool.NewRegistry(),
	)

	publication, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		PublishWorkflowToolInput{Description: "Summarize the supplied material."},
	)
	require.NoError(t, err)
	require.Equal(t, workflowRuntimeToolName(repo.workflow.ID), publication.ToolName)
	require.Equal(t, repo.currentRevision.ID, publication.WorkflowRevisionID)
	require.EqualValues(t, 1, publication.Revision)

	// A mutable draft and current revision may advance without changing the
	// immutable revision selected by the active publication.
	newDSL := `{"name":"edited draft","nodes":[],"edges":[]}`
	newRevision := &repository.WorkflowRevision{
		ID: primitive.NewObjectID(), WorkflowID: repo.workflow.ID, UserID: 42,
		RevisionNumber: 2, DSLJSON: newDSL, DSLHash: workflowAsToolDSLHash(newDSL),
		CreatedAt: time.Now(),
	}
	repo.workflow.DSLJSON = newDSL
	repo.currentRevision = newRevision
	repo.revisions[newRevision.ID] = newRevision

	definitions, err := service.listPublishedWorkflowRuntimeTools(context.Background(), 42)
	require.NoError(t, err)
	require.Len(t, definitions, 1)
	require.Equal(t, publication.ToolName, definitions[0].Name)
	require.Equal(t, agentRuntime.ToolCategoryRead, definitions[0].Category)
	require.False(t, definitions[0].ApprovalRequired())

	result, err := (&mcpRuntimeToolExecutor{service: service}).Execute(
		context.Background(),
		agentRuntime.ToolCall{
			RunContext: agentRuntime.RunContext{RunID: "agent-run-1", UserID: 42},
			ActionID:   "action-1",
			Name:       publication.ToolName,
			Arguments:  json.RawMessage(`{"user_input":"summarize this"}`),
		},
	)
	require.NoError(t, err)
	require.NotEmpty(t, result.Content)
	var structured map[string]any
	require.NoError(t, json.Unmarshal(result.StructuredContent, &structured))
	require.Equal(t, workflowToolResultSchema, structured["schema"])
	require.Equal(t, publication.WorkflowRevisionID.Hex(), structured["workflow_revision_id"])

	repo.mu.Lock()
	require.Len(t, repo.runs, 1)
	var childRun *repository.WorkflowRunRecord
	for _, run := range repo.runs {
		childRun = run
	}
	repo.mu.Unlock()
	require.NotNil(t, childRun)
	require.Equal(t, publication.WorkflowRevisionID, childRun.WorkflowRevisionID)
	require.Equal(t, string(workflowTool.SourceRuntime), childRun.InvocationSource)
	require.Equal(t, "agent-run-1", childRun.ParentRunID)
	require.Equal(t, "action-1", childRun.ParentActionID)

	_, err = service.GetWorkflowToolPublication(context.Background(), 99, repo.workflow.ID.Hex())
	require.ErrorIs(t, err, repository.ErrWorkflowToolPublicationNotFound)

	_, err = service.UnpublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		publication.Revision+1,
	)
	require.ErrorIs(t, err, repository.ErrWorkflowToolPublicationConflict)
	disabled, err := service.UnpublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		publication.Revision,
	)
	require.NoError(t, err)
	require.Equal(t, repository.WorkflowToolPublicationDisabled, disabled.Status)

	active, err := store.ListActiveWorkflowToolPublications(context.Background(), 42, 20)
	require.NoError(t, err)
	require.Empty(t, active)
}

func TestPublishedWorkflowToolResumesHumanInputWaitWithoutCreatingAnotherChildRun(t *testing.T) {
	workflowDSL := dsl.WorkflowDSL{
		Name: "Clarified digest",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{
				ID:   "clarify",
				Type: "wait",
				Properties: json.RawMessage(
					`{"resume_mode":"human_input","reason":"Which repository should be inspected?"}`,
				),
			},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-clarify", Source: "start", Target: "clarify"},
			{ID: "clarify-end", Source: "clarify", Target: "end"},
		},
	}
	service, repo, _ := newWorkflowAsToolTestService(
		t,
		42,
		workflowDSL,
		workflowTool.NewRegistry(),
	)
	publication, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		PublishWorkflowToolInput{Description: "Build a digest after one clarification."},
	)
	require.NoError(t, err)

	executor := &mcpRuntimeToolExecutor{service: service}
	call := agentRuntime.ToolCall{
		RunContext: agentRuntime.RunContext{RunID: "agent-run-wait", UserID: 42},
		ActionID:   "workflow-action-wait",
		Name:       publication.ToolName,
		Arguments:  json.RawMessage(`{"user_input":"inspect a repository"}`),
	}
	_, err = executor.Execute(context.Background(), call)
	var suspended *agentRuntime.ToolSuspensionError
	require.ErrorAs(t, err, &suspended)
	require.Equal(t, "Which repository should be inspected?", suspended.Continuation.Prompt)
	require.Equal(t, workflowToolContinuationVersion, suspended.Continuation.Version)

	repo.mu.Lock()
	require.Len(t, repo.runs, 1)
	var childRunID primitive.ObjectID
	for id, run := range repo.runs {
		childRunID = id
		require.Equal(t, WorkflowRunStatusSuspended, run.Status)
		require.Equal(t, call.RunContext.RunID, run.ParentRunID)
		require.Equal(t, call.ActionID, run.ParentActionID)
	}
	repo.mu.Unlock()

	result, err := executor.ResumeTool(context.Background(), agentRuntime.ToolResumeRequest{
		Call:          call,
		Continuation:  suspended.Continuation,
		HumanResponse: "twitter-clone",
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.Content)
	var structured map[string]interface{}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &structured))
	require.Equal(t, childRunID.Hex(), structured["workflow_run_id"])
	require.Equal(t, WorkflowRunStatusSuccess, structured["status"])

	repo.mu.Lock()
	require.Len(t, repo.runs, 1)
	require.Equal(t, WorkflowRunStatusSuccess, repo.runs[childRunID].Status)
	require.Equal(t, call.RunContext.RunID, repo.runs[childRunID].ParentRunID)
	require.Equal(t, call.ActionID, repo.runs[childRunID].ParentActionID)
	repo.mu.Unlock()

	replayed, err := executor.ResumeTool(context.Background(), agentRuntime.ToolResumeRequest{
		Call:          call,
		Continuation:  suspended.Continuation,
		HumanResponse: "twitter-clone",
	})
	require.NoError(t, err)
	require.Equal(t, result.Content, replayed.Content)
	repo.mu.Lock()
	require.Len(t, repo.runs, 1)
	repo.mu.Unlock()

	tamperedCall := call
	tamperedCall.ActionID = "different-action"
	_, err = executor.ResumeTool(context.Background(), agentRuntime.ToolResumeRequest{
		Call:          tamperedCall,
		Continuation:  suspended.Continuation,
		HumanResponse: "twitter-clone",
	})
	require.Error(t, err)
}

func TestPublishedWorkflowToolBridgesChildWriteApprovalBackToParentAgent(t *testing.T) {
	var writeCalls int
	registry := workflowTool.NewRegistry()
	require.NoError(t, registry.RegisterHandler(
		workflowTool.ToolSpec{
			Name: "GovernedWrite", Description: "perform one governed write",
			InputSchema: json.RawMessage(`{"type":"object","additionalProperties":true}`),
			Category:    workflowTool.CategoryWrite, Permission: workflowTool.PermissionAuthenticated,
			Approval:    workflowTool.ApprovalRequired,
			Idempotency: workflowTool.IdempotencyPolicy{Required: true},
			Timeout:     time.Second,
		},
		workflowTool.HandlerFunc(func(
			_ context.Context,
			_ map[string]interface{},
		) (map[string]interface{}, error) {
			writeCalls++
			return map[string]interface{}{"content": "governed write complete"}, nil
		}),
	))
	definition := dsl.WorkflowDSL{
		Name: "Governed write workflow",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{
				ID: "write", Type: "tool",
				Properties: json.RawMessage(`{"tool_name":"GovernedWrite"}`),
			},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-write", Source: "start", Target: "write"},
			{ID: "write-end", Source: "write", Target: "end"},
		},
	}
	rawDSL, err := json.Marshal(definition)
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 42, Name: definition.Name, DSLJSON: string(rawDSL),
	}
	workflowRepo := newApprovalWorkflowRepositoryFake(workflow)
	workflowRepo.currentRevision.DSLHash = workflowAsToolDSLHash(string(rawDSL))
	repo := &workflowAsToolAgentRepository{
		approvalWorkflowRepositoryFake: workflowRepo,
		dialogues:                      &assistRuntimeRepository{},
	}
	governedExecutor := workflowTool.NewExecutor(
		registry,
		workflowTool.WithApprovalGate(NewPersistentApprovalGate(workflowRepo, time.Hour)),
		workflowTool.WithIdempotencyStore(NewPersistentToolResultStore(workflowRepo)),
	)
	publicationStore := newMemoryWorkflowToolPublicationStore()
	runStore := &memoryAgentExecutionRunStore{}
	capabilities, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWorkflowCapability())
	require.NoError(t, err)
	service := NewAgentService(
		"http://127.0.0.1:1/v1",
		"test",
		"default-model",
		"127.0.0.1:1",
		repo,
		nil,
		nil,
		WithWorkflowToolExecutor(governedExecutor),
		WithWorkflowToolPublications(publicationStore, true, 20, time.Second),
		WithAgentCapabilityCatalog(capabilities),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithUnifiedAgentApprovalRecovery(true),
		WithAgentRunRecovery(testAgentCheckpointCipher(t), 64*1024, time.Minute),
	)
	defer service.Close()

	publication, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		workflow.ID.Hex(),
		PublishWorkflowToolInput{Description: "Run one governed write workflow."},
	)
	require.NoError(t, err)
	model := &workflowApprovalRuntimeModel{toolName: publication.ToolName}
	service.runtimeRunner = agentRuntime.NewReActRunner(
		model,
		&mcpRuntimeToolExecutor{service: service},
		nil,
		agentRuntime.WithTokenCounter(workflowBudgetTokenCounter{count: 1}),
		agentRuntime.WithCostEstimator(workflowBudgetCostEstimator{microsPerToken: 1}),
	)

	suspended, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "run the governed workflow",
		PreferredCapabilityIDs: []string{CapabilityWorkflowRun},
	})
	require.NoError(t, err)
	require.Equal(t, string(agentRuntime.RunStatusApprovalRequired), suspended.RunStatus)
	require.Zero(t, writeCalls)
	require.NotNil(t, runStore.run)
	require.Equal(t, repository.AgentExecutionRunApprovalRequired, runStore.run.Status)
	require.Equal(t, repository.AgentExecutionResumeDelegatedApproval, runStore.run.PendingResumeKind)

	approvals, total, err := service.ListToolApprovals(
		context.Background(),
		42,
		repository.ToolApprovalStatusPending,
		1,
		20,
	)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	approval := approvals[0]
	require.Equal(t, approval.ID, runStore.run.ApprovalRequestID)

	workflowRepo.mu.Lock()
	require.Len(t, workflowRepo.runs, 1)
	var childRun *repository.WorkflowRunRecord
	for _, candidate := range workflowRepo.runs {
		copy := *candidate
		childRun = &copy
	}
	workflowRepo.mu.Unlock()
	require.NotNil(t, childRun)
	require.Equal(t, WorkflowRunStatusSuspended, childRun.Status)
	require.Equal(t, runStore.run.ID, childRun.ParentRunID)

	approved, err := service.DecideToolApproval(
		context.Background(),
		42,
		approval.ID,
		repository.ToolApprovalStatusApproved,
		"",
		approval.Revision,
	)
	require.NoError(t, err)
	grant, err := service.IssueWorkflowResumeGrant(
		context.Background(),
		42,
		approved.ID,
		childRun.Revision,
	)
	require.NoError(t, err)

	parentRevision := runStore.run.Revision
	resumed, err := service.ResumeAgentExecutionRun(
		context.Background(),
		ResumeAgentExecutionRequest{
			UserID: 42, RunID: runStore.run.ID, ExpectedRevision: parentRevision,
			ApprovalID: approved.ID, ResumeToken: grant.ResumeToken,
		},
	)
	require.NoError(t, err)
	require.Equal(t, "The governed workflow completed.", resumed.Response)
	require.Equal(t, string(agentRuntime.RunStatusCompleted), resumed.RunStatus)
	require.Equal(t, 1, writeCalls)
	require.Equal(t, repository.AgentExecutionRunCompleted, runStore.run.Status)
	require.Equal(t, 2, model.calls)

	workflowRepo.mu.Lock()
	require.Equal(t, WorkflowRunStatusSuccess, workflowRepo.runs[childRun.ID].Status)
	workflowRepo.mu.Unlock()

	_, err = service.ResumeAgentExecutionRun(
		context.Background(),
		ResumeAgentExecutionRequest{
			UserID: 42, RunID: runStore.run.ID, ExpectedRevision: runStore.run.Revision,
			ApprovalID: approved.ID, ResumeToken: grant.ResumeToken,
		},
	)
	require.Error(t, err)
	require.Equal(t, 1, writeCalls)
}

func TestWorkflowToolPublicationRejectsUnsafeNodeTypes(t *testing.T) {
	writeProperties := json.RawMessage(`{"tool_name":"DangerWrite"}`)
	tests := []struct {
		name       string
		unsafeNode dsl.NodeDSL
	}{
		{name: "wait", unsafeNode: dsl.NodeDSL{ID: "unsafe", Type: "wait"}},
		{name: "nested agent", unsafeNode: dsl.NodeDSL{ID: "unsafe", Type: "agent"}},
		{name: "write tool", unsafeNode: dsl.NodeDSL{ID: "unsafe", Type: "tool", Properties: writeProperties}},
		{
			name: "recursive workflow",
			unsafeNode: dsl.NodeDSL{
				ID: "unsafe", Type: "tool",
				Properties: json.RawMessage(
					`{"tool_name":"workflow_` + primitive.NewObjectID().Hex() + `"}`,
				),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := workflowTool.NewRegistry()
			require.NoError(t, registry.RegisterHandler(
				workflowTool.ToolSpec{
					Name: "DangerWrite", Description: "write",
					InputSchema: json.RawMessage(`{"type":"object"}`),
					Category:    workflowTool.CategoryWrite, Approval: workflowTool.ApprovalRequired,
					Permission: workflowTool.PermissionAuthenticated, Timeout: time.Second,
					Idempotency: workflowTool.IdempotencyPolicy{Required: true},
				},
				workflowTool.HandlerFunc(func(
					context.Context,
					map[string]interface{},
				) (map[string]interface{}, error) {
					return map[string]interface{}{}, nil
				}),
			))
			definition := dsl.WorkflowDSL{
				Name: "unsafe",
				Nodes: []dsl.NodeDSL{
					{ID: "start", Type: "start"},
					test.unsafeNode,
					{ID: "end", Type: "end"},
				},
				Edges: []dsl.EdgeDSL{
					{ID: "start-unsafe", Source: "start", Target: "unsafe"},
					{ID: "unsafe-end", Source: "unsafe", Target: "end"},
				},
			}
			service, repo, _ := newWorkflowAsToolTestService(t, 42, definition, registry)
			_, err := service.PublishWorkflowTool(
				context.Background(),
				42,
				repo.workflow.ID.Hex(),
				PublishWorkflowToolInput{},
			)
			require.Error(t, err)
			require.True(t, errors.Is(err, ErrWorkflowNotPublishable), "error = %v", err)
		})
	}
}

func TestWorkflowToolPublicationRejectsNonIdempotentWriteWithApprovalBridge(t *testing.T) {
	registry := workflowTool.NewRegistry()
	require.NoError(t, registry.RegisterHandler(
		workflowTool.ToolSpec{
			Name: "NonIdempotentWrite", Description: "write without replay protection",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Category:    workflowTool.CategoryWrite, Permission: workflowTool.PermissionAuthenticated,
			Approval: workflowTool.ApprovalRequired, Timeout: time.Second,
		},
		workflowTool.HandlerFunc(func(
			context.Context,
			map[string]interface{},
		) (map[string]interface{}, error) {
			return map[string]interface{}{}, nil
		}),
	))
	definition := dsl.WorkflowDSL{
		Name: "non-idempotent write",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "write", Type: "tool", Properties: json.RawMessage(`{"tool_name":"NonIdempotentWrite"}`)},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-write", Source: "start", Target: "write"},
			{ID: "write-end", Source: "write", Target: "end"},
		},
	}
	service, repo, _ := newWorkflowAsToolTestService(t, 42, definition, registry)
	service.recoverableAgentRuns = true
	service.unifiedAgentApprovalRecovery = true
	service.agentExecutionRunStore = &memoryAgentExecutionRunStore{}
	service.agentCheckpointCipher = testAgentCheckpointCipher(t)

	_, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		PublishWorkflowToolInput{},
	)
	require.ErrorIs(t, err, ErrWorkflowNotPublishable)
	require.ErrorContains(t, err, "idempotency")
}

func TestWorkflowToolPublicationRejectsNonHumanOrUserTokenWait(t *testing.T) {
	tests := []struct {
		name       string
		properties json.RawMessage
	}{
		{
			name:       "external callback",
			properties: json.RawMessage(`{"resume_mode":"external_callback","reason":"callback"}`),
		},
		{
			name:       "user supplied token",
			properties: json.RawMessage(`{"resume_mode":"human_input","reason":"Choose scope","resume_token":"unsafe"}`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := dsl.WorkflowDSL{
				Name: "invalid wait",
				Nodes: []dsl.NodeDSL{
					{ID: "start", Type: "start"},
					{ID: "wait", Type: "wait", Properties: test.properties},
					{ID: "end", Type: "end"},
				},
				Edges: []dsl.EdgeDSL{
					{ID: "start-wait", Source: "start", Target: "wait"},
					{ID: "wait-end", Source: "wait", Target: "end"},
				},
			}
			service, repo, _ := newWorkflowAsToolTestService(
				t,
				42,
				definition,
				workflowTool.NewRegistry(),
			)
			_, err := service.PublishWorkflowTool(
				context.Background(),
				42,
				repo.workflow.ID.Hex(),
				PublishWorkflowToolInput{},
			)
			require.ErrorIs(t, err, ErrWorkflowNotPublishable)
		})
	}
}

func TestRunAgentWorkflowCapabilityUsesOnlyPublishedTenantTools(t *testing.T) {
	definition := dsl.WorkflowDSL{
		Name: "Knowledge digest",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
	}
	rawDSL, err := json.Marshal(definition)
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 42, Name: definition.Name,
		DSLJSON: string(rawDSL),
	}
	workflowRepo := newApprovalWorkflowRepositoryFake(workflow)
	workflowRepo.currentRevision.DSLHash = workflowAsToolDSLHash(string(rawDSL))
	repo := &workflowAsToolAgentRepository{
		approvalWorkflowRepositoryFake: workflowRepo,
		dialogues:                      &assistRuntimeRepository{},
	}
	toolName := workflowRuntimeToolName(workflow.ID)
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "The published workflow completed.",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "workflow-action", Type: agentRuntime.ActionToolCall, Name: toolName,
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "workflow-action", Name: toolName, Content: "completed",
				StructuredContent: json.RawMessage(`{"schema":"workflow.run.v1","status":"success"}`),
			}},
		}},
	}}
	catalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWorkflowCapability())
	require.NoError(t, err)
	store := newMemoryWorkflowToolPublicationStore()
	service := NewAgentService(
		"http://127.0.0.1:1/v1",
		"test",
		"default-model",
		"127.0.0.1:1",
		repo,
		nil,
		nil,
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithWorkflowToolPublications(store, true, 20, time.Second),
	)
	defer service.Close()

	_, err = service.PublishWorkflowTool(
		context.Background(),
		42,
		workflow.ID.Hex(),
		PublishWorkflowToolInput{Description: "Build a read-only knowledge digest."},
	)
	require.NoError(t, err)

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID:                 42,
		Content:                "run my workflow for this topic",
		PreferredCapabilityIDs: []string{CapabilityWorkflowRun},
	})
	require.NoError(t, err)
	require.Equal(t, ExecutionProfileRuntimeWorkflow, result.ExecutionProfile)
	require.Equal(t, "The published workflow completed.", result.Response)
	require.Equal(t, agentRuntime.ModeWorkflow, runner.request.Context.Mode)
	require.Equal(t, profileUnifiedWorkflow, runner.request.Context.AgentProfileID)
	require.Len(t, runner.request.Tools, 1)
	require.Equal(t, toolName, runner.request.Tools[0].Name)
	require.Equal(t, agentRuntime.ToolCategoryRead, runner.request.Tools[0].Category)
	require.Len(t, result.ToolActivities, 1)
	require.Len(t, repo.dialogues.saved, 2)
}

func TestWorkflowToolInputSchemaRejectsReferencesAndReservedIdentity(t *testing.T) {
	t.Parallel()

	require.ErrorContains(
		t,
		validateWorkflowToolInputSchema(
			`{"type":"object","properties":{"payload":{"$ref":"#/definitions/payload"}}}`,
		),
		"references are not supported",
	)
	require.ErrorContains(
		t,
		validateWorkflowToolInputSchema(
			`{"type":"object","properties":{"user_id":{"type":"integer"}}}`,
		),
		"reserved",
	)
	require.ErrorContains(
		t,
		validateWorkflowToolInputSchema(
			`{"type":"object","properties":{"parent_run_id":{"type":"string"}}}`,
		),
		"reserved",
	)
	require.Equal(
		t,
		[]string{"alpha", "zeta"},
		workflowToolInputFields(
			`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}`,
		),
	)
}

func TestPublishWorkflowToolRequiresFeatureFlag(t *testing.T) {
	service, repo, store := newWorkflowAsToolTestService(
		t,
		42,
		dsl.WorkflowDSL{
			Name:  "Disabled publication",
			Nodes: []dsl.NodeDSL{{ID: "start", Type: "start"}, {ID: "end", Type: "end"}},
			Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
		},
		workflowTool.NewRegistry(),
	)
	service.workflowAsToolEnabled = false

	_, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		PublishWorkflowToolInput{},
	)
	require.ErrorIs(t, err, ErrWorkflowAsToolDisabled)

	publications, err := store.ListActiveWorkflowToolPublications(context.Background(), 42, 20)
	require.NoError(t, err)
	require.Empty(t, publications)
}

func newWorkflowAsToolTestService(
	t *testing.T,
	userID uint64,
	definition dsl.WorkflowDSL,
	registry *workflowTool.ToolRegistry,
) (*AgentService, *approvalWorkflowRepositoryFake, *memoryWorkflowToolPublicationStore) {
	t.Helper()
	rawDSL, err := json.Marshal(definition)
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: userID,
		Name: definition.Name, DSLJSON: string(rawDSL),
	}
	repo := newApprovalWorkflowRepositoryFake(workflow)
	repo.currentRevision.DSLHash = workflowAsToolDSLHash(repo.currentRevision.DSLJSON)
	store := newMemoryWorkflowToolPublicationStore()
	service := &AgentService{
		repo:                         repo,
		workflowToolExecutor:         workflowTool.NewExecutor(registry),
		workflowToolPublicationStore: store,
		workflowAsToolEnabled:        true,
		workflowToolCatalogLimit:     defaultWorkflowToolCatalogLimit,
		workflowToolTimeout:          time.Second,
	}
	return service, repo, store
}

func workflowAsToolDSLHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func workflowToolPublicationStoreKey(userID uint64, workflowID primitive.ObjectID) string {
	return strconv.FormatUint(userID, 10) + ":" + workflowID.Hex()
}

func cloneWorkflowToolPublication(
	publication *repository.WorkflowToolPublication,
) *repository.WorkflowToolPublication {
	if publication == nil {
		return nil
	}
	copy := *publication
	return &copy
}
