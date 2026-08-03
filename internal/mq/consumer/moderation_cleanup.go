package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	amqp "github.com/rabbitmq/amqp091-go"

	"twitter-clone/internal/events"
)

const (
	moderationCleanupPageSize     = 500
	moderationProgressTTL         = 7 * 24 * time.Hour
	moderationCompletionMarkerTTL = 30 * 24 * time.Hour
	moderationProgressKeyPrefix   = "timeline:moderation:cleanup:cursor:"
	moderationCompletionKeyPrefix = "timeline:moderation:cleanup:done:"
)

type failureDisposition string

const (
	failureDispositionRetried      failureDisposition = "retried"
	failureDispositionDLQ          failureDisposition = "dlq"
	failureDispositionRequeued     failureDisposition = "requeued"
	failureDispositionAckUncertain failureDisposition = "acknowledgement_uncertain"
)

type moderationCleanupStats struct {
	Pages            int
	FollowersScanned int
	TimelinesRemoved int
	AlreadyCompleted bool
}

func (c *TimelineConsumer) SetModerationCleanupObserver(observer ModerationCleanupObserver) {
	if observer == nil {
		c.moderationObserver = noopModerationCleanupObserver{}
		return
	}
	c.moderationObserver = observer
}

func (c *TimelineConsumer) consumeModerationCleanup(ctx context.Context) {
	messages, err := c.mq.Consume(QueueTweetModerationCleanup, ConsumerName+"-moderation-cleanup")
	if err != nil {
		log.Printf("failed to consume moderation cleanup queue: %v", err)
		return
	}
	log.Println("Listening for tweet.moderated events...")

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				if !waitForModerationReconnect(ctx) {
					return
				}
				messages, err = c.mq.Consume(QueueTweetModerationCleanup, ConsumerName+"-moderation-cleanup")
				if err != nil {
					log.Printf("failed to reconnect moderation cleanup consumer: %v", err)
					continue
				}
				continue
			}
			c.handleModerationCleanupMessage(ctx, msg)
		}
	}
}

func waitForModerationReconnect(ctx context.Context) bool {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *TimelineConsumer) handleModerationCleanupMessage(ctx context.Context, msg amqp.Delivery) {
	startedAt := time.Now()
	observer := c.moderationCleanupObserver()
	observer.ObserveEvent("received")

	var event events.TweetModeratedEvent
	if err := json.Unmarshal(msg.Body, &event); err != nil || event.Validate() != nil {
		observer.ObserveEvent("malformed")
		disposition := c.routeModerationMessageToDLQ(msg)
		observer.ObserveEvent(string(disposition))
		observer.ObserveDuration("failed", time.Since(startedAt))
		return
	}

	stats, err := c.cleanupModeratedTweet(ctx, event)
	if err != nil {
		if ctx.Err() != nil {
			_ = msg.Nack(false, true)
			observer.ObserveEvent(string(failureDispositionRequeued))
		} else {
			disposition := c.handleFailure(msg, RoutingKeyTweetModerated)
			observer.ObserveEvent(string(disposition))
		}
		observer.ObserveDuration("failed", time.Since(startedAt))
		log.Printf("tweet moderation cleanup failed: event_key=%s error=%v", event.EventKey, err)
		return
	}

	if err := msg.Ack(false); err != nil {
		log.Printf("failed to ack tweet moderation cleanup: event_key=%s error=%v", event.EventKey, err)
	}
	result := "completed"
	if stats.AlreadyCompleted {
		result = "duplicate"
	}
	observer.ObserveEvent(result)
	observer.ObserveDuration(result, time.Since(startedAt))
	log.Printf(
		"tweet moderation cleanup %s: event_key=%s pages=%d followers_scanned=%d timelines_removed=%d",
		result,
		event.EventKey,
		stats.Pages,
		stats.FollowersScanned,
		stats.TimelinesRemoved,
	)
}

func (c *TimelineConsumer) routeModerationMessageToDLQ(msg amqp.Delivery) failureDisposition {
	return c.handlePermanentFailure(msg, RoutingKeyTweetModerated)
}

func (c *TimelineConsumer) cleanupModeratedTweet(ctx context.Context, event events.TweetModeratedEvent) (moderationCleanupStats, error) {
	if err := event.Validate(); err != nil {
		return moderationCleanupStats{}, err
	}
	if c.redisClient == nil || c.timelineCache == nil || c.followerPager == nil {
		return moderationCleanupStats{}, errors.New("moderation cleanup dependencies are unavailable")
	}

	doneKey := moderationCompletionKeyPrefix + event.EventKey
	done, err := c.redisClient.Get(ctx, doneKey).Result()
	if err == nil && done == "1" {
		return moderationCleanupStats{AlreadyCompleted: true}, nil
	}
	if err != nil && err != redis.Nil {
		return moderationCleanupStats{}, fmt.Errorf("read moderation completion marker: %w", err)
	}

	if err := c.timelineCache.InvalidateBaseTweet(ctx, event.TweetID); err != nil {
		return moderationCleanupStats{}, fmt.Errorf("invalidate moderated tweet cache: %w", err)
	}
	if err := c.timelineCache.RemoveFromUserTimeline(ctx, event.AuthorID, event.TweetID); err != nil {
		return moderationCleanupStats{}, fmt.Errorf("remove moderated tweet from author timeline: %w", err)
	}

	progressKey := moderationProgressKeyPrefix + event.EventKey
	cursor, err := c.loadModerationCleanupCursor(ctx, progressKey)
	if err != nil {
		return moderationCleanupStats{}, err
	}

	stats := moderationCleanupStats{}
	observer := c.moderationCleanupObserver()
	for {
		page, listErr := c.followerPager.ListFollowerPage(ctx, event.AuthorID, cursor, moderationCleanupPageSize)
		if listErr != nil {
			return stats, fmt.Errorf("list follower cleanup page: %w", listErr)
		}
		if page.HasMore && (len(page.FollowerIDs) == 0 || page.NextCursor == 0 || (cursor > 0 && page.NextCursor >= cursor)) {
			return stats, errors.New("invalid follower cleanup cursor progression")
		}

		removed, cleanupErr := c.timelineCache.BatchRemoveFromTimelineAndUnread(ctx, page.FollowerIDs, event.TweetID)
		if cleanupErr != nil {
			observer.ObservePage("failed", len(page.FollowerIDs), removed)
			return stats, cleanupErr
		}
		stats.Pages++
		stats.FollowersScanned += len(page.FollowerIDs)
		stats.TimelinesRemoved += removed
		observer.ObservePage("completed", len(page.FollowerIDs), removed)

		if !page.HasMore {
			break
		}
		if err := c.redisClient.Set(ctx, progressKey, strconv.FormatUint(page.NextCursor, 10), moderationProgressTTL).Err(); err != nil {
			return stats, fmt.Errorf("persist moderation cleanup cursor: %w", err)
		}
		cursor = page.NextCursor
	}

	pipe := c.redisClient.TxPipeline()
	pipe.Set(ctx, doneKey, "1", moderationCompletionMarkerTTL)
	pipe.Del(ctx, progressKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return stats, fmt.Errorf("commit moderation completion marker: %w", err)
	}
	return stats, nil
}

func (c *TimelineConsumer) moderationCleanupObserver() ModerationCleanupObserver {
	if c == nil || c.moderationObserver == nil {
		return noopModerationCleanupObserver{}
	}
	return c.moderationObserver
}

func (c *TimelineConsumer) loadModerationCleanupCursor(ctx context.Context, progressKey string) (uint64, error) {
	raw, err := c.redisClient.Get(ctx, progressKey).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read moderation cleanup cursor: %w", err)
	}
	cursor, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || cursor == 0 {
		return 0, errors.New("invalid persisted moderation cleanup cursor")
	}
	return cursor, nil
}
