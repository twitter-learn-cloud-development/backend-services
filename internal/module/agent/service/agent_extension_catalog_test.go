package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"twitter-clone/internal/module/agent/extension"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/skill"

	"github.com/stretchr/testify/require"
)

func TestAgentExtensionCatalogAggregatesGovernedSources(t *testing.T) {
	t.Parallel()

	skillSource := &extensionSkillSourceFake{versions: []skill.Version{{
		ID: "workflow_digest", Version: "v1-aabbcc", DisplayName: "Weekly digest",
		Description: "Build a reviewed weekly digest.",
	}}}
	mcpSource := &extensionMCPSourceFake{tools: []externalmcp.ExecutableTool{{
		ConnectionID: "connection-1", ConnectionName: "CRM", ConnectionScope: externalmcp.ScopeProject,
		HealthStatus: externalmcp.HealthStatusHealthy, ServerID: "crm", SnapshotID: "snapshot-1",
		SnapshotVersion: 3, SchemaHash: strings.Repeat("a", 64),
		Schema: externalmcp.ToolSchema{
			Name: "create_record", QualifiedName: "crm.create_record", Description: "Create a CRM record.",
		},
		Policy: externalmcp.ToolPolicy{Category: externalmcp.ToolCategoryWrite, Enabled: true},
	}}}
	service := newAgentExtensionCatalogTestService(t, skillSource, mcpSource)

	page, err := service.ListAgentExtensions(context.Background(), 42, extension.Query{PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, extension.ContractVersionV1, page.ContractVersion)
	require.Len(t, page.Entries, 9)
	require.Equal(t, uint64(42), skillSource.userID)
	require.Equal(t, uint64(42), mcpSource.userID)
	require.Equal(t, maxAgentSkillCatalogLimit, skillSource.limit)
	require.False(t, page.HasMore)
	require.Empty(t, page.NextCursor)

	selectedSkill := requireExtensionEntry(t, page.Entries, extension.KindSkill, "workflow_digest")
	require.Equal(t, CapabilitySkillRun, selectedSkill.CapabilityID)
	require.Equal(t, extension.ApprovalInherited, selectedSkill.ApprovalMode)
	require.Equal(t, &extension.SkillReference{
		SkillID: "workflow_digest", Version: "v1-aabbcc",
	}, selectedSkill.Skill)

	selectedMCP := requireExtensionEntry(t, page.Entries, extension.KindMCPTool, "crm.create_record")
	require.Equal(t, "CRM / create_record", selectedMCP.DisplayName)
	require.Equal(t, "snapshot-3-aaaaaaaaaaaa", selectedMCP.Version)
	require.Equal(t, extension.ScopeProject, selectedMCP.Scope)
	require.Equal(t, extension.CategoryWrite, selectedMCP.Category)
	require.Equal(t, extension.ApprovalRequired, selectedMCP.ApprovalMode)
	require.Equal(t, extension.HealthHealthy, selectedMCP.HealthStatus)
	require.Equal(t, &extension.MCPReference{
		ConnectionID: "connection-1", ServerID: "crm", SnapshotID: "snapshot-1",
		QualifiedToolName: "crm.create_record",
	}, selectedMCP.MCP)

	require.Equal(t, []extension.SourceStatus{
		{Source: extension.SourceBuiltIn, State: extension.SourceStateReady, EntryCount: 7},
		{Source: extension.SourceExternalMCP, State: extension.SourceStateReady, EntryCount: 1},
		{Source: extension.SourceWorkflow, State: extension.SourceStateReady, EntryCount: 1},
	}, page.Sources)

	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	for _, forbidden := range []string{"endpoint", "credential", "api_key", "input_schema", "secret-value"} {
		require.NotContains(t, strings.ToLower(string(encoded)), forbidden)
	}
}

func TestAgentExtensionCatalogKeepsDisabledSourcesVisible(t *testing.T) {
	t.Parallel()

	service := newAgentExtensionCatalogTestService(t, nil, nil)
	service.skillCatalogEnabled = false
	service.workflowAsToolEnabled = false
	service.externalMCPEnabled = false

	page, err := service.ListAgentExtensions(context.Background(), 42, extension.Query{
		Kind: extension.KindCapability,
	})
	require.NoError(t, err)
	require.Len(t, page.Entries, 7)
	require.Equal(t, []extension.SourceStatus{
		{Source: extension.SourceBuiltIn, State: extension.SourceStateReady, EntryCount: 7},
		{Source: extension.SourceExternalMCP, State: extension.SourceStateDisabled, EntryCount: 0},
		{Source: extension.SourceWorkflow, State: extension.SourceStateDisabled, EntryCount: 0},
	}, page.Sources)
}

func TestAgentExtensionCatalogFailsClosedOnSourceError(t *testing.T) {
	t.Parallel()

	expected := errors.New("skill store unavailable")
	service := newAgentExtensionCatalogTestService(
		t,
		&extensionSkillSourceFake{err: expected},
		&extensionMCPSourceFake{},
	)
	page, err := service.ListAgentExtensions(context.Background(), 42, extension.Query{})
	require.ErrorIs(t, err, expected)
	require.Empty(t, page)
}

func TestAgentExtensionCatalogRejectsDisabledInvalidAndCancelledRequests(t *testing.T) {
	t.Parallel()

	service := newAgentExtensionCatalogTestService(t, nil, nil)
	service.extensionCatalogEnabled = false
	_, err := service.ListAgentExtensions(context.Background(), 42, extension.Query{})
	require.ErrorIs(t, err, extension.ErrCatalogDisabled)

	service.extensionCatalogEnabled = true
	_, err = service.ListAgentExtensions(context.Background(), 0, extension.Query{})
	require.ErrorIs(t, err, ErrInvalidUnifiedAgentRequest)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = service.ListAgentExtensions(cancelled, 42, extension.Query{})
	require.ErrorIs(t, err, context.Canceled)
}

type extensionSkillSourceFake struct {
	versions []skill.Version
	err      error
	userID   uint64
	limit    int
}

func (source *extensionSkillSourceFake) List(
	_ context.Context,
	userID uint64,
	limit int,
) ([]skill.Version, error) {
	source.userID = userID
	source.limit = limit
	return append([]skill.Version(nil), source.versions...), source.err
}

type extensionMCPSourceFake struct {
	tools  []externalmcp.ExecutableTool
	err    error
	userID uint64
}

func (source *extensionMCPSourceFake) ListGovernedTools(
	_ context.Context,
	userID uint64,
) ([]externalmcp.ExecutableTool, error) {
	source.userID = userID
	return append([]externalmcp.ExecutableTool(nil), source.tools...), source.err
}

func newAgentExtensionCatalogTestService(
	t *testing.T,
	skillSource AgentExtensionSkillSource,
	mcpSource AgentExtensionMCPSource,
) *AgentService {
	t.Helper()
	catalog, err := NewBuiltInAgentCapabilityCatalog(
		WithAvailableExternalMCPCapability(),
		WithAvailableWorkflowCapability(),
		WithAvailableSkillCapability(),
	)
	require.NoError(t, err)
	return &AgentService{
		extensionCatalogEnabled: true,
		extensionCatalogLimit:   defaultAgentExtensionCatalogLimit,
		capabilityCatalog:       catalog,
		skillCatalogEnabled:     true,
		workflowAsToolEnabled:   true,
		externalMCPEnabled:      true,
		extensionSkillSource:    skillSource,
		extensionMCPSource:      mcpSource,
	}
}

func requireExtensionEntry(
	t *testing.T,
	entries []extension.Entry,
	kind string,
	name string,
) extension.Entry {
	t.Helper()
	for _, entry := range entries {
		if entry.Kind == kind && entry.Name == name {
			return entry
		}
	}
	t.Fatalf("extension entry %s/%s was not found: %+v", kind, name, entries)
	return extension.Entry{}
}
