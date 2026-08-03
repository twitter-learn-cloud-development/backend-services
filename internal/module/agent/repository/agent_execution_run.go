package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentStrategy "twitter-clone/internal/module/agent/strategy"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	CollectionAgentExecutionRuns          = "agent_execution_runs"
	AgentExecutionStateVersion            = int64(2)
	AgentExecutionResumeHuman             = "human_response"
	AgentExecutionResumeApproval          = "tool_approval"
	AgentExecutionResumeDelegatedApproval = "delegated_tool_approval"
)

type AgentExecutionRunStatus string

const (
	AgentExecutionRunRunning          AgentExecutionRunStatus = "running"
	AgentExecutionRunCompleted        AgentExecutionRunStatus = "completed"
	AgentExecutionRunAwaitingHuman    AgentExecutionRunStatus = "awaiting_human"
	AgentExecutionRunApprovalRequired AgentExecutionRunStatus = "approval_required"
	AgentExecutionRunFailed           AgentExecutionRunStatus = "failed"
	AgentExecutionRunCanceled         AgentExecutionRunStatus = "canceled"
)

var (
	ErrAgentExecutionRunNotFound = errors.New("agent execution run not found")
	ErrAgentExecutionRunConflict = errors.New("agent execution run state conflict")
	ErrAgentDraftNotPublishable  = errors.New("agent execution run has no publishable draft")
)

// AgentExecutionRun is the authoritative lifecycle record for a model-driven
// Agent run. Trace collections remain evidence stores and Workflow runs retain
// their independent DAG-specific state model.
type AgentExecutionRun struct {
	ID                     string                  `bson:"_id" json:"id"`
	UserID                 uint64                  `bson:"user_id" json:"user_id"`
	DialogueID             string                  `bson:"dialogue_id,omitempty" json:"dialogue_id,omitempty"`
	ExecutionProfile       string                  `bson:"execution_profile" json:"execution_profile"`
	CapabilityIDs          []string                `bson:"capability_ids" json:"capability_ids"`
	SkillID                string                  `bson:"skill_id,omitempty" json:"skill_id,omitempty"`
	SkillVersion           string                  `bson:"skill_version,omitempty" json:"skill_version,omitempty"`
	TaskTemplateID         string                  `bson:"task_template_id,omitempty" json:"task_template_id,omitempty"`
	TaskTemplateRevision   int64                   `bson:"task_template_revision,omitempty" json:"task_template_revision,omitempty"`
	ExecutionStrategyPlan  *agentStrategy.Plan     `bson:"execution_strategy_plan,omitempty" json:"execution_strategy_plan,omitempty"`
	Mode                   string                  `bson:"mode,omitempty" json:"mode,omitempty"`
	Model                  string                  `bson:"model,omitempty" json:"model,omitempty"`
	AgentProfileID         string                  `bson:"agent_profile_id,omitempty" json:"agent_profile_id,omitempty"`
	AgentProfileVersion    string                  `bson:"agent_profile_version,omitempty" json:"agent_profile_version,omitempty"`
	PromptTemplateID       string                  `bson:"prompt_template_id,omitempty" json:"prompt_template_id,omitempty"`
	PromptTemplateVersion  string                  `bson:"prompt_template_version,omitempty" json:"prompt_template_version,omitempty"`
	Status                 AgentExecutionRunStatus `bson:"status" json:"status"`
	Revision               int64                   `bson:"revision" json:"revision"`
	StateVersion           int64                   `bson:"state_version" json:"state_version"`
	InputDigest            string                  `bson:"input_digest" json:"input_digest"`
	ResultDigest           string                  `bson:"result_digest,omitempty" json:"result_digest,omitempty"`
	PublishableDraft       bool                    `bson:"publishable_draft" json:"publishable_draft"`
	PublishedTweetID       uint64                  `bson:"published_tweet_id,omitempty" json:"published_tweet_id,omitempty"`
	DraftPublishedAt       time.Time               `bson:"draft_published_at,omitempty" json:"draft_published_at,omitempty"`
	FailureCode            string                  `bson:"failure_code,omitempty" json:"failure_code,omitempty"`
	FailureDigest          string                  `bson:"failure_digest,omitempty" json:"failure_digest,omitempty"`
	PendingActionType      string                  `bson:"pending_action_type,omitempty" json:"pending_action_type,omitempty"`
	PendingActionName      string                  `bson:"pending_action_name,omitempty" json:"pending_action_name,omitempty"`
	PendingActionID        string                  `bson:"pending_action_id,omitempty" json:"pending_action_id,omitempty"`
	PendingResumeKind      string                  `bson:"pending_resume_kind,omitempty" json:"pending_resume_kind,omitempty"`
	ApprovalRequestID      string                  `bson:"approval_request_id,omitempty" json:"approval_request_id,omitempty"`
	ApprovalInputDigest    string                  `bson:"approval_input_digest,omitempty" json:"-"`
	ApprovalIdempotencyKey string                  `bson:"approval_idempotency_key,omitempty" json:"-"`
	ApprovalExpiresAt      time.Time               `bson:"approval_expires_at,omitempty" json:"approval_expires_at,omitempty"`
	StepCount              int                     `bson:"step_count" json:"step_count"`
	InputTokens            int                     `bson:"input_tokens" json:"input_tokens"`
	OutputTokens           int                     `bson:"output_tokens" json:"output_tokens"`
	TotalTokens            int                     `bson:"total_tokens" json:"total_tokens"`
	UsageEstimated         bool                    `bson:"usage_estimated" json:"usage_estimated"`
	EstimatedCostMicros    int64                   `bson:"estimated_cost_micros" json:"estimated_cost_micros"`
	CostEstimated          bool                    `bson:"cost_estimated" json:"cost_estimated"`
	PricingVersion         string                  `bson:"pricing_version,omitempty" json:"pricing_version,omitempty"`
	MaxSteps               int                     `bson:"max_steps" json:"max_steps"`
	MaxTotalTokens         int                     `bson:"max_total_tokens" json:"max_total_tokens"`
	MaxEstimatedCostMicros int64                   `bson:"max_estimated_cost_micros" json:"max_estimated_cost_micros"`
	AccountingVersion      string                  `bson:"accounting_version,omitempty" json:"accounting_version,omitempty"`
	ResumeSupported        bool                    `bson:"resume_supported" json:"resume_supported"`
	CheckpointVersion      string                  `bson:"checkpoint_version,omitempty" json:"-"`
	CheckpointKeyID        string                  `bson:"checkpoint_key_id,omitempty" json:"-"`
	CheckpointNonce        string                  `bson:"checkpoint_nonce,omitempty" json:"-"`
	CheckpointCiphertext   string                  `bson:"checkpoint_ciphertext,omitempty" json:"-"`
	CheckpointDigest       string                  `bson:"checkpoint_digest,omitempty" json:"-"`
	CheckpointSizeBytes    int                     `bson:"checkpoint_size_bytes,omitempty" json:"-"`
	ResumeAttemptID        string                  `bson:"resume_attempt_id,omitempty" json:"-"`
	ResumeLeaseUntil       time.Time               `bson:"resume_lease_until,omitempty" json:"-"`
	ResumeClaimedAt        time.Time               `bson:"resume_claimed_at,omitempty" json:"-"`
	ResumeTokenHash        string                  `bson:"resume_token_hash,omitempty" json:"-"`
	ResumeGrantIssuedAt    time.Time               `bson:"resume_grant_issued_at,omitempty" json:"-"`
	ResumeGrantExpiresAt   time.Time               `bson:"resume_grant_expires_at,omitempty" json:"-"`
	ResumeCount            int                     `bson:"resume_count" json:"resume_count"`
	StartedAt              time.Time               `bson:"started_at" json:"started_at"`
	UpdatedAt              time.Time               `bson:"updated_at" json:"updated_at"`
	SuspendedAt            time.Time               `bson:"suspended_at,omitempty" json:"suspended_at,omitempty"`
	FinishedAt             time.Time               `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
}

type AgentExecutionRunCommit struct {
	RunID                   string
	UserID                  uint64
	ExpectedRevision        int64
	DialogueID              string
	Status                  AgentExecutionRunStatus
	Mode                    string
	Model                   string
	AgentProfileID          string
	AgentProfileVersion     string
	PromptTemplateID        string
	PromptTemplateVersion   string
	ResultDigest            string
	PublishableDraft        bool
	FailureCode             string
	FailureDigest           string
	PendingActionType       string
	PendingActionName       string
	PendingActionID         string
	PendingResumeKind       string
	ApprovalRequestID       string
	ApprovalInputDigest     string
	ApprovalIdempotencyKey  string
	ApprovalExpiresAt       time.Time
	StepCount               int
	InputTokens             int
	OutputTokens            int
	TotalTokens             int
	UsageEstimated          bool
	EstimatedCostMicros     int64
	CostEstimated           bool
	PricingVersion          string
	MaxSteps                int
	MaxTotalTokens          int
	MaxEstimatedCostMicros  int64
	AccountingVersion       string
	ResumeSupported         bool
	CheckpointVersion       string
	CheckpointKeyID         string
	CheckpointNonce         string
	CheckpointCiphertext    string
	CheckpointDigest        string
	CheckpointSizeBytes     int
	ExpectedResumeAttemptID string
	UpdatedAt               time.Time
}

type AgentExecutionRunClaim struct {
	RunID             string
	UserID            uint64
	ExpectedRevision  int64
	AttemptID         string
	LeaseDuration     time.Duration
	ClaimedAt         time.Time
	PendingStatus     AgentExecutionRunStatus
	ApprovalRequestID string
	ResumeTokenHash   string
	DelegatedApproval bool
}

type AgentExecutionResumeGrant struct {
	RunID             string
	UserID            uint64
	ApprovalRequestID string
	ExpectedRevision  int64
	TokenHash         string
	IssuedAt          time.Time
	ExpiresAt         time.Time
}

type AgentExecutionApprovalRunStore interface {
	IssueAgentExecutionResumeGrant(context.Context, AgentExecutionResumeGrant) (*AgentExecutionRun, error)
	RejectAgentExecutionRunApproval(context.Context, string, uint64, string, time.Time) (*AgentExecutionRun, error)
}

type AgentExecutionRunStore interface {
	CreateAgentExecutionRun(context.Context, *AgentExecutionRun) error
	CommitAgentExecutionRun(context.Context, AgentExecutionRunCommit) (*AgentExecutionRun, error)
	ClaimAgentExecutionRun(context.Context, AgentExecutionRunClaim) (*AgentExecutionRun, error)
	GetAgentExecutionRun(context.Context, string, uint64) (*AgentExecutionRun, error)
}

// AgentDraftAdoptionStore is independent from lifecycle CAS. A completed Run
// can be attributed to at most one first publish while later HTTP retries or
// alternate tweet IDs remain a replay of the same adoption fact.
type AgentDraftAdoptionStore interface {
	MarkAgentDraftPublished(
		context.Context,
		string,
		uint64,
		uint64,
		time.Time,
	) (*AgentExecutionRun, bool, error)
}

type MongoAgentExecutionRunRepository struct {
	collection *mongo.Collection
}

var _ AgentDraftAdoptionStore = (*MongoAgentExecutionRunRepository)(nil)

func NewMongoAgentExecutionRunRepository(db *mongo.Database) *MongoAgentExecutionRunRepository {
	if db == nil {
		return &MongoAgentExecutionRunRepository{}
	}
	return &MongoAgentExecutionRunRepository{collection: db.Collection(CollectionAgentExecutionRuns)}
}

func (r *MongoAgentExecutionRunRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil || r.collection == nil {
		return errors.New("agent execution run repository is unavailable")
	}
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "started_at", Value: -1}},
			Options: options.Index().SetName("idx_agent_execution_run_user_started"),
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1}, {Key: "updated_at", Value: 1},
			},
			Options: options.Index().SetName("idx_agent_execution_run_status_updated"),
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1}, {Key: "resume_lease_until", Value: 1},
			},
			Options: options.Index().SetName("idx_agent_execution_run_resume_lease"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "approval_request_id", Value: 1}},
			Options: options.Index().SetName("idx_agent_execution_run_user_approval").SetSparse(true),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1}, {Key: "dialogue_id", Value: 1}, {Key: "started_at", Value: -1},
			},
			Options: options.Index().SetName("idx_agent_execution_run_user_dialogue_started"),
		},
		{
			Keys: bson.D{
				{Key: "publishable_draft", Value: 1}, {Key: "draft_published_at", Value: 1},
				{Key: "finished_at", Value: -1},
			},
			Options: options.Index().SetName("idx_agent_execution_run_draft_adoption"),
		},
	})
	if err != nil {
		return fmt.Errorf("create agent execution run indexes failed: %w", err)
	}
	return nil
}

func (r *MongoAgentExecutionRunRepository) CreateAgentExecutionRun(
	ctx context.Context,
	run *AgentExecutionRun,
) error {
	if r == nil || r.collection == nil {
		return errors.New("agent execution run repository is unavailable")
	}
	if err := normalizeNewAgentExecutionRun(run, time.Now()); err != nil {
		return err
	}
	if _, err := r.collection.InsertOne(ctx, run); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrAgentExecutionRunConflict
		}
		return fmt.Errorf("insert agent execution run failed: %w", err)
	}
	return nil
}

func (r *MongoAgentExecutionRunRepository) CommitAgentExecutionRun(
	ctx context.Context,
	commit AgentExecutionRunCommit,
) (*AgentExecutionRun, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("agent execution run repository is unavailable")
	}
	if err := validateAgentExecutionRunCommit(commit); err != nil {
		return nil, err
	}
	now := commit.UpdatedAt
	if now.IsZero() {
		now = time.Now()
	}
	setFields := bson.M{
		"dialogue_id":               strings.TrimSpace(commit.DialogueID),
		"status":                    commit.Status,
		"mode":                      strings.TrimSpace(commit.Mode),
		"model":                     strings.TrimSpace(commit.Model),
		"agent_profile_id":          strings.TrimSpace(commit.AgentProfileID),
		"agent_profile_version":     strings.TrimSpace(commit.AgentProfileVersion),
		"prompt_template_id":        strings.TrimSpace(commit.PromptTemplateID),
		"prompt_template_version":   strings.TrimSpace(commit.PromptTemplateVersion),
		"result_digest":             strings.TrimSpace(commit.ResultDigest),
		"publishable_draft":         commit.PublishableDraft,
		"failure_code":              strings.TrimSpace(commit.FailureCode),
		"failure_digest":            strings.TrimSpace(commit.FailureDigest),
		"pending_action_type":       strings.TrimSpace(commit.PendingActionType),
		"pending_action_name":       strings.TrimSpace(commit.PendingActionName),
		"pending_action_id":         strings.TrimSpace(commit.PendingActionID),
		"pending_resume_kind":       strings.TrimSpace(commit.PendingResumeKind),
		"step_count":                commit.StepCount,
		"input_tokens":              commit.InputTokens,
		"output_tokens":             commit.OutputTokens,
		"total_tokens":              commit.TotalTokens,
		"usage_estimated":           commit.UsageEstimated,
		"estimated_cost_micros":     commit.EstimatedCostMicros,
		"cost_estimated":            commit.CostEstimated,
		"pricing_version":           strings.TrimSpace(commit.PricingVersion),
		"max_steps":                 commit.MaxSteps,
		"max_total_tokens":          commit.MaxTotalTokens,
		"max_estimated_cost_micros": commit.MaxEstimatedCostMicros,
		"accounting_version":        strings.TrimSpace(commit.AccountingVersion),
		"resume_supported":          commit.ResumeSupported,
		"updated_at":                now,
	}
	unsetFields := bson.M{
		"resume_attempt_id":       "",
		"resume_lease_until":      "",
		"resume_claimed_at":       "",
		"resume_token_hash":       "",
		"resume_grant_issued_at":  "",
		"resume_grant_expires_at": "",
	}
	if commit.ResumeSupported {
		setFields["checkpoint_version"] = strings.TrimSpace(commit.CheckpointVersion)
		setFields["checkpoint_key_id"] = strings.TrimSpace(commit.CheckpointKeyID)
		setFields["checkpoint_nonce"] = strings.TrimSpace(commit.CheckpointNonce)
		setFields["checkpoint_ciphertext"] = strings.TrimSpace(commit.CheckpointCiphertext)
		setFields["checkpoint_digest"] = strings.TrimSpace(commit.CheckpointDigest)
		setFields["checkpoint_size_bytes"] = commit.CheckpointSizeBytes
		if commit.Status == AgentExecutionRunApprovalRequired {
			setFields["approval_request_id"] = strings.TrimSpace(commit.ApprovalRequestID)
			setFields["approval_input_digest"] = strings.TrimSpace(commit.ApprovalInputDigest)
			setFields["approval_idempotency_key"] = strings.TrimSpace(commit.ApprovalIdempotencyKey)
			setFields["approval_expires_at"] = commit.ApprovalExpiresAt
		} else {
			unsetFields["approval_request_id"] = ""
			unsetFields["approval_input_digest"] = ""
			unsetFields["approval_idempotency_key"] = ""
			unsetFields["approval_expires_at"] = ""
		}
	} else {
		unsetFields["checkpoint_version"] = ""
		unsetFields["checkpoint_key_id"] = ""
		unsetFields["checkpoint_nonce"] = ""
		unsetFields["checkpoint_ciphertext"] = ""
		unsetFields["checkpoint_digest"] = ""
		unsetFields["checkpoint_size_bytes"] = ""
		unsetFields["approval_request_id"] = ""
		unsetFields["approval_input_digest"] = ""
		unsetFields["approval_idempotency_key"] = ""
		unsetFields["approval_expires_at"] = ""
	}
	if isSuspendedAgentExecutionStatus(commit.Status) {
		setFields["suspended_at"] = now
		unsetFields["finished_at"] = ""
	} else {
		setFields["finished_at"] = now
		unsetFields["suspended_at"] = ""
	}
	update := bson.M{"$set": setFields, "$inc": bson.M{"revision": 1}}
	if len(unsetFields) > 0 {
		update["$unset"] = unsetFields
	}
	var updated AgentExecutionRun
	filter := bson.M{
		"_id": commit.RunID, "user_id": commit.UserID,
		"status": AgentExecutionRunRunning, "revision": commit.ExpectedRevision,
	}
	if attemptID := strings.TrimSpace(commit.ExpectedResumeAttemptID); attemptID != "" {
		filter["resume_attempt_id"] = attemptID
	}
	err := r.collection.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	if err == nil {
		return &updated, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("commit agent execution run failed: %w", err)
	}
	if _, getErr := r.GetAgentExecutionRun(ctx, commit.RunID, commit.UserID); getErr != nil {
		return nil, getErr
	}
	return nil, ErrAgentExecutionRunConflict
}

func (r *MongoAgentExecutionRunRepository) ClaimAgentExecutionRun(
	ctx context.Context,
	claim AgentExecutionRunClaim,
) (*AgentExecutionRun, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("agent execution run repository is unavailable")
	}
	if err := validateAgentExecutionRunClaim(claim); err != nil {
		return nil, err
	}
	now := claim.ClaimedAt
	if now.IsZero() {
		now = time.Now()
	}
	leaseUntil := now.Add(claim.LeaseDuration)
	pendingStatus := claim.PendingStatus
	if pendingStatus == "" {
		pendingStatus = AgentExecutionRunAwaitingHuman
	}
	suspendedFilter := bson.M{"status": pendingStatus}
	expiredLeaseFilter := bson.M{
		"status":             AgentExecutionRunRunning,
		"resume_attempt_id":  bson.M{"$ne": ""},
		"resume_lease_until": bson.M{"$lte": now},
	}
	filter := bson.M{
		"_id": claim.RunID, "user_id": claim.UserID, "revision": claim.ExpectedRevision,
		"resume_supported":      true,
		"checkpoint_ciphertext": bson.M{"$ne": ""},
		"$or":                   bson.A{suspendedFilter, expiredLeaseFilter},
	}
	if pendingStatus == AgentExecutionRunApprovalRequired {
		filter["approval_request_id"] = strings.TrimSpace(claim.ApprovalRequestID)
		if claim.DelegatedApproval {
			filter["pending_resume_kind"] = AgentExecutionResumeDelegatedApproval
		} else {
			filter["resume_token_hash"] = strings.TrimSpace(claim.ResumeTokenHash)
			filter["resume_grant_expires_at"] = bson.M{"$gt": now}
		}
	}
	update := bson.M{
		"$set": bson.M{
			"status": AgentExecutionRunRunning, "resume_attempt_id": claim.AttemptID,
			"resume_claimed_at": now, "resume_lease_until": leaseUntil, "updated_at": now,
		},
		"$unset": bson.M{
			"suspended_at": "", "finished_at": "", "resume_token_hash": "",
			"resume_grant_issued_at": "", "resume_grant_expires_at": "",
		},
		"$inc": bson.M{"revision": 1, "resume_count": 1},
	}
	var updated AgentExecutionRun
	err := r.collection.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	if err == nil {
		return &updated, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("claim agent execution run failed: %w", err)
	}
	if _, getErr := r.GetAgentExecutionRun(ctx, claim.RunID, claim.UserID); getErr != nil {
		return nil, getErr
	}
	return nil, ErrAgentExecutionRunConflict
}

func (r *MongoAgentExecutionRunRepository) IssueAgentExecutionResumeGrant(
	ctx context.Context,
	grant AgentExecutionResumeGrant,
) (*AgentExecutionRun, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("agent execution run repository is unavailable")
	}
	if strings.TrimSpace(grant.RunID) == "" || grant.UserID == 0 ||
		strings.TrimSpace(grant.ApprovalRequestID) == "" || grant.ExpectedRevision <= 0 ||
		strings.TrimSpace(grant.TokenHash) == "" || grant.IssuedAt.IsZero() || !grant.ExpiresAt.After(grant.IssuedAt) {
		return nil, errors.New("agent execution resume grant is incomplete")
	}
	filter := bson.M{
		"_id": grant.RunID, "user_id": grant.UserID, "revision": grant.ExpectedRevision,
		"approval_request_id": grant.ApprovalRequestID, "resume_supported": true,
		"checkpoint_ciphertext": bson.M{"$ne": ""},
		"$or": bson.A{
			bson.M{"status": AgentExecutionRunApprovalRequired},
			bson.M{"status": AgentExecutionRunRunning, "resume_lease_until": bson.M{"$lte": grant.IssuedAt}},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"status":                  AgentExecutionRunApprovalRequired,
			"resume_token_hash":       grant.TokenHash,
			"resume_grant_issued_at":  grant.IssuedAt,
			"resume_grant_expires_at": grant.ExpiresAt,
			"suspended_at":            grant.IssuedAt,
			"updated_at":              grant.IssuedAt,
		},
		"$unset": bson.M{"resume_attempt_id": "", "resume_lease_until": "", "resume_claimed_at": "", "finished_at": ""},
		"$inc":   bson.M{"revision": 1},
	}
	var updated AgentExecutionRun
	err := r.collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&updated)
	if err == nil {
		return &updated, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("issue agent execution resume grant failed: %w", err)
	}
	if _, getErr := r.GetAgentExecutionRun(ctx, grant.RunID, grant.UserID); getErr != nil {
		return nil, getErr
	}
	return nil, ErrAgentExecutionRunConflict
}

func (r *MongoAgentExecutionRunRepository) RejectAgentExecutionRunApproval(
	ctx context.Context,
	runID string,
	userID uint64,
	approvalRequestID string,
	now time.Time,
) (*AgentExecutionRun, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("agent execution run repository is unavailable")
	}
	if strings.TrimSpace(runID) == "" || userID == 0 || strings.TrimSpace(approvalRequestID) == "" {
		return nil, errors.New("agent execution approval rejection identity is incomplete")
	}
	if now.IsZero() {
		now = time.Now()
	}
	filter := bson.M{
		"_id": runID, "user_id": userID, "approval_request_id": approvalRequestID,
		"$or": bson.A{
			bson.M{"status": AgentExecutionRunApprovalRequired},
			bson.M{"status": AgentExecutionRunRunning, "resume_lease_until": bson.M{"$lte": now}},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"status": AgentExecutionRunFailed, "failure_code": "approval_rejected",
			"updated_at": now, "finished_at": now, "resume_supported": false,
		},
		"$unset": bson.M{
			"pending_action_type": "", "pending_action_name": "", "pending_action_id": "",
			"pending_resume_kind": "",
			"approval_request_id": "", "approval_input_digest": "", "approval_idempotency_key": "", "approval_expires_at": "",
			"suspended_at": "", "checkpoint_version": "", "checkpoint_key_id": "", "checkpoint_nonce": "",
			"checkpoint_ciphertext": "", "checkpoint_digest": "", "checkpoint_size_bytes": "",
			"resume_attempt_id": "", "resume_lease_until": "", "resume_claimed_at": "",
			"resume_token_hash": "", "resume_grant_issued_at": "", "resume_grant_expires_at": "",
		},
		"$inc": bson.M{"revision": 1},
	}
	var updated AgentExecutionRun
	err := r.collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&updated)
	if err == nil {
		return &updated, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("reject agent execution approval failed: %w", err)
	}
	if _, getErr := r.GetAgentExecutionRun(ctx, runID, userID); getErr != nil {
		return nil, getErr
	}
	return nil, ErrAgentExecutionRunConflict
}

func (r *MongoAgentExecutionRunRepository) GetAgentExecutionRun(
	ctx context.Context,
	runID string,
	userID uint64,
) (*AgentExecutionRun, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("agent execution run repository is unavailable")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" || userID == 0 {
		return nil, errors.New("agent execution run identity is incomplete")
	}
	var run AgentExecutionRun
	err := r.collection.FindOne(ctx, bson.M{"_id": runID, "user_id": userID}).Decode(&run)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrAgentExecutionRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find agent execution run failed: %w", err)
	}
	return &run, nil
}

func (r *MongoAgentExecutionRunRepository) MarkAgentDraftPublished(
	ctx context.Context,
	runID string,
	userID uint64,
	tweetID uint64,
	publishedAt time.Time,
) (*AgentExecutionRun, bool, error) {
	if r == nil || r.collection == nil {
		return nil, false, errors.New("agent execution run repository is unavailable")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" || userID == 0 || tweetID == 0 {
		return nil, false, errors.New("agent draft adoption identity is incomplete")
	}
	if publishedAt.IsZero() {
		publishedAt = time.Now()
	}
	filter := bson.M{
		"_id": runID, "user_id": userID,
		"status": AgentExecutionRunCompleted, "publishable_draft": true,
		"$or": bson.A{
			bson.M{"published_tweet_id": bson.M{"$exists": false}},
			bson.M{"published_tweet_id": uint64(0)},
		},
	}
	var updated AgentExecutionRun
	err := r.collection.FindOneAndUpdate(
		ctx,
		filter,
		bson.M{"$set": bson.M{
			"published_tweet_id": tweetID,
			"draft_published_at": publishedAt,
		}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	if err == nil {
		return &updated, true, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, fmt.Errorf("mark agent draft published: %w", err)
	}
	existing, getErr := r.GetAgentExecutionRun(ctx, runID, userID)
	if getErr != nil {
		return nil, false, getErr
	}
	if existing.Status != AgentExecutionRunCompleted || !existing.PublishableDraft {
		return nil, false, ErrAgentDraftNotPublishable
	}
	if existing.PublishedTweetID != 0 {
		return existing, false, nil
	}
	return nil, false, ErrAgentExecutionRunConflict
}

func normalizeNewAgentExecutionRun(run *AgentExecutionRun, now time.Time) error {
	if run == nil {
		return errors.New("agent execution run is nil")
	}
	run.ID = strings.TrimSpace(run.ID)
	run.ExecutionProfile = strings.TrimSpace(run.ExecutionProfile)
	run.InputDigest = strings.TrimSpace(run.InputDigest)
	if run.ID == "" || run.UserID == 0 || run.ExecutionProfile == "" || run.InputDigest == "" {
		return errors.New("agent execution run identity and provenance are required")
	}
	if run.Status == "" {
		run.Status = AgentExecutionRunRunning
	}
	if run.Status != AgentExecutionRunRunning {
		return errors.New("new agent execution run must start in running status")
	}
	if run.Revision <= 0 {
		run.Revision = 1
	}
	if run.StateVersion <= 0 {
		run.StateVersion = AgentExecutionStateVersion
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.StartedAt
	}
	run.CapabilityIDs = append([]string(nil), run.CapabilityIDs...)
	if run.ExecutionStrategyPlan != nil {
		if err := agentStrategy.ValidatePlan(*run.ExecutionStrategyPlan); err != nil {
			return fmt.Errorf("invalid agent execution strategy plan: %w", err)
		}
		cloned := agentStrategy.ClonePlan(*run.ExecutionStrategyPlan)
		run.ExecutionStrategyPlan = &cloned
	}
	return nil
}

func validateAgentExecutionRunCommit(commit AgentExecutionRunCommit) error {
	if strings.TrimSpace(commit.RunID) == "" || commit.UserID == 0 || commit.ExpectedRevision <= 0 {
		return errors.New("agent execution run commit identity is incomplete")
	}
	if commit.StepCount < 0 || commit.InputTokens < 0 || commit.OutputTokens < 0 ||
		commit.TotalTokens < 0 || commit.EstimatedCostMicros < 0 || commit.MaxSteps < 0 ||
		commit.MaxTotalTokens < 0 || commit.MaxEstimatedCostMicros < 0 {
		return errors.New("agent execution run accounting values cannot be negative")
	}
	if version := strings.TrimSpace(commit.AccountingVersion); version != "" &&
		version != ExecutionAccountingVersion {
		return errors.New("agent execution run accounting version is unsupported")
	}
	if commit.PublishableDraft && commit.Status != AgentExecutionRunCompleted {
		return errors.New("only a completed agent execution run can contain a publishable draft")
	}
	resumeKind := strings.TrimSpace(commit.PendingResumeKind)
	if resumeKind != "" &&
		resumeKind != AgentExecutionResumeHuman &&
		resumeKind != AgentExecutionResumeApproval &&
		resumeKind != AgentExecutionResumeDelegatedApproval {
		return errors.New("agent execution run resume kind is unsupported")
	}
	switch commit.Status {
	case AgentExecutionRunCompleted, AgentExecutionRunAwaitingHuman,
		AgentExecutionRunApprovalRequired, AgentExecutionRunFailed, AgentExecutionRunCanceled:
		if commit.ResumeSupported {
			actionType := strings.TrimSpace(commit.PendingActionType)
			actionID := strings.TrimSpace(commit.PendingActionID)
			actionName := strings.TrimSpace(commit.PendingActionName)
			resumableHuman := commit.Status == AgentExecutionRunAwaitingHuman &&
				((actionType == "ask_human" && (resumeKind == "" || resumeKind == AgentExecutionResumeHuman)) ||
					(actionType == "tool_call" && resumeKind == AgentExecutionResumeHuman &&
						actionID != "" && actionName != ""))
			resumableApproval := commit.Status == AgentExecutionRunApprovalRequired &&
				actionType == "tool_call" &&
				(resumeKind == "" || resumeKind == AgentExecutionResumeApproval ||
					resumeKind == AgentExecutionResumeDelegatedApproval) &&
				actionID != "" &&
				actionName != "" &&
				strings.TrimSpace(commit.ApprovalRequestID) != "" &&
				strings.TrimSpace(commit.ApprovalInputDigest) != "" &&
				!commit.ApprovalExpiresAt.IsZero()
			if (!resumableHuman && !resumableApproval) ||
				strings.TrimSpace(commit.CheckpointVersion) == "" ||
				strings.TrimSpace(commit.CheckpointKeyID) == "" ||
				strings.TrimSpace(commit.CheckpointNonce) == "" ||
				strings.TrimSpace(commit.CheckpointCiphertext) == "" ||
				strings.TrimSpace(commit.CheckpointDigest) == "" || commit.CheckpointSizeBytes <= 0 {
				return errors.New("resumable agent execution commit checkpoint is incomplete")
			}
		}
		return nil
	default:
		return fmt.Errorf("invalid agent execution run target status %q", commit.Status)
	}
}

func validateAgentExecutionRunClaim(claim AgentExecutionRunClaim) error {
	if strings.TrimSpace(claim.RunID) == "" || claim.UserID == 0 || claim.ExpectedRevision <= 0 ||
		strings.TrimSpace(claim.AttemptID) == "" || claim.LeaseDuration <= 0 {
		return errors.New("agent execution run claim is incomplete")
	}
	if claim.PendingStatus != "" && claim.PendingStatus != AgentExecutionRunAwaitingHuman && claim.PendingStatus != AgentExecutionRunApprovalRequired {
		return errors.New("agent execution run claim status is invalid")
	}
	if claim.PendingStatus == AgentExecutionRunApprovalRequired &&
		(strings.TrimSpace(claim.ApprovalRequestID) == "" ||
			(!claim.DelegatedApproval && strings.TrimSpace(claim.ResumeTokenHash) == "")) {
		return errors.New("agent execution approval claim is incomplete")
	}
	return nil
}

func isSuspendedAgentExecutionStatus(status AgentExecutionRunStatus) bool {
	return status == AgentExecutionRunAwaitingHuman || status == AgentExecutionRunApprovalRequired
}
