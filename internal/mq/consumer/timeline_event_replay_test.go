package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"twitter-clone/internal/events"
)

type timelineEventReplayPublished struct {
	exchange   string
	routingKey string
	message    amqp.Publishing
}

type timelineEventReplayBrokerFake struct {
	deliveries      []amqp.Delivery
	getIndex        int
	published       []timelineEventReplayPublished
	publishErr      error
	confirmErr      error
	topologyErr     error
	confirmCalls    int
	topologyStarted bool
	requestedQueues []string
	operations      *[]string
	onGet           func(int)
}

func (b *timelineEventReplayBrokerFake) beginTopology() {
	if b.topologyStarted {
		return
	}
	b.topologyStarted = true
	b.record("topology")
}

func (b *timelineEventReplayBrokerFake) DeclareExchange(string, string, bool) error {
	b.beginTopology()
	return b.topologyErr
}

func (b *timelineEventReplayBrokerFake) DeclareQueue(string, bool) (amqp.Queue, error) {
	b.beginTopology()
	return amqp.Queue{}, b.topologyErr
}

func (b *timelineEventReplayBrokerFake) DeclareQueueWithArgs(string, bool, amqp.Table) (amqp.Queue, error) {
	b.beginTopology()
	return amqp.Queue{}, b.topologyErr
}

func (b *timelineEventReplayBrokerFake) BindQueue(string, string, string) error {
	b.beginTopology()
	return b.topologyErr
}

func (b *timelineEventReplayBrokerFake) EnablePublisherConfirms() error {
	b.confirmCalls++
	b.record("confirm")
	return b.confirmErr
}

func (b *timelineEventReplayBrokerFake) GetMessage(queueName string, autoAck bool) (amqp.Delivery, bool, error) {
	if autoAck {
		return amqp.Delivery{}, false, errors.New("replay must use manual acknowledgement")
	}
	b.record("get")
	b.requestedQueues = append(b.requestedQueues, queueName)
	if b.getIndex >= len(b.deliveries) {
		return amqp.Delivery{}, false, nil
	}
	delivery := b.deliveries[b.getIndex]
	b.getIndex++
	if b.onGet != nil {
		b.onGet(b.getIndex)
	}
	return delivery, true, nil
}

func (b *timelineEventReplayBrokerFake) PublishMessageConfirmed(
	_ context.Context,
	exchange string,
	routingKey string,
	message amqp.Publishing,
) error {
	b.record("publish")
	if b.publishErr != nil {
		return b.publishErr
	}
	b.published = append(b.published, timelineEventReplayPublished{
		exchange: exchange, routingKey: routingKey, message: message,
	})
	return nil
}

func (b *timelineEventReplayBrokerFake) record(operation string) {
	if b.operations != nil {
		*b.operations = append(*b.operations, operation)
	}
}

type timelineEventReplayAcknowledgerFake struct {
	acked      int
	nacked     int
	requeue    bool
	ackErr     error
	nackErr    error
	operations *[]string
}

func (a *timelineEventReplayAcknowledgerFake) Ack(uint64, bool) error {
	a.acked++
	if a.operations != nil {
		*a.operations = append(*a.operations, "ack")
	}
	return a.ackErr
}

func (a *timelineEventReplayAcknowledgerFake) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked++
	a.requeue = requeue
	if a.operations != nil {
		*a.operations = append(*a.operations, "nack")
	}
	return a.nackErr
}

func (a *timelineEventReplayAcknowledgerFake) Reject(_ uint64, requeue bool) error {
	return a.Nack(0, false, requeue)
}

func TestTimelineEventDLQReplayerInspectRetainsWithoutChangingTopology(t *testing.T) {
	ack := &timelineEventReplayAcknowledgerFake{}
	broker := &timelineEventReplayBrokerFake{deliveries: []amqp.Delivery{
		timelineEventReplayDelivery(
			ack,
			1,
			RoutingKeyTweetCreated+".dlq",
			validTimelineCreatedReplayBody(9001, 42),
			nil,
		),
	}}
	replayer, err := NewTimelineEventDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), TimelineEventReplayOptions{
		Kind: TimelineEventCreated, Limit: 1, MaxReplayCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "inspect" || report.Event != TimelineEventCreated || report.Inspected != 1 ||
		report.Eligible != 1 || report.Replayed != 0 || report.Retained != 0 || report.Empty {
		t.Fatalf("report = %+v", report)
	}
	if broker.topologyStarted || broker.confirmCalls != 0 || len(broker.published) != 0 {
		t.Fatalf("topology=%t confirms=%d published=%d", broker.topologyStarted, broker.confirmCalls, len(broker.published))
	}
	if ack.acked != 0 || ack.nacked != 1 || !ack.requeue {
		t.Fatalf("ack = %+v", ack)
	}
	if len(broker.requestedQueues) != 1 || broker.requestedQueues[0] != QueueTweetFanoutDLQ {
		t.Fatalf("requested queues = %+v", broker.requestedQueues)
	}
}

func TestTimelineEventDLQReplayerExecuteEnsuresTopologyAndConfirmsBeforeAck(t *testing.T) {
	operations := make([]string, 0, 6)
	ack := &timelineEventReplayAcknowledgerFake{operations: &operations}
	broker := &timelineEventReplayBrokerFake{
		operations: &operations,
		deliveries: []amqp.Delivery{
			timelineEventReplayDelivery(
				ack,
				2,
				RoutingKeyTweetCreated+".dlq",
				validTimelineCreatedReplayBody(9001, 42),
				amqp.Table{
					timelineRetryHeader: int32(3),
					"x-death":           []interface{}{"internal-only"},
					"traceparent":       "00-test",
				},
			),
		},
	}
	replayer, err := NewTimelineEventDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}
	replayedAt := time.Unix(1_800_000_000, 123_000_000)
	replayer.now = func() time.Time { return replayedAt }

	report, err := replayer.Run(context.Background(), TimelineEventReplayOptions{
		Kind: TimelineEventCreated, Limit: 1, Execute: true, MaxReplayCount: 2,
		Operator: "on-call", Reason: "incident-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(operations, ",") != "topology,confirm,get,publish,ack" {
		t.Fatalf("operation order = %v", operations)
	}
	if report.Replayed != 1 || report.Eligible != 1 || report.OperatorSHA256 == "" || report.ReasonSHA256 == "" {
		t.Fatalf("report = %+v", report)
	}
	if ack.acked != 1 || ack.nacked != 0 || len(broker.published) != 1 {
		t.Fatalf("ack=%+v published=%+v", ack, broker.published)
	}
	published := broker.published[0]
	if published.exchange != ExchangeTimelineIngress || published.routingKey != RoutingKeyTimelineTweetCreated {
		t.Fatalf("published destination = %+v", published)
	}
	for _, removed := range []string{timelineRetryHeader, "x-death"} {
		if _, exists := published.message.Headers[removed]; exists {
			t.Fatalf("transport header %s was retained: %+v", removed, published.message.Headers)
		}
	}
	if published.message.Headers[timelineEventReplayHeader] != int32(1) ||
		published.message.Headers[timelineEventReplayedAtHeader] != replayedAt.UTC().UnixMilli() ||
		published.message.Headers["traceparent"] != "00-test" {
		t.Fatalf("published headers = %+v", published.message.Headers)
	}
}

func TestTimelineEventDLQReplayerRoutesDeletedEventOnlyToTimelineIngress(t *testing.T) {
	ack := &timelineEventReplayAcknowledgerFake{}
	broker := &timelineEventReplayBrokerFake{deliveries: []amqp.Delivery{
		timelineEventReplayDelivery(
			ack,
			3,
			RoutingKeyTweetDeleted+".dlq",
			validTimelineDeletedReplayBody(9002, 43),
			nil,
		),
	}}
	replayer, err := NewTimelineEventDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), TimelineEventReplayOptions{
		Kind: TimelineEventDeleted, Limit: 1, Execute: true, MaxReplayCount: 1,
		Operator: "on-call", Reason: "delete projection recovery",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Queue != QueueTweetDeleteDLQ || report.Replayed != 1 || len(broker.published) != 1 {
		t.Fatalf("report=%+v published=%+v", report, broker.published)
	}
	if broker.published[0].exchange != ExchangeTimelineIngress ||
		broker.published[0].routingKey != RoutingKeyTimelineTweetDeleted {
		t.Fatalf("published destination = %+v", broker.published[0])
	}
}

func TestTimelineEventDLQReplayerRetainsPoisonLimitedAndInvalidCount(t *testing.T) {
	poisonAck := &timelineEventReplayAcknowledgerFake{}
	limitedAck := &timelineEventReplayAcknowledgerFake{}
	invalidCountAck := &timelineEventReplayAcknowledgerFake{}
	broker := &timelineEventReplayBrokerFake{deliveries: []amqp.Delivery{
		timelineEventReplayDelivery(poisonAck, 4, RoutingKeyTweetCreated+".dlq", "{", nil),
		timelineEventReplayDelivery(
			limitedAck,
			5,
			RoutingKeyTweetCreated+".dlq",
			validTimelineCreatedReplayBody(9002, 43),
			amqp.Table{timelineEventReplayHeader: int32(1)},
		),
		timelineEventReplayDelivery(
			invalidCountAck,
			6,
			RoutingKeyTweetCreated+".dlq",
			validTimelineCreatedReplayBody(9003, 44),
			amqp.Table{timelineEventReplayHeader: "invalid"},
		),
	}}
	replayer, err := NewTimelineEventDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), TimelineEventReplayOptions{
		Kind: TimelineEventCreated, Limit: 3, Execute: true, MaxReplayCount: 1,
		Operator: "on-call", Reason: "poison review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Inspected != 3 || report.Eligible != 0 || report.Replayed != 0 || report.Retained != 3 {
		t.Fatalf("report = %+v", report)
	}
	if report.Entries[0].ErrorCode != timelineEventReplayErrorMalformedEvent ||
		report.Entries[1].ErrorCode != timelineEventReplayErrorReplayLimit ||
		report.Entries[2].ErrorCode != timelineEventReplayErrorInvalidCount {
		t.Fatalf("entries = %+v", report.Entries)
	}
	for _, ack := range []*timelineEventReplayAcknowledgerFake{poisonAck, limitedAck, invalidCountAck} {
		if ack.nacked != 1 || !ack.requeue {
			t.Fatalf("retained ack = %+v", ack)
		}
	}
	if len(broker.published) != 0 {
		t.Fatalf("published = %+v", broker.published)
	}
}

func TestTimelineEventDLQReplayerPublishFailureRetainsCurrentAndRemaining(t *testing.T) {
	firstAck := &timelineEventReplayAcknowledgerFake{}
	secondAck := &timelineEventReplayAcknowledgerFake{}
	broker := &timelineEventReplayBrokerFake{
		deliveries: []amqp.Delivery{
			timelineEventReplayDelivery(firstAck, 7, RoutingKeyTweetCreated+".dlq", validTimelineCreatedReplayBody(9001, 42), nil),
			timelineEventReplayDelivery(secondAck, 8, RoutingKeyTweetCreated+".dlq", validTimelineCreatedReplayBody(9002, 43), nil),
		},
		publishErr: errors.New("rabbitmq unavailable"),
	}
	replayer, err := NewTimelineEventDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), TimelineEventReplayOptions{
		Kind: TimelineEventCreated, Limit: 2, Execute: true, MaxReplayCount: 1,
		Operator: "on-call", Reason: "retry outage",
	})
	if err == nil {
		t.Fatal("expected publish failure")
	}
	if report.Inspected != 1 || report.Replayed != 0 || report.Retained != 1 ||
		report.Entries[0].ErrorCode != timelineEventReplayErrorPublishFailed {
		t.Fatalf("report = %+v", report)
	}
	if firstAck.nacked != 1 || !firstAck.requeue || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("first ack=%+v second ack=%+v", firstAck, secondAck)
	}
}

func TestTimelineEventDLQReplayerReportsAcknowledgementUncertainty(t *testing.T) {
	firstAck := &timelineEventReplayAcknowledgerFake{ackErr: errors.New("channel closed")}
	secondAck := &timelineEventReplayAcknowledgerFake{}
	broker := &timelineEventReplayBrokerFake{deliveries: []amqp.Delivery{
		timelineEventReplayDelivery(firstAck, 9, RoutingKeyTweetCreated+".dlq", validTimelineCreatedReplayBody(9001, 42), nil),
		timelineEventReplayDelivery(secondAck, 10, RoutingKeyTweetCreated+".dlq", validTimelineCreatedReplayBody(9002, 43), nil),
	}}
	replayer, err := NewTimelineEventDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), TimelineEventReplayOptions{
		Kind: TimelineEventCreated, Limit: 2, Execute: true, MaxReplayCount: 1,
		Operator: "on-call", Reason: "ack audit",
	})
	if err == nil {
		t.Fatal("expected acknowledgement failure")
	}
	if report.Uncertain != 1 || report.Replayed != 0 ||
		report.Entries[0].Outcome != timelineEventReplayOutcomeAckUncertain ||
		report.Entries[0].ErrorCode != timelineEventReplayErrorAckFailed {
		t.Fatalf("report = %+v", report)
	}
	if len(broker.published) != 1 || firstAck.acked != 1 || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("published=%+v first ack=%+v second ack=%+v", broker.published, firstAck, secondAck)
	}
}

func TestTimelineEventDLQReplayerFailsClosedBeforeReading(t *testing.T) {
	t.Run("topology", func(t *testing.T) {
		broker := &timelineEventReplayBrokerFake{topologyErr: errors.New("binding denied")}
		replayer, err := NewTimelineEventDLQReplayer(broker)
		if err != nil {
			t.Fatal(err)
		}
		_, err = replayer.Run(context.Background(), TimelineEventReplayOptions{
			Kind: TimelineEventCreated, Limit: 1, Execute: true, MaxReplayCount: 1,
			Operator: "on-call", Reason: "incident",
		})
		if err == nil || broker.confirmCalls != 0 || len(broker.requestedQueues) != 0 {
			t.Fatalf("err=%v confirms=%d reads=%v", err, broker.confirmCalls, broker.requestedQueues)
		}
	})

	t.Run("publisher confirm", func(t *testing.T) {
		broker := &timelineEventReplayBrokerFake{confirmErr: errors.New("confirm unavailable")}
		replayer, err := NewTimelineEventDLQReplayer(broker)
		if err != nil {
			t.Fatal(err)
		}
		_, err = replayer.Run(context.Background(), TimelineEventReplayOptions{
			Kind: TimelineEventCreated, Limit: 1, Execute: true, MaxReplayCount: 1,
			Operator: "on-call", Reason: "incident",
		})
		if err == nil || !broker.topologyStarted || len(broker.requestedQueues) != 0 {
			t.Fatalf("err=%v topology=%t reads=%v", err, broker.topologyStarted, broker.requestedQueues)
		}
	})
}

func TestTimelineEventDLQReplayerCancellationRequeuesTakenBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ack := &timelineEventReplayAcknowledgerFake{}
	broker := &timelineEventReplayBrokerFake{
		deliveries: []amqp.Delivery{
			timelineEventReplayDelivery(ack, 11, RoutingKeyTweetCreated+".dlq", validTimelineCreatedReplayBody(9001, 42), nil),
		},
		onGet: func(index int) {
			if index == 1 {
				cancel()
			}
		},
	}
	replayer, err := NewTimelineEventDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	_, err = replayer.Run(ctx, TimelineEventReplayOptions{
		Kind: TimelineEventCreated, Limit: 2, MaxReplayCount: 1,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if ack.nacked != 1 || !ack.requeue {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestTimelineEventReplayValidationAndReportPrivacy(t *testing.T) {
	for _, options := range []TimelineEventReplayOptions{
		{Limit: 1, MaxReplayCount: 1},
		{Kind: TimelineEventCreated, Limit: 1, Execute: true, MaxReplayCount: 1},
	} {
		if err := options.Validate(); err == nil {
			t.Fatalf("expected invalid options: %+v", options)
		}
	}

	ack := &timelineEventReplayAcknowledgerFake{}
	broker := &timelineEventReplayBrokerFake{deliveries: []amqp.Delivery{
		timelineEventReplayDelivery(
			ack,
			12,
			RoutingKeyTweetCreated+".dlq",
			validTimelineCreatedReplayBody(991122, 887766),
			nil,
		),
	}}
	replayer, err := NewTimelineEventDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}
	report, err := replayer.Run(context.Background(), TimelineEventReplayOptions{
		Kind: TimelineEventCreated, Limit: 1, Execute: true, MaxReplayCount: 1,
		Operator: "alice@example.com", Reason: "customer escalation 314159",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(encoded)
	for _, forbidden := range []string{
		"alice@example.com", "customer escalation", "991122", "887766", "tweet_id", "author_id", "secret-content",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("report exposed %q: %s", forbidden, serialized)
		}
	}
}

func timelineEventReplayDelivery(
	ack amqp.Acknowledger,
	deliveryTag uint64,
	routingKey string,
	body string,
	headers amqp.Table,
) amqp.Delivery {
	return amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  deliveryTag,
		RoutingKey:   routingKey,
		ContentType:  "application/json",
		Headers:      headers,
		Body:         []byte(body),
		Timestamp:    time.UnixMilli(1_700_000_000_000),
	}
}

func validTimelineCreatedReplayBody(tweetID, authorID uint64) string {
	body, err := json.Marshal(events.TweetCreatedEvent{
		TweetID: tweetID, AuthorID: authorID, Content: "secret-content",
	})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func validTimelineDeletedReplayBody(tweetID, authorID uint64) string {
	body, err := json.Marshal(events.TweetDeletedEvent{TweetID: tweetID, AuthorID: authorID})
	if err != nil {
		panic(err)
	}
	return string(body)
}
