package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const ExecutionAccountingVersion = "execution.accounting.v1"

// AgentRunAccountingStore owns the bounded, tenant-scoped child lookup used
// by the read-only Agent accounting projection.
type AgentRunAccountingStore interface {
	ListDirectChildWorkflowRuns(
		context.Context,
		uint64,
		string,
		int,
	) ([]*WorkflowRunRecord, int64, error)
}

func (r *MongoAgentRepository) ListDirectChildWorkflowRuns(
	ctx context.Context,
	userID uint64,
	parentRunID string,
	limit int,
) ([]*WorkflowRunRecord, int64, error) {
	if r == nil || r.runColl == nil {
		return nil, 0, errors.New("workflow run repository is unavailable")
	}
	parentRunID = strings.TrimSpace(parentRunID)
	if userID == 0 || parentRunID == "" || limit <= 0 {
		return nil, 0, errors.New("agent run child query is incomplete")
	}
	filter := bson.M{"user_id": userID, "parent_run_id": parentRunID}
	total, err := r.runColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count direct child workflow runs failed: %w", err)
	}
	cursor, err := r.runColl.Find(
		ctx,
		filter,
		options.Find().
			SetSort(bson.D{{Key: "started_at", Value: 1}, {Key: "_id", Value: 1}}).
			SetLimit(int64(limit)).
			SetProjection(bson.M{
				"_id": 1, "workflow_id": 1, "user_id": 1, "status": 1,
				"parent_run_id": 1, "parent_action_id": 1,
				"node_executions": 1, "input_tokens": 1, "output_tokens": 1,
				"total_tokens": 1, "usage_estimated": 1,
				"estimated_cost_micros": 1, "cost_estimated": 1,
				"pricing_version": 1, "max_steps": 1, "max_total_tokens": 1,
				"max_estimated_cost_micros": 1, "accounting_version": 1,
				"started_at": 1, "suspended_at": 1, "finished_at": 1,
			}),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("find direct child workflow runs failed: %w", err)
	}
	defer cursor.Close(ctx)
	var runs []*WorkflowRunRecord
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, 0, fmt.Errorf("decode direct child workflow runs failed: %w", err)
	}
	return runs, total, nil
}
