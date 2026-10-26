package domain

import "context"

// ApplyLike exposes the persisted relation identity and whether this transaction
// created it. An idempotent HTTP retry must not create another domain event.
type LikeMutationRepository interface {
	ApplyLike(context.Context, uint64, uint64) (*Like, bool, error)
}
