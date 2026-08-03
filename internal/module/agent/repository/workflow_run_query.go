package repository

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type WorkflowRunQueryRepository interface {
	ListWorkflowRuns(
		ctx context.Context,
		userID uint64,
		workflowID primitive.ObjectID,
		status string,
		page int,
		pageSize int,
	) ([]*WorkflowRunRecord, int64, error)
}

func (r *MongoAgentRepository) ListWorkflowRuns(
	ctx context.Context,
	userID uint64,
	workflowID primitive.ObjectID,
	status string,
	page int,
	pageSize int,
) ([]*WorkflowRunRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	filter := bson.M{"user_id": userID}
	if !workflowID.IsZero() {
		filter["workflow_id"] = workflowID
	}
	if status != "" {
		filter["status"] = status
	}
	total, err := r.runColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count workflow runs failed: %w", err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "started_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetSkip(int64((page - 1) * pageSize)).
		SetLimit(int64(pageSize))
	cursor, err := r.runColl.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find workflow runs failed: %w", err)
	}
	defer cursor.Close(ctx)
	var runs []*WorkflowRunRecord
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, 0, fmt.Errorf("decode workflow runs failed: %w", err)
	}
	return runs, total, nil
}
