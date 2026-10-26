package projection

import (
	"context"
	"twitter-clone/internal/domain"
	"twitter-clone/internal/events"
)

const (
	PairTask   = "follow_pair_v1"
	AuthorTask = "follow_author_v1"
	PageSize   = 100
)

type Checkpoint struct {
	Event           events.FollowRelationEvent `json:"event"`
	Cursor          uint64                     `json:"cursor"`
	Generation      uint64                     `json:"generation"`
	Initialized     bool                       `json:"initialized"`
	Repair          string                     `json:"repair,omitempty"`
	ApprovedPlan    string                     `json:"approved_plan,omitempty"`
	OperatorDigest  string                     `json:"operator_digest,omitempty"`
	ReasonDigest    string                     `json:"reason_digest,omitempty"`
	OperationDigest string                     `json:"operation_digest,omitempty"`
}
type PairFact struct {
	Generation uint64
	Active     bool
}
type AuthorState struct {
	AuthorID   uint64 `gorm:"primaryKey;autoIncrement:false"`
	Revision   uint64
	Epoch      uint64
	Celebrity  bool
	Cursor     uint64
	Complete   bool
	LastRepair string `gorm:"type:varchar(64)"`
}

func (AuthorState) TableName() string { return "follow_author_projections" }

type IngressReceipt struct {
	EventKey          string `gorm:"primaryKey;type:varchar(191)"`
	Digest            string `gorm:"type:char(64);not null"`
	CreatedAt         int64
	RegistrationToken string `gorm:"type:varchar(64);not null"`
}

func (IngressReceipt) TableName() string { return "follow_projection_receipts" }

type RelationRow struct{ ID, FollowerID uint64 }
type TweetRow struct {
	ID          uint64
	VisibleType int
	DeletedAt   int64
}
type RelationPage struct {
	Rows []RelationRow
	Next uint64
	More bool
}
type TweetPage struct {
	Rows []TweetRow
	Next uint64
	More bool
}
type Fence struct {
	Epoch uint64
	Token string
	Until int64
}
type Store interface {
	Register(context.Context, events.FollowRelationEvent) (bool, error)
	WithAuthorLock(context.Context, uint64, func(context.Context) error) error
	Owns(context.Context, *domain.OutboxTask) (bool, error)
	ReadPair(context.Context, uint64, uint64) (PairFact, error)
	ReadAuthor(context.Context, uint64) (AuthorState, error)
	SaveAuthor(context.Context, AuthorState) error
	FollowerCount(context.Context, uint64) (int64, error)
	HistoryFollowers(context.Context, uint64, uint64) (RelationPage, error)
	ActiveFollowers(context.Context, uint64, []uint64) (map[uint64]bool, error)
	HistoryTweets(context.Context, uint64, uint64) (TweetPage, error)
	Advance(context.Context, *domain.OutboxTask, Checkpoint, bool) (bool, error)
	Interrupt(context.Context, *domain.OutboxTask) (bool, error)
}
type Cache interface {
	Celebrity(context.Context, uint64) (bool, error)
	Begin(context.Context, uint64, AuthorState, Fence) error
	Follower(context.Context, uint64, uint64, bool, Fence) error
	PairTweets(context.Context, uint64, uint64, bool, []TweetRow, Fence) error
}
type Observer interface {
	Observe(string, string)
	Duration(string, float64)
}
type Engine struct {
	Store    Store
	Cache    Cache
	Observer Observer
}
