package remote

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	agentCredential "twitter-clone/internal/module/agent/credential"
	agentModel "twitter-clone/internal/module/agent/model"
	agentProject "twitter-clone/internal/module/agent/project"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	maxConnectionNameRunes = 80
	maxEndpointBytes       = 2048
	maxBearerTokenBytes    = 16 * 1024
	maxToolCount           = 128
	maxToolNameBytes       = 96
	maxToolArgumentBytes   = 128
	maxToolDescription     = 4096
	maxToolSchemaBytes     = 64 * 1024
	maxSnapshotBytes       = 1024 * 1024
	maxCatalogConnections  = 20
	maxCatalogTools        = 64
	defaultDiscoveryTime   = 20 * time.Second
	defaultExecutionTime   = 20 * time.Second
)

var (
	toolNamePattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	projectIDPattern = regexp.MustCompile(`^agentproj_[a-f0-9]{32}$`)
)

type ManagerOption func(*Manager)

type Manager struct {
	store                     Store
	cipher                    agentCredential.SecretCipher
	endpointPolicy            *agentModel.EndpointPolicy
	discoverer                Discoverer
	caller                    Caller
	enabled                   bool
	projectScopeEnabled       bool
	projectAccess             agentProject.AccessResolver
	managedCredentialsEnabled bool
	managedCredentials        ManagedCredentialResolver
	discoveryTimeout          time.Duration
	executionTimeout          time.Duration
	healthStore               HealthStore
	healthProber              HealthProber
	healthConfig              HealthCheckConfig
	healthObserver            HealthObserver
	healthOwner               string
	healthMu                  sync.Mutex
	healthCancel              context.CancelFunc
	healthWG                  sync.WaitGroup
	healthStarted             bool
	closeOnce                 sync.Once
	now                       func() time.Time
	newID                     func(string) (string, error)
}

func WithEnabled(enabled bool) ManagerOption {
	return func(manager *Manager) { manager.enabled = enabled }
}

func WithDiscoveryTimeout(timeout time.Duration) ManagerOption {
	return func(manager *Manager) {
		if timeout > 0 {
			manager.discoveryTimeout = timeout
		}
	}
}

func WithExecutionTimeout(timeout time.Duration) ManagerOption {
	return func(manager *Manager) {
		if timeout > 0 {
			manager.executionTimeout = timeout
		}
	}
}

func WithCaller(caller Caller) ManagerOption {
	return func(manager *Manager) { manager.caller = caller }
}

func WithProjectScope(enabled bool, access agentProject.AccessResolver) ManagerOption {
	return func(manager *Manager) {
		manager.projectScopeEnabled = enabled
		manager.projectAccess = access
	}
}

func WithManagedCredentials(enabled bool, resolver ManagedCredentialResolver) ManagerOption {
	return func(manager *Manager) {
		manager.managedCredentialsEnabled = enabled
		manager.managedCredentials = resolver
	}
}

func WithHealthChecks(config HealthCheckConfig) ManagerOption {
	return func(manager *Manager) { manager.healthConfig = config }
}

func WithHealthObserver(observer HealthObserver) ManagerOption {
	return func(manager *Manager) { manager.healthObserver = observer }
}

func WithHealthProber(prober HealthProber) ManagerOption {
	return func(manager *Manager) { manager.healthProber = prober }
}

func WithHealthOwner(owner string) ManagerOption {
	return func(manager *Manager) { manager.healthOwner = strings.TrimSpace(owner) }
}

func NewManager(
	store Store,
	cipher agentCredential.SecretCipher,
	endpointPolicy *agentModel.EndpointPolicy,
	discoverer Discoverer,
	options ...ManagerOption,
) *Manager {
	if endpointPolicy == nil {
		endpointPolicy = agentModel.NewEndpointPolicy()
	}
	manager := &Manager{
		store: store, cipher: cipher, endpointPolicy: endpointPolicy, discoverer: discoverer,
		discoveryTimeout: defaultDiscoveryTime, executionTimeout: defaultExecutionTime,
		healthConfig: DefaultHealthCheckConfig(), now: time.Now, newID: randomID,
	}
	if healthStore, ok := store.(HealthStore); ok {
		manager.healthStore = healthStore
	}
	if caller, ok := discoverer.(Caller); ok {
		manager.caller = caller
	}
	if prober, ok := discoverer.(HealthProber); ok {
		manager.healthProber = prober
	}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
	}
	return manager
}

func (manager *Manager) CreateConnection(
	ctx context.Context,
	userID uint64,
	input ConnectionInput,
) (*Connection, error) {
	if manager == nil || manager.store == nil {
		return nil, errors.New("external MCP connection store is unavailable")
	}
	if userID == 0 {
		return nil, errors.New("user id is required")
	}
	normalized, err := manager.validateInput(input, true)
	if err != nil {
		return nil, err
	}
	if normalized.Scope == ScopeProject {
		if err := manager.requireProjectAccess(ctx, userID, normalized.ProjectID, agentProject.PermissionManageConnections); err != nil {
			return nil, err
		}
	}
	id, err := manager.newID("mcpconn")
	if err != nil {
		return nil, err
	}
	now := manager.now().UTC()
	connection := &Connection{
		ID: id, UserID: userID, Scope: normalized.Scope, ProjectID: normalized.ProjectID,
		ServerID: serverIDFromConnectionID(id),
		Name:     normalized.Name, Transport: normalized.Transport, Endpoint: normalized.Endpoint,
		AuthType: normalized.AuthType, CredentialSource: normalized.CredentialSource,
		ManagedCredentialRef: normalized.ManagedCredentialRef,
		Status:               ConnectionStatusActive, DiscoveryStatus: DiscoveryStatusUnchecked,
		HealthStatus: HealthStatusUnknown, NextHealthCheckAt: now,
		CredentialVersion: 1, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := manager.configureCredential(ctx, connection, normalized.BearerToken); err != nil {
		return nil, err
	}
	if err := manager.store.CreateMCPConnection(ctx, connection); err != nil {
		return nil, err
	}
	return connection, nil
}

func (manager *Manager) UpdateConnection(
	ctx context.Context,
	userID uint64,
	id string,
	expectedRevision int64,
	input ConnectionInput,
) (*Connection, error) {
	if expectedRevision <= 0 {
		return nil, errors.New("expected revision is required")
	}
	existing, err := manager.getConnectionForPermission(ctx, userID, id, agentProject.PermissionManageConnections)
	if err != nil {
		return nil, err
	}
	if existing.Status != ConnectionStatusActive {
		return nil, errors.New("external MCP connection is revoked")
	}
	if existing.Revision != expectedRevision {
		return nil, ErrRevisionConflict
	}
	previousRequest := connectionDiscoveryRequest(existing, resolvedConnectionCredential{})
	if strings.TrimSpace(input.CredentialSource) == "" {
		input.CredentialSource = normalizedCredentialSource(existing)
	}
	if strings.EqualFold(strings.TrimSpace(input.CredentialSource), CredentialSourceManaged) &&
		strings.TrimSpace(input.ManagedCredentialRef) == "" {
		input.ManagedCredentialRef = existing.ManagedCredentialRef
	}
	normalized, err := manager.validateInput(input, false)
	if err != nil {
		return nil, err
	}
	if normalized.Scope != normalizedConnectionScope(existing) || normalized.ProjectID != existing.ProjectID {
		return nil, errors.New("external MCP connection scope cannot be changed")
	}
	previousSource := normalizedCredentialSource(existing)
	previousManagedRef := existing.ManagedCredentialRef
	previousManagedVersion := existing.ManagedCredentialVersion
	previousAuthType := existing.AuthType
	endpointChanged := normalized.Transport != existing.Transport || normalized.Endpoint != existing.Endpoint
	existing.Name = normalized.Name
	existing.Transport = normalized.Transport
	existing.Endpoint = normalized.Endpoint
	existing.CredentialSource = normalized.CredentialSource
	existing.ManagedCredentialRef = normalized.ManagedCredentialRef

	switch normalized.AuthType {
	case AuthNone:
		if previousAuthType != AuthNone || existing.HasSecret || previousSource != CredentialSourceUser {
			existing.CredentialVersion++
		}
		existing.AuthType = AuthNone
		existing.HasSecret = false
		existing.CredentialSource = CredentialSourceUser
		existing.ManagedCredentialRef = ""
		existing.ManagedCredentialVersion = 0
		existing.EncryptionKeyID = ""
		existing.SecretNonce = ""
		existing.EncryptedCredential = ""
	case AuthBearer:
		existing.AuthType = AuthBearer
		switch normalized.CredentialSource {
		case CredentialSourceUser:
			existing.ManagedCredentialRef = ""
			existing.ManagedCredentialVersion = 0
			if normalized.BearerToken == "" {
				if previousSource != CredentialSourceUser || previousAuthType != AuthBearer || !existing.HasSecret {
					return nil, errors.New("bearer token is required when switching to a user credential")
				}
			} else {
				existing.CredentialVersion++
				if err := manager.sealCredential(existing, normalized.BearerToken); err != nil {
					return nil, err
				}
			}
		case CredentialSourceManaged:
			resolved, resolveErr := manager.resolveManagedCredential(ctx, existing)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if previousSource != CredentialSourceManaged || previousManagedRef != existing.ManagedCredentialRef ||
				previousManagedVersion != resolved.Version || previousAuthType != AuthBearer || endpointChanged {
				existing.CredentialVersion++
			}
			existing.HasSecret = true
			existing.ManagedCredentialVersion = resolved.Version
			existing.EncryptionKeyID = ""
			existing.SecretNonce = ""
			existing.EncryptedCredential = ""
		default:
			return nil, errors.New("external MCP credential source is invalid")
		}
	}
	credentialBindingChanged := previousSource != normalizedCredentialSource(existing) ||
		previousManagedRef != existing.ManagedCredentialRef ||
		previousManagedVersion != existing.ManagedCredentialVersion ||
		previousAuthType != existing.AuthType || normalized.BearerToken != ""
	if endpointChanged || credentialBindingChanged {
		existing.LatestSnapshotID = ""
		existing.PendingSnapshotID = ""
		existing.ActiveSnapshotID = ""
		existing.ToolPolicies = nil
	}
	existing.DiscoveryStatus = DiscoveryStatusUnchecked
	existing.LastErrorCode = ""
	existing.LastCheckedAt = time.Time{}
	if err := manager.store.UpdateMCPConnection(ctx, existing, expectedRevision); err != nil {
		return nil, err
	}
	currentRequest := connectionDiscoveryRequest(existing, resolvedConnectionCredential{})
	if clientPoolIdentity(previousRequest) != clientPoolIdentity(currentRequest) {
		manager.invalidate(previousRequest)
		manager.resetHealth(ctx, existing)
	}
	return existing, nil
}

func (manager *Manager) ListConnections(
	ctx context.Context,
	userID uint64,
	page, pageSize int,
) ([]*Connection, int64, error) {
	if manager == nil || manager.store == nil {
		return nil, 0, errors.New("external MCP connection store is unavailable")
	}
	if userID == 0 {
		return nil, 0, errors.New("user id is required")
	}
	if manager.projectScopeEnabled {
		projectStore, ok := manager.store.(ProjectStore)
		if !ok {
			return nil, 0, ErrProjectStoreUnavailable
		}
		projectIDs, err := manager.accessibleProjectIDs(ctx, userID)
		if err != nil {
			return nil, 0, err
		}
		return projectStore.ListMCPConnectionsByAccess(ctx, userID, projectIDs, page, pageSize)
	}
	return manager.store.ListMCPConnections(ctx, userID, page, pageSize)
}

func (manager *Manager) GetConnection(ctx context.Context, userID uint64, id string) (*Connection, error) {
	if manager == nil || manager.store == nil {
		return nil, errors.New("external MCP connection store is unavailable")
	}
	id = strings.TrimSpace(id)
	if userID == 0 || id == "" {
		return nil, ErrConnectionNotFound
	}
	return manager.getConnectionForPermission(ctx, userID, id, agentProject.PermissionUse)
}

func (manager *Manager) RevokeConnection(
	ctx context.Context,
	userID uint64,
	id string,
	expectedRevision int64,
) error {
	if expectedRevision <= 0 {
		return errors.New("expected revision is required")
	}
	connection, err := manager.getConnectionForPermission(ctx, userID, id, agentProject.PermissionManageConnections)
	if err != nil {
		return err
	}
	if connection.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	if err := manager.store.RevokeMCPConnection(ctx, id, connection.UserID, expectedRevision); err != nil {
		return err
	}
	manager.invalidate(connectionDiscoveryRequest(connection, resolvedConnectionCredential{}))
	return nil
}

func (manager *Manager) DiscoverTools(
	ctx context.Context,
	userID uint64,
	id string,
	expectedRevision int64,
) (*Connection, *ToolSchemaSnapshot, error) {
	if manager == nil || !manager.enabled {
		return nil, nil, ErrDisabled
	}
	if manager.discoverer == nil {
		return nil, nil, errors.New("external MCP discoverer is unavailable")
	}
	connection, err := manager.getConnectionForPermission(ctx, userID, id, agentProject.PermissionManageConnections)
	if err != nil {
		return nil, nil, err
	}
	if connection.Status != ConnectionStatusActive {
		return nil, nil, errors.New("external MCP connection is revoked")
	}
	if expectedRevision <= 0 || connection.Revision != expectedRevision {
		return nil, nil, ErrRevisionConflict
	}
	credential, err := manager.openCredential(ctx, connection)
	if err != nil {
		return nil, nil, err
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, manager.discoveryTimeout)
	defer cancel()
	tools, err := manager.discoverer.Discover(
		discoveryCtx,
		connectionDiscoveryRequest(connection, credential),
	)
	checkedAt := manager.now().UTC()
	if err != nil {
		connection.DiscoveryStatus = DiscoveryStatusFailed
		connection.LastErrorCode = discoveryErrorCode(err)
		connection.LastCheckedAt = checkedAt
		_ = manager.store.UpdateMCPConnection(ctx, connection, expectedRevision)
		return nil, nil, fmt.Errorf("external MCP discovery failed: %w", err)
	}
	normalized, schemaHash, err := normalizeTools(connection.ServerID, tools)
	if err != nil {
		connection.DiscoveryStatus = DiscoveryStatusFailed
		connection.LastErrorCode = "invalid_tool_schema"
		connection.LastCheckedAt = checkedAt
		_ = manager.store.UpdateMCPConnection(ctx, connection, expectedRevision)
		return nil, nil, err
	}
	snapshotID, err := manager.newID("mcpsnap")
	if err != nil {
		return nil, nil, err
	}
	snapshot, err := manager.store.SaveMCPToolSnapshot(ctx, &ToolSchemaSnapshot{
		ID: snapshotID, ConnectionID: connection.ID, UserID: connection.UserID,
		ServerID: connection.ServerID, SchemaHash: schemaHash, Version: checkedAt.UnixNano(),
		Tools: normalized, CreatedAt: checkedAt,
	})
	if err != nil {
		return nil, nil, err
	}
	connection.LatestSnapshotID = snapshot.ID
	connection.LastCheckedAt = checkedAt
	connection.LastErrorCode = ""
	if connection.ActiveSnapshotID == snapshot.ID {
		connection.PendingSnapshotID = ""
		connection.DiscoveryStatus = DiscoveryStatusReady
	} else {
		connection.PendingSnapshotID = snapshot.ID
		connection.DiscoveryStatus = DiscoveryStatusReviewRequired
	}
	if err := manager.store.UpdateMCPConnection(ctx, connection, expectedRevision); err != nil {
		return nil, nil, err
	}
	return connection, snapshot, nil
}

func (manager *Manager) ApproveSnapshot(
	ctx context.Context,
	userID uint64,
	connectionID string,
	snapshotID string,
	expectedRevision int64,
) (*Connection, *ToolSchemaSnapshot, error) {
	if manager == nil || !manager.enabled {
		return nil, nil, ErrDisabled
	}
	connection, err := manager.getConnectionForPermission(ctx, userID, connectionID, agentProject.PermissionManageConnections)
	if err != nil {
		return nil, nil, err
	}
	if expectedRevision <= 0 || connection.Revision != expectedRevision {
		return nil, nil, ErrRevisionConflict
	}
	if connection.Status != ConnectionStatusActive {
		return nil, nil, errors.New("external MCP connection is revoked")
	}
	if connection.PendingSnapshotID == "" || connection.PendingSnapshotID != snapshotID {
		return nil, nil, errors.New("only the latest pending MCP schema snapshot can be approved")
	}
	snapshot, err := manager.store.GetMCPToolSnapshot(ctx, snapshotID, connectionID, connection.UserID)
	if err != nil {
		return nil, nil, err
	}
	activeSnapshotChanged := connection.ActiveSnapshotID != snapshot.ID
	connection.ActiveSnapshotID = snapshot.ID
	connection.PendingSnapshotID = ""
	connection.DiscoveryStatus = DiscoveryStatusReady
	connection.LastErrorCode = ""
	if activeSnapshotChanged {
		connection.ToolPolicies = nil
	}
	if err := manager.store.UpdateMCPConnection(ctx, connection, expectedRevision); err != nil {
		return nil, nil, err
	}
	return connection, snapshot, nil
}

func (manager *Manager) ListTools(
	ctx context.Context,
	userID uint64,
	connectionID string,
) (*Connection, *ToolSchemaSnapshot, []ToolView, error) {
	connection, err := manager.getConnectionForPermission(ctx, userID, connectionID, agentProject.PermissionUse)
	if err != nil {
		return nil, nil, nil, err
	}
	if connection.ActiveSnapshotID == "" {
		return nil, nil, nil, errors.New("external MCP connection has no approved schema snapshot")
	}
	snapshot, err := manager.store.GetMCPToolSnapshot(
		ctx,
		connection.ActiveSnapshotID,
		connection.ID,
		connection.UserID,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	return connection, snapshot, toolViews(connection, snapshot), nil
}

func (manager *Manager) ConfigureTool(
	ctx context.Context,
	userID uint64,
	connectionID string,
	expectedRevision int64,
	input ToolPolicyInput,
) (*Connection, ToolView, error) {
	if manager == nil || !manager.enabled {
		return nil, ToolView{}, ErrDisabled
	}
	connection, err := manager.getConnectionForPermission(ctx, userID, connectionID, agentProject.PermissionManageConnections)
	if err != nil {
		return nil, ToolView{}, err
	}
	if connection.ActiveSnapshotID == "" {
		return nil, ToolView{}, errors.New("external MCP connection has no approved schema snapshot")
	}
	snapshot, err := manager.store.GetMCPToolSnapshot(
		ctx, connection.ActiveSnapshotID, connection.ID, connection.UserID,
	)
	if err != nil {
		return nil, ToolView{}, err
	}
	views := toolViews(connection, snapshot)
	if connection.Status != ConnectionStatusActive {
		return nil, ToolView{}, errors.New("external MCP connection is revoked")
	}
	if expectedRevision <= 0 || connection.Revision != expectedRevision {
		return nil, ToolView{}, ErrRevisionConflict
	}
	input.SnapshotID = strings.TrimSpace(input.SnapshotID)
	if input.SnapshotID == "" || input.SnapshotID != connection.ActiveSnapshotID || input.SnapshotID != snapshot.ID {
		return nil, ToolView{}, ErrSnapshotMismatch
	}
	input.ToolName = strings.TrimSpace(input.ToolName)
	input.Category = strings.ToLower(strings.TrimSpace(input.Category))
	if input.Category == "" {
		input.Category = ToolCategoryRisky
	}
	switch input.Category {
	case ToolCategoryRead, ToolCategoryWrite, ToolCategoryRisky:
	default:
		return nil, ToolView{}, errors.New("external MCP tool category must be read, write, or risky")
	}

	var selected ToolView
	found := false
	for _, view := range views {
		if input.ToolName == view.Schema.Name || input.ToolName == view.Schema.QualifiedName {
			selected = view
			found = true
			break
		}
	}
	if !found {
		return nil, ToolView{}, ErrToolNotFound
	}
	if input.Enabled {
		if connection.DiscoveryStatus != DiscoveryStatusReady {
			return nil, ToolView{}, errors.New("external MCP schema review is not current")
		}
		switch input.Category {
		case ToolCategoryRead:
			if !selected.Schema.DeclaredReadOnly {
				return nil, ToolView{}, ErrToolRiskBlocked
			}
		case ToolCategoryRisky:
			// Risky tools are executable only through a resumable workflow
			// approval boundary. They are never exposed to the unified Agent
			// runtime catalog.
		case ToolCategoryWrite:
			if !selected.Schema.SupportsWriteIdempotency() {
				return nil, ToolView{}, ErrToolWriteBlocked
			}
		default:
			return nil, ToolView{}, ErrToolRiskBlocked
		}
	}

	policy := ToolPolicy{
		SnapshotID: snapshot.ID, ToolName: selected.Schema.Name,
		QualifiedName: selected.Schema.QualifiedName, Category: input.Category,
		Enabled: input.Enabled, UpdatedAt: manager.now().UTC(),
	}
	if input.Enabled && connection.FirstActivatedAt.IsZero() {
		connection.FirstActivatedAt = policy.UpdatedAt
	}
	policies := make([]ToolPolicy, 0, len(connection.ToolPolicies)+1)
	replaced := false
	for _, existing := range connection.ToolPolicies {
		if existing.SnapshotID != snapshot.ID {
			continue
		}
		if existing.QualifiedName == policy.QualifiedName {
			policies = append(policies, policy)
			replaced = true
			continue
		}
		policies = append(policies, existing)
	}
	if !replaced {
		policies = append(policies, policy)
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].QualifiedName < policies[j].QualifiedName })
	connection.ToolPolicies = policies
	if err := manager.store.UpdateMCPConnection(ctx, connection, expectedRevision); err != nil {
		return nil, ToolView{}, err
	}
	selected.Policy = policy
	return connection, selected, nil
}

func (manager *Manager) ListExecutableTools(
	ctx context.Context,
	userID uint64,
) ([]ExecutableTool, error) {
	return manager.listGovernedTools(ctx, userID, true)
}

// ListGovernedTools returns the bounded tenant catalog including read, risky
// and write tools. Every returned write tool has an immutable, reviewed remote
// idempotency contract; callers still need an approval gate before execution.
func (manager *Manager) ListGovernedTools(
	ctx context.Context,
	userID uint64,
) ([]ExecutableTool, error) {
	return manager.listGovernedTools(ctx, userID, false)
}

func (manager *Manager) listGovernedTools(
	ctx context.Context,
	userID uint64,
	readOnly bool,
) ([]ExecutableTool, error) {
	if manager == nil || !manager.enabled || manager.caller == nil {
		return nil, ErrDisabled
	}
	if userID == 0 {
		return nil, errors.New("user id is required")
	}
	bindings, err := manager.executionBindingsForActor(ctx, userID, maxCatalogConnections)
	if err != nil {
		return nil, err
	}
	tools := make([]ExecutableTool, 0)
	for _, binding := range bindings {
		connection := binding.Connection
		snapshot := binding.Snapshot
		if connection.Status != ConnectionStatusActive ||
			connection.DiscoveryStatus != DiscoveryStatusReady ||
			connection.ActiveSnapshotID == "" ||
			connection.ActiveSnapshotID != snapshot.ID {
			continue
		}
		for _, view := range toolViews(&connection, &snapshot) {
			if !view.Policy.Enabled || !catalogToolAllowed(view, readOnly) {
				continue
			}
			tools = append(tools, ExecutableTool{
				ConnectionID: connection.ID, ConnectionName: connection.Name,
				ConnectionOwnerID: connection.UserID, ConnectionScope: normalizedConnectionScope(&connection),
				ConnectionRevision: connection.Revision,
				HealthStatus:       connection.HealthStatus, Transport: connection.Transport,
				ConnectionCreatedAt: connection.CreatedAt, ConnectionActivatedAt: connection.FirstActivatedAt,
				ServerID: connection.ServerID, SnapshotID: snapshot.ID,
				SnapshotVersion: snapshot.Version, SchemaHash: snapshot.SchemaHash,
				Schema: view.Schema, Policy: view.Policy,
			})
			if len(tools) >= maxCatalogTools {
				break
			}
		}
		if len(tools) >= maxCatalogTools {
			break
		}
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Schema.QualifiedName < tools[j].Schema.QualifiedName
	})
	return tools, nil
}

func catalogToolAllowed(view ToolView, readOnly bool) bool {
	switch view.Policy.Category {
	case ToolCategoryRead:
		return view.Schema.DeclaredReadOnly
	case ToolCategoryRisky:
		return !readOnly
	case ToolCategoryWrite:
		return !readOnly && view.Schema.SupportsWriteIdempotency()
	default:
		return false
	}
}

func (manager *Manager) CallTool(
	ctx context.Context,
	userID uint64,
	qualifiedName string,
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	if manager == nil || !manager.enabled || manager.caller == nil {
		return nil, ErrDisabled
	}
	connection, schema, _, err := manager.resolveExecutableTool(ctx, userID, qualifiedName)
	if err != nil {
		return nil, err
	}
	return manager.callResolvedTool(ctx, connection, schema, arguments)
}

// CallGovernedTool is the governed execution path. Risky tools require a
// durable approval boundary. Write tools additionally require the immutable
// reviewed idempotency contract and a platform-owned execution key.
func (manager *Manager) CallGovernedTool(
	ctx context.Context,
	userID uint64,
	qualifiedName string,
	arguments map[string]interface{},
	executionIdempotencyKey string,
) (*mcp.CallToolResult, error) {
	if manager == nil || !manager.enabled || manager.caller == nil {
		return nil, ErrDisabled
	}
	connection, schema, policy, err := manager.resolveGovernedTool(ctx, userID, qualifiedName)
	if err != nil {
		return nil, err
	}
	if policy.Category == ToolCategoryWrite {
		remoteKey, keyErr := DeriveRemoteIdempotencyKey(executionIdempotencyKey)
		if keyErr != nil {
			return nil, keyErr
		}
		arguments = cloneRemoteArguments(arguments)
		arguments[schema.IdempotencyKeyArgument] = remoteKey
	}
	return manager.callResolvedTool(ctx, connection, schema, arguments)
}

func (manager *Manager) callResolvedTool(
	ctx context.Context,
	connection *Connection,
	schema ToolSchema,
	arguments map[string]interface{},
) (*mcp.CallToolResult, error) {
	credential, err := manager.openCredential(ctx, connection)
	if err != nil {
		return nil, err
	}
	executionCtx, cancel := context.WithTimeout(ctx, manager.executionTimeout)
	defer cancel()
	result, err := manager.caller.Call(
		executionCtx,
		connectionDiscoveryRequest(connection, credential),
		schema.Name,
		arguments,
	)
	if err != nil {
		return nil, &RemoteCallError{Cause: err}
	}
	if result == nil {
		return nil, &RemoteCallError{Cause: errors.New("empty external MCP tool result")}
	}
	if result.IsError {
		return nil, errors.New("external MCP tool returned an error")
	}
	return result, nil
}

func (manager *Manager) GetExecutableTool(
	ctx context.Context,
	userID uint64,
	qualifiedName string,
) (ExecutableTool, error) {
	if manager == nil || !manager.enabled || manager.caller == nil {
		return ExecutableTool{}, ErrDisabled
	}
	connection, schema, policy, err := manager.resolveExecutableTool(ctx, userID, qualifiedName)
	if err != nil {
		return ExecutableTool{}, err
	}
	return ExecutableTool{
		ConnectionID:          connection.ID,
		ConnectionName:        connection.Name,
		ConnectionOwnerID:     connection.UserID,
		ConnectionScope:       normalizedConnectionScope(connection),
		ConnectionRevision:    connection.Revision,
		HealthStatus:          connection.HealthStatus,
		Transport:             connection.Transport,
		ConnectionCreatedAt:   connection.CreatedAt,
		ConnectionActivatedAt: connection.FirstActivatedAt,
		ServerID:              connection.ServerID,
		SnapshotID:            connection.ActiveSnapshotID,
		Schema:                schema,
		Policy:                policy,
	}, nil
}

// GetGovernedTool resolves the active, tenant-scoped policy used by Workflow
// nodes. The returned view contains no endpoint or credential material.
func (manager *Manager) GetGovernedTool(
	ctx context.Context,
	userID uint64,
	qualifiedName string,
) (ExecutableTool, error) {
	if manager == nil || !manager.enabled || manager.caller == nil {
		return ExecutableTool{}, ErrDisabled
	}
	connection, schema, policy, err := manager.resolveGovernedTool(ctx, userID, qualifiedName)
	if err != nil {
		return ExecutableTool{}, err
	}
	return ExecutableTool{
		ConnectionID:          connection.ID,
		ConnectionName:        connection.Name,
		ConnectionOwnerID:     connection.UserID,
		ConnectionScope:       normalizedConnectionScope(connection),
		ConnectionRevision:    connection.Revision,
		HealthStatus:          connection.HealthStatus,
		Transport:             connection.Transport,
		ConnectionCreatedAt:   connection.CreatedAt,
		ConnectionActivatedAt: connection.FirstActivatedAt,
		ServerID:              connection.ServerID,
		SnapshotID:            connection.ActiveSnapshotID,
		Schema:                schema,
		Policy:                policy,
	}, nil
}

func (manager *Manager) resolveExecutableTool(
	ctx context.Context,
	userID uint64,
	qualifiedName string,
) (*Connection, ToolSchema, ToolPolicy, error) {
	connection, schema, policy, err := manager.resolveGovernedTool(ctx, userID, qualifiedName)
	if err != nil {
		return nil, ToolSchema{}, ToolPolicy{}, err
	}
	if !schema.DeclaredReadOnly || policy.Category != ToolCategoryRead {
		return nil, ToolSchema{}, ToolPolicy{}, ErrToolDisabled
	}
	return connection, schema, policy, nil
}

func (manager *Manager) resolveGovernedTool(
	ctx context.Context,
	userID uint64,
	qualifiedName string,
) (*Connection, ToolSchema, ToolPolicy, error) {
	serverID, toolName, ok := splitQualifiedToolName(qualifiedName)
	if userID == 0 || !ok {
		return nil, ToolSchema{}, ToolPolicy{}, ErrToolNotFound
	}
	connection, err := manager.getConnectionByServerIDForPermission(ctx, userID, serverID, agentProject.PermissionUse)
	if err != nil {
		return nil, ToolSchema{}, ToolPolicy{}, err
	}
	if connection.Status != ConnectionStatusActive ||
		connection.DiscoveryStatus != DiscoveryStatusReady ||
		connection.ActiveSnapshotID == "" {
		return nil, ToolSchema{}, ToolPolicy{}, ErrToolDisabled
	}
	snapshot, err := manager.store.GetMCPToolSnapshot(
		ctx,
		connection.ActiveSnapshotID,
		connection.ID,
		connection.UserID,
	)
	if err != nil {
		return nil, ToolSchema{}, ToolPolicy{}, err
	}
	for _, view := range toolViews(connection, snapshot) {
		if view.Schema.Name != toolName || view.Schema.QualifiedName != qualifiedName {
			continue
		}
		if !view.Policy.Enabled {
			return nil, ToolSchema{}, ToolPolicy{}, ErrToolDisabled
		}
		switch view.Policy.Category {
		case ToolCategoryRead:
			if !view.Schema.DeclaredReadOnly {
				return nil, ToolSchema{}, ToolPolicy{}, ErrToolRiskBlocked
			}
		case ToolCategoryRisky:
		case ToolCategoryWrite:
			if !view.Schema.SupportsWriteIdempotency() {
				return nil, ToolSchema{}, ToolPolicy{}, ErrToolWriteBlocked
			}
		default:
			return nil, ToolSchema{}, ToolPolicy{}, ErrToolDisabled
		}
		return connection, view.Schema, view.Policy, nil
	}
	return nil, ToolSchema{}, ToolPolicy{}, ErrToolNotFound
}

func toolViews(connection *Connection, snapshot *ToolSchemaSnapshot) []ToolView {
	if connection == nil || snapshot == nil {
		return nil
	}
	policies := make(map[string]ToolPolicy, len(connection.ToolPolicies))
	for _, policy := range connection.ToolPolicies {
		if policy.SnapshotID == snapshot.ID {
			policies[policy.QualifiedName] = policy
		}
	}
	views := make([]ToolView, 0, len(snapshot.Tools))
	for _, schema := range snapshot.Tools {
		policy, exists := policies[schema.QualifiedName]
		if !exists {
			policy = ToolPolicy{
				SnapshotID: snapshot.ID, ToolName: schema.Name,
				QualifiedName: schema.QualifiedName, Category: ToolCategoryRisky,
			}
		}
		views = append(views, ToolView{Schema: schema, Policy: policy})
	}
	return views
}

func splitQualifiedToolName(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	separator := strings.IndexByte(value, '.')
	if separator <= 0 || separator == len(value)-1 {
		return "", "", false
	}
	serverID := value[:separator]
	toolName := value[separator+1:]
	if !strings.HasPrefix(serverID, "mcp_") || !toolNamePattern.MatchString(toolName) {
		return "", "", false
	}
	return serverID, toolName, true
}

// IsQualifiedToolName allows the Workflow compiler to recognize dynamic MCP
// nodes without querying connection state while a DSL is being saved.
func IsQualifiedToolName(value string) bool {
	_, _, ok := splitQualifiedToolName(value)
	return ok
}

func (manager *Manager) validateInput(input ConnectionInput, creating bool) (ConnectionInput, error) {
	input.Scope = strings.ToLower(strings.TrimSpace(input.Scope))
	if input.Scope == "" {
		input.Scope = ScopeUser
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	switch input.Scope {
	case ScopeUser:
		if input.ProjectID != "" {
			return input, errors.New("project id is not valid for a user-scoped MCP connection")
		}
	case ScopeProject:
		if !manager.projectScopeEnabled || manager.projectAccess == nil {
			return input, ErrProjectScopeDisabled
		}
		if !projectIDPattern.MatchString(input.ProjectID) {
			return input, errors.New("a valid Agent project id is required for project-scoped MCP connections")
		}
	default:
		return input, errors.New("external MCP connection scope must be user or project")
	}
	input.Name = strings.TrimSpace(input.Name)
	if utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > maxConnectionNameRunes {
		return input, errors.New("external MCP connection name must contain 1-80 characters")
	}
	input.Transport = strings.ToLower(strings.TrimSpace(input.Transport))
	if input.Transport != TransportStreamableHTTP && input.Transport != TransportSSE {
		return input, errors.New("external MCP transport must be streamable_http or sse")
	}
	input.Endpoint = strings.TrimSpace(input.Endpoint)
	if input.Endpoint == "" || len(input.Endpoint) > maxEndpointBytes {
		return input, errors.New("external MCP endpoint must contain 1-2048 bytes")
	}
	if err := manager.endpointPolicy.Validate(input.Endpoint, "external-mcp"); err != nil {
		return input, err
	}
	input.AuthType = strings.ToLower(strings.TrimSpace(input.AuthType))
	if input.AuthType == "" {
		input.AuthType = AuthNone
	}
	if input.AuthType != AuthNone && input.AuthType != AuthBearer {
		return input, errors.New("external MCP auth type must be none or bearer")
	}
	input.CredentialSource = strings.ToLower(strings.TrimSpace(input.CredentialSource))
	if input.CredentialSource == "" {
		input.CredentialSource = CredentialSourceUser
	}
	input.ManagedCredentialRef = strings.TrimSpace(input.ManagedCredentialRef)
	input.BearerToken = strings.TrimSpace(input.BearerToken)
	if strings.ContainsAny(input.BearerToken, "\r\n") || len(input.BearerToken) > maxBearerTokenBytes {
		return input, errors.New("external MCP bearer token is invalid")
	}
	if input.AuthType == AuthNone && input.BearerToken != "" {
		return input, errors.New("bearer token requires bearer auth type")
	}
	switch input.CredentialSource {
	case CredentialSourceUser:
		if input.ManagedCredentialRef != "" {
			return input, errors.New("managed credential reference requires managed credential source")
		}
	case CredentialSourceManaged:
		if !manager.managedCredentialsEnabled || manager.managedCredentials == nil {
			return input, ErrManagedCredentialsDisabled
		}
		if input.Scope != ScopeProject {
			return input, errors.New("managed external MCP credentials require project scope")
		}
		if input.AuthType != AuthBearer {
			return input, errors.New("managed external MCP credentials require bearer auth")
		}
		if input.BearerToken != "" {
			return input, errors.New("bearer token must not be sent with a managed credential reference")
		}
		if !managedCredentialReferencePattern.MatchString(input.ManagedCredentialRef) {
			return input, errors.New("managed external MCP credential reference is invalid")
		}
	default:
		return input, errors.New("external MCP credential source must be user or managed")
	}
	parsedEndpoint, _ := url.Parse(input.Endpoint)
	if input.AuthType == AuthBearer && parsedEndpoint.Scheme != "https" {
		return input, errors.New("bearer-authenticated external MCP endpoints must use https")
	}
	if creating && input.AuthType == AuthBearer && input.CredentialSource == CredentialSourceUser && input.BearerToken == "" {
		return input, errors.New("bearer token is required")
	}
	return input, nil
}

func (manager *Manager) getConnectionForPermission(
	ctx context.Context,
	userID uint64,
	id string,
	permission string,
) (*Connection, error) {
	if userID == 0 || strings.TrimSpace(id) == "" {
		return nil, ErrConnectionNotFound
	}
	if manager.projectScopeEnabled {
		projectStore, ok := manager.store.(ProjectStore)
		if !ok {
			return nil, ErrProjectStoreUnavailable
		}
		connection, err := projectStore.GetMCPConnectionByID(ctx, strings.TrimSpace(id))
		if err != nil {
			return nil, err
		}
		if err := manager.authorizeConnection(ctx, userID, connection, permission); err != nil {
			return nil, err
		}
		return connection, nil
	}
	connection, err := manager.store.GetMCPConnection(ctx, strings.TrimSpace(id), userID)
	if err != nil {
		return nil, err
	}
	if normalizedConnectionScope(connection) == ScopeProject {
		return nil, ErrProjectScopeDisabled
	}
	return connection, nil
}

func (manager *Manager) getConnectionByServerIDForPermission(
	ctx context.Context,
	userID uint64,
	serverID string,
	permission string,
) (*Connection, error) {
	if manager.projectScopeEnabled {
		projectStore, ok := manager.store.(ProjectStore)
		if !ok {
			return nil, ErrProjectStoreUnavailable
		}
		connection, err := projectStore.GetMCPConnectionByServerIDUnscoped(ctx, serverID)
		if err != nil {
			return nil, err
		}
		if err := manager.authorizeConnection(ctx, userID, connection, permission); err != nil {
			return nil, err
		}
		return connection, nil
	}
	connection, err := manager.store.GetMCPConnectionByServerID(ctx, serverID, userID)
	if err != nil {
		return nil, err
	}
	if normalizedConnectionScope(connection) == ScopeProject {
		return nil, ErrProjectScopeDisabled
	}
	return connection, nil
}

func (manager *Manager) authorizeConnection(
	ctx context.Context,
	userID uint64,
	connection *Connection,
	permission string,
) error {
	if connection == nil {
		return ErrConnectionNotFound
	}
	switch normalizedConnectionScope(connection) {
	case ScopeUser:
		if connection.UserID != userID {
			return ErrConnectionNotFound
		}
		return nil
	case ScopeProject:
		return manager.requireProjectAccess(ctx, userID, connection.ProjectID, permission)
	default:
		return ErrConnectionNotFound
	}
}

func (manager *Manager) requireProjectAccess(
	ctx context.Context,
	userID uint64,
	projectID string,
	permission string,
) error {
	if !manager.projectScopeEnabled || manager.projectAccess == nil {
		return ErrProjectScopeDisabled
	}
	return manager.projectAccess.RequireAccess(ctx, userID, projectID, permission)
}

func (manager *Manager) accessibleProjectIDs(ctx context.Context, userID uint64) ([]string, error) {
	if !manager.projectScopeEnabled || manager.projectAccess == nil {
		return nil, ErrProjectScopeDisabled
	}
	return manager.projectAccess.ListAccessibleProjectIDs(ctx, userID)
}

func (manager *Manager) executionBindingsForActor(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]ExecutionBinding, error) {
	if !manager.projectScopeEnabled {
		return manager.store.ListMCPExecutionBindings(ctx, userID, limit)
	}
	projectStore, ok := manager.store.(ProjectStore)
	if !ok {
		return nil, ErrProjectStoreUnavailable
	}
	projectIDs, err := manager.accessibleProjectIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	return projectStore.ListMCPExecutionBindingsByAccess(ctx, userID, projectIDs, limit)
}

func normalizedConnectionScope(connection *Connection) string {
	if connection == nil {
		return ""
	}
	if strings.TrimSpace(connection.Scope) == "" {
		return ScopeUser
	}
	return strings.ToLower(strings.TrimSpace(connection.Scope))
}

func normalizedCredentialSource(connection *Connection) string {
	if connection == nil || strings.TrimSpace(connection.CredentialSource) == "" {
		return CredentialSourceUser
	}
	return strings.ToLower(strings.TrimSpace(connection.CredentialSource))
}

type resolvedConnectionCredential struct {
	bearerToken string
	identity    string
}

func (manager *Manager) configureCredential(
	ctx context.Context,
	connection *Connection,
	bearerToken string,
) error {
	switch normalizedCredentialSource(connection) {
	case CredentialSourceUser:
		connection.ManagedCredentialRef = ""
		connection.ManagedCredentialVersion = 0
		return manager.sealCredential(connection, bearerToken)
	case CredentialSourceManaged:
		resolved, err := manager.resolveManagedCredential(ctx, connection)
		if err != nil {
			return err
		}
		connection.HasSecret = true
		connection.ManagedCredentialVersion = resolved.Version
		connection.EncryptionKeyID = ""
		connection.SecretNonce = ""
		connection.EncryptedCredential = ""
		return nil
	default:
		return errors.New("external MCP credential source is invalid")
	}
}

func (manager *Manager) sealCredential(connection *Connection, bearerToken string) error {
	if connection.AuthType == AuthNone {
		connection.HasSecret = false
		return nil
	}
	if manager.cipher == nil {
		return agentCredential.ErrSecretCipherUnavailable
	}
	secret, err := manager.cipher.Encrypt([]byte(bearerToken), credentialAAD(connection))
	if err != nil {
		return err
	}
	connection.HasSecret = true
	connection.EncryptionKeyID = secret.KeyID
	connection.SecretNonce = secret.Nonce
	connection.EncryptedCredential = secret.Ciphertext
	return nil
}

func (manager *Manager) openCredential(
	ctx context.Context,
	connection *Connection,
) (resolvedConnectionCredential, error) {
	if connection.AuthType == AuthNone {
		return resolvedConnectionCredential{identity: fmt.Sprintf("none:%d", connection.CredentialVersion)}, nil
	}
	if normalizedCredentialSource(connection) == CredentialSourceManaged {
		resolved, err := manager.resolveManagedCredential(ctx, connection)
		if err != nil {
			return resolvedConnectionCredential{}, err
		}
		if connection.ManagedCredentialVersion <= 0 || connection.ManagedCredentialVersion != resolved.Version {
			return resolvedConnectionCredential{}, ErrManagedCredentialBinding
		}
		return resolvedConnectionCredential{
			bearerToken: resolved.BearerToken,
			identity:    fmt.Sprintf("managed:%d:%d:%s", connection.CredentialVersion, resolved.Version, resolved.Identity),
		}, nil
	}
	if !connection.HasSecret || manager.cipher == nil {
		return resolvedConnectionCredential{}, agentCredential.ErrSecretCipherUnavailable
	}
	plaintext, err := manager.cipher.Decrypt(agentCredential.EncryptedSecret{
		KeyID: connection.EncryptionKeyID, Nonce: connection.SecretNonce,
		Ciphertext: connection.EncryptedCredential,
	}, credentialAAD(connection))
	if err != nil {
		return resolvedConnectionCredential{}, err
	}
	return resolvedConnectionCredential{
		bearerToken: string(plaintext),
		identity:    fmt.Sprintf("user:%d", connection.CredentialVersion),
	}, nil
}

func (manager *Manager) resolveManagedCredential(
	ctx context.Context,
	connection *Connection,
) (ResolvedManagedCredential, error) {
	if !manager.managedCredentialsEnabled || manager.managedCredentials == nil {
		return ResolvedManagedCredential{}, ErrManagedCredentialsDisabled
	}
	if connection == nil || normalizedConnectionScope(connection) != ScopeProject ||
		connection.AuthType != AuthBearer || strings.TrimSpace(connection.ManagedCredentialRef) == "" {
		return ResolvedManagedCredential{}, ErrManagedCredentialBinding
	}
	return manager.managedCredentials.Resolve(ctx, ManagedCredentialRequest{
		Reference: connection.ManagedCredentialRef,
		ProjectID: connection.ProjectID,
		Endpoint:  connection.Endpoint,
		AuthType:  connection.AuthType,
	})
}

func credentialAAD(connection *Connection) []byte {
	return []byte(fmt.Sprintf("external-mcp|%s|%d|%s|%s|%d", connection.ID, connection.UserID,
		connection.CredentialSource, connection.AuthType, connection.CredentialVersion))
}

func normalizeTools(serverID string, tools []mcp.Tool) ([]ToolSchema, string, error) {
	if len(tools) == 0 {
		return nil, "", errors.New("external MCP server returned no tools")
	}
	if len(tools) > maxToolCount {
		return nil, "", fmt.Errorf("external MCP server returned %d tools; maximum is %d", len(tools), maxToolCount)
	}
	normalized := make([]ToolSchema, 0, len(tools))
	seen := make(map[string]struct{}, len(tools))
	totalBytes := 0
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" || len(name) > maxToolNameBytes || !toolNamePattern.MatchString(name) {
			return nil, "", fmt.Errorf("external MCP tool name %q is invalid", name)
		}
		if _, exists := seen[name]; exists {
			return nil, "", fmt.Errorf("external MCP tool name %q is duplicated", name)
		}
		seen[name] = struct{}{}
		description := strings.TrimSpace(tool.Description)
		if len(description) > maxToolDescription {
			return nil, "", fmt.Errorf("external MCP tool %q description is too large", name)
		}
		encoded, err := json.Marshal(tool)
		if err != nil {
			return nil, "", fmt.Errorf("encode external MCP tool %q: %w", name, err)
		}
		var wire struct {
			InputSchema  json.RawMessage `json:"inputSchema"`
			OutputSchema json.RawMessage `json:"outputSchema"`
		}
		if err := json.Unmarshal(encoded, &wire); err != nil {
			return nil, "", fmt.Errorf("decode external MCP tool %q schema: %w", name, err)
		}
		inputSchema, err := canonicalJSON(wire.InputSchema, true)
		if err != nil {
			return nil, "", fmt.Errorf("external MCP tool %q input schema is invalid: %w", name, err)
		}
		outputSchema, err := canonicalJSON(wire.OutputSchema, false)
		if err != nil {
			return nil, "", fmt.Errorf("external MCP tool %q output schema is invalid: %w", name, err)
		}
		if len(inputSchema) > maxToolSchemaBytes || len(outputSchema) > maxToolSchemaBytes {
			return nil, "", fmt.Errorf("external MCP tool %q schema is too large", name)
		}
		qualifiedName := serverID + "." + name
		declaredReadOnly := tool.Annotations.ReadOnlyHint != nil && *tool.Annotations.ReadOnlyHint &&
			(tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint)
		declaredIdempotent := tool.Annotations.IdempotentHint != nil && *tool.Annotations.IdempotentHint
		idempotencyKeyArgument, err := idempotencyKeyArgument(tool, inputSchema)
		if err != nil {
			return nil, "", fmt.Errorf("external MCP tool %q idempotency contract is invalid: %w", name, err)
		}
		item := ToolSchema{
			Name: name, QualifiedName: qualifiedName, Description: description,
			InputSchemaJSON: inputSchema, OutputSchemaJSON: outputSchema,
			DeclaredReadOnly: declaredReadOnly, DeclaredIdempotent: declaredIdempotent,
			IdempotencyKeyArgument: idempotencyKeyArgument,
		}
		totalBytes += len(name) + len(qualifiedName) + len(description) + len(inputSchema) + len(outputSchema)
		if totalBytes > maxSnapshotBytes {
			return nil, "", errors.New("external MCP tool schema snapshot is too large")
		}
		normalized = append(normalized, item)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Name < normalized[j].Name })
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(encoded)
	return normalized, hex.EncodeToString(digest[:]), nil
}

func idempotencyKeyArgument(tool mcp.Tool, inputSchema string) (string, error) {
	if tool.Meta == nil || tool.Meta.AdditionalFields == nil {
		return "", nil
	}
	value, exists := tool.Meta.AdditionalFields[IdempotencyKeyArgumentMetaField]
	if !exists {
		return "", nil
	}
	argument, ok := value.(string)
	argument = strings.TrimSpace(argument)
	if !ok || argument == "" || len(argument) > maxToolArgumentBytes || !toolNamePattern.MatchString(argument) {
		return "", errors.New("idempotency key argument metadata must be a valid 1-128 byte property name")
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(inputSchema), &schema); err != nil {
		return "", err
	}
	property, exists := schema.Properties[argument]
	if !exists || property.Type != "string" {
		return "", fmt.Errorf("input property %q must exist with type string", argument)
	}
	required := false
	for _, field := range schema.Required {
		if field == argument {
			required = true
			break
		}
	}
	if !required {
		return "", fmt.Errorf("input property %q must be required", argument)
	}
	return argument, nil
}

func cloneRemoteArguments(arguments map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(arguments)+1)
	for key, value := range arguments {
		cloned[key] = value
	}
	return cloned
}

func canonicalJSON(raw json.RawMessage, required bool) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		if required {
			return "{}", nil
		}
		return "", nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func discoveryErrorCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, agentModel.ErrEndpointNotAllowed):
		return "endpoint_not_allowed"
	default:
		return "connection_failed"
	}
}

func randomID(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate external MCP identifier: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(buffer), nil
}

func serverIDFromConnectionID(connectionID string) string {
	suffix := strings.TrimPrefix(connectionID, "mcpconn_")
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	return "mcp_" + suffix
}
