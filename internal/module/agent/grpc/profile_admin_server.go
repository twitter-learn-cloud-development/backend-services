package grpc

import (
	"context"
	"crypto/subtle"
	"errors"
	"math"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/module/agent/profile"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/service"
)

func (s *AgentServer) CreateAgentProfileDraft(ctx context.Context, req *aiAgentv1.CreateAgentProfileDraftRequest) (*aiAgentv1.CreateAgentProfileDraftResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || req.Spec == nil {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id and profile spec are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleEditor); err != nil {
		return nil, err
	}
	candidate, err := agentProfileFromProto(req.Spec)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	record, err := s.profileManager.CreateDraft(ctx, candidate, req.ActorUserId)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	version, err := s.agentProfileVersionToProto(record)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.CreateAgentProfileDraftResponse{Code: 200, Msg: "success", ProfileVersion: version}, nil
}

func (s *AgentServer) PublishAgentProfileVersion(ctx context.Context, req *aiAgentv1.PublishAgentProfileVersionRequest) (*aiAgentv1.PublishAgentProfileVersionResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || strings.TrimSpace(req.ProfileId) == "" || strings.TrimSpace(req.Version) == "" || req.ExpectedRevision < 1 {
		return nil, status.Error(codes.InvalidArgument, "actor, profile identity and expected revision are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleAdmin); err != nil {
		return nil, err
	}
	if !s.profileDirectPublishEnabled {
		return nil, status.Error(codes.PermissionDenied, "direct Agent Profile publishing is disabled; use publish approval")
	}
	if err := s.profileManager.PublishVersion(ctx, req.ProfileId, req.Version, req.ExpectedRevision, req.ActorUserId); err != nil {
		return nil, profileAdministrationStatus(err)
	}
	record, err := s.profileManager.GetVersion(ctx, req.ProfileId, req.Version)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	version, err := s.agentProfileVersionToProto(record)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.PublishAgentProfileVersionResponse{Code: 200, Msg: "success", ProfileVersion: version}, nil
}

func (s *AgentServer) RequestAgentProfilePublishApproval(ctx context.Context, req *aiAgentv1.RequestAgentProfilePublishApprovalRequest) (*aiAgentv1.RequestAgentProfilePublishApprovalResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || strings.TrimSpace(req.ProfileId) == "" || strings.TrimSpace(req.Version) == "" || req.ExpectedVersionRevision < 1 {
		return nil, status.Error(codes.InvalidArgument, "actor, profile identity and expected version revision are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleEditor); err != nil {
		return nil, err
	}
	var evidenceReference *profile.QualityEvidenceReference
	if req.QualityEvidence != nil {
		value := agentQualityEvidenceReferenceFromProto(req.QualityEvidence)
		evidenceReference = &value
	}
	approval, err := s.profileManager.RequestPublishApprovalWithEvidence(
		ctx,
		req.ProfileId,
		req.Version,
		req.ExpectedVersionRevision,
		req.ActorUserId,
		evidenceReference,
	)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.RequestAgentProfilePublishApprovalResponse{Code: 200, Msg: "success", Approval: agentProfilePublishApprovalToProto(approval)}, nil
}

func (s *AgentServer) ListAgentProfilePublishApprovals(ctx context.Context, req *aiAgentv1.ListAgentProfilePublishApprovalsRequest) (*aiAgentv1.ListAgentProfilePublishApprovalsResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleViewer); err != nil {
		return nil, err
	}
	records, total, err := s.profileManager.ListPublishApprovals(ctx, req.ProfileId, req.Status, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	approvals := make([]*aiAgentv1.AgentProfilePublishApproval, 0, len(records))
	for _, record := range records {
		approvals = append(approvals, agentProfilePublishApprovalToProto(record))
	}
	return &aiAgentv1.ListAgentProfilePublishApprovalsResponse{Code: 200, Msg: "success", Approvals: approvals, Total: total}, nil
}

func (s *AgentServer) GetAgentProfilePublishApproval(ctx context.Context, req *aiAgentv1.GetAgentProfilePublishApprovalRequest) (*aiAgentv1.GetAgentProfilePublishApprovalResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || strings.TrimSpace(req.ApprovalId) == "" {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id and approval_id are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleViewer); err != nil {
		return nil, err
	}
	record, err := s.profileManager.GetPublishApproval(ctx, req.ApprovalId)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.GetAgentProfilePublishApprovalResponse{Code: 200, Msg: "success", Approval: agentProfilePublishApprovalToProto(record)}, nil
}

func (s *AgentServer) DecideAgentProfilePublishApproval(ctx context.Context, req *aiAgentv1.DecideAgentProfilePublishApprovalRequest) (*aiAgentv1.DecideAgentProfilePublishApprovalResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || strings.TrimSpace(req.ApprovalId) == "" || req.ExpectedRevision < 1 {
		return nil, status.Error(codes.InvalidArgument, "actor, approval identity and expected revision are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleApprover); err != nil {
		return nil, err
	}
	record, err := s.profileManager.DecidePublishApproval(ctx, req.ApprovalId, req.ExpectedRevision, req.ActorUserId, req.Decision, req.Reason)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.DecideAgentProfilePublishApprovalResponse{Code: 200, Msg: "success", Approval: agentProfilePublishApprovalToProto(record)}, nil
}

func (s *AgentServer) RetryAgentProfilePublishApproval(ctx context.Context, req *aiAgentv1.RetryAgentProfilePublishApprovalRequest) (*aiAgentv1.RetryAgentProfilePublishApprovalResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || strings.TrimSpace(req.ApprovalId) == "" || req.ExpectedRevision < 1 {
		return nil, status.Error(codes.InvalidArgument, "actor, approval identity and expected revision are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleApprover); err != nil {
		return nil, err
	}
	record, err := s.profileManager.RetryPublishApproval(ctx, req.ApprovalId, req.ExpectedRevision, req.ActorUserId)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.RetryAgentProfilePublishApprovalResponse{Code: 200, Msg: "success", Approval: agentProfilePublishApprovalToProto(record)}, nil
}

func (s *AgentServer) ListAgentProfileVersions(ctx context.Context, req *aiAgentv1.ListAgentProfileVersionsRequest) (*aiAgentv1.ListAgentProfileVersionsResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleViewer); err != nil {
		return nil, err
	}
	records, total, err := s.profileManager.ListVersions(ctx, req.ProfileId, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	versions := make([]*aiAgentv1.AgentProfileVersion, 0, len(records))
	for _, record := range records {
		converted, convertErr := s.agentProfileVersionToProto(record)
		if convertErr != nil {
			return nil, profileAdministrationStatus(convertErr)
		}
		versions = append(versions, converted)
	}
	return &aiAgentv1.ListAgentProfileVersionsResponse{Code: 200, Msg: "success", ProfileVersions: versions, Total: total}, nil
}

func (s *AgentServer) GetAgentProfileVersion(ctx context.Context, req *aiAgentv1.GetAgentProfileVersionRequest) (*aiAgentv1.GetAgentProfileVersionResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || strings.TrimSpace(req.ProfileId) == "" || strings.TrimSpace(req.Version) == "" {
		return nil, status.Error(codes.InvalidArgument, "actor and profile identity are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleViewer); err != nil {
		return nil, err
	}
	record, err := s.profileManager.GetVersion(ctx, req.ProfileId, req.Version)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	version, err := s.agentProfileVersionToProto(record)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.GetAgentProfileVersionResponse{Code: 200, Msg: "success", ProfileVersion: version}, nil
}

func (s *AgentServer) GetAgentProfileRelease(ctx context.Context, req *aiAgentv1.GetAgentProfileReleaseRequest) (*aiAgentv1.GetAgentProfileReleaseResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || strings.TrimSpace(req.ProfileId) == "" {
		return nil, status.Error(codes.InvalidArgument, "actor and profile_id are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleViewer); err != nil {
		return nil, err
	}
	record, err := s.profileManager.GetRelease(ctx, req.ProfileId)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.GetAgentProfileReleaseResponse{Code: 200, Msg: "success", ProfileRelease: agentProfileReleaseToProto(record)}, nil
}

func (s *AgentServer) UpsertAgentProfileRelease(ctx context.Context, req *aiAgentv1.UpsertAgentProfileReleaseRequest) (*aiAgentv1.UpsertAgentProfileReleaseResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleAdmin); err != nil {
		return nil, err
	}
	record, err := s.profileManager.UpsertRelease(ctx, profile.Release{
		ProfileID:            req.ProfileId,
		StableVersion:        req.StableVersion,
		CandidateVersion:     req.CandidateVersion,
		CandidateBasisPoints: int(req.CandidateBasisPoints),
		Salt:                 req.Salt,
	}, req.ExpectedRevision, req.ActorUserId)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.UpsertAgentProfileReleaseResponse{Code: 200, Msg: "success", ProfileRelease: agentProfileReleaseToProto(record)}, nil
}

func (s *AgentServer) ListAgentProfileAuditEvents(ctx context.Context, req *aiAgentv1.ListAgentProfileAuditEventsRequest) (*aiAgentv1.ListAgentProfileAuditEventsResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleViewer); err != nil {
		return nil, err
	}
	records, total, err := s.profileManager.ListAuditEvents(ctx, req.ProfileId, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	events := make([]*aiAgentv1.AgentProfileAuditEvent, 0, len(records))
	for _, record := range records {
		events = append(events, agentProfileAuditEventToProto(record))
	}
	return &aiAgentv1.ListAgentProfileAuditEventsResponse{Code: 200, Msg: "success", AuditEvents: events, Total: total}, nil
}

func (s *AgentServer) GetAgentProfileManagementAccess(ctx context.Context, req *aiAgentv1.GetAgentProfileManagementAccessRequest) (*aiAgentv1.GetAgentProfileManagementAccessResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	if s.profileAccessManager == nil {
		return nil, status.Error(codes.Unavailable, "Agent Profile access manager is disabled")
	}
	access, err := s.profileAccessManager.ResolveAccess(ctx, req.ActorUserId)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.GetAgentProfileManagementAccessResponse{
		Code: 200, Msg: "success",
		Access: &aiAgentv1.AgentProfileManagementAccess{
			Roles: access.Roles, StaticRoles: access.StaticRoles, DynamicRoles: access.DynamicRoles,
			RootAdmin: access.RootAdmin, DynamicRbacEnabled: s.profileAccessManager.DynamicEnabled(),
			ExperimentsEnabled: s.profileExperimentManager != nil,
		},
	}, nil
}

func (s *AgentServer) ListAgentProfileRoleBindings(ctx context.Context, req *aiAgentv1.ListAgentProfileRoleBindingsRequest) (*aiAgentv1.ListAgentProfileRoleBindingsResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	if s.profileAccessManager == nil {
		return nil, status.Error(codes.Unavailable, "Agent Profile access manager is disabled")
	}
	records, total, err := s.profileAccessManager.ListBindings(ctx, req.ActorUserId, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	bindings := make([]*aiAgentv1.AgentProfileRoleBinding, 0, len(records))
	for _, record := range records {
		bindings = append(bindings, agentProfileRoleBindingToProto(record))
	}
	return &aiAgentv1.ListAgentProfileRoleBindingsResponse{Code: 200, Msg: "success", RoleBindings: bindings, Total: total}, nil
}

func (s *AgentServer) UpsertAgentProfileRoleBinding(ctx context.Context, req *aiAgentv1.UpsertAgentProfileRoleBindingRequest) (*aiAgentv1.UpsertAgentProfileRoleBindingResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || req.SubjectUserId == 0 || req.ExpectedRevision < 0 {
		return nil, status.Error(codes.InvalidArgument, "actor, subject and a non-negative expected revision are required")
	}
	if s.profileAccessManager == nil {
		return nil, status.Error(codes.Unavailable, "Agent Profile access manager is disabled")
	}
	record, err := s.profileAccessManager.UpsertBinding(ctx, req.ActorUserId, req.SubjectUserId, req.Roles, req.ExpectedRevision)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.UpsertAgentProfileRoleBindingResponse{
		Code: 200, Msg: "success", RoleBinding: agentProfileRoleBindingToProto(record),
	}, nil
}

func (s *AgentServer) DeleteAgentProfileRoleBinding(ctx context.Context, req *aiAgentv1.DeleteAgentProfileRoleBindingRequest) (*aiAgentv1.DeleteAgentProfileRoleBindingResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || req.SubjectUserId == 0 || req.ExpectedRevision < 1 {
		return nil, status.Error(codes.InvalidArgument, "actor, subject and expected revision are required")
	}
	if s.profileAccessManager == nil {
		return nil, status.Error(codes.Unavailable, "Agent Profile access manager is disabled")
	}
	if err := s.profileAccessManager.DeleteBinding(ctx, req.ActorUserId, req.SubjectUserId, req.ExpectedRevision); err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.DeleteAgentProfileRoleBindingResponse{Code: 200, Msg: "success"}, nil
}

func (s *AgentServer) ListAgentProfileRoleAuditEvents(ctx context.Context, req *aiAgentv1.ListAgentProfileRoleAuditEventsRequest) (*aiAgentv1.ListAgentProfileRoleAuditEventsResponse, error) {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	if s.profileAccessManager == nil {
		return nil, status.Error(codes.Unavailable, "Agent Profile access manager is disabled")
	}
	records, total, err := s.profileAccessManager.ListAuditEvents(ctx, req.ActorUserId, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	events := make([]*aiAgentv1.AgentProfileRoleAuditEvent, 0, len(records))
	for _, record := range records {
		events = append(events, agentProfileRoleAuditEventToProto(record))
	}
	return &aiAgentv1.ListAgentProfileRoleAuditEventsResponse{Code: 200, Msg: "success", AuditEvents: events, Total: total}, nil
}

func (s *AgentServer) StartAgentProfileExperiment(ctx context.Context, req *aiAgentv1.StartAgentProfileExperimentRequest) (*aiAgentv1.StartAgentProfileExperimentResponse, error) {
	if err := s.authorizeProfileExperiment(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || strings.TrimSpace(req.ProfileId) == "" || req.ExpectedReleaseRevision < 1 {
		return nil, status.Error(codes.InvalidArgument, "actor, profile_id and expected release revision are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleAdmin); err != nil {
		return nil, err
	}
	policyValue, err := profile.NormalizeExperimentPolicy(agentProfileExperimentPolicyFromProto(req.Policy))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	record, err := s.profileExperimentManager.Start(ctx, req.ProfileId, req.ExpectedReleaseRevision, policyValue, req.ActorUserId)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.StartAgentProfileExperimentResponse{Code: 200, Msg: "success", Experiment: agentProfileExperimentToProto(record)}, nil
}

func (s *AgentServer) ListAgentProfileExperiments(ctx context.Context, req *aiAgentv1.ListAgentProfileExperimentsRequest) (*aiAgentv1.ListAgentProfileExperimentsResponse, error) {
	if err := s.authorizeProfileExperiment(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleViewer); err != nil {
		return nil, err
	}
	records, total, err := s.profileExperimentManager.List(ctx, req.ProfileId, req.Status, int(req.Page), int(req.PageSize))
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	experiments := make([]*aiAgentv1.AgentProfileExperiment, 0, len(records))
	for _, record := range records {
		experiments = append(experiments, agentProfileExperimentToProto(record))
	}
	return &aiAgentv1.ListAgentProfileExperimentsResponse{Code: 200, Msg: "success", Experiments: experiments, Total: total}, nil
}

func (s *AgentServer) GetAgentProfileExperiment(ctx context.Context, req *aiAgentv1.GetAgentProfileExperimentRequest) (*aiAgentv1.GetAgentProfileExperimentResponse, error) {
	if err := s.authorizeProfileExperiment(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleViewer); err != nil {
		return nil, err
	}
	experimentID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.ExperimentId))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "experiment_id is invalid")
	}
	record, err := s.profileExperimentManager.Get(ctx, experimentID)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.GetAgentProfileExperimentResponse{Code: 200, Msg: "success", Experiment: agentProfileExperimentToProto(record)}, nil
}

func (s *AgentServer) EvaluateAgentProfileExperiment(ctx context.Context, req *aiAgentv1.EvaluateAgentProfileExperimentRequest) (*aiAgentv1.EvaluateAgentProfileExperimentResponse, error) {
	if err := s.authorizeProfileExperiment(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || req.ExpectedRevision < 1 {
		return nil, status.Error(codes.InvalidArgument, "actor and expected revision are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleAdmin); err != nil {
		return nil, err
	}
	experimentID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.ExperimentId))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "experiment_id is invalid")
	}
	record, err := s.profileExperimentManager.Evaluate(ctx, experimentID, req.ExpectedRevision, req.ActorUserId)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.EvaluateAgentProfileExperimentResponse{Code: 200, Msg: "success", Experiment: agentProfileExperimentToProto(record)}, nil
}

func (s *AgentServer) StopAgentProfileExperiment(ctx context.Context, req *aiAgentv1.StopAgentProfileExperimentRequest) (*aiAgentv1.StopAgentProfileExperimentResponse, error) {
	if err := s.authorizeProfileExperiment(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || req.ExpectedRevision < 1 {
		return nil, status.Error(codes.InvalidArgument, "actor and expected revision are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleAdmin); err != nil {
		return nil, err
	}
	experimentID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.ExperimentId))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "experiment_id is invalid")
	}
	record, err := s.profileExperimentManager.Stop(ctx, experimentID, req.ExpectedRevision, req.ActorUserId)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.StopAgentProfileExperimentResponse{Code: 200, Msg: "success", Experiment: agentProfileExperimentToProto(record)}, nil
}

func (s *AgentServer) RecordAgentProfileExperimentOutcome(ctx context.Context, req *aiAgentv1.RecordAgentProfileExperimentOutcomeRequest) (*aiAgentv1.RecordAgentProfileExperimentOutcomeResponse, error) {
	if err := s.authorizeProfileExperiment(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || strings.TrimSpace(req.EventId) == "" || strings.TrimSpace(req.Signal) == "" {
		return nil, status.Error(codes.InvalidArgument, "actor, experiment, event and outcome signal are required")
	}
	if err := s.authorizeProfileRole(ctx, req.ActorUserId, profile.ManagementRoleAdmin); err != nil {
		return nil, err
	}
	experimentID, err := primitive.ObjectIDFromHex(strings.TrimSpace(req.ExperimentId))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "experiment_id is invalid")
	}
	replay, err := s.profileExperimentManager.RecordOutcome(
		ctx, experimentID, req.EventId, req.Signal, req.Positive, req.ActorUserId,
	)
	if err != nil {
		return nil, profileAdministrationStatus(err)
	}
	return &aiAgentv1.RecordAgentProfileExperimentOutcomeResponse{
		Code: 200, Msg: "success", IdempotentReplay: replay,
	}, nil
}

func (s *AgentServer) authorizeProfileExperiment(ctx context.Context) error {
	if err := s.authorizeProfileAdministration(ctx); err != nil {
		return err
	}
	if s.profileExperimentManager == nil {
		return status.Error(codes.Unavailable, "Agent Profile experiments are disabled")
	}
	return nil
}

func (s *AgentServer) authorizeProfileAdministration(ctx context.Context) error {
	if s == nil || s.profileManager == nil || s.profileAdminToken == "" {
		return status.Error(codes.Unavailable, "Agent Profile administration is disabled")
	}
	values := metadata.ValueFromIncomingContext(ctx, profile.AdminTokenMetadataKey)
	if len(values) == 0 {
		return status.Error(codes.Unauthenticated, "Agent Profile administration credentials are required")
	}
	if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte(s.profileAdminToken)) != 1 {
		return status.Error(codes.PermissionDenied, "Agent Profile administration credentials are invalid")
	}
	return nil
}

func (s *AgentServer) authorizeProfileRole(ctx context.Context, actorUserID uint64, role profile.ManagementRole) error {
	if s == nil || s.profileAccessManager == nil {
		return status.Error(codes.Unavailable, "Agent Profile access manager is disabled")
	}
	if err := s.profileAccessManager.RequireRole(ctx, actorUserID, role); err != nil {
		return profileAdministrationStatus(err)
	}
	return nil
}

func agentProfileFromProto(spec *aiAgentv1.AgentProfileSpec) (profile.AgentProfile, error) {
	if spec.TimeoutMillis < 0 || spec.TimeoutMillis > math.MaxInt64/int64(time.Millisecond) {
		return profile.AgentProfile{}, errors.New("profile timeout_millis is invalid")
	}
	return profile.AgentProfile{
		ID:      spec.ProfileId,
		Version: spec.Version,
		Prompt: profile.PromptProfile{
			ID:           spec.PromptId,
			Version:      spec.PromptVersion,
			SystemPrompt: spec.SystemPrompt,
		},
		Budget: agentRuntime.Budget{
			MaxSteps:               int(spec.MaxSteps),
			MaxInputTokens:         int(spec.MaxInputTokens),
			MaxOutputTokens:        int(spec.MaxOutputTokens),
			MaxTotalTokens:         int(spec.MaxTotalTokens),
			MaxEstimatedCostMicros: spec.MaxEstimatedCostMicros,
			Timeout:                time.Duration(spec.TimeoutMillis) * time.Millisecond,
		},
		AllowedTools: append([]string(nil), spec.AllowedTools...),
	}, nil
}

func (s *AgentServer) agentProfileVersionToProto(record *repository.ProfileVersionRecord) (*aiAgentv1.AgentProfileVersion, error) {
	candidate, err := s.profileManager.DecodeVersion(record)
	if err != nil {
		return nil, err
	}
	return &aiAgentv1.AgentProfileVersion{
		Id: record.ID.Hex(),
		Spec: &aiAgentv1.AgentProfileSpec{
			ProfileId:              candidate.ID,
			Version:                candidate.Version,
			PromptId:               candidate.Prompt.ID,
			PromptVersion:          candidate.Prompt.Version,
			SystemPrompt:           candidate.Prompt.SystemPrompt,
			MaxSteps:               int32(candidate.Budget.MaxSteps),
			MaxInputTokens:         int32(candidate.Budget.MaxInputTokens),
			MaxOutputTokens:        int32(candidate.Budget.MaxOutputTokens),
			MaxTotalTokens:         int32(candidate.Budget.MaxTotalTokens),
			MaxEstimatedCostMicros: candidate.Budget.MaxEstimatedCostMicros,
			TimeoutMillis:          candidate.Budget.Timeout.Milliseconds(),
			AllowedTools:           append([]string(nil), candidate.AllowedTools...),
		},
		Status:       record.Status,
		Revision:     record.Revision,
		CreatedBy:    record.CreatedBy,
		PublishedBy:  record.PublishedBy,
		CreatedAt:    unixMillis(record.CreatedAt),
		UpdatedAt:    unixMillis(record.UpdatedAt),
		PublishedAt:  unixMillis(record.PublishedAt),
		SnapshotHash: record.SnapshotHash,
	}, nil
}

func agentProfileReleaseToProto(record *repository.ProfileReleaseRecord) *aiAgentv1.AgentProfileRelease {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentProfileRelease{
		ProfileId:            record.ProfileID,
		StableVersion:        record.StableVersion,
		CandidateVersion:     record.CandidateVersion,
		CandidateBasisPoints: int32(record.CandidateBasisPoints),
		Salt:                 record.Salt,
		Revision:             record.Revision,
		CreatedBy:            record.CreatedBy,
		UpdatedBy:            record.UpdatedBy,
		CreatedAt:            unixMillis(record.CreatedAt),
		UpdatedAt:            unixMillis(record.UpdatedAt),
	}
}

func agentProfileAuditEventToProto(record *repository.ProfileAuditEvent) *aiAgentv1.AgentProfileAuditEvent {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentProfileAuditEvent{
		Id:              record.ID.Hex(),
		OperationId:     record.OperationID,
		Action:          record.Action,
		Outcome:         record.Outcome,
		ProfileId:       record.ProfileID,
		Version:         record.Version,
		ApprovalId:      record.ApprovalID,
		ExperimentId:    record.ExperimentID,
		ActorUserId:     record.ActorUserID,
		VersionRevision: record.VersionRevision,
		ReleaseRevision: record.ReleaseRevision,
		SnapshotHash:    record.SnapshotHash,
		ErrorCode:       record.ErrorCode,
		CreatedAt:       unixMillis(record.CreatedAt),
	}
}

func agentProfileExperimentPolicyFromProto(policyValue *aiAgentv1.AgentProfileExperimentPolicy) profile.ExperimentPolicy {
	if policyValue == nil {
		return profile.ExperimentPolicy{}
	}
	return profile.ExperimentPolicy{
		MinSamplesPerArm: int(policyValue.MinSamplesPerArm), TargetSamplesPerArm: int(policyValue.TargetSamplesPerArm),
		MaxErrorRateIncreaseBasisPoints:   int(policyValue.MaxErrorRateIncreaseBasisPoints),
		MaxP95LatencyIncreaseBasisPoints:  int(policyValue.MaxP95LatencyIncreaseBasisPoints),
		MaxAverageCostIncreaseBasisPoints: int(policyValue.MaxAverageCostIncreaseBasisPoints),
		OutcomeSignal:                     policyValue.OutcomeSignal,
		MinOutcomeSamplesPerArm:           int(policyValue.MinOutcomeSamplesPerArm),
		MaxOutcomeRateDecreaseBasisPoints: int(policyValue.MaxOutcomeRateDecreaseBasisPoints),
	}
}

func agentProfileExperimentToProto(record *repository.ProfileExperimentRecord) *aiAgentv1.AgentProfileExperiment {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentProfileExperiment{
		ExperimentId: record.ID.Hex(), ProfileId: record.ProfileID,
		StableVersion: record.StableVersion, CandidateVersion: record.CandidateVersion,
		CandidateBasisPoints: int32(record.CandidateBasisPoints), ReleaseRevision: record.ReleaseRevision,
		Policy: &aiAgentv1.AgentProfileExperimentPolicy{
			MinSamplesPerArm: int32(record.Policy.MinSamplesPerArm), TargetSamplesPerArm: int32(record.Policy.TargetSamplesPerArm),
			MaxErrorRateIncreaseBasisPoints:   int32(record.Policy.MaxErrorRateIncreaseBasisPoints),
			MaxP95LatencyIncreaseBasisPoints:  int32(record.Policy.MaxP95LatencyIncreaseBasisPoints),
			MaxAverageCostIncreaseBasisPoints: int32(record.Policy.MaxAverageCostIncreaseBasisPoints),
			OutcomeSignal:                     record.Policy.OutcomeSignal,
			MinOutcomeSamplesPerArm:           int32(record.Policy.MinOutcomeSamplesPerArm),
			MaxOutcomeRateDecreaseBasisPoints: int32(record.Policy.MaxOutcomeRateDecreaseBasisPoints),
		},
		Status: record.Status, Decision: record.Decision, DecisionReason: record.DecisionReason,
		Stats: &aiAgentv1.AgentProfileExperimentStats{
			Stable:    agentProfileExperimentArmStatsToProto(record.Stats.Stable),
			Candidate: agentProfileExperimentArmStatsToProto(record.Stats.Candidate),
		},
		Revision: record.Revision, CreatedBy: record.CreatedBy, UpdatedBy: record.UpdatedBy,
		StartedAt: unixMillis(record.StartedAt), CompletedAt: unixMillis(record.CompletedAt), UpdatedAt: unixMillis(record.UpdatedAt),
	}
}

func agentProfileExperimentArmStatsToProto(stats profile.ExperimentArmStats) *aiAgentv1.AgentProfileExperimentArmStats {
	return &aiAgentv1.AgentProfileExperimentArmStats{
		Samples: int32(stats.Samples), Successes: int32(stats.Successes), Failures: int32(stats.Failures),
		ErrorRateBasisPoints: int32(stats.ErrorRateBPS), P95LatencyMillis: stats.P95LatencyMS,
		AverageCostMicros: stats.AverageCostMicros,
		OutcomeSamples:    int32(stats.OutcomeSamples), OutcomePositives: int32(stats.OutcomePositives),
		OutcomeRateBasisPoints: int32(stats.OutcomeRateBPS),
	}
}

func agentProfilePublishApprovalToProto(record *repository.ProfilePublishApprovalRecord) *aiAgentv1.AgentProfilePublishApproval {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentProfilePublishApproval{
		ApprovalId: record.ID.Hex(), ProfileId: record.ProfileID, Version: record.Version,
		SnapshotHash: record.SnapshotHash, ExpectedVersionRevision: record.ExpectedVersionRevision,
		Status: record.Status, Decision: record.Decision, Reason: record.Reason, Revision: record.Revision,
		RequestedBy: record.RequestedBy, DecidedBy: record.DecidedBy, ApplyingBy: record.ApplyingBy,
		ErrorCode: record.ErrorCode, RequestedAt: unixMillis(record.RequestedAt), DecidedAt: unixMillis(record.DecidedAt),
		ApplyLeaseUntil: unixMillis(record.ApplyLeaseUntil), AppliedAt: unixMillis(record.AppliedAt), UpdatedAt: unixMillis(record.UpdatedAt),
		QualityEvidence: agentProfileQualityEvidenceToProto(record.QualityEvidence),
	}
}

func agentQualityEvidenceReferenceFromProto(reference *aiAgentv1.AgentEvalEvidenceReference) profile.QualityEvidenceReference {
	if reference == nil {
		return profile.QualityEvidenceReference{}
	}
	return profile.QualityEvidenceReference{
		Storage: reference.Storage, Bucket: reference.Bucket, Key: reference.Key,
		VersionID: reference.VersionId, ETag: reference.Etag,
		ReportSHA256: reference.ReportSha256, Length: int(reference.Length), ContentType: reference.ContentType,
		RetentionMode: reference.RetentionMode, RetainUntil: timeFromUnixMillis(reference.RetainUntil),
		ArchivedAt: timeFromUnixMillis(reference.ArchivedAt), DatasetVersion: reference.DatasetVersion,
		DatasetSHA256: reference.DatasetSha256, ExecutionConfigHash: reference.ExecutionConfigSha256,
		IntegrityKeyID: reference.IntegrityKeyId,
	}
}

func agentProfileQualityEvidenceToProto(evidence *profile.QualityEvidence) *aiAgentv1.AgentProfileQualityEvidence {
	if evidence == nil {
		return nil
	}
	reference := evidence.Reference
	return &aiAgentv1.AgentProfileQualityEvidence{
		Reference: &aiAgentv1.AgentEvalEvidenceReference{
			Storage: reference.Storage, Bucket: reference.Bucket, Key: reference.Key,
			VersionId: reference.VersionID, Etag: reference.ETag,
			ReportSha256: reference.ReportSHA256, Length: int32(reference.Length), ContentType: reference.ContentType,
			RetentionMode: reference.RetentionMode, RetainUntil: unixMillis(reference.RetainUntil),
			ArchivedAt: unixMillis(reference.ArchivedAt), DatasetVersion: reference.DatasetVersion,
			DatasetSha256: reference.DatasetSHA256, ExecutionConfigSha256: reference.ExecutionConfigHash,
			IntegrityKeyId: reference.IntegrityKeyID,
		},
		ProfileId: evidence.ProfileID, ProfileVersion: evidence.ProfileVersion, GateStatus: evidence.GateStatus,
		Cases: int32(evidence.Cases), Passed: int32(evidence.Passed),
		TaskCompletionRateBps:        int32(evidence.TaskCompletionRateBPS),
		ReadToolSelectionAccuracyBps: int32(evidence.ReadToolSelectionAccuracyBPS),
		SemanticPassRateBps:          int32(evidence.SemanticPassRateBPS), ApprovalPassRateBps: int32(evidence.ApprovalPassRateBPS),
		ReportSignedAt: unixMillis(evidence.ReportSignedAt), VerifiedAt: unixMillis(evidence.VerifiedAt),
	}
}

func agentProfileRoleBindingToProto(record *repository.ProfileRoleBindingRecord) *aiAgentv1.AgentProfileRoleBinding {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentProfileRoleBinding{
		UserId: record.UserID, Roles: append([]string(nil), record.Roles...), Revision: record.Revision,
		CreatedBy: record.CreatedBy, UpdatedBy: record.UpdatedBy,
		CreatedAt: unixMillis(record.CreatedAt), UpdatedAt: unixMillis(record.UpdatedAt),
	}
}

func agentProfileRoleAuditEventToProto(record *repository.ProfileRoleAuditEvent) *aiAgentv1.AgentProfileRoleAuditEvent {
	if record == nil {
		return nil
	}
	return &aiAgentv1.AgentProfileRoleAuditEvent{
		Id: record.ID.Hex(), OperationId: record.OperationID, Action: record.Action, Outcome: record.Outcome,
		ActorUserId: record.ActorUserID, SubjectUserId: record.SubjectUserID,
		Roles: append([]string(nil), record.Roles...), Revision: record.Revision,
		ErrorCode: record.ErrorCode, CreatedAt: unixMillis(record.CreatedAt),
	}
}

func profileAdministrationStatus(err error) error {
	switch {
	case errors.Is(err, profile.ErrInvalidManagementRole):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, repository.ErrProfileVersionNotFound), errors.Is(err, repository.ErrProfileReleaseNotFound), errors.Is(err, repository.ErrProfilePublishApprovalNotFound), errors.Is(err, repository.ErrProfileExperimentNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, repository.ErrProfileRoleBindingNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, repository.ErrProfileVersionConflict), errors.Is(err, repository.ErrProfileReleaseConflict), errors.Is(err, repository.ErrProfilePublishApprovalConflict), errors.Is(err, repository.ErrProfileRoleBindingConflict), errors.Is(err, repository.ErrProfileExperimentConflict), errors.Is(err, repository.ErrProfileExperimentAlreadyRunning), errors.Is(err, repository.ErrProfileExperimentOutcomeConflict):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, repository.ErrProfilePublishSelfApproval), errors.Is(err, service.ErrProfileAccessForbidden), errors.Is(err, service.ErrProfileAdminRoleRequiresRoot):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, service.ErrProfilePublishApprovalUnavailable), errors.Is(err, service.ErrProfileDynamicRBACUnavailable), errors.Is(err, service.ErrProfileExperimentDisabled), errors.Is(err, profile.ErrQualityEvidenceUnavailable):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, repository.ErrProfileExperimentObservationNotFound), errors.Is(err, service.ErrProfileExperimentOutcomeNotConfigured), errors.Is(err, service.ErrProfileExperimentOutcomeSignalMismatch):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Errorf(codes.FailedPrecondition, "Agent Profile operation failed: %v", err)
	}
}

func unixMillis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

func timeFromUnixMillis(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
