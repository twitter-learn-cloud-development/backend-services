package environment

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	ExternalMCPEnvironmentName = "external.mcp.v1"
	externalMCPSnapshotSchema  = "agent.environment.external_mcp.catalog.v1"
	maxExternalMCPTools        = 64
	maxExternalMCPIdentitySize = 256

	externalMCPScopeUser    = "user"
	externalMCPScopeProject = "project"
)

// ExternalMCPToolBinding is a credential-free projection of one currently
// authorized tool bound to an active, immutable schema snapshot and mutable
// tool policy. The owning MCP adapter remains authoritative for tenant and
// project membership checks.
type ExternalMCPToolBinding struct {
	Tool                agentRuntime.ToolDefinition
	ConnectionID        string
	ConnectionOwnerID   uint64
	ConnectionScope     string
	ConnectionRevision  int64
	ServerID            string
	SnapshotID          string
	SnapshotVersion     int64
	SchemaHash          string
	PolicySnapshotID    string
	PolicyToolName      string
	PolicyCategory      string
	PolicyQualifiedName string
	PolicyEnabled       bool
}

// ExternalMCPToolCatalog must resolve current connection access, active
// snapshot and tool policy on every call. It must never return credentials or
// endpoints through ExternalMCPToolBinding.
type ExternalMCPToolCatalog interface {
	ListExternalMCPTools(ctx context.Context, userID uint64) ([]ExternalMCPToolBinding, error)
}

// ExternalMCPEnvironment exposes the tenant's currently governed remote tools
// without owning a remote client or an executor.
type ExternalMCPEnvironment struct {
	catalog ExternalMCPToolCatalog
	userID  uint64
	now     func() time.Time
}

type ExternalMCPOption func(*ExternalMCPEnvironment) error

func WithExternalMCPClock(now func() time.Time) ExternalMCPOption {
	return func(environment *ExternalMCPEnvironment) error {
		if now == nil {
			return fmt.Errorf("external MCP environment clock is required")
		}
		environment.now = now
		return nil
	}
}

func NewExternalMCPEnvironment(
	catalog ExternalMCPToolCatalog,
	userID uint64,
	options ...ExternalMCPOption,
) (*ExternalMCPEnvironment, error) {
	if catalog == nil {
		return nil, fmt.Errorf("external MCP tool catalog is required")
	}
	if userID == 0 {
		return nil, fmt.Errorf("external MCP environment user is required")
	}
	environment := &ExternalMCPEnvironment{catalog: catalog, userID: userID, now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("external MCP environment option is required")
		}
		if err := option(environment); err != nil {
			return nil, err
		}
	}
	return environment, nil
}

func (environment *ExternalMCPEnvironment) Name() string {
	return ExternalMCPEnvironmentName
}

func (environment *ExternalMCPEnvironment) Tools(
	ctx context.Context,
	task agentRuntime.TaskSpec,
) ([]agentRuntime.ToolDefinition, error) {
	bindings, err := environment.bindings(ctx, task)
	if err != nil {
		return nil, err
	}
	tools := make([]agentRuntime.ToolDefinition, 0, len(bindings))
	for _, binding := range bindings {
		tools = append(tools, cloneExternalMCPToolDefinition(binding.Tool))
	}
	return tools, nil
}

func (environment *ExternalMCPEnvironment) Snapshot(
	ctx context.Context,
	request agentRuntime.SnapshotRequest,
) (agentRuntime.EnvironmentSnapshot, error) {
	if environment == nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("external MCP environment is not configured")
	}
	if request.Phase != agentRuntime.SnapshotPhaseBefore && request.Phase != agentRuntime.SnapshotPhaseAfter {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("unsupported external MCP snapshot phase %q", request.Phase)
	}
	if len(request.Scope) != 0 {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("external MCP catalog snapshot does not support resource scope")
	}

	bindings, err := environment.bindings(ctx, request.Task)
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, err
	}
	tools := make([]externalMCPToolSnapshotIdentity, 0, len(bindings))
	for _, binding := range bindings {
		tools = append(tools, externalMCPToolSnapshotIdentity{
			Name: binding.Tool.Name, Category: binding.Tool.Category,
			RequiresApproval:   binding.Tool.ApprovalRequired(),
			ConnectionRevision: binding.ConnectionRevision,
			BindingDigest:      externalMCPBindingDigest(binding),
		})
	}
	identity, err := json.Marshal(externalMCPSnapshotCatalog{
		Schema: externalMCPSnapshotSchema, Environment: ExternalMCPEnvironmentName,
		ActorDigest: externalMCPActorDigest(environment.userID), Tools: tools,
	})
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("encode external MCP catalog identity: %w", err)
	}
	digest := sha256Digest(identity)
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	metadata, err := json.Marshal(externalMCPSnapshotMetadata{
		Schema: externalMCPSnapshotSchema, Phase: request.Phase,
		ActorDigest: externalMCPActorDigest(environment.userID), ToolCount: len(tools), Tools: tools,
	})
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("encode external MCP catalog metadata: %w", err)
	}
	return agentRuntime.EnvironmentSnapshot{
		ID:          "external-mcp-catalog:" + hexDigest[:24],
		Environment: ExternalMCPEnvironmentName,
		CapturedAt:  environment.now().UTC(),
		Digest:      digest,
		Reference:   "agent-environment://external.mcp.v1/catalog/sha256/" + hexDigest,
		Metadata:    metadata,
	}, nil
}

func (environment *ExternalMCPEnvironment) bindings(
	ctx context.Context,
	task agentRuntime.TaskSpec,
) ([]ExternalMCPToolBinding, error) {
	if environment == nil || environment.catalog == nil || environment.userID == 0 {
		return nil, fmt.Errorf("external MCP environment is not configured")
	}
	if ctx == nil {
		return nil, fmt.Errorf("external MCP environment context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := task.Validate(); err != nil {
		return nil, fmt.Errorf("validate external MCP task: %w", err)
	}

	requested := make(map[string]struct{}, len(task.AllowedTools))
	for _, name := range task.AllowedTools {
		requested[name] = struct{}{}
	}
	bindings, err := environment.catalog.ListExternalMCPTools(ctx, environment.userID)
	if err != nil {
		return nil, fmt.Errorf("list external MCP tools: %w", err)
	}
	if len(bindings) > maxExternalMCPTools {
		return nil, fmt.Errorf("external MCP catalog exceeds %d tools", maxExternalMCPTools)
	}

	seenNames := make(map[string]struct{}, len(bindings))
	seenBindings := make(map[string]struct{}, len(bindings))
	available := make([]ExternalMCPToolBinding, 0, len(bindings))
	for _, candidate := range bindings {
		binding, normalizeErr := normalizeExternalMCPToolBinding(candidate, environment.userID)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		if _, exists := seenNames[binding.Tool.Name]; exists {
			return nil, fmt.Errorf("duplicate external MCP catalog tool %q", binding.Tool.Name)
		}
		seenNames[binding.Tool.Name] = struct{}{}
		bindingDigest := externalMCPBindingDigest(binding)
		if _, exists := seenBindings[bindingDigest]; exists {
			return nil, fmt.Errorf("duplicate external MCP binding for tool %q", binding.Tool.Name)
		}
		seenBindings[bindingDigest] = struct{}{}
		if _, allowed := requested[binding.Tool.Name]; !allowed {
			continue
		}
		available = append(available, binding)
	}
	sort.Slice(available, func(left, right int) bool {
		return available[left].Tool.Name < available[right].Tool.Name
	})
	return available, nil
}

func normalizeExternalMCPToolBinding(
	binding ExternalMCPToolBinding,
	actorUserID uint64,
) (ExternalMCPToolBinding, error) {
	binding.Tool.Name = strings.TrimSpace(binding.Tool.Name)
	binding.ConnectionID = strings.TrimSpace(binding.ConnectionID)
	binding.ConnectionScope = strings.ToLower(strings.TrimSpace(binding.ConnectionScope))
	binding.ServerID = strings.TrimSpace(binding.ServerID)
	binding.SnapshotID = strings.TrimSpace(binding.SnapshotID)
	binding.SchemaHash = strings.TrimSpace(binding.SchemaHash)
	binding.PolicySnapshotID = strings.TrimSpace(binding.PolicySnapshotID)
	binding.PolicyToolName = strings.TrimSpace(binding.PolicyToolName)
	binding.PolicyCategory = strings.ToLower(strings.TrimSpace(binding.PolicyCategory))
	binding.PolicyQualifiedName = strings.TrimSpace(binding.PolicyQualifiedName)

	for name, value := range map[string]string{
		"connection": binding.ConnectionID, "server": binding.ServerID, "snapshot": binding.SnapshotID,
		"policy snapshot": binding.PolicySnapshotID, "policy tool": binding.PolicyToolName,
		"policy qualified tool": binding.PolicyQualifiedName, "tool": binding.Tool.Name,
	} {
		if value == "" || len(value) > maxExternalMCPIdentitySize {
			return ExternalMCPToolBinding{}, fmt.Errorf("external MCP %s identity is invalid", name)
		}
	}
	if binding.ConnectionOwnerID == 0 {
		return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q connection owner is invalid", binding.Tool.Name)
	}
	if binding.ConnectionRevision < 1 {
		return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q connection revision is invalid", binding.Tool.Name)
	}
	switch binding.ConnectionScope {
	case externalMCPScopeUser:
		if binding.ConnectionOwnerID != actorUserID {
			return ExternalMCPToolBinding{}, fmt.Errorf("external MCP user-scoped tool %q owner does not match actor", binding.Tool.Name)
		}
	case externalMCPScopeProject:
	default:
		return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q connection scope is invalid", binding.Tool.Name)
	}
	if binding.SnapshotVersion < 1 || !validExternalMCPSHA256Hex(binding.SchemaHash) {
		return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q snapshot binding is invalid", binding.Tool.Name)
	}
	if !binding.PolicyEnabled || binding.PolicySnapshotID != binding.SnapshotID {
		return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q policy is not active for the snapshot", binding.Tool.Name)
	}
	if binding.PolicyQualifiedName != binding.Tool.Name ||
		binding.Tool.Name != binding.ServerID+"."+binding.PolicyToolName {
		return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q stable identity is invalid", binding.Tool.Name)
	}
	switch binding.PolicyCategory {
	case string(agentRuntime.ToolCategoryRead):
		if binding.Tool.Category != agentRuntime.ToolCategoryRead {
			return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q policy category does not match runtime category", binding.Tool.Name)
		}
		if binding.Tool.RequiresApproval {
			return ExternalMCPToolBinding{}, fmt.Errorf("external MCP read tool %q must not require approval", binding.Tool.Name)
		}
	case string(agentRuntime.ToolCategoryRisky), string(agentRuntime.ToolCategoryWrite):
		if string(binding.Tool.Category) != binding.PolicyCategory {
			return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q policy category does not match runtime category", binding.Tool.Name)
		}
		if !binding.Tool.RequiresApproval {
			return ExternalMCPToolBinding{}, fmt.Errorf("external MCP governed tool %q must require approval", binding.Tool.Name)
		}
	default:
		return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q category is invalid", binding.Tool.Name)
	}
	if len(binding.Tool.InputSchema) == 0 || !json.Valid(binding.Tool.InputSchema) {
		return ExternalMCPToolBinding{}, fmt.Errorf("external MCP tool %q input schema is invalid", binding.Tool.Name)
	}
	binding.Tool = cloneExternalMCPToolDefinition(binding.Tool)
	return binding, nil
}

func externalMCPBindingDigest(binding ExternalMCPToolBinding) string {
	identity, _ := json.Marshal(struct {
		ConnectionID        string                    `json:"connection_id"`
		ConnectionOwnerID   uint64                    `json:"connection_owner_id"`
		ConnectionScope     string                    `json:"connection_scope"`
		ConnectionRevision  int64                     `json:"connection_revision"`
		ServerID            string                    `json:"server_id"`
		SnapshotID          string                    `json:"snapshot_id"`
		SnapshotVersion     int64                     `json:"snapshot_version"`
		SchemaHash          string                    `json:"schema_hash"`
		PolicySnapshotID    string                    `json:"policy_snapshot_id"`
		PolicyToolName      string                    `json:"policy_tool_name"`
		PolicyCategory      string                    `json:"policy_category"`
		PolicyQualifiedName string                    `json:"policy_qualified_name"`
		Category            agentRuntime.ToolCategory `json:"category"`
	}{
		ConnectionID: binding.ConnectionID, ConnectionOwnerID: binding.ConnectionOwnerID,
		ConnectionScope: binding.ConnectionScope, ConnectionRevision: binding.ConnectionRevision,
		ServerID: binding.ServerID, SnapshotID: binding.SnapshotID,
		SnapshotVersion: binding.SnapshotVersion, SchemaHash: binding.SchemaHash,
		PolicySnapshotID: binding.PolicySnapshotID, PolicyToolName: binding.PolicyToolName,
		PolicyCategory: binding.PolicyCategory, PolicyQualifiedName: binding.PolicyQualifiedName,
		Category: binding.Tool.Category,
	})
	return sha256Digest(identity)
}

func validExternalMCPSHA256Hex(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func cloneExternalMCPToolDefinition(tool agentRuntime.ToolDefinition) agentRuntime.ToolDefinition {
	tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
	return tool
}

type externalMCPSnapshotCatalog struct {
	Schema      string                            `json:"schema"`
	Environment string                            `json:"environment"`
	ActorDigest string                            `json:"actor_digest"`
	Tools       []externalMCPToolSnapshotIdentity `json:"tools"`
}

type externalMCPToolSnapshotIdentity struct {
	Name               string                    `json:"name"`
	Category           agentRuntime.ToolCategory `json:"category"`
	RequiresApproval   bool                      `json:"requires_approval"`
	ConnectionRevision int64                     `json:"connection_revision"`
	BindingDigest      string                    `json:"binding_digest"`
}

type externalMCPSnapshotMetadata struct {
	Schema      string                            `json:"schema"`
	Phase       agentRuntime.SnapshotPhase        `json:"phase"`
	ActorDigest string                            `json:"actor_digest"`
	ToolCount   int                               `json:"tool_count"`
	Tools       []externalMCPToolSnapshotIdentity `json:"tools"`
}

type ExternalMCPSnapshotToolView struct {
	Name               string
	Category           agentRuntime.ToolCategory
	RequiresApproval   bool
	ConnectionRevision int64
	BindingDigest      string
}

type ExternalMCPSnapshotView struct {
	Tools []ExternalMCPSnapshotToolView
}

func DecodeExternalMCPSnapshot(
	snapshot *agentRuntime.EnvironmentSnapshot,
	phase agentRuntime.SnapshotPhase,
	userID uint64,
) (ExternalMCPSnapshotView, error) {
	if snapshot == nil || snapshot.Environment != ExternalMCPEnvironmentName || userID == 0 {
		return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP snapshot identity is invalid")
	}
	var metadata externalMCPSnapshotMetadata
	if len(snapshot.Metadata) == 0 || !json.Valid(snapshot.Metadata) || json.Unmarshal(snapshot.Metadata, &metadata) != nil {
		return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP snapshot metadata is invalid")
	}
	if metadata.Schema != externalMCPSnapshotSchema || metadata.Phase != phase ||
		metadata.ActorDigest != externalMCPActorDigest(userID) || metadata.ToolCount != len(metadata.Tools) ||
		len(metadata.Tools) > maxExternalMCPTools {
		return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP snapshot metadata binding is invalid")
	}
	views := make([]ExternalMCPSnapshotToolView, 0, len(metadata.Tools))
	seen := make(map[string]struct{}, len(metadata.Tools))
	previous := ""
	for _, tool := range metadata.Tools {
		if tool.Name == "" || tool.ConnectionRevision < 1 || !validExternalMCPDigest(tool.BindingDigest) {
			return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP snapshot tool binding is invalid")
		}
		if previous != "" && tool.Name <= previous {
			return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP snapshot tools are not stable")
		}
		previous = tool.Name
		if _, exists := seen[tool.Name]; exists {
			return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP snapshot contains duplicate tool %q", tool.Name)
		}
		seen[tool.Name] = struct{}{}
		switch tool.Category {
		case agentRuntime.ToolCategoryRead:
			if tool.RequiresApproval {
				return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP read snapshot approval binding is invalid")
			}
		case agentRuntime.ToolCategoryRisky, agentRuntime.ToolCategoryWrite:
			if !tool.RequiresApproval {
				return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP governed snapshot approval binding is invalid")
			}
		default:
			return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP snapshot category is invalid")
		}
		views = append(views, ExternalMCPSnapshotToolView{
			Name: tool.Name, Category: tool.Category, RequiresApproval: tool.RequiresApproval,
			ConnectionRevision: tool.ConnectionRevision, BindingDigest: tool.BindingDigest,
		})
	}
	identity, err := json.Marshal(externalMCPSnapshotCatalog{
		Schema: metadata.Schema, Environment: ExternalMCPEnvironmentName,
		ActorDigest: metadata.ActorDigest, Tools: metadata.Tools,
	})
	if err != nil {
		return ExternalMCPSnapshotView{}, fmt.Errorf("encode external MCP snapshot identity: %w", err)
	}
	digest := sha256Digest(identity)
	if digest != snapshot.Digest {
		return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP snapshot digest is invalid")
	}
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	if snapshot.ID != "external-mcp-catalog:"+hexDigest[:24] ||
		snapshot.Reference != "agent-environment://external.mcp.v1/catalog/sha256/"+hexDigest {
		return ExternalMCPSnapshotView{}, fmt.Errorf("external MCP snapshot reference binding is invalid")
	}
	return ExternalMCPSnapshotView{Tools: views}, nil
}

func externalMCPActorDigest(userID uint64) string {
	return sha256Digest([]byte(fmt.Sprintf("external-mcp-actor:%d", userID)))
}

func validExternalMCPDigest(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	return validExternalMCPSHA256Hex(strings.TrimPrefix(value, prefix))
}

var _ agentRuntime.Environment = (*ExternalMCPEnvironment)(nil)
