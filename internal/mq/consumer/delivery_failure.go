package consumer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"

	"twitter-clone/pkg/logger"
)

const (
	timelineRetryHeader         = "x-retry-count"
	timelineFailureRouteTimeout = 5 * time.Second
)

type timelineFailurePublisher interface {
	EnablePublisherConfirms() error
	PublishMessageConfirmed(ctx context.Context, exchange, routingKey string, message amqp.Publishing) error
}

type timelineFailureRouter struct {
	publisher timelineFailurePublisher
	wait      func(time.Duration)
}

func newTimelineFailureRouter(publisher timelineFailurePublisher) (*timelineFailureRouter, error) {
	if publisher == nil {
		return nil, errors.New("timeline failure publisher is required")
	}
	if err := publisher.EnablePublisherConfirms(); err != nil {
		return nil, fmt.Errorf("enable timeline failure publisher confirms: %w", err)
	}
	return &timelineFailureRouter{publisher: publisher, wait: time.Sleep}, nil
}

func (r *timelineFailureRouter) route(message amqp.Delivery, routingKey string, permanent bool) failureDisposition {
	retryCount, validRetryCount := timelineRetryCount(message.Headers)
	if !validRetryCount {
		permanent = true
		retryCount = MaxRetries
	}
	recoveryRoute, supported := timelineRecoveryRouteForSource(routingKey)
	if !supported {
		log.Printf("timeline failure routing rejected: routing_key=%s result=unsupported", routingKey)
		return r.requeueAfterFailure(message, retryCount)
	}

	exchange := ExchangeTimelineRetry
	destination := recoveryRoute.retryRoutingKey
	disposition := failureDispositionRetried
	publishing := copyTimelineFailureMessage(message)
	if permanent || retryCount >= MaxRetries {
		exchange = ExchangeDLX
		destination = routingKey + ".dlq"
		disposition = failureDispositionDLQ
		publishing.Expiration = ""
	} else {
		retryCount++
		publishing.Headers[timelineRetryHeader] = int32(retryCount)
		publishing.Expiration = strconv.FormatInt(
			int64(time.Second/time.Millisecond)*(1<<uint(retryCount-1)),
			10,
		)
	}

	publishCtx, cancel := context.WithTimeout(context.Background(), timelineFailureRouteTimeout)
	err := r.publisher.PublishMessageConfirmed(publishCtx, exchange, destination, publishing)
	cancel()
	if err != nil {
		log.Printf(
			"timeline failure routing publish failed: routing_key=%s disposition=%s retry_count=%d error=%v",
			routingKey,
			disposition,
			retryCount,
			err,
		)
		return r.requeueAfterFailure(message, retryCount)
	}

	if err := message.Ack(false); err != nil {
		log.Printf(
			"timeline failure routing acknowledgement uncertain: routing_key=%s disposition=%s retry_count=%d error=%v",
			routingKey,
			disposition,
			retryCount,
			err,
		)
		return failureDispositionAckUncertain
	}
	if disposition == failureDispositionDLQ {
		logger.Error(context.Background(), "Message exceeded max retries and routed to DLQ",
			zap.String("routing_key", routingKey),
			zap.Int("retry_count", retryCount),
			zap.Int("body_bytes", len(message.Body)),
		)
	}
	return disposition
}

func (r *timelineFailureRouter) requeueAfterFailure(message amqp.Delivery, retryCount int) failureDisposition {
	delay := timelineFailureRequeueDelay(retryCount)
	if r.wait != nil {
		r.wait(delay)
	}
	if err := message.Nack(false, true); err != nil {
		log.Printf("timeline failure fallback requeue failed: delay=%s error=%v", delay, err)
	}
	return failureDispositionRequeued
}

func timelineRetryCount(headers amqp.Table) (int, bool) {
	if headers == nil {
		return 0, true
	}
	value, exists := headers[timelineRetryHeader]
	if !exists {
		return 0, true
	}
	var count int64
	switch typed := value.(type) {
	case int8:
		count = int64(typed)
	case int16:
		count = int64(typed)
	case int32:
		count = int64(typed)
	case int64:
		count = typed
	case int:
		count = int64(typed)
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 32)
		if err != nil {
			return 0, false
		}
		count = parsed
	default:
		return 0, false
	}
	if count < 0 || count > MaxRetries {
		return 0, false
	}
	return int(count), true
}

func timelineFailureRequeueDelay(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount > 2 {
		retryCount = 2
	}
	return time.Second * time.Duration(1<<uint(retryCount))
}

func copyTimelineFailureMessage(message amqp.Delivery) amqp.Publishing {
	headers := make(amqp.Table, len(message.Headers)+1)
	for key, value := range message.Headers {
		headers[key] = value
	}
	return amqp.Publishing{
		Headers:         headers,
		ContentType:     message.ContentType,
		ContentEncoding: message.ContentEncoding,
		DeliveryMode:    amqp.Persistent,
		Priority:        message.Priority,
		CorrelationId:   message.CorrelationId,
		ReplyTo:         message.ReplyTo,
		MessageId:       message.MessageId,
		Timestamp:       message.Timestamp,
		Type:            message.Type,
		Body:            append([]byte(nil), message.Body...),
	}
}
