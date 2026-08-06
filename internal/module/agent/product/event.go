package product

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const (
	EventDraftReady              = "draft_ready"
	EventDraftPublished          = "draft_published"
	EventConnectorConfigured     = "connector_configured"
	EventConnectorActivated      = "connector_activated"
	EventConnectorUsed           = "connector_used"
	EventConnectorFirstUsed      = "connector_first_used"
	EventConnectorReused         = "connector_reused"
	SubjectAgentRun              = "agent_run"
	SubjectExternalMCPConnection = "external_mcp_connection"
)

var ErrEventConflict = errors.New("agent product event conflicts with the persisted event")

// Dimensions contains only bounded product dimensions. User, Run, Connector,
// model and tool identities remain fields rather than metric labels.
type Dimensions struct {
	ExecutionProfile string `bson:"execution_profile,omitempty" json:"execution_profile,omitempty"`
	Strategy         string `bson:"strategy,omitempty" json:"strategy,omitempty"`
	Scope            string `bson:"scope,omitempty" json:"scope,omitempty"`
	Transport        string `bson:"transport,omitempty" json:"transport,omitempty"`
}

// Event is an append-only, content-free product fact. ID is derived from the
// logical identity so retries across requests and processes are idempotent.
type Event struct {
	ID               string     `bson:"_id" json:"id"`
	Kind             string     `bson:"kind" json:"kind"`
	UserID           uint64     `bson:"user_id" json:"user_id"`
	SubjectType      string     `bson:"subject_type" json:"subject_type"`
	SubjectID        string     `bson:"subject_id" json:"subject_id"`
	RelatedID        string     `bson:"related_id,omitempty" json:"related_id,omitempty"`
	OccurrenceDigest string     `bson:"occurrence_digest,omitempty" json:"occurrence_digest,omitempty"`
	Dimensions       Dimensions `bson:"dimensions" json:"dimensions"`
	OccurredAt       time.Time  `bson:"occurred_at" json:"occurred_at"`
	CreatedAt        time.Time  `bson:"created_at" json:"created_at"`
}

type Store interface {
	RecordProductEvent(ctx context.Context, event *Event) (created bool, err error)
	CountProductEvents(
		ctx context.Context,
		userID uint64,
		subjectType string,
		subjectID string,
		kind string,
		limit int64,
	) (int64, error)
}

func NewEvent(
	kind string,
	userID uint64,
	subjectType string,
	subjectID string,
	occurrenceKey string,
	relatedID string,
	dimensions Dimensions,
	occurredAt time.Time,
) (*Event, error) {
	kind = strings.TrimSpace(kind)
	subjectType = strings.TrimSpace(subjectType)
	subjectID = strings.TrimSpace(subjectID)
	occurrenceKey = strings.TrimSpace(occurrenceKey)
	event := &Event{
		Kind: kind, UserID: userID, SubjectType: subjectType, SubjectID: subjectID,
		RelatedID: strings.TrimSpace(relatedID), Dimensions: normalizeDimensions(dimensions),
		OccurredAt: occurredAt,
	}
	if occurrenceKey != "" {
		digest := sha256.Sum256([]byte(occurrenceKey))
		event.OccurrenceDigest = hex.EncodeToString(digest[:])
	}
	identity := strings.Join([]string{
		"agent-product-event:v1", kind, subjectType, subjectID, event.OccurrenceDigest,
	}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	event.ID = hex.EncodeToString(digest[:])
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return event, nil
}

func (event *Event) Validate() error {
	if event == nil {
		return errors.New("agent product event is required")
	}
	if len(strings.TrimSpace(event.ID)) != sha256.Size*2 || event.UserID == 0 ||
		strings.TrimSpace(event.Kind) == "" || strings.TrimSpace(event.SubjectType) == "" ||
		strings.TrimSpace(event.SubjectID) == "" || event.OccurredAt.IsZero() {
		return errors.New("agent product event identity is incomplete")
	}
	if len(event.Kind) > 64 || len(event.SubjectType) > 64 || len(event.SubjectID) > 160 ||
		len(event.RelatedID) > 160 ||
		(event.OccurrenceDigest != "" && len(event.OccurrenceDigest) != sha256.Size*2) {
		return errors.New("agent product event field exceeds its bound")
	}
	for _, value := range []string{
		event.Dimensions.ExecutionProfile,
		event.Dimensions.Strategy,
		event.Dimensions.Scope,
		event.Dimensions.Transport,
	} {
		if len(value) > 80 {
			return errors.New("agent product event dimension exceeds its bound")
		}
	}
	return nil
}

func SameFact(left, right *Event) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID && left.Kind == right.Kind && left.UserID == right.UserID &&
		left.SubjectType == right.SubjectType && left.SubjectID == right.SubjectID &&
		left.RelatedID == right.RelatedID && left.OccurrenceDigest == right.OccurrenceDigest
}

func normalizeDimensions(dimensions Dimensions) Dimensions {
	dimensions.ExecutionProfile = strings.TrimSpace(dimensions.ExecutionProfile)
	dimensions.Strategy = strings.TrimSpace(dimensions.Strategy)
	dimensions.Scope = strings.TrimSpace(dimensions.Scope)
	dimensions.Transport = strings.TrimSpace(dimensions.Transport)
	return dimensions
}
