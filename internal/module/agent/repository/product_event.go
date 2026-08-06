package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentProduct "twitter-clone/internal/module/agent/product"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CollectionAgentProductEvents = "agent_product_events"

type MongoProductEventRepository struct {
	collection *mongo.Collection
}

func NewMongoProductEventRepository(db *mongo.Database) *MongoProductEventRepository {
	if db == nil {
		return &MongoProductEventRepository{}
	}
	return &MongoProductEventRepository{collection: db.Collection(CollectionAgentProductEvents)}
}

func (r *MongoProductEventRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil || r.collection == nil {
		return errors.New("agent product event repository is unavailable")
	}
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "kind", Value: 1}, {Key: "occurred_at", Value: -1}},
			Options: options.Index().SetName("idx_agent_product_event_kind_occurred"),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1}, {Key: "subject_type", Value: 1}, {Key: "subject_id", Value: 1},
				{Key: "kind", Value: 1},
			},
			Options: options.Index().SetName("idx_agent_product_event_user_subject_kind"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "kind", Value: 1}, {Key: "occurred_at", Value: -1}},
			Options: options.Index().SetName("idx_agent_product_event_user_kind_occurred"),
		},
	})
	if err != nil {
		return fmt.Errorf("create agent product event indexes: %w", err)
	}
	return nil
}

func (r *MongoProductEventRepository) RecordProductEvent(
	ctx context.Context,
	event *agentProduct.Event,
) (bool, error) {
	if r == nil || r.collection == nil {
		return false, errors.New("agent product event repository is unavailable")
	}
	if err := event.Validate(); err != nil {
		return false, err
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if _, err := r.collection.InsertOne(ctx, event); err == nil {
		return true, nil
	} else if !mongo.IsDuplicateKeyError(err) {
		return false, fmt.Errorf("insert agent product event: %w", err)
	}

	var existing agentProduct.Event
	if err := r.collection.FindOne(ctx, bson.M{"_id": event.ID}).Decode(&existing); err != nil {
		return false, fmt.Errorf("read replayed agent product event: %w", err)
	}
	if !agentProduct.SameFact(&existing, event) {
		return false, agentProduct.ErrEventConflict
	}
	return false, nil
}

func (r *MongoProductEventRepository) CountProductEvents(
	ctx context.Context,
	userID uint64,
	subjectType string,
	subjectID string,
	kind string,
	limit int64,
) (int64, error) {
	if r == nil || r.collection == nil {
		return 0, errors.New("agent product event repository is unavailable")
	}
	subjectType = strings.TrimSpace(subjectType)
	subjectID = strings.TrimSpace(subjectID)
	kind = strings.TrimSpace(kind)
	if userID == 0 || subjectType == "" || subjectID == "" || kind == "" || limit <= 0 || limit > 100 {
		return 0, errors.New("agent product event count query is invalid")
	}
	count, err := r.collection.CountDocuments(
		ctx,
		bson.M{
			"user_id":      userID,
			"subject_type": subjectType,
			"subject_id":   subjectID,
			"kind":         kind,
		},
		options.Count().SetLimit(limit),
	)
	if err != nil {
		return 0, fmt.Errorf("count agent product events: %w", err)
	}
	return count, nil
}

var _ agentProduct.Store = (*MongoProductEventRepository)(nil)
