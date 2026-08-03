package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	"twitter-clone/internal/events"
)

const (
	riskControlEventsExchange  = "twitter.events"
	riskControlIngressExchange = "agent.risk.ingress"
	riskControlRetryExchange   = "agent.risk.retry"
	riskControlDLX             = "agent.risk.dlx"

	riskControlQueue      = "queue.tweet.risk"
	riskControlRetryQueue = "queue.agent.risk.retry.v1"
	riskControlDLQ        = "queue.agent.risk.dlq.v1"

	riskControlSourceRoutingKey  = "tweet.created"
	riskControlIngressRoutingKey = "tweet.created.agent-risk"
	riskControlRetryRoutingKey   = "tweet.created.agent-risk.retry"
	riskControlDLQRoutingKey     = "tweet.created.agent-risk.dlq"

	riskControlRetryHeader     = "x-agent-risk-retry-count"
	riskControlConsumerName    = "agent-risk-control-v1"
	riskControlPrefetch        = 8
	riskControlMaxRetries      = 3
	riskControlDispatchTimeout = 10 * time.Second
	riskControlPublishTimeout  = 5 * time.Second
	riskControlReconnectDelay  = 5 * time.Second
)

type riskControlBroker interface {
	DeclareExchange(name, kind string, durable bool) error
	DeclareQueue(name string, durable bool) (amqp.Queue, error)
	DeclareQueueWithArgs(name string, durable bool, args amqp.Table) (amqp.Queue, error)
	BindQueue(queueName, routingKey, exchangeName string) error
	SetQoS(prefetchCount int) error
	EnablePublisherConfirms() error
	Consume(queueName, consumer string) (<-chan amqp.Delivery, error)
	PublishMessageConfirmed(ctx context.Context, exchange, routingKey string, message amqp.Publishing) error
}

type riskWorkflowClient interface {
	ExecuteWorkflow(
		ctx context.Context,
		options client.StartWorkflowOptions,
		workflow interface{},
		args ...interface{},
	) (client.WorkflowRun, error)
}

type RiskControlObserver interface {
	Observe(result string)
}

type noopRiskControlObserver struct{}

func (noopRiskControlObserver) Observe(string) {}

type RiskControl struct {
	broker         riskControlBroker
	temporalClient riskWorkflowClient
	observer       RiskControlObserver
	wait           func(context.Context, time.Duration) bool
}

func DeclareRiskControlTopology(broker riskControlBroker) error {
	if broker == nil {
		return errors.New("risk control broker is required")
	}
	for _, exchange := range []string{
		riskControlEventsExchange,
		riskControlIngressExchange,
		riskControlRetryExchange,
		riskControlDLX,
	} {
		if err := broker.DeclareExchange(exchange, "topic", true); err != nil {
			return fmt.Errorf("declare risk control exchange %s: %w", exchange, err)
		}
	}
	if _, err := broker.DeclareQueue(riskControlQueue, true); err != nil {
		return fmt.Errorf("declare risk control queue: %w", err)
	}
	if _, err := broker.DeclareQueue(riskControlDLQ, true); err != nil {
		return fmt.Errorf("declare risk control dlq: %w", err)
	}
	retryArgs := amqp.Table{
		"x-dead-letter-exchange":    riskControlIngressExchange,
		"x-dead-letter-routing-key": riskControlIngressRoutingKey,
	}
	if _, err := broker.DeclareQueueWithArgs(riskControlRetryQueue, true, retryArgs); err != nil {
		return fmt.Errorf("declare risk control retry queue: %w", err)
	}
	bindings := []struct {
		queue      string
		routingKey string
		exchange   string
	}{
		{riskControlQueue, riskControlSourceRoutingKey, riskControlEventsExchange},
		{riskControlQueue, riskControlIngressRoutingKey, riskControlIngressExchange},
		{riskControlRetryQueue, riskControlRetryRoutingKey, riskControlRetryExchange},
		{riskControlDLQ, riskControlDLQRoutingKey, riskControlDLX},
	}
	for _, binding := range bindings {
		if err := broker.BindQueue(binding.queue, binding.routingKey, binding.exchange); err != nil {
			return fmt.Errorf(
				"bind risk control queue %s to %s/%s: %w",
				binding.queue,
				binding.exchange,
				binding.routingKey,
				err,
			)
		}
	}
	if err := broker.SetQoS(riskControlPrefetch); err != nil {
		return fmt.Errorf("configure risk control qos: %w", err)
	}
	return nil
}

func NewRiskControl(
	broker riskControlBroker,
	temporalClient riskWorkflowClient,
	observer RiskControlObserver,
) (*RiskControl, error) {
	if broker == nil || temporalClient == nil {
		return nil, errors.New("risk control broker and temporal client are required")
	}
	if err := DeclareRiskControlTopology(broker); err != nil {
		return nil, err
	}
	if err := broker.EnablePublisherConfirms(); err != nil {
		return nil, fmt.Errorf("enable risk control publisher confirms: %w", err)
	}
	if observer == nil {
		observer = noopRiskControlObserver{}
	}
	return &RiskControl{
		broker:         broker,
		temporalClient: temporalClient,
		observer:       observer,
		wait:           waitRiskControlDelay,
	}, nil
}

func (r *RiskControl) Run(ctx context.Context) error {
	for {
		messages, err := r.broker.Consume(riskControlQueue, riskControlConsumerName)
		if err != nil {
			slog.ErrorContext(ctx, "consume risk control queue failed", "error", err)
			if !r.wait(ctx, riskControlReconnectDelay) {
				return nil
			}
			continue
		}

		for {
			select {
			case <-ctx.Done():
				return nil
			case message, ok := <-messages:
				if !ok {
					slog.WarnContext(ctx, "risk control delivery channel closed")
					if !r.wait(ctx, riskControlReconnectDelay) {
						return nil
					}
					goto reconnect
				}
				r.handle(ctx, message)
			}
		}
	reconnect:
	}
}

func (r *RiskControl) handle(ctx context.Context, message amqp.Delivery) {
	r.observer.Observe("received")
	event, err := decodeRiskControlEvent(message.Body)
	if err != nil {
		r.observer.Observe("malformed")
		r.routeFailure(ctx, message, true, err)
		return
	}

	workflowID := fmt.Sprintf("RiskControl-Tweet-%d", event.TweetID)
	dispatchCtx, cancel := context.WithTimeout(ctx, riskControlDispatchTimeout)
	_, err = r.temporalClient.ExecuteWorkflow(
		dispatchCtx,
		client.StartWorkflowOptions{
			ID:                                       workflowID,
			TaskQueue:                                "AGENT_TASK_QUEUE",
			WorkflowIDReusePolicy:                    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
			WorkflowExecutionErrorWhenAlreadyStarted: true,
		},
		TweetRiskControlWorkflow,
		event,
	)
	cancel()
	if err != nil {
		if temporal.IsWorkflowExecutionAlreadyStartedError(err) {
			r.observer.Observe("duplicate")
			r.ack(message, "duplicate")
			return
		}
		if ctx.Err() != nil {
			r.observer.Observe("shutdown_requeued")
			if nackErr := message.Nack(false, true); nackErr != nil {
				slog.ErrorContext(ctx, "requeue canceled risk control event failed", "error", nackErr)
			}
			return
		}
		r.routeFailure(ctx, message, false, err)
		return
	}

	r.observer.Observe("dispatched")
	r.ack(message, "dispatched")
}

func decodeRiskControlEvent(body []byte) (events.TweetCreatedEvent, error) {
	var event events.TweetCreatedEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return events.TweetCreatedEvent{}, fmt.Errorf("decode tweet created event: %w", err)
	}
	if event.TweetID == 0 || event.AuthorID == 0 {
		return events.TweetCreatedEvent{}, errors.New("tweet_id and author_id are required")
	}
	return event, nil
}

func (r *RiskControl) routeFailure(
	ctx context.Context,
	message amqp.Delivery,
	permanent bool,
	cause error,
) {
	retryCount, validRetryCount := riskControlRetryCount(message.Headers)
	if !validRetryCount {
		permanent = true
		retryCount = riskControlMaxRetries
	}
	exchange := riskControlRetryExchange
	routingKey := riskControlRetryRoutingKey
	result := "retried"
	publishing := copyRiskControlMessage(message)
	if permanent || retryCount >= riskControlMaxRetries {
		exchange = riskControlDLX
		routingKey = riskControlDLQRoutingKey
		result = "dlq"
		publishing.Expiration = ""
	} else {
		retryCount++
		publishing.Headers[riskControlRetryHeader] = int32(retryCount)
		publishing.Expiration = strconv.FormatInt(
			int64(time.Second/time.Millisecond)*(1<<uint(retryCount-1)),
			10,
		)
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), riskControlPublishTimeout)
	err := r.broker.PublishMessageConfirmed(publishCtx, exchange, routingKey, publishing)
	cancel()
	if err != nil {
		r.observer.Observe("publish_failed")
		slog.ErrorContext(ctx, "route risk control event failed",
			"result", result,
			"retry_count", retryCount,
			"error", err,
			"cause", cause,
		)
		r.requeueAfterPublishFailure(ctx, message, retryCount)
		return
	}

	r.observer.Observe(result)
	slog.WarnContext(ctx, "risk control event deferred",
		"result", result,
		"retry_count", retryCount,
		"cause", cause,
	)
	r.ack(message, result)
}

func (r *RiskControl) ack(message amqp.Delivery, result string) {
	if err := message.Ack(false); err != nil {
		r.observer.Observe("acknowledgement_uncertain")
		slog.Warn("ack risk control event failed", "result", result, "error", err)
	}
}

func (r *RiskControl) requeueAfterPublishFailure(ctx context.Context, message amqp.Delivery, retryCount int) {
	delay := riskControlFallbackDelay(retryCount)
	if r.wait != nil {
		r.wait(ctx, delay)
	}
	if err := message.Nack(false, true); err != nil {
		slog.ErrorContext(ctx, "fallback requeue risk control event failed", "delay", delay, "error", err)
		return
	}
	r.observer.Observe("requeued")
}

func riskControlRetryCount(headers amqp.Table) (int, bool) {
	if headers == nil {
		return 0, true
	}
	value, exists := headers[riskControlRetryHeader]
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
	if count < 0 || count > riskControlMaxRetries {
		return 0, false
	}
	return int(count), true
}

func riskControlFallbackDelay(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount > 2 {
		retryCount = 2
	}
	return time.Second * time.Duration(1<<uint(retryCount))
}

func waitRiskControlDelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func copyRiskControlMessage(message amqp.Delivery) amqp.Publishing {
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
