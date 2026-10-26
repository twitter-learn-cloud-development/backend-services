package domain

import "context"

// TweetProjectionRepository serializes cooperating index writers on one Tweet.
// Reads include soft-deleted records; nil means the current row is absent.
// The lock occupies one connection, without keeping a SQL transaction open.
type TweetProjectionRepository interface {
	WithProjectionLock(context.Context, uint64, func(context.Context) error) error
	ReadProjectionTweet(context.Context, uint64) (*Tweet, error)
}
