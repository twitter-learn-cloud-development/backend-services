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

	"twitter-clone/internal/module/agent/attribution"
)

const CollectionAgentPublishedContentAttributions = "agent_published_content_attributions"

type MongoContentAttributionRepository struct {
	collection *mongo.Collection
}

func NewMongoContentAttributionRepository(db *mongo.Database) *MongoContentAttributionRepository {
	if db == nil {
		return &MongoContentAttributionRepository{}
	}
	return &MongoContentAttributionRepository{collection: db.Collection(CollectionAgentPublishedContentAttributions)}
}

func (r *MongoContentAttributionRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil || r.collection == nil {
		return errors.New("content attribution repository is unavailable")
	}
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "tweet_id", Value: 1}},
			Options: options.Index().SetName("uniq_agent_published_tweet").SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "author_user_id", Value: 1}, {Key: "source_run_id", Value: 1}},
			Options: options.Index().SetName("idx_agent_published_source_run"),
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetName("ttl_agent_published_attribution").SetExpireAfterSeconds(0),
		},
	})
	if err != nil {
		return fmt.Errorf("create content attribution indexes: %w", err)
	}
	return nil
}

func (r *MongoContentAttributionRepository) SavePublishedContent(ctx context.Context, record *attribution.PublishedContent) error {
	if r == nil || r.collection == nil {
		return errors.New("content attribution repository is unavailable")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	now := time.Now()
	if record.ID.IsZero() {
		record.ID = primitive.NewObjectID()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = now
	}
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"tweet_id": record.TweetID},
		bson.M{"$setOnInsert": record},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("upsert published content attribution: %w", err)
	}
	existing, err := r.GetPublishedContent(ctx, record.TweetID)
	if err != nil {
		return err
	}
	if existing.AuthorUserID != record.AuthorUserID || existing.SourceRunID != record.SourceRunID {
		return attribution.ErrPublishedContentConflict
	}
	return nil
}

func (r *MongoContentAttributionRepository) GetPublishedContent(ctx context.Context, tweetID uint64) (*attribution.PublishedContent, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("content attribution repository is unavailable")
	}
	if tweetID == 0 {
		return nil, errors.New("tweet id is required")
	}
	var record attribution.PublishedContent
	if err := r.collection.FindOne(ctx, bson.M{"tweet_id": tweetID}).Decode(&record); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, attribution.ErrPublishedContentNotFound
		}
		return nil, fmt.Errorf("find published content attribution: %w", err)
	}
	return &record, nil
}

func (r *MongoContentAttributionRepository) MarkOutcomeRecorded(
	ctx context.Context,
	tweetID uint64,
	eventID, kind string,
	occurredAt time.Time,
) (bool, error) {
	if r == nil || r.collection == nil {
		return false, errors.New("content attribution repository is unavailable")
	}
	eventID, kind = strings.TrimSpace(eventID), strings.TrimSpace(kind)
	if tweetID == 0 || eventID == "" || len(eventID) > 160 || occurredAt.IsZero() {
		return false, errors.New("valid engagement outcome identity is required")
	}
	if kind != attribution.EngagementKindLike && kind != attribution.EngagementKindComment {
		return false, errors.New("valid engagement outcome kind is required")
	}
	now := time.Now()
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"tweet_id": tweetID,
		"$or": bson.A{
			bson.M{"outcome_recorded_at": bson.M{"$exists": false}},
			bson.M{"outcome_recorded_at": time.Time{}},
		},
	}, bson.M{"$set": bson.M{
		"outcome_recorded_at":    now,
		"engagement_event_id":    eventID,
		"engagement_kind":        kind,
		"engagement_occurred_at": occurredAt,
		"updated_at":             now,
	}})
	if err != nil {
		return false, fmt.Errorf("mark content engagement outcome: %w", err)
	}
	if result.MatchedCount == 1 {
		return true, nil
	}
	if _, err := r.GetPublishedContent(ctx, tweetID); err != nil {
		return false, err
	}
	return false, nil
}
