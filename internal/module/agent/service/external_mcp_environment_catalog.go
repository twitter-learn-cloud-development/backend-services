package service

import (
	"context"
	"fmt"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
)

type externalMCPEnvironmentCatalog struct {
	source AgentExtensionMCPSource
}

func (catalog *externalMCPEnvironmentCatalog) ListExternalMCPTools(
	ctx context.Context,
	userID uint64,
) ([]agentEnvironment.ExternalMCPToolBinding, error) {
	if catalog == nil || catalog.source == nil {
		return nil, fmt.Errorf("external MCP environment catalog is not configured")
	}
	tools, err := catalog.source.ListGovernedTools(ctx, userID)
	if err != nil {
		return nil, err
	}
	bindings := make([]agentEnvironment.ExternalMCPToolBinding, 0, len(tools))
	for _, tool := range tools {
		definitions := externalMCPRuntimeTools([]externalmcp.ExecutableTool{tool})
		if len(definitions) != 1 {
			return nil, fmt.Errorf("project external MCP tool %q into the runtime catalog", tool.Schema.QualifiedName)
		}
		bindings = append(bindings, agentEnvironment.ExternalMCPToolBinding{
			Tool:                definitions[0],
			ConnectionID:        tool.ConnectionID,
			ConnectionOwnerID:   tool.ConnectionOwnerID,
			ConnectionScope:     tool.ConnectionScope,
			ConnectionRevision:  tool.ConnectionRevision,
			ServerID:            tool.ServerID,
			SnapshotID:          tool.SnapshotID,
			SnapshotVersion:     tool.SnapshotVersion,
			SchemaHash:          tool.SchemaHash,
			PolicySnapshotID:    tool.Policy.SnapshotID,
			PolicyToolName:      tool.Policy.ToolName,
			PolicyCategory:      tool.Policy.Category,
			PolicyQualifiedName: tool.Policy.QualifiedName,
			PolicyEnabled:       tool.Policy.Enabled,
		})
	}
	return bindings, nil
}

func (s *AgentService) newExternalMCPEnvironment(
	userID uint64,
) (*agentEnvironment.ExternalMCPEnvironment, error) {
	if s == nil || !s.externalMCPEnabled {
		return nil, externalmcp.ErrDisabled
	}
	manager, err := s.externalMCP()
	if err != nil {
		return nil, err
	}
	return agentEnvironment.NewExternalMCPEnvironment(
		&externalMCPEnvironmentCatalog{source: manager},
		userID,
	)
}

var _ agentEnvironment.ExternalMCPToolCatalog = (*externalMCPEnvironmentCatalog)(nil)
