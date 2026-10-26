package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

const (
	deleteESOutboxTaskType     = "delete_es"
	deleteVectorOutboxTaskType = "delete_vector"
)

func (c *TimelineConsumer) SetProjectionRepository(repo domain.TweetProjectionRepository) {
	c.projectionRepo = repo
}

func (c *TimelineConsumer) enqueueProjectionDeletion(ctx context.Context, event events.TweetDeletedEvent) error {
	if event.TweetID == 0 || event.AuthorID == 0 || c.outboxRepo == nil || c.newOutboxTaskID == nil {
		return errors.New("projection deletion enqueue unavailable")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	for _, taskType := range []string{deleteESOutboxTaskType, deleteVectorOutboxTaskType} {
		id, err := c.newOutboxTaskID()
		if err != nil {
			return errors.New("projection task identity unavailable")
		}
		key := fmt.Sprintf("timeline:%s:tweet:%d:v1", taskType, event.TweetID)
		if _, err := c.outboxRepo.CreateIdempotent(ctx, &domain.OutboxTask{
			ID: id, DedupKey: &key, TaskType: taskType, Payload: string(payload), MaxRetries: 5,
		}); err != nil {
			return errors.New("durable projection deletion enqueue failed")
		}
	}
	return nil
}

func (c *TimelineConsumer) reconcileSearchProjection(ctx context.Context, event *events.TweetCreatedEvent) error {
	if c.projectionRepo == nil || event == nil || event.TweetID == 0 || event.AuthorID == 0 {
		return errors.New("current projection reader unavailable")
	}
	return c.projectionRepo.WithProjectionLock(ctx, event.TweetID, func(locked context.Context) error {
		current, err := c.projectionRepo.ReadProjectionTweet(locked, event.TweetID)
		if err != nil {
			return err
		}
		if !publicProjection(current) {
			return c.deleteBothProjections(locked, event.TweetID)
		}
		if current.ID != event.TweetID || current.UserID != event.AuthorID {
			return errors.New("projection identity mismatch")
		}
		fresh := &events.TweetCreatedEvent{TweetID: current.ID, AuthorID: current.UserID,
			ParentID: current.ParentID, Content: current.Content, Type: current.Type,
			VisibleType: current.VisibleType, CreatedAt: current.CreatedAt}
		executor := c.executeESIndex
		if c.projectionIndexExecutor != nil {
			executor = c.projectionIndexExecutor
		}
		if err := executor(locked, fresh); err != nil {
			return err
		}
		// Visibility/deletion can change during model/HTTP work. Recheck after
		// acknowledged writes while keeping competing projections serialized.
		after, err := c.projectionRepo.ReadProjectionTweet(locked, event.TweetID)
		if err != nil {
			return err
		}
		if !publicProjection(after) {
			return c.deleteBothProjections(locked, event.TweetID)
		}
		if after.ID != current.ID || after.UserID != current.UserID || after.UpdatedAt != current.UpdatedAt || after.Content != current.Content {
			return errors.New("projection fact changed during write")
		}
		return nil
	})
}

func publicProjection(tweet *domain.Tweet) bool {
	return tweet != nil && tweet.DeletedAt == 0 && tweet.VisibleType == domain.VisiblePublic
}

func (c *TimelineConsumer) executeProjectionDeletion(ctx context.Context, id uint64, taskType string) error {
	if c.projectionRepo == nil || id == 0 {
		return errors.New("projection deletion reader unavailable")
	}
	return c.projectionRepo.WithProjectionLock(ctx, id, func(locked context.Context) error {
		current, err := c.projectionRepo.ReadProjectionTweet(locked, id)
		if err != nil {
			return err
		}
		if publicProjection(current) {
			return errors.New("deletion is not current")
		}
		if taskType == deleteESOutboxTaskType {
			return c.deleteESProjection(locked, id)
		}
		if taskType == deleteVectorOutboxTaskType {
			return c.deleteVectorProjection(locked, id)
		}
		return errors.New("unknown deletion projection")
	})
}

func (c *TimelineConsumer) deleteBothProjections(ctx context.Context, id uint64) error {
	return errors.Join(c.deleteESProjection(ctx, id), c.deleteVectorProjection(ctx, id))
}
func (c *TimelineConsumer) deleteESProjection(ctx context.Context, id uint64) error {
	if c.deleteESExecutor != nil {
		return c.deleteESExecutor(ctx, id)
	}
	if c.esClient == nil {
		return errors.New("ES projection unavailable")
	}
	return c.esClient.DeleteTweet(ctx, fmt.Sprintf("%d", id))
}
func (c *TimelineConsumer) deleteVectorProjection(ctx context.Context, id uint64) error {
	if c.deleteVectorExecutor != nil {
		return c.deleteVectorExecutor(ctx, id)
	}
	if c.qdrantClient == nil {
		return errors.New("vector projection unavailable")
	}
	return c.qdrantClient.DeleteTweet(ctx, id)
}
