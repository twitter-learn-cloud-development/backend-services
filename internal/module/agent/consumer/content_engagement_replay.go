package consumer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	contentEngagementReplayHeader     = "x-agent-profile-replay-count"
	contentEngagementReplayedAtHeader = "x-agent-profile-replayed-at-unix-ms"

	contentEngagementReplayLimitMax      = 100
	contentEngagementReplayCountMax      = 10
	contentEngagementReplayOperatorLimit = 128
	contentEngagementReplayReasonLimit   = 512
	contentEngagementReplayBodyLimit     = 1 << 20

	replayOutcomeEligible          = "eligible"
	replayOutcomeReplayed          = "replayed"
	replayOutcomeRetainedInvalid   = "retained_invalid"
	replayOutcomeRetainedLimit     = "retained_replay_limit"
	replayOutcomeRetainedPublish   = "retained_publish_failed"
	replayOutcomeAcknowledgement   = "acknowledgement_uncertain"
	replayErrorUnsupportedRoute    = "unsupported_routing_key"
	replayErrorMalformedEvent      = "malformed_event"
	replayErrorOversizedEvent      = "oversized_event"
	replayErrorInvalidReplayCount  = "invalid_replay_count"
	replayErrorReplayLimit         = "replay_limit_reached"
	replayErrorPublishFailed       = "publish_failed"
	replayErrorAcknowledgementFail = "ack_failed"
)

type contentEngagementReplayBroker interface {
	EnablePublisherConfirms() error
	GetMessage(queueName string, autoAck bool) (amqp.Delivery, bool, error)
	PublishMessageConfirmed(ctx context.Context, exchange, routingKey string, message amqp.Publishing) error
}

type ContentEngagementReplayOptions struct {
	Limit          int
	Execute        bool
	MaxReplayCount int
	Operator       string
	Reason         string
}

func (o *ContentEngagementReplayOptions) Validate() error {
	if o == nil {
		return errors.New("content engagement replay options are required")
	}
	o.Operator = strings.TrimSpace(o.Operator)
	o.Reason = strings.TrimSpace(o.Reason)
	if o.Limit <= 0 || o.Limit > contentEngagementReplayLimitMax {
		return fmt.Errorf("limit must be between 1 and %d", contentEngagementReplayLimitMax)
	}
	if o.MaxReplayCount <= 0 || o.MaxReplayCount > contentEngagementReplayCountMax {
		return fmt.Errorf("max replay count must be between 1 and %d", contentEngagementReplayCountMax)
	}
	if len(o.Operator) > contentEngagementReplayOperatorLimit {
		return fmt.Errorf("operator exceeds %d bytes", contentEngagementReplayOperatorLimit)
	}
	if len(o.Reason) > contentEngagementReplayReasonLimit {
		return fmt.Errorf("reason exceeds %d bytes", contentEngagementReplayReasonLimit)
	}
	if o.Execute && (o.Operator == "" || o.Reason == "") {
		return errors.New("operator and reason are required when execute is enabled")
	}
	return nil
}

type ContentEngagementReplayEntry struct {
	MessageSHA256      string `json:"message_sha256"`
	Kind               string `json:"kind"`
	OriginalRoutingKey string `json:"original_routing_key,omitempty"`
	ReplayCount        int    `json:"replay_count"`
	Outcome            string `json:"outcome"`
	ErrorCode          string `json:"error_code,omitempty"`
}

type ContentEngagementReplayReport struct {
	GeneratedAt  time.Time                      `json:"generated_at"`
	Mode         string                         `json:"mode"`
	Queue        string                         `json:"queue"`
	Operator     string                         `json:"operator,omitempty"`
	ReasonSHA256 string                         `json:"reason_sha256,omitempty"`
	Limit        int                            `json:"limit"`
	Inspected    int                            `json:"inspected"`
	Eligible     int                            `json:"eligible"`
	Replayed     int                            `json:"replayed"`
	Retained     int                            `json:"retained"`
	Uncertain    int                            `json:"uncertain"`
	Empty        bool                           `json:"empty"`
	Entries      []ContentEngagementReplayEntry `json:"entries"`
}

type ContentEngagementDLQReplayer struct {
	broker contentEngagementReplayBroker
	now    func() time.Time
}

func NewContentEngagementDLQReplayer(broker contentEngagementReplayBroker) (*ContentEngagementDLQReplayer, error) {
	if broker == nil {
		return nil, errors.New("content engagement replay broker is required")
	}
	if err := broker.EnablePublisherConfirms(); err != nil {
		return nil, fmt.Errorf("enable content engagement replay publisher confirms: %w", err)
	}
	return &ContentEngagementDLQReplayer{
		broker: broker,
		now:    time.Now,
	}, nil
}

func (r *ContentEngagementDLQReplayer) Run(
	ctx context.Context,
	options ContentEngagementReplayOptions,
) (ContentEngagementReplayReport, error) {
	if err := options.Validate(); err != nil {
		return ContentEngagementReplayReport{}, err
	}
	mode := "inspect"
	if options.Execute {
		mode = "execute"
	}
	report := ContentEngagementReplayReport{
		GeneratedAt: r.now().UTC(),
		Mode:        mode,
		Queue:       contentEngagementDLQ,
		Operator:    options.Operator,
		Limit:       options.Limit,
		Entries:     make([]ContentEngagementReplayEntry, 0, options.Limit),
	}
	if options.Reason != "" {
		report.ReasonSHA256 = replayDigest([]byte(options.Reason))
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

func (r *ContentEngagementDLQReplayer) takeBatch(ctx context.Context, limit int) ([]amqp.Delivery, error) {
	deliveries := make([]amqp.Delivery, 0, limit)
	for len(deliveries) < limit {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, requeueContentEngagementDeliveries(deliveries))
		}
		delivery, ok, err := r.broker.GetMessage(contentEngagementDLQ, false)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("get content engagement dlq message: %w", err), requeueContentEngagementDeliveries(deliveries))
		}
		if !ok {
			break
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func (r *ContentEngagementDLQReplayer) inspect(
	deliveries []amqp.Delivery,
	options ContentEngagementReplayOptions,
	report ContentEngagementReplayReport,
) (ContentEngagementReplayReport, error) {
	for _, delivery := range deliveries {
		entry, eligible := inspectContentEngagementReplayDelivery(delivery, options.MaxReplayCount)
		report.Inspected++
		if eligible {
			report.Eligible++
		} else {
			report.Retained++
		}
		report.Entries = append(report.Entries, entry)
	}
	if err := requeueContentEngagementDeliveries(deliveries); err != nil {
		return report, fmt.Errorf("retain inspected content engagement messages: %w", err)
	}
	return report, nil
}

func (r *ContentEngagementDLQReplayer) replay(
	ctx context.Context,
	deliveries []amqp.Delivery,
	options ContentEngagementReplayOptions,
	report ContentEngagementReplayReport,
) (ContentEngagementReplayReport, error) {
	for index, delivery := range deliveries {
		if err := ctx.Err(); err != nil {
			return report, errors.Join(err, requeueContentEngagementDeliveries(deliveries[index:]))
		}
		entry, eligible := inspectContentEngagementReplayDelivery(delivery, options.MaxReplayCount)
		report.Inspected++
		if !eligible {
			report.Retained++
			report.Entries = append(report.Entries, entry)
			if err := delivery.Nack(false, true); err != nil {
				return report, errors.Join(
					fmt.Errorf("retain ineligible content engagement message: %w", err),
					requeueContentEngagementDeliveries(deliveries[index+1:]),
				)
			}
			continue
		}
		report.Eligible++
		message := contentEngagementReplayMessage(delivery, entry.ReplayCount+1, r.now())
		if err := r.broker.PublishMessageConfirmed(ctx, contentEngagementExchange, entry.OriginalRoutingKey, message); err != nil {
			entry.Outcome = replayOutcomeRetainedPublish
			entry.ErrorCode = replayErrorPublishFailed
			report.Retained++
			report.Entries = append(report.Entries, entry)
			return report, errors.Join(
				fmt.Errorf("publish content engagement replay: %w", err),
				requeueContentEngagementDeliveries(deliveries[index:]),
			)
		}
		if err := delivery.Ack(false); err != nil {
			entry.Outcome = replayOutcomeAcknowledgement
			entry.ErrorCode = replayErrorAcknowledgementFail
			report.Uncertain++
			report.Entries = append(report.Entries, entry)
			return report, errors.Join(
				fmt.Errorf("ack replayed content engagement message: %w", err),
				requeueContentEngagementDeliveries(deliveries[index+1:]),
			)
		}
		entry.Outcome = replayOutcomeReplayed
		entry.ReplayCount++
		report.Replayed++
		report.Entries = append(report.Entries, entry)
	}
	return report, nil
}

func inspectContentEngagementReplayDelivery(
	delivery amqp.Delivery,
	maxReplayCount int,
) (ContentEngagementReplayEntry, bool) {
	replayCount, validReplayCount := contentEngagementHeaderCount(
		delivery.Headers,
		contentEngagementReplayHeader,
		contentEngagementReplayCountMax,
	)
	entry := ContentEngagementReplayEntry{
		MessageSHA256: replayDigest(delivery.Body),
		Kind:          "unknown",
		ReplayCount:   replayCount,
		Outcome:       replayOutcomeEligible,
	}
	if !validReplayCount {
		entry.Outcome = replayOutcomeRetainedInvalid
		entry.ErrorCode = replayErrorInvalidReplayCount
		return entry, false
	}
	originalRoutingKey, ok := contentEngagementOriginalRoutingKey(delivery.RoutingKey)
	if !ok {
		entry.Outcome = replayOutcomeRetainedInvalid
		entry.ErrorCode = replayErrorUnsupportedRoute
		return entry, false
	}
	entry.OriginalRoutingKey = originalRoutingKey
	entry.Kind = contentEngagementKindForRoutingKey(originalRoutingKey)
	if len(delivery.Body) == 0 || len(delivery.Body) > contentEngagementReplayBodyLimit {
		entry.Outcome = replayOutcomeRetainedInvalid
		entry.ErrorCode = replayErrorOversizedEvent
		return entry, false
	}
	candidate := delivery
	candidate.RoutingKey = originalRoutingKey
	event, err := decodeContentEngagement(candidate)
	if err != nil || event.Validate() != nil {
		entry.Outcome = replayOutcomeRetainedInvalid
		entry.ErrorCode = replayErrorMalformedEvent
		return entry, false
	}
	if entry.ReplayCount >= maxReplayCount {
		entry.Outcome = replayOutcomeRetainedLimit
		entry.ErrorCode = replayErrorReplayLimit
		return entry, false
	}
	return entry, true
}

func contentEngagementOriginalRoutingKey(routingKey string) (string, bool) {
	switch routingKey {
	case dlqRoutingKey(routingTweetLiked):
		return routingTweetLiked, true
	case dlqRoutingKey(routingCommentCreate):
		return routingCommentCreate, true
	default:
		return "", false
	}
}

func contentEngagementReplayMessage(delivery amqp.Delivery, replayCount int, replayedAt time.Time) amqp.Publishing {
	message := copyContentEngagementMessage(delivery)
	delete(message.Headers, "x-agent-profile-retry-count")
	message.Headers[contentEngagementReplayHeader] = int32(replayCount)
	message.Headers[contentEngagementReplayedAtHeader] = replayedAt.UTC().UnixMilli()
	message.Expiration = ""
	return message
}

func requeueContentEngagementDeliveries(deliveries []amqp.Delivery) error {
	var joined error
	for _, delivery := range deliveries {
		if err := delivery.Nack(false, true); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func replayDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
