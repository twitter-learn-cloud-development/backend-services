package consumer

import (
	"context"
	"encoding/json"
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
	"twitter-clone/internal/events"
)

func TestEngagementReplayInspectAndDedicatedConfirmRoute(t *testing.T) {
	for _, kind := range []string{RoutingKeyTweetLiked, RoutingKeyCommentCreated} {
		t.Run(kind, func(t *testing.T) {
			eventName := "liked"
			queue := QueueTweetLikedDLQ
			if kind == RoutingKeyCommentCreated {
				eventName = "comment"
				queue = QueueCommentCreatedDLQ
			}
			for _, execute := range []bool{false, true} {
				ops := []string{}
				ack := &timelineEventReplayAcknowledgerFake{operations: &ops}
				delivery := engagementDelivery(t, 1, kind)
				delivery.RoutingKey = kind + ".dlq"
				delivery.Acknowledger = ack
				delivery.MessageId = "original"
				delivery.Headers = amqp.Table{timelineRetryHeader: int32(3), "x-death": "old", timelineEventReplayHeader: int32(0)}
				broker := &timelineEventReplayBrokerFake{deliveries: []amqp.Delivery{delivery}, operations: &ops}
				runner, err := NewEngagementDLQReplayer(broker)
				require.NoError(t, err)
				report, err := runner.Run(context.Background(), EngagementReplayOptions{Event: eventName, Limit: 1, MaxReplayCount: 1, Execute: execute, Operator: "operator", Reason: "repair"})
				require.NoError(t, err)
				require.Equal(t, []string{queue}, broker.requestedQueues)
				if execute {
					require.Equal(t, []string{"topology", "confirm", "get", "publish", "ack"}, ops)
					require.Equal(t, 1, report.Replayed)
					require.Len(t, broker.published, 1)
					route, _ := timelineRecoveryRouteForSource(kind)
					pub := broker.published[0]
					require.Equal(t, ExchangeTimelineIngress, pub.exchange)
					require.Equal(t, route.ingressRoutingKey, pub.routingKey)
					require.Equal(t, delivery.Body, pub.message.Body)
					require.Equal(t, "original", pub.message.MessageId)
					require.Equal(t, int32(1), pub.message.Headers[timelineEventReplayHeader])
					require.NotContains(t, pub.message.Headers, timelineRetryHeader)
					require.NotContains(t, pub.message.Headers, "x-death")
					require.Equal(t, int32(3), delivery.Headers[timelineRetryHeader])
				} else {
					require.Empty(t, broker.published)
					require.Zero(t, ack.acked)
					require.True(t, ack.requeue)
					require.False(t, broker.topologyStarted)
					require.Equal(t, 1, report.Eligible)
				}
				raw, err := json.Marshal(report)
				require.NoError(t, err)
				require.NotContains(t, string(raw), "tweet_id")
				require.NotContains(t, string(raw), "tweet_user_id")
				require.NotContains(t, string(raw), "operator\"")
				require.NotContains(t, string(raw), "content")
			}
		})
	}
}
func TestEngagementReplayPoisonExpiryAndAttemptLimits(t *testing.T) {
	for _, scenario := range []string{"wrong_route", "missing_time", "future", "expired", "bad_count", "limit", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			delivery := engagementDelivery(t, 1, RoutingKeyTweetLiked)
			delivery.RoutingKey = RoutingKeyTweetLiked + ".dlq"
			delivery.Acknowledger = &timelineEventReplayAcknowledgerFake{}
			var event events.TweetLikedEvent
			require.NoError(t, json.Unmarshal(delivery.Body, &event))
			switch scenario {
			case "wrong_route":
				delivery.RoutingKey = RoutingKeyCommentCreated + ".dlq"
			case "missing_time":
				event.OccurredAtUnixMS = 0
			case "future":
				event.OccurredAtUnixMS = time.Now().Add(time.Hour).UnixMilli()
			case "expired":
				event.OccurredAtUnixMS = time.Now().Add(-49 * time.Hour).UnixMilli()
			case "bad_count":
				delivery.Headers = amqp.Table{timelineEventReplayHeader: "bad"}
			case "limit":
				delivery.Headers = amqp.Table{timelineEventReplayHeader: int32(1)}
			}
			if scenario == "missing_time" || scenario == "future" || scenario == "expired" {
				delivery.Body, _ = json.Marshal(event)
			}
			if scenario == "oversized" {
				delivery.Body = []byte(strings.Repeat("x", 256*1024+1))
			}
			broker := &timelineEventReplayBrokerFake{deliveries: []amqp.Delivery{delivery}}
			runner, err := NewEngagementDLQReplayer(broker)
			require.NoError(t, err)
			report, err := runner.Run(context.Background(), EngagementReplayOptions{Event: "liked", Limit: 1, MaxReplayCount: 1, Execute: true, Operator: "op", Reason: "repair"})
			require.NoError(t, err)
			require.Equal(t, 1, report.Retained)
			require.Empty(t, broker.published)
			ack := delivery.Acknowledger.(*timelineEventReplayAcknowledgerFake)
			require.Zero(t, ack.acked)
			require.True(t, ack.requeue)
		})
	}
	require.Error(t, (&TimelineEventReplayOptions{Kind: "liked", Limit: 1, MaxReplayCount: 1}).Validate()) // lifecycle CLI stays restricted.
}
func TestEngagementReplayFailureAndCancellationRetainBatch(t *testing.T) {
	for _, scenario := range []string{"publish", "ack", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			a, b := &timelineEventReplayAcknowledgerFake{}, &timelineEventReplayAcknowledgerFake{}
			first := engagementDelivery(t, 1, RoutingKeyTweetLiked)
			first.RoutingKey = RoutingKeyTweetLiked + ".dlq"
			first.Acknowledger = a
			second := first
			second.Acknowledger = b
			broker := &timelineEventReplayBrokerFake{deliveries: []amqp.Delivery{first, second}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "publish":
				broker.publishErr = errors.New("timeout")
			case "ack":
				a.ackErr = errors.New("closed")
			case "cancel":
				broker.onGet = func(index int) {
					if index == 2 {
						cancel()
					}
				}
			}
			runner, err := NewEngagementDLQReplayer(broker)
			require.NoError(t, err)
			report, err := runner.Run(ctx, EngagementReplayOptions{Event: "liked", Limit: 2, MaxReplayCount: 1, Execute: true, Operator: "op", Reason: "repair"})
			require.Error(t, err)
			require.True(t, b.requeue)
			require.Equal(t, 1, b.nacked)
			if scenario == "ack" {
				require.Equal(t, 1, report.Uncertain)
				require.Equal(t, 1, a.acked)
			} else {
				require.Zero(t, a.acked)
				require.True(t, a.requeue)
			}
		})
	}
}
func TestEngagementRecoveryTopologyIsScoped(t *testing.T) {
	broker := newTimelineRecoveryTopologyFake()
	require.NoError(t, DeclareEngagementRecoveryTopology(broker))
	require.Len(t, broker.queues, 6)
	for _, kind := range []string{RoutingKeyTweetLiked, RoutingKeyCommentCreated} {
		route, _ := timelineRecoveryRouteForSource(kind)
		require.Equal(t, route.ingressRoutingKey, broker.queues[route.retryQueue]["x-dead-letter-routing-key"])
		assertTimelineTopologyBinding(t, broker.bindings, timelineTopologyBinding{route.queue, route.ingressRoutingKey, ExchangeTimelineIngress})
	}
	require.NotContains(t, broker.queues, QueueTweetFanout)
	require.NotContains(t, broker.queues, QueueTweetDelete)
}
