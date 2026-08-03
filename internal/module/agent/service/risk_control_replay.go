package service

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
	riskControlReplayHeader     = "x-agent-risk-replay-count"
	riskControlReplayedAtHeader = "x-agent-risk-replayed-at-unix-ms"

	riskControlReplayLimitMax      = 100
	riskControlReplayCountMax      = 10
	riskControlReplayOperatorLimit = 128
	riskControlReplayReasonLimit   = 512
	riskControlReplayBodyLimit     = 1 << 20

	riskControlReplayOutcomeEligible        = "eligible"
	riskControlReplayOutcomeReplayed        = "replayed"
	riskControlReplayOutcomeRetainedInvalid = "retained_invalid"
	riskControlReplayOutcomeRetainedLimit   = "retained_replay_limit"
	riskControlReplayOutcomeRetainedPublish = "retained_publish_failed"
	riskControlReplayOutcomeAckUncertain    = "acknowledgement_uncertain"

	riskControlReplayErrorUnsupportedRoute = "unsupported_routing_key"
	riskControlReplayErrorMalformedEvent   = "malformed_event"
	riskControlReplayErrorOversizedEvent   = "oversized_event"
	riskControlReplayErrorInvalidCount     = "invalid_replay_count"
	riskControlReplayErrorReplayLimit      = "replay_limit_reached"
	riskControlReplayErrorPublishFailed    = "publish_failed"
	riskControlReplayErrorAckFailed        = "ack_failed"
)

type riskControlReplayBroker interface {
	EnablePublisherConfirms() error
	GetMessage(queueName string, autoAck bool) (amqp.Delivery, bool, error)
	PublishMessageConfirmed(ctx context.Context, exchange, routingKey string, message amqp.Publishing) error
}

// RiskControlReplayOptions bounds a single operator-initiated inspection or replay.
type RiskControlReplayOptions struct {
	Limit          int
	Execute        bool
	MaxReplayCount int
	Operator       string
	Reason         string
}

func (o *RiskControlReplayOptions) Validate() error {
	if o == nil {
		return errors.New("risk control replay options are required")
	}
	o.Operator = strings.TrimSpace(o.Operator)
	o.Reason = strings.TrimSpace(o.Reason)
	if o.Limit <= 0 || o.Limit > riskControlReplayLimitMax {
		return fmt.Errorf("limit must be between 1 and %d", riskControlReplayLimitMax)
	}
	if o.MaxReplayCount <= 0 || o.MaxReplayCount > riskControlReplayCountMax {
		return fmt.Errorf("max replay count must be between 1 and %d", riskControlReplayCountMax)
	}
	if len(o.Operator) > riskControlReplayOperatorLimit {
		return fmt.Errorf("operator exceeds %d bytes", riskControlReplayOperatorLimit)
	}
	if len(o.Reason) > riskControlReplayReasonLimit {
		return fmt.Errorf("reason exceeds %d bytes", riskControlReplayReasonLimit)
	}
	if o.Execute && (o.Operator == "" || o.Reason == "") {
		return errors.New("operator and reason are required when execute is enabled")
	}
	return nil
}

// RiskControlReplayEntry excludes tweet IDs, author IDs and event bodies.
type RiskControlReplayEntry struct {
	MessageSHA256          string `json:"message_sha256"`
	WorkflowIdentitySHA256 string `json:"workflow_identity_sha256,omitempty"`
	ReplayCount            int    `json:"replay_count"`
	Outcome                string `json:"outcome"`
	ErrorCode              string `json:"error_code,omitempty"`
}

// RiskControlReplayReport is safe to retain as an operational audit artifact.
type RiskControlReplayReport struct {
	GeneratedAt    time.Time                `json:"generated_at"`
	Mode           string                   `json:"mode"`
	Queue          string                   `json:"queue"`
	OperatorSHA256 string                   `json:"operator_sha256,omitempty"`
	ReasonSHA256   string                   `json:"reason_sha256,omitempty"`
	Limit          int                      `json:"limit"`
	Inspected      int                      `json:"inspected"`
	Eligible       int                      `json:"eligible"`
	Replayed       int                      `json:"replayed"`
	Retained       int                      `json:"retained"`
	Uncertain      int                      `json:"uncertain"`
	Empty          bool                     `json:"empty"`
	Entries        []RiskControlReplayEntry `json:"entries"`
}

type RiskControlDLQReplayer struct {
	broker riskControlReplayBroker
	now    func() time.Time
}

func NewRiskControlDLQReplayer(broker riskControlReplayBroker) (*RiskControlDLQReplayer, error) {
	if broker == nil {
		return nil, errors.New("risk control replay broker is required")
	}
	return &RiskControlDLQReplayer{broker: broker, now: time.Now}, nil
}

func (r *RiskControlDLQReplayer) Run(
	ctx context.Context,
	options RiskControlReplayOptions,
) (RiskControlReplayReport, error) {
	if err := options.Validate(); err != nil {
		return RiskControlReplayReport{}, err
	}
	mode := "inspect"
	if options.Execute {
		mode = "execute"
	}
	report := RiskControlReplayReport{
		GeneratedAt: r.now().UTC(),
		Mode:        mode,
		Queue:       riskControlDLQ,
		Limit:       options.Limit,
		Entries:     make([]RiskControlReplayEntry, 0, options.Limit),
	}
	if options.Operator != "" {
		report.OperatorSHA256 = riskControlReplayDigest([]byte(options.Operator))
	}
	if options.Reason != "" {
		report.ReasonSHA256 = riskControlReplayDigest([]byte(options.Reason))
	}
	if options.Execute {
		if err := r.broker.EnablePublisherConfirms(); err != nil {
			return report, fmt.Errorf("enable risk control replay publisher confirms: %w", err)
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

func (r *RiskControlDLQReplayer) takeBatch(ctx context.Context, limit int) ([]amqp.Delivery, error) {
	deliveries := make([]amqp.Delivery, 0, limit)
	for len(deliveries) < limit {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, requeueRiskControlDeliveries(deliveries))
		}
		delivery, ok, err := r.broker.GetMessage(riskControlDLQ, false)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("get risk control dlq message: %w", err),
				requeueRiskControlDeliveries(deliveries),
			)
		}
		if !ok {
			break
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func (r *RiskControlDLQReplayer) inspect(
	deliveries []amqp.Delivery,
	options RiskControlReplayOptions,
	report RiskControlReplayReport,
) (RiskControlReplayReport, error) {
	for _, delivery := range deliveries {
		entry, eligible := inspectRiskControlReplayDelivery(delivery, options.MaxReplayCount)
		report.Inspected++
		if eligible {
			report.Eligible++
		} else {
			report.Retained++
		}
		report.Entries = append(report.Entries, entry)
	}
	if err := requeueRiskControlDeliveries(deliveries); err != nil {
		return report, fmt.Errorf("retain inspected risk control messages: %w", err)
	}
	return report, nil
}

func (r *RiskControlDLQReplayer) replay(
	ctx context.Context,
	deliveries []amqp.Delivery,
	options RiskControlReplayOptions,
	report RiskControlReplayReport,
) (RiskControlReplayReport, error) {
	for index, delivery := range deliveries {
		if err := ctx.Err(); err != nil {
			return report, errors.Join(err, requeueRiskControlDeliveries(deliveries[index:]))
		}
		entry, eligible := inspectRiskControlReplayDelivery(delivery, options.MaxReplayCount)
		report.Inspected++
		if !eligible {
			report.Retained++
			report.Entries = append(report.Entries, entry)
			if err := delivery.Nack(false, true); err != nil {
				return report, errors.Join(
					fmt.Errorf("retain ineligible risk control message: %w", err),
					requeueRiskControlDeliveries(deliveries[index+1:]),
				)
			}
			continue
		}

		report.Eligible++
		message := riskControlReplayMessage(delivery, entry.ReplayCount+1, r.now())
		if err := r.broker.PublishMessageConfirmed(
			ctx,
			riskControlIngressExchange,
			riskControlIngressRoutingKey,
			message,
		); err != nil {
			entry.Outcome = riskControlReplayOutcomeRetainedPublish
			entry.ErrorCode = riskControlReplayErrorPublishFailed
			report.Retained++
			report.Entries = append(report.Entries, entry)
			return report, errors.Join(
				fmt.Errorf("publish risk control replay: %w", err),
				requeueRiskControlDeliveries(deliveries[index:]),
			)
		}
		if err := delivery.Ack(false); err != nil {
			entry.Outcome = riskControlReplayOutcomeAckUncertain
			entry.ErrorCode = riskControlReplayErrorAckFailed
			report.Uncertain++
			report.Entries = append(report.Entries, entry)
			return report, errors.Join(
				fmt.Errorf("ack replayed risk control message: %w", err),
				requeueRiskControlDeliveries(deliveries[index+1:]),
			)
		}
		entry.Outcome = riskControlReplayOutcomeReplayed
		entry.ReplayCount++
		report.Replayed++
		report.Entries = append(report.Entries, entry)
	}
	return report, nil
}

func inspectRiskControlReplayDelivery(
	delivery amqp.Delivery,
	maxReplayCount int,
) (RiskControlReplayEntry, bool) {
	entry := RiskControlReplayEntry{
		MessageSHA256: riskControlReplayDigest(delivery.Body),
		Outcome:       riskControlReplayOutcomeEligible,
	}
	if delivery.RoutingKey != riskControlDLQRoutingKey {
		entry.Outcome = riskControlReplayOutcomeRetainedInvalid
		entry.ErrorCode = riskControlReplayErrorUnsupportedRoute
		return entry, false
	}

	replayCount, validCount := riskControlReplayHeaderCount(delivery.Headers)
	entry.ReplayCount = replayCount
	if !validCount {
		entry.Outcome = riskControlReplayOutcomeRetainedInvalid
		entry.ErrorCode = riskControlReplayErrorInvalidCount
		return entry, false
	}
	if len(delivery.Body) == 0 || len(delivery.Body) > riskControlReplayBodyLimit {
		entry.Outcome = riskControlReplayOutcomeRetainedInvalid
		entry.ErrorCode = riskControlReplayErrorOversizedEvent
		return entry, false
	}
	event, err := decodeRiskControlEvent(delivery.Body)
	if err != nil {
		entry.Outcome = riskControlReplayOutcomeRetainedInvalid
		entry.ErrorCode = riskControlReplayErrorMalformedEvent
		return entry, false
	}
	entry.WorkflowIdentitySHA256 = riskControlReplayDigest(
		[]byte(fmt.Sprintf("RiskControl-Tweet-%d", event.TweetID)),
	)
	if entry.ReplayCount >= maxReplayCount {
		entry.Outcome = riskControlReplayOutcomeRetainedLimit
		entry.ErrorCode = riskControlReplayErrorReplayLimit
		return entry, false
	}
	return entry, true
}

func riskControlReplayHeaderCount(headers amqp.Table) (int, bool) {
	if headers == nil {
		return 0, true
	}
	value, exists := headers[riskControlReplayHeader]
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
	if count < 0 || count > riskControlReplayCountMax {
		return 0, false
	}
	return int(count), true
}

func riskControlReplayMessage(delivery amqp.Delivery, replayCount int, replayedAt time.Time) amqp.Publishing {
	message := copyRiskControlMessage(delivery)
	for _, header := range []string{
		riskControlRetryHeader,
		"x-death",
		"x-first-death-exchange",
		"x-first-death-queue",
		"x-first-death-reason",
		"x-last-death-exchange",
		"x-last-death-queue",
		"x-last-death-reason",
	} {
		delete(message.Headers, header)
	}
	message.Headers[riskControlReplayHeader] = int32(replayCount)
	message.Headers[riskControlReplayedAtHeader] = replayedAt.UTC().UnixMilli()
	return message
}

func requeueRiskControlDeliveries(deliveries []amqp.Delivery) error {
	var joined error
	for _, delivery := range deliveries {
		if err := delivery.Nack(false, true); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func riskControlReplayDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
