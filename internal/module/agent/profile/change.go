package profile

import (
	"context"
	"errors"
	"strings"
)

const (
	CatalogChangeSchemaV1 = "agent_profile_catalog_change_v1"

	ChangeChannelEnv            = "AGENT_PROFILE_CHANGE_CHANNEL"
	DefaultChangeChannel        = "agent.profile.catalog.changed.v1"
	SyncIntervalEnv             = "AGENT_PROFILE_SYNC_INTERVAL"
	AdminTokenEnv               = "AGENT_PROFILE_ADMIN_TOKEN"
	AdminUserIDsEnv             = "AGENT_PROFILE_ADMIN_USER_IDS"
	ViewerUserIDsEnv            = "AGENT_PROFILE_VIEWER_USER_IDS"
	EditorUserIDsEnv            = "AGENT_PROFILE_EDITOR_USER_IDS"
	ApproverUserIDsEnv          = "AGENT_PROFILE_APPROVER_USER_IDS"
	DirectPublishEnabledEnv     = "AGENT_PROFILE_DIRECT_PUBLISH_ENABLED"
	DynamicRBACEnabledEnv       = "AGENT_PROFILE_DYNAMIC_RBAC_ENABLED"
	ExperimentsEnabledEnv       = "AGENT_PROFILE_EXPERIMENTS_ENABLED"
	ExperimentIntervalEnv       = "AGENT_PROFILE_EXPERIMENT_RECONCILE_INTERVAL"
	ContentAttributionWindowEnv = "AGENT_PROFILE_CONTENT_ATTRIBUTION_WINDOW"
	AdminTokenMetadataKey       = "x-agent-profile-admin-token"
)

// CatalogChangeEvent is an invalidation hint. Receivers always rebuild the
// authoritative catalog from MongoDB instead of trusting event payload data.
type CatalogChangeEvent struct {
	Schema               string `json:"schema"`
	OperationID          string `json:"operation_id"`
	ProfileID            string `json:"profile_id"`
	VersionRevision      int64  `json:"version_revision,omitempty"`
	ReleaseRevision      int64  `json:"release_revision,omitempty"`
	OccurredAtUnixMillis int64  `json:"occurred_at_unix_millis"`
}

func (e CatalogChangeEvent) Validate() error {
	if strings.TrimSpace(e.Schema) != CatalogChangeSchemaV1 {
		return errors.New("unsupported profile catalog change schema")
	}
	if strings.TrimSpace(e.OperationID) == "" || strings.TrimSpace(e.ProfileID) == "" {
		return errors.New("profile catalog change identity is required")
	}
	if e.VersionRevision < 0 || e.ReleaseRevision < 0 || e.OccurredAtUnixMillis <= 0 {
		return errors.New("profile catalog change revisions and occurrence time are invalid")
	}
	return nil
}

type CatalogChangePublisher interface {
	PublishCatalogChange(ctx context.Context, event CatalogChangeEvent) error
}

type CatalogChangeSubscription interface {
	Events() <-chan CatalogChangeEvent
	Errors() <-chan error
	Close() error
}

type CatalogChangeBus interface {
	CatalogChangePublisher
	SubscribeCatalogChanges(ctx context.Context) (CatalogChangeSubscription, error)
}
