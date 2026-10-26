package domain

import (
	"context"
	"errors"
)

const TweetSubjectDeletionSchemaV1 = "tweet-subject-deletion/v1"

var (
	ErrTweetSubjectDeletionNotFound = errors.New("tweet subject deletion operation not found")
	ErrTweetSubjectDeletionExists   = errors.New("tweet subject deletion operation already exists")
)

// TweetSubjectInventory is deliberately limited to records owned by the
// subject. Dependent relation rows for authored tweets are removed as part of
// the same transaction but do not count as another user's owned content.
type TweetSubjectInventory struct {
	AuthoredTweets   int64
	AuthoredComments int64
	Likes            int64
	Bookmarks        int64
	Retweets         int64
	PollVotes        int64
}

func (inventory TweetSubjectInventory) Total() int64 {
	return inventory.AuthoredTweets + inventory.AuthoredComments + inventory.Likes +
		inventory.Bookmarks + inventory.Retweets + inventory.PollVotes
}

// TweetSubjectDeletionOperation is the durable, content-free idempotency
// receipt committed in the same transaction as deletion and cleanup events.
type TweetSubjectDeletionOperation struct {
	ID               uint64 `gorm:"primaryKey;autoIncrement;column:id"`
	OperationKey     string `gorm:"type:char(64);not null;uniqueIndex;column:operation_key"`
	SubjectDigest    string `gorm:"type:char(64);not null;index;column:subject_digest"`
	GovernanceDigest string `gorm:"type:char(64);not null;column:governance_digest"`
	BeforeTweets     int64  `gorm:"not null;column:before_tweets"`
	BeforeComments   int64  `gorm:"not null;column:before_comments"`
	BeforeLikes      int64  `gorm:"not null;column:before_likes"`
	BeforeBookmarks  int64  `gorm:"not null;column:before_bookmarks"`
	BeforeRetweets   int64  `gorm:"not null;column:before_retweets"`
	BeforePollVotes  int64  `gorm:"not null;column:before_poll_votes"`
	AfterTweets      int64  `gorm:"not null;column:after_tweets"`
	AfterComments    int64  `gorm:"not null;column:after_comments"`
	AfterLikes       int64  `gorm:"not null;column:after_likes"`
	AfterBookmarks   int64  `gorm:"not null;column:after_bookmarks"`
	AfterRetweets    int64  `gorm:"not null;column:after_retweets"`
	AfterPollVotes   int64  `gorm:"not null;column:after_poll_votes"`
	ReceiptDigest    string `gorm:"type:char(64);not null;column:receipt_digest"`
	CompletedAt      int64  `gorm:"not null;index;column:completed_at"`
	CreatedAt        int64  `gorm:"not null;column:created_at"`
}

func (TweetSubjectDeletionOperation) TableName() string {
	return "tweet_subject_deletion_operations"
}

func (operation TweetSubjectDeletionOperation) BeforeInventory() TweetSubjectInventory {
	return TweetSubjectInventory{
		AuthoredTweets: operation.BeforeTweets, AuthoredComments: operation.BeforeComments,
		Likes: operation.BeforeLikes, Bookmarks: operation.BeforeBookmarks,
		Retweets: operation.BeforeRetweets, PollVotes: operation.BeforePollVotes,
	}
}

func (operation TweetSubjectDeletionOperation) AfterInventory() TweetSubjectInventory {
	return TweetSubjectInventory{
		AuthoredTweets: operation.AfterTweets, AuthoredComments: operation.AfterComments,
		Likes: operation.AfterLikes, Bookmarks: operation.AfterBookmarks,
		Retweets: operation.AfterRetweets, PollVotes: operation.AfterPollVotes,
	}
}

type TweetSubjectDeletionRepository interface {
	GetOperation(context.Context, string) (*TweetSubjectDeletionOperation, error)
	CreateOperation(context.Context, *TweetSubjectDeletionOperation) error
	ObserveSubject(context.Context, uint64) (TweetSubjectInventory, error)
	ListAuthoredTweetIDs(context.Context, uint64, int) ([]uint64, error)
	DeleteOwnedData(context.Context, uint64, []uint64) error
}
