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

type moderationReplayBrokerFake struct {
	deliveries   []amqp.Delivery
	getIndex     int
	published    []moderationPublishedMessage
	publishErr   error
	confirmErr   error
	topologyErr  error
	confirmCalls int
	requestedAt  []string
	operations   *[]string
}

type moderationPublishedMessage struct {
	exchange   string
	routingKey string
	message    amqp.Publishing
}

func (b *moderationReplayBrokerFake) DeclareExchange(string, string, bool) error {
	return b.topologyErr
}

func (b *moderationReplayBrokerFake) DeclareQueue(string, bool) (amqp.Queue, error) {
	return amqp.Queue{}, b.topologyErr
}

func (b *moderationReplayBrokerFake) DeclareQueueWithArgs(string, bool, amqp.Table) (amqp.Queue, error) {
	return amqp.Queue{}, b.topologyErr
}

func (b *moderationReplayBrokerFake) BindQueue(string, string, string) error {
	return b.topologyErr
}

func (b *moderationReplayBrokerFake) EnablePublisherConfirms() error {
	b.confirmCalls++
	b.record("confirm")
	return b.confirmErr
}

func (b *moderationReplayBrokerFake) GetMessage(queueName string, autoAck bool) (amqp.Delivery, bool, error) {
	if autoAck {
		return amqp.Delivery{}, false, errors.New("replay must use manual acknowledgement")
	}
	b.record("get")
	b.requestedAt = append(b.requestedAt, queueName)
	if b.getIndex >= len(b.deliveries) {
		return amqp.Delivery{}, false, nil
	}
	delivery := b.deliveries[b.getIndex]
	b.getIndex++
	return delivery, true, nil
}

func (b *moderationReplayBrokerFake) PublishMessageConfirmed(
	_ context.Context,
	exchange string,
	routingKey string,
	message amqp.Publishing,
) error {
	b.record("publish")
	if b.publishErr != nil {
		return b.publishErr
	}
	b.published = append(b.published, moderationPublishedMessage{
		exchange: exchange, routingKey: routingKey, message: message,
	})
	return nil
}

func (b *moderationReplayBrokerFake) record(operation string) {
	if b.operations != nil {
		*b.operations = append(*b.operations, operation)
	}
}

type moderationReplayAcknowledgerFake struct {
	acked      int
	nacked     int
	requeue    bool
	ackErr     error
	nackErr    error
	operations *[]string
}

func (a *moderationReplayAcknowledgerFake) Ack(uint64, bool) error {
	a.acked++
	if a.operations != nil {
		*a.operations = append(*a.operations, "ack")
	}
	return a.ackErr
}

func (a *moderationReplayAcknowledgerFake) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked++
	a.requeue = requeue
	if a.operations != nil {
		*a.operations = append(*a.operations, "nack")
	}
	return a.nackErr
}

func (a *moderationReplayAcknowledgerFake) Reject(_ uint64, requeue bool) error {
	return a.Nack(0, false, requeue)
}

func TestModerationDLQReplayerInspectRetainsEligibleMessageWithoutConfirmMode(t *testing.T) {
	ack := &moderationReplayAcknowledgerFake{}
	broker := &moderationReplayBrokerFake{deliveries: []amqp.Delivery{
		moderationReplayDelivery(ack, 1, RoutingKeyTweetModerated+".dlq", validModerationReplayBody(9001, 42), nil),
	}}
	replayer, err := NewModerationDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), ModerationReplayOptions{
		Limit: 1, MaxReplayCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "inspect" || report.Inspected != 1 || report.Eligible != 1 ||
		report.Replayed != 0 || report.Retained != 0 || report.Empty {
		t.Fatalf("report = %+v", report)
	}
	if broker.confirmCalls != 0 || len(broker.published) != 0 {
		t.Fatalf("confirm calls=%d published=%d", broker.confirmCalls, len(broker.published))
	}
	if ack.acked != 0 || ack.nacked != 1 || !ack.requeue {
		t.Fatalf("ack = %+v", ack)
	}
	if len(broker.requestedAt) != 1 || broker.requestedAt[0] != QueueTweetModerationCleanupDLQ {
		t.Fatalf("requested queues = %+v", broker.requestedAt)
	}
}

func TestModerationDLQReplayerExecuteConfirmsPublishBeforeAcknowledgement(t *testing.T) {
	operations := make([]string, 0, 4)
	ack := &moderationReplayAcknowledgerFake{operations: &operations}
	broker := &moderationReplayBrokerFake{
		operations: &operations,
		deliveries: []amqp.Delivery{
			moderationReplayDelivery(ack, 2, RoutingKeyTweetModerated+".dlq", validModerationReplayBody(9001, 42), amqp.Table{
				"x-retry-count": int32(3),
				"x-death":       []interface{}{"internal-only"},
				"traceparent":   "00-test",
			}),
		},
	}
	replayer, err := NewModerationDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}
	replayedAt := time.Unix(1_800_000_000, 123_000_000)
	replayer.now = func() time.Time { return replayedAt }

	report, err := replayer.Run(context.Background(), ModerationReplayOptions{
		Limit: 1, Execute: true, MaxReplayCount: 2, Operator: "on-call", Reason: "incident-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(operations, ",") != "confirm,get,publish,ack" {
		t.Fatalf("operation order = %v", operations)
	}
	if report.Mode != "execute" || report.Replayed != 1 || report.Eligible != 1 ||
		report.OperatorSHA256 == "" || report.ReasonSHA256 == "" || report.Uncertain != 0 {
		t.Fatalf("report = %+v", report)
	}
	if ack.acked != 1 || ack.nacked != 0 || len(broker.published) != 1 {
		t.Fatalf("ack=%+v published=%+v", ack, broker.published)
	}
	published := broker.published[0]
	if published.exchange != ExchangeTimelineIngress || published.routingKey != RoutingKeyTimelineTweetModerated {
		t.Fatalf("published destination = %+v", published)
	}
	if _, exists := published.message.Headers["x-retry-count"]; exists {
		t.Fatalf("retry header was retained: %+v", published.message.Headers)
	}
	if _, exists := published.message.Headers["x-death"]; exists {
		t.Fatalf("dead-letter header was retained: %+v", published.message.Headers)
	}
	if published.message.Headers[moderationReplayHeader] != int32(1) ||
		published.message.Headers[moderationReplayedAtHeader] != replayedAt.UTC().UnixMilli() ||
		published.message.Headers["traceparent"] != "00-test" {
		t.Fatalf("published headers = %+v", published.message.Headers)
	}
	if report.Entries[0].ReplayCount != 1 || report.Entries[0].Outcome != moderationReplayOutcomeReplayed {
		t.Fatalf("entry = %+v", report.Entries[0])
	}
}

func TestModerationDLQReplayerRetainsPoisonLimitedAndInvalidCountMessages(t *testing.T) {
	poisonAck := &moderationReplayAcknowledgerFake{}
	limitedAck := &moderationReplayAcknowledgerFake{}
	invalidCountAck := &moderationReplayAcknowledgerFake{}
	broker := &moderationReplayBrokerFake{deliveries: []amqp.Delivery{
		moderationReplayDelivery(poisonAck, 3, RoutingKeyTweetModerated+".dlq", "{", nil),
		moderationReplayDelivery(limitedAck, 4, RoutingKeyTweetModerated+".dlq", validModerationReplayBody(9002, 43), amqp.Table{
			moderationReplayHeader: int32(1),
		}),
		moderationReplayDelivery(invalidCountAck, 5, RoutingKeyTweetModerated+".dlq", validModerationReplayBody(9003, 44), amqp.Table{
			moderationReplayHeader: "not-a-number",
		}),
	}}
	replayer, err := NewModerationDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), ModerationReplayOptions{
		Limit: 3, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "poison review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Inspected != 3 || report.Eligible != 0 || report.Replayed != 0 || report.Retained != 3 {
		t.Fatalf("report = %+v", report)
	}
	if report.Entries[0].ErrorCode != moderationReplayErrorMalformedEvent ||
		report.Entries[1].ErrorCode != moderationReplayErrorReplayLimit ||
		report.Entries[2].ErrorCode != moderationReplayErrorInvalidCount {
		t.Fatalf("entries = %+v", report.Entries)
	}
	for _, ack := range []*moderationReplayAcknowledgerFake{poisonAck, limitedAck, invalidCountAck} {
		if ack.nacked != 1 || !ack.requeue {
			t.Fatalf("retained ack = %+v", ack)
		}
	}
	if len(broker.published) != 0 {
		t.Fatalf("published = %+v", broker.published)
	}
}

func TestInspectModerationReplayDeliveryRejectsUnsupportedRouteAndOversizedEvent(t *testing.T) {
	unsupported, eligible := inspectModerationReplayDelivery(amqp.Delivery{
		RoutingKey: "tweet.deleted.dlq",
		Body:       []byte(validModerationReplayBody(9001, 42)),
	}, 1)
	if eligible || unsupported.ErrorCode != moderationReplayErrorUnsupportedRoute {
		t.Fatalf("unsupported entry = %+v eligible=%t", unsupported, eligible)
	}

	oversized, eligible := inspectModerationReplayDelivery(amqp.Delivery{
		RoutingKey: RoutingKeyTweetModerated + ".dlq",
		Body:       make([]byte, moderationReplayBodyLimit+1),
	}, 1)
	if eligible || oversized.ErrorCode != moderationReplayErrorOversizedEvent {
		t.Fatalf("oversized entry = %+v eligible=%t", oversized, eligible)
	}
}

func TestModerationDLQReplayerPublishFailureRetainsCurrentAndRemaining(t *testing.T) {
	firstAck := &moderationReplayAcknowledgerFake{}
	secondAck := &moderationReplayAcknowledgerFake{}
	broker := &moderationReplayBrokerFake{
		deliveries: []amqp.Delivery{
			moderationReplayDelivery(firstAck, 6, RoutingKeyTweetModerated+".dlq", validModerationReplayBody(9001, 42), nil),
			moderationReplayDelivery(secondAck, 7, RoutingKeyTweetModerated+".dlq", validModerationReplayBody(9002, 43), nil),
		},
		publishErr: errors.New("rabbitmq unavailable"),
	}
	replayer, err := NewModerationDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), ModerationReplayOptions{
		Limit: 2, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "retry outage",
	})
	if err == nil {
		t.Fatal("expected publish failure")
	}
	if report.Inspected != 1 || report.Replayed != 0 || report.Retained != 1 ||
		report.Entries[0].ErrorCode != moderationReplayErrorPublishFailed {
		t.Fatalf("report = %+v", report)
	}
	if firstAck.nacked != 1 || !firstAck.requeue || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("first ack=%+v second ack=%+v", firstAck, secondAck)
	}
}

func TestModerationDLQReplayerReportsAcknowledgementUncertainty(t *testing.T) {
	firstAck := &moderationReplayAcknowledgerFake{ackErr: errors.New("channel closed")}
	secondAck := &moderationReplayAcknowledgerFake{}
	broker := &moderationReplayBrokerFake{deliveries: []amqp.Delivery{
		moderationReplayDelivery(firstAck, 8, RoutingKeyTweetModerated+".dlq", validModerationReplayBody(9001, 42), nil),
		moderationReplayDelivery(secondAck, 9, RoutingKeyTweetModerated+".dlq", validModerationReplayBody(9002, 43), nil),
	}}
	replayer, err := NewModerationDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), ModerationReplayOptions{
		Limit: 2, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "ack audit",
	})
	if err == nil {
		t.Fatal("expected acknowledgement failure")
	}
	if report.Uncertain != 1 || report.Replayed != 0 ||
		report.Entries[0].Outcome != moderationReplayOutcomeAckUncertain ||
		report.Entries[0].ErrorCode != moderationReplayErrorAckFailed {
		t.Fatalf("report = %+v", report)
	}
	if len(broker.published) != 1 || firstAck.acked != 1 || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("published=%+v first ack=%+v second ack=%+v", broker.published, firstAck, secondAck)
	}
}

func TestModerationDLQReplayerExecuteRequiresPublisherConfirmsBeforeReading(t *testing.T) {
	broker := &moderationReplayBrokerFake{confirmErr: errors.New("confirm unavailable")}
	replayer, err := NewModerationDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	_, err = replayer.Run(context.Background(), ModerationReplayOptions{
		Limit: 1, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "incident",
	})
	if err == nil {
		t.Fatal("expected publisher confirm initialization to fail")
	}
	if len(broker.requestedAt) != 0 {
		t.Fatalf("read messages before publisher confirm: %+v", broker.requestedAt)
	}
}

func TestModerationReplayOptionsRequireAuditFieldsForExecution(t *testing.T) {
	options := ModerationReplayOptions{Limit: 1, Execute: true, MaxReplayCount: 1}
	if err := options.Validate(); err == nil {
		t.Fatal("expected missing operator and reason to fail")
	}
}

func TestModerationReplayReportDoesNotExposeEventOrAuditInput(t *testing.T) {
	ack := &moderationReplayAcknowledgerFake{}
	broker := &moderationReplayBrokerFake{deliveries: []amqp.Delivery{
		moderationReplayDelivery(ack, 10, RoutingKeyTweetModerated+".dlq", validModerationReplayBody(991122, 887766), nil),
	}}
	replayer, err := NewModerationDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}
	report, err := replayer.Run(context.Background(), ModerationReplayOptions{
		Limit: 1, Execute: true, MaxReplayCount: 1,
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
		"alice@example.com", "customer escalation", "tweet_id", "author_id", "shadowban", "tweet-moderated:v1",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("report exposed %q: %s", forbidden, serialized)
		}
	}
}

func moderationReplayDelivery(
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

func validModerationReplayBody(tweetID, authorID uint64) string {
	event := events.NewTweetModeratedEvent(tweetID, authorID, events.TweetModerationShadowban, 1_700_000_000_000)
	body, err := json.Marshal(event)
	if err != nil {
		panic(err)
	}
	return string(body)
}
