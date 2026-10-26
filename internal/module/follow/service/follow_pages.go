package service

import (
	"context"
	"errors"
)

const maxPublicFollowPageSize = 100

var (
	ErrInvalidFollowPageRequest    = errors.New("invalid follow page request")
	ErrFollowPaginationUnavailable = errors.New("follow relation pagination is unavailable")
	ErrInvalidFollowPage           = errors.New("invalid follow relation page")
)

func publicFollowPageLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > maxPublicFollowPageSize {
		return maxPublicFollowPageSize
	}
	return limit
}

func validateFollowPageRequest(ctx context.Context, userID uint64) error {
	if userID == 0 {
		return ErrInvalidFollowPageRequest
	}
	return ctx.Err()
}

// The pager owns relation IDs; never manufacture a cursor from user IDs when
// it is absent or inconsistent. A broken page must not become an infinite loop.
func validateFollowPage(ids []uint64, nextCursor uint64, hasMore bool, cursor uint64, limit int) error {
	if len(ids) > limit || (!hasMore && nextCursor != 0) {
		return ErrInvalidFollowPage
	}
	if hasMore && (len(ids) != limit || nextCursor == 0 || (cursor != 0 && nextCursor >= cursor)) {
		return ErrInvalidFollowPage
	}
	for _, id := range ids {
		if id == 0 {
			return ErrInvalidFollowPage
		}
	}
	return nil
}

func nonNilFollowIDs(ids []uint64) []uint64 {
	if ids == nil {
		return []uint64{}
	}
	return ids
}
