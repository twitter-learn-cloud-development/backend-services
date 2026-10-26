package consumer

import (
	"context"
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
	"time"
)

func TestEngagementSessionRebuildAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opened, closed atomic.Int32
	ready := make(chan struct{})
	done := make(chan struct{})
	consumer := &TimelineConsumer{engagementSessionFactory: func(sessionCtx context.Context, queue, kind string) (*engagementSession, error) {
		require.Equal(t, QueueTweetLiked, queue)
		require.Equal(t, RoutingKeyTweetLiked, kind)
		count := opened.Add(1)
		if count == 1 {
			return nil, errors.New("initial connect failed")
		}
		messages := make(chan amqp.Delivery)
		if count == 2 {
			close(messages)
		} else {
			close(ready)
		}
		router, err := newTimelineFailureRouter(&timelineFailurePublisherFake{})
		require.NoError(t, err)
		return &engagementSession{messages: messages, router: router, close: func() { require.ErrorIs(t, sessionCtx.Err(), context.Canceled); closed.Add(1) }}, nil
	}}
	go func() { defer close(done); consumer.consumeTweetLiked(ctx) }()
	select {
	case <-ready:
	case <-time.After(13 * time.Second):
		t.Fatal("session was not rebuilt")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown stalled")
	}
	require.Equal(t, int32(3), opened.Load())
	require.Equal(t, int32(2), closed.Load())
}
func TestEngagementSessionCancellationDoesNotStartAnotherConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan struct{})
	var count atomic.Int32
	c := &TimelineConsumer{engagementSessionFactory: func(ctx context.Context, _, _ string) (*engagementSession, error) {
		count.Add(1)
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	go func() { defer close(done); c.consumeCommentCreated(ctx) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("setup cancellation stalled")
	}
	require.Equal(t, int32(1), count.Load())
}

type engagementSessionAck struct {
	timelineFailureAcknowledgerFake
	onAck func()
}

func (a *engagementSessionAck) Ack(tag uint64, multiple bool) error {
	err := a.timelineFailureAcknowledgerFake.Ack(tag, multiple)
	if a.onAck != nil {
		a.onAck()
	}
	return err
}
func TestEngagementSessionUsesOwnedFailurePublisher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	acked := make(chan struct{})
	owned, shared := &timelineFailurePublisherFake{}, &timelineFailurePublisherFake{}
	ownedRouter, err := newTimelineFailureRouter(owned)
	require.NoError(t, err)
	sharedRouter, err := newTimelineFailureRouter(shared)
	require.NoError(t, err)
	ack := &engagementSessionAck{onAck: func() { close(acked) }}
	delivery := engagementDelivery(t, 1, RoutingKeyTweetLiked)
	delivery.Acknowledger = ack
	messages := make(chan amqp.Delivery, 1)
	messages <- delivery
	c := &TimelineConsumer{failureRouter: sharedRouter, engagementSessionFactory: func(ctx context.Context, _, _ string) (*engagementSession, error) {
		return &engagementSession{ctx: ctx, messages: messages, router: ownedRouter, close: func() {}}, nil
	}, engagementExecutor: func(context.Context, engagementEvent) (string, error) { return "", errors.New("transient") }}
	go func() { defer close(done); c.consumeTweetLiked(ctx) }()
	select {
	case <-acked:
	case <-time.After(time.Second):
		t.Fatal("delivery stalled")
	}
	cancel()
	<-done
	require.Len(t, owned.published, 1)
	require.Empty(t, shared.published)
}
func TestEngagementSessionStopsInFlightProjection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan struct{})
	messages := make(chan amqp.Delivery, 1)
	ack := &timelineFailureAcknowledgerFake{}
	delivery := engagementDelivery(t, 1, RoutingKeyTweetLiked)
	delivery.Acknowledger = ack
	messages <- delivery
	router, err := newTimelineFailureRouter(&timelineFailurePublisherFake{})
	require.NoError(t, err)
	c := &TimelineConsumer{engagementSessionFactory: func(ctx context.Context, _, _ string) (*engagementSession, error) {
		return &engagementSession{ctx: ctx, messages: messages, router: router, close: func() {}}, nil
	}, engagementExecutor: func(ctx context.Context, _ engagementEvent) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}}
	go func() { defer close(done); c.consumeTweetLiked(ctx) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("in-flight work ignored shutdown")
	}
	require.Zero(t, ack.acked)
	require.True(t, ack.requeue)
}
