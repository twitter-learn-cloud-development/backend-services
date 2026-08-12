package environment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

// ToolCatalog exposes capability metadata only. Tool execution remains behind
// Runtime's governed ToolExecutor boundary.
type ToolCatalog interface {
	ListTools(ctx context.Context) ([]agentRuntime.ToolDefinition, error)
}

type readCatalogConfig struct {
	name             string
	label            string
	snapshotSchema   string
	snapshotIDPrefix string
	referencePrefix  string
	toolNames        []string
}

type readCatalogEnvironment struct {
	catalog ToolCatalog
	config  readCatalogConfig
	now     func() time.Time
}

func newReadCatalogEnvironment(catalog ToolCatalog, config readCatalogConfig) (*readCatalogEnvironment, error) {
	if catalog == nil {
		return nil, fmt.Errorf("%s tool catalog is required", config.label)
	}
	if strings.TrimSpace(config.name) == "" || strings.TrimSpace(config.label) == "" ||
		strings.TrimSpace(config.snapshotSchema) == "" || strings.TrimSpace(config.snapshotIDPrefix) == "" ||
		strings.TrimSpace(config.referencePrefix) == "" {
		return nil, fmt.Errorf("read catalog environment identity is incomplete")
	}
	if len(config.toolNames) == 0 {
		return nil, fmt.Errorf("%s tool policy is required", config.label)
	}
	seen := make(map[string]struct{}, len(config.toolNames))
	toolNames := make([]string, 0, len(config.toolNames))
	for _, candidate := range config.toolNames {
		name := strings.TrimSpace(candidate)
		if name == "" {
			return nil, fmt.Errorf("%s tool policy contains an empty name", config.label)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("%s tool policy contains duplicate %q", config.label, name)
		}
		seen[name] = struct{}{}
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)
	config.toolNames = toolNames
	return &readCatalogEnvironment{catalog: catalog, config: config, now: time.Now}, nil
}

func (environment *readCatalogEnvironment) tools(
	ctx context.Context,
	task agentRuntime.TaskSpec,
) ([]agentRuntime.ToolDefinition, error) {
	if environment == nil || environment.catalog == nil {
		return nil, fmt.Errorf("read catalog environment is not configured")
	}
	if ctx == nil {
		return nil, fmt.Errorf("%s environment context is required", environment.config.label)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := task.Validate(); err != nil {
		return nil, fmt.Errorf("validate %s task: %w", environment.config.label, err)
	}

	requested := make(map[string]struct{}, len(task.AllowedTools))
	for _, name := range task.AllowedTools {
		requested[name] = struct{}{}
	}
	policy := make(map[string]struct{}, len(environment.config.toolNames))
	for _, name := range environment.config.toolNames {
		policy[name] = struct{}{}
	}
	tools, err := environment.catalog.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("list %s tools: %w", environment.config.label, err)
	}

	seen := make(map[string]struct{}, len(tools))
	available := make([]agentRuntime.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return nil, fmt.Errorf("catalog tool name is required")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate catalog tool %q", name)
		}
		seen[name] = struct{}{}

		if _, allowed := policy[name]; !allowed {
			continue
		}
		if _, allowed := requested[name]; !allowed {
			continue
		}
		if tool.Category != agentRuntime.ToolCategoryRead || tool.ApprovalRequired() {
			return nil, fmt.Errorf("%s tool %q is not safely classified", environment.config.label, name)
		}
		if len(bytes.TrimSpace(tool.InputSchema)) != 0 && !json.Valid(tool.InputSchema) {
			return nil, fmt.Errorf("%s tool %q input schema is invalid", environment.config.label, name)
		}
		tool.Name = name
		tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
		available = append(available, tool)
	}

	sort.Slice(available, func(left, right int) bool {
		return available[left].Name < available[right].Name
	})
	return available, nil
}

func (environment *readCatalogEnvironment) snapshot(
	ctx context.Context,
	request agentRuntime.SnapshotRequest,
) (agentRuntime.EnvironmentSnapshot, error) {
	if environment == nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("read catalog environment is not configured")
	}
	if request.Phase != agentRuntime.SnapshotPhaseBefore && request.Phase != agentRuntime.SnapshotPhaseAfter {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("unsupported %s snapshot phase %q", environment.config.label, request.Phase)
	}
	if len(request.Scope) != 0 {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("%s catalog snapshot does not support resource scope", environment.config.label)
	}

	tools, err := environment.tools(ctx, request.Task)
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, err
	}
	identityTools := make([]readCatalogToolIdentity, 0, len(tools))
	metadataTools := make([]readCatalogToolIdentity, 0, len(tools))
	for _, tool := range tools {
		identity := readCatalogToolIdentity{Name: tool.Name, Category: tool.Category}
		identityTools = append(identityTools, identity)
		metadataTools = append(metadataTools, identity)
	}

	identity, err := json.Marshal(readCatalogIdentity{
		Schema: environment.config.snapshotSchema, Environment: environment.config.name, Tools: identityTools,
	})
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("encode %s catalog identity: %w", environment.config.label, err)
	}
	digest := sha256Digest(identity)
	hexDigest := strings.TrimPrefix(digest, "sha256:")
	metadata, err := json.Marshal(readCatalogSnapshotMetadata{
		Schema: environment.config.snapshotSchema, Phase: request.Phase,
		ToolCount: len(metadataTools), Tools: metadataTools,
	})
	if err != nil {
		return agentRuntime.EnvironmentSnapshot{}, fmt.Errorf("encode %s catalog metadata: %w", environment.config.label, err)
	}

	return agentRuntime.EnvironmentSnapshot{
		ID:          environment.config.snapshotIDPrefix + hexDigest[:24],
		Environment: environment.config.name,
		CapturedAt:  environment.now().UTC(),
		Digest:      digest,
		Reference:   environment.config.referencePrefix + hexDigest,
		Metadata:    metadata,
	}, nil
}

type readCatalogIdentity struct {
	Schema      string                    `json:"schema"`
	Environment string                    `json:"environment"`
	Tools       []readCatalogToolIdentity `json:"tools"`
}

type readCatalogToolIdentity struct {
	Name     string                    `json:"name"`
	Category agentRuntime.ToolCategory `json:"category"`
}

type readCatalogSnapshotMetadata struct {
	Schema    string                     `json:"schema"`
	Phase     agentRuntime.SnapshotPhase `json:"phase"`
	ToolCount int                        `json:"tool_count"`
	Tools     []readCatalogToolIdentity  `json:"tools"`
}

func sha256Digest(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
