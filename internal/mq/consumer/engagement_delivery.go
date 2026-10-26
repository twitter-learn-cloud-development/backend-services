package consumer

import (
	"context"
	"errors"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"log"
	"time"
	"twitter-clone/internal/domain"
)

func (c *TimelineConsumer) consumeTweetLiked(ctx context.Context) {
	c.consumeEngagement(ctx, QueueTweetLiked, RoutingKeyTweetLiked)
}
func (c *TimelineConsumer) consumeCommentCreated(ctx context.Context) {
	c.consumeEngagement(ctx, QueueCommentCreated, RoutingKeyCommentCreated)
}
func (c *TimelineConsumer) consumeEngagement(ctx context.Context, queue, kind string) {
	for ctx.Err() == nil {
		if c.engagementSessionFactory == nil {
			return
		}
		sessionCtx, cancelSession := context.WithCancel(ctx)
		session, err := c.engagementSessionFactory(sessionCtx, queue, kind)
		if err != nil {
			cancelSession()
			log.Print("engagement consume unavailable")
			c.engagementResult(kind, "consume_unavailable", time.Now())
			if !waitForModerationReconnect(ctx) {
				return
			}
			continue
		}
		if session == nil || session.messages == nil || session.router == nil || session.close == nil {
			cancelSession()
			if session != nil && session.close != nil {
				session.close()
			}
			c.engagementResult(kind, "consume_unavailable", time.Now())
			if !waitForModerationReconnect(ctx) {
				return
			}
			continue
		}
		handler := *c
		handler.failureRouter = session.router
		handlerContext := session.ctx
		if handlerContext == nil {
			handlerContext = sessionCtx
		}
		for {
			select {
			case <-ctx.Done():
				cancelSession()
				session.close()
				return
			case msg, ok := <-session.messages:
				if !ok {
					cancelSession()
					session.close()
					c.engagementResult(kind, "consume_unavailable", time.Now())
					if !waitForModerationReconnect(ctx) {
						return
					}
					goto reconnect
				}
				if ctx.Err() != nil {
					cancelSession()
					session.close()
					return
				}
				handler.handleEngagementMessage(handlerContext, msg, kind)
			}
		}
	reconnect:
	}
}
func (c *TimelineConsumer) handleEngagementMessage(parent context.Context, msg amqp.Delivery, kind string) {
	start := time.Now()
	event, err := decodeEngagement(msg, kind)
	if err != nil {
		c.engagementResult(kind, "malformed", start)
		c.handlePermanentFailure(msg, kind)
		return
	}
	if event.Occurred > time.Now().Add(5*time.Minute).UnixMilli() {
		c.engagementResult(kind, "malformed", start)
		c.handlePermanentFailure(msg, kind)
		return
	}
	ctx := propagation.TraceContext{}.Extract(parent, propagation.MapCarrier{"traceparent": event.Traceparent, "tracestate": event.Tracestate})
	ctx, span := otel.Tracer("engagement-consumer").Start(ctx, "Engagement.Project")
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result := ""
	if c.engagementExecutor != nil {
		result, err = c.engagementExecutor(ctx, event)
	} else {
		projector := &engagementProjector{redis: c.redisClient}
		if !engagementCurrentAge(event) {
			result, err = projector.Project(ctx, event, false)
		} else if c.projectionRepo == nil {
			err = errors.New("engagement fact reader unavailable")
		} else {
			err = c.projectionRepo.WithProjectionLock(ctx, event.TweetID, func(locked context.Context) error {
				fact, err := c.projectionRepo.ReadProjectionTweet(locked, event.TweetID)
				if err != nil {
					return err
				}
				if fact != nil && (fact.ID != event.TweetID || fact.UserID != event.AuthorID) {
					return errEngagementIdentityConflict
				}
				eligible := fact != nil && fact.DeletedAt == 0 && fact.VisibleType == domain.VisiblePublic && fact.CreatedAt >= time.Now().Add(-48*time.Hour).UnixMilli()
				result, err = projector.Project(locked, event, eligible)
				return err
			})
		}
	}
	if err != nil {
		span.SetStatus(codes.Error, "engagement projection failed")
		if parent.Err() != nil {
			if msg.Nack(false, true) != nil {
				c.engagementResult(kind, "ack_uncertain", start)
				return
			}
			c.engagementResult(kind, "cancelled", start)
			return
		}
		c.engagementResult(kind, "failed", start)
		log.Printf("engagement projection failed: kind=%s", kind)
		if errors.Is(err, errEngagementIdentityConflict) || errors.Is(err, errEngagementInvalidTime) {
			c.handlePermanentFailure(msg, kind)
		} else {
			c.handleFailure(msg, kind)
		}
		return
	}
	if msg.Ack(false) != nil {
		c.engagementResult(kind, "ack_uncertain", start)
		return
	}
	c.engagementResult(kind, result, start)
}
func (c *TimelineConsumer) engagementResult(kind, result string, start time.Time) {
	if c.engagementObserver != nil {
		c.engagementObserver.Observe(kind, result, time.Since(start))
	}
}
