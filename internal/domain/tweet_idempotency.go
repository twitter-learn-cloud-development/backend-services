package domain

import (
	"context"
	"errors"
)

var (
	ErrTweetCreateIdempotencyNotFound = errors.New("tweet create idempotency record not found")
	ErrTweetCreateIdempotencyExists   = errors.New("tweet create idempotency key already exists")
)

// TweetCreateIdempotency binds a caller-owned key to one committed tweet.
// It is inserted in the same transaction as the tweet and its outbox event.
type TweetCreateIdempotency struct {
	ID             uint64 `gorm:"primaryKey;autoIncrement;column:id"`
	UserID         uint64 `gorm:"not null;uniqueIndex:uk_tweet_create_user_key,priority:1;column:user_id"`
	IdempotencyKey string `gorm:"type:varchar(160);not null;uniqueIndex:uk_tweet_create_user_key,priority:2;column:idempotency_key"`
	InputDigest    string `gorm:"type:char(64);not null;column:input_digest"`
	TweetID        uint64 `gorm:"not null;index;column:tweet_id"`
	CreatedAt      int64  `gorm:"not null;index;column:created_at"`
}

func (TweetCreateIdempotency) TableName() string {
	return "tweet_create_idempotency"
}

type TweetCreateIdempotencyRepository interface {
	Create(ctx context.Context, record *TweetCreateIdempotency) error
	Get(ctx context.Context, userID uint64, idempotencyKey string) (*TweetCreateIdempotency, error)
}
