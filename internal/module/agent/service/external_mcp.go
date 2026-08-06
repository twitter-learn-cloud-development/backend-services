package service

import (
	"context"
	"errors"

	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
)

func (s *AgentService) CreateExternalMCPConnection(
	ctx context.Context,
	userID uint64,
	input externalmcp.ConnectionInput,
) (*externalmcp.Connection, error) {
	manager, err := s.externalMCP()
	if err != nil {
		return nil, err
	}
	connection, err := manager.CreateConnection(ctx, userID, input)
	if err == nil {
		s.recordExternalMCPConnectionFacts(ctx, connection)
	}
	return connection, err
}

func (s *AgentService) UpdateExternalMCPConnection(
	ctx context.Context,
	userID uint64,
	connectionID string,
	expectedRevision int64,
	input externalmcp.ConnectionInput,
) (*externalmcp.Connection, error) {
	manager, err := s.externalMCP()
	if err != nil {
		return nil, err
	}
	connection, err := manager.UpdateConnection(ctx, userID, connectionID, expectedRevision, input)
	if err == nil {
		s.recordExternalMCPConnectionFacts(ctx, connection)
	}
	return connection, err
}

func (s *AgentService) ListExternalMCPConnections(
	ctx context.Context,
	userID uint64,
	page, pageSize int,
) ([]*externalmcp.Connection, int64, error) {
	manager, err := s.externalMCP()
	if err != nil {
		return nil, 0, err
	}
	return manager.ListConnections(ctx, userID, page, pageSize)
}

func (s *AgentService) GetExternalMCPConnection(
	ctx context.Context,
	userID uint64,
	connectionID string,
) (*externalmcp.Connection, error) {
	manager, err := s.externalMCP()
	if err != nil {
		return nil, err
	}
	return manager.GetConnection(ctx, userID, connectionID)
}

func (s *AgentService) RevokeExternalMCPConnection(
	ctx context.Context,
	userID uint64,
	connectionID string,
	expectedRevision int64,
) error {
	manager, err := s.externalMCP()
	if err != nil {
		return err
	}
	return manager.RevokeConnection(ctx, userID, connectionID, expectedRevision)
}

func (s *AgentService) DiscoverExternalMCPTools(
	ctx context.Context,
	userID uint64,
	connectionID string,
	expectedRevision int64,
) (*externalmcp.Connection, *externalmcp.ToolSchemaSnapshot, error) {
	manager, err := s.externalMCP()
	if err != nil {
		return nil, nil, err
	}
	connection, snapshot, err := manager.DiscoverTools(ctx, userID, connectionID, expectedRevision)
	if err == nil {
		s.recordExternalMCPConnectionFacts(ctx, connection)
	}
	return connection, snapshot, err
}

func (s *AgentService) ApproveExternalMCPSnapshot(
	ctx context.Context,
	userID uint64,
	connectionID string,
	snapshotID string,
	expectedRevision int64,
) (*externalmcp.Connection, *externalmcp.ToolSchemaSnapshot, error) {
	manager, err := s.externalMCP()
	if err != nil {
		return nil, nil, err
	}
	connection, snapshot, err := manager.ApproveSnapshot(ctx, userID, connectionID, snapshotID, expectedRevision)
	if err == nil {
		s.recordExternalMCPConnectionFacts(ctx, connection)
	}
	return connection, snapshot, err
}

func (s *AgentService) ListExternalMCPTools(
	ctx context.Context,
	userID uint64,
	connectionID string,
) (*externalmcp.Connection, *externalmcp.ToolSchemaSnapshot, []externalmcp.ToolView, error) {
	manager, err := s.externalMCP()
	if err != nil {
		return nil, nil, nil, err
	}
	return manager.ListTools(ctx, userID, connectionID)
}

func (s *AgentService) ConfigureExternalMCPTool(
	ctx context.Context,
	userID uint64,
	connectionID string,
	expectedRevision int64,
	input externalmcp.ToolPolicyInput,
) (*externalmcp.Connection, externalmcp.ToolView, error) {
	manager, err := s.externalMCP()
	if err != nil {
		return nil, externalmcp.ToolView{}, err
	}
	connection, view, err := manager.ConfigureTool(ctx, userID, connectionID, expectedRevision, input)
	if err == nil {
		s.recordExternalMCPConnectionFacts(ctx, connection)
	}
	return connection, view, err
}

func (s *AgentService) externalMCP() (*externalmcp.Manager, error) {
	if s == nil || s.externalMCPManager == nil {
		return nil, errors.New("external MCP connection manager is unavailable")
	}
	return s.externalMCPManager, nil
}
