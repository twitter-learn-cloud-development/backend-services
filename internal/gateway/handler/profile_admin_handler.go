package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/gateway/middleware"
	"twitter-clone/internal/module/agent/profile"
)

type agentProfileSpecRequest struct {
	ProfileID              string   `json:"profile_id" binding:"required"`
	Version                string   `json:"version" binding:"required"`
	PromptID               string   `json:"prompt_id" binding:"required"`
	PromptVersion          string   `json:"prompt_version" binding:"required"`
	SystemPrompt           string   `json:"system_prompt" binding:"required"`
	MaxSteps               int32    `json:"max_steps" binding:"required"`
	MaxInputTokens         int32    `json:"max_input_tokens" binding:"required"`
	MaxOutputTokens        int32    `json:"max_output_tokens" binding:"required"`
	MaxTotalTokens         int32    `json:"max_total_tokens" binding:"required"`
	MaxEstimatedCostMicros int64    `json:"max_estimated_cost_micros" binding:"min=0"`
	TimeoutMillis          int64    `json:"timeout_millis" binding:"required"`
	AllowedTools           []string `json:"allowed_tools"`
}

type publishAgentProfileRequest struct {
	ExpectedRevision int64 `json:"expected_revision" binding:"required"`
}

type requestAgentProfilePublishApprovalRequest struct {
	ExpectedVersionRevision int64                              `json:"expected_version_revision" binding:"required"`
	QualityEvidence         *agentEvalEvidenceReferenceRequest `json:"quality_evidence"`
}

type agentEvalEvidenceReferenceRequest struct {
	Storage               string `json:"storage"`
	Bucket                string `json:"bucket"`
	Key                   string `json:"key"`
	VersionID             string `json:"version_id"`
	ETag                  string `json:"etag"`
	ReportSHA256          string `json:"report_sha256"`
	Length                int32  `json:"length"`
	ContentType           string `json:"content_type"`
	RetentionMode         string `json:"retention_mode"`
	RetainUntil           int64  `json:"retain_until"`
	ArchivedAt            int64  `json:"archived_at"`
	DatasetVersion        string `json:"dataset_version"`
	DatasetSHA256         string `json:"dataset_sha256"`
	ExecutionConfigSHA256 string `json:"execution_config_sha256"`
	IntegrityKeyID        string `json:"integrity_key_id"`
}

type decideAgentProfilePublishApprovalRequest struct {
	Decision         string `json:"decision" binding:"required"`
	Reason           string `json:"reason"`
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
}

type retryAgentProfilePublishApprovalRequest struct {
	ExpectedRevision int64 `json:"expected_revision" binding:"required"`
}

type upsertAgentProfileReleaseRequest struct {
	StableVersion        string `json:"stable_version" binding:"required"`
	CandidateVersion     string `json:"candidate_version" binding:"required"`
	CandidateBasisPoints int32  `json:"candidate_basis_points"`
	Salt                 string `json:"salt"`
	ExpectedRevision     int64  `json:"expected_revision"`
}

type upsertAgentProfileRoleBindingRequest struct {
	Roles            []string `json:"roles" binding:"required,min=1"`
	ExpectedRevision int64    `json:"expected_revision" binding:"min=0"`
}

type agentProfileExperimentPolicyRequest struct {
	MinSamplesPerArm                  int32  `json:"min_samples_per_arm"`
	TargetSamplesPerArm               int32  `json:"target_samples_per_arm"`
	MaxErrorRateIncreaseBasisPoints   int32  `json:"max_error_rate_increase_basis_points"`
	MaxP95LatencyIncreaseBasisPoints  int32  `json:"max_p95_latency_increase_basis_points"`
	MaxAverageCostIncreaseBasisPoints int32  `json:"max_average_cost_increase_basis_points"`
	OutcomeSignal                     string `json:"outcome_signal"`
	MinOutcomeSamplesPerArm           int32  `json:"min_outcome_samples_per_arm"`
	MaxOutcomeRateDecreaseBasisPoints int32  `json:"max_outcome_rate_decrease_basis_points"`
}

type startAgentProfileExperimentRequest struct {
	ProfileID               string                              `json:"profile_id" binding:"required"`
	ExpectedReleaseRevision int64                               `json:"expected_release_revision" binding:"required"`
	Policy                  agentProfileExperimentPolicyRequest `json:"policy"`
}

type mutateAgentProfileExperimentRequest struct {
	ExpectedRevision int64 `json:"expected_revision" binding:"required"`
}

type recordAgentProfileExperimentOutcomeRequest struct {
	EventID  string `json:"event_id" binding:"required,max=128"`
	Signal   string `json:"signal" binding:"required"`
	Positive *bool  `json:"positive" binding:"required"`
}

func (h *AgentHandler) CreateAgentProfileDraft(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 15*time.Second, ProfileRoleEditor)
	if !ok {
		return
	}
	defer cancel()
	var req agentProfileSpecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.agentClient.CreateAgentProfileDraft(ctx, &aiAgentv1.CreateAgentProfileDraftRequest{
		ActorUserId: actorUserID,
		Spec:        agentProfileSpecRequestToProto(req),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"profile_version": agentProfileVersionToJSON(resp.ProfileVersion)})
}

func (h *AgentHandler) PublishAgentProfileVersion(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 15*time.Second, ProfileRoleAdmin)
	if !ok {
		return
	}
	defer cancel()
	if !h.profileDirectPublishEnabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "direct Agent Profile publishing is disabled; use publish approval"})
		return
	}
	var req publishAgentProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.agentClient.PublishAgentProfileVersion(ctx, &aiAgentv1.PublishAgentProfileVersionRequest{
		ActorUserId:      actorUserID,
		ProfileId:        c.Param("profile_id"),
		Version:          c.Param("version"),
		ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"profile_version": agentProfileVersionToJSON(resp.ProfileVersion)})
}

func (h *AgentHandler) RequestAgentProfilePublishApproval(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 15*time.Second, ProfileRoleEditor)
	if !ok {
		return
	}
	defer cancel()
	var req requestAgentProfilePublishApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.agentClient.RequestAgentProfilePublishApproval(ctx, &aiAgentv1.RequestAgentProfilePublishApprovalRequest{
		ActorUserId: actorUserID, ProfileId: c.Param("profile_id"), Version: c.Param("version"),
		ExpectedVersionRevision: req.ExpectedVersionRevision, QualityEvidence: agentEvalEvidenceReferenceRequestToProto(req.QualityEvidence),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"approval": agentProfilePublishApprovalToJSON(resp.Approval)})
}

func (h *AgentHandler) ListAgentProfilePublishApprovals(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleViewer)
	if !ok {
		return
	}
	defer cancel()
	page, pageSize, valid := parseProfileAdministrationPagination(c)
	if !valid {
		return
	}
	resp, err := h.agentClient.ListAgentProfilePublishApprovals(ctx, &aiAgentv1.ListAgentProfilePublishApprovalsRequest{
		ActorUserId: actorUserID, ProfileId: c.Query("profile_id"), Status: c.Query("status"),
		Page: uint32(page), PageSize: uint32(pageSize),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	items := make([]gin.H, 0, len(resp.Approvals))
	for _, approval := range resp.Approvals {
		items = append(items, agentProfilePublishApprovalToJSON(approval))
	}
	c.JSON(http.StatusOK, gin.H{"approvals": items, "total": resp.Total})
}

func (h *AgentHandler) GetAgentProfilePublishApproval(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleViewer)
	if !ok {
		return
	}
	defer cancel()
	resp, err := h.agentClient.GetAgentProfilePublishApproval(ctx, &aiAgentv1.GetAgentProfilePublishApprovalRequest{
		ActorUserId: actorUserID, ApprovalId: c.Param("approval_id"),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"approval": agentProfilePublishApprovalToJSON(resp.Approval)})
}

func (h *AgentHandler) DecideAgentProfilePublishApproval(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 30*time.Second, ProfileRoleApprover)
	if !ok {
		return
	}
	defer cancel()
	var req decideAgentProfilePublishApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.agentClient.DecideAgentProfilePublishApproval(ctx, &aiAgentv1.DecideAgentProfilePublishApprovalRequest{
		ActorUserId: actorUserID, ApprovalId: c.Param("approval_id"), Decision: req.Decision,
		Reason: req.Reason, ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"approval": agentProfilePublishApprovalToJSON(resp.Approval)})
}

func (h *AgentHandler) RetryAgentProfilePublishApproval(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 30*time.Second, ProfileRoleApprover)
	if !ok {
		return
	}
	defer cancel()
	var req retryAgentProfilePublishApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.agentClient.RetryAgentProfilePublishApproval(ctx, &aiAgentv1.RetryAgentProfilePublishApprovalRequest{
		ActorUserId: actorUserID, ApprovalId: c.Param("approval_id"), ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"approval": agentProfilePublishApprovalToJSON(resp.Approval)})
}

func (h *AgentHandler) ListAgentProfileVersions(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleViewer)
	if !ok {
		return
	}
	defer cancel()
	page, pageSize, valid := parseProfileAdministrationPagination(c)
	if !valid {
		return
	}
	resp, err := h.agentClient.ListAgentProfileVersions(ctx, &aiAgentv1.ListAgentProfileVersionsRequest{
		ActorUserId: actorUserID,
		ProfileId:   c.Query("profile_id"),
		Page:        uint32(page),
		PageSize:    uint32(pageSize),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	items := make([]gin.H, 0, len(resp.ProfileVersions))
	for _, version := range resp.ProfileVersions {
		items = append(items, agentProfileVersionToJSON(version))
	}
	c.JSON(http.StatusOK, gin.H{"profile_versions": items, "total": resp.Total})
}

func (h *AgentHandler) GetAgentProfileVersion(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleViewer)
	if !ok {
		return
	}
	defer cancel()
	resp, err := h.agentClient.GetAgentProfileVersion(ctx, &aiAgentv1.GetAgentProfileVersionRequest{
		ActorUserId: actorUserID, ProfileId: c.Param("profile_id"), Version: c.Param("version"),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"profile_version": agentProfileVersionToJSON(resp.ProfileVersion)})
}

func (h *AgentHandler) GetAgentProfileRelease(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleViewer)
	if !ok {
		return
	}
	defer cancel()
	resp, err := h.agentClient.GetAgentProfileRelease(ctx, &aiAgentv1.GetAgentProfileReleaseRequest{
		ActorUserId: actorUserID, ProfileId: c.Param("profile_id"),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"profile_release": agentProfileReleaseToJSON(resp.ProfileRelease)})
}

func (h *AgentHandler) UpsertAgentProfileRelease(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 15*time.Second, ProfileRoleAdmin)
	if !ok {
		return
	}
	defer cancel()
	var req upsertAgentProfileReleaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.agentClient.UpsertAgentProfileRelease(ctx, &aiAgentv1.UpsertAgentProfileReleaseRequest{
		ActorUserId:          actorUserID,
		ProfileId:            c.Param("profile_id"),
		StableVersion:        req.StableVersion,
		CandidateVersion:     req.CandidateVersion,
		CandidateBasisPoints: req.CandidateBasisPoints,
		Salt:                 req.Salt,
		ExpectedRevision:     req.ExpectedRevision,
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"profile_release": agentProfileReleaseToJSON(resp.ProfileRelease)})
}

func (h *AgentHandler) ListAgentProfileAuditEvents(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleViewer)
	if !ok {
		return
	}
	defer cancel()
	page, pageSize, valid := parseProfileAdministrationPagination(c)
	if !valid {
		return
	}
	resp, err := h.agentClient.ListAgentProfileAuditEvents(ctx, &aiAgentv1.ListAgentProfileAuditEventsRequest{
		ActorUserId: actorUserID,
		ProfileId:   c.Query("profile_id"),
		Page:        uint32(page),
		PageSize:    uint32(pageSize),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	items := make([]gin.H, 0, len(resp.AuditEvents))
	for _, event := range resp.AuditEvents {
		items = append(items, agentProfileAuditEventToJSON(event))
	}
	c.JSON(http.StatusOK, gin.H{"audit_events": items, "total": resp.Total})
}

func (h *AgentHandler) StartAgentProfileExperiment(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 15*time.Second, ProfileRoleAdmin)
	if !ok {
		return
	}
	defer cancel()
	var req startAgentProfileExperimentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.agentClient.StartAgentProfileExperiment(ctx, &aiAgentv1.StartAgentProfileExperimentRequest{
		ActorUserId: actorUserID, ProfileId: req.ProfileID, ExpectedReleaseRevision: req.ExpectedReleaseRevision,
		Policy: agentProfileExperimentPolicyRequestToProto(req.Policy),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"experiment": agentProfileExperimentToJSON(resp.Experiment)})
}

func (h *AgentHandler) ListAgentProfileExperiments(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleViewer)
	if !ok {
		return
	}
	defer cancel()
	page, pageSize, valid := parseProfileAdministrationPagination(c)
	if !valid {
		return
	}
	resp, err := h.agentClient.ListAgentProfileExperiments(ctx, &aiAgentv1.ListAgentProfileExperimentsRequest{
		ActorUserId: actorUserID, ProfileId: c.Query("profile_id"), Status: c.Query("status"),
		Page: uint32(page), PageSize: uint32(pageSize),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	items := make([]gin.H, 0, len(resp.Experiments))
	for _, experiment := range resp.Experiments {
		items = append(items, agentProfileExperimentToJSON(experiment))
	}
	c.JSON(http.StatusOK, gin.H{"experiments": items, "total": resp.Total})
}

func (h *AgentHandler) GetAgentProfileExperiment(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleViewer)
	if !ok {
		return
	}
	defer cancel()
	resp, err := h.agentClient.GetAgentProfileExperiment(ctx, &aiAgentv1.GetAgentProfileExperimentRequest{
		ActorUserId: actorUserID, ExperimentId: c.Param("experiment_id"),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"experiment": agentProfileExperimentToJSON(resp.Experiment)})
}

func (h *AgentHandler) EvaluateAgentProfileExperiment(c *gin.Context) {
	h.mutateAgentProfileExperiment(c, false)
}

func (h *AgentHandler) StopAgentProfileExperiment(c *gin.Context) {
	h.mutateAgentProfileExperiment(c, true)
}

func (h *AgentHandler) RecordAgentProfileExperimentOutcome(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 15*time.Second, ProfileRoleAdmin)
	if !ok {
		return
	}
	defer cancel()
	var req recordAgentProfileExperimentOutcomeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.agentClient.RecordAgentProfileExperimentOutcome(ctx, &aiAgentv1.RecordAgentProfileExperimentOutcomeRequest{
		ActorUserId: actorUserID, ExperimentId: c.Param("experiment_id"),
		EventId: req.EventID, Signal: req.Signal, Positive: *req.Positive,
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"idempotent_replay": resp.IdempotentReplay})
}

func (h *AgentHandler) mutateAgentProfileExperiment(c *gin.Context, stop bool) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 30*time.Second, ProfileRoleAdmin)
	if !ok {
		return
	}
	defer cancel()
	var req mutateAgentProfileExperimentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var experiment *aiAgentv1.AgentProfileExperiment
	if stop {
		resp, err := h.agentClient.StopAgentProfileExperiment(ctx, &aiAgentv1.StopAgentProfileExperimentRequest{
			ActorUserId: actorUserID, ExperimentId: c.Param("experiment_id"), ExpectedRevision: req.ExpectedRevision,
		})
		if err != nil {
			writeProfileAdministrationError(c, err)
			return
		}
		experiment = resp.Experiment
	} else {
		resp, err := h.agentClient.EvaluateAgentProfileExperiment(ctx, &aiAgentv1.EvaluateAgentProfileExperimentRequest{
			ActorUserId: actorUserID, ExperimentId: c.Param("experiment_id"), ExpectedRevision: req.ExpectedRevision,
		})
		if err != nil {
			writeProfileAdministrationError(c, err)
			return
		}
		experiment = resp.Experiment
	}
	c.JSON(http.StatusOK, gin.H{"experiment": agentProfileExperimentToJSON(experiment)})
}

func (h *AgentHandler) ListAgentProfileRoleBindings(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleAdmin)
	if !ok {
		return
	}
	defer cancel()
	page, pageSize, valid := parseProfileAdministrationPagination(c)
	if !valid {
		return
	}
	resp, err := h.agentClient.ListAgentProfileRoleBindings(ctx, &aiAgentv1.ListAgentProfileRoleBindingsRequest{
		ActorUserId: actorUserID, Page: uint32(page), PageSize: uint32(pageSize),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	items := make([]gin.H, 0, len(resp.RoleBindings))
	for _, binding := range resp.RoleBindings {
		items = append(items, agentProfileRoleBindingToJSON(binding))
	}
	c.JSON(http.StatusOK, gin.H{"role_bindings": items, "total": resp.Total})
}

func (h *AgentHandler) UpsertAgentProfileRoleBinding(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleAdmin)
	if !ok {
		return
	}
	defer cancel()
	subjectUserID, valid := parseProfileSubjectUserID(c)
	if !valid {
		return
	}
	var req upsertAgentProfileRoleBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.agentClient.UpsertAgentProfileRoleBinding(ctx, &aiAgentv1.UpsertAgentProfileRoleBindingRequest{
		ActorUserId: actorUserID, SubjectUserId: subjectUserID,
		Roles: append([]string(nil), req.Roles...), ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"role_binding": agentProfileRoleBindingToJSON(resp.RoleBinding)})
}

func (h *AgentHandler) DeleteAgentProfileRoleBinding(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleAdmin)
	if !ok {
		return
	}
	defer cancel()
	subjectUserID, valid := parseProfileSubjectUserID(c)
	if !valid {
		return
	}
	expectedRevision, err := strconv.ParseInt(c.Query("expected_revision"), 10, 64)
	if err != nil || expectedRevision < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected_revision must be positive"})
		return
	}
	if _, err := h.agentClient.DeleteAgentProfileRoleBinding(ctx, &aiAgentv1.DeleteAgentProfileRoleBindingRequest{
		ActorUserId: actorUserID, SubjectUserId: subjectUserID, ExpectedRevision: expectedRevision,
	}); err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AgentHandler) ListAgentProfileRoleAuditEvents(c *gin.Context) {
	ctx, cancel, actorUserID, ok := h.profileManagementContext(c, 10*time.Second, ProfileRoleAdmin)
	if !ok {
		return
	}
	defer cancel()
	page, pageSize, valid := parseProfileAdministrationPagination(c)
	if !valid {
		return
	}
	resp, err := h.agentClient.ListAgentProfileRoleAuditEvents(ctx, &aiAgentv1.ListAgentProfileRoleAuditEventsRequest{
		ActorUserId: actorUserID, Page: uint32(page), PageSize: uint32(pageSize),
	})
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	items := make([]gin.H, 0, len(resp.AuditEvents))
	for _, event := range resp.AuditEvents {
		items = append(items, agentProfileRoleAuditEventToJSON(event))
	}
	c.JSON(http.StatusOK, gin.H{"audit_events": items, "total": resp.Total})
}

func (h *AgentHandler) GetAgentProfileCatalogAccess(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.profileAdminToken == "" {
		setSensitiveResponseHeaders(c)
		c.JSON(http.StatusOK, gin.H{"enabled": false, "roles": []string{}, "direct_publish_enabled": false})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, profile.AdminTokenMetadataKey, h.profileAdminToken)
	access, err := h.resolveProfileManagementAccess(ctx, userID)
	if err != nil {
		writeProfileAdministrationError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"enabled":                len(access.roles) > 0,
		"roles":                  access.roles,
		"static_roles":           access.staticRoles,
		"dynamic_roles":          access.dynamicRoles,
		"root_admin":             access.rootAdmin,
		"dynamic_rbac_enabled":   access.dynamicEnabled,
		"direct_publish_enabled": h.profileDirectPublishEnabled && hasProfileRoleNames(access.roles, ProfileRoleAdmin),
		"experiments_enabled":    access.experimentsEnabled,
	})
}

func (h *AgentHandler) profileManagementContext(c *gin.Context, timeout time.Duration, requiredRole ProfileManagementRole) (context.Context, context.CancelFunc, uint64, bool) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, nil, 0, false
	}
	if h.profileAdminToken == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Agent Profile administration is disabled"})
		return nil, nil, 0, false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	ctx = metadata.AppendToOutgoingContext(ctx, profile.AdminTokenMetadataKey, h.profileAdminToken)
	access, err := h.resolveProfileManagementAccess(ctx, userID)
	if err != nil {
		cancel()
		writeProfileAdministrationError(c, err)
		return nil, nil, 0, false
	}
	if !hasProfileRoleNames(access.roles, requiredRole) {
		cancel()
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return nil, nil, 0, false
	}
	setSensitiveResponseHeaders(c)
	return ctx, cancel, userID, true
}

type profileManagementAccess struct {
	roles              []string
	staticRoles        []string
	dynamicRoles       []string
	rootAdmin          bool
	dynamicEnabled     bool
	experimentsEnabled bool
}

func (h *AgentHandler) resolveProfileManagementAccess(ctx context.Context, userID uint64) (profileManagementAccess, error) {
	resp, err := h.agentClient.GetAgentProfileManagementAccess(ctx, &aiAgentv1.GetAgentProfileManagementAccessRequest{ActorUserId: userID})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			roles := h.profileRolesForUser(userID)
			return profileManagementAccess{roles: roles, staticRoles: append([]string(nil), roles...)}, nil
		}
		return profileManagementAccess{}, err
	}
	if resp == nil || resp.Access == nil {
		return profileManagementAccess{}, status.Error(codes.Unavailable, "Agent Profile access response is unavailable")
	}
	return profileManagementAccess{
		roles:              append([]string(nil), resp.Access.Roles...),
		staticRoles:        append([]string(nil), resp.Access.StaticRoles...),
		dynamicRoles:       append([]string(nil), resp.Access.DynamicRoles...),
		rootAdmin:          resp.Access.RootAdmin,
		dynamicEnabled:     resp.Access.DynamicRbacEnabled,
		experimentsEnabled: resp.Access.ExperimentsEnabled,
	}, nil
}

func hasProfileRoleNames(roles []string, requiredRole ProfileManagementRole) bool {
	hasAny := false
	for _, role := range roles {
		if role != string(ProfileRoleViewer) && role != string(ProfileRoleEditor) && role != string(ProfileRoleApprover) && role != string(ProfileRoleAdmin) {
			continue
		}
		hasAny = true
		if role == string(ProfileRoleAdmin) || role == string(requiredRole) {
			return true
		}
	}
	return requiredRole == ProfileRoleViewer && hasAny
}

func (h *AgentHandler) hasProfileRole(userID uint64, requiredRole ProfileManagementRole) bool {
	if userID == 0 {
		return false
	}
	if _, admin := h.profileRoleUserIDs[ProfileRoleAdmin][userID]; admin {
		return true
	}
	if requiredRole == ProfileRoleViewer {
		for _, role := range []ProfileManagementRole{ProfileRoleViewer, ProfileRoleEditor, ProfileRoleApprover} {
			if _, allowed := h.profileRoleUserIDs[role][userID]; allowed {
				return true
			}
		}
		return false
	}
	_, allowed := h.profileRoleUserIDs[requiredRole][userID]
	return allowed
}

func (h *AgentHandler) profileRolesForUser(userID uint64) []string {
	roles := make([]string, 0, 4)
	for _, role := range []ProfileManagementRole{ProfileRoleViewer, ProfileRoleEditor, ProfileRoleApprover, ProfileRoleAdmin} {
		if _, allowed := h.profileRoleUserIDs[role][userID]; allowed {
			roles = append(roles, string(role))
		}
	}
	return roles
}

func writeProfileAdministrationError(c *gin.Context, err error) {
	httpStatus := http.StatusInternalServerError
	switch status.Code(err) {
	case codes.InvalidArgument:
		httpStatus = http.StatusBadRequest
	case codes.Unauthenticated:
		httpStatus = http.StatusUnauthorized
	case codes.PermissionDenied:
		httpStatus = http.StatusForbidden
	case codes.NotFound:
		httpStatus = http.StatusNotFound
	case codes.Aborted:
		httpStatus = http.StatusConflict
	case codes.FailedPrecondition:
		httpStatus = http.StatusUnprocessableEntity
	case codes.Unavailable:
		httpStatus = http.StatusServiceUnavailable
	}
	c.JSON(httpStatus, gin.H{"error": status.Convert(err).Message()})
}

func parseProfileAdministrationPagination(c *gin.Context) (int, int, bool) {
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageErr != nil || pageSizeErr != nil || page < 1 || pageSize < 1 || pageSize > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page must be positive and page_size must be between 1 and 100"})
		return 0, 0, false
	}
	return page, pageSize, true
}

func parseProfileSubjectUserID(c *gin.Context) (uint64, bool) {
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil || userID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id must be a positive integer"})
		return 0, false
	}
	return userID, true
}

func agentProfileSpecRequestToProto(req agentProfileSpecRequest) *aiAgentv1.AgentProfileSpec {
	return &aiAgentv1.AgentProfileSpec{
		ProfileId: req.ProfileID, Version: req.Version,
		PromptId: req.PromptID, PromptVersion: req.PromptVersion, SystemPrompt: req.SystemPrompt,
		MaxSteps: req.MaxSteps, MaxInputTokens: req.MaxInputTokens,
		MaxOutputTokens: req.MaxOutputTokens, MaxTotalTokens: req.MaxTotalTokens,
		MaxEstimatedCostMicros: req.MaxEstimatedCostMicros, TimeoutMillis: req.TimeoutMillis,
		AllowedTools: append([]string(nil), req.AllowedTools...),
	}
}

func agentProfileVersionToJSON(version *aiAgentv1.AgentProfileVersion) gin.H {
	if version == nil {
		return gin.H{}
	}
	return gin.H{
		"id": version.Id, "spec": agentProfileSpecToJSON(version.Spec), "status": version.Status,
		"revision": version.Revision, "created_by": strconv.FormatUint(version.CreatedBy, 10),
		"published_by": strconv.FormatUint(version.PublishedBy, 10), "created_at": version.CreatedAt,
		"updated_at": version.UpdatedAt, "published_at": version.PublishedAt, "snapshot_hash": version.SnapshotHash,
	}
}

func agentProfileSpecToJSON(spec *aiAgentv1.AgentProfileSpec) gin.H {
	if spec == nil {
		return gin.H{}
	}
	return gin.H{
		"profile_id": spec.ProfileId, "version": spec.Version,
		"prompt_id": spec.PromptId, "prompt_version": spec.PromptVersion, "system_prompt": spec.SystemPrompt,
		"max_steps": spec.MaxSteps, "max_input_tokens": spec.MaxInputTokens,
		"max_output_tokens": spec.MaxOutputTokens, "max_total_tokens": spec.MaxTotalTokens,
		"max_estimated_cost_micros": spec.MaxEstimatedCostMicros, "timeout_millis": spec.TimeoutMillis,
		"allowed_tools": spec.AllowedTools,
	}
}

func agentProfileReleaseToJSON(release *aiAgentv1.AgentProfileRelease) gin.H {
	if release == nil {
		return gin.H{}
	}
	return gin.H{
		"profile_id": release.ProfileId, "stable_version": release.StableVersion,
		"candidate_version": release.CandidateVersion, "candidate_basis_points": release.CandidateBasisPoints,
		"salt": release.Salt, "revision": release.Revision,
		"created_by": strconv.FormatUint(release.CreatedBy, 10), "updated_by": strconv.FormatUint(release.UpdatedBy, 10),
		"created_at": release.CreatedAt, "updated_at": release.UpdatedAt,
	}
}

func agentProfileAuditEventToJSON(event *aiAgentv1.AgentProfileAuditEvent) gin.H {
	if event == nil {
		return gin.H{}
	}
	return gin.H{
		"id": event.Id, "operation_id": event.OperationId, "action": event.Action, "outcome": event.Outcome,
		"profile_id": event.ProfileId, "version": event.Version,
		"approval_id":      event.ApprovalId,
		"experiment_id":    event.ExperimentId,
		"actor_user_id":    strconv.FormatUint(event.ActorUserId, 10),
		"version_revision": event.VersionRevision, "release_revision": event.ReleaseRevision,
		"snapshot_hash": event.SnapshotHash, "error_code": event.ErrorCode, "created_at": event.CreatedAt,
	}
}

func agentProfileExperimentPolicyRequestToProto(policy agentProfileExperimentPolicyRequest) *aiAgentv1.AgentProfileExperimentPolicy {
	return &aiAgentv1.AgentProfileExperimentPolicy{
		MinSamplesPerArm: policy.MinSamplesPerArm, TargetSamplesPerArm: policy.TargetSamplesPerArm,
		MaxErrorRateIncreaseBasisPoints:   policy.MaxErrorRateIncreaseBasisPoints,
		MaxP95LatencyIncreaseBasisPoints:  policy.MaxP95LatencyIncreaseBasisPoints,
		MaxAverageCostIncreaseBasisPoints: policy.MaxAverageCostIncreaseBasisPoints,
		OutcomeSignal:                     policy.OutcomeSignal,
		MinOutcomeSamplesPerArm:           policy.MinOutcomeSamplesPerArm,
		MaxOutcomeRateDecreaseBasisPoints: policy.MaxOutcomeRateDecreaseBasisPoints,
	}
}

func agentProfileExperimentToJSON(experiment *aiAgentv1.AgentProfileExperiment) gin.H {
	if experiment == nil {
		return gin.H{}
	}
	return gin.H{
		"experiment_id": experiment.ExperimentId, "profile_id": experiment.ProfileId,
		"stable_version": experiment.StableVersion, "candidate_version": experiment.CandidateVersion,
		"candidate_basis_points": experiment.CandidateBasisPoints, "release_revision": experiment.ReleaseRevision,
		"policy": agentProfileExperimentPolicyToJSON(experiment.Policy), "status": experiment.Status,
		"decision": experiment.Decision, "decision_reason": experiment.DecisionReason,
		"stats": agentProfileExperimentStatsToJSON(experiment.Stats), "revision": experiment.Revision,
		"created_by": strconv.FormatUint(experiment.CreatedBy, 10), "updated_by": strconv.FormatUint(experiment.UpdatedBy, 10),
		"started_at": experiment.StartedAt, "completed_at": experiment.CompletedAt, "updated_at": experiment.UpdatedAt,
	}
}

func agentProfileExperimentPolicyToJSON(policy *aiAgentv1.AgentProfileExperimentPolicy) gin.H {
	if policy == nil {
		return gin.H{}
	}
	return gin.H{
		"min_samples_per_arm": policy.MinSamplesPerArm, "target_samples_per_arm": policy.TargetSamplesPerArm,
		"max_error_rate_increase_basis_points":   policy.MaxErrorRateIncreaseBasisPoints,
		"max_p95_latency_increase_basis_points":  policy.MaxP95LatencyIncreaseBasisPoints,
		"max_average_cost_increase_basis_points": policy.MaxAverageCostIncreaseBasisPoints,
		"outcome_signal":                         policy.OutcomeSignal,
		"min_outcome_samples_per_arm":            policy.MinOutcomeSamplesPerArm,
		"max_outcome_rate_decrease_basis_points": policy.MaxOutcomeRateDecreaseBasisPoints,
	}
}

func agentProfileExperimentStatsToJSON(stats *aiAgentv1.AgentProfileExperimentStats) gin.H {
	if stats == nil {
		return gin.H{}
	}
	return gin.H{"stable": agentProfileExperimentArmStatsToJSON(stats.Stable), "candidate": agentProfileExperimentArmStatsToJSON(stats.Candidate)}
}

func agentProfileExperimentArmStatsToJSON(stats *aiAgentv1.AgentProfileExperimentArmStats) gin.H {
	if stats == nil {
		return gin.H{}
	}
	return gin.H{
		"samples": stats.Samples, "successes": stats.Successes, "failures": stats.Failures,
		"error_rate_basis_points": stats.ErrorRateBasisPoints, "p95_latency_millis": stats.P95LatencyMillis,
		"average_cost_micros": stats.AverageCostMicros,
		"outcome_samples":     stats.OutcomeSamples, "outcome_positives": stats.OutcomePositives,
		"outcome_rate_basis_points": stats.OutcomeRateBasisPoints,
	}
}

func agentProfilePublishApprovalToJSON(approval *aiAgentv1.AgentProfilePublishApproval) gin.H {
	if approval == nil {
		return gin.H{}
	}
	return gin.H{
		"approval_id": approval.ApprovalId, "profile_id": approval.ProfileId, "version": approval.Version,
		"snapshot_hash": approval.SnapshotHash, "expected_version_revision": approval.ExpectedVersionRevision,
		"status": approval.Status, "decision": approval.Decision, "reason": approval.Reason,
		"revision": approval.Revision, "requested_by": strconv.FormatUint(approval.RequestedBy, 10),
		"decided_by": strconv.FormatUint(approval.DecidedBy, 10), "applying_by": strconv.FormatUint(approval.ApplyingBy, 10),
		"error_code": approval.ErrorCode, "requested_at": approval.RequestedAt, "decided_at": approval.DecidedAt,
		"apply_lease_until": approval.ApplyLeaseUntil, "applied_at": approval.AppliedAt, "updated_at": approval.UpdatedAt,
		"quality_evidence": agentProfileQualityEvidenceToJSON(approval.QualityEvidence),
	}
}

func agentEvalEvidenceReferenceRequestToProto(reference *agentEvalEvidenceReferenceRequest) *aiAgentv1.AgentEvalEvidenceReference {
	if reference == nil {
		return nil
	}
	return &aiAgentv1.AgentEvalEvidenceReference{
		Storage: reference.Storage, Bucket: reference.Bucket, Key: reference.Key,
		VersionId: reference.VersionID, Etag: reference.ETag,
		ReportSha256: reference.ReportSHA256, Length: reference.Length, ContentType: reference.ContentType,
		RetentionMode: reference.RetentionMode, RetainUntil: reference.RetainUntil, ArchivedAt: reference.ArchivedAt,
		DatasetVersion: reference.DatasetVersion, DatasetSha256: reference.DatasetSHA256,
		ExecutionConfigSha256: reference.ExecutionConfigSHA256, IntegrityKeyId: reference.IntegrityKeyID,
	}
}

func agentProfileQualityEvidenceToJSON(evidence *aiAgentv1.AgentProfileQualityEvidence) any {
	if evidence == nil {
		return nil
	}
	reference := evidence.Reference
	var referenceJSON any
	if reference != nil {
		referenceJSON = gin.H{
			"storage": reference.Storage, "bucket": reference.Bucket, "key": reference.Key,
			"version_id": reference.VersionId, "etag": reference.Etag,
			"report_sha256": reference.ReportSha256, "length": reference.Length, "content_type": reference.ContentType,
			"retention_mode": reference.RetentionMode, "retain_until": reference.RetainUntil, "archived_at": reference.ArchivedAt,
			"dataset_version": reference.DatasetVersion, "dataset_sha256": reference.DatasetSha256,
			"execution_config_sha256": reference.ExecutionConfigSha256, "integrity_key_id": reference.IntegrityKeyId,
		}
	}
	return gin.H{
		"reference": referenceJSON, "profile_id": evidence.ProfileId, "profile_version": evidence.ProfileVersion,
		"gate_status": evidence.GateStatus, "cases": evidence.Cases, "passed": evidence.Passed,
		"task_completion_rate_bps":         evidence.TaskCompletionRateBps,
		"read_tool_selection_accuracy_bps": evidence.ReadToolSelectionAccuracyBps,
		"semantic_pass_rate_bps":           evidence.SemanticPassRateBps, "approval_pass_rate_bps": evidence.ApprovalPassRateBps,
		"report_signed_at": evidence.ReportSignedAt, "verified_at": evidence.VerifiedAt,
	}
}

func agentProfileRoleBindingToJSON(binding *aiAgentv1.AgentProfileRoleBinding) gin.H {
	if binding == nil {
		return gin.H{}
	}
	return gin.H{
		"user_id": strconv.FormatUint(binding.UserId, 10), "roles": binding.Roles,
		"revision":   binding.Revision,
		"created_by": strconv.FormatUint(binding.CreatedBy, 10),
		"updated_by": strconv.FormatUint(binding.UpdatedBy, 10),
		"created_at": binding.CreatedAt, "updated_at": binding.UpdatedAt,
	}
}

func agentProfileRoleAuditEventToJSON(event *aiAgentv1.AgentProfileRoleAuditEvent) gin.H {
	if event == nil {
		return gin.H{}
	}
	return gin.H{
		"id": event.Id, "operation_id": event.OperationId, "action": event.Action,
		"outcome":         event.Outcome,
		"actor_user_id":   strconv.FormatUint(event.ActorUserId, 10),
		"subject_user_id": strconv.FormatUint(event.SubjectUserId, 10),
		"roles":           event.Roles, "revision": event.Revision,
		"error_code": event.ErrorCode, "created_at": event.CreatedAt,
	}
}
