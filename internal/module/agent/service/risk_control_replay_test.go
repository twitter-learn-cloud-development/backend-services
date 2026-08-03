package service

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

type riskControlReplayBrokerFake struct {
	deliveries   []amqp.Delivery
	getIndex     int
	published    []riskControlPublished
	publishErr   error
	confirmErr   error
	confirmCalls int
	requestedAt  []string
	operations   *[]string
	afterGet     func(int)
}

func (b *riskControlReplayBrokerFake) EnablePublisherConfirms() error {
	b.confirmCalls++
	b.record("confirm")
	return b.confirmErr
}

func (b *riskControlReplayBrokerFake) GetMessage(queueName string, autoAck bool) (amqp.Delivery, bool, error) {
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
	if b.afterGet != nil {
		b.afterGet(b.getIndex)
	}
	return delivery, true, nil
}

func (b *riskControlReplayBrokerFake) PublishMessageConfirmed(
	_ context.Context,
	exchange string,
	routingKey string,
	message amqp.Publishing,
) error {
	b.record("publish")
	if b.publishErr != nil {
		return b.publishErr
	}
	b.published = append(b.published, riskControlPublished{
		exchange: exchange, routingKey: routingKey, message: message,
	})
	return nil
}

func (b *riskControlReplayBrokerFake) record(operation string) {
	if b.operations != nil {
		*b.operations = append(*b.operations, operation)
	}
}

func TestRiskControlDLQReplayerInspectRetainsEligibleMessageWithoutConfirmMode(t *testing.T) {
	ack := &riskControlAcknowledgerFake{}
	broker := &riskControlReplayBrokerFake{deliveries: []amqp.Delivery{
		riskControlReplayDelivery(ack, 1, riskControlDLQRoutingKey, validRiskControlReplayBody(9001, 42, "private content"), nil),
	}}
	replayer, err := NewRiskControlDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), RiskControlReplayOptions{
		Limit: 1, MaxReplayCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "inspect" || report.Queue != riskControlDLQ || report.Inspected != 1 ||
		report.Eligible != 1 || report.Replayed != 0 || report.Retained != 0 || report.Empty {
		t.Fatalf("report = %+v", report)
	}
	if broker.confirmCalls != 0 || len(broker.published) != 0 {
		t.Fatalf("confirm calls=%d published=%d", broker.confirmCalls, len(broker.published))
	}
	if ack.acked != 0 || ack.nacked != 1 || !ack.requeue {
		t.Fatalf("ack = %+v", ack)
	}
	if len(broker.requestedAt) != 1 || broker.requestedAt[0] != riskControlDLQ {
		t.Fatalf("requested queues = %+v", broker.requestedAt)
	}
}

func TestRiskControlDLQReplayerExecuteConfirmsPublishBeforeAcknowledgement(t *testing.T) {
	operations := make([]string, 0, 4)
	ack := &riskControlAcknowledgerFake{operations: &operations}
	broker := &riskControlReplayBrokerFake{
		operations: &operations,
		deliveries: []amqp.Delivery{
			riskControlReplayDelivery(
				ack,
				2,
				riskControlDLQRoutingKey,
				validRiskControlReplayBody(9001, 42, "private content"),
				amqp.Table{
					riskControlRetryHeader: int32(3),
					"x-death":              []interface{}{"private broker metadata"},
					"traceparent":          "00-test",
				},
			),
		},
	}
	replayer, err := NewRiskControlDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}
	replayedAt := time.Unix(1_800_000_000, 123_000_000)
	replayer.now = func() time.Time { return replayedAt }

	report, err := replayer.Run(context.Background(), RiskControlReplayOptions{
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
	if published.exchange != riskControlIngressExchange || published.routingKey != riskControlIngressRoutingKey {
		t.Fatalf("published destination = %+v", published)
	}
	if _, exists := published.message.Headers[riskControlRetryHeader]; exists {
		t.Fatalf("retry header was retained: %+v", published.message.Headers)
	}
	if _, exists := published.message.Headers["x-death"]; exists {
		t.Fatalf("dead-letter metadata was retained: %+v", published.message.Headers)
	}
	if published.message.Headers[riskControlReplayHeader] != int32(1) ||
		published.message.Headers[riskControlReplayedAtHeader] != replayedAt.UTC().UnixMilli() ||
		published.message.Headers["traceparent"] != "00-test" {
		t.Fatalf("published headers = %+v", published.message.Headers)
	}
	if report.Entries[0].ReplayCount != 1 ||
		report.Entries[0].Outcome != riskControlReplayOutcomeReplayed ||
		report.Entries[0].WorkflowIdentitySHA256 == "" {
		t.Fatalf("entry = %+v", report.Entries[0])
	}
}

func TestRiskControlDLQReplayerRetainsPoisonLimitedAndInvalidCountMessages(t *testing.T) {
	poisonAck := &riskControlAcknowledgerFake{}
	limitedAck := &riskControlAcknowledgerFake{}
	invalidCountAck := &riskControlAcknowledgerFake{}
	broker := &riskControlReplayBrokerFake{deliveries: []amqp.Delivery{
		riskControlReplayDelivery(poisonAck, 3, riskControlDLQRoutingKey, "{", nil),
		riskControlReplayDelivery(
			limitedAck,
			4,
			riskControlDLQRoutingKey,
			validRiskControlReplayBody(9002, 43, "content"),
			amqp.Table{riskControlReplayHeader: int32(1)},
		),
		riskControlReplayDelivery(
			invalidCountAck,
			5,
			riskControlDLQRoutingKey,
			validRiskControlReplayBody(9003, 44, "content"),
			amqp.Table{riskControlReplayHeader: "not-a-number"},
		),
	}}
	replayer, err := NewRiskControlDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), RiskControlReplayOptions{
		Limit: 3, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "poison review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Inspected != 3 || report.Eligible != 0 || report.Replayed != 0 || report.Retained != 3 {
		t.Fatalf("report = %+v", report)
	}
	if report.Entries[0].ErrorCode != riskControlReplayErrorMalformedEvent ||
		report.Entries[1].ErrorCode != riskControlReplayErrorReplayLimit ||
		report.Entries[2].ErrorCode != riskControlReplayErrorInvalidCount {
		t.Fatalf("entries = %+v", report.Entries)
	}
	for _, ack := range []*riskControlAcknowledgerFake{poisonAck, limitedAck, invalidCountAck} {
		if ack.nacked != 1 || !ack.requeue {
			t.Fatalf("retained ack = %+v", ack)
		}
	}
	if len(broker.published) != 0 {
		t.Fatalf("published = %+v", broker.published)
	}
}

func TestInspectRiskControlReplayDeliveryRejectsUnsupportedRouteAndOversizedEvent(t *testing.T) {
	unsupported, eligible := inspectRiskControlReplayDelivery(amqp.Delivery{
		RoutingKey: "tweet.created.dlq",
		Body:       []byte(validRiskControlReplayBody(9001, 42, "content")),
	}, 1)
	if eligible || unsupported.ErrorCode != riskControlReplayErrorUnsupportedRoute {
		t.Fatalf("unsupported entry = %+v eligible=%t", unsupported, eligible)
	}

	oversized, eligible := inspectRiskControlReplayDelivery(amqp.Delivery{
		RoutingKey: riskControlDLQRoutingKey,
		Body:       make([]byte, riskControlReplayBodyLimit+1),
	}, 1)
	if eligible || oversized.ErrorCode != riskControlReplayErrorOversizedEvent {
		t.Fatalf("oversized entry = %+v eligible=%t", oversized, eligible)
	}
}

func TestRiskControlDLQReplayerPublishFailureRetainsCurrentAndRemaining(t *testing.T) {
	firstAck := &riskControlAcknowledgerFake{}
	secondAck := &riskControlAcknowledgerFake{}
	broker := &riskControlReplayBrokerFake{
		deliveries: []amqp.Delivery{
			riskControlReplayDelivery(firstAck, 6, riskControlDLQRoutingKey, validRiskControlReplayBody(9001, 42, "content"), nil),
			riskControlReplayDelivery(secondAck, 7, riskControlDLQRoutingKey, validRiskControlReplayBody(9002, 43, "content"), nil),
		},
		publishErr: errors.New("rabbitmq unavailable"),
	}
	replayer, err := NewRiskControlDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), RiskControlReplayOptions{
		Limit: 2, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "retry outage",
	})
	if err == nil {
		t.Fatal("expected publish failure")
	}
	if report.Inspected != 1 || report.Replayed != 0 || report.Retained != 1 ||
		report.Entries[0].ErrorCode != riskControlReplayErrorPublishFailed {
		t.Fatalf("report = %+v", report)
	}
	if firstAck.nacked != 1 || !firstAck.requeue || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("first ack=%+v second ack=%+v", firstAck, secondAck)
	}
}

func TestRiskControlDLQReplayerReportsAcknowledgementUncertainty(t *testing.T) {
	firstAck := &riskControlAcknowledgerFake{ackErr: errors.New("channel closed")}
	secondAck := &riskControlAcknowledgerFake{}
	broker := &riskControlReplayBrokerFake{deliveries: []amqp.Delivery{
		riskControlReplayDelivery(firstAck, 8, riskControlDLQRoutingKey, validRiskControlReplayBody(9001, 42, "content"), nil),
		riskControlReplayDelivery(secondAck, 9, riskControlDLQRoutingKey, validRiskControlReplayBody(9002, 43, "content"), nil),
	}}
	replayer, err := NewRiskControlDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), RiskControlReplayOptions{
		Limit: 2, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "ack audit",
	})
	if err == nil {
		t.Fatal("expected acknowledgement failure")
	}
	if report.Uncertain != 1 || report.Replayed != 0 ||
		report.Entries[0].Outcome != riskControlReplayOutcomeAckUncertain ||
		report.Entries[0].ErrorCode != riskControlReplayErrorAckFailed {
		t.Fatalf("report = %+v", report)
	}
	if len(broker.published) != 1 || firstAck.acked != 1 || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("published=%+v first ack=%+v second ack=%+v", broker.published, firstAck, secondAck)
	}
}

func TestRiskControlDLQReplayerExecuteRequiresPublisherConfirmsBeforeReading(t *testing.T) {
	broker := &riskControlReplayBrokerFake{confirmErr: errors.New("confirm unavailable")}
	replayer, err := NewRiskControlDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	_, err = replayer.Run(context.Background(), RiskControlReplayOptions{
		Limit: 1, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "incident",
	})
	if err == nil {
		t.Fatal("expected publisher confirm initialization to fail")
	}
	if len(broker.requestedAt) != 0 {
		t.Fatalf("read messages before publisher confirm: %+v", broker.requestedAt)
	}
}

func TestRiskControlDLQReplayerCancellationRequeuesBufferedMessages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	firstAck := &riskControlAcknowledgerFake{}
	secondAck := &riskControlAcknowledgerFake{}
	broker := &riskControlReplayBrokerFake{
		deliveries: []amqp.Delivery{
			riskControlReplayDelivery(firstAck, 10, riskControlDLQRoutingKey, validRiskControlReplayBody(9001, 42, "content"), nil),
			riskControlReplayDelivery(secondAck, 11, riskControlDLQRoutingKey, validRiskControlReplayBody(9002, 43, "content"), nil),
		},
		afterGet: func(index int) {
			if index == 2 {
				cancel()
			}
		},
	}
	replayer, err := NewRiskControlDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(ctx, RiskControlReplayOptions{
		Limit: 2, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "cancel audit",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if report.Inspected != 0 || len(broker.published) != 0 {
		t.Fatalf("report=%+v published=%+v", report, broker.published)
	}
	if firstAck.nacked != 1 || !firstAck.requeue || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("first ack=%+v second ack=%+v", firstAck, secondAck)
	}
}

func TestRiskControlReplayOptionsRequireAuditFieldsForExecution(t *testing.T) {
	options := RiskControlReplayOptions{Limit: 1, Execute: true, MaxReplayCount: 1}
	if err := options.Validate(); err == nil {
		t.Fatal("expected missing operator and reason to fail")
	}
}

func TestRiskControlReplayReportDoesNotExposeEventOrAuditInput(t *testing.T) {
	ack := &riskControlAcknowledgerFake{}
	broker := &riskControlReplayBrokerFake{deliveries: []amqp.Delivery{
		riskControlReplayDelivery(
			ack,
			12,
			riskControlDLQRoutingKey,
			validRiskControlReplayBody(991122, 887766, "customer-private-risk-content"),
			nil,
		),
	}}
	replayer, err := NewRiskControlDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}
	report, err := replayer.Run(context.Background(), RiskControlReplayOptions{
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
		"alice@example.com",
		"customer escalation",
		"customer-private-risk-content",
		"991122",
		"887766",
		"tweet_id",
		"author_id",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("report exposed %q: %s", forbidden, serialized)
		}
	}
}

func riskControlReplayDelivery(
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

func validRiskControlReplayBody(tweetID, authorID uint64, content string) string {
	body, err := json.Marshal(events.TweetCreatedEvent{
		TweetID: tweetID, AuthorID: authorID, Content: content, CreatedAt: 1_700_000_000_000,
	})
	if err != nil {
		panic(err)
	}
	return string(body)
}
