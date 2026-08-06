package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"twitter-clone/internal/module/agent/extension"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/skill"
)

const (
	defaultAgentExtensionCatalogLimit = extension.DefaultPageSize
	maxAgentExtensionCatalogLimit     = extension.MaxPageSize
)

// AgentExtensionSkillSource is a read-only projection boundary. Implementors
// must enforce tenant access before returning immutable Skill versions.
type AgentExtensionSkillSource interface {
	List(context.Context, uint64, int) ([]skill.Version, error)
}

// AgentExtensionMCPSource returns only reviewed, enabled and tenant-authorized
// tools. Runtime policy and approval checks remain mandatory during execution.
type AgentExtensionMCPSource interface {
	ListGovernedTools(context.Context, uint64) ([]externalmcp.ExecutableTool, error)
}

type workflowSkillExtensionSource struct {
	service *AgentService
}

func (source workflowSkillExtensionSource) List(
	ctx context.Context,
	userID uint64,
	limit int,
) ([]skill.Version, error) {
	return source.service.ListAgentSkills(ctx, userID, limit)
}

// ListAgentExtensions assembles a bounded, credential-free tenant snapshot.
// The returned references are for discovery only: selecting or invoking an
// entry still resolves through Capability, Skill or governed MCP authority.
func (s *AgentService) ListAgentExtensions(
	ctx context.Context,
	userID uint64,
	query extension.Query,
) (extension.Page, error) {
	if s == nil || !s.extensionCatalogEnabled {
		return extension.Page{}, extension.ErrCatalogDisabled
	}
	if err := ctx.Err(); err != nil {
		return extension.Page{}, err
	}
	if userID == 0 {
		return extension.Page{}, fmt.Errorf("%w: user_id is required", ErrInvalidUnifiedAgentRequest)
	}
	if s.capabilityCatalog == nil {
		return extension.Page{}, fmt.Errorf("agent extension capability source is unavailable")
	}

	entries := make([]extension.Entry, 0, extension.MaxCatalogItems)
	sources := make([]extension.SourceStatus, 0, 3)

	capabilities, err := s.capabilityCatalog.List(ctx)
	if err != nil {
		return extension.Page{}, fmt.Errorf("list extension capabilities: %w", err)
	}
	for _, definition := range capabilities {
		entries = append(entries, capabilityExtensionEntry(definition))
	}
	sources = append(sources, extension.SourceStatus{
		Source: extension.SourceBuiltIn, State: extension.SourceStateReady, EntryCount: len(capabilities),
	})

	skillState := extension.SourceStateDisabled
	skillCount := 0
	if s.skillCatalogEnabled && s.workflowAsToolEnabled && s.extensionSkillSource != nil {
		versions, listErr := s.extensionSkillSource.List(ctx, userID, maxAgentSkillCatalogLimit)
		if listErr != nil {
			return extension.Page{}, fmt.Errorf("list extension skills: %w", listErr)
		}
		for _, version := range versions {
			entries = append(entries, skillExtensionEntry(version))
		}
		skillState = extension.SourceStateReady
		skillCount = len(versions)
	}
	sources = append(sources, extension.SourceStatus{
		Source: extension.SourceWorkflow, State: skillState, EntryCount: skillCount,
	})

	mcpState := extension.SourceStateDisabled
	mcpCount := 0
	if s.externalMCPEnabled && s.extensionMCPSource != nil {
		tools, listErr := s.extensionMCPSource.ListGovernedTools(ctx, userID)
		if listErr != nil {
			return extension.Page{}, fmt.Errorf("list extension MCP tools: %w", listErr)
		}
		for _, tool := range tools {
			entries = append(entries, mcpExtensionEntry(tool))
		}
		mcpState = extension.SourceStateReady
		mcpCount = len(tools)
	}
	sources = append(sources, extension.SourceStatus{
		Source: extension.SourceExternalMCP, State: mcpState, EntryCount: mcpCount,
	})

	return extension.BuildPage(entries, sources, query, s.extensionCatalogLimit)
}

func capabilityExtensionEntry(definition AgentCapabilityDefinition) extension.Entry {
	status := extension.StatusPlanned
	if definition.Status == AgentCapabilityAvailable {
		status = extension.StatusAvailable
	}
	return extension.Entry{
		ContractVersion: extension.ContractVersionV1,
		ID:              stableExtensionID(extension.KindCapability, definition.ID, definition.Version),
		Kind:            extension.KindCapability,
		Name:            definition.ID,
		DisplayName:     capabilityDisplayName(definition.ID),
		Description:     boundedExtensionText(definition.Description, 512),
		Version:         definition.Version,
		Source:          extension.SourceBuiltIn,
		CapabilityID:    definition.ID,
		Category:        capabilityExtensionCategory(definition.ID),
		Scope:           extension.ScopePlatform,
		Status:          status,
		ApprovalMode:    extension.ApprovalNone,
		HealthStatus:    extension.HealthNotApplicable,
	}
}

func skillExtensionEntry(version skill.Version) extension.Entry {
	return extension.Entry{
		ContractVersion: extension.ContractVersionV1,
		ID:              stableExtensionID(extension.KindSkill, version.ID, version.Version),
		Kind:            extension.KindSkill,
		Name:            version.ID,
		DisplayName:     boundedExtensionText(version.DisplayName, 160),
		Description:     boundedExtensionText(version.Description, 512),
		Version:         version.Version,
		Source:          extension.SourceWorkflow,
		CapabilityID:    CapabilitySkillRun,
		Category:        extension.CategoryWorkflow,
		Scope:           extension.ScopeUser,
		Status:          extension.StatusAvailable,
		ApprovalMode:    extension.ApprovalInherited,
		HealthStatus:    extension.HealthNotApplicable,
		Skill: &extension.SkillReference{
			SkillID: version.ID,
			Version: version.Version,
		},
	}
}

func mcpExtensionEntry(tool externalmcp.ExecutableTool) extension.Entry {
	category := extension.CategoryRisky
	approvalMode := extension.ApprovalRequired
	switch tool.Policy.Category {
	case externalmcp.ToolCategoryRead:
		category = extension.CategoryRead
		approvalMode = extension.ApprovalNone
	case externalmcp.ToolCategoryWrite:
		category = extension.CategoryWrite
	}
	displayName := tool.Schema.QualifiedName
	if strings.TrimSpace(tool.ConnectionName) != "" {
		displayName = strings.TrimSpace(tool.ConnectionName) + " / " + tool.Schema.Name
	}
	return extension.Entry{
		ContractVersion: extension.ContractVersionV1,
		ID: stableExtensionID(
			extension.KindMCPTool,
			tool.ConnectionID,
			tool.SnapshotID,
			tool.Schema.QualifiedName,
		),
		Kind:         extension.KindMCPTool,
		Name:         boundedExtensionText(tool.Schema.QualifiedName, 160),
		DisplayName:  boundedExtensionText(displayName, 160),
		Description:  boundedExtensionText(tool.Schema.Description, 512),
		Version:      mcpExtensionVersion(tool),
		Source:       extension.SourceExternalMCP,
		CapabilityID: CapabilityExternalMCP,
		Category:     category,
		Scope:        normalizeExtensionMCPScope(tool.ConnectionScope),
		Status:       extension.StatusAvailable,
		ApprovalMode: approvalMode,
		HealthStatus: normalizeExtensionMCPHealth(tool.HealthStatus),
		MCP: &extension.MCPReference{
			ConnectionID:      tool.ConnectionID,
			ServerID:          tool.ServerID,
			SnapshotID:        tool.SnapshotID,
			QualifiedToolName: tool.Schema.QualifiedName,
		},
	}
}

func stableExtensionID(kind string, parts ...string) string {
	payload := kind + "\x00" + strings.Join(parts, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return kind + "_" + hex.EncodeToString(digest[:16])
}

func capabilityDisplayName(capabilityID string) string {
	switch capabilityID {
	case CapabilityConversationReply:
		return "Conversation"
	case CapabilityPlatformSearch:
		return "Platform Search"
	case CapabilityWebSearch:
		return "Web Search"
	case CapabilityContentDraft:
		return "Content Draft"
	case CapabilityExternalMCP:
		return "External MCP"
	case CapabilityWorkflowRun:
		return "Workflow Run"
	case CapabilitySkillRun:
		return "Skill Run"
	default:
		return capabilityID
	}
}

func capabilityExtensionCategory(capabilityID string) string {
	switch capabilityID {
	case CapabilityPlatformSearch, CapabilityWebSearch:
		return extension.CategoryRead
	case CapabilityWorkflowRun, CapabilitySkillRun:
		return extension.CategoryWorkflow
	default:
		return extension.CategoryGeneral
	}
}

func mcpExtensionVersion(tool externalmcp.ExecutableTool) string {
	hash := strings.TrimSpace(tool.SchemaHash)
	if len(hash) > 12 {
		hash = hash[:12]
	}
	if tool.SnapshotVersion > 0 && hash != "" {
		return fmt.Sprintf("snapshot-%d-%s", tool.SnapshotVersion, hash)
	}
	return "snapshot-" + strings.TrimSpace(tool.SnapshotID)
}

func normalizeExtensionMCPScope(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), externalmcp.ScopeProject) {
		return extension.ScopeProject
	}
	return extension.ScopeUser
}

func normalizeExtensionMCPHealth(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case externalmcp.HealthStatusHealthy:
		return extension.HealthHealthy
	case externalmcp.HealthStatusDegraded:
		return extension.HealthDegraded
	case externalmcp.HealthStatusUnhealthy:
		return extension.HealthUnhealthy
	default:
		return extension.HealthUnknown
	}
}

func boundedExtensionText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes < 1 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maxRunes]))
}
