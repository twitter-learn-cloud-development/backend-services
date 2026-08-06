package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type contentEngagementReplayBrokerFake struct {
	deliveries  []amqp.Delivery
	getIndex    int
	published   []publishedContentEngagementMessage
	publishErr  error
	confirmErr  error
	requestedAt []string
}

func (b *contentEngagementReplayBrokerFake) EnablePublisherConfirms() error {
	return b.confirmErr
}

func (b *contentEngagementReplayBrokerFake) GetMessage(queueName string, autoAck bool) (amqp.Delivery, bool, error) {
	if autoAck {
		return amqp.Delivery{}, false, errors.New("replay must use manual acknowledgement")
	}
	b.requestedAt = append(b.requestedAt, queueName)
	if b.getIndex >= len(b.deliveries) {
		return amqp.Delivery{}, false, nil
	}
	delivery := b.deliveries[b.getIndex]
	b.getIndex++
	return delivery, true, nil
}

func (b *contentEngagementReplayBrokerFake) PublishMessageConfirmed(
	_ context.Context,
	exchange string,
	routingKey string,
	message amqp.Publishing,
) error {
	if b.publishErr != nil {
		return b.publishErr
	}
	b.published = append(b.published, publishedContentEngagementMessage{
		exchange: exchange, routingKey: routingKey, message: message,
	})
	return nil
}

func TestContentEngagementDLQReplayerInspectRetainsEligibleMessages(t *testing.T) {
	firstAck := &deliveryAcknowledgerFake{}
	secondAck := &deliveryAcknowledgerFake{}
	broker := &contentEngagementReplayBrokerFake{deliveries: []amqp.Delivery{
		contentEngagementDLQDelivery(firstAck, 1, dlqRoutingKey(routingTweetLiked),
			`{"tweet_id":9001,"user_id":77,"tweet_user_id":42,"occurred_at_unix_ms":1700000000000}`, nil),
		contentEngagementDLQDelivery(secondAck, 2, dlqRoutingKey(routingCommentCreate),
			`{"comment_id":8,"tweet_id":9001,"user_id":78,"tweet_user_id":42,"occurred_at_unix_ms":1700000000001}`, nil),
	}}
	replayer, err := NewContentEngagementDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), ContentEngagementReplayOptions{
		Limit: 2, MaxReplayCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "inspect" || report.Inspected != 2 || report.Eligible != 2 ||
		report.Replayed != 0 || report.Retained != 0 || report.Empty {
		t.Fatalf("report = %+v", report)
	}
	if firstAck.acked != 0 || firstAck.nacked != 1 || !firstAck.requeue ||
		secondAck.acked != 0 || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("first ack=%+v second ack=%+v", firstAck, secondAck)
	}
	if len(broker.published) != 0 {
		t.Fatalf("inspect published %d messages", len(broker.published))
	}
}

func TestContentEngagementDLQReplayerExecutePublishesValidatedMessage(t *testing.T) {
	ack := &deliveryAcknowledgerFake{}
	broker := &contentEngagementReplayBrokerFake{deliveries: []amqp.Delivery{
		contentEngagementDLQDelivery(ack, 3, dlqRoutingKey(routingTweetLiked),
			`{"tweet_id":9001,"user_id":77,"tweet_user_id":42,"occurred_at_unix_ms":1700000000000}`,
			amqp.Table{"x-agent-profile-retry-count": int32(3), "traceparent": "00-test"}),
	}}
	replayer, err := NewContentEngagementDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}
	replayedAt := time.Unix(1_800_000_000, 123_000_000)
	replayer.now = func() time.Time { return replayedAt }

	report, err := replayer.Run(context.Background(), ContentEngagementReplayOptions{
		Limit: 1, Execute: true, MaxReplayCount: 2, Operator: "on-call", Reason: "incident-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != "execute" || report.Replayed != 1 || report.Eligible != 1 ||
		report.Operator != "on-call" || report.ReasonSHA256 == "" {
		t.Fatalf("report = %+v", report)
	}
	if ack.acked != 1 || ack.nacked != 0 || len(broker.published) != 1 {
		t.Fatalf("ack=%+v published=%+v", ack, broker.published)
	}
	published := broker.published[0]
	if published.exchange != contentEngagementExchange || published.routingKey != routingTweetLiked {
		t.Fatalf("published destination = %+v", published)
	}
	if _, exists := published.message.Headers["x-agent-profile-retry-count"]; exists {
		t.Fatalf("retry header was retained: %+v", published.message.Headers)
	}
	if published.message.Headers[contentEngagementReplayHeader] != int32(1) ||
		published.message.Headers[contentEngagementReplayedAtHeader] != replayedAt.UTC().UnixMilli() ||
		published.message.Headers["traceparent"] != "00-test" {
		t.Fatalf("published headers = %+v", published.message.Headers)
	}
	if report.Entries[0].ReplayCount != 1 || report.Entries[0].Outcome != replayOutcomeReplayed {
		t.Fatalf("entry = %+v", report.Entries[0])
	}
}

func TestContentEngagementDLQReplayerRetainsPoisonAndReplayLimitedMessages(t *testing.T) {
	poisonAck := &deliveryAcknowledgerFake{}
	limitedAck := &deliveryAcknowledgerFake{}
	broker := &contentEngagementReplayBrokerFake{deliveries: []amqp.Delivery{
		contentEngagementDLQDelivery(poisonAck, 4, dlqRoutingKey(routingCommentCreate), `{`, nil),
		contentEngagementDLQDelivery(limitedAck, 5, dlqRoutingKey(routingTweetLiked),
			`{"tweet_id":9001,"user_id":77,"tweet_user_id":42,"occurred_at_unix_ms":1700000000000}`,
			amqp.Table{contentEngagementReplayHeader: int32(1)}),
	}}
	replayer, err := NewContentEngagementDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), ContentEngagementReplayOptions{
		Limit: 2, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "poison review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Inspected != 2 || report.Eligible != 0 || report.Replayed != 0 || report.Retained != 2 {
		t.Fatalf("report = %+v", report)
	}
	if report.Entries[0].ErrorCode != replayErrorMalformedEvent ||
		report.Entries[1].ErrorCode != replayErrorReplayLimit {
		t.Fatalf("entries = %+v", report.Entries)
	}
	if poisonAck.nacked != 1 || !poisonAck.requeue || limitedAck.nacked != 1 || !limitedAck.requeue {
		t.Fatalf("poison ack=%+v limited ack=%+v", poisonAck, limitedAck)
	}
	if len(broker.published) != 0 {
		t.Fatalf("published = %+v", broker.published)
	}
}

func TestContentEngagementDLQReplayerPublishFailureRetainsCurrentAndRemaining(t *testing.T) {
	firstAck := &deliveryAcknowledgerFake{}
	secondAck := &deliveryAcknowledgerFake{}
	broker := &contentEngagementReplayBrokerFake{
		deliveries: []amqp.Delivery{
			contentEngagementDLQDelivery(firstAck, 6, dlqRoutingKey(routingTweetLiked),
				`{"tweet_id":9001,"user_id":77,"tweet_user_id":42,"occurred_at_unix_ms":1700000000000}`, nil),
			contentEngagementDLQDelivery(secondAck, 7, dlqRoutingKey(routingTweetLiked),
				`{"tweet_id":9002,"user_id":78,"tweet_user_id":43,"occurred_at_unix_ms":1700000000001}`, nil),
		},
		publishErr: errors.New("rabbitmq unavailable"),
	}
	replayer, err := NewContentEngagementDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), ContentEngagementReplayOptions{
		Limit: 2, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "retry outage",
	})
	if err == nil {
		t.Fatal("expected publish failure")
	}
	if report.Inspected != 1 || report.Replayed != 0 || report.Retained != 1 ||
		report.Entries[0].ErrorCode != replayErrorPublishFailed {
		t.Fatalf("report = %+v", report)
	}
	if firstAck.nacked != 1 || !firstAck.requeue || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("first ack=%+v second ack=%+v", firstAck, secondAck)
	}
}

func TestContentEngagementDLQReplayerReportsAcknowledgementUncertainty(t *testing.T) {
	firstAck := &deliveryAcknowledgerFake{ackErr: errors.New("channel closed")}
	secondAck := &deliveryAcknowledgerFake{}
	broker := &contentEngagementReplayBrokerFake{deliveries: []amqp.Delivery{
		contentEngagementDLQDelivery(firstAck, 8, dlqRoutingKey(routingTweetLiked),
			`{"tweet_id":9001,"user_id":77,"tweet_user_id":42,"occurred_at_unix_ms":1700000000000}`, nil),
		contentEngagementDLQDelivery(secondAck, 9, dlqRoutingKey(routingTweetLiked),
			`{"tweet_id":9002,"user_id":78,"tweet_user_id":43,"occurred_at_unix_ms":1700000000001}`, nil),
	}}
	replayer, err := NewContentEngagementDLQReplayer(broker)
	if err != nil {
		t.Fatal(err)
	}

	report, err := replayer.Run(context.Background(), ContentEngagementReplayOptions{
		Limit: 2, Execute: true, MaxReplayCount: 1, Operator: "on-call", Reason: "ack audit",
	})
	if err == nil {
		t.Fatal("expected acknowledgement failure")
	}
	if report.Uncertain != 1 || report.Replayed != 0 ||
		report.Entries[0].Outcome != replayOutcomeAcknowledgement ||
		report.Entries[0].ErrorCode != replayErrorAcknowledgementFail {
		t.Fatalf("report = %+v", report)
	}
	if len(broker.published) != 1 || firstAck.acked != 1 || secondAck.nacked != 1 || !secondAck.requeue {
		t.Fatalf("published=%+v first ack=%+v second ack=%+v", broker.published, firstAck, secondAck)
	}
}

func TestContentEngagementReplayOptionsRequireAuditFieldsForExecution(t *testing.T) {
	replayer, err := NewContentEngagementDLQReplayer(&contentEngagementReplayBrokerFake{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = replayer.Run(context.Background(), ContentEngagementReplayOptions{
		Limit: 1, Execute: true, MaxReplayCount: 1,
	})
	if err == nil {
		t.Fatal("expected missing operator and reason to fail")
	}
}

func TestNewContentEngagementDLQReplayerRequiresPublisherConfirms(t *testing.T) {
	_, err := NewContentEngagementDLQReplayer(&contentEngagementReplayBrokerFake{
		confirmErr: errors.New("confirm unavailable"),
	})
	if err == nil {
		t.Fatal("expected publisher confirm initialization to fail")
	}
}

func contentEngagementDLQDelivery(
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
