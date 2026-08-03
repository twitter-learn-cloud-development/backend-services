package consumer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"twitter-clone/internal/events"
)

const (
	moderationReplayHeader     = "x-timeline-moderation-replay-count"
	moderationReplayedAtHeader = "x-timeline-moderation-replayed-at-unix-ms"

	moderationReplayLimitMax      = 100
	moderationReplayCountMax      = 10
	moderationReplayOperatorLimit = 128
	moderationReplayReasonLimit   = 512
	moderationReplayBodyLimit     = 1 << 20

	moderationReplayOutcomeEligible        = "eligible"
	moderationReplayOutcomeReplayed        = "replayed"
	moderationReplayOutcomeRetainedInvalid = "retained_invalid"
	moderationReplayOutcomeRetainedLimit   = "retained_replay_limit"
	moderationReplayOutcomeRetainedPublish = "retained_publish_failed"
	moderationReplayOutcomeAckUncertain    = "acknowledgement_uncertain"

	moderationReplayErrorUnsupportedRoute = "unsupported_routing_key"
	moderationReplayErrorMalformedEvent   = "malformed_event"
	moderationReplayErrorOversizedEvent   = "oversized_event"
	moderationReplayErrorInvalidCount     = "invalid_replay_count"
	moderationReplayErrorReplayLimit      = "replay_limit_reached"
	moderationReplayErrorPublishFailed    = "publish_failed"
	moderationReplayErrorAckFailed        = "ack_failed"
)

type moderationReplayBroker interface {
	timelineRecoveryTopologyBroker
	EnablePublisherConfirms() error
	GetMessage(queueName string, autoAck bool) (amqp.Delivery, bool, error)
	PublishMessageConfirmed(ctx context.Context, exchange, routingKey string, message amqp.Publishing) error
}

// ModerationReplayOptions bounds a single operator-initiated inspection or replay.
type ModerationReplayOptions struct {
	Limit          int
	Execute        bool
	MaxReplayCount int
	Operator       string
	Reason         string
}

func (o *ModerationReplayOptions) Validate() error {
	if o == nil {
		return errors.New("moderation replay options are required")
	}
	o.Operator = strings.TrimSpace(o.Operator)
	o.Reason = strings.TrimSpace(o.Reason)
	if o.Limit <= 0 || o.Limit > moderationReplayLimitMax {
		return fmt.Errorf("limit must be between 1 and %d", moderationReplayLimitMax)
	}
	if o.MaxReplayCount <= 0 || o.MaxReplayCount > moderationReplayCountMax {
		return fmt.Errorf("max replay count must be between 1 and %d", moderationReplayCountMax)
	}
	if len(o.Operator) > moderationReplayOperatorLimit {
		return fmt.Errorf("operator exceeds %d bytes", moderationReplayOperatorLimit)
	}
	if len(o.Reason) > moderationReplayReasonLimit {
		return fmt.Errorf("reason exceeds %d bytes", moderationReplayReasonLimit)
	}
	if o.Execute && (o.Operator == "" || o.Reason == "") {
		return errors.New("operator and reason are required when execute is enabled")
	}
	return nil
}

// ModerationReplayEntry deliberately excludes tweet IDs, author IDs and event bodies.
type ModerationReplayEntry struct {
	MessageSHA256  string `json:"message_sha256"`
	EventKeySHA256 string `json:"event_key_sha256,omitempty"`
	ReplayCount    int    `json:"replay_count"`
	Outcome        string `json:"outcome"`
	ErrorCode      string `json:"error_code,omitempty"`
}

// ModerationReplayReport is safe to retain as an operational audit artifact.
type ModerationReplayReport struct {
	GeneratedAt    time.Time               `json:"generated_at"`
	Mode           string                  `json:"mode"`
	Queue          string                  `json:"queue"`
	OperatorSHA256 string                  `json:"operator_sha256,omitempty"`
	ReasonSHA256   string                  `json:"reason_sha256,omitempty"`
	Limit          int                     `json:"limit"`
	Inspected      int                     `json:"inspected"`
	Eligible       int                     `json:"eligible"`
	Replayed       int                     `json:"replayed"`
	Retained       int                     `json:"retained"`
	Uncertain      int                     `json:"uncertain"`
	Empty          bool                    `json:"empty"`
	Entries        []ModerationReplayEntry `json:"entries"`
}

type ModerationDLQReplayer struct {
	broker moderationReplayBroker
	now    func() time.Time
}

func NewModerationDLQReplayer(broker moderationReplayBroker) (*ModerationDLQReplayer, error) {
	if broker == nil {
		return nil, errors.New("moderation replay broker is required")
	}
	return &ModerationDLQReplayer{broker: broker, now: time.Now}, nil
}

func (r *ModerationDLQReplayer) Run(
	ctx context.Context,
	options ModerationReplayOptions,
) (ModerationReplayReport, error) {
	if err := options.Validate(); err != nil {
		return ModerationReplayReport{}, err
	}
	mode := "inspect"
	if options.Execute {
		mode = "execute"
	}
	report := ModerationReplayReport{
		GeneratedAt: r.now().UTC(),
		Mode:        mode,
		Queue:       QueueTweetModerationCleanupDLQ,
		Limit:       options.Limit,
		Entries:     make([]ModerationReplayEntry, 0, options.Limit),
	}
	if options.Operator != "" {
		report.OperatorSHA256 = moderationReplayDigest([]byte(options.Operator))
	}
	if options.Reason != "" {
		report.ReasonSHA256 = moderationReplayDigest([]byte(options.Reason))
	}
	if options.Execute {
		if err := DeclareTimelineRecoveryTopology(r.broker); err != nil {
			return report, fmt.Errorf("declare moderation replay topology: %w", err)
		}
		if err := r.broker.EnablePublisherConfirms(); err != nil {
			return report, fmt.Errorf("enable moderation replay publisher confirms: %w", err)
		}
	}

	deliveries, err := r.takeBatch(ctx, options.Limit)
	if err != nil {
		return report, err
	}
	report.Empty = len(deliveries) == 0
	if len(deliveries) == 0 {
		return report, nil
	}
	if !options.Execute {
		return r.inspect(deliveries, options, report)
	}
	return r.replay(ctx, deliveries, options, report)
}

func (r *ModerationDLQReplayer) takeBatch(ctx context.Context, limit int) ([]amqp.Delivery, error) {
	deliveries := make([]amqp.Delivery, 0, limit)
	for len(deliveries) < limit {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, requeueModerationDeliveries(deliveries))
		}
		delivery, ok, err := r.broker.GetMessage(QueueTweetModerationCleanupDLQ, false)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("get moderation cleanup dlq message: %w", err),
				requeueModerationDeliveries(deliveries),
			)
		}
		if !ok {
			break
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func (r *ModerationDLQReplayer) inspect(
	deliveries []amqp.Delivery,
	options ModerationReplayOptions,
	report ModerationReplayReport,
) (ModerationReplayReport, error) {
	for _, delivery := range deliveries {
		entry, eligible := inspectModerationReplayDelivery(delivery, options.MaxReplayCount)
		report.Inspected++
		if eligible {
			report.Eligible++
		} else {
			report.Retained++
		}
		report.Entries = append(report.Entries, entry)
	}
	if err := requeueModerationDeliveries(deliveries); err != nil {
		return report, fmt.Errorf("retain inspected moderation messages: %w", err)
	}
	return report, nil
}

func (r *ModerationDLQReplayer) replay(
	ctx context.Context,
	deliveries []amqp.Delivery,
	options ModerationReplayOptions,
	report ModerationReplayReport,
) (ModerationReplayReport, error) {
	for index, delivery := range deliveries {
		if err := ctx.Err(); err != nil {
			return report, errors.Join(err, requeueModerationDeliveries(deliveries[index:]))
		}
		entry, eligible := inspectModerationReplayDelivery(delivery, options.MaxReplayCount)
		report.Inspected++
		if !eligible {
			report.Retained++
			report.Entries = append(report.Entries, entry)
			if err := delivery.Nack(false, true); err != nil {
				return report, errors.Join(
					fmt.Errorf("retain ineligible moderation message: %w", err),
					requeueModerationDeliveries(deliveries[index+1:]),
				)
			}
			continue
		}

		report.Eligible++
		message := moderationReplayMessage(delivery, entry.ReplayCount+1, r.now())
		if err := r.broker.PublishMessageConfirmed(
			ctx,
			ExchangeTimelineIngress,
			RoutingKeyTimelineTweetModerated,
			message,
		); err != nil {
			entry.Outcome = moderationReplayOutcomeRetainedPublish
			entry.ErrorCode = moderationReplayErrorPublishFailed
			report.Retained++
			report.Entries = append(report.Entries, entry)
			return report, errors.Join(
				fmt.Errorf("publish moderation replay: %w", err),
				requeueModerationDeliveries(deliveries[index:]),
			)
		}
		if err := delivery.Ack(false); err != nil {
			entry.Outcome = moderationReplayOutcomeAckUncertain
			entry.ErrorCode = moderationReplayErrorAckFailed
			report.Uncertain++
			report.Entries = append(report.Entries, entry)
			return report, errors.Join(
				fmt.Errorf("ack replayed moderation message: %w", err),
				requeueModerationDeliveries(deliveries[index+1:]),
			)
		}
		entry.Outcome = moderationReplayOutcomeReplayed
		entry.ReplayCount++
		report.Replayed++
		report.Entries = append(report.Entries, entry)
	}
	return report, nil
}

func inspectModerationReplayDelivery(delivery amqp.Delivery, maxReplayCount int) (ModerationReplayEntry, bool) {
	entry := ModerationReplayEntry{
		MessageSHA256: moderationReplayDigest(delivery.Body),
		Outcome:       moderationReplayOutcomeEligible,
	}
	if delivery.RoutingKey != RoutingKeyTweetModerated+".dlq" {
		entry.Outcome = moderationReplayOutcomeRetainedInvalid
		entry.ErrorCode = moderationReplayErrorUnsupportedRoute
		return entry, false
	}

	replayCount, validCount := moderationReplayHeaderCount(delivery.Headers)
	entry.ReplayCount = replayCount
	if !validCount {
		entry.Outcome = moderationReplayOutcomeRetainedInvalid
		entry.ErrorCode = moderationReplayErrorInvalidCount
		return entry, false
	}
	if len(delivery.Body) == 0 || len(delivery.Body) > moderationReplayBodyLimit {
		entry.Outcome = moderationReplayOutcomeRetainedInvalid
		entry.ErrorCode = moderationReplayErrorOversizedEvent
		return entry, false
	}

	var event events.TweetModeratedEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil || event.Validate() != nil {
		entry.Outcome = moderationReplayOutcomeRetainedInvalid
		entry.ErrorCode = moderationReplayErrorMalformedEvent
		return entry, false
	}
	entry.EventKeySHA256 = moderationReplayDigest([]byte(event.EventKey))
	if entry.ReplayCount >= maxReplayCount {
		entry.Outcome = moderationReplayOutcomeRetainedLimit
		entry.ErrorCode = moderationReplayErrorReplayLimit
		return entry, false
	}
	return entry, true
}

func moderationReplayHeaderCount(headers amqp.Table) (int, bool) {
	if headers == nil {
		return 0, true
	}
	value, exists := headers[moderationReplayHeader]
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
	if count < 0 || count > moderationReplayCountMax {
		return 0, false
	}
	return int(count), true
}

func moderationReplayMessage(delivery amqp.Delivery, replayCount int, replayedAt time.Time) amqp.Publishing {
	headers := make(amqp.Table, len(delivery.Headers)+2)
	for key, value := range delivery.Headers {
		headers[key] = value
	}
	clearTimelineReplayTransportHeaders(headers)
	headers[moderationReplayHeader] = int32(replayCount)
	headers[moderationReplayedAtHeader] = replayedAt.UTC().UnixMilli()
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

func requeueModerationDeliveries(deliveries []amqp.Delivery) error {
	var joined error
	for _, delivery := range deliveries {
		if err := delivery.Nack(false, true); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func moderationReplayDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
