package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"twitter-clone/internal/events"
	"twitter-clone/internal/module/agent/attribution"
	agentService "twitter-clone/internal/module/agent/service"
)

const (
	contentEngagementExchange      = "twitter.events"
	contentEngagementRetryExchange = "agent.profile.retry"
	contentEngagementDLX           = "agent.profile.dlx"

	contentEngagementQueue        = "queue.agent.profile.content-engagement.v1"
	contentEngagementLikeRetry    = "queue.agent.profile.content-engagement.like.retry.v1"
	contentEngagementCommentRetry = "queue.agent.profile.content-engagement.comment.retry.v1"
	contentEngagementDLQ          = "queue.agent.profile.content-engagement.dlq.v1"

	routingTweetLiked    = "tweet.liked"
	routingCommentCreate = "comment.created"

	contentEngagementConsumerName = "agent-profile-content-engagement-v1"
	contentEngagementPrefetch     = 16
	contentEngagementMaxRetries   = 3
	contentEngagementTimeout      = 5 * time.Second
)

type contentEngagementBroker interface {
	DeclareExchange(name, kind string, durable bool) error
	DeclareQueue(name string, durable bool) (amqp.Queue, error)
	DeclareQueueWithArgs(name string, durable bool, args amqp.Table) (amqp.Queue, error)
	BindQueue(queueName, routingKey, exchangeName string) error
	SetQoS(prefetchCount int) error
	EnablePublisherConfirms() error
	Consume(queueName, consumer string) (<-chan amqp.Delivery, error)
	PublishMessageConfirmed(ctx context.Context, exchange, routingKey string, message amqp.Publishing) error
}

type contentEngagementProcessor interface {
	Process(ctx context.Context, event attribution.ContentEngagement) (string, error)
}

type ContentEngagementObserver interface {
	Observe(kind, result string)
}

type noopContentEngagementObserver struct{}

func (noopContentEngagementObserver) Observe(string, string) {}

type ContentEngagementConsumer struct {
	broker    contentEngagementBroker
	processor contentEngagementProcessor
	observer  ContentEngagementObserver
	wait      func(context.Context, time.Duration) bool
}

func NewContentEngagementConsumer(
	broker contentEngagementBroker,
	processor contentEngagementProcessor,
	observer ContentEngagementObserver,
) (*ContentEngagementConsumer, error) {
	if broker == nil || processor == nil {
		return nil, errors.New("content engagement broker and processor are required")
	}
	if observer == nil {
		observer = noopContentEngagementObserver{}
	}
	consumer := &ContentEngagementConsumer{
		broker: broker, processor: processor, observer: observer, wait: waitContentEngagementDelay,
	}
	if err := consumer.declareTopology(); err != nil {
		return nil, err
	}
	if err := broker.EnablePublisherConfirms(); err != nil {
		return nil, fmt.Errorf("enable content engagement publisher confirms: %w", err)
	}
	return consumer, nil
}

func (c *ContentEngagementConsumer) declareTopology() error {
	for _, exchange := range []string{contentEngagementExchange, contentEngagementRetryExchange, contentEngagementDLX} {
		if err := c.broker.DeclareExchange(exchange, "topic", true); err != nil {
			return fmt.Errorf("declare content engagement exchange %s: %w", exchange, err)
		}
	}
	if _, err := c.broker.DeclareQueue(contentEngagementQueue, true); err != nil {
		return fmt.Errorf("declare content engagement queue: %w", err)
	}
	if _, err := c.broker.DeclareQueue(contentEngagementDLQ, true); err != nil {
		return fmt.Errorf("declare content engagement dlq: %w", err)
	}
	retryQueues := []struct {
		name       string
		routingKey string
	}{
		{name: contentEngagementLikeRetry, routingKey: routingTweetLiked},
		{name: contentEngagementCommentRetry, routingKey: routingCommentCreate},
	}
	for _, retry := range retryQueues {
		args := amqp.Table{
			"x-dead-letter-exchange":    contentEngagementExchange,
			"x-dead-letter-routing-key": retry.routingKey,
		}
		if _, err := c.broker.DeclareQueueWithArgs(retry.name, true, args); err != nil {
			return fmt.Errorf("declare %s: %w", retry.name, err)
		}
		if err := c.broker.BindQueue(retry.name, retryRoutingKey(retry.routingKey), contentEngagementRetryExchange); err != nil {
			return fmt.Errorf("bind %s: %w", retry.name, err)
		}
	}
	for _, routingKey := range []string{routingTweetLiked, routingCommentCreate} {
		if err := c.broker.BindQueue(contentEngagementQueue, routingKey, contentEngagementExchange); err != nil {
			return fmt.Errorf("bind content engagement routing key %s: %w", routingKey, err)
		}
		if err := c.broker.BindQueue(contentEngagementDLQ, dlqRoutingKey(routingKey), contentEngagementDLX); err != nil {
			return fmt.Errorf("bind content engagement dlq routing key %s: %w", routingKey, err)
		}
	}
	if err := c.broker.SetQoS(contentEngagementPrefetch); err != nil {
		return fmt.Errorf("configure content engagement qos: %w", err)
	}
	return nil
}

func (c *ContentEngagementConsumer) Run(ctx context.Context) error {
	messages, err := c.broker.Consume(contentEngagementQueue, contentEngagementConsumerName)
	if err != nil {
		return fmt.Errorf("consume content engagement queue: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case message, ok := <-messages:
			if !ok {
				return errors.New("content engagement delivery channel closed")
			}
			c.handle(ctx, message)
		}
	}
}

func (c *ContentEngagementConsumer) handle(ctx context.Context, message amqp.Delivery) {
	event, err := decodeContentEngagement(message)
	if err != nil {
		c.observer.Observe("unknown", "malformed")
		c.routeFailure(ctx, message, true, err)
		return
	}
	processCtx, cancel := context.WithTimeout(ctx, contentEngagementTimeout)
	result, err := c.processor.Process(processCtx, event)
	cancel()
	if err != nil {
		c.observer.Observe(event.Kind, "failed")
		c.routeFailure(ctx, message, agentService.IsPermanentContentEngagementError(err), err)
		return
	}
	c.observer.Observe(event.Kind, result)
	if err := message.Ack(false); err != nil {
		slog.WarnContext(ctx, "ack content engagement event failed", "kind", event.Kind, "error", err)
	}
}

func decodeContentEngagement(message amqp.Delivery) (attribution.ContentEngagement, error) {
	occurredAt := message.Timestamp
	switch message.RoutingKey {
	case routingTweetLiked:
		var event events.TweetLikedEvent
		if err := json.Unmarshal(message.Body, &event); err != nil {
			return attribution.ContentEngagement{}, fmt.Errorf("decode tweet liked event: %w", err)
		}
		if event.OccurredAtUnixMS > 0 {
			occurredAt = time.UnixMilli(event.OccurredAtUnixMS)
		}
		if occurredAt.IsZero() {
			occurredAt = time.Now()
		}
		return attribution.ContentEngagement{
			EventID: fmt.Sprintf("like:%d:%d", event.TweetID, event.UserID),
			Kind:    attribution.EngagementKindLike, TweetID: event.TweetID,
			ActorUserID: event.UserID, AuthorUserID: event.TweetUser, OccurredAt: occurredAt,
		}, nil
	case routingCommentCreate:
		var event events.CommentCreatedEvent
		if err := json.Unmarshal(message.Body, &event); err != nil {
			return attribution.ContentEngagement{}, fmt.Errorf("decode comment created event: %w", err)
		}
		if event.OccurredAtUnixMS > 0 {
			occurredAt = time.UnixMilli(event.OccurredAtUnixMS)
		}
		if occurredAt.IsZero() {
			occurredAt = time.Now()
		}
		return attribution.ContentEngagement{
			EventID: fmt.Sprintf("comment:%d", event.CommentID),
			Kind:    attribution.EngagementKindComment, TweetID: event.TweetID,
			ActorUserID: event.UserID, AuthorUserID: event.TweetUser, OccurredAt: occurredAt,
		}, nil
	default:
		return attribution.ContentEngagement{}, fmt.Errorf("unsupported content engagement routing key %q", message.RoutingKey)
	}
}

func (c *ContentEngagementConsumer) routeFailure(ctx context.Context, message amqp.Delivery, permanent bool, cause error) {
	retryCount, validRetryCount := contentEngagementRetryCount(message.Headers)
	if !validRetryCount {
		permanent = true
		retryCount = contentEngagementMaxRetries
	}
	exchange, routingKey, result := contentEngagementRetryExchange, retryRoutingKey(message.RoutingKey), "retried"
	messageToPublish := copyContentEngagementMessage(message)
	if permanent || retryCount >= contentEngagementMaxRetries {
		exchange, routingKey, result = contentEngagementDLX, dlqRoutingKey(message.RoutingKey), "dlq"
		messageToPublish.Expiration = ""
	} else {
		retryCount++
		messageToPublish.Headers["x-agent-profile-retry-count"] = int32(retryCount)
		messageToPublish.Expiration = strconv.FormatInt(int64(time.Second/time.Millisecond)*(1<<uint(retryCount-1)), 10)
	}
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), contentEngagementTimeout)
	err := c.broker.PublishMessageConfirmed(publishCtx, exchange, routingKey, messageToPublish)
	cancel()
	if err != nil {
		c.observer.Observe(contentEngagementKindForRoutingKey(message.RoutingKey), "publish_failed")
		slog.ErrorContext(ctx, "route failed content engagement event failed", "result", result, "error", err, "cause", cause)
		if c.wait != nil {
			c.wait(ctx, contentEngagementFallbackDelay(retryCount))
		}
		if nackErr := message.Nack(false, true); nackErr != nil {
			slog.ErrorContext(ctx, "fallback requeue content engagement event failed", "error", nackErr)
			return
		}
		c.observer.Observe(contentEngagementKindForRoutingKey(message.RoutingKey), "requeued")
		return
	}
	c.observer.Observe(contentEngagementKindForRoutingKey(message.RoutingKey), result)
	slog.WarnContext(ctx, "content engagement event processing deferred",
		"routing_key", message.RoutingKey, "result", result, "retry_count", retryCount, "error", cause,
	)
	if err := message.Ack(false); err != nil {
		c.observer.Observe(contentEngagementKindForRoutingKey(message.RoutingKey), "acknowledgement_uncertain")
		slog.WarnContext(ctx, "ack deferred content engagement event failed", "error", err)
	}
}

func copyContentEngagementMessage(message amqp.Delivery) amqp.Publishing {
	headers := amqp.Table{}
	for key, value := range message.Headers {
		headers[key] = value
	}
	return amqp.Publishing{
		Headers: headers, ContentType: message.ContentType, DeliveryMode: amqp.Persistent,
		MessageId: message.MessageId, CorrelationId: message.CorrelationId,
		Timestamp: message.Timestamp, Body: append([]byte(nil), message.Body...),
	}
}

func contentEngagementRetryCount(headers amqp.Table) (int, bool) {
	return contentEngagementHeaderCount(headers, "x-agent-profile-retry-count", contentEngagementMaxRetries)
}

func contentEngagementHeaderCount(headers amqp.Table, key string, maximum int) (int, bool) {
	if headers == nil {
		return 0, true
	}
	value, exists := headers[key]
	if !exists {
		return 0, true
	}
	var count int64
	switch typed := value.(type) {
	case int8:
		count = int64(typed)
	case int16:
		count = int64(typed)
	case int32:
		count = int64(typed)
	case int64:
		count = typed
	case int:
		count = int64(typed)
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 32)
		if err != nil {
			return 0, false
		}
		count = parsed
	default:
		return 0, false
	}
	if count < 0 || count > int64(maximum) {
		return 0, false
	}
	return int(count), true
}

func contentEngagementFallbackDelay(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount > 2 {
		retryCount = 2
	}
	return time.Second * time.Duration(1<<uint(retryCount))
}

func waitContentEngagementDelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func retryRoutingKey(routingKey string) string { return routingKey + ".agent-profile.retry" }
func dlqRoutingKey(routingKey string) string   { return routingKey + ".agent-profile.dlq" }

func contentEngagementKindForRoutingKey(routingKey string) string {
	switch routingKey {
	case routingTweetLiked:
		return attribution.EngagementKindLike
	case routingCommentCreate:
		return attribution.EngagementKindComment
	default:
		return "unknown"
	}
}
