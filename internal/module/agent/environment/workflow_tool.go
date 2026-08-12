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
	WorkflowToolEnvironmentName = "workflow.tool.v1"
	workflowToolSnapshotSchema  = "agent.environment.workflow_tool.catalog.v1"
	defaultWorkflowToolLimit    = 20
	maxWorkflowToolLimit        = 100
)

// WorkflowToolBinding is a tenant-authorized projection of one active
// publication bound to an immutable workflow revision. The environment never
// receives the workflow DSL or execution credentials.
type WorkflowToolBinding struct {
	Tool                   agentRuntime.ToolDefinition
	PublicationID          string
	PublicationRevision    int64
	WorkflowID             string
	WorkflowRevisionID     string
	WorkflowRevisionNumber int64
	WorkflowDSLHash        string
}

// WorkflowToolBindingIdentity is the low-sensitivity immutable identity used
// to bind a Runtime observation to the exact published workflow revision.
type WorkflowToolBindingIdentity struct {
	PublicationID          string
	PublicationRevision    int64
	WorkflowID             string
	WorkflowRevisionID     string
	WorkflowRevisionNumber int64
	WorkflowDSLHash        string
}

// WorkflowToolCatalog resolves current tenant authorization and immutable
// publication bindings. Implementations must revalidate ownership and active
// publication state on every call.
type WorkflowToolCatalog interface {
	ListWorkflowTools(ctx context.Context, userID uint64, limit int) ([]WorkflowToolBinding, error)
}

// WorkflowToolEnvironment exposes published workflows as governed Runtime
// tools. Tool execution remains exclusively behind Runtime's ToolExecutor.
type WorkflowToolEnvironment struct {
	catalog WorkflowToolCatalog
	userID  uint64
	limit   int
	now     func() time.Time
}

type WorkflowToolOption func(*WorkflowToolEnvironment) error

func WithWorkflowToolClock(now func() time.Time) WorkflowToolOption {
	return func(environment *WorkflowToolEnvironment) error {
		if now == nil {
			return fmt.Errorf("workflow tool environment clock is required")
		}
		environment.now = now
		return nil
	}
}

func WithWorkflowToolLimit(limit int) WorkflowToolOption {
	return func(environment *WorkflowToolEnvironment) error {
		if limit < 1 || limit > maxWorkflowToolLimit {
			return fmt.Errorf("workflow tool catalog limit must be between 1 and %d", maxWorkflowToolLimit)
		}
		environment.limit = limit
		return nil
	}
}

func NewWorkflowToolEnvironment(
	catalog WorkflowToolCatalog,
	userID uint64,
	options ...WorkflowToolOption,
) (*WorkflowToolEnvironment, error) {
	if catalog == nil {
		return nil, fmt.Errorf("workflow tool catalog is required")
	}
	if userID == 0 {
		return nil, fmt.Errorf("workflow tool environment user is required")
	}
	environment := &WorkflowToolEnvironment{
		catalog: catalog,
		userID:  userID,
		limit:   defaultWorkflowToolLimit,
		now:     time.Now,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("workflow tool environment option is required")
		}
		if err := option(environment); err != nil {
			return nil, err
		}
	}
	return environment, nil
}

func (environment *WorkflowToolEnvironment) Name() string {
	return WorkflowToolEnvironmentName
}

func (environment *WorkflowToolEnvironment) Tools(
	ctx context.Context,
	task agentRuntime.TaskSpec,
) ([]agentRuntime.ToolDefinition, error) {
	bindings, err := environment.bindings(ctx, task)
	if err != nil {
		return nil, err
	}
	tools := make([]agentRuntime.ToolDefinition, 0, len(bindings))
	for _, binding := range bindings {
		tools = append(tools, cloneWorkflowToolDefinition(binding.Tool))
	}
	return tools, nil
}

func (environment *WorkflowToolEnvironment) Snapshot(
	ctx context.Context,
	request agentRuntime.SnapshotRequest,
) (agentRuntime.EnvironmentSnapshot, error) {
	if environment == nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("workflow tool environment is not configured")
	}
	if request.Phase != agentRuntime.SnapshotPhaseBefore && request.Phase != agentRuntime.SnapshotPhaseAfter {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("unsupported workflow tool snapshot phase %q", request.Phase)
	}
	if len(request.Scope) != 0 {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("workflow tool catalog snapshot does not support resource scope")
	}

	bindings, err := environment.bindings(ctx, request.Task)
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, err
	}
	tools := make([]workflowToolSnapshotIdentity, 0, len(bindings))
	for _, binding := range bindings {
		tools = append(tools, workflowToolSnapshotIdentity{
			Name:             binding.Tool.Name,
			Category:         binding.Tool.Category,
			RequiresApproval: binding.Tool.ApprovalRequired(),
			BindingDigest:    workflowBindingDigest(binding),
		})
	}
	actorDigest := workflowToolActorDigest(environment.userID)
	identity, err := json.Marshal(workflowToolSnapshotCatalog{
		Schema: workflowToolSnapshotSchema, Environment: WorkflowToolEnvironmentName,
		ActorDigest: actorDigest, Tools: tools,
	})
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("encode workflow tool catalog identity: %w", err)
	}
	digest := sha256Digest(identity)
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	metadata, err := json.Marshal(workflowToolSnapshotMetadata{
		Schema: workflowToolSnapshotSchema, Phase: request.Phase,
		ActorDigest: actorDigest, ToolCount: len(tools), Tools: tools,
	})
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("encode workflow tool catalog metadata: %w", err)
	}
	return agentRuntime.EnvironmentSnapshot{
		ID:          "workflow-tool-catalog:" + hexDigest[:24],
		Environment: WorkflowToolEnvironmentName,
		CapturedAt:  environment.now().UTC(),
		Digest:      digest,
		Reference:   "agent-environment://workflow.tool.v1/catalog/sha256/" + hexDigest,
		Metadata:    metadata,
	}, nil
}

func (environment *WorkflowToolEnvironment) bindings(
	ctx context.Context,
	task agentRuntime.TaskSpec,
) ([]WorkflowToolBinding, error) {
	if environment == nil || environment.catalog == nil || environment.userID == 0 {
		return nil, fmt.Errorf("workflow tool environment is not configured")
	}
	if ctx == nil {
		return nil, fmt.Errorf("workflow tool environment context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := task.Validate(); err != nil {
		return nil, fmt.Errorf("validate workflow tool task: %w", err)
	}

	requested := make(map[string]struct{}, len(task.AllowedTools))
	for _, name := range task.AllowedTools {
		requested[name] = struct{}{}
	}
	bindings, err := environment.catalog.ListWorkflowTools(ctx, environment.userID, environment.limit)
	if err != nil {
		return nil, fmt.Errorf("list workflow tools: %w", err)
	}

	seenNames := make(map[string]struct{}, len(bindings))
	seenBindings := make(map[string]struct{}, len(bindings))
	available := make([]WorkflowToolBinding, 0, len(bindings))
	for _, candidate := range bindings {
		binding, err := normalizeWorkflowToolBinding(candidate)
		if err != nil {
			return nil, err
		}
		if _, exists := seenNames[binding.Tool.Name]; exists {
			return nil, fmt.Errorf("duplicate workflow catalog tool %q", binding.Tool.Name)
		}
		seenNames[binding.Tool.Name] = struct{}{}
		bindingDigest := workflowBindingDigest(binding)
		if _, exists := seenBindings[bindingDigest]; exists {
			return nil, fmt.Errorf("duplicate workflow publication binding for tool %q", binding.Tool.Name)
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

func normalizeWorkflowToolBinding(binding WorkflowToolBinding) (WorkflowToolBinding, error) {
	binding.Tool.Name = strings.TrimSpace(binding.Tool.Name)
	binding.PublicationID = strings.TrimSpace(binding.PublicationID)
	binding.WorkflowID = strings.TrimSpace(binding.WorkflowID)
	binding.WorkflowRevisionID = strings.TrimSpace(binding.WorkflowRevisionID)
	binding.WorkflowDSLHash = strings.TrimSpace(binding.WorkflowDSLHash)
	if binding.Tool.Name == "" {
		return WorkflowToolBinding{}, fmt.Errorf("workflow catalog tool name is required")
	}
	if !validObjectIdentity(binding.PublicationID) || !validObjectIdentity(binding.WorkflowID) ||
		!validObjectIdentity(binding.WorkflowRevisionID) {
		return WorkflowToolBinding{}, fmt.Errorf("workflow tool %q immutable identity is invalid", binding.Tool.Name)
	}
	if binding.PublicationRevision < 1 || binding.WorkflowRevisionNumber < 1 {
		return WorkflowToolBinding{}, fmt.Errorf("workflow tool %q immutable revision is invalid", binding.Tool.Name)
	}
	if binding.Tool.Name != "workflow_"+binding.WorkflowID {
		return WorkflowToolBinding{}, fmt.Errorf("workflow tool %q stable identity is invalid", binding.Tool.Name)
	}
	if !validSHA256Hex(binding.WorkflowDSLHash) {
		return WorkflowToolBinding{}, fmt.Errorf("workflow tool %q DSL hash is invalid", binding.Tool.Name)
	}
	switch binding.Tool.Category {
	case agentRuntime.ToolCategoryRead, agentRuntime.ToolCategoryWrite, agentRuntime.ToolCategoryRisky:
	default:
		return WorkflowToolBinding{}, fmt.Errorf("workflow tool %q category is invalid", binding.Tool.Name)
	}
	if len(binding.Tool.InputSchema) != 0 && !json.Valid(binding.Tool.InputSchema) {
		return WorkflowToolBinding{}, fmt.Errorf("workflow tool %q input schema is invalid", binding.Tool.Name)
	}
	binding.Tool = cloneWorkflowToolDefinition(binding.Tool)
	return binding, nil
}

func workflowBindingDigest(binding WorkflowToolBinding) string {
	digest, _ := WorkflowToolBindingDigest(WorkflowToolBindingIdentity{
		PublicationID: binding.PublicationID, PublicationRevision: binding.PublicationRevision,
		WorkflowID: binding.WorkflowID, WorkflowRevisionID: binding.WorkflowRevisionID,
		WorkflowRevisionNumber: binding.WorkflowRevisionNumber, WorkflowDSLHash: binding.WorkflowDSLHash,
	})
	return digest
}

// WorkflowToolBindingDigest returns the canonical digest shared by catalog
// snapshots and structured Workflow-as-Tool observations.
func WorkflowToolBindingDigest(identity WorkflowToolBindingIdentity) (string, error) {
	identity.PublicationID = strings.TrimSpace(identity.PublicationID)
	identity.WorkflowID = strings.TrimSpace(identity.WorkflowID)
	identity.WorkflowRevisionID = strings.TrimSpace(identity.WorkflowRevisionID)
	identity.WorkflowDSLHash = strings.TrimSpace(identity.WorkflowDSLHash)
	if !validObjectIdentity(identity.PublicationID) || !validObjectIdentity(identity.WorkflowID) ||
		!validObjectIdentity(identity.WorkflowRevisionID) ||
		identity.PublicationRevision < 1 || identity.WorkflowRevisionNumber < 1 ||
		!validSHA256Hex(identity.WorkflowDSLHash) {
		return "", fmt.Errorf("workflow tool immutable binding identity is invalid")
	}
	canonical, err := json.Marshal(struct {
		PublicationID          string `json:"publication_id"`
		PublicationRevision    int64  `json:"publication_revision"`
		WorkflowID             string `json:"workflow_id"`
		WorkflowRevisionID     string `json:"workflow_revision_id"`
		WorkflowRevisionNumber int64  `json:"workflow_revision_number"`
		WorkflowDSLHash        string `json:"workflow_dsl_hash"`
	}{
		PublicationID: identity.PublicationID, PublicationRevision: identity.PublicationRevision,
		WorkflowID: identity.WorkflowID, WorkflowRevisionID: identity.WorkflowRevisionID,
		WorkflowRevisionNumber: identity.WorkflowRevisionNumber, WorkflowDSLHash: identity.WorkflowDSLHash,
	})
	if err != nil {
		return "", fmt.Errorf("encode workflow tool immutable binding: %w", err)
	}
	return sha256Digest(canonical), nil
}

func validObjectIdentity(value string) bool {
	if len(value) != 24 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 12 && value == strings.ToLower(value)
}

func validSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && value == strings.ToLower(value)
}

func cloneWorkflowToolDefinition(tool agentRuntime.ToolDefinition) agentRuntime.ToolDefinition {
	tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
	return tool
}

type workflowToolSnapshotCatalog struct {
	Schema      string                         `json:"schema"`
	Environment string                         `json:"environment"`
	ActorDigest string                         `json:"actor_digest"`
	Tools       []workflowToolSnapshotIdentity `json:"tools"`
}

type workflowToolSnapshotIdentity struct {
	Name             string                    `json:"name"`
	Category         agentRuntime.ToolCategory `json:"category"`
	RequiresApproval bool                      `json:"requires_approval"`
	BindingDigest    string                    `json:"binding_digest"`
}

type workflowToolSnapshotMetadata struct {
	Schema      string                         `json:"schema"`
	Phase       agentRuntime.SnapshotPhase     `json:"phase"`
	ActorDigest string                         `json:"actor_digest"`
	ToolCount   int                            `json:"tool_count"`
	Tools       []workflowToolSnapshotIdentity `json:"tools"`
}

type WorkflowToolSnapshotBindingView struct {
	Name             string
	Category         agentRuntime.ToolCategory
	RequiresApproval bool
	BindingDigest    string
}

type WorkflowToolSnapshotView struct {
	Tools []WorkflowToolSnapshotBindingView
}

// DecodeWorkflowToolSnapshot validates tenant, phase, digest and stable catalog
// ordering before exposing the low-sensitivity immutable binding digests.
func DecodeWorkflowToolSnapshot(
	snapshot *agentRuntime.EnvironmentSnapshot,
	phase agentRuntime.SnapshotPhase,
	userID uint64,
) (WorkflowToolSnapshotView, error) {
	if snapshot == nil || snapshot.Environment != WorkflowToolEnvironmentName || userID == 0 {
		return WorkflowToolSnapshotView{}, fmt.Errorf("workflow tool snapshot identity is invalid")
	}
	var metadata workflowToolSnapshotMetadata
	if len(snapshot.Metadata) == 0 || !json.Valid(snapshot.Metadata) ||
		json.Unmarshal(snapshot.Metadata, &metadata) != nil {
		return WorkflowToolSnapshotView{}, fmt.Errorf("workflow tool snapshot metadata is invalid")
	}
	if metadata.Schema != workflowToolSnapshotSchema || metadata.Phase != phase ||
		metadata.ActorDigest != workflowToolActorDigest(userID) ||
		metadata.ToolCount != len(metadata.Tools) || len(metadata.Tools) > maxWorkflowToolLimit {
		return WorkflowToolSnapshotView{}, fmt.Errorf("workflow tool snapshot metadata binding is invalid")
	}
	views := make([]WorkflowToolSnapshotBindingView, 0, len(metadata.Tools))
	previous := ""
	for _, tool := range metadata.Tools {
		if tool.Name == "" || !validWorkflowToolDigest(tool.BindingDigest) ||
			(previous != "" && tool.Name <= previous) {
			return WorkflowToolSnapshotView{}, fmt.Errorf("workflow tool snapshot binding is invalid")
		}
		previous = tool.Name
		switch tool.Category {
		case agentRuntime.ToolCategoryRead:
			if tool.RequiresApproval {
				return WorkflowToolSnapshotView{}, fmt.Errorf("workflow read snapshot approval binding is invalid")
			}
		case agentRuntime.ToolCategoryWrite, agentRuntime.ToolCategoryRisky:
			if !tool.RequiresApproval {
				return WorkflowToolSnapshotView{}, fmt.Errorf("workflow governed snapshot approval binding is invalid")
			}
		default:
			return WorkflowToolSnapshotView{}, fmt.Errorf("workflow tool snapshot category is invalid")
		}
		views = append(views, WorkflowToolSnapshotBindingView{
			Name: tool.Name, Category: tool.Category,
			RequiresApproval: tool.RequiresApproval, BindingDigest: tool.BindingDigest,
		})
	}
	identity, err := json.Marshal(workflowToolSnapshotCatalog{
		Schema: metadata.Schema, Environment: WorkflowToolEnvironmentName,
		ActorDigest: metadata.ActorDigest, Tools: metadata.Tools,
	})
	if err != nil {
		return WorkflowToolSnapshotView{}, fmt.Errorf("encode workflow tool snapshot identity: %w", err)
	}
	digest := sha256Digest(identity)
	if snapshot.Digest != digest {
		return WorkflowToolSnapshotView{}, fmt.Errorf("workflow tool snapshot digest is invalid")
	}
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	if snapshot.ID != "workflow-tool-catalog:"+hexDigest[:24] ||
		snapshot.Reference != "agent-environment://workflow.tool.v1/catalog/sha256/"+hexDigest {
		return WorkflowToolSnapshotView{}, fmt.Errorf("workflow tool snapshot reference is invalid")
	}
	return WorkflowToolSnapshotView{Tools: views}, nil
}

func workflowToolActorDigest(userID uint64) string {
	return sha256Digest([]byte(fmt.Sprintf("workflow-tool-actor:%d", userID)))
}

func validWorkflowToolDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") &&
		validSHA256Hex(strings.TrimPrefix(value, "sha256:"))
}

var _ agentRuntime.Environment = (*WorkflowToolEnvironment)(nil)
