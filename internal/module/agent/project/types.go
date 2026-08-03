package project

import (
	"context"
	"errors"
	"time"
)

const (
	RoleOwner  = "owner"
	RoleEditor = "editor"
	RoleViewer = "viewer"

	PermissionUse               = "use"
	PermissionManageConnections = "manage_connections"
	PermissionManageMembers     = "manage_members"
)

var (
	ErrDisabled         = errors.New("Agent projects are disabled")
	ErrNotFound         = errors.New("Agent project not found")
	ErrAccessDenied     = errors.New("Agent project access denied")
	ErrRevisionConflict = errors.New("Agent project revision conflict")
	ErrMemberNotFound   = errors.New("Agent project member not found")
	ErrUserNotFound     = errors.New("Agent project member user not found")
)

// Member is embedded in the project document so membership changes and the
// project revision share one atomic CAS boundary.
type Member struct {
	UserID    uint64    `bson:"user_id" json:"user_id"`
	Role      string    `bson:"role" json:"role"`
	AddedBy   uint64    `bson:"added_by" json:"added_by"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

type Project struct {
	ID        string    `bson:"_id" json:"id"`
	Name      string    `bson:"name" json:"name"`
	OwnerID   uint64    `bson:"owner_id" json:"owner_id"`
	Members   []Member  `bson:"members" json:"members"`
	Revision  int64     `bson:"revision" json:"revision"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

type Store interface {
	CreateProject(ctx context.Context, project *Project) error
	GetProject(ctx context.Context, projectID string) (*Project, error)
	ListProjectsForUser(ctx context.Context, userID uint64, page, pageSize int) ([]*Project, int64, error)
	ListProjectIDsForUser(ctx context.Context, userID uint64, limit int) ([]string, error)
	ReplaceProjectMembers(ctx context.Context, project *Project, expectedRevision int64) error
}

// UserDirectory is deliberately narrow. Production uses User Service while
// tests and future enterprise directories can provide isolated adapters.
type UserDirectory interface {
	UserExists(ctx context.Context, userID uint64) (bool, error)
}

// AccessResolver is the only project dependency needed by MCP. It keeps the
// connector package independent from Mongo, gRPC and organization providers.
type AccessResolver interface {
	RequireAccess(ctx context.Context, userID uint64, projectID, permission string) error
	ListAccessibleProjectIDs(ctx context.Context, userID uint64) ([]string, error)
}
