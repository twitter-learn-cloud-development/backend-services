package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type timelineFailurePublisherFake struct {
	confirmErr   error
	publishErr   error
	confirmCalls int
	published    []timelineFailurePublished
	operations   *[]string
}

type timelineFailurePublished struct {
	exchange   string
	routingKey string
	message    amqp.Publishing
}

func (p *timelineFailurePublisherFake) EnablePublisherConfirms() error {
	p.confirmCalls++
	if p.operations != nil {
		*p.operations = append(*p.operations, "confirm")
	}
	return p.confirmErr
}

func (p *timelineFailurePublisherFake) PublishMessageConfirmed(
	_ context.Context,
	exchange string,
	routingKey string,
	message amqp.Publishing,
) error {
	if p.operations != nil {
		*p.operations = append(*p.operations, "publish")
	}
	if p.publishErr != nil {
		return p.publishErr
	}
	p.published = append(p.published, timelineFailurePublished{
		exchange: exchange, routingKey: routingKey, message: message,
	})
	return nil
}

type timelineFailureAcknowledgerFake struct {
	acked      int
	nacked     int
	requeue    bool
	ackErr     error
	nackErr    error
	operations *[]string
}

func (a *timelineFailureAcknowledgerFake) Ack(uint64, bool) error {
	a.acked++
	if a.operations != nil {
		*a.operations = append(*a.operations, "ack")
	}
	return a.ackErr
}

func (a *timelineFailureAcknowledgerFake) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked++
	a.requeue = requeue
	if a.operations != nil {
		*a.operations = append(*a.operations, "nack")
	}
	return a.nackErr
}

func (a *timelineFailureAcknowledgerFake) Reject(_ uint64, requeue bool) error {
	return a.Nack(0, false, requeue)
}

func TestTimelineFailureRouterConfirmsRetryBeforeAcknowledgement(t *testing.T) {
	operations := make([]string, 0, 3)
	publisher := &timelineFailurePublisherFake{operations: &operations}
	router, err := newTimelineFailureRouter(publisher)
	if err != nil {
		t.Fatal(err)
	}
	ack := &timelineFailureAcknowledgerFake{operations: &operations}
	delivery := timelineFailureDelivery(ack, nil)

	disposition := router.route(delivery, RoutingKeyTweetCreated, false)
	if disposition != failureDispositionRetried {
		t.Fatalf("disposition = %s", disposition)
	}
	if len(publisher.published) != 1 || ack.acked != 1 || ack.nacked != 0 {
		t.Fatalf("published=%+v ack=%+v", publisher.published, ack)
	}
	if got := operations; len(got) != 3 || got[0] != "confirm" || got[1] != "publish" || got[2] != "ack" {
		t.Fatalf("operation order = %v", got)
	}
	published := publisher.published[0]
	if published.exchange != ExchangeTimelineRetry || published.routingKey != RoutingKeyTimelineTweetCreatedRetry ||
		published.message.Expiration != "1000" || published.message.Headers[timelineRetryHeader] != int32(1) {
		t.Fatalf("published retry = %+v", published)
	}
	if delivery.Headers != nil {
		t.Fatalf("source headers were mutated: %+v", delivery.Headers)
	}
}

func TestTimelineFailureRouterRoutesPermanentAndInvalidRetryCountToDLQ(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		permanent bool
		headers   amqp.Table
	}{
		{name: "permanent", permanent: true},
		{name: "invalid retry count", headers: amqp.Table{timelineRetryHeader: "invalid"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			publisher := &timelineFailurePublisherFake{}
			router, err := newTimelineFailureRouter(publisher)
			if err != nil {
				t.Fatal(err)
			}
			ack := &timelineFailureAcknowledgerFake{}
			disposition := router.route(
				timelineFailureDelivery(ack, testCase.headers),
				RoutingKeyTweetDeleted,
				testCase.permanent,
			)
			if disposition != failureDispositionDLQ || len(publisher.published) != 1 || ack.acked != 1 {
				t.Fatalf("disposition=%s published=%+v ack=%+v", disposition, publisher.published, ack)
			}
			published := publisher.published[0]
			if published.exchange != ExchangeDLX || published.routingKey != RoutingKeyTweetDeleted+".dlq" ||
				published.message.Expiration != "" {
				t.Fatalf("published dlq = %+v", published)
			}
		})
	}
}

func TestTimelineFailureRouterPublishFailureWaitsBeforeRequeue(t *testing.T) {
	publisher := &timelineFailurePublisherFake{publishErr: errors.New("broker unavailable")}
	router, err := newTimelineFailureRouter(publisher)
	if err != nil {
		t.Fatal(err)
	}
	var waited time.Duration
	router.wait = func(delay time.Duration) { waited = delay }
	ack := &timelineFailureAcknowledgerFake{}

	disposition := router.route(timelineFailureDelivery(ack, amqp.Table{timelineRetryHeader: int32(1)}), RoutingKeyTweetModerated, false)
	if disposition != failureDispositionRequeued || waited != 4*time.Second {
		t.Fatalf("disposition=%s waited=%s", disposition, waited)
	}
	if ack.acked != 0 || ack.nacked != 1 || !ack.requeue {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestTimelineFailureRouterReportsAcknowledgementUncertainty(t *testing.T) {
	publisher := &timelineFailurePublisherFake{}
	router, err := newTimelineFailureRouter(publisher)
	if err != nil {
		t.Fatal(err)
	}
	ack := &timelineFailureAcknowledgerFake{ackErr: errors.New("channel closed")}

	disposition := router.route(timelineFailureDelivery(ack, nil), RoutingKeyTweetCreated, false)
	if disposition != failureDispositionAckUncertain || len(publisher.published) != 1 || ack.acked != 1 {
		t.Fatalf("disposition=%s published=%+v ack=%+v", disposition, publisher.published, ack)
	}
}

func TestNewTimelineFailureRouterRequiresPublisherConfirms(t *testing.T) {
	_, err := newTimelineFailureRouter(&timelineFailurePublisherFake{confirmErr: errors.New("confirm unavailable")})
	if err == nil {
		t.Fatal("expected publisher confirm initialization failure")
	}
}

func timelineFailureDelivery(ack amqp.Acknowledger, headers amqp.Table) amqp.Delivery {
	return amqp.Delivery{
		Acknowledger: ack,
		DeliveryTag:  1,
		RoutingKey:   RoutingKeyTweetCreated,
		ContentType:  "application/json",
		Headers:      headers,
		Body:         []byte(`{"tweet_id":9001,"author_id":42}`),
		Timestamp:    time.UnixMilli(1_700_000_000_000),
	}
}
