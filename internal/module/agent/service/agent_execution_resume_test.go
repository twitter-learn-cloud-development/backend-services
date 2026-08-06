package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	agentCredential "twitter-clone/internal/module/agent/credential"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type agentApprovalRepositoryFake struct {
	repository.AgentRepository
	repository.ToolApprovalRepository
}

type resumableRuntimeRunnerFake struct {
	runCalls    int
	resumeCalls int
	resume      agentRuntime.ResumeRequest
}

type toolContinuationRuntimeRunnerFake struct {
	resumeCalls int
	resume      agentRuntime.ResumeRequest
}

func (r *toolContinuationRuntimeRunnerFake) Run(
	_ context.Context,
	request agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	action := agentRuntime.Action{
		ID: "workflow-action-1", Type: agentRuntime.ActionToolCall,
		Name:      "workflow_507f1f77bcf86cd799439011",
		Arguments: json.RawMessage(`{"user_input":"inspect"}`),
	}
	continuation := &agentRuntime.ToolContinuation{
		Version: workflowToolContinuationVersion,
		Prompt:  "Which repository should be inspected?",
		State:   json.RawMessage(`{"workflow_resume_token":"secret-child-token"}`),
	}
	messages := append([]agentRuntime.Message(nil), request.Messages...)
	messages = append(messages, agentRuntime.Message{
		Role: agentRuntime.RoleAssistant, Actions: []agentRuntime.Action{action},
	})
	return agentRuntime.RunResult{
		Context: request.Context, Status: agentRuntime.RunStatusAwaitingHuman,
		Messages: messages,
		Steps: []agentRuntime.Step{{
			Index:   1,
			Actions: []agentRuntime.Action{action},
			Observations: []agentRuntime.Observation{{
				ActionID: action.ID, Name: action.Name, IsError: true,
			}},
		}},
		PendingAction:           &action,
		PendingResumeKind:       agentRuntime.ResumeKindHumanResponse,
		PendingToolContinuation: continuation,
	}, nil
}

func (r *toolContinuationRuntimeRunnerFake) Resume(
	_ context.Context,
	request agentRuntime.ResumeRequest,
) (agentRuntime.RunResult, error) {
	r.resumeCalls++
	r.resume = request
	return agentRuntime.RunResult{
		Context:     request.Checkpoint.Context,
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "The workflow inspected twitter-clone.",
		Messages: append(
			append([]agentRuntime.Message(nil), request.Checkpoint.Messages...),
			agentRuntime.Message{
				Role:    agentRuntime.RoleAssistant,
				Content: "The workflow inspected twitter-clone.",
			},
		),
		Steps: append(
			append([]agentRuntime.Step(nil), request.Checkpoint.Steps...),
			agentRuntime.Step{Index: 2, Actions: []agentRuntime.Action{{
				ID: "final-2", Type: agentRuntime.ActionFinalAnswer,
				Content: "The workflow inspected twitter-clone.",
			}}},
		),
	}, nil
}

func (r *resumableRuntimeRunnerFake) Run(
	_ context.Context,
	request agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	r.runCalls++
	action := agentRuntime.Action{
		ID: "question-1", Type: agentRuntime.ActionAskHuman, Content: "Which repository scope?",
	}
	messages := append([]agentRuntime.Message(nil), request.Messages...)
	messages = append(messages, agentRuntime.Message{
		Role: agentRuntime.RoleAssistant, Content: action.Content, Actions: []agentRuntime.Action{action},
	})
	return agentRuntime.RunResult{
		Context: request.Context, Status: agentRuntime.RunStatusAwaitingHuman,
		Messages: messages, Steps: []agentRuntime.Step{{Index: 1, Actions: []agentRuntime.Action{action}}},
		PendingAction: &action, Usage: agentRuntime.TokenUsage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14},
	}, nil
}

func (r *resumableRuntimeRunnerFake) Resume(
	_ context.Context,
	request agentRuntime.ResumeRequest,
) (agentRuntime.RunResult, error) {
	r.resumeCalls++
	r.resume = request
	messages := append([]agentRuntime.Message(nil), request.Checkpoint.Messages...)
	messages = append(messages,
		agentRuntime.Message{Role: agentRuntime.RoleUser, Content: request.HumanResponse},
		agentRuntime.Message{Role: agentRuntime.RoleAssistant, Content: "Repository scope selected."},
	)
	steps := append([]agentRuntime.Step(nil), request.Checkpoint.Steps...)
	steps = append(steps, agentRuntime.Step{Index: 2, Actions: []agentRuntime.Action{{
		ID: "final-2", Type: agentRuntime.ActionFinalAnswer, Content: "Repository scope selected.",
	}}})
	usage := request.Checkpoint.Usage
	usage.Add(agentRuntime.TokenUsage{InputTokens: 8, OutputTokens: 3, TotalTokens: 11})
	return agentRuntime.RunResult{
		Context: request.Checkpoint.Context, Status: agentRuntime.RunStatusCompleted,
		FinalAnswer: "Repository scope selected.", Messages: messages, Steps: steps, Usage: usage,
	}, nil
}

func testAgentCheckpointCipher(t *testing.T) agentCredential.SecretCipher {
	t.Helper()
	cipher, err := agentCredential.NewAESGCMCipher("checkpoint-v1", map[string][]byte{
		"checkpoint-v1": bytes.Repeat([]byte{9}, 32),
	})
	if err != nil {
		t.Fatalf("NewAESGCMCipher() error = %v", err)
	}
	return cipher
}

func TestAgentExecutionRunPersistsEncryptedToolContinuationAndHumanResume(t *testing.T) {
	dialogueRepo := &assistRuntimeRepository{}
	runStore := &memoryAgentExecutionRunStore{}
	runner := &toolContinuationRuntimeRunnerFake{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		dialogueRepo, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithAgentRunRecovery(testAgentCheckpointCipher(t), 64*1024, time.Minute),
	)
	defer service.Close()

	suspended, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "Help me inspect the repository scope",
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if suspended.RunStatus != UnifiedAgentRunStatusAwaitingHuman ||
		suspended.Response != "Which repository should be inspected?" ||
		suspended.ApprovalState.Action != string(agentRuntime.ActionAskHuman) {
		t.Fatalf("RunAgent() suspended result = %+v", suspended)
	}
	if runStore.run == nil ||
		runStore.run.PendingActionType != string(agentRuntime.ActionToolCall) ||
		runStore.run.PendingResumeKind != repository.AgentExecutionResumeHuman ||
		!runStore.run.ResumeSupported ||
		strings.Contains(runStore.run.CheckpointCiphertext, "secret-child-token") {
		t.Fatalf("persisted tool continuation run = %+v", runStore.run)
	}
	view := agentExecutionRunViewAt(runStore.run, time.Now())
	if view.PendingActionType != string(agentRuntime.ActionAskHuman) {
		t.Fatalf("sanitized run view = %+v", view)
	}

	resumed, err := service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: 42, RunID: suspended.RunID, ExpectedRevision: suspended.ApprovalState.Revision,
		HumanResponse: "twitter-clone",
	})
	if err != nil {
		t.Fatalf("ResumeAgentExecutionRun() error = %v", err)
	}
	if resumed.RunStatus != UnifiedAgentRunStatusCompleted ||
		resumed.Response != "The workflow inspected twitter-clone." ||
		runner.resumeCalls != 1 ||
		runner.resume.Checkpoint.PendingToolContinuation == nil ||
		runner.resume.HumanResponse != "twitter-clone" {
		t.Fatalf("resumed result/request = %+v/%+v", resumed, runner.resume)
	}
	if runStore.run.Status != repository.AgentExecutionRunCompleted ||
		runStore.run.PendingResumeKind != "" ||
		runStore.run.CheckpointCiphertext != "" {
		t.Fatalf("completed tool continuation run = %+v", runStore.run)
	}
}

func TestAgentExecutionRunEncryptedCheckpointAndHumanResume(t *testing.T) {
	dialogueRepo := &assistRuntimeRepository{}
	runStore := &memoryAgentExecutionRunStore{}
	runner := &resumableRuntimeRunnerFake{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		dialogueRepo, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithAgentRunRecovery(testAgentCheckpointCipher(t), 64*1024, time.Minute),
	)
	defer service.Close()

	suspended, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "Help me choose a scope",
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if suspended.RunStatus != UnifiedAgentRunStatusAwaitingHuman ||
		suspended.ApprovalState.Revision != 2 || suspended.ApprovalState.Status != AgentApprovalStatusInputRequired {
		t.Fatalf("RunAgent() suspended result = %+v", suspended)
	}
	if runStore.run == nil || !runStore.run.ResumeSupported || runStore.run.CheckpointCiphertext == "" ||
		runStore.run.CheckpointVersion != agentRuntime.ReActCheckpointVersion ||
		strings.Contains(runStore.run.CheckpointCiphertext, "Which repository scope?") {
		t.Fatalf("persisted checkpoint = %+v", runStore.run)
	}

	resumed, err := service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: 42, RunID: suspended.RunID, ExpectedRevision: suspended.ApprovalState.Revision,
		HumanResponse: "repository",
	})
	if err != nil {
		t.Fatalf("ResumeAgentExecutionRun() error = %v", err)
	}
	if resumed.RunStatus != UnifiedAgentRunStatusCompleted || resumed.Response != "Repository scope selected." ||
		resumed.ApprovalState.Revision != 4 {
		t.Fatalf("ResumeAgentExecutionRun() result = %+v", resumed)
	}
	if runner.resumeCalls != 1 || runner.resume.HumanResponse != "repository" ||
		runner.resume.Checkpoint.Context.RunID != suspended.RunID {
		t.Fatalf("runner resume request = %+v", runner.resume)
	}
	if runStore.run.Status != repository.AgentExecutionRunCompleted || runStore.run.ResumeSupported ||
		runStore.run.CheckpointCiphertext != "" || runStore.run.ResumeAttemptID != "" {
		t.Fatalf("completed run = %+v", runStore.run)
	}
	if len(dialogueRepo.saved) != 4 || dialogueRepo.saved[2].Content != "repository" ||
		dialogueRepo.saved[3].Content != "Repository scope selected." {
		t.Fatalf("persisted dialogue messages = %+v", dialogueRepo.saved)
	}
}

func TestAgentExecutionRunApprovalGrantAndResumeConsumesBoundToken(t *testing.T) {
	const qualifiedName = "mcp_server.mutate"
	approvalRepo := newApprovalWorkflowRepositoryFake(nil)
	dialogueRepo := &assistRuntimeRepository{}
	repo := &agentApprovalRepositoryFake{
		AgentRepository:        dialogueRepo,
		ToolApprovalRepository: approvalRepo,
	}
	runStore := &memoryAgentExecutionRunStore{}
	runner := &resumableRuntimeRunnerFake{}
	mcpStore := &externalMCPRuntimeStore{
		connection: externalmcp.Connection{
			ID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Status: externalmcp.ConnectionStatusActive, DiscoveryStatus: externalmcp.DiscoveryStatusReady,
			ActiveSnapshotID: "mcpsnap_1",
			ToolPolicies: []externalmcp.ToolPolicy{{
				SnapshotID: "mcpsnap_1", ToolName: "mutate", QualifiedName: qualifiedName,
				Category: externalmcp.ToolCategoryRisky, Enabled: true,
			}},
		},
		snapshot: externalmcp.ToolSchemaSnapshot{
			ID: "mcpsnap_1", ConnectionID: "mcpconn_1", UserID: 42, ServerID: "mcp_server",
			Tools: []externalmcp.ToolSchema{{
				Name: "mutate", QualifiedName: qualifiedName,
				InputSchemaJSON: `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`,
			}},
		},
	}
	manager := externalmcp.NewManager(
		mcpStore, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(&externalMCPRuntimeCaller{}),
	)
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithUnifiedAgentApprovalRecovery(true),
		WithAgentRunRecovery(testAgentCheckpointCipher(t), 64*1024, time.Minute),
		WithExternalMCPEnabled(true),
		WithExternalMCPManager(manager),
	)
	defer service.Close()

	profile, err := service.resolveAgentProfile(context.Background(), profileUnifiedExternalMCPGoverned, 42)
	if err != nil {
		t.Fatalf("resolveAgentProfile() error = %v", err)
	}
	approvalID := primitive.NewObjectID()
	dialogueID := primitive.NewObjectID()
	action := agentRuntime.Action{
		ID: "action-1", Type: agentRuntime.ActionToolCall, Name: qualifiedName,
		Arguments: json.RawMessage(`{"value":"update"}`),
	}
	run := &repository.AgentExecutionRun{
		ID: "run-approval-1", UserID: 42, DialogueID: dialogueID.Hex(),
		ExecutionProfile: ExecutionProfileRuntimeExternalMCP,
		CapabilityIDs:    []string{CapabilityExternalMCP},
		Model:            "test-model",
		AgentProfileID:   profile.ID, AgentProfileVersion: profile.Version,
		PromptTemplateID: profile.Prompt.ID, PromptTemplateVersion: profile.Prompt.Version,
		Status: repository.AgentExecutionRunApprovalRequired, Revision: 2,
		StateVersion: repository.AgentExecutionStateVersion, ResumeSupported: true,
		PendingActionType: string(agentRuntime.ActionToolCall), PendingActionName: qualifiedName,
		PendingActionID: action.ID, ApprovalRequestID: approvalID.Hex(),
		ApprovalInputDigest: "input-digest", ApprovalIdempotencyKey: "run-approval-1:action-1:mcp_server.mutate",
		ApprovalExpiresAt: time.Now().Add(time.Minute), StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	runtimeRequest := agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID: run.ID, UserID: run.UserID, Mode: agentRuntime.ModeConsult,
			AgentProfileID: profile.ID, AgentProfileVersion: profile.Version,
			PromptTemplateID: profile.Prompt.ID, PromptTemplateVersion: profile.Prompt.Version,
		},
		Model: run.Model,
	}
	runtimeResult := agentRuntime.RunResult{
		Context: runtimeRequest.Context, Status: agentRuntime.RunStatusApprovalRequired,
		ApprovalID: approvalID.Hex(), PendingAction: &action,
		Messages: []agentRuntime.Message{{Role: agentRuntime.RoleAssistant, Actions: []agentRuntime.Action{action}}},
		Steps: []agentRuntime.Step{{
			Index: 1, Actions: []agentRuntime.Action{action},
			Observations: []agentRuntime.Observation{{ActionID: action.ID, IsError: true, Content: "approval required"}},
		}},
	}
	sealed, err := service.sealAgentRunCheckpoint(run, runtimeRequest, runtimeResult)
	if err != nil {
		t.Fatalf("sealAgentRunCheckpoint() error = %v", err)
	}
	run.CheckpointVersion = sealed.Version
	run.CheckpointKeyID = sealed.KeyID
	run.CheckpointNonce = sealed.Nonce
	run.CheckpointCiphertext = sealed.Ciphertext
	run.CheckpointDigest = sealed.Digest
	run.CheckpointSizeBytes = sealed.SizeBytes
	runStore.run = run
	approvalRepo.approvals[approvalID] = &repository.ToolApprovalRequest{
		ID: approvalID, UserID: 42, RunID: run.ID, StepID: action.ID, ToolName: qualifiedName,
		Source: string(workflowTool.SourceRuntime), Category: string(agentRuntime.ToolCategoryRisky),
		Status: repository.ToolApprovalStatusApproved, InputDigest: run.ApprovalInputDigest,
		IdempotencyKey: run.ApprovalIdempotencyKey, Revision: 2, ExpiresAt: run.ApprovalExpiresAt,
	}

	grant, err := service.IssueAgentResumeGrant(context.Background(), 42, approvalID.Hex(), run.Revision)
	if err != nil {
		t.Fatalf("IssueAgentResumeGrant() error = %v", err)
	}
	if grant.ResumeToken == "" || grant.Run.Revision != 3 ||
		runStore.run.ResumeTokenHash == "" || runStore.run.ResumeTokenHash == grant.ResumeToken {
		t.Fatalf("resume grant = %+v, stored token hash = %q", grant, runStore.run.ResumeTokenHash)
	}
	if _, err := service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: 42, RunID: run.ID, ExpectedRevision: grant.Run.Revision,
		ApprovalID: approvalID.Hex(), ResumeToken: "wrong-token",
	}); !errors.Is(err, repository.ErrAgentExecutionRunConflict) || runner.resumeCalls != 0 {
		t.Fatalf("wrong token resume error/calls = %v/%d", err, runner.resumeCalls)
	}

	resumed, err := service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: 42, RunID: run.ID, ExpectedRevision: grant.Run.Revision,
		ApprovalID: approvalID.Hex(), ResumeToken: grant.ResumeToken,
	})
	if err != nil {
		t.Fatalf("ResumeAgentExecutionRun() error = %v", err)
	}
	if resumed.RunStatus != UnifiedAgentRunStatusCompleted || runner.resume.ApprovalID != approvalID.Hex() {
		t.Fatalf("resumed result/request = %+v/%+v", resumed, runner.resume)
	}
	if runStore.run.Status != repository.AgentExecutionRunCompleted || runStore.run.ResumeTokenHash != "" ||
		runStore.run.ApprovalRequestID != "" || runStore.run.CheckpointCiphertext != "" {
		t.Fatalf("completed approval run = %+v", runStore.run)
	}
	if len(dialogueRepo.saved) != 1 || dialogueRepo.saved[0].Role != repository.RoleAssistant {
		t.Fatalf("approval resume persisted messages = %+v", dialogueRepo.saved)
	}
	if _, err := service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: 42, RunID: run.ID, ExpectedRevision: grant.Run.Revision,
		ApprovalID: approvalID.Hex(), ResumeToken: grant.ResumeToken,
	}); err == nil || runner.resumeCalls != 1 {
		t.Fatalf("replayed token resume error/calls = %v/%d", err, runner.resumeCalls)
	}
}

func TestRuntimeToolApprovalRejectionTerminatesAgentRun(t *testing.T) {
	approvalID := primitive.NewObjectID()
	approvalRepo := newApprovalWorkflowRepositoryFake(nil)
	approvalRepo.approvals[approvalID] = &repository.ToolApprovalRequest{
		ID: approvalID, UserID: 42, RunID: "run-rejected-1", StepID: "action-1",
		ToolName: "mcp_server.mutate", Source: string(workflowTool.SourceRuntime),
		Category: string(agentRuntime.ToolCategoryRisky), Status: repository.ToolApprovalStatusPending,
		Revision: 1, ExpiresAt: time.Now().Add(time.Minute),
	}
	runStore := &memoryAgentExecutionRunStore{run: &repository.AgentExecutionRun{
		ID: "run-rejected-1", UserID: 42, Status: repository.AgentExecutionRunApprovalRequired,
		Revision: 2, ResumeSupported: true, ApprovalRequestID: approvalID.Hex(),
		PendingActionType: string(agentRuntime.ActionToolCall), PendingActionName: "mcp_server.mutate",
		PendingActionID: "action-1", CheckpointCiphertext: "encrypted",
	}}
	service := &AgentService{
		repo: &agentApprovalRepositoryFake{
			AgentRepository:        &assistRuntimeRepository{},
			ToolApprovalRepository: approvalRepo,
		},
		agentExecutionRunStore: runStore,
	}

	view, err := service.DecideToolApproval(
		context.Background(), 42, approvalID.Hex(), repository.ToolApprovalStatusRejected, "not allowed", 1,
	)
	if err != nil {
		t.Fatalf("DecideToolApproval() error = %v", err)
	}
	if view.Status != repository.ToolApprovalStatusRejected ||
		runStore.run.Status != repository.AgentExecutionRunFailed ||
		runStore.run.FailureCode != "approval_rejected" || runStore.run.ResumeSupported ||
		runStore.run.CheckpointCiphertext != "" || runStore.run.ApprovalRequestID != "" {
		t.Fatalf("rejected approval/run = %+v/%+v", view, runStore.run)
	}
}

func TestAgentExecutionRunClaimRejectsDuplicateAndStaleAttempt(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store := &memoryAgentExecutionRunStore{run: &repository.AgentExecutionRun{
		ID: "run-1", UserID: 42, Status: repository.AgentExecutionRunAwaitingHuman,
		Revision: 2, ResumeSupported: true, CheckpointCiphertext: "ciphertext",
	}}
	first, err := store.ClaimAgentExecutionRun(context.Background(), repository.AgentExecutionRunClaim{
		RunID: "run-1", UserID: 42, ExpectedRevision: 2, AttemptID: "attempt-1",
		LeaseDuration: time.Minute, ClaimedAt: now,
	})
	if err != nil {
		t.Fatalf("first ClaimAgentExecutionRun() error = %v", err)
	}
	if _, err := store.ClaimAgentExecutionRun(context.Background(), repository.AgentExecutionRunClaim{
		RunID: "run-1", UserID: 42, ExpectedRevision: 2, AttemptID: "attempt-duplicate",
		LeaseDuration: time.Minute, ClaimedAt: now,
	}); !errors.Is(err, repository.ErrAgentExecutionRunConflict) {
		t.Fatalf("duplicate claim error = %v", err)
	}
	second, err := store.ClaimAgentExecutionRun(context.Background(), repository.AgentExecutionRunClaim{
		RunID: "run-1", UserID: 42, ExpectedRevision: first.Revision, AttemptID: "attempt-2",
		LeaseDuration: time.Minute, ClaimedAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("expired ClaimAgentExecutionRun() error = %v", err)
	}
	_, err = store.CommitAgentExecutionRun(context.Background(), repository.AgentExecutionRunCommit{
		RunID: "run-1", UserID: 42, ExpectedRevision: second.Revision,
		ExpectedResumeAttemptID: "attempt-1", Status: repository.AgentExecutionRunCompleted,
	})
	if !errors.Is(err, repository.ErrAgentExecutionRunConflict) {
		t.Fatalf("stale attempt commit error = %v", err)
	}
}

func TestAgentExecutionRunViewReopensExpiredResumeLease(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	run := &repository.AgentExecutionRun{
		ID: "run-1", UserID: 42, Status: repository.AgentExecutionRunRunning,
		Revision: 3, ResumeSupported: true, PendingActionType: string(agentRuntime.ActionAskHuman),
		ResumeAttemptID: "attempt-1", ResumeLeaseUntil: now.Add(-time.Second),
	}

	expired := agentExecutionRunViewAt(run, now)
	if expired.Status != string(repository.AgentExecutionRunAwaitingHuman) ||
		!expired.ResumeSupported || expired.Revision != 3 {
		t.Fatalf("expired resume view = %+v", expired)
	}

	run.ResumeLeaseUntil = now.Add(time.Minute)
	active := agentExecutionRunViewAt(run, now)
	if active.Status != string(repository.AgentExecutionRunRunning) || active.ResumeSupported {
		t.Fatalf("active resume view = %+v", active)
	}
}

func TestAgentRunCheckpointRejectsSensitiveToolArguments(t *testing.T) {
	service := &AgentService{
		agentCheckpointCipher: testAgentCheckpointCipher(t), agentCheckpointMaxBytes: 64 * 1024,
	}
	action := agentRuntime.Action{
		ID: "question-1", Type: agentRuntime.ActionAskHuman, Content: "Continue?",
		Arguments: []byte(`{"api_key":"must-not-persist"}`),
	}
	_, err := service.sealAgentRunCheckpoint(
		&repository.AgentExecutionRun{ID: "run-1", UserID: 42},
		agentRuntime.RunRequest{Model: "test-model"},
		agentRuntime.RunResult{
			Context: agentRuntime.RunContext{RunID: "run-1", UserID: 42},
			Status:  agentRuntime.RunStatusAwaitingHuman, PendingAction: &action,
			Messages: []agentRuntime.Message{{Role: agentRuntime.RoleAssistant, Actions: []agentRuntime.Action{action}}},
			Steps:    []agentRuntime.Step{{Index: 1, Actions: []agentRuntime.Action{action}}},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "sensitive key") {
		t.Fatalf("sealAgentRunCheckpoint() error = %v", err)
	}
}

func TestOpenAgentRunCheckpointClassifiesDecryptFailure(t *testing.T) {
	oldCipher, err := agentCredential.NewAESGCMCipher("old", map[string][]byte{
		"old": bytes.Repeat([]byte{1}, 32),
	})
	if err != nil {
		t.Fatalf("NewAESGCMCipher(old) error = %v", err)
	}
	currentCipher, err := agentCredential.NewAESGCMCipher("current", map[string][]byte{
		"current": bytes.Repeat([]byte{2}, 32),
	})
	if err != nil {
		t.Fatalf("NewAESGCMCipher(current) error = %v", err)
	}
	run := &repository.AgentExecutionRun{ID: "run-1", UserID: 42, ResumeSupported: true}
	secret, err := oldCipher.Encrypt([]byte(`{"version":"react.v1"}`), agentRunCheckpointAAD(run))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	run.CheckpointKeyID = secret.KeyID
	run.CheckpointNonce = secret.Nonce
	run.CheckpointCiphertext = secret.Ciphertext
	run.CheckpointSizeBytes = len(`{"version":"react.v1"}`)

	service := &AgentService{agentCheckpointCipher: currentCipher}
	_, err = service.openAgentRunCheckpoint(run)
	if !errors.Is(err, ErrAgentRunCheckpointInvalid) ||
		strings.Contains(strings.ToLower(err.Error()), "cipher") {
		t.Fatalf("openAgentRunCheckpoint() error = %v", err)
	}
}
