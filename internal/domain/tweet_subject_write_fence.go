package domain

import (
	"context"
	"errors"
)

const TweetSubjectWriteFenceSchemaV1 = "tweet-subject-write-fence/v1"

var (
	ErrTweetSubjectWriteFenced        = errors.New("tweet subject writes are fenced")
	ErrTweetSubjectWriteFenceMissing  = errors.New("tweet subject write fence is missing")
	ErrTweetSubjectWriteFenceConflict = errors.New("tweet subject write fence conflicts with an existing request")
	ErrTweetSubjectWriteFenceCorrupt  = errors.New("tweet subject write fence is invalid")
)

// TweetSubjectWriteFence is the per-subject serialization anchor for Tweet
// writes. Every subject-owned write locks this row in the same transaction as
// the business mutation, so fence installation cannot race a late write.
type TweetSubjectWriteFence struct {
	UserID           uint64 `gorm:"primaryKey;column:user_id"`
	SchemaVersion    string `gorm:"type:varchar(64);not null;column:schema_version"`
	Fenced           bool   `gorm:"not null;default:false;index;column:fenced"`
	RequestID        string `gorm:"type:varchar(64);not null;default:'';column:request_id"`
	SubjectDigest    string `gorm:"type:char(64);not null;default:'';column:subject_digest"`
	PlanDigest       string `gorm:"type:char(64);not null;default:'';column:plan_digest"`
	CapabilityDigest string `gorm:"type:char(64);not null;default:'';column:capability_digest"`
	GovernanceDigest string `gorm:"type:char(64);not null;default:'';column:governance_digest"`
	FenceRevision    int64  `gorm:"not null;default:0;column:fence_revision"`
	EnforcedAt       int64  `gorm:"not null;default:0;column:enforced_at"`
	CreatedAt        int64  `gorm:"not null;column:created_at"`
	UpdatedAt        int64  `gorm:"not null;column:updated_at"`
}

func (TweetSubjectWriteFence) TableName() string {
	return "tweet_subject_write_fences"
}

type TweetSubjectWriteFenceRepository interface {
	AuthorizeSubjectWrite(context.Context, uint64) error
	RequireSubjectWriteFence(context.Context, uint64) error
	EnforceSubjectWriteFence(context.Context, *TweetSubjectWriteFence) (*TweetSubjectWriteFence, bool, error)
}
