package consumer

import (
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type timelineTopologyBinding struct {
	queue      string
	routingKey string
	exchange   string
}

type timelineRecoveryTopologyFake struct {
	exchanges map[string]string
	queues    map[string]amqp.Table
	bindings  []timelineTopologyBinding
}

func newTimelineRecoveryTopologyFake() *timelineRecoveryTopologyFake {
	return &timelineRecoveryTopologyFake{
		exchanges: make(map[string]string),
		queues:    make(map[string]amqp.Table),
	}
}

func (b *timelineRecoveryTopologyFake) DeclareExchange(name, kind string, _ bool) error {
	b.exchanges[name] = kind
	return nil
}

func (b *timelineRecoveryTopologyFake) DeclareQueue(name string, _ bool) (amqp.Queue, error) {
	b.queues[name] = nil
	return amqp.Queue{Name: name}, nil
}

func (b *timelineRecoveryTopologyFake) DeclareQueueWithArgs(
	name string,
	_ bool,
	args amqp.Table,
) (amqp.Queue, error) {
	copyArgs := make(amqp.Table, len(args))
	for key, value := range args {
		copyArgs[key] = value
	}
	b.queues[name] = copyArgs
	return amqp.Queue{Name: name}, nil
}

func (b *timelineRecoveryTopologyFake) BindQueue(queueName, routingKey, exchangeName string) error {
	b.bindings = append(b.bindings, timelineTopologyBinding{
		queue: queueName, routingKey: routingKey, exchange: exchangeName,
	})
	return nil
}

func TestDeclareTimelineRecoveryTopologyUsesDedicatedVersionedRoutes(t *testing.T) {
	broker := newTimelineRecoveryTopologyFake()
	if err := DeclareTimelineRecoveryTopology(broker); err != nil {
		t.Fatal(err)
	}
	if broker.exchanges[ExchangeTimelineIngress] != "topic" ||
		broker.exchanges[ExchangeTimelineRetry] != "topic" || len(broker.exchanges) != 2 {
		t.Fatalf("exchanges = %+v", broker.exchanges)
	}

	for _, route := range timelineRecoveryRoutes {
		if _, exists := broker.queues[route.queue]; !exists {
			t.Fatalf("missing target queue %s", route.queue)
		}
		args, exists := broker.queues[route.retryQueue]
		if !exists {
			t.Fatalf("missing retry queue %s", route.retryQueue)
		}
		if args["x-dead-letter-exchange"] != ExchangeTimelineIngress ||
			args["x-dead-letter-routing-key"] != route.ingressRoutingKey {
			t.Fatalf("retry args for %s = %+v", route.retryQueue, args)
		}
		assertTimelineTopologyBinding(t, broker.bindings, timelineTopologyBinding{
			queue: route.queue, routingKey: route.ingressRoutingKey, exchange: ExchangeTimelineIngress,
		})
		assertTimelineTopologyBinding(t, broker.bindings, timelineTopologyBinding{
			queue: route.retryQueue, routingKey: route.retryRoutingKey, exchange: ExchangeTimelineRetry,
		})
	}

	for _, legacyQueue := range []string{
		"queue.tweet.fanout.retry",
		"queue.tweet.delete.retry",
		"queue.tweet.moderation.cleanup.retry",
	} {
		if _, exists := broker.queues[legacyQueue]; exists {
			t.Fatalf("legacy retry queue was redeclared: %s", legacyQueue)
		}
	}
}

func assertTimelineTopologyBinding(
	t *testing.T,
	bindings []timelineTopologyBinding,
	expected timelineTopologyBinding,
) {
	t.Helper()
	for _, binding := range bindings {
		if binding == expected {
			return
		}
	}
	t.Fatalf("missing binding %+v in %+v", expected, bindings)
}
