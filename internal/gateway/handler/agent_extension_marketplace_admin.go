package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/gateway/middleware"
	"twitter-clone/internal/module/agent/marketplace"
)

type registerAgentMarketplacePublisherBody struct {
	PublisherID     string   `json:"publisher_id" binding:"required"`
	DisplayName     string   `json:"display_name" binding:"required"`
	OwnerUserIDs    []string `json:"owner_user_ids" binding:"required"`
	InitialKeyID    string   `json:"initial_key_id" binding:"required"`
	PublicKeyBase64 string   `json:"public_key_base64" binding:"required"`
}

type rotateAgentMarketplaceKeyBody struct {
	KeyID            string `json:"key_id" binding:"required"`
	PublicKeyBase64  string `json:"public_key_base64" binding:"required"`
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
}

type revokeAgentMarketplaceKeyBody struct {
	ExpectedRevision int64 `json:"expected_revision" binding:"required"`
}

type setAgentMarketplaceVerificationBody struct {
	Verification     string `json:"verification" binding:"required"`
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
}

type publishAgentMarketplaceReleaseBody struct {
	Manifest struct {
		ContractVersion      string   `json:"contract_version" binding:"required"`
		PackageID            string   `json:"package_id" binding:"required"`
		Kind                 string   `json:"kind" binding:"required"`
		Version              string   `json:"version" binding:"required"`
		PublisherID          string   `json:"publisher_id" binding:"required"`
		DisplayName          string   `json:"display_name" binding:"required"`
		Description          string   `json:"description"`
		ArtifactDigestSHA256 string   `json:"artifact_digest_sha256" binding:"required"`
		CapabilityIDs        []string `json:"capability_ids" binding:"required"`
		RequestedPermissions []string `json:"requested_permissions"`
	} `json:"manifest" binding:"required"`
	SignatureKeyID            string `json:"signature_key_id" binding:"required"`
	SignatureBase64           string `json:"signature_base64" binding:"required"`
	ExpectedPublisherRevision int64  `json:"expected_publisher_revision" binding:"required"`
}

type withdrawAgentMarketplaceReleaseBody struct {
	ReasonCode       string `json:"reason_code" binding:"required"`
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
}

func (h *AgentHandler) GetAgentMarketplaceManagementAccess(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 10*time.Second)
	if !ok {
		return
	}
	defer cancel()
	response, err := h.agentClient.GetAgentMarketplaceManagementAccess(ctx, &aiAgentv1.GetAgentMarketplaceManagementAccessRequest{ActorUserId: actorUserID})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	access := response.Access
	if access == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "extension marketplace access is unavailable"})
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"contract_version": access.ContractVersion, "enabled": access.Enabled,
		"platform_admin": access.PlatformAdmin, "owned_publisher_ids": access.OwnedPublisherIds,
	})
}

func (h *AgentHandler) ListAgentMarketplacePublishers(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 15*time.Second)
	if !ok {
		return
	}
	defer cancel()
	page, pageSize, ok := parseMarketplaceManagementPage(c)
	if !ok {
		return
	}
	response, err := h.agentClient.ListAgentMarketplacePublishers(ctx, &aiAgentv1.ListAgentMarketplacePublishersRequest{
		ActorUserId: actorUserID, Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	items := make([]gin.H, 0, len(response.Publishers))
	for _, publisher := range response.Publishers {
		items = append(items, agentMarketplaceManagedPublisherToJSON(publisher))
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"publishers": items, "total": response.Total})
}

func (h *AgentHandler) RegisterAgentMarketplacePublisher(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 15*time.Second)
	if !ok {
		return
	}
	defer cancel()
	var body registerAgentMarketplacePublisherBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	owners, err := parseMarketplaceOwnerUserIDs(body.OwnerUserIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response, err := h.agentClient.RegisterAgentMarketplacePublisher(ctx, &aiAgentv1.RegisterAgentMarketplacePublisherRequest{
		ActorUserId: actorUserID, PublisherId: body.PublisherID, DisplayName: body.DisplayName,
		OwnerUserIds: owners, InitialKeyId: body.InitialKeyID, PublicKeyBase64: body.PublicKeyBase64,
	})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusCreated, gin.H{"publisher": agentMarketplaceManagedPublisherToJSON(response.Publisher)})
}

func (h *AgentHandler) RotateAgentMarketplacePublisherKey(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 15*time.Second)
	if !ok {
		return
	}
	defer cancel()
	var body rotateAgentMarketplaceKeyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response, err := h.agentClient.RotateAgentMarketplacePublisherKey(ctx, &aiAgentv1.RotateAgentMarketplacePublisherKeyRequest{
		ActorUserId: actorUserID, PublisherId: c.Param("publisher_id"), KeyId: body.KeyID,
		PublicKeyBase64: body.PublicKeyBase64, ExpectedRevision: body.ExpectedRevision,
	})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"publisher": agentMarketplaceManagedPublisherToJSON(response.Publisher)})
}

func (h *AgentHandler) RevokeAgentMarketplacePublisherKey(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 15*time.Second)
	if !ok {
		return
	}
	defer cancel()
	var body revokeAgentMarketplaceKeyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response, err := h.agentClient.RevokeAgentMarketplacePublisherKey(ctx, &aiAgentv1.RevokeAgentMarketplacePublisherKeyRequest{
		ActorUserId: actorUserID, PublisherId: c.Param("publisher_id"), KeyId: c.Param("key_id"),
		ExpectedRevision: body.ExpectedRevision,
	})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"publisher": agentMarketplaceManagedPublisherToJSON(response.Publisher)})
}

func (h *AgentHandler) SetAgentMarketplacePublisherVerification(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 15*time.Second)
	if !ok {
		return
	}
	defer cancel()
	var body setAgentMarketplaceVerificationBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response, err := h.agentClient.SetAgentMarketplacePublisherVerification(ctx, &aiAgentv1.SetAgentMarketplacePublisherVerificationRequest{
		ActorUserId: actorUserID, PublisherId: c.Param("publisher_id"),
		Verification: body.Verification, ExpectedRevision: body.ExpectedRevision,
	})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"publisher": agentMarketplaceManagedPublisherToJSON(response.Publisher)})
}

func (h *AgentHandler) ListAgentMarketplaceManagedReleases(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 15*time.Second)
	if !ok {
		return
	}
	defer cancel()
	page, pageSize, ok := parseMarketplaceManagementPage(c)
	if !ok {
		return
	}
	response, err := h.agentClient.ListAgentMarketplaceManagedReleases(ctx, &aiAgentv1.ListAgentMarketplaceManagedReleasesRequest{
		ActorUserId: actorUserID, PublisherId: strings.TrimSpace(c.Query("publisher_id")),
		Status: strings.TrimSpace(c.Query("status")), Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	items := make([]gin.H, 0, len(response.Releases))
	for _, release := range response.Releases {
		items = append(items, agentMarketplaceManagedReleaseToJSON(release))
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"releases": items, "total": response.Total})
}

func (h *AgentHandler) PublishAgentMarketplaceRelease(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 20*time.Second)
	if !ok {
		return
	}
	defer cancel()
	var body publishAgentMarketplaceReleaseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response, err := h.agentClient.PublishAgentMarketplaceRelease(ctx, &aiAgentv1.PublishAgentMarketplaceReleaseRequest{
		ActorUserId: actorUserID,
		Manifest: &aiAgentv1.AgentMarketplaceManifest{
			ContractVersion: body.Manifest.ContractVersion, PackageId: body.Manifest.PackageID,
			Kind: body.Manifest.Kind, Version: body.Manifest.Version, PublisherId: body.Manifest.PublisherID,
			DisplayName: body.Manifest.DisplayName, Description: body.Manifest.Description,
			ArtifactDigestSha256: body.Manifest.ArtifactDigestSHA256,
			CapabilityIds:        append([]string(nil), body.Manifest.CapabilityIDs...),
			RequestedPermissions: append([]string(nil), body.Manifest.RequestedPermissions...),
		},
		SignatureKeyId: body.SignatureKeyID, SignatureBase64: body.SignatureBase64,
		ExpectedPublisherRevision: body.ExpectedPublisherRevision,
	})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusCreated, gin.H{"release": agentMarketplaceManagedReleaseToJSON(response.Release)})
}

func (h *AgentHandler) WithdrawAgentMarketplaceRelease(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 15*time.Second)
	if !ok {
		return
	}
	defer cancel()
	var body withdrawAgentMarketplaceReleaseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response, err := h.agentClient.WithdrawAgentMarketplaceRelease(ctx, &aiAgentv1.WithdrawAgentMarketplaceReleaseRequest{
		ActorUserId: actorUserID, ReleaseId: c.Param("release_id"),
		ReasonCode: body.ReasonCode, ExpectedRevision: body.ExpectedRevision,
	})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"release": agentMarketplaceManagedReleaseToJSON(response.Release)})
}

func (h *AgentHandler) ListAgentMarketplaceAuditEvents(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.extensionMarketplaceManagementContext(c, 15*time.Second)
	if !ok {
		return
	}
	defer cancel()
	page, pageSize, ok := parseMarketplaceManagementPage(c)
	if !ok {
		return
	}
	response, err := h.agentClient.ListAgentMarketplaceAuditEvents(ctx, &aiAgentv1.ListAgentMarketplaceAuditEventsRequest{
		ActorUserId: actorUserID, PublisherId: strings.TrimSpace(c.Query("publisher_id")),
		Action: strings.TrimSpace(c.Query("action")), Outcome: strings.TrimSpace(c.Query("outcome")),
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeExtensionMarketplaceAdministrationError(c, err)
		return
	}
	items := make([]gin.H, 0, len(response.Events))
	for _, event := range response.Events {
		items = append(items, agentMarketplaceAuditToJSON(event))
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"events": items, "total": response.Total})
}

func (h *AgentHandler) extensionMarketplaceManagementContext(
	c *gin.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc, uint64, bool) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, nil, 0, false
	}
	if h.extensionMarketplaceAdminToken == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "extension marketplace administration is disabled"})
		return nil, nil, 0, false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	ctx = metadata.AppendToOutgoingContext(ctx, marketplace.AdminTokenMetadataKey, h.extensionMarketplaceAdminToken)
	setSensitiveResponseHeaders(c)
	return ctx, cancel, userID, true
}

func parseMarketplaceManagementPage(c *gin.Context) (int32, int32, bool) {
	parse := func(name string, fallback int64) (int32, bool) {
		raw := strings.TrimSpace(c.Query(name))
		if raw == "" {
			return int32(fallback), true
		}
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || value < 1 || (name == "page_size" && value > marketplace.MaxManagementPageSize) {
			c.JSON(http.StatusBadRequest, gin.H{"error": name + " is invalid"})
			return 0, false
		}
		return int32(value), true
	}
	page, ok := parse("page", 1)
	if !ok {
		return 0, 0, false
	}
	pageSize, ok := parse("page_size", marketplace.DefaultManagementPageSize)
	return page, pageSize, ok
}

func parseMarketplaceOwnerUserIDs(values []string) ([]uint64, error) {
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
		if err != nil || parsed == 0 {
			return nil, strconv.ErrSyntax
		}
		result = append(result, parsed)
	}
	return result, nil
}

func agentMarketplaceManagedPublisherToJSON(item *aiAgentv1.AgentMarketplaceManagedPublisher) gin.H {
	if item == nil {
		return gin.H{}
	}
	keys := make([]gin.H, 0, len(item.SigningKeys))
	for _, key := range item.SigningKeys {
		if key != nil {
			keys = append(keys, gin.H{"key_id": key.KeyId, "algorithm": key.Algorithm, "public_key_base64": key.PublicKeyBase64, "status": key.Status})
		}
	}
	owners := make([]string, 0, len(item.OwnerUserIds))
	for _, owner := range item.OwnerUserIds {
		owners = append(owners, strconv.FormatUint(owner, 10))
	}
	return gin.H{
		"contract_version": item.ContractVersion, "publisher_id": item.PublisherId,
		"display_name": item.DisplayName, "verification": item.Verification,
		"signing_keys": keys, "owner_user_ids": owners, "revision": item.Revision,
		"created_by": strconv.FormatUint(item.CreatedBy, 10), "updated_by": strconv.FormatUint(item.UpdatedBy, 10),
		"verified_at_unix_ms": item.VerifiedAtUnixMs, "created_at_unix_ms": item.CreatedAtUnixMs,
		"updated_at_unix_ms": item.UpdatedAtUnixMs,
	}
}

func agentMarketplaceManagedReleaseToJSON(item *aiAgentv1.AgentMarketplaceManagedRelease) gin.H {
	if item == nil {
		return gin.H{}
	}
	manifest := gin.H{}
	if item.Manifest != nil {
		manifest = gin.H{
			"contract_version": item.Manifest.ContractVersion, "package_id": item.Manifest.PackageId,
			"kind": item.Manifest.Kind, "version": item.Manifest.Version, "publisher_id": item.Manifest.PublisherId,
			"display_name": item.Manifest.DisplayName, "description": item.Manifest.Description,
			"artifact_digest_sha256": item.Manifest.ArtifactDigestSha256,
			"capability_ids":         item.Manifest.CapabilityIds, "requested_permissions": item.Manifest.RequestedPermissions,
		}
	}
	return gin.H{
		"contract_version": item.ContractVersion, "release_id": item.ReleaseId, "manifest": manifest,
		"signature_key_id": item.SignatureKeyId, "status": item.Status, "revision": item.Revision,
		"published_by": strconv.FormatUint(item.PublishedBy, 10), "withdrawn_by": strconv.FormatUint(item.WithdrawnBy, 10),
		"withdrawal_reason_code": item.WithdrawalReasonCode, "published_at_unix_ms": item.PublishedAtUnixMs,
		"withdrawn_at_unix_ms": item.WithdrawnAtUnixMs, "created_at_unix_ms": item.CreatedAtUnixMs,
		"updated_at_unix_ms": item.UpdatedAtUnixMs,
	}
}

func agentMarketplaceAuditToJSON(item *aiAgentv1.AgentMarketplaceAuditEvent) gin.H {
	if item == nil {
		return gin.H{}
	}
	return gin.H{
		"contract_version": item.ContractVersion, "event_id": item.EventId, "operation_id": item.OperationId,
		"action": item.Action, "outcome": item.Outcome, "actor_user_id": strconv.FormatUint(item.ActorUserId, 10),
		"publisher_id": item.PublisherId, "package_id": item.PackageId, "version": item.Version,
		"key_id": item.KeyId, "revision": item.Revision, "reason_code": item.ReasonCode,
		"error_code": item.ErrorCode, "created_at_unix_ms": item.CreatedAtUnixMs,
	}
}

func writeExtensionMarketplaceAdministrationError(c *gin.Context, err error) {
	message := status.Convert(err).Message()
	switch status.Code(err) {
	case codes.Unauthenticated:
		c.JSON(http.StatusUnauthorized, gin.H{"error": message})
	case codes.PermissionDenied:
		c.JSON(http.StatusForbidden, gin.H{"error": message})
	case codes.InvalidArgument:
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
	case codes.NotFound:
		c.JSON(http.StatusNotFound, gin.H{"error": message})
	case codes.Aborted, codes.AlreadyExists:
		c.JSON(http.StatusConflict, gin.H{"error": message})
	case codes.Unavailable:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": message})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": message})
	}
}
