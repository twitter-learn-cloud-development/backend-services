package consumer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	timelineEventReplayHeader     = "x-timeline-event-replay-count"
	timelineEventReplayedAtHeader = "x-timeline-event-replayed-at-unix-ms"

	timelineEventReplayLimitMax      = 100
	timelineEventReplayCountMax      = 10
	timelineEventReplayOperatorLimit = 128
	timelineEventReplayReasonLimit   = 512
	timelineEventReplayBodyLimit     = 1 << 20

	timelineEventReplayOutcomeEligible        = "eligible"
	timelineEventReplayOutcomeReplayed        = "replayed"
	timelineEventReplayOutcomeRetainedInvalid = "retained_invalid"
	timelineEventReplayOutcomeRetainedLimit   = "retained_replay_limit"
	timelineEventReplayOutcomeRetainedPublish = "retained_publish_failed"
	timelineEventReplayOutcomeAckUncertain    = "acknowledgement_uncertain"

	timelineEventReplayErrorUnsupportedRoute = "unsupported_routing_key"
	timelineEventReplayErrorMalformedEvent   = "malformed_event"
	timelineEventReplayErrorOversizedEvent   = "oversized_event"
	timelineEventReplayErrorInvalidCount     = "invalid_replay_count"
	timelineEventReplayErrorReplayLimit      = "replay_limit_reached"
	timelineEventReplayErrorPublishFailed    = "publish_failed"
	timelineEventReplayErrorAckFailed        = "ack_failed"
)

type TimelineEventKind string

const (
	TimelineEventCreated TimelineEventKind = "created"
	TimelineEventDeleted TimelineEventKind = "deleted"
)

type timelineEventReplayRoute struct {
	kind              TimelineEventKind
	queue             string
	dlqRoutingKey     string
	ingressRoutingKey string
}

func timelineEventReplayRouteForKind(kind TimelineEventKind) (timelineEventReplayRoute, bool) {
	switch kind {
	case TimelineEventCreated:
		return timelineEventReplayRoute{
			kind:              kind,
			queue:             QueueTweetFanoutDLQ,
			dlqRoutingKey:     RoutingKeyTweetCreated + ".dlq",
			ingressRoutingKey: RoutingKeyTimelineTweetCreated,
		}, true
	case TimelineEventDeleted:
		return timelineEventReplayRoute{
			kind:              kind,
			queue:             QueueTweetDeleteDLQ,
			dlqRoutingKey:     RoutingKeyTweetDeleted + ".dlq",
			ingressRoutingKey: RoutingKeyTimelineTweetDeleted,
		}, true
	default:
		return timelineEventReplayRoute{}, false
	}
}

type timelineEventReplayBroker interface {
	timelineRecoveryTopologyBroker
	EnablePublisherConfirms() error
	GetMessage(queueName string, autoAck bool) (amqp.Delivery, bool, error)
	PublishMessageConfirmed(ctx context.Context, exchange, routingKey string, message amqp.Publishing) error
}

type TimelineEventReplayOptions struct {
	Kind           TimelineEventKind
	Limit          int
	Execute        bool
	MaxReplayCount int
	Operator       string
	Reason         string
}

func (o *TimelineEventReplayOptions) Validate() error {
	if o == nil {
		return errors.New("timeline event replay options are required")
	}
	o.Operator = strings.TrimSpace(o.Operator)
	o.Reason = strings.TrimSpace(o.Reason)
	if _, supported := timelineEventReplayRouteForKind(o.Kind); !supported {
		return errors.New("event must be created or deleted")
	}
	if o.Limit <= 0 || o.Limit > timelineEventReplayLimitMax {
		return fmt.Errorf("limit must be between 1 and %d", timelineEventReplayLimitMax)
	}
	if o.MaxReplayCount <= 0 || o.MaxReplayCount > timelineEventReplayCountMax {
		return fmt.Errorf("max replay count must be between 1 and %d", timelineEventReplayCountMax)
	}
	if len(o.Operator) > timelineEventReplayOperatorLimit {
		return fmt.Errorf("operator exceeds %d bytes", timelineEventReplayOperatorLimit)
	}
	if len(o.Reason) > timelineEventReplayReasonLimit {
		return fmt.Errorf("reason exceeds %d bytes", timelineEventReplayReasonLimit)
	}
	if o.Execute && (o.Operator == "" || o.Reason == "") {
		return errors.New("operator and reason are required when execute is enabled")
	}
	return nil
}

// TimelineEventReplayEntry excludes tweet IDs, author IDs and event bodies.
type TimelineEventReplayEntry struct {
	MessageSHA256       string `json:"message_sha256"`
	EventIdentitySHA256 string `json:"event_identity_sha256,omitempty"`
	ReplayCount         int    `json:"replay_count"`
	Outcome             string `json:"outcome"`
	ErrorCode           string `json:"error_code,omitempty"`
}

type TimelineEventReplayReport struct {
	GeneratedAt    time.Time                  `json:"generated_at"`
	Mode           string                     `json:"mode"`
	Event          TimelineEventKind          `json:"event"`
	Queue          string                     `json:"queue"`
	OperatorSHA256 string                     `json:"operator_sha256,omitempty"`
	ReasonSHA256   string                     `json:"reason_sha256,omitempty"`
	Limit          int                        `json:"limit"`
	Inspected      int                        `json:"inspected"`
	Eligible       int                        `json:"eligible"`
	Replayed       int                        `json:"replayed"`
	Retained       int                        `json:"retained"`
	Uncertain      int                        `json:"uncertain"`
	Empty          bool                       `json:"empty"`
	Entries        []TimelineEventReplayEntry `json:"entries"`
}

type TimelineEventDLQReplayer struct {
	broker timelineEventReplayBroker
	now    func() time.Time
}

func NewTimelineEventDLQReplayer(broker timelineEventReplayBroker) (*TimelineEventDLQReplayer, error) {
	if broker == nil {
		return nil, errors.New("timeline event replay broker is required")
	}
	return &TimelineEventDLQReplayer{broker: broker, now: time.Now}, nil
}

func (r *TimelineEventDLQReplayer) Run(
	ctx context.Context,
	options TimelineEventReplayOptions,
) (TimelineEventReplayReport, error) {
	if err := options.Validate(); err != nil {
		return TimelineEventReplayReport{}, err
	}
	route, _ := timelineEventReplayRouteForKind(options.Kind)
	mode := "inspect"
	if options.Execute {
		mode = "execute"
	}
	report := TimelineEventReplayReport{
		GeneratedAt: r.now().UTC(),
		Mode:        mode,
		Event:       route.kind,
		Queue:       route.queue,
		Limit:       options.Limit,
		Entries:     make([]TimelineEventReplayEntry, 0, options.Limit),
	}
	if options.Operator != "" {
		report.OperatorSHA256 = timelineEventReplayDigest([]byte(options.Operator))
	}
	if options.Reason != "" {
		report.ReasonSHA256 = timelineEventReplayDigest([]byte(options.Reason))
	}
	if options.Execute {
		if err := DeclareTimelineRecoveryTopology(r.broker); err != nil {
			return report, fmt.Errorf("declare timeline replay topology: %w", err)
		}
		if err := r.broker.EnablePublisherConfirms(); err != nil {
			return report, fmt.Errorf("enable timeline event replay publisher confirms: %w", err)
		}
	}

	deliveries, err := r.takeBatch(ctx, route.queue, options.Limit)
	if err != nil {
		return report, err
	}
	report.Empty = len(deliveries) == 0
	if len(deliveries) == 0 {
		return report, nil
	}
	if !options.Execute {
		return r.inspect(deliveries, route, options, report)
	}
	return r.replay(ctx, deliveries, route, options, report)
}

func (r *TimelineEventDLQReplayer) takeBatch(
	ctx context.Context,
	queue string,
	limit int,
) ([]amqp.Delivery, error) {
	deliveries := make([]amqp.Delivery, 0, limit)
	for len(deliveries) < limit {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, requeueTimelineEventDeliveries(deliveries))
		}
		delivery, ok, err := r.broker.GetMessage(queue, false)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("get timeline %s dlq message: %w", queue, err),
				requeueTimelineEventDeliveries(deliveries),
			)
		}
		if !ok {
			break
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func (r *TimelineEventDLQReplayer) inspect(
	deliveries []amqp.Delivery,
	route timelineEventReplayRoute,
	options TimelineEventReplayOptions,
	report TimelineEventReplayReport,
) (TimelineEventReplayReport, error) {
	for _, delivery := range deliveries {
		entry, eligible := inspectTimelineEventReplayDelivery(delivery, route, options.MaxReplayCount)
		report.Inspected++
		if eligible {
			report.Eligible++
		} else {
			report.Retained++
		}
		report.Entries = append(report.Entries, entry)
	}
	if err := requeueTimelineEventDeliveries(deliveries); err != nil {
		return report, fmt.Errorf("retain inspected timeline event messages: %w", err)
	}
	return report, nil
}

func (r *TimelineEventDLQReplayer) replay(
	ctx context.Context,
	deliveries []amqp.Delivery,
	route timelineEventReplayRoute,
	options TimelineEventReplayOptions,
	report TimelineEventReplayReport,
) (TimelineEventReplayReport, error) {
	for index, delivery := range deliveries {
		if err := ctx.Err(); err != nil {
			return report, errors.Join(err, requeueTimelineEventDeliveries(deliveries[index:]))
		}
		entry, eligible := inspectTimelineEventReplayDelivery(delivery, route, options.MaxReplayCount)
		report.Inspected++
		if !eligible {
			report.Retained++
			report.Entries = append(report.Entries, entry)
			if err := delivery.Nack(false, true); err != nil {
				return report, errors.Join(
					fmt.Errorf("retain ineligible timeline event message: %w", err),
					requeueTimelineEventDeliveries(deliveries[index+1:]),
				)
			}
			continue
		}

		report.Eligible++
		message := timelineEventReplayMessage(delivery, entry.ReplayCount+1, r.now())
		if err := r.broker.PublishMessageConfirmed(
			ctx,
			ExchangeTimelineIngress,
			route.ingressRoutingKey,
			message,
		); err != nil {
			entry.Outcome = timelineEventReplayOutcomeRetainedPublish
			entry.ErrorCode = timelineEventReplayErrorPublishFailed
			report.Retained++
			report.Entries = append(report.Entries, entry)
			return report, errors.Join(
				fmt.Errorf("publish timeline %s replay: %w", route.kind, err),
				requeueTimelineEventDeliveries(deliveries[index:]),
			)
		}
		if err := delivery.Ack(false); err != nil {
			entry.Outcome = timelineEventReplayOutcomeAckUncertain
			entry.ErrorCode = timelineEventReplayErrorAckFailed
			report.Uncertain++
			report.Entries = append(report.Entries, entry)
			return report, errors.Join(
				fmt.Errorf("ack replayed timeline %s message: %w", route.kind, err),
				requeueTimelineEventDeliveries(deliveries[index+1:]),
			)
		}
		entry.Outcome = timelineEventReplayOutcomeReplayed
		entry.ReplayCount++
		report.Replayed++
		report.Entries = append(report.Entries, entry)
	}
	return report, nil
}

func inspectTimelineEventReplayDelivery(
	delivery amqp.Delivery,
	route timelineEventReplayRoute,
	maxReplayCount int,
) (TimelineEventReplayEntry, bool) {
	entry := TimelineEventReplayEntry{
		MessageSHA256: timelineEventReplayDigest(delivery.Body),
		Outcome:       timelineEventReplayOutcomeEligible,
	}
	if delivery.RoutingKey != route.dlqRoutingKey {
		entry.Outcome = timelineEventReplayOutcomeRetainedInvalid
		entry.ErrorCode = timelineEventReplayErrorUnsupportedRoute
		return entry, false
	}

	replayCount, validCount := timelineEventReplayHeaderCount(delivery.Headers)
	entry.ReplayCount = replayCount
	if !validCount {
		entry.Outcome = timelineEventReplayOutcomeRetainedInvalid
		entry.ErrorCode = timelineEventReplayErrorInvalidCount
		return entry, false
	}
	if len(delivery.Body) == 0 || len(delivery.Body) > timelineEventReplayBodyLimit {
		entry.Outcome = timelineEventReplayOutcomeRetainedInvalid
		entry.ErrorCode = timelineEventReplayErrorOversizedEvent
		return entry, false
	}

	var tweetID, authorID uint64
	switch route.kind {
	case TimelineEventCreated:
		event, err := decodeTimelineTweetCreatedEvent(delivery.Body)
		if err != nil {
			entry.Outcome = timelineEventReplayOutcomeRetainedInvalid
			entry.ErrorCode = timelineEventReplayErrorMalformedEvent
			return entry, false
		}
		tweetID, authorID = event.TweetID, event.AuthorID
	case TimelineEventDeleted:
		event, err := decodeTimelineTweetDeletedEvent(delivery.Body)
		if err != nil {
			entry.Outcome = timelineEventReplayOutcomeRetainedInvalid
			entry.ErrorCode = timelineEventReplayErrorMalformedEvent
			return entry, false
		}
		tweetID, authorID = event.TweetID, event.AuthorID
	default:
		entry.Outcome = timelineEventReplayOutcomeRetainedInvalid
		entry.ErrorCode = timelineEventReplayErrorUnsupportedRoute
		return entry, false
	}
	entry.EventIdentitySHA256 = timelineEventReplayDigest([]byte(
		fmt.Sprintf("timeline:%s:%d:%d", route.kind, authorID, tweetID),
	))
	if entry.ReplayCount >= maxReplayCount {
		entry.Outcome = timelineEventReplayOutcomeRetainedLimit
		entry.ErrorCode = timelineEventReplayErrorReplayLimit
		return entry, false
	}
	return entry, true
}

func timelineEventReplayHeaderCount(headers amqp.Table) (int, bool) {
	if headers == nil {
		return 0, true
	}
	value, exists := headers[timelineEventReplayHeader]
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
	if count < 0 || count > timelineEventReplayCountMax {
		return 0, false
	}
	return int(count), true
}

func timelineEventReplayMessage(delivery amqp.Delivery, replayCount int, replayedAt time.Time) amqp.Publishing {
	headers := make(amqp.Table, len(delivery.Headers)+2)
	for key, value := range delivery.Headers {
		headers[key] = value
	}
	clearTimelineReplayTransportHeaders(headers)
	headers[timelineEventReplayHeader] = int32(replayCount)
	headers[timelineEventReplayedAtHeader] = replayedAt.UTC().UnixMilli()
	return amqp.Publishing{
		Headers:         headers,
		ContentType:     delivery.ContentType,
		ContentEncoding: delivery.ContentEncoding,
		DeliveryMode:    amqp.Persistent,
		Priority:        delivery.Priority,
		CorrelationId:   delivery.CorrelationId,
		ReplyTo:         delivery.ReplyTo,
		MessageId:       delivery.MessageId,
		Timestamp:       delivery.Timestamp,
		Type:            delivery.Type,
		Body:            append([]byte(nil), delivery.Body...),
	}
}

func clearTimelineReplayTransportHeaders(headers amqp.Table) {
	for _, header := range []string{
		timelineRetryHeader,
		"x-death",
		"x-first-death-exchange",
		"x-first-death-queue",
		"x-first-death-reason",
		"x-last-death-exchange",
		"x-last-death-queue",
		"x-last-death-reason",
	} {
		delete(headers, header)
	}
}

func requeueTimelineEventDeliveries(deliveries []amqp.Delivery) error {
	var joined error
	for _, delivery := range deliveries {
		if err := delivery.Nack(false, true); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func timelineEventReplayDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
