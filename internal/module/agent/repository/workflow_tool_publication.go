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

const CollectionWorkflowToolPublications = "agent_workflow_tool_publications"

const (
	WorkflowToolPublicationActive   = "active"
	WorkflowToolPublicationDisabled = "disabled"
)

var (
	ErrWorkflowToolPublicationNotFound = errors.New("workflow tool publication not found")
	ErrWorkflowToolPublicationConflict = errors.New("workflow tool publication revision conflict")
)

// WorkflowToolPublication binds one stable Runtime tool name to an immutable
// workflow revision. Draft workflow edits never mutate an active publication.
type WorkflowToolPublication struct {
	ID                     primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID                 uint64             `bson:"user_id" json:"user_id"`
	WorkflowID             primitive.ObjectID `bson:"workflow_id" json:"workflow_id"`
	WorkflowRevisionID     primitive.ObjectID `bson:"workflow_revision_id" json:"workflow_revision_id"`
	WorkflowRevisionNumber int64              `bson:"workflow_revision_number" json:"workflow_revision_number"`
	WorkflowDSLHash        string             `bson:"workflow_dsl_hash" json:"workflow_dsl_hash"`
	ToolName               string             `bson:"tool_name" json:"tool_name"`
	DisplayName            string             `bson:"display_name" json:"display_name"`
	Description            string             `bson:"description" json:"description"`
	InputSchemaJSON        string             `bson:"input_schema_json" json:"input_schema_json"`
	Status                 string             `bson:"status" json:"status"`
	Revision               int64              `bson:"revision" json:"revision"`
	CreatedAt              time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt              time.Time          `bson:"updated_at" json:"updated_at"`
}

// WorkflowToolPublicationStore is intentionally separate from AgentRepository.
// Dynamic Runtime tools can evolve without expanding every workflow test fake.
type WorkflowToolPublicationStore interface {
	SaveWorkflowToolPublication(
		ctx context.Context,
		publication *WorkflowToolPublication,
		expectedRevision int64,
	) error
	GetWorkflowToolPublication(
		ctx context.Context,
		userID uint64,
		workflowID primitive.ObjectID,
	) (*WorkflowToolPublication, error)
	GetWorkflowToolPublicationByName(
		ctx context.Context,
		userID uint64,
		toolName string,
	) (*WorkflowToolPublication, error)
	ListActiveWorkflowToolPublications(
		ctx context.Context,
		userID uint64,
		limit int,
	) ([]*WorkflowToolPublication, error)
}

type MongoWorkflowToolPublicationRepository struct {
	collection *mongo.Collection
}

func NewMongoWorkflowToolPublicationRepository(db *mongo.Database) *MongoWorkflowToolPublicationRepository {
	if db == nil {
		return &MongoWorkflowToolPublicationRepository{}
	}
	return &MongoWorkflowToolPublicationRepository{
		collection: db.Collection(CollectionWorkflowToolPublications),
	}
}

func (r *MongoWorkflowToolPublicationRepository) EnsureIndexes(ctx context.Context) error {
	if r == nil || r.collection == nil {
		return errors.New("workflow tool publication repository is unavailable")
	}
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "workflow_id", Value: 1},
			},
			Options: options.Index().
				SetName("uniq_workflow_tool_user_workflow").
				SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "tool_name", Value: 1},
			},
			Options: options.Index().
				SetName("uniq_workflow_tool_user_name").
				SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "status", Value: 1},
				{Key: "tool_name", Value: 1},
			},
			Options: options.Index().SetName("idx_workflow_tool_active_catalog"),
		},
	})
	if err != nil {
		return fmt.Errorf("create workflow tool publication indexes: %w", err)
	}
	return nil
}

func (r *MongoWorkflowToolPublicationRepository) SaveWorkflowToolPublication(
	ctx context.Context,
	publication *WorkflowToolPublication,
	expectedRevision int64,
) error {
	if r == nil || r.collection == nil {
		return errors.New("workflow tool publication repository is unavailable")
	}
	if err := validateWorkflowToolPublication(publication); err != nil {
		return err
	}
	if expectedRevision < 0 {
		return errors.New("expected workflow tool publication revision cannot be negative")
	}

	now := time.Now()
	publication.UpdatedAt = now
	if expectedRevision == 0 {
		if publication.ID.IsZero() {
			publication.ID = primitive.NewObjectID()
		}
		if publication.CreatedAt.IsZero() {
			publication.CreatedAt = now
		}
		publication.Revision = 1
		if _, err := r.collection.InsertOne(ctx, publication); err != nil {
			if mongo.IsDuplicateKeyError(err) {
				return ErrWorkflowToolPublicationConflict
			}
			return fmt.Errorf("insert workflow tool publication: %w", err)
		}
		return nil
	}

	update := bson.M{
		"$set": bson.M{
			"workflow_revision_id":     publication.WorkflowRevisionID,
			"workflow_revision_number": publication.WorkflowRevisionNumber,
			"workflow_dsl_hash":        publication.WorkflowDSLHash,
			"tool_name":                publication.ToolName,
			"display_name":             publication.DisplayName,
			"description":              publication.Description,
			"input_schema_json":        publication.InputSchemaJSON,
			"status":                   publication.Status,
			"updated_at":               now,
		},
		"$inc": bson.M{"revision": 1},
	}
	var saved WorkflowToolPublication
	err := r.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"user_id":     publication.UserID,
			"workflow_id": publication.WorkflowID,
			"revision":    expectedRevision,
		},
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&saved)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) || mongo.IsDuplicateKeyError(err) {
			return ErrWorkflowToolPublicationConflict
		}
		return fmt.Errorf("update workflow tool publication: %w", err)
	}
	*publication = saved
	return nil
}

func (r *MongoWorkflowToolPublicationRepository) GetWorkflowToolPublication(
	ctx context.Context,
	userID uint64,
	workflowID primitive.ObjectID,
) (*WorkflowToolPublication, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("workflow tool publication repository is unavailable")
	}
	return r.findOne(ctx, bson.M{"user_id": userID, "workflow_id": workflowID})
}

func (r *MongoWorkflowToolPublicationRepository) GetWorkflowToolPublicationByName(
	ctx context.Context,
	userID uint64,
	toolName string,
) (*WorkflowToolPublication, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("workflow tool publication repository is unavailable")
	}
	return r.findOne(ctx, bson.M{
		"user_id":   userID,
		"tool_name": strings.TrimSpace(toolName),
	})
}

func (r *MongoWorkflowToolPublicationRepository) ListActiveWorkflowToolPublications(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]*WorkflowToolPublication, error) {
	if r == nil || r.collection == nil {
		return nil, errors.New("workflow tool publication repository is unavailable")
	}
	if userID == 0 {
		return nil, errors.New("workflow tool publication user is required")
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	cursor, err := r.collection.Find(
		ctx,
		bson.M{"user_id": userID, "status": WorkflowToolPublicationActive},
		options.Find().
			SetSort(bson.D{{Key: "tool_name", Value: 1}}).
			SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("list workflow tool publications: %w", err)
	}
	defer cursor.Close(ctx)
	var publications []*WorkflowToolPublication
	if err := cursor.All(ctx, &publications); err != nil {
		return nil, fmt.Errorf("decode workflow tool publications: %w", err)
	}
	return publications, nil
}

func (r *MongoWorkflowToolPublicationRepository) findOne(
	ctx context.Context,
	filter bson.M,
) (*WorkflowToolPublication, error) {
	var publication WorkflowToolPublication
	if err := r.collection.FindOne(ctx, filter).Decode(&publication); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrWorkflowToolPublicationNotFound
		}
		return nil, fmt.Errorf("find workflow tool publication: %w", err)
	}
	return &publication, nil
}

func validateWorkflowToolPublication(publication *WorkflowToolPublication) error {
	if publication == nil {
		return errors.New("workflow tool publication is required")
	}
	if publication.UserID == 0 || publication.WorkflowID.IsZero() ||
		publication.WorkflowRevisionID.IsZero() {
		return errors.New("workflow tool publication identity is incomplete")
	}
	if publication.WorkflowRevisionNumber < 1 ||
		strings.TrimSpace(publication.WorkflowDSLHash) == "" ||
		strings.TrimSpace(publication.ToolName) == "" ||
		strings.TrimSpace(publication.DisplayName) == "" ||
		strings.TrimSpace(publication.Description) == "" ||
		strings.TrimSpace(publication.InputSchemaJSON) == "" {
		return errors.New("workflow tool publication metadata is incomplete")
	}
	switch publication.Status {
	case WorkflowToolPublicationActive, WorkflowToolPublicationDisabled:
	default:
		return fmt.Errorf("invalid workflow tool publication status %q", publication.Status)
	}
	return nil
}
