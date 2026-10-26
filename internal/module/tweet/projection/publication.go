package projection

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
	"twitter-clone/internal/infrastructure/canal"
)

type PublicationRow struct {
	Subject           string `json:"event_digest"`
	Type              string `json:"event_type"`
	PayloadDigest     string `json:"payload_digest"`
	Receipt           string `json:"receipt"`
	RetentionEligible bool   `json:"retention_eligible"`
}
type PublicationPlan struct {
	Version string           `json:"version"`
	Target  string           `json:"target_digest"`
	Rows    []PublicationRow `json:"rows"`
	Digest  string           `json:"plan_digest"`
}
type PublicationMaintenance struct {
	Repository domain.OutboxPublicationRepository
	Publisher  canal.MQProducer
	Target     string
	Now        func() time.Time
}

func (m PublicationMaintenance) Inspect(ctx context.Context, ids []uint64) (PublicationPlan, error) {
	ids, err := NormalizeIDs(ids)
	if err != nil {
		return PublicationPlan{}, err
	}
	if m.Repository == nil || len(m.Target) != 64 || m.Now == nil {
		return PublicationPlan{}, errors.New("publication maintenance unavailable")
	}
	events, err := m.Repository.ReadEvents(ctx, ids)
	if err != nil {
		return PublicationPlan{}, err
	}
	if len(events) != len(ids) {
		return PublicationPlan{}, errors.New("publication scope mismatch")
	}
	plan := PublicationPlan{Version: "publication-plan-v1", Target: m.Target, Rows: []PublicationRow{}}
	// The digest binds immutable event identities/content, so partial confirmed
	// replay can resume with the original authorization without another publish set.
	bindings := []string{m.Target}
	for i, event := range events {
		if event.ID != ids[i] || !validPublicationEvent(event) {
			return PublicationPlan{}, errors.New("invalid publication identity or schema")
		}
		if _, ok := canal.OutboxRoute(event.EventType); !ok {
			return PublicationPlan{}, errors.New("unsupported publication route")
		}
		digest, err := domain.OutboxPayloadDigest(event.Payload)
		if err != nil {
			return PublicationPlan{}, err
		}
		row := PublicationRow{Subject: Subject(event.ID), Type: event.EventType, PayloadDigest: digest, Receipt: "missing"}
		receipt, err := m.Repository.ReadPublication(ctx, event.ID)
		if err != nil {
			return PublicationPlan{}, err
		}
		if receipt != nil {
			if receipt.EventType != event.EventType || receipt.PayloadDigest != digest || receipt.ConfirmedAt <= 0 {
				row.Receipt = "conflict"
			} else {
				row.Receipt = "confirmed"
				cutoff := m.Now().Add(-72 * time.Hour).UnixMilli()
				row.RetentionEligible = event.CreatedAt > 0 && event.CreatedAt < cutoff && receipt.ConfirmedAt < cutoff
			}
		}
		plan.Rows = append(plan.Rows, row)
		bindings = append(bindings, row.Subject+"|"+row.Type+"|"+digest)
	}
	b, _ := json.Marshal(bindings)
	plan.Digest = Hash(b)
	return plan, nil
}

func validPublicationEvent(record domain.OutboxEvent) bool {
	switch record.EventType {
	case events.OutboxEventTypeUserFollowed, events.OutboxEventTypeUserUnfollowed:
		var event events.FollowRelationEvent
		return json.Unmarshal([]byte(record.Payload), &event) == nil && event.Validate(record.EventType == events.OutboxEventTypeUserFollowed) == nil
	case "TWEET_CREATED", "TWEET_DELETED":
		var event events.TweetDeletedEvent
		return json.Unmarshal([]byte(record.Payload), &event) == nil && event.TweetID > 0 && event.AuthorID > 0
	case "TWEET_MODERATED":
		var event events.TweetModeratedEvent
		return json.Unmarshal([]byte(record.Payload), &event) == nil && event.Validate() == nil
	case "TWEET_LIKED":
		var event events.TweetLikedEvent
		return json.Unmarshal([]byte(record.Payload), &event) == nil && event.TweetID > 0 && event.UserID > 0
	case "COMMENT_CREATED":
		var event events.CommentCreatedEvent
		return json.Unmarshal([]byte(record.Payload), &event) == nil && event.CommentID > 0 && event.TweetID > 0 && event.UserID > 0
	default:
		return false
	}
}
func (m PublicationMaintenance) Replay(ctx context.Context, ids []uint64, auth RepairAuthorization) (PublicationPlan, int, error) {
	if !auth.Valid() || m.Publisher == nil {
		return PublicationPlan{}, 0, errors.New("explicit publication replay authorization unavailable")
	}
	plan, err := m.Inspect(ctx, ids)
	if err != nil {
		return PublicationPlan{}, 0, err
	}
	if plan.Digest != auth.ApprovedPlan {
		return plan, 0, errors.New("publication plan changed")
	}
	for _, row := range plan.Rows {
		if row.Receipt == "conflict" {
			return plan, 0, errors.New("publication receipt conflict")
		}
		// Engagement consumers still acknowledge before asynchronous work. Until
		// M6 adds event-level idempotency, even an approved replay must fail closed.
		if row.Type != "TWEET_CREATED" && row.Type != "TWEET_DELETED" && row.Type != "TWEET_MODERATED" {
			return plan, 0, errors.New("publication replay route is not yet idempotent")
		}
	}
	ids, _ = NormalizeIDs(ids)
	events, err := m.Repository.ReadEvents(ctx, ids)
	if err != nil {
		return plan, 0, err
	}
	if len(events) != len(plan.Rows) {
		return plan, 0, errors.New("publication scope changed")
	}
	confirmed := 0
	for i, event := range events {
		digest, err := domain.OutboxPayloadDigest(event.Payload)
		if err != nil || digest != plan.Rows[i].PayloadDigest || Subject(event.ID) != plan.Rows[i].Subject || event.EventType != plan.Rows[i].Type {
			return plan, confirmed, errors.New("publication event changed")
		}
		route, ok := canal.OutboxRoute(event.EventType)
		if !ok {
			return plan, confirmed, errors.New("publication route changed")
		}
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = m.Publisher.PublishRawJSON(callCtx, route.Exchange, route.RoutingKey, []byte(event.Payload))
		cancel()
		if err != nil {
			return plan, confirmed, errors.New("publication confirmation unavailable")
		}
		if err := m.Repository.RecordPublication(ctx, domain.OutboxPublication{EventID: event.ID, EventType: event.EventType, PayloadDigest: digest, ConfirmedAt: m.Now().UnixMilli(), Operation: auth.Operation, OperatorDigest: Hash([]byte(auth.Operator)), ReasonDigest: Hash([]byte(auth.Reason))}); err != nil {
			return plan, confirmed, err
		}
		confirmed++
		if i+1 < len(events) {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return plan, confirmed, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return plan, confirmed, nil
}
