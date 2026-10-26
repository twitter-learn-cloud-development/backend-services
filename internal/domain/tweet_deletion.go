package domain

import (
	"context"
	"errors"
)

var ErrTweetDeletionNotFound = errors.New("active tweet for deletion not found")

// TweetDeletionRepository reads only the identity needed to authorize a delete.
// It must lock the active row using the caller's UoW until commit or rollback.
type TweetDeletionRepository interface {
	GetForDeletion(ctx context.Context, id uint64) (*Tweet, error)
}
