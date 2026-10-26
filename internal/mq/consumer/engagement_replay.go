package consumer

import (
	"context"
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"strings"
	"time"
)

type EngagementReplayOptions struct {
	Event                 string
	Limit, MaxReplayCount int
	Execute               bool
	Operator, Reason      string
}

func (o *EngagementReplayOptions) Validate() error {
	if o == nil {
		return errors.New("engagement replay options required")
	}
	o.Operator = strings.TrimSpace(o.Operator)
	o.Reason = strings.TrimSpace(o.Reason)
	if o.Event != "liked" && o.Event != "comment" {
		return errors.New("event must be liked or comment")
	}
	if o.Limit < 1 || o.Limit > 100 || o.MaxReplayCount < 1 || o.MaxReplayCount > 10 {
		return errors.New("replay limits out of range")
	}
	if len(o.Operator) > 128 || len(o.Reason) > 512 || (o.Execute && (o.Operator == "" || o.Reason == "")) {
		return errors.New("bounded operator and reason required for execution")
	}
	return nil
}

type EngagementDLQReplayer struct{ engine *TimelineEventDLQReplayer }

func NewEngagementDLQReplayer(broker timelineEventReplayBroker) (*EngagementDLQReplayer, error) {
	engine, err := NewTimelineEventDLQReplayer(broker)
	if err != nil {
		return nil, err
	}
	engine.declare = DeclareEngagementRecoveryTopology
	engine.inspector = func(d amqp.Delivery, r timelineEventReplayRoute, max int) (TimelineEventReplayEntry, bool) {
		return inspectEngagementReplay(d, r, max, engine.now())
	}
	return &EngagementDLQReplayer{engine: engine}, nil
}
func (r *EngagementDLQReplayer) Run(ctx context.Context, options EngagementReplayOptions) (TimelineEventReplayReport, error) {
	if err := options.Validate(); err != nil {
		return TimelineEventReplayReport{}, err
	}
	source, queue := RoutingKeyTweetLiked, QueueTweetLikedDLQ
	if options.Event == "comment" {
		source, queue = RoutingKeyCommentCreated, QueueCommentCreatedDLQ
	}
	route, _ := timelineRecoveryRouteForSource(source)
	recovery := timelineEventReplayRoute{kind: TimelineEventKind(options.Event), queue: queue, dlqRoutingKey: source + ".dlq", ingressRoutingKey: route.ingressRoutingKey}
	shared := TimelineEventReplayOptions{Kind: recovery.kind, Limit: options.Limit, MaxReplayCount: options.MaxReplayCount, Execute: options.Execute, Operator: options.Operator, Reason: options.Reason}
	return r.engine.runRoute(ctx, shared, recovery)
}
func inspectEngagementReplay(delivery amqp.Delivery, route timelineEventReplayRoute, max int, now time.Time) (TimelineEventReplayEntry, bool) {
	entry := TimelineEventReplayEntry{MessageSHA256: timelineEventReplayDigest(delivery.Body), Outcome: timelineEventReplayOutcomeRetainedInvalid}
	if delivery.RoutingKey != route.dlqRoutingKey {
		entry.ErrorCode = timelineEventReplayErrorUnsupportedRoute
		return entry, false
	}
	count, valid := timelineEventReplayHeaderCount(delivery.Headers)
	entry.ReplayCount = count
	if !valid {
		entry.ErrorCode = timelineEventReplayErrorInvalidCount
		return entry, false
	}
	kind := RoutingKeyTweetLiked
	if route.kind == "comment" {
		kind = RoutingKeyCommentCreated
	}
	event, err := decodeEngagement(delivery, kind)
	if err != nil {
		entry.ErrorCode = timelineEventReplayErrorMalformedEvent
		return entry, false
	}
	entry.EventIdentitySHA256 = event.Digest
	if event.Occurred > now.Add(5*time.Minute).UnixMilli() {
		entry.ErrorCode = "future_event"
		return entry, false
	}
	if event.Occurred < now.Add(-48*time.Hour).UnixMilli() {
		entry.Outcome = "retained_expired"
		entry.ErrorCode = "event_expired"
		return entry, false
	}
	if count >= max {
		entry.Outcome = timelineEventReplayOutcomeRetainedLimit
		entry.ErrorCode = timelineEventReplayErrorReplayLimit
		return entry, false
	}
	entry.Outcome = timelineEventReplayOutcomeEligible
	return entry, true
}
