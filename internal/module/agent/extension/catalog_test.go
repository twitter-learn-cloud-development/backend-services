package extension

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPageFiltersSortsAndBindsCursor(t *testing.T) {
	entries := []Entry{
		mcpEntry("mcp-b", "server.weather", ScopeProject, CategoryRead),
		skillEntry("skill-b", "Weekly digest"),
		capabilityEntry("web.search", StatusPlanned),
		capabilityEntry("conversation.reply", StatusAvailable),
		mcpEntry("mcp-a", "server.calendar", ScopeUser, CategoryWrite),
	}
	sources := []SourceStatus{
		{Source: SourceExternalMCP, State: SourceStateReady, EntryCount: 2},
		{Source: SourceWorkflow, State: SourceStateReady, EntryCount: 1},
		{Source: SourceBuiltIn, State: SourceStateReady, EntryCount: 2},
	}

	first, err := BuildPage(entries, sources, Query{PageSize: 2}, 20)
	require.NoError(t, err)
	require.Equal(t, ContractVersionV1, first.ContractVersion)
	require.Equal(t, []string{"conversation.reply", "web.search"}, entryNames(first.Entries))
	require.True(t, first.HasMore)
	require.NotEmpty(t, first.NextCursor)
	require.Equal(t, SourceBuiltIn, first.Sources[0].Source)

	second, err := BuildPage(entries, sources, Query{PageSize: 2, AfterCursor: first.NextCursor}, 20)
	require.NoError(t, err)
	require.Equal(t, []string{"skill-b", "server.calendar"}, entryNames(second.Entries))
	require.True(t, second.HasMore)

	_, err = BuildPage(entries, sources, Query{
		Kind: KindSkill, PageSize: 2, AfterCursor: first.NextCursor,
	}, 20)
	require.ErrorIs(t, err, ErrInvalidCursor)
}

func TestBuildPageFiltersByKindScopeCategoryStatusAndSearch(t *testing.T) {
	entries := []Entry{
		mcpEntry("mcp-read", "server.search", ScopeProject, CategoryRead),
		mcpEntry("mcp-write", "server.publish", ScopeUser, CategoryWrite),
		skillEntry("skill-news", "Daily news digest"),
		capabilityEntry("web.search", StatusPlanned),
	}

	page, err := BuildPage(entries, nil, Query{
		Kind: KindMCPTool, Scope: ScopeProject, Category: CategoryRead,
		Status: StatusAvailable, Search: "SEARCH",
	}, 20)
	require.NoError(t, err)
	require.Equal(t, []string{"server.search"}, entryNames(page.Entries))
	require.False(t, page.HasMore)
}

func TestBuildPageRejectsInvalidAndDuplicateEntries(t *testing.T) {
	entry := skillEntry("skill-a", "Skill A")
	_, err := BuildPage([]Entry{entry, entry}, nil, Query{}, 20)
	require.ErrorIs(t, err, ErrInvalidEntry)

	invalid := entry
	invalid.Skill = nil
	_, err = BuildPage([]Entry{invalid}, nil, Query{}, 20)
	require.ErrorIs(t, err, ErrInvalidEntry)
}

func TestBuildPageReturnsClones(t *testing.T) {
	entry := skillEntry("skill-a", "Skill A")
	page, err := BuildPage([]Entry{entry}, nil, Query{}, 20)
	require.NoError(t, err)

	page.Entries[0].Skill.SkillID = "changed"
	require.Equal(t, "skill-a", entry.Skill.SkillID)
}

func TestBuildPageRejectsInvalidQuery(t *testing.T) {
	_, err := BuildPage(nil, nil, Query{Kind: "unknown"}, 20)
	require.ErrorIs(t, err, ErrInvalidQuery)

	_, err = BuildPage(nil, nil, Query{AfterCursor: "not-base64"}, 20)
	require.True(t, errors.Is(err, ErrInvalidCursor))
}

func capabilityEntry(name, status string) Entry {
	return Entry{
		ContractVersion: ContractVersionV1,
		ID:              "capability:" + name,
		Kind:            KindCapability,
		Name:            name,
		DisplayName:     name,
		Description:     "Built-in capability",
		Version:         "v1",
		Source:          SourceBuiltIn,
		CapabilityID:    name,
		Category:        CategoryGeneral,
		Scope:           ScopePlatform,
		Status:          status,
		ApprovalMode:    ApprovalNone,
		HealthStatus:    HealthNotApplicable,
	}
}

func skillEntry(id, displayName string) Entry {
	return Entry{
		ContractVersion: ContractVersionV1,
		ID:              "skill:" + id,
		Kind:            KindSkill,
		Name:            id,
		DisplayName:     displayName,
		Description:     "Workflow skill",
		Version:         "v1-deadbeef",
		Source:          SourceWorkflow,
		CapabilityID:    "skill.run",
		Category:        CategoryWorkflow,
		Scope:           ScopeUser,
		Status:          StatusAvailable,
		ApprovalMode:    ApprovalInherited,
		HealthStatus:    HealthNotApplicable,
		Skill:           &SkillReference{SkillID: id, Version: "v1-deadbeef"},
	}
}

func mcpEntry(id, qualifiedName, scope, category string) Entry {
	approval := ApprovalNone
	if category != CategoryRead {
		approval = ApprovalRequired
	}
	return Entry{
		ContractVersion: ContractVersionV1,
		ID:              id,
		Kind:            KindMCPTool,
		Name:            qualifiedName,
		DisplayName:     qualifiedName,
		Description:     "MCP tool",
		Version:         "snapshot-v1",
		Source:          SourceExternalMCP,
		CapabilityID:    "connector.mcp",
		Category:        category,
		Scope:           scope,
		Status:          StatusAvailable,
		ApprovalMode:    approval,
		HealthStatus:    HealthHealthy,
		MCP: &MCPReference{
			ConnectionID: id, ServerID: "server", SnapshotID: "snapshot",
			QualifiedToolName: qualifiedName,
		},
	}
}

func entryNames(entries []Entry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name)
	}
	return result
}
