package grpc

import (
	"context"
	"errors"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	agentproject "twitter-clone/internal/module/agent/project"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *AgentServer) CreateAgentProject(
	ctx context.Context,
	req *aiAgentv1.CreateAgentProjectRequest,
) (*aiAgentv1.CreateAgentProjectResponse, error) {
	project, err := s.svc.CreateAgentProject(ctx, req.ActorUserId, req.Name)
	if err != nil {
		return nil, agentProjectError(err, codes.InvalidArgument)
	}
	return &aiAgentv1.CreateAgentProjectResponse{
		Code: 200, Msg: "success", Project: agentProjectToProto(project, req.ActorUserId),
	}, nil
}

func (s *AgentServer) ListAgentProjects(
	ctx context.Context,
	req *aiAgentv1.ListAgentProjectsRequest,
) (*aiAgentv1.ListAgentProjectsResponse, error) {
	projects, total, err := s.svc.ListAgentProjects(ctx, req.UserId, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, agentProjectError(err, codes.Internal)
	}
	items := make([]*aiAgentv1.AgentProject, 0, len(projects))
	for _, project := range projects {
		items = append(items, agentProjectToProto(project, req.UserId))
	}
	return &aiAgentv1.ListAgentProjectsResponse{
		Code: 200, Msg: "success", Projects: items, Total: total,
	}, nil
}

func (s *AgentServer) GetAgentProject(
	ctx context.Context,
	req *aiAgentv1.GetAgentProjectRequest,
) (*aiAgentv1.GetAgentProjectResponse, error) {
	project, err := s.svc.GetAgentProject(ctx, req.UserId, req.ProjectId)
	if err != nil {
		return nil, agentProjectError(err, codes.NotFound)
	}
	return &aiAgentv1.GetAgentProjectResponse{
		Code: 200, Msg: "success", Project: agentProjectToProto(project, req.UserId),
	}, nil
}

func (s *AgentServer) UpsertAgentProjectMember(
	ctx context.Context,
	req *aiAgentv1.UpsertAgentProjectMemberRequest,
) (*aiAgentv1.UpsertAgentProjectMemberResponse, error) {
	project, err := s.svc.UpsertAgentProjectMember(
		ctx, req.ActorUserId, req.ProjectId, req.TargetUserId, req.Role, req.ExpectedRevision,
	)
	if err != nil {
		return nil, agentProjectError(err, codes.InvalidArgument)
	}
	return &aiAgentv1.UpsertAgentProjectMemberResponse{
		Code: 200, Msg: "success", Project: agentProjectToProto(project, req.ActorUserId),
	}, nil
}

func (s *AgentServer) RemoveAgentProjectMember(
	ctx context.Context,
	req *aiAgentv1.RemoveAgentProjectMemberRequest,
) (*aiAgentv1.RemoveAgentProjectMemberResponse, error) {
	project, err := s.svc.RemoveAgentProjectMember(
		ctx, req.ActorUserId, req.ProjectId, req.TargetUserId, req.ExpectedRevision,
	)
	if err != nil {
		return nil, agentProjectError(err, codes.InvalidArgument)
	}
	return &aiAgentv1.RemoveAgentProjectMemberResponse{
		Code: 200, Msg: "success", Project: agentProjectToProto(project, req.ActorUserId),
	}, nil
}

func agentProjectToProto(project *agentproject.Project, currentUserID uint64) *aiAgentv1.AgentProject {
	if project == nil {
		return nil
	}
	members := make([]*aiAgentv1.AgentProjectMember, 0, len(project.Members))
	currentRole := ""
	for _, member := range project.Members {
		members = append(members, &aiAgentv1.AgentProjectMember{
			UserId: member.UserID, Role: member.Role, AddedBy: member.AddedBy,
			CreatedAt: unixOrZero(member.CreatedAt), UpdatedAt: unixOrZero(member.UpdatedAt),
		})
		if member.UserID == currentUserID {
			currentRole = member.Role
		}
	}
	return &aiAgentv1.AgentProject{
		ProjectId: project.ID, Name: project.Name, OwnerId: project.OwnerID,
		Members: members, Revision: project.Revision, CreatedAt: unixOrZero(project.CreatedAt),
		UpdatedAt: unixOrZero(project.UpdatedAt), CurrentRole: currentRole,
	}
}

func agentProjectError(err error, fallback codes.Code) error {
	switch {
	case errors.Is(err, agentproject.ErrDisabled):
		return status.Error(codes.FailedPrecondition, agentproject.ErrDisabled.Error())
	case errors.Is(err, agentproject.ErrNotFound), errors.Is(err, agentproject.ErrMemberNotFound):
		return status.Error(codes.NotFound, "Agent project resource not found")
	case errors.Is(err, agentproject.ErrAccessDenied):
		return status.Error(codes.PermissionDenied, agentproject.ErrAccessDenied.Error())
	case errors.Is(err, agentproject.ErrRevisionConflict):
		return status.Error(codes.Aborted, agentproject.ErrRevisionConflict.Error())
	case errors.Is(err, agentproject.ErrUserNotFound):
		return status.Error(codes.InvalidArgument, agentproject.ErrUserNotFound.Error())
	default:
		return status.Errorf(fallback, "Agent project request failed: %v", err)
	}
}
