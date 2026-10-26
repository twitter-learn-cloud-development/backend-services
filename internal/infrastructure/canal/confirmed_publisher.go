package canal

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type ConfirmPublisher interface {
	EnablePublisherConfirms() error
	PublishMessageConfirmed(context.Context, string, string, amqp.Publishing) error
}
type confirmedRelayPublisher struct{ publisher ConfirmPublisher }

func NewConfirmedRelayPublisher(publisher ConfirmPublisher) (MQProducer, error) {
	if publisher == nil {
		return nil, errors.New("relay confirm publisher unavailable")
	}
	if err := publisher.EnablePublisherConfirms(); err != nil {
		return nil, fmt.Errorf("relay confirm setup: %w", err)
	}
	return &confirmedRelayPublisher{publisher: publisher}, nil
}
func (p *confirmedRelayPublisher) PublishRawJSON(ctx context.Context, exchange, key string, body []byte) error {
	digest := sha256.Sum256(append([]byte(key+":"), body...))
	return p.publisher.PublishMessageConfirmed(ctx, exchange, key, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent, Timestamp: time.Now(),
		MessageId: fmt.Sprintf("%x", digest[:]), Body: body,
	})
}
