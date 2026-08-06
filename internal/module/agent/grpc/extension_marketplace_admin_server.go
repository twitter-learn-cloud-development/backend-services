package grpc

import (
	"context"
	"crypto/subtle"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/module/agent/marketplace"
	"twitter-clone/internal/module/agent/service"
)

func (s *AgentServer) GetAgentMarketplaceManagementAccess(
	ctx context.Context,
	req *aiAgentv1.GetAgentMarketplaceManagementAccessRequest,
) (*aiAgentv1.GetAgentMarketplaceManagementAccessResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	access, err := s.extensionMarketplaceManager.ResolveAccess(ctx, req.ActorUserId)
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	return &aiAgentv1.GetAgentMarketplaceManagementAccessResponse{
		Code: 200, Msg: "success",
		Access: &aiAgentv1.AgentMarketplaceManagementAccess{
			ContractVersion: marketplace.ControlContractVersion,
			Enabled:         access.Enabled, PlatformAdmin: access.PlatformAdmin,
			OwnedPublisherIds: append([]string(nil), access.OwnedPublisherIDs...),
		},
	}, nil
}

func (s *AgentServer) ListAgentMarketplacePublishers(
	ctx context.Context,
	req *aiAgentv1.ListAgentMarketplacePublishersRequest,
) (*aiAgentv1.ListAgentMarketplacePublishersResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	records, total, err := s.extensionMarketplaceManager.ListPublishers(
		ctx, req.ActorUserId, marketplace.ManagementPage{Page: int(req.Page), PageSize: int(req.PageSize)},
	)
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	items := make([]*aiAgentv1.AgentMarketplaceManagedPublisher, 0, len(records))
	for _, record := range records {
		items = append(items, extensionMarketplacePublisherToProto(record))
	}
	return &aiAgentv1.ListAgentMarketplacePublishersResponse{Code: 200, Msg: "success", Publishers: items, Total: total}, nil
}

func (s *AgentServer) RegisterAgentMarketplacePublisher(
	ctx context.Context,
	req *aiAgentv1.RegisterAgentMarketplacePublisherRequest,
) (*aiAgentv1.RegisterAgentMarketplacePublisherResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	record, err := s.extensionMarketplaceManager.RegisterPublisher(ctx, req.ActorUserId, service.RegisterExtensionPublisherRequest{
		PublisherID: req.PublisherId, DisplayName: req.DisplayName,
		OwnerUserIDs: append([]uint64(nil), req.OwnerUserIds...), InitialKeyID: req.InitialKeyId,
		PublicKeyBase64: req.PublicKeyBase64,
	})
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	return &aiAgentv1.RegisterAgentMarketplacePublisherResponse{
		Code: 200, Msg: "success", Publisher: extensionMarketplacePublisherToProto(record),
	}, nil
}

func (s *AgentServer) RotateAgentMarketplacePublisherKey(
	ctx context.Context,
	req *aiAgentv1.RotateAgentMarketplacePublisherKeyRequest,
) (*aiAgentv1.RotateAgentMarketplacePublisherKeyResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	record, err := s.extensionMarketplaceManager.RotatePublisherKey(ctx, req.ActorUserId, req.PublisherId, marketplace.SigningKey{
		KeyID: req.KeyId, Algorithm: marketplace.SignatureAlgorithmEd25519,
		PublicKeyBase64: req.PublicKeyBase64, Status: marketplace.KeyActive,
	}, req.ExpectedRevision)
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	return &aiAgentv1.RotateAgentMarketplacePublisherKeyResponse{
		Code: 200, Msg: "success", Publisher: extensionMarketplacePublisherToProto(record),
	}, nil
}

func (s *AgentServer) RevokeAgentMarketplacePublisherKey(
	ctx context.Context,
	req *aiAgentv1.RevokeAgentMarketplacePublisherKeyRequest,
) (*aiAgentv1.RevokeAgentMarketplacePublisherKeyResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	record, err := s.extensionMarketplaceManager.RevokePublisherKey(
		ctx, req.ActorUserId, req.PublisherId, req.KeyId, req.ExpectedRevision,
	)
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	return &aiAgentv1.RevokeAgentMarketplacePublisherKeyResponse{
		Code: 200, Msg: "success", Publisher: extensionMarketplacePublisherToProto(record),
	}, nil
}

func (s *AgentServer) SetAgentMarketplacePublisherVerification(
	ctx context.Context,
	req *aiAgentv1.SetAgentMarketplacePublisherVerificationRequest,
) (*aiAgentv1.SetAgentMarketplacePublisherVerificationResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	record, err := s.extensionMarketplaceManager.SetPublisherVerification(
		ctx, req.ActorUserId, req.PublisherId, req.Verification, req.ExpectedRevision,
	)
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	return &aiAgentv1.SetAgentMarketplacePublisherVerificationResponse{
		Code: 200, Msg: "success", Publisher: extensionMarketplacePublisherToProto(record),
	}, nil
}

func (s *AgentServer) ListAgentMarketplaceManagedReleases(
	ctx context.Context,
	req *aiAgentv1.ListAgentMarketplaceManagedReleasesRequest,
) (*aiAgentv1.ListAgentMarketplaceManagedReleasesResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	records, total, err := s.extensionMarketplaceManager.ListReleases(
		ctx, req.ActorUserId, req.PublisherId, req.Status,
		marketplace.ManagementPage{Page: int(req.Page), PageSize: int(req.PageSize)},
	)
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	items := make([]*aiAgentv1.AgentMarketplaceManagedRelease, 0, len(records))
	for _, record := range records {
		items = append(items, extensionMarketplaceReleaseToProto(record))
	}
	return &aiAgentv1.ListAgentMarketplaceManagedReleasesResponse{Code: 200, Msg: "success", Releases: items, Total: total}, nil
}

func (s *AgentServer) PublishAgentMarketplaceRelease(
	ctx context.Context,
	req *aiAgentv1.PublishAgentMarketplaceReleaseRequest,
) (*aiAgentv1.PublishAgentMarketplaceReleaseResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 || req.Manifest == nil {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id and manifest are required")
	}
	record, err := s.extensionMarketplaceManager.PublishRelease(ctx, req.ActorUserId, service.PublishExtensionReleaseRequest{
		Manifest:       extensionMarketplaceManifestFromProto(req.Manifest),
		SignatureKeyID: req.SignatureKeyId, SignatureBase64: req.SignatureBase64,
		ExpectedPublisherRevision: req.ExpectedPublisherRevision,
	})
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	return &aiAgentv1.PublishAgentMarketplaceReleaseResponse{
		Code: 200, Msg: "success", Release: extensionMarketplaceReleaseToProto(record),
	}, nil
}

func (s *AgentServer) WithdrawAgentMarketplaceRelease(
	ctx context.Context,
	req *aiAgentv1.WithdrawAgentMarketplaceReleaseRequest,
) (*aiAgentv1.WithdrawAgentMarketplaceReleaseResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	record, err := s.extensionMarketplaceManager.WithdrawRelease(
		ctx, req.ActorUserId, req.ReleaseId, req.ReasonCode, req.ExpectedRevision,
	)
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	return &aiAgentv1.WithdrawAgentMarketplaceReleaseResponse{
		Code: 200, Msg: "success", Release: extensionMarketplaceReleaseToProto(record),
	}, nil
}

func (s *AgentServer) ListAgentMarketplaceAuditEvents(
	ctx context.Context,
	req *aiAgentv1.ListAgentMarketplaceAuditEventsRequest,
) (*aiAgentv1.ListAgentMarketplaceAuditEventsResponse, error) {
	if err := s.authorizeExtensionMarketplaceAdministration(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.ActorUserId == 0 {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id is required")
	}
	records, total, err := s.extensionMarketplaceManager.ListAuditEvents(
		ctx, req.ActorUserId, req.PublisherId, req.Action, req.Outcome,
		marketplace.ManagementPage{Page: int(req.Page), PageSize: int(req.PageSize)},
	)
	if err != nil {
		return nil, extensionMarketplaceAdministrationStatus(err)
	}
	items := make([]*aiAgentv1.AgentMarketplaceAuditEvent, 0, len(records))
	for _, record := range records {
		items = append(items, extensionMarketplaceAuditToProto(record))
	}
	return &aiAgentv1.ListAgentMarketplaceAuditEventsResponse{Code: 200, Msg: "success", Events: items, Total: total}, nil
}

func (s *AgentServer) authorizeExtensionMarketplaceAdministration(ctx context.Context) error {
	if s == nil || s.extensionMarketplaceManager == nil || s.extensionMarketplaceAdminToken == "" {
		return status.Error(codes.Unavailable, "extension marketplace administration is disabled")
	}
	values := metadata.ValueFromIncomingContext(ctx, marketplace.AdminTokenMetadataKey)
	if len(values) == 0 {
		return status.Error(codes.Unauthenticated, "extension marketplace administration credentials are required")
	}
	if len(values) != 1 || subtle.ConstantTimeCompare([]byte(values[0]), []byte(s.extensionMarketplaceAdminToken)) != 1 {
		return status.Error(codes.PermissionDenied, "extension marketplace administration credentials are invalid")
	}
	return nil
}

func extensionMarketplacePublisherToProto(record marketplace.PublisherControl) *aiAgentv1.AgentMarketplaceManagedPublisher {
	keys := make([]*aiAgentv1.AgentMarketplaceSigningKey, 0, len(record.Publisher.SigningKeys))
	for _, key := range record.Publisher.SigningKeys {
		keys = append(keys, &aiAgentv1.AgentMarketplaceSigningKey{
			KeyId: key.KeyID, Algorithm: key.Algorithm,
			PublicKeyBase64: key.PublicKeyBase64, Status: key.Status,
		})
	}
	return &aiAgentv1.AgentMarketplaceManagedPublisher{
		ContractVersion: marketplace.ControlContractVersion,
		PublisherId:     record.Publisher.PublisherID, DisplayName: record.Publisher.DisplayName,
		Verification: record.Publisher.Verification, SigningKeys: keys,
		OwnerUserIds: append([]uint64(nil), record.OwnerUserIDs...), Revision: record.Revision,
		CreatedBy: record.CreatedBy, UpdatedBy: record.UpdatedBy,
		VerifiedAtUnixMs: unixMillis(record.Publisher.VerifiedAt),
		CreatedAtUnixMs:  unixMillis(record.CreatedAt), UpdatedAtUnixMs: unixMillis(record.UpdatedAt),
	}
}

func extensionMarketplaceManifestFromProto(value *aiAgentv1.AgentMarketplaceManifest) marketplace.Manifest {
	if value == nil {
		return marketplace.Manifest{}
	}
	return marketplace.Manifest{
		ContractVersion: value.ContractVersion, PackageID: value.PackageId, Kind: value.Kind,
		Version: value.Version, PublisherID: value.PublisherId, DisplayName: value.DisplayName,
		Description: value.Description, ArtifactDigestSHA256: value.ArtifactDigestSha256,
		CapabilityIDs:        append([]string(nil), value.CapabilityIds...),
		RequestedPermissions: append([]string(nil), value.RequestedPermissions...),
	}
}

func extensionMarketplaceManifestToProto(value marketplace.Manifest) *aiAgentv1.AgentMarketplaceManifest {
	return &aiAgentv1.AgentMarketplaceManifest{
		ContractVersion: value.ContractVersion, PackageId: value.PackageID, Kind: value.Kind,
		Version: value.Version, PublisherId: value.PublisherID, DisplayName: value.DisplayName,
		Description: value.Description, ArtifactDigestSha256: value.ArtifactDigestSHA256,
		CapabilityIds:        append([]string(nil), value.CapabilityIDs...),
		RequestedPermissions: append([]string(nil), value.RequestedPermissions...),
	}
}

func extensionMarketplaceReleaseToProto(record marketplace.ReleaseControl) *aiAgentv1.AgentMarketplaceManagedRelease {
	return &aiAgentv1.AgentMarketplaceManagedRelease{
		ContractVersion: marketplace.ControlContractVersion, ReleaseId: record.Release.ReleaseID,
		Manifest:       extensionMarketplaceManifestToProto(record.Release.Manifest),
		SignatureKeyId: record.Release.SignatureKeyID, Status: record.Release.Status,
		Revision: record.Revision, PublishedBy: record.PublishedBy, WithdrawnBy: record.WithdrawnBy,
		WithdrawalReasonCode: record.WithdrawalReasonCode,
		PublishedAtUnixMs:    unixMillis(record.Release.PublishedAt), WithdrawnAtUnixMs: unixMillis(record.WithdrawnAt),
		CreatedAtUnixMs: unixMillis(record.CreatedAt), UpdatedAtUnixMs: unixMillis(record.UpdatedAt),
	}
}

func extensionMarketplaceAuditToProto(record marketplace.AuditEvent) *aiAgentv1.AgentMarketplaceAuditEvent {
	return &aiAgentv1.AgentMarketplaceAuditEvent{
		ContractVersion: marketplace.AuditContractVersion, EventId: record.EventID,
		OperationId: record.OperationID, Action: record.Action, Outcome: record.Outcome,
		ActorUserId: record.ActorUserID, PublisherId: record.PublisherID,
		PackageId: record.PackageID, Version: record.Version, KeyId: record.KeyID,
		Revision: record.Revision, ReasonCode: record.ReasonCode,
		ErrorCode: record.ErrorCode, CreatedAtUnixMs: unixMillis(record.CreatedAt),
	}
}

func extensionMarketplaceAdministrationStatus(err error) error {
	switch {
	case errors.Is(err, marketplace.ErrControlDisabled):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, marketplace.ErrControlForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, marketplace.ErrPublisherNotFound), errors.Is(err, marketplace.ErrReleaseNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, marketplace.ErrPublisherConflict), errors.Is(err, marketplace.ErrReleaseConflict),
		errors.Is(err, marketplace.ErrRevisionConflict):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, marketplace.ErrInvalidPublisher), errors.Is(err, marketplace.ErrInvalidManifest),
		errors.Is(err, marketplace.ErrInvalidRelease), errors.Is(err, marketplace.ErrInvalidControlRecord),
		errors.Is(err, marketplace.ErrInvalidAuditEvent), errors.Is(err, marketplace.ErrInvalidWithdrawal),
		errors.Is(err, marketplace.ErrInvalidQuery), errors.Is(err, marketplace.ErrSignatureVerification):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Errorf(codes.Internal, "extension marketplace administration failed: %v", err)
	}
}
