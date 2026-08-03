package consumer

import (
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ExchangeTimelineIngress = "timeline.ingress"
	ExchangeTimelineRetry   = "timeline.retry"

	QueueTweetFanoutRetryV2             = "queue.tweet.fanout.retry.v2"
	QueueTweetDeleteRetryV2             = "queue.tweet.delete.retry.v2"
	QueueTweetModerationCleanupRetryV2  = "queue.tweet.moderation.cleanup.retry.v2"
	RoutingKeyTimelineTweetCreated      = "tweet.created.timeline"
	RoutingKeyTimelineTweetDeleted      = "tweet.deleted.timeline"
	RoutingKeyTimelineTweetModerated    = "tweet.moderated.timeline"
	RoutingKeyTimelineTweetCreatedRetry = "tweet.created.timeline.retry"
	RoutingKeyTimelineTweetDeletedRetry = "tweet.deleted.timeline.retry"
	RoutingKeyTimelineModeratedRetry    = "tweet.moderated.timeline.retry"
)

type timelineRecoveryTopologyBroker interface {
	DeclareExchange(name, kind string, durable bool) error
	DeclareQueue(name string, durable bool) (amqp.Queue, error)
	DeclareQueueWithArgs(name string, durable bool, args amqp.Table) (amqp.Queue, error)
	BindQueue(queueName, routingKey, exchangeName string) error
}

type timelineRecoveryRoute struct {
	sourceRoutingKey  string
	ingressRoutingKey string
	retryRoutingKey   string
	queue             string
	retryQueue        string
}

var timelineRecoveryRoutes = []timelineRecoveryRoute{
	{
		sourceRoutingKey:  RoutingKeyTweetCreated,
		ingressRoutingKey: RoutingKeyTimelineTweetCreated,
		retryRoutingKey:   RoutingKeyTimelineTweetCreatedRetry,
		queue:             QueueTweetFanout,
		retryQueue:        QueueTweetFanoutRetryV2,
	},
	{
		sourceRoutingKey:  RoutingKeyTweetDeleted,
		ingressRoutingKey: RoutingKeyTimelineTweetDeleted,
		retryRoutingKey:   RoutingKeyTimelineTweetDeletedRetry,
		queue:             QueueTweetDelete,
		retryQueue:        QueueTweetDeleteRetryV2,
	},
	{
		sourceRoutingKey:  RoutingKeyTweetModerated,
		ingressRoutingKey: RoutingKeyTimelineTweetModerated,
		retryRoutingKey:   RoutingKeyTimelineModeratedRetry,
		queue:             QueueTweetModerationCleanup,
		retryQueue:        QueueTweetModerationCleanupRetryV2,
	},
}

// DeclareTimelineRecoveryTopology creates only additive, versioned recovery
// resources. Legacy retry queues remain untouched so in-flight messages can
// drain during a rolling upgrade.
func DeclareTimelineRecoveryTopology(broker timelineRecoveryTopologyBroker) error {
	if broker == nil {
		return errors.New("timeline recovery topology broker is required")
	}
	if err := broker.DeclareExchange(ExchangeTimelineIngress, "topic", true); err != nil {
		return fmt.Errorf("declare timeline ingress exchange: %w", err)
	}
	if err := broker.DeclareExchange(ExchangeTimelineRetry, "topic", true); err != nil {
		return fmt.Errorf("declare timeline retry exchange: %w", err)
	}

	for _, route := range timelineRecoveryRoutes {
		if _, err := broker.DeclareQueue(route.queue, true); err != nil {
			return fmt.Errorf("declare timeline recovery target queue %s: %w", route.queue, err)
		}
		if err := broker.BindQueue(route.queue, route.ingressRoutingKey, ExchangeTimelineIngress); err != nil {
			return fmt.Errorf(
				"bind timeline recovery target %s to %s/%s: %w",
				route.queue,
				ExchangeTimelineIngress,
				route.ingressRoutingKey,
				err,
			)
		}

		retryArgs := amqp.Table{
			"x-dead-letter-exchange":    ExchangeTimelineIngress,
			"x-dead-letter-routing-key": route.ingressRoutingKey,
		}
		if _, err := broker.DeclareQueueWithArgs(route.retryQueue, true, retryArgs); err != nil {
			return fmt.Errorf("declare timeline retry queue %s: %w", route.retryQueue, err)
		}
		if err := broker.BindQueue(route.retryQueue, route.retryRoutingKey, ExchangeTimelineRetry); err != nil {
			return fmt.Errorf(
				"bind timeline retry queue %s to %s/%s: %w",
				route.retryQueue,
				ExchangeTimelineRetry,
				route.retryRoutingKey,
				err,
			)
		}
	}
	return nil
}

func timelineRecoveryRouteForSource(routingKey string) (timelineRecoveryRoute, bool) {
	for _, route := range timelineRecoveryRoutes {
		if route.sourceRoutingKey == routingKey {
			return route, true
		}
	}
	return timelineRecoveryRoute{}, false
}
