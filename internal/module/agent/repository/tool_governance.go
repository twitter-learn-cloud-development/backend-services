package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	CollectionToolApprovals  = "agent_tool_approvals"
	CollectionToolExecutions = "agent_tool_executions"

	ToolApprovalStatusPending   = "pending"
	ToolApprovalStatusApproved  = "approved"
	ToolApprovalStatusRejected  = "rejected"
	ToolApprovalStatusExecuting = "executing"
	ToolApprovalStatusConsumed  = "consumed"
	ToolApprovalStatusExpired   = "expired"

	ToolExecutionStatusExecuting = "executing"
	ToolExecutionStatusSucceeded = "succeeded"
	ToolExecutionStatusFailed    = "failed"
)

var (
	ErrToolApprovalNotFound        = errors.New("tool approval not found")
	ErrToolApprovalConflict        = errors.New("tool approval state conflict")
	ErrToolApprovalUnavailable     = errors.New("approved tool request is unavailable")
	ErrToolExecutionInProgress     = errors.New("tool execution is already in progress")
	ErrToolExecutionConflict       = errors.New("tool idempotency key was used with different inputs")
	ErrToolExecutionClaimInvalid   = errors.New("tool execution claim is invalid")
	ErrWorkflowResumeConflict      = errors.New("workflow resume token is invalid or already consumed")
	ErrWorkflowResumeGrantConflict = errors.New("workflow resume grant state changed")
)

type ToolApprovalRequest struct {
	ID                  primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	UserID              uint64                 `bson:"user_id" json:"user_id"`
	RunID               string                 `bson:"run_id" json:"run_id"`
	StepID              string                 `bson:"step_id" json:"step_id"`
	ToolName            string                 `bson:"tool_name" json:"tool_name"`
	Source              string                 `bson:"source" json:"source"`
	Category            string                 `bson:"category" json:"category"`
	Status              string                 `bson:"status" json:"status"`
	RedactedInputs      map[string]interface{} `bson:"redacted_inputs,omitempty" json:"redacted_inputs,omitempty"`
	InputDigest         string                 `bson:"input_digest" json:"input_digest"`
	IdempotencyKey      string                 `bson:"idempotency_key" json:"idempotency_key"`
	Reason              string                 `bson:"reason,omitempty" json:"reason,omitempty"`
	ApproverUserID      uint64                 `bson:"approver_user_id,omitempty" json:"approver_user_id,omitempty"`
	ExecutionAttemptID  string                 `bson:"execution_attempt_id,omitempty" json:"-"`
	ExecutionLeaseUntil time.Time              `bson:"execution_lease_until,omitempty" json:"-"`
	Revision            int64                  `bson:"revision" json:"revision"`
	CreatedAt           time.Time              `bson:"created_at" json:"created_at"`
	UpdatedAt           time.Time              `bson:"updated_at" json:"updated_at"`
	ExpiresAt           time.Time              `bson:"expires_at" json:"expires_at"`
	DecidedAt           time.Time              `bson:"decided_at,omitempty" json:"decided_at,omitempty"`
	ConsumedAt          time.Time              `bson:"consumed_at,omitempty" json:"consumed_at,omitempty"`
}

type ToolApprovalMatch struct {
	UserID         uint64
	RunID          string
	StepID         string
	ToolName       string
	InputDigest    string
	IdempotencyKey string
}

type ToolApprovalRepository interface {
	CreateOrGetToolApproval(ctx context.Context, request *ToolApprovalRequest) (*ToolApprovalRequest, error)
	GetToolApproval(ctx context.Context, approvalID primitive.ObjectID, userID uint64) (*ToolApprovalRequest, error)
	ListToolApprovals(ctx context.Context, userID uint64, status string, page, pageSize int) ([]*ToolApprovalRequest, int64, error)
	DecideToolApproval(ctx context.Context, approvalID primitive.ObjectID, userID uint64, decision, reason string, expectedRevision int64) (*ToolApprovalRequest, error)
	ClaimApprovedToolApproval(ctx context.Context, match ToolApprovalMatch, attemptID string, leaseUntil time.Time) (*ToolApprovalRequest, error)
	CompleteToolApproval(ctx context.Context, approvalID primitive.ObjectID, attemptID string) error
	ReleaseToolApproval(ctx context.Context, approvalID primitive.ObjectID, attemptID string) error
}

type ToolExecutionRecord struct {
	ID              primitive.ObjectID     `bson:"_id,omitempty" json:"id"`
	UserID          uint64                 `bson:"user_id" json:"user_id"`
	ToolName        string                 `bson:"tool_name" json:"tool_name"`
	IdempotencyKey  string                 `bson:"idempotency_key" json:"idempotency_key"`
	InputDigest     string                 `bson:"input_digest" json:"input_digest"`
	Status          string                 `bson:"status" json:"status"`
	AttemptID       string                 `bson:"attempt_id,omitempty" json:"-"`
	LeaseUntil      time.Time              `bson:"lease_until,omitempty" json:"-"`
	Output          map[string]interface{} `bson:"output,omitempty" json:"output,omitempty"`
	OutputReference *ToolResultReference   `bson:"output_reference,omitempty" json:"output_reference,omitempty"`
	OutputDigest    string                 `bson:"output_digest,omitempty" json:"output_digest,omitempty"`
	OutputLength    int                    `bson:"output_length,omitempty" json:"output_length,omitempty"`
	ErrorMessage    string                 `bson:"error_message,omitempty" json:"error_message,omitempty"`
	CreatedAt       time.Time              `bson:"created_at" json:"created_at"`
	UpdatedAt       time.Time              `bson:"updated_at" json:"updated_at"`
	CompletedAt     time.Time              `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
}

type ToolResultReference struct {
	Storage     string `bson:"storage" json:"storage"`
	Bucket      string `bson:"bucket" json:"bucket"`
	Key         string `bson:"key" json:"key"`
	Digest      string `bson:"digest" json:"digest"`
	Length      int    `bson:"length" json:"length"`
	ContentType string `bson:"content_type" json:"content_type"`
}

type ToolExecutionResult struct {
	Output          map[string]interface{}
	OutputReference *ToolResultReference
	Digest          string
	Length          int
}

type ToolExecutionRepository interface {
	ClaimToolExecution(ctx context.Context, record *ToolExecutionRecord) (*ToolExecutionRecord, bool, error)
	CompleteToolExecution(ctx context.Context, executionID primitive.ObjectID, attemptID string, result ToolExecutionResult) error
	FailToolExecution(ctx context.Context, executionID primitive.ObjectID, attemptID, errorMessage string) error
}

type ToolGovernanceReconcileResult struct {
	ExpiredApprovals       int64
	ReleasedApprovalLeases int64
	FailedExecutionLeases  int64
	FailedSuspendedRuns    int64
	FailedAgentRuns        int64
}

type ToolGovernanceReconcileRepository interface {
	ReconcileToolGovernance(ctx context.Context, now time.Time) (ToolGovernanceReconcileResult, error)
}

type WorkflowResumeRepository interface {
	ClaimWorkflowRunResume(ctx context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, tokenHash, attemptID string) (*WorkflowRunRecord, error)
	RejectWorkflowRunForApproval(ctx context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, reason string) error
}

type WorkflowResumeGrantRepository interface {
	IssueWorkflowResumeGrant(ctx context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, expectedRevision int64, tokenHash string, issuedAt, expiresAt time.Time) (*WorkflowRunRecord, error)
}

func (r *MongoAgentRepository) CreateOrGetToolApproval(ctx context.Context, request *ToolApprovalRequest) (*ToolApprovalRequest, error) {
	if request == nil {
		return nil, errors.New("tool approval request is nil")
	}
	now := time.Now()
	if request.ID.IsZero() {
		request.ID = primitive.NewObjectID()
	}
	request.Status = ToolApprovalStatusPending
	request.Revision = 1
	request.CreatedAt = now
	request.UpdatedAt = now
	if request.ExpiresAt.IsZero() {
		request.ExpiresAt = now.Add(15 * time.Minute)
	}
	_, err := r.approvalColl.InsertOne(ctx, request)
	if err == nil {
		return request, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return nil, fmt.Errorf("insert tool approval failed: %w", err)
	}
	var existing ToolApprovalRequest
	findErr := r.approvalColl.FindOne(ctx, approvalInvocationFilter(request.UserID, request.RunID, request.StepID, request.ToolName, request.IdempotencyKey, request.InputDigest)).Decode(&existing)
	if findErr != nil {
		return nil, fmt.Errorf("find existing tool approval failed: %w", findErr)
	}
	return &existing, nil
}

func (r *MongoAgentRepository) GetToolApproval(ctx context.Context, approvalID primitive.ObjectID, userID uint64) (*ToolApprovalRequest, error) {
	var request ToolApprovalRequest
	if err := r.approvalColl.FindOne(ctx, bson.M{"_id": approvalID, "user_id": userID}).Decode(&request); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrToolApprovalNotFound
		}
		return nil, fmt.Errorf("find tool approval failed: %w", err)
	}
	return &request, nil
}

func (r *MongoAgentRepository) ListToolApprovals(ctx context.Context, userID uint64, status string, page, pageSize int) ([]*ToolApprovalRequest, int64, error) {
	now := time.Now()
	if _, err := r.approvalColl.UpdateMany(ctx, bson.M{
		"user_id": userID, "status": bson.M{"$in": bson.A{ToolApprovalStatusPending, ToolApprovalStatusApproved}},
		"expires_at": bson.M{"$lte": now},
	}, bson.M{"$set": bson.M{"status": ToolApprovalStatusExpired, "updated_at": now}, "$inc": bson.M{"revision": 1}}); err != nil {
		return nil, 0, fmt.Errorf("expire tool approvals failed: %w", err)
	}

	filter := bson.M{"user_id": userID}
	if status != "" {
		filter["status"] = status
	}
	total, err := r.approvalColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count tool approvals failed: %w", err)
	}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(int64((page - 1) * pageSize)).SetLimit(int64(pageSize))
	cursor, err := r.approvalColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("list tool approvals failed: %w", err)
	}
	defer cursor.Close(ctx)
	var requests []*ToolApprovalRequest
	if err := cursor.All(ctx, &requests); err != nil {
		return nil, 0, fmt.Errorf("decode tool approvals failed: %w", err)
	}
	return requests, total, nil
}

func (r *MongoAgentRepository) DecideToolApproval(ctx context.Context, approvalID primitive.ObjectID, userID uint64, decision, reason string, expectedRevision int64) (*ToolApprovalRequest, error) {
	now := time.Now()
	filter := bson.M{
		"_id": approvalID, "user_id": userID, "status": ToolApprovalStatusPending,
		"revision": expectedRevision, "expires_at": bson.M{"$gt": now},
	}
	update := bson.M{"$set": bson.M{
		"status": decision, "reason": reason, "approver_user_id": userID,
		"decided_at": now, "updated_at": now,
	}, "$inc": bson.M{"revision": 1}}
	var updated ToolApprovalRequest
	err := r.approvalColl.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&updated)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrToolApprovalConflict
		}
		return nil, fmt.Errorf("decide tool approval failed: %w", err)
	}
	return &updated, nil
}

func (r *MongoAgentRepository) ClaimApprovedToolApproval(ctx context.Context, match ToolApprovalMatch, attemptID string, leaseUntil time.Time) (*ToolApprovalRequest, error) {
	now := time.Now()
	filter := approvalInvocationFilter(match.UserID, match.RunID, match.StepID, match.ToolName, match.IdempotencyKey, match.InputDigest)
	filter["expires_at"] = bson.M{"$gt": now}
	filter["$or"] = bson.A{
		bson.M{"status": ToolApprovalStatusApproved},
		bson.M{"status": ToolApprovalStatusExecuting, "execution_lease_until": bson.M{"$lte": now}},
	}
	update := bson.M{"$set": bson.M{
		"status": ToolApprovalStatusExecuting, "execution_attempt_id": attemptID,
		"execution_lease_until": leaseUntil, "updated_at": now,
	}, "$inc": bson.M{"revision": 1}}
	var claimed ToolApprovalRequest
	err := r.approvalColl.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&claimed)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrToolApprovalUnavailable
		}
		return nil, fmt.Errorf("claim approved tool request failed: %w", err)
	}
	return &claimed, nil
}

func (r *MongoAgentRepository) CompleteToolApproval(ctx context.Context, approvalID primitive.ObjectID, attemptID string) error {
	now := time.Now()
	result, err := r.approvalColl.UpdateOne(ctx, bson.M{
		"_id": approvalID, "status": ToolApprovalStatusExecuting, "execution_attempt_id": attemptID,
	}, bson.M{"$set": bson.M{
		"status": ToolApprovalStatusConsumed, "consumed_at": now, "updated_at": now,
		"execution_attempt_id": "", "execution_lease_until": time.Time{},
	}, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return fmt.Errorf("complete tool approval failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrToolApprovalConflict
	}
	return nil
}

func (r *MongoAgentRepository) ReleaseToolApproval(ctx context.Context, approvalID primitive.ObjectID, attemptID string) error {
	now := time.Now()
	result, err := r.approvalColl.UpdateOne(ctx, bson.M{
		"_id": approvalID, "status": ToolApprovalStatusExecuting, "execution_attempt_id": attemptID,
	}, bson.M{"$set": bson.M{
		"status": ToolApprovalStatusApproved, "updated_at": now,
		"execution_attempt_id": "", "execution_lease_until": time.Time{},
	}, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return fmt.Errorf("release tool approval failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrToolApprovalConflict
	}
	return nil
}

func (r *MongoAgentRepository) ClaimToolExecution(ctx context.Context, record *ToolExecutionRecord) (*ToolExecutionRecord, bool, error) {
	if record == nil {
		return nil, false, errors.New("tool execution record is nil")
	}
	now := time.Now()
	if record.ID.IsZero() {
		record.ID = primitive.NewObjectID()
	}
	record.Status = ToolExecutionStatusExecuting
	record.CreatedAt = now
	record.UpdatedAt = now
	_, err := r.executionColl.InsertOne(ctx, record)
	if err == nil {
		return record, true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return nil, false, fmt.Errorf("insert tool execution failed: %w", err)
	}

	filter := bson.M{"user_id": record.UserID, "tool_name": record.ToolName, "idempotency_key": record.IdempotencyKey}
	var existing ToolExecutionRecord
	if err := r.executionColl.FindOne(ctx, filter).Decode(&existing); err != nil {
		return nil, false, fmt.Errorf("find tool execution failed: %w", err)
	}
	if existing.InputDigest != record.InputDigest {
		return nil, false, ErrToolExecutionConflict
	}
	if existing.Status == ToolExecutionStatusSucceeded {
		return &existing, false, nil
	}
	if existing.Status == ToolExecutionStatusExecuting && existing.LeaseUntil.After(now) {
		return nil, false, ErrToolExecutionInProgress
	}

	claimFilter := bson.M{"_id": existing.ID, "input_digest": record.InputDigest, "$or": bson.A{
		bson.M{"status": ToolExecutionStatusFailed},
		bson.M{"status": ToolExecutionStatusExecuting, "lease_until": bson.M{"$lte": now}},
	}}
	claimUpdate := bson.M{"$set": bson.M{
		"status": ToolExecutionStatusExecuting, "attempt_id": record.AttemptID,
		"lease_until": record.LeaseUntil, "error_message": "", "updated_at": now,
	}}
	var claimed ToolExecutionRecord
	err = r.executionColl.FindOneAndUpdate(ctx, claimFilter, claimUpdate, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&claimed)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, false, ErrToolExecutionInProgress
		}
		return nil, false, fmt.Errorf("claim tool execution failed: %w", err)
	}
	return &claimed, true, nil
}

func (r *MongoAgentRepository) CompleteToolExecution(ctx context.Context, executionID primitive.ObjectID, attemptID string, executionResult ToolExecutionResult) error {
	now := time.Now()
	set := bson.M{
		"status": ToolExecutionStatusSucceeded, "completed_at": now,
		"updated_at": now, "lease_until": time.Time{},
		"output_digest": executionResult.Digest, "output_length": executionResult.Length,
	}
	unset := bson.M{}
	if executionResult.OutputReference != nil {
		set["output_reference"] = executionResult.OutputReference
		unset["output"] = ""
	} else {
		set["output"] = executionResult.Output
		unset["output_reference"] = ""
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	result, err := r.executionColl.UpdateOne(ctx, bson.M{
		"_id": executionID, "status": ToolExecutionStatusExecuting, "attempt_id": attemptID,
	}, update)
	if err != nil {
		return fmt.Errorf("complete tool execution failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrToolExecutionClaimInvalid
	}
	return nil
}

func (r *MongoAgentRepository) FailToolExecution(ctx context.Context, executionID primitive.ObjectID, attemptID, errorMessage string) error {
	now := time.Now()
	result, err := r.executionColl.UpdateOne(ctx, bson.M{
		"_id": executionID, "status": ToolExecutionStatusExecuting, "attempt_id": attemptID,
	}, bson.M{"$set": bson.M{
		"status": ToolExecutionStatusFailed, "error_message": errorMessage,
		"updated_at": now, "lease_until": time.Time{},
	}})
	if err != nil {
		return fmt.Errorf("fail tool execution failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrToolExecutionClaimInvalid
	}
	return nil
}

func (r *MongoAgentRepository) ReconcileToolGovernance(ctx context.Context, now time.Time) (ToolGovernanceReconcileResult, error) {
	if now.IsZero() {
		now = time.Now()
	}
	var reconciled ToolGovernanceReconcileResult

	expired, err := r.approvalColl.UpdateMany(ctx, bson.M{
		"status":     bson.M{"$in": bson.A{ToolApprovalStatusPending, ToolApprovalStatusApproved}},
		"expires_at": bson.M{"$lte": now},
	}, bson.M{
		"$set": bson.M{"status": ToolApprovalStatusExpired, "updated_at": now},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return reconciled, fmt.Errorf("reconcile expired tool approvals failed: %w", err)
	}
	reconciled.ExpiredApprovals += expired.ModifiedCount

	released, err := r.approvalColl.UpdateMany(ctx, bson.M{
		"status": ToolApprovalStatusExecuting, "execution_lease_until": bson.M{"$lte": now},
		"expires_at": bson.M{"$gt": now},
	}, bson.M{
		"$set": bson.M{
			"status": ToolApprovalStatusApproved, "updated_at": now,
			"execution_attempt_id": "", "execution_lease_until": time.Time{},
		},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return reconciled, fmt.Errorf("reconcile tool approval leases failed: %w", err)
	}
	reconciled.ReleasedApprovalLeases = released.ModifiedCount

	expiredExecuting, err := r.approvalColl.UpdateMany(ctx, bson.M{
		"status": ToolApprovalStatusExecuting, "execution_lease_until": bson.M{"$lte": now},
		"expires_at": bson.M{"$lte": now},
	}, bson.M{
		"$set": bson.M{
			"status": ToolApprovalStatusExpired, "updated_at": now,
			"execution_attempt_id": "", "execution_lease_until": time.Time{},
		},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return reconciled, fmt.Errorf("reconcile expired executing approvals failed: %w", err)
	}
	reconciled.ExpiredApprovals += expiredExecuting.ModifiedCount

	failedExecutions, err := r.executionColl.UpdateMany(ctx, bson.M{
		"status": ToolExecutionStatusExecuting, "lease_until": bson.M{"$lte": now},
	}, bson.M{"$set": bson.M{
		"status": ToolExecutionStatusFailed, "error_message": "execution lease expired during reconciliation",
		"updated_at": now, "lease_until": time.Time{}, "attempt_id": "",
	}})
	if err != nil {
		return reconciled, fmt.Errorf("reconcile tool execution leases failed: %w", err)
	}
	reconciled.FailedExecutionLeases = failedExecutions.ModifiedCount

	var workflowApprovalIDs []primitive.ObjectID
	var runtimeApprovalIDs []primitive.ObjectID
	cursor, err := r.approvalColl.Find(ctx, bson.M{
		"status":            ToolApprovalStatusExpired,
		"run_reconciled_at": bson.M{"$exists": false},
	}, options.Find().SetProjection(bson.M{"_id": 1, "source": 1}).SetLimit(200))
	if err != nil {
		return reconciled, fmt.Errorf("find expired approvals for workflow reconciliation failed: %w", err)
	}
	defer cursor.Close(ctx)
	var approvals []struct {
		ID     primitive.ObjectID `bson:"_id"`
		Source string             `bson:"source"`
	}
	if err := cursor.All(ctx, &approvals); err != nil {
		return reconciled, fmt.Errorf("decode expired approvals for workflow reconciliation failed: %w", err)
	}
	for _, approval := range approvals {
		if approval.Source == "runtime" {
			runtimeApprovalIDs = append(runtimeApprovalIDs, approval.ID)
			continue
		}
		workflowApprovalIDs = append(workflowApprovalIDs, approval.ID)
	}
	if len(workflowApprovalIDs) == 0 && len(runtimeApprovalIDs) == 0 {
		return reconciled, nil
	}

	if len(workflowApprovalIDs) > 0 {
		failedRuns, updateErr := r.runColl.UpdateMany(ctx, bson.M{
			"status": "suspended", "approval_request_id": bson.M{"$in": workflowApprovalIDs},
		}, bson.M{
			"$set": bson.M{
				"status": "failed", "error_message": "tool approval expired", "finished_at": now,
				"checkpoint_json": "", "waiting_node_id": "", "resume_token": "", "resume_token_hash": "", "resume_attempt_id": "",
				"resume_grant_issued_at": time.Time{}, "resume_grant_expires_at": time.Time{},
			},
			"$inc": bson.M{"revision": 1},
		})
		if updateErr != nil {
			return reconciled, fmt.Errorf("reconcile suspended workflow runs failed: %w", updateErr)
		}
		reconciled.FailedSuspendedRuns = failedRuns.ModifiedCount

		delegatedApprovalKeys := make([]string, 0, len(workflowApprovalIDs))
		for _, approvalID := range workflowApprovalIDs {
			delegatedApprovalKeys = append(delegatedApprovalKeys, approvalID.Hex())
		}
		failedAgentRuns, updateErr := r.agentExecutionRunColl.UpdateMany(ctx, bson.M{
			"status":              AgentExecutionRunApprovalRequired,
			"pending_resume_kind": AgentExecutionResumeDelegatedApproval,
			"approval_request_id": bson.M{"$in": delegatedApprovalKeys},
		}, bson.M{
			"$set": bson.M{
				"status": AgentExecutionRunFailed, "failure_code": "approval_expired",
				"resume_supported": false, "finished_at": now, "updated_at": now,
				"pending_action_type": "", "pending_action_name": "", "pending_action_id": "",
				"pending_resume_kind": "", "approval_request_id": "", "approval_input_digest": "",
				"approval_idempotency_key": "", "approval_expires_at": time.Time{},
				"checkpoint_version": "", "checkpoint_key_id": "", "checkpoint_nonce": "",
				"checkpoint_ciphertext": "", "checkpoint_digest": "", "checkpoint_size_bytes": 0,
				"resume_attempt_id": "", "resume_lease_until": time.Time{}, "resume_claimed_at": time.Time{},
				"resume_token_hash": "", "resume_grant_issued_at": time.Time{}, "resume_grant_expires_at": time.Time{},
			},
			"$inc": bson.M{"revision": 1},
		})
		if updateErr != nil {
			return reconciled, fmt.Errorf("reconcile delegated agent approvals failed: %w", updateErr)
		}
		reconciled.FailedAgentRuns += failedAgentRuns.ModifiedCount
	}

	if len(runtimeApprovalIDs) > 0 {
		runtimeApprovalKeys := make([]string, 0, len(runtimeApprovalIDs))
		for _, approvalID := range runtimeApprovalIDs {
			runtimeApprovalKeys = append(runtimeApprovalKeys, approvalID.Hex())
		}
		failedRuns, updateErr := r.agentExecutionRunColl.UpdateMany(ctx, bson.M{
			"status":              AgentExecutionRunApprovalRequired,
			"approval_request_id": bson.M{"$in": runtimeApprovalKeys},
		}, bson.M{
			"$set": bson.M{
				"status": AgentExecutionRunFailed, "failure_code": "approval_expired",
				"resume_supported": false, "finished_at": now, "updated_at": now,
				"pending_action_type": "", "pending_action_name": "", "pending_action_id": "",
				"pending_resume_kind": "",
				"approval_request_id": "", "approval_input_digest": "", "approval_idempotency_key": "", "approval_expires_at": time.Time{},
				"checkpoint_version": "", "checkpoint_key_id": "", "checkpoint_nonce": "", "checkpoint_ciphertext": "", "checkpoint_digest": "", "checkpoint_size_bytes": 0,
				"resume_attempt_id": "", "resume_lease_until": time.Time{}, "resume_claimed_at": time.Time{},
				"resume_token_hash": "", "resume_grant_issued_at": time.Time{}, "resume_grant_expires_at": time.Time{},
			},
			"$inc": bson.M{"revision": 1},
		})
		if updateErr != nil {
			return reconciled, fmt.Errorf("reconcile suspended agent runs failed: %w", updateErr)
		}
		reconciled.FailedAgentRuns += failedRuns.ModifiedCount
	}

	allApprovalIDs := append(workflowApprovalIDs, runtimeApprovalIDs...)
	if _, err := r.approvalColl.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": allApprovalIDs}}, bson.M{
		"$set": bson.M{"run_reconciled_at": now},
	}); err != nil {
		return reconciled, fmt.Errorf("mark expired approvals reconciled failed: %w", err)
	}
	return reconciled, nil
}

func (r *MongoAgentRepository) ClaimWorkflowRunResume(ctx context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, tokenHash, attemptID string) (*WorkflowRunRecord, error) {
	now := time.Now()
	filter := bson.M{
		"_id": runID, "user_id": userID, "status": "suspended",
		"resume_token_hash": tokenHash,
		"$or": bson.A{
			bson.M{"resume_grant_expires_at": bson.M{"$exists": false}},
			bson.M{"resume_grant_expires_at": time.Time{}},
			bson.M{"resume_grant_expires_at": bson.M{"$gt": now}},
		},
	}
	if !approvalID.IsZero() {
		filter["approval_request_id"] = approvalID
	}
	update := bson.M{"$set": bson.M{
		"status": "running", "resume_token_hash": "", "resume_attempt_id": attemptID,
		"resume_grant_issued_at": time.Time{}, "resume_grant_expires_at": time.Time{},
		"error_message": "", "updated_at": now,
	}, "$inc": bson.M{"revision": 1}}
	var run WorkflowRunRecord
	err := r.runColl.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&run)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrWorkflowResumeConflict
		}
		return nil, fmt.Errorf("claim workflow run resume failed: %w", err)
	}
	return &run, nil
}

func (r *MongoAgentRepository) IssueWorkflowResumeGrant(
	ctx context.Context,
	runID primitive.ObjectID,
	userID uint64,
	approvalID primitive.ObjectID,
	expectedRevision int64,
	tokenHash string,
	issuedAt, expiresAt time.Time,
) (*WorkflowRunRecord, error) {
	if expectedRevision <= 0 || tokenHash == "" || issuedAt.IsZero() || !expiresAt.After(issuedAt) {
		return nil, errors.New("workflow resume grant is invalid")
	}
	filter := bson.M{
		"_id": runID, "user_id": userID, "status": "suspended",
		"approval_request_id": approvalID, "revision": expectedRevision,
	}
	update := bson.M{
		"$set": bson.M{
			"resume_token": "", "resume_token_hash": tokenHash, "resume_attempt_id": "",
			"resume_grant_issued_at": issuedAt, "resume_grant_expires_at": expiresAt,
		},
		"$inc": bson.M{"revision": 1},
	}
	var run WorkflowRunRecord
	err := r.runColl.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&run)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrWorkflowResumeGrantConflict
		}
		return nil, fmt.Errorf("issue workflow resume grant failed: %w", err)
	}
	return &run, nil
}

func (r *MongoAgentRepository) RejectWorkflowRunForApproval(ctx context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, reason string) error {
	now := time.Now()
	result, err := r.runColl.UpdateOne(ctx, bson.M{
		"_id": runID, "user_id": userID, "status": "suspended", "approval_request_id": approvalID,
	}, bson.M{"$set": bson.M{
		"status": "rejected", "error_message": reason, "finished_at": now,
		"checkpoint_json": "", "waiting_node_id": "", "resume_token": "", "resume_token_hash": "", "resume_attempt_id": "",
		"resume_grant_issued_at": time.Time{}, "resume_grant_expires_at": time.Time{},
	}, "$inc": bson.M{"revision": 1}})
	if err != nil {
		return fmt.Errorf("reject workflow run failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrWorkflowResumeConflict
	}
	return nil
}

func approvalInvocationFilter(userID uint64, runID, stepID, toolName, idempotencyKey, inputDigest string) bson.M {
	return bson.M{
		"user_id": userID, "run_id": runID, "step_id": stepID, "tool_name": toolName,
		"idempotency_key": idempotencyKey, "input_digest": inputDigest,
	}
}
