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
	WorkflowCompensationStatusPlanned   = "planned"
	WorkflowCompensationStatusExecuting = "executing"
	WorkflowCompensationStatusSucceeded = "succeeded"
	WorkflowCompensationStatusFailed    = "failed"
	WorkflowCompensationStatusSuspended = "suspended"
)

var ErrWorkflowCompensationConflict = errors.New("workflow compensation conflict")

var (
	ErrWorkflowCompensationUnavailable  = errors.New("workflow compensation is unavailable")
	ErrWorkflowCompensationClaimInvalid = errors.New("workflow compensation claim is invalid")
)

type WorkflowCompensationRecord struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	RunID              primitive.ObjectID `bson:"run_id" json:"run_id"`
	WorkflowID         primitive.ObjectID `bson:"workflow_id" json:"workflow_id"`
	WorkflowRevisionID primitive.ObjectID `bson:"workflow_revision_id" json:"workflow_revision_id"`
	UserID             uint64             `bson:"user_id" json:"user_id"`
	Sequence           int                `bson:"sequence" json:"sequence"`
	SourceNodeID       string             `bson:"source_node_id" json:"source_node_id"`
	StepID             string             `bson:"step_id" json:"step_id"`
	ToolName           string             `bson:"tool_name" json:"tool_name"`
	InputJSON          string             `bson:"input_json" json:"input_json"`
	InputHash          string             `bson:"input_hash" json:"input_hash"`
	PlanHash           string             `bson:"plan_hash" json:"plan_hash"`
	TimeoutSec         int                `bson:"timeout_sec,omitempty" json:"timeout_sec,omitempty"`
	RetryJSON          string             `bson:"retry_json,omitempty" json:"retry_json,omitempty"`
	IdempotencyKey     string             `bson:"idempotency_key" json:"idempotency_key"`
	Status             string             `bson:"status" json:"status"`
	Attempt            int                `bson:"attempt" json:"attempt"`
	ErrorMessage       string             `bson:"error_message,omitempty" json:"error_message,omitempty"`
	ApprovalRequestID  primitive.ObjectID `bson:"approval_request_id,omitempty" json:"approval_request_id,omitempty"`
	AttemptID          string             `bson:"attempt_id,omitempty" json:"-"`
	LeaseUntil         time.Time          `bson:"lease_until,omitempty" json:"lease_until,omitempty"`
	OutputJSON         string             `bson:"output_json,omitempty" json:"output_json,omitempty"`
	CreatedAt          time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt          time.Time          `bson:"updated_at" json:"updated_at"`
	FinishedAt         time.Time          `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
}

type WorkflowCompensationRepository interface {
	SaveWorkflowCompensationPlan(ctx context.Context, records []*WorkflowCompensationRecord) error
	ListWorkflowCompensations(ctx context.Context, runID primitive.ObjectID, userID uint64) ([]*WorkflowCompensationRecord, error)
	ClaimWorkflowCompensation(ctx context.Context, runID primitive.ObjectID, userID uint64, sequence int, attemptID string, leaseUntil time.Time, approvalID primitive.ObjectID, retryFailed bool) (*WorkflowCompensationRecord, error)
	CompleteWorkflowCompensation(ctx context.Context, compensationID primitive.ObjectID, attemptID, outputJSON string) error
	SuspendWorkflowCompensation(ctx context.Context, compensationID primitive.ObjectID, attemptID string, approvalID primitive.ObjectID) error
	FailWorkflowCompensation(ctx context.Context, compensationID primitive.ObjectID, attemptID, errorMessage string) error
	RejectWorkflowCompensation(ctx context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, reason string) error
}

type WorkflowCompensationRecoveryCandidate struct {
	RunID    primitive.ObjectID `bson:"run_id"`
	UserID   uint64             `bson:"user_id"`
	Sequence int                `bson:"sequence"`
}

// WorkflowCompensationRecoveryRepository exposes only expired in-flight
// candidates. ClaimWorkflowCompensation remains the authority that arbitrates
// multiple service instances before any side effect is retried.
type WorkflowCompensationRecoveryRepository interface {
	ListExpiredWorkflowCompensationCandidates(ctx context.Context, now time.Time, limit int) ([]WorkflowCompensationRecoveryCandidate, error)
}

func (r *MongoAgentRepository) SaveWorkflowCompensationPlan(ctx context.Context, records []*WorkflowCompensationRecord) error {
	if len(records) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		if record.RunID.IsZero() || record.UserID == 0 || record.Sequence < 1 || record.PlanHash == "" {
			return errors.New("workflow compensation record is incomplete")
		}
		if record.ID.IsZero() {
			record.ID = primitive.NewObjectID()
		}
		now := time.Now()
		if record.CreatedAt.IsZero() {
			record.CreatedAt = now
		}
		if record.UpdatedAt.IsZero() {
			record.UpdatedAt = record.CreatedAt
		}
		if record.Status == "" {
			record.Status = WorkflowCompensationStatusPlanned
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{
				"run_id": record.RunID, "user_id": record.UserID,
				"sequence": record.Sequence, "plan_hash": record.PlanHash,
			}).
			SetUpdate(bson.M{"$setOnInsert": record}).SetUpsert(true))
	}
	if len(models) == 0 {
		return nil
	}
	if _, err := r.workflowCompensationColl.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(true)); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrWorkflowCompensationConflict
		}
		return fmt.Errorf("save workflow compensation plan failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) ListWorkflowCompensations(ctx context.Context, runID primitive.ObjectID, userID uint64) ([]*WorkflowCompensationRecord, error) {
	cursor, err := r.workflowCompensationColl.Find(ctx, bson.M{
		"run_id": runID, "user_id": userID,
	}, options.Find().SetSort(bson.D{{Key: "sequence", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("find workflow compensations failed: %w", err)
	}
	defer cursor.Close(ctx)
	var records []*WorkflowCompensationRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, fmt.Errorf("decode workflow compensations failed: %w", err)
	}
	return records, nil
}

func (r *MongoAgentRepository) ListExpiredWorkflowCompensationCandidates(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]WorkflowCompensationRecoveryCandidate, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	// Sorting before grouping selects the strict first non-succeeded journal
	// entry for each run. Later planned entries must never jump an earlier
	// failed, suspended or still-leased compensation.
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "status", Value: bson.D{{Key: "$ne", Value: WorkflowCompensationStatusSucceeded}}}}}},
		{{Key: "$sort", Value: bson.D{{Key: "run_id", Value: 1}, {Key: "sequence", Value: 1}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "run_id", Value: "$run_id"}, {Key: "user_id", Value: "$user_id"}}},
			{Key: "record", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
		}}},
		{{Key: "$match", Value: bson.D{
			{Key: "record.status", Value: WorkflowCompensationStatusExecuting},
			{Key: "record.lease_until", Value: bson.D{{Key: "$lte", Value: now}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "record.lease_until", Value: 1}, {Key: "record.run_id", Value: 1}}}},
		{{Key: "$limit", Value: int64(limit)}},
		{{Key: "$replaceRoot", Value: bson.D{{Key: "newRoot", Value: "$record"}}}},
		{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0}, {Key: "run_id", Value: 1}, {Key: "user_id", Value: 1}, {Key: "sequence", Value: 1},
		}}},
	}
	cursor, err := r.workflowCompensationColl.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("find expired workflow compensation candidates failed: %w", err)
	}
	defer cursor.Close(ctx)
	var candidates []WorkflowCompensationRecoveryCandidate
	if err := cursor.All(ctx, &candidates); err != nil {
		return nil, fmt.Errorf("decode expired workflow compensation candidates failed: %w", err)
	}
	return candidates, nil
}

func (r *MongoAgentRepository) ClaimWorkflowCompensation(
	ctx context.Context,
	runID primitive.ObjectID,
	userID uint64,
	sequence int,
	attemptID string,
	leaseUntil time.Time,
	approvalID primitive.ObjectID,
	retryFailed bool,
) (*WorkflowCompensationRecord, error) {
	if runID.IsZero() || userID == 0 || sequence < 1 || attemptID == "" || leaseUntil.IsZero() {
		return nil, errors.New("workflow compensation claim is incomplete")
	}
	now := time.Now()
	claimable := bson.A{
		bson.M{"status": WorkflowCompensationStatusPlanned},
		bson.M{"status": WorkflowCompensationStatusExecuting, "lease_until": bson.M{"$lte": now}},
	}
	if retryFailed {
		claimable = append(claimable, bson.M{"status": WorkflowCompensationStatusFailed})
	}
	if !approvalID.IsZero() {
		claimable = append(claimable, bson.M{
			"status": WorkflowCompensationStatusSuspended, "approval_request_id": approvalID,
		})
	}
	filter := bson.M{
		"run_id": runID, "user_id": userID, "sequence": sequence, "$or": claimable,
	}
	update := bson.M{
		"$set": bson.M{
			"status": WorkflowCompensationStatusExecuting, "attempt_id": attemptID,
			"lease_until": leaseUntil, "approval_request_id": primitive.NilObjectID,
			"error_message": "", "output_json": "", "finished_at": time.Time{}, "updated_at": now,
		},
		"$inc": bson.M{"attempt": 1},
	}
	var claimed WorkflowCompensationRecord
	err := r.workflowCompensationColl.FindOneAndUpdate(
		ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&claimed)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrWorkflowCompensationUnavailable
		}
		return nil, fmt.Errorf("claim workflow compensation failed: %w", err)
	}
	return &claimed, nil
}

func (r *MongoAgentRepository) CompleteWorkflowCompensation(ctx context.Context, compensationID primitive.ObjectID, attemptID, outputJSON string) error {
	now := time.Now()
	result, err := r.workflowCompensationColl.UpdateOne(ctx, bson.M{
		"_id": compensationID, "status": WorkflowCompensationStatusExecuting, "attempt_id": attemptID,
	}, bson.M{"$set": bson.M{
		"status": WorkflowCompensationStatusSucceeded, "output_json": outputJSON,
		"attempt_id": "", "lease_until": time.Time{}, "updated_at": now, "finished_at": now,
	}})
	if err != nil {
		return fmt.Errorf("complete workflow compensation failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrWorkflowCompensationClaimInvalid
	}
	return nil
}

func (r *MongoAgentRepository) SuspendWorkflowCompensation(ctx context.Context, compensationID primitive.ObjectID, attemptID string, approvalID primitive.ObjectID) error {
	if approvalID.IsZero() {
		return errors.New("workflow compensation approval id is required")
	}
	now := time.Now()
	result, err := r.workflowCompensationColl.UpdateOne(ctx, bson.M{
		"_id": compensationID, "status": WorkflowCompensationStatusExecuting, "attempt_id": attemptID,
	}, bson.M{"$set": bson.M{
		"status": WorkflowCompensationStatusSuspended, "approval_request_id": approvalID,
		"attempt_id": "", "lease_until": time.Time{}, "updated_at": now,
	}})
	if err != nil {
		return fmt.Errorf("suspend workflow compensation failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrWorkflowCompensationClaimInvalid
	}
	return nil
}

func (r *MongoAgentRepository) FailWorkflowCompensation(ctx context.Context, compensationID primitive.ObjectID, attemptID, errorMessage string) error {
	now := time.Now()
	result, err := r.workflowCompensationColl.UpdateOne(ctx, bson.M{
		"_id": compensationID, "status": WorkflowCompensationStatusExecuting, "attempt_id": attemptID,
	}, bson.M{"$set": bson.M{
		"status": WorkflowCompensationStatusFailed, "error_message": errorMessage,
		"attempt_id": "", "lease_until": time.Time{}, "updated_at": now, "finished_at": now,
	}})
	if err != nil {
		return fmt.Errorf("fail workflow compensation failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrWorkflowCompensationClaimInvalid
	}
	return nil
}

func (r *MongoAgentRepository) RejectWorkflowCompensation(ctx context.Context, runID primitive.ObjectID, userID uint64, approvalID primitive.ObjectID, reason string) error {
	now := time.Now()
	result, err := r.workflowCompensationColl.UpdateOne(ctx, bson.M{
		"run_id": runID, "user_id": userID, "status": WorkflowCompensationStatusSuspended,
		"approval_request_id": approvalID,
	}, bson.M{"$set": bson.M{
		"status": WorkflowCompensationStatusFailed, "error_message": reason,
		"approval_request_id": primitive.NilObjectID, "updated_at": now, "finished_at": now,
	}})
	if err != nil {
		return fmt.Errorf("reject workflow compensation failed: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrWorkflowCompensationUnavailable
	}
	return nil
}
