package events

import (
	"errors"
	"fmt"
)

const (
	OutboxEventTypeUserFollowed   = "USER_FOLLOWED"
	OutboxEventTypeUserUnfollowed = "USER_UNFOLLOWED"
	FollowRelationSchemaVersion   = "follow-relation-v1"
)

// Old user.followed/user.unfollowed readers still see their two original fields.
// RelationID distinguishes refollow generations; consumers must revalidate facts.
type FollowRelationEvent struct {
	FollowerID       uint64 `json:"follower_id"`
	FolloweeID       uint64 `json:"followee_id"`
	RelationID       uint64 `json:"relation_id"`
	SchemaVersion    string `json:"schema_version"`
	EventKey         string `json:"event_key"`
	OccurredAtUnixMS int64  `json:"occurred_at_unix_ms"`
	Traceparent      string `json:"traceparent,omitempty"`
	Tracestate       string `json:"tracestate,omitempty"`
}

func (e FollowRelationEvent) Validate(following bool) error {
	if e.FollowerID == 0 || e.FolloweeID == 0 || e.FollowerID == e.FolloweeID || e.RelationID == 0 || e.OccurredAtUnixMS <= 0 || e.SchemaVersion != FollowRelationSchemaVersion || e.EventKey != FollowRelationEventKey(e.RelationID, following) {
		return errors.New("invalid follow relation event")
	}
	return nil
}

func FollowRelationEventKey(id uint64, following bool) string {
	action := "unfollowed"
	if following {
		action = "followed"
	}
	return fmt.Sprintf("follow:%d:%s", id, action)
}
