package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

type memoryAgentExecutionRunStore struct {
	mu                sync.Mutex
	run               *repository.AgentExecutionRun
	createCalls       int
	commitCalls       int
	createErr         error
	commitErr         error
	commitCtxErr      error
	draftPublishCalls int
}

func (s *memoryAgentExecutionRunStore) CreateAgentExecutionRun(
	_ context.Context,
	run *repository.AgentExecutionRun,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	if s.createErr != nil {
		return s.createErr
	}
	copyRun := *run
	copyRun.CapabilityIDs = append([]string(nil), run.CapabilityIDs...)
	if run.ExecutionStrategyPlan != nil {
		cloned := agentStrategy.ClonePlan(*run.ExecutionStrategyPlan)
		copyRun.ExecutionStrategyPlan = &cloned
	}
	s.run = &copyRun
	return nil
}

func (s *memoryAgentExecutionRunStore) CommitAgentExecutionRun(
	ctx context.Context,
	commit repository.AgentExecutionRunCommit,
) (*repository.AgentExecutionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitCalls++
	s.commitCtxErr = ctx.Err()
	if s.commitErr != nil {
		return nil, s.commitErr
	}
	if s.run == nil || s.run.ID != commit.RunID || s.run.UserID != commit.UserID ||
		s.run.Revision != commit.ExpectedRevision || s.run.Status != repository.AgentExecutionRunRunning ||
		(commit.ExpectedResumeAttemptID != "" && s.run.ResumeAttemptID != commit.ExpectedResumeAttemptID) {
		return nil, repository.ErrAgentExecutionRunConflict
	}
	s.run.DialogueID = commit.DialogueID
	s.run.Status = commit.Status
	s.run.Mode = commit.Mode
	s.run.Model = commit.Model
	s.run.AgentProfileID = commit.AgentProfileID
	s.run.AgentProfileVersion = commit.AgentProfileVersion
	s.run.PromptTemplateID = commit.PromptTemplateID
	s.run.PromptTemplateVersion = commit.PromptTemplateVersion
	s.run.ResultDigest = commit.ResultDigest
	s.run.PublishableDraft = commit.PublishableDraft
	s.run.FailureCode = commit.FailureCode
	s.run.FailureDigest = commit.FailureDigest
	s.run.PendingActionType = commit.PendingActionType
	s.run.PendingActionName = commit.PendingActionName
	s.run.PendingActionID = commit.PendingActionID
	s.run.PendingResumeKind = commit.PendingResumeKind
	s.run.ApprovalRequestID = commit.ApprovalRequestID
	s.run.ApprovalInputDigest = commit.ApprovalInputDigest
	s.run.ApprovalIdempotencyKey = commit.ApprovalIdempotencyKey
	s.run.ApprovalExpiresAt = commit.ApprovalExpiresAt
	s.run.StepCount = commit.StepCount
	s.run.TotalTokens = commit.TotalTokens
	s.run.InputTokens = commit.InputTokens
	s.run.OutputTokens = commit.OutputTokens
	s.run.EstimatedCostMicros = commit.EstimatedCostMicros
	s.run.UsageEstimated = commit.UsageEstimated
	s.run.CostEstimated = commit.CostEstimated
	s.run.PricingVersion = commit.PricingVersion
	s.run.MaxSteps = commit.MaxSteps
	s.run.MaxTotalTokens = commit.MaxTotalTokens
	s.run.MaxEstimatedCostMicros = commit.MaxEstimatedCostMicros
	s.run.AccountingVersion = commit.AccountingVersion
	s.run.ResumeSupported = commit.ResumeSupported
	s.run.CheckpointVersion = commit.CheckpointVersion
	s.run.CheckpointKeyID = commit.CheckpointKeyID
	s.run.CheckpointNonce = commit.CheckpointNonce
	s.run.CheckpointCiphertext = commit.CheckpointCiphertext
	s.run.CheckpointDigest = commit.CheckpointDigest
	s.run.CheckpointSizeBytes = commit.CheckpointSizeBytes
	s.run.ResumeAttemptID = ""
	s.run.ResumeLeaseUntil = time.Time{}
	s.run.ResumeClaimedAt = time.Time{}
	s.run.ResumeTokenHash = ""
	s.run.ResumeGrantIssuedAt = time.Time{}
	s.run.ResumeGrantExpiresAt = time.Time{}
	s.run.Revision++
	copyRun := *s.run
	copyRun.CapabilityIDs = append([]string(nil), s.run.CapabilityIDs...)
	if s.run.ExecutionStrategyPlan != nil {
		cloned := agentStrategy.ClonePlan(*s.run.ExecutionStrategyPlan)
		copyRun.ExecutionStrategyPlan = &cloned
	}
	return &copyRun, nil
}

func (s *memoryAgentExecutionRunStore) MarkAgentDraftPublished(
	_ context.Context,
	runID string,
	userID uint64,
	tweetID uint64,
	publishedAt time.Time,
) (*repository.AgentExecutionRun, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.draftPublishCalls++
	if s.run == nil || s.run.ID != runID || s.run.UserID != userID ||
		s.run.Status != repository.AgentExecutionRunCompleted || !s.run.PublishableDraft {
		return nil, false, repository.ErrAgentDraftNotPublishable
	}
	recorded := s.run.PublishedTweetID == 0
	if recorded {
		s.run.PublishedTweetID = tweetID
		s.run.DraftPublishedAt = publishedAt
	}
	copyRun := *s.run
	return &copyRun, recorded, nil
}

func (s *memoryAgentExecutionRunStore) ClaimAgentExecutionRun(
	_ context.Context,
	claim repository.AgentExecutionRunClaim,
) (*repository.AgentExecutionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := claim.ClaimedAt
	if now.IsZero() {
		now = time.Now()
	}
	pendingStatus := claim.PendingStatus
	if pendingStatus == "" {
		pendingStatus = repository.AgentExecutionRunAwaitingHuman
	}
	claimable := s.run != nil && s.run.ID == claim.RunID && s.run.UserID == claim.UserID &&
		s.run.Revision == claim.ExpectedRevision && s.run.ResumeSupported &&
		s.run.CheckpointCiphertext != "" &&
		(s.run.Status == pendingStatus ||
			(s.run.Status == repository.AgentExecutionRunRunning && s.run.ResumeAttemptID != "" &&
				!s.run.ResumeLeaseUntil.After(now)))
	if claimable && pendingStatus == repository.AgentExecutionRunApprovalRequired {
		claimable = claim.ApprovalRequestID != "" && s.run.ApprovalRequestID == claim.ApprovalRequestID
		if claim.DelegatedApproval {
			claimable = claimable &&
				s.run.PendingResumeKind == repository.AgentExecutionResumeDelegatedApproval
		} else {
			claimable = claimable &&
				claim.ResumeTokenHash != "" &&
				s.run.ResumeTokenHash == claim.ResumeTokenHash &&
				s.run.ResumeGrantExpiresAt.After(now)
		}
	}
	if !claimable {
		return nil, repository.ErrAgentExecutionRunConflict
	}
	s.run.Status = repository.AgentExecutionRunRunning
	s.run.ResumeAttemptID = claim.AttemptID
	s.run.ResumeClaimedAt = now
	s.run.ResumeLeaseUntil = now.Add(claim.LeaseDuration)
	s.run.ResumeTokenHash = ""
	s.run.ResumeGrantIssuedAt = time.Time{}
	s.run.ResumeGrantExpiresAt = time.Time{}
	s.run.ResumeCount++
	s.run.Revision++
	copyRun := *s.run
	copyRun.CapabilityIDs = append([]string(nil), s.run.CapabilityIDs...)
	return &copyRun, nil
}

func (s *memoryAgentExecutionRunStore) IssueAgentExecutionResumeGrant(
	_ context.Context,
	grant repository.AgentExecutionResumeGrant,
) (*repository.AgentExecutionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil || s.run.ID != grant.RunID || s.run.UserID != grant.UserID ||
		s.run.Revision != grant.ExpectedRevision ||
		s.run.Status != repository.AgentExecutionRunApprovalRequired ||
		s.run.ApprovalRequestID != grant.ApprovalRequestID || grant.TokenHash == "" ||
		!grant.ExpiresAt.After(grant.IssuedAt) {
		return nil, repository.ErrAgentExecutionRunConflict
	}
	s.run.ResumeTokenHash = grant.TokenHash
	s.run.ResumeGrantIssuedAt = grant.IssuedAt
	s.run.ResumeGrantExpiresAt = grant.ExpiresAt
	s.run.Revision++
	copyRun := *s.run
	copyRun.CapabilityIDs = append([]string(nil), s.run.CapabilityIDs...)
	return &copyRun, nil
}

func (s *memoryAgentExecutionRunStore) RejectAgentExecutionRunApproval(
	_ context.Context,
	runID string,
	userID uint64,
	approvalID string,
	now time.Time,
) (*repository.AgentExecutionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil || s.run.ID != runID || s.run.UserID != userID ||
		s.run.Status != repository.AgentExecutionRunApprovalRequired ||
		s.run.ApprovalRequestID != approvalID {
		return nil, repository.ErrAgentExecutionRunConflict
	}
	s.run.Status = repository.AgentExecutionRunFailed
	s.run.FailureCode = "approval_rejected"
	s.run.ResumeSupported = false
	s.run.PendingActionType = ""
	s.run.PendingActionName = ""
	s.run.PendingActionID = ""
	s.run.PendingResumeKind = ""
	s.run.ApprovalRequestID = ""
	s.run.ApprovalInputDigest = ""
	s.run.ApprovalIdempotencyKey = ""
	s.run.ApprovalExpiresAt = time.Time{}
	s.run.CheckpointCiphertext = ""
	s.run.ResumeTokenHash = ""
	s.run.FinishedAt = now
	s.run.Revision++
	copyRun := *s.run
	copyRun.CapabilityIDs = append([]string(nil), s.run.CapabilityIDs...)
	return &copyRun, nil
}

func (s *memoryAgentExecutionRunStore) GetAgentExecutionRun(
	_ context.Context,
	runID string,
	userID uint64,
) (*repository.AgentExecutionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil || s.run.ID != runID || s.run.UserID != userID {
		return nil, repository.ErrAgentExecutionRunNotFound
	}
	copyRun := *s.run
	copyRun.CapabilityIDs = append([]string(nil), s.run.CapabilityIDs...)
	return &copyRun, nil
}

func TestRunAgentPersistsAuthoritativeExecutionLifecycle(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "durable reply",
		Steps: []agentRuntime.Step{{Index: 1}},
		Usage: agentRuntime.TokenUsage{
			InputTokens: 12, OutputTokens: 8, TotalTokens: 20, Estimated: true,
			EstimatedCostMicros: 45, CostEstimated: true, PricingVersion: "pricing-v1",
		},
	}}
	store := &memoryAgentExecutionRunStore{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "persist this request",
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if store.createCalls != 1 || store.commitCalls != 1 || runner.calls != 1 {
		t.Fatalf("calls create=%d commit=%d runner=%d", store.createCalls, store.commitCalls, runner.calls)
	}
	if store.run == nil || store.run.Status != repository.AgentExecutionRunCompleted {
		t.Fatalf("persisted run = %+v", store.run)
	}
	if store.run.ID != result.RunID || runner.request.Context.RunID != result.RunID {
		t.Fatalf("run IDs persisted=%q runtime=%q response=%q", store.run.ID, runner.request.Context.RunID, result.RunID)
	}
	if store.run.DialogueID != result.DialogueID || store.run.ExecutionProfile != ExecutionProfileRuntimeChat {
		t.Fatalf("persisted run routing = %+v", store.run)
	}
	if store.run.InputDigest == "" || store.run.InputDigest == "persist this request" ||
		store.run.ResultDigest == "" || store.run.TotalTokens != 20 {
		t.Fatalf("persisted run provenance = %+v", store.run)
	}
	if store.run.AccountingVersion != repository.ExecutionAccountingVersion ||
		store.run.MaxSteps <= 0 || !store.run.UsageEstimated || !store.run.CostEstimated ||
		store.run.EstimatedCostMicros != 45 || store.run.PricingVersion != "pricing-v1" {
		t.Fatalf("persisted run accounting = %+v", store.run)
	}
	if store.run.ExecutionStrategyPlan == nil ||
		store.run.ExecutionStrategyPlan.SelectedStrategy != agentStrategy.KindSingleAgent ||
		store.run.ExecutionStrategyPlan.ReasonCode != agentStrategy.ReasonSingleCapabilityScope ||
		store.run.ExecutionStrategyPlan.PlanDigest == "" ||
		result.ExecutionStrategyPlan.PlanDigest != store.run.ExecutionStrategyPlan.PlanDigest {
		t.Fatalf("persisted execution strategy = %+v, result = %+v", store.run.ExecutionStrategyPlan, result.ExecutionStrategyPlan)
	}
	if store.run.ResumeSupported {
		t.Fatal("lifecycle-only rollout must not claim resumable checkpoints")
	}
}

func TestRunAgentFailsClosedBeforeModelWhenExecutionRunCreateFails(t *testing.T) {
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "must not run",
	}}
	store := &memoryAgentExecutionRunStore{createErr: errors.New("mongo unavailable")}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
	)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{UserID: 42, Content: "hello"})
	if err == nil || !strings.Contains(err.Error(), "create agent execution run") {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if runner.calls != 0 || store.commitCalls != 0 {
		t.Fatalf("runner calls=%d commit calls=%d, want zero", runner.calls, store.commitCalls)
	}
}

func TestRunAgentFailsClosedWhenExecutionRunCommitFails(t *testing.T) {
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "uncommitted reply",
	}}
	store := &memoryAgentExecutionRunStore{commitErr: errors.New("write conflict")}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{UserID: 42, Content: "hello"})
	if err == nil || !strings.Contains(err.Error(), "commit agent execution run") {
		t.Fatalf("RunAgent() result=%+v error=%v", result, err)
	}
}

func TestRunAgentReturnsAwaitingHumanWithoutClaimingUnsupportedResume(t *testing.T) {
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusAwaitingHuman,
		PendingAction: &agentRuntime.Action{
			ID: "question-1", Type: agentRuntime.ActionAskHuman, Content: "Which scope?",
		},
	}}
	store := &memoryAgentExecutionRunStore{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{UserID: 42, Content: "help me decide"})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.RunStatus != UnifiedAgentRunStatusAwaitingHuman || result.Response != "Which scope?" ||
		result.ApprovalState.Status != AgentApprovalStatusInputRequired {
		t.Fatalf("RunAgent() result = %+v", result)
	}
	if store.run == nil || store.run.Status != repository.AgentExecutionRunAwaitingHuman ||
		store.run.PendingActionType != string(agentRuntime.ActionAskHuman) || store.run.ResumeSupported {
		t.Fatalf("persisted awaiting-human run = %+v", store.run)
	}
}

func TestRunAgentMarksPostProcessingFailureAsFailed(t *testing.T) {
	runner := &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "reply that cannot be persisted",
	}}
	store := &memoryAgentExecutionRunStore{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{saveErr: errors.New("dialogue write failed")}, nil, nil,
		WithAgentRunner(runner),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
	)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{UserID: 42, Content: "hello"})
	if err == nil || !strings.Contains(err.Error(), "persist chat conversation failed") {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if store.run == nil || store.run.Status != repository.AgentExecutionRunFailed ||
		store.run.FailureCode != "agent_execution_failed" || store.run.FailureDigest == "" {
		t.Fatalf("persisted failed run = %+v", store.run)
	}
}

func TestFinishAgentExecutionRunUsesDetachedBoundedCommitContext(t *testing.T) {
	run := &repository.AgentExecutionRun{
		ID: "run-1", UserID: 42, ExecutionProfile: ExecutionProfileRuntimeChat,
		Status: repository.AgentExecutionRunRunning, Revision: 1, InputDigest: "digest",
	}
	store := &memoryAgentExecutionRunStore{run: run}
	service := &AgentService{agentExecutionRunStore: store, recoverableAgentRuns: true}
	capture := &agentExecutionCapture{
		runID: "run-1", called: true,
		request: agentRuntime.RunRequest{Context: agentRuntime.RunContext{
			RunID: "run-1", UserID: 42, Mode: agentRuntime.ModeChat,
		}},
		result: agentRuntime.RunResult{Status: agentRuntime.RunStatusFailed},
	}
	ctx, cancel := context.WithCancel(context.WithValue(
		context.Background(), agentExecutionCaptureContextKey{}, capture,
	))
	cancel()

	if err := service.finishUnifiedAgentExecutionRun(ctx, run, nil, context.Canceled); err != nil {
		t.Fatalf("finishUnifiedAgentExecutionRun() error = %v", err)
	}
	if store.commitCtxErr != nil {
		t.Fatalf("commit context error = %v, want detached active context", store.commitCtxErr)
	}
	if store.run.Status != repository.AgentExecutionRunCanceled {
		t.Fatalf("persisted status = %s, want canceled", store.run.Status)
	}
}

func TestAgentExecutionDigestIsRunScoped(t *testing.T) {
	first := agentExecutionScopedDigest("run-1", "hello")
	second := agentExecutionScopedDigest("run-2", "hello")
	if first == "" || second == "" || first == second || strings.Contains(first, "hello") {
		t.Fatalf("run-scoped digests first=%q second=%q", first, second)
	}
}
