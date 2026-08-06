package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"twitter-clone/internal/module/agent/repository"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

const (
	defaultToolApprovalTTL        = 15 * time.Minute
	defaultToolExecutionLease     = 5 * time.Minute
	defaultWorkflowResumeGrantTTL = 5 * time.Minute
)

type PersistentApprovalGate struct {
	repo  repository.ToolApprovalRepository
	ttl   time.Duration
	lease time.Duration
	now   func() time.Time
}

func NewPersistentApprovalGate(repo repository.ToolApprovalRepository, ttl time.Duration) *PersistentApprovalGate {
	if ttl <= 0 {
		ttl = defaultToolApprovalTTL
	}
	return &PersistentApprovalGate{
		repo: repo, ttl: ttl, lease: defaultToolExecutionLease, now: time.Now,
	}
}

func (g *PersistentApprovalGate) Authorize(ctx context.Context, check workflowTool.ApprovalCheck) (workflowTool.ApprovalGrant, error) {
	if g == nil || g.repo == nil {
		return workflowTool.ApprovalGrant{}, workflowTool.ErrApprovalRequired
	}
	if check.Identity.UserID == 0 || check.RunID == "" || check.StepID == "" {
		return workflowTool.ApprovalGrant{}, workflowTool.ErrApprovalRequired
	}
	digest := workflowTool.DigestInputs(check.Inputs)
	match := repository.ToolApprovalMatch{
		UserID: check.Identity.UserID, RunID: check.RunID, StepID: check.StepID,
		ToolName: check.Tool.Name, InputDigest: digest, IdempotencyKey: check.IdempotencyKey,
	}
	attemptID := workflowTool.NewAttemptID()
	claimed, err := g.repo.ClaimApprovedToolApproval(ctx, match, attemptID, g.now().Add(g.lease))
	if err == nil {
		return workflowTool.ApprovalGrant{
			ApprovalID: claimed.ID.Hex(), AttemptID: attemptID, IdempotencyKey: claimed.IdempotencyKey,
		}, nil
	}
	if !errors.Is(err, repository.ErrToolApprovalUnavailable) {
		return workflowTool.ApprovalGrant{}, fmt.Errorf("claim tool approval: %w", err)
	}

	request, err := g.repo.CreateOrGetToolApproval(ctx, &repository.ToolApprovalRequest{
		UserID: check.Identity.UserID, RunID: check.RunID, StepID: check.StepID,
		ToolName: check.Tool.Name, Source: string(check.Source), Category: string(check.Tool.Category),
		RedactedInputs: workflowTool.RedactInputs(check.Inputs, check.Tool.SensitiveFields),
		InputDigest:    digest, IdempotencyKey: check.IdempotencyKey,
		Reason:    fmt.Sprintf("%s tool %s requires human approval", check.Tool.Category, check.Tool.Name),
		ExpiresAt: g.now().Add(g.ttl),
	})
	if err != nil {
		return workflowTool.ApprovalGrant{}, fmt.Errorf("persist tool approval request: %w", err)
	}
	if !request.ExpiresAt.IsZero() && !request.ExpiresAt.After(g.now()) {
		return workflowTool.ApprovalGrant{}, errors.New("tool approval expired")
	}
	switch request.Status {
	case repository.ToolApprovalStatusPending:
		return workflowTool.ApprovalGrant{}, &workflowTool.ApprovalPendingError{ApprovalID: request.ID.Hex()}
	case repository.ToolApprovalStatusApproved, repository.ToolApprovalStatusExecuting:
		return workflowTool.ApprovalGrant{}, workflowTool.ErrAlreadyExecuting
	case repository.ToolApprovalStatusRejected:
		return workflowTool.ApprovalGrant{}, fmt.Errorf("tool approval was rejected: %s", request.Reason)
	case repository.ToolApprovalStatusExpired:
		return workflowTool.ApprovalGrant{}, errors.New("tool approval expired")
	case repository.ToolApprovalStatusConsumed:
		return workflowTool.ApprovalGrant{}, errors.New("tool approval was already consumed")
	default:
		return workflowTool.ApprovalGrant{}, fmt.Errorf("unsupported tool approval status %q", request.Status)
	}
}

func (g *PersistentApprovalGate) Complete(ctx context.Context, grant workflowTool.ApprovalGrant) error {
	if g == nil || g.repo == nil || grant.ApprovalID == "" {
		return nil
	}
	id, err := primitive.ObjectIDFromHex(grant.ApprovalID)
	if err != nil {
		return fmt.Errorf("invalid approval grant id: %w", err)
	}
	return g.repo.CompleteToolApproval(ctx, id, grant.AttemptID)
}

func (g *PersistentApprovalGate) Release(ctx context.Context, grant workflowTool.ApprovalGrant, _ error) error {
	if g == nil || g.repo == nil || grant.ApprovalID == "" {
		return nil
	}
	id, err := primitive.ObjectIDFromHex(grant.ApprovalID)
	if err != nil {
		return fmt.Errorf("invalid approval grant id: %w", err)
	}
	return g.repo.ReleaseToolApproval(ctx, id, grant.AttemptID)
}

type ToolApprovalView struct {
	ID             string
	UserID         uint64
	RunID          string
	StepID         string
	ToolName       string
	Source         string
	Category       string
	Status         string
	RedactedInputs map[string]interface{}
	IdempotencyKey string
	Reason         string
	Revision       int64
	CreatedAt      time.Time
	ExpiresAt      time.Time
	DecidedAt      time.Time
}

type WorkflowResumeGrantView struct {
	Run         *repository.WorkflowRunRecord
	ApprovalID  string
	ResumeToken string
	ExpiresAt   time.Time
}

func (s *AgentService) ListToolApprovals(ctx context.Context, userID uint64, status string, page, pageSize int) ([]*ToolApprovalView, int64, error) {
	repo, err := s.toolApprovalRepository()
	if err != nil {
		return nil, 0, err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "" && !validToolApprovalStatus(status) {
		return nil, 0, errors.New("invalid tool approval status")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	requests, total, err := repo.ListToolApprovals(ctx, userID, status, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	views := make([]*ToolApprovalView, 0, len(requests))
	for _, request := range requests {
		views = append(views, toolApprovalView(request))
	}
	return views, total, nil
}

func (s *AgentService) DecideToolApproval(ctx context.Context, userID uint64, approvalID, decision, reason string, expectedRevision int64) (*ToolApprovalView, error) {
	repo, err := s.toolApprovalRepository()
	if err != nil {
		return nil, err
	}
	id, err := primitive.ObjectIDFromHex(strings.TrimSpace(approvalID))
	if err != nil {
		return nil, fmt.Errorf("invalid approval_id: %w", err)
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != repository.ToolApprovalStatusApproved && decision != repository.ToolApprovalStatusRejected {
		return nil, errors.New("decision must be approved or rejected")
	}
	if expectedRevision <= 0 {
		return nil, errors.New("expected_revision must be positive")
	}
	updated, err := repo.DecideToolApproval(ctx, id, userID, decision, strings.TrimSpace(reason), expectedRevision)
	if err != nil {
		return nil, err
	}
	if decision == repository.ToolApprovalStatusRejected && updated.RunID != "" {
		if updated.Source == string(workflowTool.SourceRuntime) {
			approvalStore, ok := s.agentExecutionRunStore.(repository.AgentExecutionApprovalRunStore)
			if !ok {
				return nil, ErrAgentExecutionRunStoreUnavailable
			}
			if _, rejectErr := approvalStore.RejectAgentExecutionRunApproval(
				ctx, updated.RunID, userID, updated.ID.Hex(), time.Now(),
			); rejectErr != nil && !errors.Is(rejectErr, repository.ErrAgentExecutionRunConflict) {
				return nil, rejectErr
			}
			return toolApprovalView(updated), nil
		}
		if runID, parseErr := primitive.ObjectIDFromHex(updated.RunID); parseErr == nil {
			workflowRun, _ := s.repo.GetWorkflowRun(ctx, runID, userID)
			if strings.HasSuffix(updated.StepID, "$compensate") {
				if compensationRepo, ok := s.repo.(repository.WorkflowCompensationRepository); ok {
					rejectReason := updated.Reason
					if rejectReason == "" {
						rejectReason = "workflow compensation was rejected by the user"
					}
					if err := compensationRepo.RejectWorkflowCompensation(ctx, runID, userID, updated.ID, rejectReason); err != nil && !errors.Is(err, repository.ErrWorkflowCompensationUnavailable) {
						return nil, err
					}
				}
			}
			if resumeRepo, ok := s.repo.(repository.WorkflowResumeRepository); ok {
				rejectReason := updated.Reason
				if rejectReason == "" {
					rejectReason = "tool execution was rejected by the user"
				}
				if err := resumeRepo.RejectWorkflowRunForApproval(ctx, runID, userID, updated.ID, rejectReason); err != nil && !errors.Is(err, repository.ErrWorkflowResumeConflict) {
					return nil, err
				}
			}
			if workflowRun != nil &&
				workflowRun.InvocationSource == string(workflowTool.SourceRuntime) &&
				strings.TrimSpace(workflowRun.ParentRunID) != "" {
				approvalStore, ok := s.agentExecutionRunStore.(repository.AgentExecutionApprovalRunStore)
				if !ok {
					return nil, ErrAgentExecutionRunStoreUnavailable
				}
				if _, rejectErr := approvalStore.RejectAgentExecutionRunApproval(
					ctx,
					workflowRun.ParentRunID,
					userID,
					updated.ID.Hex(),
					time.Now(),
				); rejectErr != nil && !errors.Is(rejectErr, repository.ErrAgentExecutionRunConflict) {
					return nil, rejectErr
				}
			}
		}
	}
	return toolApprovalView(updated), nil
}

func (s *AgentService) IssueWorkflowResumeGrant(
	ctx context.Context,
	userID uint64,
	approvalID string,
	expectedRunRevision int64,
) (*WorkflowResumeGrantView, error) {
	if userID == 0 {
		return nil, errors.New("user_id is required")
	}
	if expectedRunRevision <= 0 {
		return nil, errors.New("expected_run_revision must be positive")
	}
	approvalOID, err := primitive.ObjectIDFromHex(strings.TrimSpace(approvalID))
	if err != nil {
		return nil, fmt.Errorf("invalid approval_id: %w", err)
	}
	approvalRepo, err := s.toolApprovalRepository()
	if err != nil {
		return nil, err
	}
	approval, err := approvalRepo.GetToolApproval(ctx, approvalOID, userID)
	if err != nil {
		return nil, err
	}
	if approval.Status != repository.ToolApprovalStatusApproved {
		return nil, errors.New("tool approval is not approved")
	}
	now := time.Now()
	if !approval.ExpiresAt.IsZero() && !approval.ExpiresAt.After(now) {
		return nil, errors.New("tool approval expired")
	}
	runOID, err := primitive.ObjectIDFromHex(approval.RunID)
	if err != nil {
		return nil, fmt.Errorf("approval run_id is invalid: %w", err)
	}
	run, err := s.repo.GetWorkflowRun(ctx, runOID, userID)
	if err != nil {
		return nil, err
	}
	if run.Status != WorkflowRunStatusSuspended || run.ApprovalRequestID != approvalOID {
		return nil, repository.ErrWorkflowResumeGrantConflict
	}
	if run.Revision != expectedRunRevision {
		return nil, repository.ErrWorkflowResumeGrantConflict
	}
	resumeToken, err := newWorkflowResumeToken()
	if err != nil {
		return nil, err
	}
	expiresAt := now.Add(defaultWorkflowResumeGrantTTL)
	if !approval.ExpiresAt.IsZero() && approval.ExpiresAt.Before(expiresAt) {
		expiresAt = approval.ExpiresAt
	}
	if !expiresAt.After(now) {
		return nil, errors.New("workflow resume grant expiry is invalid")
	}
	grantRepo, ok := s.repo.(repository.WorkflowResumeGrantRepository)
	if !ok {
		return nil, errors.New("workflow resume grant repository is not available")
	}
	updatedRun, err := grantRepo.IssueWorkflowResumeGrant(
		ctx, runOID, userID, approvalOID, expectedRunRevision,
		hashWorkflowResumeToken(resumeToken), now, expiresAt,
	)
	if err != nil {
		return nil, err
	}
	return &WorkflowResumeGrantView{
		Run: updatedRun, ApprovalID: approvalOID.Hex(), ResumeToken: resumeToken, ExpiresAt: expiresAt,
	}, nil
}

func (s *AgentService) toolApprovalRepository() (repository.ToolApprovalRepository, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("agent repository is not initialized")
	}
	repo, ok := s.repo.(repository.ToolApprovalRepository)
	if !ok {
		return nil, errors.New("tool approval repository is not available")
	}
	return repo, nil
}

func toolApprovalView(request *repository.ToolApprovalRequest) *ToolApprovalView {
	if request == nil {
		return nil
	}
	return &ToolApprovalView{
		ID: request.ID.Hex(), UserID: request.UserID, RunID: request.RunID, StepID: request.StepID,
		ToolName: request.ToolName, Source: request.Source, Category: request.Category, Status: request.Status,
		RedactedInputs: request.RedactedInputs, IdempotencyKey: request.IdempotencyKey,
		Reason: request.Reason, Revision: request.Revision, CreatedAt: request.CreatedAt,
		ExpiresAt: request.ExpiresAt, DecidedAt: request.DecidedAt,
	}
}

func validToolApprovalStatus(status string) bool {
	switch status {
	case repository.ToolApprovalStatusPending, repository.ToolApprovalStatusApproved,
		repository.ToolApprovalStatusRejected, repository.ToolApprovalStatusExecuting,
		repository.ToolApprovalStatusConsumed, repository.ToolApprovalStatusExpired:
		return true
	default:
		return false
	}
}
