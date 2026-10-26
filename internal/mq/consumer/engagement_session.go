package consumer

import (
	"context"
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"time"
	"twitter-clone/internal/infrastructure/mq"
)

type engagementSession struct {
	ctx      context.Context
	messages <-chan amqp.Delivery
	router   *timelineFailureRouter
	close    func()
}

// Each queue owns its consumer and Confirm connection; replacing one session
// cannot change the channel still used by lifecycle consumers or another queue.
func (c *TimelineConsumer) SetEngagementConnectionConfig(config mq.RabbitMQConfig) {
	c.engagementSessionFactory = func(ctx context.Context, queue, kind string) (*engagementSession, error) {
		return openEngagementSession(ctx, config, queue, kind)
	}
}
func DeclareEngagementRecoveryTopology(broker timelineRecoveryTopologyBroker) error {
	if broker == nil {
		return errors.New("engagement broker required")
	}
	for _, exchange := range []string{ExchangeEvents, ExchangeDLX, ExchangeTimelineIngress, ExchangeTimelineRetry} {
		if err := broker.DeclareExchange(exchange, "topic", true); err != nil {
			return errors.New("engagement exchange unavailable")
		}
	}
	for _, kind := range []string{RoutingKeyTweetLiked, RoutingKeyCommentCreated} {
		route, _ := timelineRecoveryRouteForSource(kind)
		dlq := QueueTweetLikedDLQ
		if kind == RoutingKeyCommentCreated {
			dlq = QueueCommentCreatedDLQ
		}
		for _, queue := range []string{route.queue, dlq} {
			if _, err := broker.DeclareQueue(queue, true); err != nil {
				return errors.New("engagement queue unavailable")
			}
		}
		for _, binding := range []struct{ queue, key, exchange string }{{route.queue, kind, ExchangeEvents}, {route.queue, route.ingressRoutingKey, ExchangeTimelineIngress}, {dlq, kind + ".dlq", ExchangeDLX}} {
			if err := broker.BindQueue(binding.queue, binding.key, binding.exchange); err != nil {
				return errors.New("engagement binding unavailable")
			}
		}
		args := amqp.Table{"x-dead-letter-exchange": ExchangeTimelineIngress, "x-dead-letter-routing-key": route.ingressRoutingKey}
		if _, err := broker.DeclareQueueWithArgs(route.retryQueue, true, args); err != nil {
			return errors.New("engagement retry unavailable")
		}
		if err := broker.BindQueue(route.retryQueue, route.retryRoutingKey, ExchangeTimelineRetry); err != nil {
			return errors.New("engagement retry binding unavailable")
		}
	}
	return nil
}
func openEngagementSession(ctx context.Context, config mq.RabbitMQConfig, queue, kind string) (*engagementSession, error) {
	route, ok := timelineRecoveryRouteForSource(kind)
	if !ok || route.queue != queue || (kind != RoutingKeyTweetLiked && kind != RoutingKeyCommentCreated) {
		return nil, errors.New("invalid engagement session")
	}
	setup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	reader, err := mq.NewRabbitMQWithContext(setup, &config)
	if err != nil {
		return nil, errors.New("engagement reader unavailable")
	}
	stopReader, err := reader.CloseOnCancellation(setup)
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	defer stopReader()
	publisher, err := mq.NewRabbitMQWithContext(setup, &config)
	if err != nil {
		_ = reader.Close()
		return nil, errors.New("engagement publisher unavailable")
	}
	stopPublisher, err := publisher.CloseOnCancellation(setup)
	if err != nil {
		_ = reader.Close()
		_ = publisher.Close()
		return nil, err
	}
	defer stopPublisher()
	success := false
	defer func() {
		if !success {
			_ = reader.Close()
			_ = publisher.Close()
		}
	}()
	if err := DeclareEngagementRecoveryTopology(reader); err != nil {
		return nil, err
	}
	if err := reader.SetQoS(1); err != nil {
		return nil, errors.New("engagement qos unavailable")
	}
	router, err := newTimelineFailureRouter(publisher)
	if err != nil {
		return nil, errors.New("engagement confirms unavailable")
	}
	messages, err := reader.Consume(queue, ConsumerName+"-"+kind)
	if err != nil {
		return nil, errors.New("engagement subscription unavailable")
	}
	if err := setup.Err(); err != nil {
		return nil, err
	}
	stopReader()
	stopPublisher()
	lifetime, stopLifetime := context.WithCancel(ctx)
	lifetimeReader, err := reader.CloseOnCancellation(lifetime)
	if err != nil {
		stopLifetime()
		return nil, err
	}
	lifetimePublisher, err := publisher.CloseOnCancellation(lifetime)
	if err != nil {
		stopLifetime()
		lifetimeReader()
		return nil, err
	}
	publisherClosed := publisher.GetChannel().NotifyClose(make(chan *amqp.Error, 1))
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-lifetime.Done():
		case <-publisherClosed:
			stopLifetime()
		}
	}()
	success = true
	return &engagementSession{ctx: lifetime, messages: messages, router: router, close: func() {
		stopLifetime()
		_ = reader.Close()
		_ = publisher.Close()
		<-watchDone
		lifetimeReader()
		lifetimePublisher()
	}}, nil
}
