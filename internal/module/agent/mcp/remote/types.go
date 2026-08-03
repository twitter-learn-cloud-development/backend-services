package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	ScopeUser    = "user"
	ScopeProject = "project"

	TransportStreamableHTTP = "streamable_http"
	TransportSSE            = "sse"

	AuthNone   = "none"
	AuthBearer = "bearer"

	CredentialSourceUser    = "user"
	CredentialSourceManaged = "managed"

	ConnectionStatusActive  = "active"
	ConnectionStatusRevoked = "revoked"

	HealthStatusUnknown   = "unknown"
	HealthStatusHealthy   = "healthy"
	HealthStatusDegraded  = "degraded"
	HealthStatusUnhealthy = "unhealthy"

	DiscoveryStatusUnchecked      = "unchecked"
	DiscoveryStatusReady          = "ready"
	DiscoveryStatusReviewRequired = "review_required"
	DiscoveryStatusFailed         = "failed"

	ToolCategoryRead  = "read"
	ToolCategoryWrite = "write"
	ToolCategoryRisky = "risky"

	// IdempotencyKeyArgumentMetaField is the opt-in MCP tool metadata extension
	// used by cooperating servers to name their required idempotency-key input.
	// The standard idempotentHint remains mandatory; this field makes the
	// server-side deduplication boundary explicit and inspectable.
	IdempotencyKeyArgumentMetaField = "io.twitter-clone/idempotency-key-argument"
)

var (
	ErrDisabled                = errors.New("external MCP connections are disabled")
	ErrConnectionNotFound      = errors.New("external MCP connection not found")
	ErrSnapshotNotFound        = errors.New("external MCP schema snapshot not found")
	ErrRevisionConflict        = errors.New("external MCP connection revision conflict")
	ErrProjectScopeDisabled    = errors.New("project-scoped MCP connections are not enabled")
	ErrProjectStoreUnavailable = errors.New("project-scoped MCP connection store is unavailable")
	ErrToolNotFound            = errors.New("external MCP tool not found")
	ErrToolDisabled            = errors.New("external MCP tool is disabled")
	ErrToolRiskBlocked         = errors.New("external MCP tool risk category does not match its reviewed schema")
	ErrToolWriteBlocked        = errors.New("external MCP write tools require an idempotency contract")
	ErrIdempotencyKeyRequired  = errors.New("external MCP write execution requires an idempotency key")
	ErrSnapshotMismatch        = errors.New("external MCP tool policy snapshot mismatch")
	ErrHealthLeaseLost         = errors.New("external MCP health-check lease is no longer owned")
	ErrClientPoolClosed        = errors.New("external MCP client pool is closed")
	ErrClientPoolSaturated     = errors.New("external MCP client pool is saturated")
	ErrConnectionInvalidated   = errors.New("external MCP client session identity is no longer valid")
)

// Connection is the persisted control-plane record. Encrypted credential
// fields are intentionally excluded from every public transport DTO.
type Connection struct {
	ID                       string       `bson:"_id" json:"id"`
	UserID                   uint64       `bson:"user_id" json:"user_id"`
	Scope                    string       `bson:"scope" json:"scope"`
	ProjectID                string       `bson:"project_id,omitempty" json:"project_id,omitempty"`
	ServerID                 string       `bson:"server_id" json:"server_id"`
	Name                     string       `bson:"name" json:"name"`
	Transport                string       `bson:"transport" json:"transport"`
	Endpoint                 string       `bson:"endpoint" json:"endpoint"`
	AuthType                 string       `bson:"auth_type" json:"auth_type"`
	CredentialSource         string       `bson:"credential_source" json:"credential_source"`
	ManagedCredentialRef     string       `bson:"managed_credential_ref,omitempty" json:"managed_credential_ref,omitempty"`
	ManagedCredentialVersion int64        `bson:"managed_credential_version,omitempty" json:"managed_credential_version,omitempty"`
	Status                   string       `bson:"status" json:"status"`
	HasSecret                bool         `bson:"has_secret" json:"has_secret"`
	EncryptionKeyID          string       `bson:"encryption_key_id,omitempty" json:"-"`
	SecretNonce              string       `bson:"secret_nonce,omitempty" json:"-"`
	EncryptedCredential      string       `bson:"encrypted_credential,omitempty" json:"-"`
	CredentialVersion        int64        `bson:"credential_version" json:"credential_version"`
	LatestSnapshotID         string       `bson:"latest_snapshot_id,omitempty" json:"latest_snapshot_id,omitempty"`
	PendingSnapshotID        string       `bson:"pending_snapshot_id,omitempty" json:"pending_snapshot_id,omitempty"`
	ActiveSnapshotID         string       `bson:"active_snapshot_id,omitempty" json:"active_snapshot_id,omitempty"`
	DiscoveryStatus          string       `bson:"discovery_status" json:"discovery_status"`
	LastErrorCode            string       `bson:"last_error_code,omitempty" json:"last_error_code,omitempty"`
	LastCheckedAt            time.Time    `bson:"last_checked_at,omitempty" json:"last_checked_at,omitempty"`
	HealthStatus             string       `bson:"health_status,omitempty" json:"health_status,omitempty"`
	HealthErrorCode          string       `bson:"health_error_code,omitempty" json:"health_error_code,omitempty"`
	HealthFailureCount       int64        `bson:"health_failure_count,omitempty" json:"health_failure_count,omitempty"`
	LastHealthCheckedAt      time.Time    `bson:"last_health_checked_at,omitempty" json:"last_health_checked_at,omitempty"`
	LastHealthyAt            time.Time    `bson:"last_healthy_at,omitempty" json:"last_healthy_at,omitempty"`
	NextHealthCheckAt        time.Time    `bson:"next_health_check_at,omitempty" json:"next_health_check_at,omitempty"`
	HealthLeaseOwner         string       `bson:"health_lease_owner,omitempty" json:"-"`
	HealthLeaseUntil         time.Time    `bson:"health_lease_until,omitempty" json:"-"`
	ToolPolicies             []ToolPolicy `bson:"tool_policies,omitempty" json:"-"`
	FirstActivatedAt         time.Time    `bson:"first_activated_at,omitempty" json:"first_activated_at,omitempty"`
	Revision                 int64        `bson:"revision" json:"revision"`
	CreatedAt                time.Time    `bson:"created_at" json:"created_at"`
	UpdatedAt                time.Time    `bson:"updated_at" json:"updated_at"`
}

// ToolSchema is a normalized, bounded copy of one remote tool contract.
// QualifiedName is stable for the lifetime of a connection.
type ToolSchema struct {
	Name                   string `bson:"name" json:"name"`
	QualifiedName          string `bson:"qualified_name" json:"qualified_name"`
	Description            string `bson:"description,omitempty" json:"description,omitempty"`
	InputSchemaJSON        string `bson:"input_schema_json" json:"input_schema_json"`
	OutputSchemaJSON       string `bson:"output_schema_json,omitempty" json:"output_schema_json,omitempty"`
	DeclaredReadOnly       bool   `bson:"declared_read_only" json:"declared_read_only"`
	DeclaredIdempotent     bool   `bson:"declared_idempotent" json:"declared_idempotent"`
	IdempotencyKeyArgument string `bson:"idempotency_key_argument,omitempty" json:"idempotency_key_argument,omitempty"`
}

// SupportsWriteIdempotency reports whether the immutable reviewed snapshot
// contains both halves of the write contract. The argument schema itself is
// validated during discovery before these fields are persisted.
func (schema ToolSchema) SupportsWriteIdempotency() bool {
	argument := strings.TrimSpace(schema.IdempotencyKeyArgument)
	if !schema.DeclaredIdempotent || argument == "" {
		return false
	}
	var contract struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(schema.InputSchemaJSON), &contract); err != nil {
		return false
	}
	property, exists := contract.Properties[argument]
	if !exists || property.Type != "string" {
		return false
	}
	for _, field := range contract.Required {
		if field == argument {
			return true
		}
	}
	return false
}

// DeriveRemoteIdempotencyKey prevents internal run, step and tool identifiers
// from being disclosed to a third-party MCP server while keeping retries for
// one logical execution stable.
func DeriveRemoteIdempotencyKey(executionKey string) (string, error) {
	executionKey = strings.TrimSpace(executionKey)
	if executionKey == "" {
		return "", ErrIdempotencyKeyRequired
	}
	digest := sha256.Sum256([]byte("external-mcp-write:v1:" + executionKey))
	return "tc_mcp_" + hex.EncodeToString(digest[:]), nil
}

// ToolPolicy is mutable connection policy bound to one immutable schema
// snapshot. The connection revision is the single CAS boundary for updates.
type ToolPolicy struct {
	SnapshotID    string    `bson:"snapshot_id" json:"snapshot_id"`
	ToolName      string    `bson:"tool_name" json:"tool_name"`
	QualifiedName string    `bson:"qualified_name" json:"qualified_name"`
	Category      string    `bson:"category" json:"category"`
	Enabled       bool      `bson:"enabled" json:"enabled"`
	UpdatedAt     time.Time `bson:"updated_at" json:"updated_at"`
}

// ToolView combines immutable schema and mutable policy without copying
// connection credentials into control-plane responses.
type ToolView struct {
	Schema ToolSchema `json:"schema"`
	Policy ToolPolicy `json:"policy"`
}

// ExecutableTool is the bounded runtime catalog view. It deliberately omits
// endpoint and credential material.
type ExecutableTool struct {
	ConnectionID          string     `json:"connection_id"`
	ConnectionName        string     `json:"connection_name"`
	ConnectionOwnerID     uint64     `json:"-"`
	ConnectionScope       string     `json:"-"`
	HealthStatus          string     `json:"health_status"`
	Transport             string     `json:"-"`
	ConnectionCreatedAt   time.Time  `json:"-"`
	ConnectionActivatedAt time.Time  `json:"-"`
	ServerID              string     `json:"server_id"`
	SnapshotID            string     `json:"snapshot_id"`
	SnapshotVersion       int64      `json:"snapshot_version"`
	SchemaHash            string     `json:"schema_hash"`
	Schema                ToolSchema `json:"schema"`
	Policy                ToolPolicy `json:"policy"`
}

// ExecutionBinding is returned only inside the repository/manager boundary
// to assemble a user catalog in a fixed number of storage queries.
type ExecutionBinding struct {
	Connection Connection
	Snapshot   ToolSchemaSnapshot
}

// ToolSchemaSnapshot is append-only. Approval changes only the connection's
// active pointer; it never mutates the reviewed schema document.
type ToolSchemaSnapshot struct {
	ID           string       `bson:"_id" json:"id"`
	ConnectionID string       `bson:"connection_id" json:"connection_id"`
	UserID       uint64       `bson:"user_id" json:"user_id"`
	ServerID     string       `bson:"server_id" json:"server_id"`
	SchemaHash   string       `bson:"schema_hash" json:"schema_hash"`
	Version      int64        `bson:"version" json:"version"`
	Tools        []ToolSchema `bson:"tools" json:"tools"`
	CreatedAt    time.Time    `bson:"created_at" json:"created_at"`
}

type ConnectionInput struct {
	Scope                string
	ProjectID            string
	Name                 string
	Transport            string
	Endpoint             string
	AuthType             string
	CredentialSource     string
	ManagedCredentialRef string
	BearerToken          string
}

type DiscoveryRequest struct {
	ConnectionID       string
	CredentialVersion  int64
	CredentialIdentity string
	Transport          string
	Endpoint           string
	BearerToken        string
}

// HealthCheckCompletion is written through an independent lease boundary.
// It intentionally does not carry a Connection revision because background
// health metadata must not conflict with user control-plane updates.
type HealthCheckCompletion struct {
	ConnectionID      string
	UserID            uint64
	LeaseOwner        string
	Outcome           string
	HealthStatus      string
	ErrorCode         string
	FailureCount      int64
	CheckedAt         time.Time
	LastHealthyAt     time.Time
	NextHealthCheckAt time.Time
}

type ToolPolicyInput struct {
	SnapshotID string
	ToolName   string
	Category   string
	Enabled    bool
}

type Discoverer interface {
	Discover(ctx context.Context, request DiscoveryRequest) ([]mcp.Tool, error)
}

type Caller interface {
	Call(
		ctx context.Context,
		request DiscoveryRequest,
		toolName string,
		arguments map[string]interface{},
	) (*mcp.CallToolResult, error)
}

type HealthProber interface {
	Ping(ctx context.Context, request DiscoveryRequest) error
}

type ConnectionInvalidator interface {
	Invalidate(request DiscoveryRequest)
}

type PoolMaintainer interface {
	Prune()
	Close() error
}

// HealthStore is kept separate from Store so existing control-plane fakes and
// alternate stores remain source-compatible when active health checks are off.
type HealthStore interface {
	ResetMCPConnectionHealth(ctx context.Context, connectionID string, userID uint64, nextCheckAt time.Time) error
	ClaimMCPConnectionsForHealth(
		ctx context.Context,
		owner string,
		now time.Time,
		leaseUntil time.Time,
		limit int,
	) ([]*Connection, error)
	CompleteMCPConnectionHealth(ctx context.Context, completion HealthCheckCompletion) error
}

// RemoteCallError keeps endpoint and protocol details out of model/user-facing
// error strings while preserving the cause for timeout and retry classification.
type RemoteCallError struct {
	Cause error
}

func (err *RemoteCallError) Error() string {
	return "external MCP call failed"
}

func (err *RemoteCallError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// Store is kept in the MCP boundary so the manager remains independent from
// Mongo and the Agent service compatibility repository.
type Store interface {
	CreateMCPConnection(ctx context.Context, connection *Connection) error
	UpdateMCPConnection(ctx context.Context, connection *Connection, expectedRevision int64) error
	ListMCPConnections(ctx context.Context, userID uint64, page, pageSize int) ([]*Connection, int64, error)
	GetMCPConnection(ctx context.Context, id string, userID uint64) (*Connection, error)
	GetMCPConnectionByServerID(ctx context.Context, serverID string, userID uint64) (*Connection, error)
	RevokeMCPConnection(ctx context.Context, id string, userID uint64, expectedRevision int64) error
	SaveMCPToolSnapshot(ctx context.Context, snapshot *ToolSchemaSnapshot) (*ToolSchemaSnapshot, error)
	GetMCPToolSnapshot(ctx context.Context, id, connectionID string, userID uint64) (*ToolSchemaSnapshot, error)
	ListMCPExecutionBindings(ctx context.Context, userID uint64, limit int) ([]ExecutionBinding, error)
}

// ProjectStore extends the legacy user-owned Store without forcing every
// isolated fake or alternate implementation to understand project access.
// Manager authorizes every returned record through project.AccessResolver.
type ProjectStore interface {
	ListMCPConnectionsByAccess(
		ctx context.Context,
		userID uint64,
		projectIDs []string,
		page, pageSize int,
	) ([]*Connection, int64, error)
	GetMCPConnectionByID(ctx context.Context, id string) (*Connection, error)
	GetMCPConnectionByServerIDUnscoped(ctx context.Context, serverID string) (*Connection, error)
	ListMCPExecutionBindingsByAccess(
		ctx context.Context,
		userID uint64,
		projectIDs []string,
		limit int,
	) ([]ExecutionBinding, error)
}
