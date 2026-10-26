package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/pkg/database/uow"
	"twitter-clone/pkg/logger"
)

var ErrInvalidFollowIdentity = errors.New("follow identities must be nonzero")

// Configure before serving requests. Missing dependencies fail closed for writes;
// read-only instances and old test constructors remain usable.
func (s *FollowService) SetOutboxDependencies(manager uow.Manager, outbox domain.OutboxEventRepository) {
	s.uow = manager
	s.outbox = outbox
}

func (s *FollowService) changeFollowWithOutbox(ctx context.Context, follower, followee uint64, following bool) error {
	if follower == 0 || followee == 0 {
		return ErrInvalidFollowIdentity
	}
	if follower == followee {
		return ErrCannotFollowSelf
	}
	writer, ok := s.repo.(domain.FollowMutationRepository)
	if !ok || s.uow == nil || s.outbox == nil {
		return domain.ErrFollowMutationUnavailable
	}
	ctx, span := otel.Tracer("follow-service").Start(ctx, "FollowService.ChangeRelation")
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stage := "begin"
	err := s.uow.Do(ctx, func(txCtx context.Context) error {
		stage = "relation"
		transition, err := writer.ApplyFollowTransition(txCtx, follower, followee, following)
		if errors.Is(err, domain.ErrFollowAlreadyActive) {
			return ErrAlreadyFollowing
		}
		if errors.Is(err, domain.ErrFollowNotActive) {
			return ErrNotFollowing
		}
		if err != nil {
			return err
		}
		if transition.RelationID == 0 || transition.OccurredAt <= 0 {
			return errors.New("invalid follow transition")
		}
		key := events.FollowRelationEventKey(transition.RelationID, following)
		carrier := propagation.MapCarrier{}
		propagation.TraceContext{}.Inject(txCtx, carrier)
		payload, err := json.Marshal(events.FollowRelationEvent{FollowerID: follower, FolloweeID: followee, RelationID: transition.RelationID, SchemaVersion: events.FollowRelationSchemaVersion, EventKey: key, OccurredAtUnixMS: transition.OccurredAt, Traceparent: carrier.Get("traceparent"), Tracestate: carrier.Get("tracestate")})
		if err != nil {
			return err
		}
		kind := events.OutboxEventTypeUserUnfollowed
		if following {
			kind = events.OutboxEventTypeUserFollowed
		}
		stage = "outbox"
		// A conflicting event key aborts the fact mutation; never ignore conflicts.
		if err := s.outbox.Create(txCtx, &domain.OutboxEvent{EventType: kind, Payload: string(payload), DedupKey: &key, CreatedAt: transition.OccurredAt}); err != nil {
			return fmt.Errorf("register follow event: %w", err)
		}
		stage = "commit"
		return nil
	})
	span.SetAttributes(attribute.Bool("follow.following", following), attribute.String("follow.stage", stage))
	if err != nil {
		span.SetStatus(codes.Error, "follow_transition_failed")
		logger.Warn(ctx, "follow transaction not confirmed", zap.String("stage", stage))
		return err
	}
	span.SetAttributes(attribute.Bool("follow.committed", true))
	logger.Info(ctx, "follow relation and outbox committed")
	return nil
}
