package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrWorkflowRunCancellationUnavailable = errors.New("workflow run cancellation is unavailable")

const (
	workflowRunStatusRunning   = "running"
	workflowRunStatusCanceling = "canceling"
	workflowRunStatusCanceled  = "canceled"
)

// WorkflowRunCancellationRepository is the durable cross-instance execution
// control port. CommitWorkflowRunExecutionState arbitrates the race between a
// normal terminal transition and a previously persisted cancellation request.
type WorkflowRunCancellationRepository interface {
	RequestWorkflowRunCancellation(
		ctx context.Context,
		runID primitive.ObjectID,
		userID uint64,
		reason string,
	) (*WorkflowRunRecord, error)
	IsWorkflowRunCancellationRequested(
		ctx context.Context,
		runID primitive.ObjectID,
		userID uint64,
	) (bool, error)
	CommitWorkflowRunExecutionState(
		ctx context.Context,
		run *WorkflowRunRecord,
	) (*WorkflowRunRecord, error)
}

func (r *MongoAgentRepository) RequestWorkflowRunCancellation(
	ctx context.Context,
	runID primitive.ObjectID,
	userID uint64,
	reason string,
) (*WorkflowRunRecord, error) {
	now := time.Now()
	var run WorkflowRunRecord
	err := r.runColl.FindOneAndUpdate(
		ctx,
		bson.M{"_id": runID, "user_id": userID, "status": workflowRunStatusRunning},
		bson.M{
			"$set": bson.M{
				"status": workflowRunStatusCanceling, "cancel_requested_at": now,
				"cancel_reason": strings.TrimSpace(reason),
			},
			"$inc": bson.M{"revision": 1},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&run)
	if err == nil {
		return &run, nil
	}
	if err != mongo.ErrNoDocuments {
		return nil, fmt.Errorf("request workflow run cancellation failed: %w", err)
	}
	existing, getErr := r.GetWorkflowRun(ctx, runID, userID)
	if getErr != nil {
		return nil, getErr
	}
	if existing.Status == workflowRunStatusCanceling || existing.Status == workflowRunStatusCanceled {
		return existing, nil
	}
	return nil, fmt.Errorf("%w while run status is %s", ErrWorkflowRunCancellationUnavailable, existing.Status)
}

func (r *MongoAgentRepository) IsWorkflowRunCancellationRequested(
	ctx context.Context,
	runID primitive.ObjectID,
	userID uint64,
) (bool, error) {
	var state struct {
		Status string `bson:"status"`
	}
	err := r.runColl.FindOne(
		ctx,
		bson.M{"_id": runID, "user_id": userID},
		options.FindOne().SetProjection(bson.M{"status": 1}),
	).Decode(&state)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, fmt.Errorf("workflow run not found: %s", runID.Hex())
		}
		return false, fmt.Errorf("read workflow run cancellation state failed: %w", err)
	}
	return state.Status == workflowRunStatusCanceling, nil
}

func (r *MongoAgentRepository) CommitWorkflowRunExecutionState(
	ctx context.Context,
	run *WorkflowRunRecord,
) (*WorkflowRunRecord, error) {
	if run == nil || run.ID.IsZero() || run.UserID == 0 {
		return nil, errors.New("workflow run identity is incomplete")
	}
	for attempt := 0; attempt < 5; attempt++ {
		current, err := r.GetWorkflowRun(ctx, run.ID, run.UserID)
		if err != nil {
			return nil, err
		}
		if current.Status != workflowRunStatusRunning && current.Status != workflowRunStatusCanceling {
			return current, nil
		}
		desired := *run
		desired.Revision = current.Revision
		desired.CancelRequestedAt = current.CancelRequestedAt
		desired.CancelReason = current.CancelReason
		desired.CanceledAt = current.CanceledAt
		if current.Status == workflowRunStatusCanceling {
			desired.Status = workflowRunStatusCanceled
			desired.ErrorMessage = "workflow canceled by user"
			if strings.TrimSpace(current.CancelReason) != "" {
				desired.ErrorMessage += ": " + strings.TrimSpace(current.CancelReason)
			}
			if desired.FinishedAt.IsZero() {
				desired.FinishedAt = time.Now()
			}
			desired.CanceledAt = desired.FinishedAt
			desired.CheckpointJSON = ""
			desired.WaitingNodeID = ""
			desired.ApprovalRequestID = primitive.NilObjectID
			desired.ResumeToken = ""
			desired.ResumeTokenHash = ""
			desired.ResumeAttemptID = ""
			desired.ResumeGrantIssuedAt = time.Time{}
			desired.ResumeGrantExpiresAt = time.Time{}
			desired.SuspendedAt = time.Time{}
		}
		result, err := r.runColl.UpdateOne(
			ctx,
			bson.M{
				"_id": run.ID, "user_id": run.UserID,
				"status": current.Status, "revision": current.Revision,
			},
			bson.M{
				"$set": workflowRunExecutionStateFields(&desired),
				"$inc": bson.M{"revision": 1},
			},
		)
		if err != nil {
			return nil, fmt.Errorf("commit workflow run execution state failed: %w", err)
		}
		if result.MatchedCount == 1 {
			desired.Revision++
			return &desired, nil
		}
	}
	return nil, errors.New("workflow run execution state changed too frequently")
}

func workflowRunExecutionStateFields(run *WorkflowRunRecord) bson.M {
	return bson.M{
		"status": run.Status, "output_json": run.OutputJSON,
		"checkpoint_json": run.CheckpointJSON, "waiting_node_id": run.WaitingNodeID,
		"approval_request_id": run.ApprovalRequestID, "resume_token": run.ResumeToken,
		"resume_token_hash": run.ResumeTokenHash, "resume_attempt_id": run.ResumeAttemptID,
		"resume_grant_issued_at": run.ResumeGrantIssuedAt, "resume_grant_expires_at": run.ResumeGrantExpiresAt,
		"state_version": run.StateVersion, "node_executions": run.NodeExecutions,
		"input_tokens": run.InputTokens, "output_tokens": run.OutputTokens,
		"total_tokens": run.TotalTokens, "usage_estimated": run.UsageEstimated,
		"estimated_cost_micros": run.EstimatedCostMicros, "cost_estimated": run.CostEstimated,
		"pricing_version": run.PricingVersion, "max_steps": run.MaxSteps,
		"max_total_tokens":          run.MaxTotalTokens,
		"max_estimated_cost_micros": run.MaxEstimatedCostMicros,
		"accounting_version":        run.AccountingVersion, "error_message": run.ErrorMessage,
		"suspended_at": run.SuspendedAt, "finished_at": run.FinishedAt,
		"cancel_requested_at": run.CancelRequestedAt, "cancel_reason": run.CancelReason,
		"canceled_at": run.CanceledAt,
	}
}
