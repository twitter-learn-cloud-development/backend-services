package service

import (
	"context"
	"errors"

	agentproject "twitter-clone/internal/module/agent/project"
)

func (s *AgentService) CreateAgentProject(
	ctx context.Context,
	actorUserID uint64,
	name string,
) (*agentproject.Project, error) {
	manager, err := s.agentProjects()
	if err != nil {
		return nil, err
	}
	return manager.CreateProject(ctx, actorUserID, name)
}

func (s *AgentService) ListAgentProjects(
	ctx context.Context,
	userID uint64,
	page, pageSize int,
) ([]*agentproject.Project, int64, error) {
	manager, err := s.agentProjects()
	if err != nil {
		return nil, 0, err
	}
	return manager.ListProjects(ctx, userID, page, pageSize)
}

func (s *AgentService) GetAgentProject(
	ctx context.Context,
	userID uint64,
	projectID string,
) (*agentproject.Project, error) {
	manager, err := s.agentProjects()
	if err != nil {
		return nil, err
	}
	return manager.GetProject(ctx, userID, projectID)
}

func (s *AgentService) UpsertAgentProjectMember(
	ctx context.Context,
	actorUserID uint64,
	projectID string,
	targetUserID uint64,
	role string,
	expectedRevision int64,
) (*agentproject.Project, error) {
	manager, err := s.agentProjects()
	if err != nil {
		return nil, err
	}
	return manager.UpsertMember(ctx, actorUserID, projectID, targetUserID, role, expectedRevision)
}

func (s *AgentService) RemoveAgentProjectMember(
	ctx context.Context,
	actorUserID uint64,
	projectID string,
	targetUserID uint64,
	expectedRevision int64,
) (*agentproject.Project, error) {
	manager, err := s.agentProjects()
	if err != nil {
		return nil, err
	}
	return manager.RemoveMember(ctx, actorUserID, projectID, targetUserID, expectedRevision)
}

func (s *AgentService) agentProjects() (*agentproject.Manager, error) {
	if s == nil || s.agentProjectManager == nil {
		return nil, errors.New("Agent project manager is unavailable")
	}
	return s.agentProjectManager, nil
}
