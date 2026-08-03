package repository

import (
	"context"
	"errors"
	"fmt"

	agentproject "twitter-clone/internal/module/agent/project"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var _ agentproject.Store = (*MongoAgentRepository)(nil)

func (r *MongoAgentRepository) CreateProject(ctx context.Context, project *agentproject.Project) error {
	if r == nil || r.agentProjectColl == nil {
		return errors.New("Agent project collection is unavailable")
	}
	if project == nil {
		return errors.New("Agent project is required")
	}
	if _, err := r.agentProjectColl.InsertOne(ctx, project); err != nil {
		return fmt.Errorf("insert Agent project failed: %w", err)
	}
	return nil
}

func (r *MongoAgentRepository) GetProject(ctx context.Context, projectID string) (*agentproject.Project, error) {
	if r == nil || r.agentProjectColl == nil {
		return nil, errors.New("Agent project collection is unavailable")
	}
	var project agentproject.Project
	if err := r.agentProjectColl.FindOne(ctx, bson.M{"_id": projectID}).Decode(&project); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, agentproject.ErrNotFound
		}
		return nil, fmt.Errorf("find Agent project failed: %w", err)
	}
	return &project, nil
}

func (r *MongoAgentRepository) ListProjectsForUser(
	ctx context.Context,
	userID uint64,
	page, pageSize int,
) ([]*agentproject.Project, int64, error) {
	if r == nil || r.agentProjectColl == nil {
		return nil, 0, errors.New("Agent project collection is unavailable")
	}
	page, pageSize = normalizeAgentProjectPagination(page, pageSize)
	filter := bson.M{"members.user_id": userID}
	total, err := r.agentProjectColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("count Agent projects failed: %w", err)
	}
	cursor, err := r.agentProjectColl.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "updated_at", Value: -1}, {Key: "_id", Value: 1}}).
		SetSkip(int64((page-1)*pageSize)).SetLimit(int64(pageSize)))
	if err != nil {
		return nil, 0, fmt.Errorf("list Agent projects failed: %w", err)
	}
	defer cursor.Close(ctx)
	var projects []*agentproject.Project
	if err := cursor.All(ctx, &projects); err != nil {
		return nil, 0, fmt.Errorf("decode Agent projects failed: %w", err)
	}
	return projects, total, nil
}

func (r *MongoAgentRepository) ListProjectIDsForUser(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]string, error) {
	if r == nil || r.agentProjectColl == nil {
		return nil, errors.New("Agent project collection is unavailable")
	}
	if limit < 1 || limit > 1000 {
		limit = 257
	}
	cursor, err := r.agentProjectColl.Find(
		ctx,
		bson.M{"members.user_id": userID},
		options.Find().SetProjection(bson.M{"_id": 1}).SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("list accessible Agent project ids failed: %w", err)
	}
	defer cursor.Close(ctx)
	var records []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &records); err != nil {
		return nil, fmt.Errorf("decode accessible Agent project ids failed: %w", err)
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids, nil
}

func (r *MongoAgentRepository) ReplaceProjectMembers(
	ctx context.Context,
	project *agentproject.Project,
	expectedRevision int64,
) error {
	if r == nil || r.agentProjectColl == nil {
		return errors.New("Agent project collection is unavailable")
	}
	if project == nil {
		return errors.New("Agent project is required")
	}
	result, err := r.agentProjectColl.UpdateOne(
		ctx,
		bson.M{"_id": project.ID, "revision": expectedRevision},
		bson.M{
			"$set": bson.M{"members": project.Members, "updated_at": project.UpdatedAt},
			"$inc": bson.M{"revision": 1},
		},
	)
	if err != nil {
		return fmt.Errorf("replace Agent project members failed: %w", err)
	}
	if result.MatchedCount == 0 {
		count, countErr := r.agentProjectColl.CountDocuments(ctx, bson.M{"_id": project.ID})
		if countErr != nil {
			return fmt.Errorf("verify Agent project member update failed: %w", countErr)
		}
		if count == 0 {
			return agentproject.ErrNotFound
		}
		return agentproject.ErrRevisionConflict
	}
	project.Revision = expectedRevision + 1
	return nil
}

func normalizeAgentProjectPagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}
