package attribution

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	EngagementKindLike    = "like"
	EngagementKindComment = "comment"
)

var (
	ErrPublishedContentNotFound = errors.New("published content attribution not found")
	ErrPublishedContentConflict = errors.New("published content attribution conflicts with existing record")
)

type PublishedContent struct {
	ID                   primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TweetID              uint64             `bson:"tweet_id" json:"tweet_id"`
	AuthorUserID         uint64             `bson:"author_user_id" json:"author_user_id"`
	SourceRunID          string             `bson:"source_run_id" json:"source_run_id"`
	PublishedAt          time.Time          `bson:"published_at" json:"published_at"`
	ExpiresAt            time.Time          `bson:"expires_at" json:"expires_at"`
	OutcomeRecordedAt    time.Time          `bson:"outcome_recorded_at,omitempty" json:"outcome_recorded_at,omitempty"`
	EngagementEventID    string             `bson:"engagement_event_id,omitempty" json:"engagement_event_id,omitempty"`
	EngagementKind       string             `bson:"engagement_kind,omitempty" json:"engagement_kind,omitempty"`
	EngagementOccurredAt time.Time          `bson:"engagement_occurred_at,omitempty" json:"engagement_occurred_at,omitempty"`
	UpdatedAt            time.Time          `bson:"updated_at" json:"updated_at"`
}

func (r *PublishedContent) Validate() error {
	if r == nil || r.TweetID == 0 || r.AuthorUserID == 0 {
		return errors.New("published content identity is required")
	}
	r.SourceRunID = strings.TrimSpace(r.SourceRunID)
	if r.SourceRunID == "" || len(r.SourceRunID) > 128 {
		return errors.New("bounded source run id is required")
	}
	if r.PublishedAt.IsZero() || !r.ExpiresAt.After(r.PublishedAt) {
		return errors.New("valid attribution window is required")
	}
	return nil
}

type ContentEngagement struct {
	EventID      string
	Kind         string
	TweetID      uint64
	ActorUserID  uint64
	AuthorUserID uint64
	OccurredAt   time.Time
}

func (e *ContentEngagement) Validate() error {
	if e == nil || e.TweetID == 0 || e.ActorUserID == 0 || e.AuthorUserID == 0 || e.OccurredAt.IsZero() {
		return errors.New("content engagement identity and occurrence time are required")
	}
	e.EventID = strings.TrimSpace(e.EventID)
	e.Kind = strings.TrimSpace(e.Kind)
	if e.EventID == "" || len(e.EventID) > 160 {
		return errors.New("bounded content engagement event id is required")
	}
	switch e.Kind {
	case EngagementKindLike, EngagementKindComment:
		return nil
	default:
		return errors.New("unsupported content engagement kind")
	}
}

type Store interface {
	SavePublishedContent(ctx context.Context, record *PublishedContent) error
	GetPublishedContent(ctx context.Context, tweetID uint64) (*PublishedContent, error)
	MarkOutcomeRecorded(ctx context.Context, tweetID uint64, eventID, kind string, occurredAt time.Time) (bool, error)
}
