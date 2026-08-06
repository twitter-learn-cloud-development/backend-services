package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"twitter-clone/internal/module/agent/attribution"
)

type contentEngagementBrokerFake struct {
	bindings     [][3]string
	published    []publishedContentEngagementMessage
	confirmErr   error
	publishErr   error
	confirmCalls int
}

type publishedContentEngagementMessage struct {
	exchange   string
	routingKey string
	message    amqp.Publishing
}

func (b *contentEngagementBrokerFake) DeclareExchange(string, string, bool) error { return nil }
func (b *contentEngagementBrokerFake) DeclareQueue(name string, _ bool) (amqp.Queue, error) {
	return amqp.Queue{Name: name}, nil
}
func (b *contentEngagementBrokerFake) DeclareQueueWithArgs(name string, _ bool, _ amqp.Table) (amqp.Queue, error) {
	return amqp.Queue{Name: name}, nil
}
func (b *contentEngagementBrokerFake) BindQueue(queueName, routingKey, exchangeName string) error {
	b.bindings = append(b.bindings, [3]string{queueName, routingKey, exchangeName})
	return nil
}
func (b *contentEngagementBrokerFake) SetQoS(int) error { return nil }
func (b *contentEngagementBrokerFake) EnablePublisherConfirms() error {
	b.confirmCalls++
	return b.confirmErr
}
func (b *contentEngagementBrokerFake) Consume(string, string) (<-chan amqp.Delivery, error) {
	return nil, errors.New("not used")
}
func (b *contentEngagementBrokerFake) PublishMessageConfirmed(_ context.Context, exchange, routingKey string, message amqp.Publishing) error {
	if b.publishErr != nil {
		return b.publishErr
	}
	b.published = append(b.published, publishedContentEngagementMessage{exchange: exchange, routingKey: routingKey, message: message})
	return nil
}

type contentEngagementProcessorFake struct {
	event  attribution.ContentEngagement
	result string
	err    error
}

func (p *contentEngagementProcessorFake) Process(_ context.Context, event attribution.ContentEngagement) (string, error) {
	p.event = event
	return p.result, p.err
}

type deliveryAcknowledgerFake struct {
	acked   int
	nacked  int
	requeue bool
	ackErr  error
	nackErr error
}

func (a *deliveryAcknowledgerFake) Ack(uint64, bool) error { a.acked++; return a.ackErr }
func (a *deliveryAcknowledgerFake) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked++
	a.requeue = requeue
	return a.nackErr
}
func (a *deliveryAcknowledgerFake) Reject(_ uint64, requeue bool) error {
	a.nacked++
	a.requeue = requeue
	return a.nackErr
}

func TestContentEngagementConsumerDecodesAndAcknowledgesLike(t *testing.T) {
	broker := &contentEngagementBrokerFake{}
	processor := &contentEngagementProcessorFake{result: "attributed"}
	consumer, err := NewContentEngagementConsumer(broker, processor, nil)
	if err != nil {
		t.Fatal(err)
	}
	ack := &deliveryAcknowledgerFake{}
	consumer.handle(context.Background(), amqp.Delivery{
		Acknowledger: ack, DeliveryTag: 1, RoutingKey: routingTweetLiked, Timestamp: time.Now(),
		Body: []byte(`{"tweet_id":9001,"user_id":77,"tweet_user_id":42,"occurred_at_unix_ms":1700000000000}`),
	})
	if ack.acked != 1 || ack.nacked != 0 || processor.event.Kind != attribution.EngagementKindLike ||
		processor.event.EventID != "like:9001:77" || processor.event.OccurredAt.UnixMilli() != 1700000000000 {
		t.Fatalf("ack=%+v event=%+v", ack, processor.event)
	}
}

func TestContentEngagementConsumerRetriesTransientFailure(t *testing.T) {
	broker := &contentEngagementBrokerFake{}
	processor := &contentEngagementProcessorFake{err: errors.New("mongo unavailable")}
	consumer, err := NewContentEngagementConsumer(broker, processor, nil)
	if err != nil {
		t.Fatal(err)
	}
	ack := &deliveryAcknowledgerFake{}
	consumer.handle(context.Background(), amqp.Delivery{
		Acknowledger: ack, DeliveryTag: 2, RoutingKey: routingCommentCreate, Timestamp: time.Now(),
		Body: []byte(`{"comment_id":8,"tweet_id":9001,"user_id":77,"tweet_user_id":42}`),
	})
	if ack.acked != 1 || len(broker.published) != 1 {
		t.Fatalf("ack=%+v published=%+v", ack, broker.published)
	}
	published := broker.published[0]
	if published.exchange != contentEngagementRetryExchange || published.routingKey != retryRoutingKey(routingCommentCreate) ||
		published.message.Expiration != "1000" || published.message.Headers["x-agent-profile-retry-count"] != int32(1) {
		t.Fatalf("published retry = %+v", published)
	}
}

func TestContentEngagementConsumerRoutesMalformedMessageToDLQ(t *testing.T) {
	broker := &contentEngagementBrokerFake{}
	consumer, err := NewContentEngagementConsumer(broker, &contentEngagementProcessorFake{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ack := &deliveryAcknowledgerFake{}
	consumer.handle(context.Background(), amqp.Delivery{
		Acknowledger: ack, DeliveryTag: 3, RoutingKey: routingTweetLiked, Body: []byte("{"),
	})
	if ack.acked != 1 || len(broker.published) != 1 || broker.published[0].exchange != contentEngagementDLX ||
		broker.published[0].routingKey != dlqRoutingKey(routingTweetLiked) {
		t.Fatalf("ack=%+v published=%+v", ack, broker.published)
	}
}

func TestContentEngagementConsumerPublishFailureWaitsThenRequeues(t *testing.T) {
	broker := &contentEngagementBrokerFake{publishErr: errors.New("broker unavailable")}
	consumer, err := NewContentEngagementConsumer(
		broker,
		&contentEngagementProcessorFake{err: errors.New("mongo unavailable")},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	var waited time.Duration
	consumer.wait = func(_ context.Context, delay time.Duration) bool {
		waited = delay
		return true
	}
	ack := &deliveryAcknowledgerFake{}
	consumer.handle(context.Background(), amqp.Delivery{
		Acknowledger: ack, DeliveryTag: 4, RoutingKey: routingTweetLiked,
		Headers: amqp.Table{"x-agent-profile-retry-count": int32(1)},
		Body:    []byte(`{"tweet_id":9001,"user_id":77,"tweet_user_id":42}`),
	})
	if waited != 4*time.Second || ack.acked != 0 || ack.nacked != 1 || !ack.requeue {
		t.Fatalf("waited=%s ack=%+v", waited, ack)
	}
}

func TestNewContentEngagementConsumerRequiresPublisherConfirms(t *testing.T) {
	_, err := NewContentEngagementConsumer(
		&contentEngagementBrokerFake{confirmErr: errors.New("confirm unavailable")},
		&contentEngagementProcessorFake{},
		nil,
	)
	if err == nil {
		t.Fatal("expected publisher confirm initialization failure")
	}
}
