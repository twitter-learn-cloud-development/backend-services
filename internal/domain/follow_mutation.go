package domain

import (
	"context"
	"errors"
)

var (
	ErrFollowAlreadyActive       = errors.New("already following")
	ErrFollowNotActive           = errors.New("not following this user")
	ErrFollowDuplicateActive     = errors.New("duplicate active follow relations require reconciliation")
	ErrFollowMutationUnavailable = errors.New("transactional follow mutation unavailable")
)

// FollowPairLock supplies a row even when a pair has no active relation. Keep it
// across unfollow/refollow so cooperating writers never lock different rows.
type FollowPairLock struct {
	FollowerID uint64 `gorm:"primaryKey;autoIncrement:false;column:follower_id"`
	FolloweeID uint64 `gorm:"primaryKey;autoIncrement:false;column:followee_id"`
}

func (FollowPairLock) TableName() string { return "follow_pair_locks" }

type FollowTransition struct {
	RelationID uint64
	OccurredAt int64
}
type FollowMutationRepository interface {
	// Must run within the caller's UoW. Duplicate requests do not mutate facts.
	ApplyFollowTransition(context.Context, uint64, uint64, bool) (FollowTransition, error)
}
