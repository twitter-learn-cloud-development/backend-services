package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/pkg/es"
)

// ConfiguredTweetEmbeddingProfile is shared by task registration and collection
// bootstrap. Existing collections are never resized or recreated automatically.
func ConfiguredTweetEmbeddingProfile() domain.EmbeddingProfile {
	dimension := 1024
	if value := os.Getenv("TWEET_EMBEDDING_DIMENSION"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			dimension = 0
		} else {
			dimension = parsed
		}
	}
	version := os.Getenv("TWEET_EMBEDDING_VERSION")
	if version == "" {
		version = "v1"
	}
	return domain.EmbeddingProfile{Model: os.Getenv("LM_STUDIO_MODEL_EMBEDDING"), Dimension: dimension, Version: version}
}

func configuredEmbeddingProfile() domain.EmbeddingProfile { return ConfiguredTweetEmbeddingProfile() }
func (c *TimelineConsumer) currentEmbeddingProfile() domain.EmbeddingProfile {
	if c.embeddingProfile.Version != "" {
		return c.embeddingProfile
	}
	return configuredEmbeddingProfile()
}

func (c *TimelineConsumer) enqueueSplitProjection(ctx context.Context, event events.TweetCreatedEvent) (bool, error) {
	if c.outboxRepo == nil || c.newOutboxTaskID == nil || event.TweetID == 0 || event.AuthorID == 0 {
		return false, errors.New("split projection enqueue unavailable")
	}
	profile := c.currentEmbeddingProfile()
	body, err := json.Marshal(domain.ProjectionTaskPayload{TweetID: event.TweetID, AuthorID: event.AuthorID, SchemaVersion: domain.ProjectionVersion, Embedding: profile})
	if err != nil {
		return false, err
	}
	createdAny := false
	for _, kind := range []string{domain.ProjectionSyncText, domain.ProjectionSyncVector} {
		id, err := c.newOutboxTaskID()
		if err != nil {
			return false, errors.New("projection task identity unavailable")
		}
		version := "v1"
		if kind == domain.ProjectionSyncVector {
			version = profile.Digest()[:16]
		}
		key := fmt.Sprintf("timeline:%s:tweet:%d:%s", kind, event.TweetID, version)
		created, err := c.outboxRepo.CreateIdempotent(ctx, &domain.OutboxTask{ID: id, DedupKey: &key, TaskType: kind, Payload: string(body), MaxRetries: 5})
		if err != nil {
			return false, errors.New("split projection registration failed")
		}
		createdAny = createdAny || created
	}
	return createdAny, nil
}

func (c *TimelineConsumer) executeProjectionTask(ctx context.Context, task *domain.OutboxTask, event *events.TweetCreatedEvent) error {
	switch task.TaskType {
	case syncESOutboxTaskType:
		if c.searchSyncExecutor != nil {
			return c.searchSyncExecutor(ctx, event)
		}
		// Drain legacy tasks by registering durable independent children.
		_, err := c.enqueueSplitProjection(ctx, *event)
		return err
	case domain.ProjectionSyncText, domain.ProjectionSyncVector:
		var payload domain.ProjectionTaskPayload
		if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil || payload.SchemaVersion != domain.ProjectionVersion {
			return errors.New("invalid projection task schema")
		}
		if task.TaskType == domain.ProjectionSyncText {
			return c.reconcileTextProjection(ctx, payload)
		}
		return c.reconcileVectorProjection(ctx, payload)
	default:
		return c.executeProjectionDeletion(ctx, event.TweetID, task.TaskType)
	}
}

func (c *TimelineConsumer) reconcileTextProjection(ctx context.Context, payload domain.ProjectionTaskPayload) error {
	if c.projectionRepo == nil {
		return errors.New("text fact reader unavailable")
	}
	return c.projectionRepo.WithProjectionLock(ctx, payload.TweetID, func(locked context.Context) error {
		current, err := c.projectionRepo.ReadProjectionTweet(locked, payload.TweetID)
		if err != nil {
			return err
		}
		if !publicProjection(current) {
			return c.deleteESProjection(locked, payload.TweetID)
		}
		if current.ID != payload.TweetID || current.UserID != payload.AuthorID {
			return errors.New("projection identity mismatch")
		}
		doc := es.TweetDocument{ID: fmt.Sprint(current.ID), UserID: fmt.Sprint(current.UserID), ParentID: fmt.Sprint(current.ParentID), Content: "Document: " + current.Content, Type: current.Type, VisibleType: current.VisibleType, CreatedAt: current.CreatedAt, ProjectionDigest: domain.ProjectionDigest(current), ProjectionVersion: domain.ProjectionVersion}
		if c.textProjectionExecutor != nil {
			err = c.textProjectionExecutor(locked, doc)
		} else if c.esClient == nil {
			err = errors.New("text writer unavailable")
		} else {
			err = c.esClient.IndexTweet(locked, doc)
		}
		if err != nil {
			return err
		}
		after, err := c.projectionRepo.ReadProjectionTweet(locked, payload.TweetID)
		if err != nil {
			return err
		}
		if !publicProjection(after) {
			return c.deleteESProjection(locked, payload.TweetID)
		}
		if domain.ProjectionDigest(after) != doc.ProjectionDigest {
			return errors.New("text fact changed")
		}
		return nil
	})
}

func (c *TimelineConsumer) reconcileVectorProjection(ctx context.Context, payload domain.ProjectionTaskPayload) error {
	if c.projectionRepo == nil {
		return errors.New("vector fact reader unavailable")
	}
	var initial *domain.Tweet
	err := c.projectionRepo.WithProjectionLock(ctx, payload.TweetID, func(locked context.Context) error {
		var err error
		initial, err = c.projectionRepo.ReadProjectionTweet(locked, payload.TweetID)
		if err != nil {
			return err
		}
		if !publicProjection(initial) {
			return c.deleteVectorProjection(locked, payload.TweetID)
		}
		if initial.ID != payload.TweetID || initial.UserID != payload.AuthorID {
			return errors.New("projection identity mismatch")
		}
		return nil
	})
	if err != nil || !publicProjection(initial) {
		return err
	}
	profile := c.currentEmbeddingProfile()
	if !payload.Embedding.Valid() || payload.Embedding != profile {
		return errors.New("vector embedding profile changed or unavailable")
	}
	// No Tweet lock during expensive model work, so BM25 can progress independently.
	var vector []float32
	if c.embeddingExecutor != nil {
		vector, err = c.embeddingExecutor(ctx, initial.Content, profile.Model)
	} else if c.aiClient == nil {
		err = errors.New("embedding unavailable")
	} else {
		vector, err = c.aiClient.GetEmbedding(ctx, initial.Content, profile.Model)
	}
	if err != nil {
		return errors.New("embedding execution failed")
	}
	if len(vector) != profile.Dimension {
		return errors.New("embedding dimension mismatch")
	}
	for _, number := range vector {
		if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
			return errors.New("invalid embedding value")
		}
	}
	return c.projectionRepo.WithProjectionLock(ctx, payload.TweetID, func(locked context.Context) error {
		current, err := c.projectionRepo.ReadProjectionTweet(locked, payload.TweetID)
		if err != nil {
			return err
		}
		if !publicProjection(current) {
			return c.deleteVectorProjection(locked, payload.TweetID)
		}
		if domain.ProjectionDigest(current) != domain.ProjectionDigest(initial) {
			return errors.New("vector fact changed during embedding")
		}
		metadata := map[string]interface{}{"user_id": fmt.Sprint(current.UserID), "parent_id": fmt.Sprint(current.ParentID), "content": "Document: " + current.Content, "type": current.Type, "visible_type": current.VisibleType, "created_at": current.CreatedAt,
			"projection_digest": domain.ProjectionDigest(current), "projection_version": domain.ProjectionVersion, "embedding_profile": profile.Digest(), "embedding_model": profile.Model, "embedding_dimension": profile.Dimension, "embedding_version": profile.Version}
		if payload.RepairOperation != "" {
			if err := c.deleteVectorProjection(locked, current.ID); err != nil {
				return err
			}
		}
		if c.vectorProjectionExecutor != nil {
			err = c.vectorProjectionExecutor(locked, current.ID, vector, metadata)
		} else if c.qdrantClient == nil {
			err = errors.New("vector writer unavailable")
		} else {
			err = c.qdrantClient.UpsertTweetProjection(locked, current.ID, vector, metadata)
		}
		if err != nil {
			return err
		}
		after, err := c.projectionRepo.ReadProjectionTweet(locked, payload.TweetID)
		if err != nil {
			return err
		}
		if !publicProjection(after) {
			return c.deleteVectorProjection(locked, payload.TweetID)
		}
		if domain.ProjectionDigest(after) != domain.ProjectionDigest(current) {
			return errors.New("vector fact changed during write")
		}
		return nil
	})
}
