package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/gateway/middleware"
)

// parseInterfaceToUint64 辅助函数：将 interface{} 兼容转换为 uint64
func parseInterfaceToUint64(val interface{}) (uint64, error) {
	if val == nil {
		return 0, nil
	}
	switch v := val.(type) {
	case float64:
		return uint64(v), nil
	case string:
		if v == "" || v == "0" {
			return 0, nil
		}
		if len(v) == 24 {
			return 0, nil
		}
		return strconv.ParseUint(v, 10, 64)
	default:
		return 0, strconv.ErrSyntax
	}
}

// AgentHandler AI Agent 处理器
type AgentHandler struct {
	agentClient                    aiAgentv1.AiAgentServiceClient
	profileAdminToken              string
	profileRoleUserIDs             map[ProfileManagementRole]map[uint64]struct{}
	profileDirectPublishEnabled    bool
	extensionMarketplaceAdminToken string
}

type ProfileManagementRole string

const (
	ProfileRoleViewer   ProfileManagementRole = "viewer"
	ProfileRoleEditor   ProfileManagementRole = "editor"
	ProfileRoleApprover ProfileManagementRole = "approver"
	ProfileRoleAdmin    ProfileManagementRole = "admin"
)

type AgentHandlerOption func(*AgentHandler)

func WithExtensionMarketplaceAdministration(token string) AgentHandlerOption {
	return func(handler *AgentHandler) {
		handler.extensionMarketplaceAdminToken = strings.TrimSpace(token)
	}
}

func WithProfileAdministration(token string, administratorIDs []uint64) AgentHandlerOption {
	return WithProfileManagementRoles(token, nil, nil, nil, administratorIDs, false)
}

func WithProfileManagementRoles(
	token string,
	viewerIDs, editorIDs, approverIDs, administratorIDs []uint64,
	directPublishEnabled bool,
) AgentHandlerOption {
	return func(handler *AgentHandler) {
		handler.profileAdminToken = token
		handler.profileDirectPublishEnabled = directPublishEnabled
		handler.profileRoleUserIDs = make(map[ProfileManagementRole]map[uint64]struct{}, 4)
		for role, userIDs := range map[ProfileManagementRole][]uint64{
			ProfileRoleViewer: viewerIDs, ProfileRoleEditor: editorIDs,
			ProfileRoleApprover: approverIDs, ProfileRoleAdmin: administratorIDs,
		} {
			members := make(map[uint64]struct{}, len(userIDs))
			for _, userID := range userIDs {
				if userID != 0 {
					members[userID] = struct{}{}
				}
			}
			handler.profileRoleUserIDs[role] = members
		}
	}
}

// NewAgentHandler 创建 Agent 处理器
func NewAgentHandler(agentClient aiAgentv1.AiAgentServiceClient, options ...AgentHandlerOption) *AgentHandler {
	handler := &AgentHandler{agentClient: agentClient}
	for _, option := range options {
		if option != nil {
			option(handler)
		}
	}
	return handler
}

// CallApiOfAiRequest 直接对话请求
type CallApiOfAiRequest struct {
	Content     string      `json:"content" binding:"required"`
	DialogueID  interface{} `json:"dialogue_id"`
	DialogueKey string      `json:"dialogue_key"`
	ModelKindID interface{} `json:"model_kind_id"`
}

type RunAgentRequest struct {
	Content                   string      `json:"content" binding:"required"`
	DialogueID                interface{} `json:"dialogue_id"`
	DialogueKey               string      `json:"dialogue_key"`
	ModelKindID               interface{} `json:"model_kind_id"`
	PreferredCapabilityIDs    []string    `json:"preferred_capability_ids"`
	WebSearchProviderConfigID string      `json:"web_search_provider_config_id"`
	SkillID                   string      `json:"skill_id"`
	SkillVersion              string      `json:"skill_version"`
}

type CreateAgentTaskTemplateRequest struct {
	ExpectedSourceRunRevision int64  `json:"expected_source_run_revision" binding:"required"`
	Name                      string `json:"name" binding:"required"`
	Description               string `json:"description"`
	InstructionTemplate       string `json:"instruction_template" binding:"required"`
	IdempotencyKey            string `json:"idempotency_key" binding:"required"`
}

type RunAgentTaskTemplateRequest struct {
	ExpectedRevision          int64       `json:"expected_revision" binding:"required"`
	Input                     string      `json:"input" binding:"required"`
	DialogueID                interface{} `json:"dialogue_id"`
	DialogueKey               string      `json:"dialogue_key"`
	ModelKindID               interface{} `json:"model_kind_id"`
	WebSearchProviderConfigID string      `json:"web_search_provider_config_id"`
}

type ResumeAgentRunRequest struct {
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
	HumanResponse    string `json:"human_response"`
	ApprovalID       string `json:"approval_id"`
	ResumeToken      string `json:"resume_token"`
}

// ConfirmPublishRequest 确认发布请求
type ConfirmPublishRequest struct {
	Content     string `json:"content" binding:"required"`
	SourceRunID string `json:"source_run_id"`
}

// MultiAgentPublishRequest 多 Agent 写推文请求
type MultiAgentPublishRequest struct {
	Domain            string   `json:"domain" binding:"required"`
	AuthorUserID      string   `json:"author_user_id" binding:"required"`
	StyleRatio        float32  `json:"style_ratio" binding:"required"`
	ReferenceTweetIDs []string `json:"reference_tweet_ids"`
	DialogueKey       string   `json:"dialogue_key"`
	Content           string   `json:"content" binding:"required"`
}

type WorkflowSaveRequest struct {
	Name    string          `json:"name" binding:"required"`
	DSLJSON string          `json:"dsl_json"`
	DSL     json.RawMessage `json:"dsl"`
}

type WorkflowRunRequest struct {
	InputJSON          string          `json:"input_json"`
	Input              json.RawMessage `json:"input"`
	WorkflowRevisionID string          `json:"workflow_revision_id"`
}

type WorkflowToolPublicationSaveRequest struct {
	WorkflowRevisionID string          `json:"workflow_revision_id"`
	Description        string          `json:"description"`
	InputSchemaJSON    string          `json:"input_schema_json"`
	InputSchema        json.RawMessage `json:"input_schema"`
	ExpectedRevision   int64           `json:"expected_revision"`
}

type WorkflowResumeRequest struct {
	ApprovalID  string          `json:"approval_id"`
	ResumeToken string          `json:"resume_token"`
	InputJSON   string          `json:"input_json"`
	Input       json.RawMessage `json:"input"`
}

type WorkflowCancelRequest struct {
	Reason string `json:"reason"`
}

type ToolApprovalDecisionRequest struct {
	Decision         string `json:"decision" binding:"required"`
	Reason           string `json:"reason"`
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
}

type WorkflowResumeGrantRequest struct {
	ExpectedRunRevision int64 `json:"expected_run_revision" binding:"required"`
}

type ProviderConfigSaveRequest struct {
	Kind     string `json:"kind"`
	Name     string `json:"name" binding:"required"`
	Provider string `json:"provider" binding:"required"`
	BaseURL  string `json:"base_url" binding:"required"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
	Revision int64  `json:"revision"`
}

type ExternalMCPConnectionSaveRequest struct {
	Scope                string `json:"scope"`
	ProjectID            string `json:"project_id"`
	Name                 string `json:"name" binding:"required"`
	Transport            string `json:"transport" binding:"required"`
	Endpoint             string `json:"endpoint" binding:"required"`
	AuthType             string `json:"auth_type"`
	CredentialSource     string `json:"credential_source"`
	ManagedCredentialRef string `json:"managed_credential_ref"`
	BearerToken          string `json:"bearer_token"`
	ExpectedRevision     int64  `json:"expected_revision"`
}

type ExternalMCPRevisionRequest struct {
	ExpectedRevision int64 `json:"expected_revision" binding:"required"`
}

type ExternalMCPToolPolicyRequest struct {
	SnapshotID       string `json:"snapshot_id" binding:"required"`
	Category         string `json:"category" binding:"required"`
	Enabled          bool   `json:"enabled"`
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
}

// CallApiOfAi 模式一：直接 AI 对话
// POST /api/v1/agent/chat
func (h *AgentHandler) CallApiOfAi(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req CallApiOfAiRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var dialogueID uint64
	if req.DialogueID != nil {
		var err error
		dialogueID, err = parseInterfaceToUint64(req.DialogueID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dialogue_id format"})
			return
		}
	}

	var modelKindID uint64
	if req.ModelKindID != nil {
		var err error
		modelKindID, err = parseInterfaceToUint64(req.ModelKindID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model_kind_id format"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.CallApiOfAi(ctx, &aiAgentv1.CallApiOfAiRequest{
		UserId:      userID,
		ModelKindId: modelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:      userID,
			DialogueId:  dialogueID,
			DialogueKey: req.DialogueKey,
			Content:     req.Content,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"response":     resp.Response,
		"dialogue_key": resp.DialogueKey,
	})
}

// RunAgent is the capability-first P8 entry point.
// POST /api/v1/agent/run
func (h *AgentHandler) RunAgent(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req RunAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dialogueID, err := parseInterfaceToUint64(req.DialogueID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dialogue_id format"})
		return
	}
	modelKindID, err := parseInterfaceToUint64(req.ModelKindID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model_kind_id format"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	resp, err := h.agentClient.RunAgent(ctx, &aiAgentv1.RunAgentRequest{
		UserId:      userID,
		ModelKindId: modelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:      userID,
			DialogueId:  dialogueID,
			DialogueKey: req.DialogueKey,
			Content:     req.Content,
		},
		PreferredCapabilityIds:    req.PreferredCapabilityIDs,
		WebSearchProviderConfigId: req.WebSearchProviderConfigID,
		SkillId:                   req.SkillID,
		SkillVersion:              req.SkillVersion,
	})
	if err != nil {
		switch status.Code(err) {
		case codes.InvalidArgument:
			c.JSON(http.StatusBadRequest, gin.H{"error": status.Convert(err).Message()})
		case codes.FailedPrecondition:
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": status.Convert(err).Message()})
		case codes.NotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": status.Convert(err).Message()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, runAgentResponseToJSON(resp))
}

// ListAgentSkills returns the authenticated user's active immutable Skill
// projections. The bounded list is intentionally not a global marketplace.
// GET /api/v1/agent/skills
func (h *AgentHandler) ListAgentSkills(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	limit := int32(0)
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || parsed < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer"})
			return
		}
		limit = int32(parsed)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListAgentSkills(
		ctx,
		&aiAgentv1.ListAgentSkillsRequest{UserId: userID, Limit: limit},
	)
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	items := make([]gin.H, 0, len(resp.Skills))
	for _, version := range resp.Skills {
		items = append(items, agentSkillToJSON(version))
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"skills": items})
}

// ListAgentExtensions returns a stable-cursor page from the authenticated
// tenant's credential-free capability, Skill and governed MCP directory.
// GET /api/v1/agent/extensions
func (h *AgentHandler) ListAgentExtensions(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	pageSize := int32(0)
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || parsed < 1 || parsed > 50 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "page_size must be within 1..50"})
			return
		}
		pageSize = int32(parsed)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListAgentExtensions(ctx, &aiAgentv1.ListAgentExtensionsRequest{
		UserId:      userID,
		Kind:        strings.TrimSpace(c.Query("kind")),
		Category:    strings.TrimSpace(c.Query("category")),
		Scope:       strings.TrimSpace(c.Query("scope")),
		Status:      strings.TrimSpace(c.Query("status")),
		Search:      strings.TrimSpace(c.Query("search")),
		AfterCursor: strings.TrimSpace(c.Query("after_cursor")),
		PageSize:    pageSize,
	})
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	items := make([]gin.H, 0, len(resp.Extensions))
	for _, item := range resp.Extensions {
		items = append(items, agentExtensionToJSON(item))
	}
	sources := make([]gin.H, 0, len(resp.Sources))
	for _, source := range resp.Sources {
		if source == nil {
			continue
		}
		sources = append(sources, gin.H{
			"source": source.Source, "state": source.State, "entry_count": source.EntryCount,
		})
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"contract_version": resp.ContractVersion,
		"extensions":       items,
		"sources":          sources,
		"next_cursor":      resp.NextCursor,
		"has_more":         resp.HasMore,
	})
}

// ListAgentMarketplaceExtensions returns signature-verified public release
// metadata. It does not install packages or grant execution permissions.
// GET /api/v1/agent/marketplace/extensions
func (h *AgentHandler) ListAgentMarketplaceExtensions(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	pageSize := int32(0)
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || parsed < 1 || parsed > 50 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "page_size must be within 1..50"})
			return
		}
		pageSize = int32(parsed)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListAgentMarketplaceExtensions(ctx, &aiAgentv1.ListAgentMarketplaceExtensionsRequest{
		UserId:      userID,
		Kind:        strings.TrimSpace(c.Query("kind")),
		PublisherId: strings.TrimSpace(c.Query("publisher_id")),
		Search:      strings.TrimSpace(c.Query("search")),
		AfterCursor: strings.TrimSpace(c.Query("after_cursor")),
		PageSize:    pageSize,
	})
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	releases := make([]gin.H, 0, len(resp.Releases))
	for _, release := range resp.Releases {
		releases = append(releases, agentMarketplaceExtensionToJSON(release))
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"contract_version": resp.ContractVersion,
		"releases":         releases,
		"next_cursor":      resp.NextCursor,
		"has_more":         resp.HasMore,
	})
}

// GetAgentSkill resolves one exact immutable version.
// GET /api/v1/agent/skills/:id?version=...
func (h *AgentHandler) GetAgentSkill(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	skillID := strings.TrimSpace(c.Param("id"))
	version := strings.TrimSpace(c.Query("version"))
	if skillID == "" || version == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "skill id and version are required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetAgentSkill(ctx, &aiAgentv1.GetAgentSkillRequest{
		UserId: userID, SkillId: skillID, Version: version,
	})
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"skill": agentSkillToJSON(resp.Skill)})
}

// CreateAgentTaskTemplate explicitly saves a reusable preset from one
// completed authoritative Agent Run.
// POST /api/v1/agent/runs/:id/task-templates
func (h *AgentHandler) CreateAgentTaskTemplate(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	runID := strings.TrimSpace(c.Param("id"))
	var req CreateAgentTaskTemplateRequest
	if runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run id is required"})
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.agentClient.CreateAgentTaskTemplate(
		ctx,
		&aiAgentv1.CreateAgentTaskTemplateRequest{
			UserId: userID, SourceRunId: runID,
			ExpectedSourceRunRevision: req.ExpectedSourceRunRevision,
			Name:                      req.Name, Description: req.Description,
			InstructionTemplate: req.InstructionTemplate,
			IdempotencyKey:      req.IdempotencyKey,
		},
	)
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusCreated, gin.H{"task_template": agentTaskTemplateToJSON(resp.TaskTemplate)})
}

// ListAgentTaskTemplates returns active presets and the independent execution
// rollout state. Listing remains available while execution is disabled.
// GET /api/v1/agent/task-templates
func (h *AgentHandler) ListAgentTaskTemplates(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	limit := int32(0)
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || parsed < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer"})
			return
		}
		limit = int32(parsed)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListAgentTaskTemplates(
		ctx,
		&aiAgentv1.ListAgentTaskTemplatesRequest{UserId: userID, Limit: limit},
	)
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	items := make([]gin.H, 0, len(resp.TaskTemplates))
	for _, template := range resp.TaskTemplates {
		items = append(items, agentTaskTemplateToJSON(template))
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"execution_enabled": resp.ExecutionEnabled,
		"task_templates":    items,
	})
}

// ArchiveAgentTaskTemplate removes a preset from the active catalog using
// optimistic concurrency while preserving source-run evidence.
// DELETE /api/v1/agent/task-templates/:id
func (h *AgentHandler) ArchiveAgentTaskTemplate(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	expectedRevision, err := strconv.ParseInt(
		strings.TrimSpace(c.Query("expected_revision")),
		10,
		64,
	)
	if err != nil || expectedRevision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected_revision must be a positive integer"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.agentClient.ArchiveAgentTaskTemplate(
		ctx,
		&aiAgentv1.ArchiveAgentTaskTemplateRequest{
			UserId: userID, TemplateId: strings.TrimSpace(c.Param("id")),
			ExpectedRevision: expectedRevision,
		},
	)
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"task_template": agentTaskTemplateToJSON(resp.TaskTemplate)})
}

// RunAgentTaskTemplate executes one selected immutable template revision
// through the unified Agent response contract.
// POST /api/v1/agent/task-templates/:id/run
func (h *AgentHandler) RunAgentTaskTemplate(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req RunAgentTaskTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	dialogueID, err := parseInterfaceToUint64(req.DialogueID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dialogue_id format"})
		return
	}
	modelKindID, err := parseInterfaceToUint64(req.ModelKindID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model_kind_id format"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	resp, err := h.agentClient.RunAgentTaskTemplate(
		ctx,
		&aiAgentv1.RunAgentTaskTemplateRequest{
			UserId: userID, TemplateId: strings.TrimSpace(c.Param("id")),
			ExpectedRevision: req.ExpectedRevision, ModelKindId: modelKindID,
			MainContent: &aiAgentv1.MainContent{
				UserId: userID, DialogueId: dialogueID,
				DialogueKey: req.DialogueKey, Content: req.Input,
			},
			WebSearchProviderConfigId: req.WebSearchProviderConfigID,
		},
	)
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, runAgentResponseToJSON(resp))
}

// GetAgentRun returns a sanitized authoritative lifecycle projection.
// GET /api/v1/agent/runs/:id
func (h *AgentHandler) GetAgentRun(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	runID := strings.TrimSpace(c.Param("id"))
	if runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run id is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetAgentRun(ctx, &aiAgentv1.GetAgentRunRequest{UserId: userID, RunId: runID})
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, agentExecutionRunToJSON(resp.Run))
}

// GetAgentRunAccounting returns versioned parent and direct-child usage only.
// GET /api/v1/agent/runs/:id/accounting
func (h *AgentHandler) GetAgentRunAccounting(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	runID := strings.TrimSpace(c.Param("id"))
	if runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "run id is required"})
		return
	}
	var childLimit int32
	if raw := strings.TrimSpace(c.Query("child_limit")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || parsed < 1 || parsed > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "child_limit must be within 1..200"})
			return
		}
		childLimit = int32(parsed)
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetAgentRunAccounting(ctx, &aiAgentv1.GetAgentRunAccountingRequest{
		UserId: userID, RunId: runID, ChildLimit: childLimit,
	})
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, agentRunAccountingToJSON(resp.Accounting))
}

// ResumeAgentRun atomically claims an ask_human or tool-approval run.
// POST /api/v1/agent/runs/:id/resume
func (h *AgentHandler) ResumeAgentRun(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ResumeAgentRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runID := strings.TrimSpace(c.Param("id"))
	humanMode := strings.TrimSpace(req.HumanResponse) != ""
	approvalMode := strings.TrimSpace(req.ApprovalID) != "" || strings.TrimSpace(req.ResumeToken) != ""
	if runID == "" || req.ExpectedRevision <= 0 || humanMode == approvalMode ||
		(approvalMode && (strings.TrimSpace(req.ApprovalID) == "" || strings.TrimSpace(req.ResumeToken) == "")) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "choose exactly one complete resume mode"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	resp, err := h.agentClient.ResumeAgentRun(ctx, &aiAgentv1.ResumeAgentRunRequest{
		UserId: userID, RunId: runID, ExpectedRevision: req.ExpectedRevision,
		HumanResponse: req.HumanResponse, ApprovalId: req.ApprovalID, ResumeToken: req.ResumeToken,
	})
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, runAgentResponseToJSON(resp))
}

// IssueAgentResumeGrant returns a short-lived plaintext token exactly once.
// POST /api/v1/agent/tool-approvals/:id/agent-resume-grant
func (h *AgentHandler) IssueAgentResumeGrant(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req WorkflowResumeGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	approvalID := strings.TrimSpace(c.Param("id"))
	if approvalID == "" || req.ExpectedRunRevision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "approval id and expected run revision are required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.IssueAgentResumeGrant(ctx, &aiAgentv1.IssueAgentResumeGrantRequest{
		UserId: userID, ApprovalId: approvalID, ExpectedRunRevision: req.ExpectedRunRevision,
	})
	if err != nil {
		writeAgentRunControlError(c, err)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"run": agentExecutionRunToJSON(resp.Run), "resume_token": resp.ResumeToken, "expires_at": resp.ExpiresAt,
	})
}

// ConsultContentRequest 推文查询请求
type ConsultContentRequest struct {
	Content     string      `json:"content" binding:"required"`
	DialogueID  interface{} `json:"dialogue_id"`
	DialogueKey string      `json:"dialogue_key"`
	ModelKindID interface{} `json:"model_kind_id"`
}

// ConsultContent 模式二：语义搜索推文和作者
// POST /api/v1/agent/consult
func (h *AgentHandler) ConsultContent(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req ConsultContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var dialogueID uint64
	if req.DialogueID != nil {
		var err error
		dialogueID, err = parseInterfaceToUint64(req.DialogueID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dialogue_id format"})
			return
		}
	}

	var modelKindID uint64
	if req.ModelKindID != nil {
		var err error
		modelKindID, err = parseInterfaceToUint64(req.ModelKindID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model_kind_id format"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.ConsultContent(ctx, &aiAgentv1.ConsultContentRequest{
		UserId:      userID,
		ModelKindId: modelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:      userID,
			DialogueId:  dialogueID,
			DialogueKey: req.DialogueKey,
			Content:     req.Content,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	tweetList := make([]gin.H, len(resp.TweetList))
	for i, t := range resp.TweetList {
		tweetList[i] = gin.H{
			"tweet_id": strconv.FormatUint(t.TweetId, 10), // 👈 无损转换为字符串输出
			"url":      t.Url,
			"summary":  t.Summary,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"response":     resp.Response,
		"tweet_list":   tweetList,
		"dialogue_key": resp.DialogueKey,
	})
}

// AssistPublishRequest 协作写推文请求
type AssistPublishRequest struct {
	Content     string      `json:"content" binding:"required"`
	DialogueID  interface{} `json:"dialogue_id"`
	DialogueKey string      `json:"dialogue_key"`
	ModelKindID interface{} `json:"model_kind_id"`
}

// AssistPublishTwitter 模式三：协助构建推文
// POST /api/v1/agent/assist
func (h *AgentHandler) AssistPublishTwitter(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req AssistPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var dialogueID uint64
	if req.DialogueID != nil {
		var err error
		dialogueID, err = parseInterfaceToUint64(req.DialogueID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dialogue_id format"})
			return
		}
	}

	var modelKindID uint64
	if req.ModelKindID != nil {
		var err error
		modelKindID, err = parseInterfaceToUint64(req.ModelKindID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model_kind_id format"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.AssistPublishTwitter(ctx, &aiAgentv1.AssistPublishTwitterRequest{
		UserId:      userID,
		ModelKindId: modelKindID,
		MainContent: &aiAgentv1.MainContent{
			UserId:      userID,
			DialogueId:  dialogueID,
			DialogueKey: req.DialogueKey,
			Content:     req.Content,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 🎯 转换 Tweet 内的 ID 避免 JS 精度截断
	tweetList := make([]gin.H, len(resp.TweetList))
	for i, t := range resp.TweetList {
		tweetList[i] = gin.H{
			"id":            strconv.FormatUint(t.Id, 10),
			"user_id":       strconv.FormatUint(t.UserId, 10),
			"content":       t.Content,
			"media_urls":    t.MediaUrls,
			"type":          t.Type,
			"visible_type":  t.VisibleType,
			"created_at":    t.CreatedAt,
			"updated_at":    t.UpdatedAt,
			"like_count":    t.LikeCount,
			"comment_count": t.CommentCount,
			"share_count":   t.ShareCount,
			"is_liked":      t.IsLiked,
			"parent_id":     strconv.FormatUint(t.ParentId, 10),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"response":     resp.Response,
		"tweet_list":   tweetList,
		"dialogue_key": resp.DialogueKey,
		"run_id":       resp.RunId,
	})
}

// ConfirmPublishTwitter 模式三第二阶段：确认发布推文
// POST /api/v1/agent/confirm
func (h *AgentHandler) ConfirmPublishTwitter(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req ConfirmPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.agentClient.ConfirmPublishTwitter(ctx, &aiAgentv1.ConfirmPublishTwitterRequest{
		UserId:      userID,
		Content:     req.Content,
		SourceRunId: req.SourceRunID,
	})
	if err != nil {
		httpStatus := http.StatusInternalServerError
		if status.Code(err) == codes.FailedPrecondition {
			httpStatus = http.StatusUnprocessableEntity
		}
		c.JSON(httpStatus, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"response": resp.Response,
		"tweet_id": strconv.FormatUint(resp.TweetId, 10),
	})
}

// MultiAgentPublishTwitter 模式四：多 Agent 协作写推文
// POST /api/v1/agent/multi
func (h *AgentHandler) MultiAgentPublishTwitter(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req MultiAgentPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 解析 author_user_id 字符串为 uint64
	authorUserID, err := strconv.ParseUint(req.AuthorUserID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid author_user_id format"})
		return
	}

	// 解析 reference_tweet_ids 字符串数组为 uint64 数组
	var refTweetIDs []uint64
	if len(req.ReferenceTweetIDs) > 0 {
		refTweetIDs = make([]uint64, len(req.ReferenceTweetIDs))
		for i, idStr := range req.ReferenceTweetIDs {
			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid reference_tweet_id format"})
				return
			}
			refTweetIDs[i] = id
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	resp, err := h.agentClient.MultiAgentPublishTwitter(ctx, &aiAgentv1.MultiAgentPublishTwitterRequest{
		UserId:            userID,
		Domain:            req.Domain,
		AuthorUserId:      authorUserID,
		StyleRatio:        req.StyleRatio,
		ReferenceTweetIds: refTweetIDs,
		DialogueKey:       req.DialogueKey,
		Content:           req.Content,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"response":     resp.Response,
		"dialogue_key": resp.DialogueKey,
	})
}

// CreateWorkflow 保存自定义工作流
// POST /api/v1/agent/workflows
func (h *AgentHandler) CreateWorkflow(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req WorkflowSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dslJSON, ok := normalizeRawJSON(req.DSLJSON, req.DSL)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing dsl or dsl_json"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	resp, err := h.agentClient.CreateWorkflow(ctx, &aiAgentv1.CreateWorkflowRequest{
		UserId:  userID,
		Name:    req.Name,
		DslJson: dslJSON,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflowDetailToJSON(resp.Workflow),
	})
}

// UpdateWorkflow 更新自定义工作流
// PUT /api/v1/agent/workflows/:id
func (h *AgentHandler) UpdateWorkflow(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	workflowID := c.Param("id")
	var req WorkflowSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dslJSON, ok := normalizeRawJSON(req.DSLJSON, req.DSL)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing dsl or dsl_json"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	resp, err := h.agentClient.UpdateWorkflow(ctx, &aiAgentv1.UpdateWorkflowRequest{
		UserId:     userID,
		WorkflowId: workflowID,
		Name:       req.Name,
		DslJson:    dslJSON,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflowDetailToJSON(resp.Workflow),
	})
}

// ListWorkflows 获取当前用户工作流列表
// GET /api/v1/agent/workflows
func (h *AgentHandler) ListWorkflows(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.ListWorkflows(ctx, &aiAgentv1.ListWorkflowsRequest{
		UserId:   userID,
		Page:     uint32(page),
		PageSize: uint32(pageSize),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	workflows := make([]gin.H, 0, len(resp.Workflows))
	for _, workflow := range resp.Workflows {
		workflows = append(workflows, gin.H{
			"workflow_id":             workflow.WorkflowId,
			"user_id":                 strconv.FormatUint(workflow.UserId, 10),
			"name":                    workflow.Name,
			"created_at":              workflow.CreatedAt,
			"updated_at":              workflow.UpdatedAt,
			"current_revision_id":     workflow.CurrentRevisionId,
			"current_revision_number": workflow.CurrentRevisionNumber,
			"current_dsl_hash":        workflow.CurrentDslHash,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"workflows": workflows,
		"total":     resp.Total,
	})
}

// GetWorkflow 获取单个工作流 DSL
// GET /api/v1/agent/workflows/:id
func (h *AgentHandler) GetWorkflow(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetWorkflow(ctx, &aiAgentv1.GetWorkflowRequest{
		UserId:     userID,
		WorkflowId: c.Param("id"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workflow": workflowDetailToJSON(resp.Workflow),
	})
}

// ListWorkflowRevisions 查询工作流不可变版本。
// GET /api/v1/agent/workflows/:id/revisions
func (h *AgentHandler) ListWorkflowRevisions(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListWorkflowRevisions(ctx, &aiAgentv1.ListWorkflowRevisionsRequest{
		UserId: userID, WorkflowId: c.Param("id"), Page: uint32(page), PageSize: uint32(pageSize),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	revisions := make([]gin.H, 0, len(resp.Revisions))
	for _, revision := range resp.Revisions {
		revisions = append(revisions, workflowRevisionSummaryToJSON(revision))
	}
	c.JSON(http.StatusOK, gin.H{"revisions": revisions, "total": resp.Total})
}

// GetWorkflowRevision 查询指定不可变版本。
// GET /api/v1/agent/workflows/:id/revisions/:revision_id
func (h *AgentHandler) GetWorkflowRevision(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetWorkflowRevision(ctx, &aiAgentv1.GetWorkflowRevisionRequest{
		UserId: userID, WorkflowId: c.Param("id"), RevisionId: c.Param("revision_id"),
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revision": workflowRevisionToJSON(resp.Revision)})
}

// RunWorkflow 执行工作流。
// POST /api/v1/agent/workflows/:id/run
// PublishWorkflowTool binds an immutable workflow revision to a stable,
// tenant-scoped Runtime tool name.
// PUT /api/v1/agent/workflows/:id/tool-publication
func (h *AgentHandler) PublishWorkflowTool(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req WorkflowToolPublicationSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	inputSchemaJSON, ok := normalizeRawJSON(req.InputSchemaJSON, req.InputSchema)
	if !ok && (req.InputSchemaJSON != "" || len(req.InputSchema) > 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input_schema or input_schema_json"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.PublishWorkflowTool(
		ctx,
		&aiAgentv1.PublishWorkflowToolRequest{
			UserId:             userID,
			WorkflowId:         c.Param("id"),
			WorkflowRevisionId: req.WorkflowRevisionID,
			Description:        req.Description,
			InputSchemaJson:    inputSchemaJSON,
			ExpectedRevision:   req.ExpectedRevision,
		},
	)
	if err != nil {
		writeWorkflowToolPublicationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"publication": workflowToolPublicationToJSON(resp.Publication),
	})
}

// GetWorkflowToolPublication returns active or disabled publication metadata.
// GET /api/v1/agent/workflows/:id/tool-publication
func (h *AgentHandler) GetWorkflowToolPublication(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetWorkflowToolPublication(
		ctx,
		&aiAgentv1.GetWorkflowToolPublicationRequest{
			UserId: userID, WorkflowId: c.Param("id"),
		},
	)
	if err != nil {
		writeWorkflowToolPublicationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"publication": workflowToolPublicationToJSON(resp.Publication),
	})
}

// UnpublishWorkflowTool removes a workflow from Runtime discovery without
// deleting the immutable publication history.
// DELETE /api/v1/agent/workflows/:id/tool-publication
func (h *AgentHandler) UnpublishWorkflowTool(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	expectedRevision, err := strconv.ParseInt(
		strings.TrimSpace(c.Query("expected_revision")),
		10,
		64,
	)
	if err != nil || expectedRevision < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid expected_revision is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.UnpublishWorkflowTool(
		ctx,
		&aiAgentv1.UnpublishWorkflowToolRequest{
			UserId: userID, WorkflowId: c.Param("id"), ExpectedRevision: expectedRevision,
		},
	)
	if err != nil {
		writeWorkflowToolPublicationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"publication": workflowToolPublicationToJSON(resp.Publication),
	})
}

// RunWorkflow executes a workflow revision directly.
// POST /api/v1/agent/workflows/:id/run
func (h *AgentHandler) RunWorkflow(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req WorkflowRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	inputJSON, ok := normalizeRawJSON(req.InputJSON, req.Input)
	if !ok && (req.InputJSON != "" || len(req.Input) > 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input or input_json"})
		return
	}
	if inputJSON == "" {
		inputJSON = "{}"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	resp, err := h.agentClient.RunWorkflow(ctx, &aiAgentv1.RunWorkflowRequest{
		UserId:             userID,
		WorkflowId:         c.Param("id"),
		InputJson:          inputJSON,
		WorkflowRevisionId: req.WorkflowRevisionID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"run":          workflowRunToJSON(resp.Run),
		"dialogue_key": resp.DialogueKey,
		"response":     resp.Response,
		"resume_token": resp.ResumeToken,
	})
}

// ListWorkflowRuns returns tenant-scoped workflow run summaries.
// GET /api/v1/agent/workflow-runs
func (h *AgentHandler) ListWorkflowRuns(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListWorkflowRuns(ctx, &aiAgentv1.ListWorkflowRunsRequest{
		UserId: userID, WorkflowId: c.Query("workflow_id"), Status: c.Query("status"),
		Page: uint32(page), PageSize: uint32(pageSize),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runs := make([]gin.H, 0, len(resp.Runs))
	for _, run := range resp.Runs {
		runs = append(runs, workflowRunSummaryToJSON(run))
	}
	c.JSON(http.StatusOK, gin.H{"runs": runs, "total": resp.Total, "page": page, "page_size": pageSize})
}

// CancelWorkflowRun persists a cross-instance cancellation request.
// POST /api/v1/agent/workflow-runs/:id/cancel
func (h *AgentHandler) CancelWorkflowRun(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req WorkflowCancelRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.CancelWorkflowRun(ctx, &aiAgentv1.CancelWorkflowRunRequest{
		UserId: userID, RunId: c.Param("id"), Reason: req.Reason,
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"run": workflowRunSummaryToJSON(resp.Run)})
}

// GetWorkflowRun 获取工作流运行记录
// GET /api/v1/agent/workflow-runs/:id
func (h *AgentHandler) GetWorkflowRun(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetWorkflowRun(ctx, &aiAgentv1.GetWorkflowRunRequest{
		UserId: userID,
		RunId:  c.Param("id"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"run": workflowRunToJSON(resp.Run),
	})
}

// GetWorkflowRunTrace returns tenant-scoped, redacted execution records that
// are stored independently from the workflow business output.
// GET /api/v1/agent/workflow-runs/:id/traces
func (h *AgentHandler) GetWorkflowRunTrace(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetWorkflowRunTrace(ctx, &aiAgentv1.GetWorkflowRunTraceRequest{
		UserId: userID, RunId: c.Param("id"),
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	steps := make([]gin.H, 0, len(resp.Steps))
	for _, record := range resp.Steps {
		steps = append(steps, executionStepTraceToJSON(record))
	}
	llmCalls := make([]gin.H, 0, len(resp.LlmCalls))
	for _, record := range resp.LlmCalls {
		llmCalls = append(llmCalls, executionLLMCallTraceToJSON(record))
	}
	toolCalls := make([]gin.H, 0, len(resp.ToolCalls))
	for _, record := range resp.ToolCalls {
		toolCalls = append(toolCalls, executionToolCallTraceToJSON(record))
	}
	c.JSON(http.StatusOK, gin.H{
		"run": executionRunTraceToJSON(resp.Run), "steps": steps,
		"llm_calls": llmCalls, "tool_calls": toolCalls,
	})
}

// SearchWorkflowBlackboard returns a stable page from a verified historical
// Blackboard version. Values are redacted and bounded by Agent Service.
// GET /api/v1/agent/workflow-runs/:id/blackboard
func (h *AgentHandler) SearchWorkflowBlackboard(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	stateVersion, err := strconv.ParseInt(c.DefaultQuery("state_version", "0"), 10, 64)
	if err != nil || stateVersion < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state_version must be a non-negative integer"})
		return
	}
	pageSize, err := strconv.ParseUint(c.DefaultQuery("page_size", "25"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page_size must be a positive integer"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.SearchWorkflowBlackboard(ctx, &aiAgentv1.SearchWorkflowBlackboardRequest{
		UserId: userID, RunId: c.Param("id"), StateVersion: stateVersion,
		Query: c.Query("query"), PathPrefix: c.Query("path_prefix"),
		AfterCursor: c.Query("after_cursor"), PageSize: uint32(pageSize),
	})
	if err != nil {
		httpStatus := http.StatusUnprocessableEntity
		if status.Code(err) == codes.InvalidArgument {
			httpStatus = http.StatusBadRequest
		}
		c.JSON(httpStatus, gin.H{"error": err.Error()})
		return
	}
	entries := make([]gin.H, 0, len(resp.Entries))
	for _, entry := range resp.Entries {
		entries = append(entries, gin.H{
			"path": entry.Path, "value_json": entry.ValueJson, "value_type": entry.ValueType,
			"value_hash": entry.ValueHash, "value_length": entry.ValueLength, "truncated": entry.Truncated,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"run_id": resp.RunId, "state_version": resp.StateVersion,
		"base_snapshot_version": resp.BaseSnapshotVersion, "base_snapshot_hash": resp.BaseSnapshotHash,
		"state_hash": resp.StateHash, "verified": resp.Verified, "entries": entries,
		"matched_total": resp.MatchedTotal, "next_cursor": resp.NextCursor, "has_more": resp.HasMore,
	})
}

// WatchWorkflowRunEvents bridges the tenant-scoped gRPC stream to resumable
// SSE. Authentication remains in the Authorization header handled upstream.
// GET /api/v1/agent/workflow-runs/:id/events
func (h *AgentHandler) WatchWorkflowRunEvents(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	stream, err := h.agentClient.WatchWorkflowRunEvents(c.Request.Context(), &aiAgentv1.WatchWorkflowRunEventsRequest{
		UserId: userID, RunId: c.Param("id"), AfterCursor: c.Query("after_cursor"),
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "workflow run event stream unavailable"})
		return
	}

	flusher, canFlush := c.Writer.(http.Flusher)
	if !canFlush {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming is not supported"})
		return
	}
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	for {
		event, recvErr := stream.Recv()
		if recvErr != nil {
			if recvErr != io.EOF && c.Request.Context().Err() == nil {
				_ = writeWorkflowRunSSE(c.Writer, "", "control", gin.H{
					"kind": "control", "reason": "stream_unavailable",
				})
				flusher.Flush()
			}
			return
		}
		eventName := "trace"
		if event.Kind == "control" {
			eventName = "control"
		}
		if err := writeWorkflowRunSSE(c.Writer, event.Cursor, eventName, workflowRunEventToJSON(event)); err != nil {
			return
		}
		flusher.Flush()
	}
}

func writeWorkflowRunSSE(writer io.Writer, cursor, eventName string, payload interface{}) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if cursor = strings.TrimSpace(cursor); cursor != "" && validWorkflowEventCursor(cursor) {
		if _, err := fmt.Fprintf(writer, "id: %s\n", cursor); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", eventName, encoded); err != nil {
		return err
	}
	return nil
}

func validWorkflowEventCursor(cursor string) bool {
	parts := strings.Split(cursor, "-")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(cursor) > 64 {
		return false
	}
	for _, part := range parts {
		if _, err := strconv.ParseUint(part, 10, 64); err != nil {
			return false
		}
	}
	return true
}

func workflowRunEventToJSON(event *aiAgentv1.WorkflowRunEvent) gin.H {
	payload := gin.H{
		"cursor": event.Cursor, "kind": event.Kind, "reset": event.GetReset_(),
		"heartbeat": event.Heartbeat, "terminal": event.Terminal,
		"reason": event.Reason, "created_at_ms": event.CreatedAtMs,
	}
	if event.Run != nil {
		payload["run"] = executionRunTraceToJSON(event.Run)
	}
	if event.Step != nil {
		payload["step"] = executionStepTraceToJSON(event.Step)
	}
	if event.LlmCall != nil {
		payload["llm_call"] = executionLLMCallTraceToJSON(event.LlmCall)
	}
	if event.ToolCall != nil {
		payload["tool_call"] = executionToolCallTraceToJSON(event.ToolCall)
	}
	return payload
}

// GetWorkflowRunReplay returns verified persistence evidence without executing
// the workflow, model, or tools.
// GET /api/v1/agent/workflow-runs/:id/replay
func (h *AgentHandler) GetWorkflowRunReplay(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetWorkflowRunReplay(ctx, &aiAgentv1.GetWorkflowRunReplayRequest{
		UserId: userID, RunId: c.Param("id"),
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	events := make([]gin.H, 0, len(resp.Events))
	for _, event := range resp.Events {
		var delta interface{}
		if err := json.Unmarshal([]byte(event.DeltaJson), &delta); err != nil {
			delta = gin.H{}
		}
		events = append(events, gin.H{
			"sequence": event.Sequence, "node_id": event.NodeId, "delta": delta,
			"event_hash": event.EventHash, "applied_at": event.AppliedAt,
		})
	}
	compensations := make([]gin.H, 0, len(resp.Compensations))
	for _, item := range resp.Compensations {
		compensations = append(compensations, gin.H{
			"sequence": item.Sequence, "source_node_id": item.SourceNodeId,
			"step_id": item.StepId, "tool_name": item.ToolName,
			"input_hash": item.InputHash, "plan_hash": item.PlanHash,
			"status": item.Status, "attempt": item.Attempt,
			"error_message": item.ErrorMessage, "approval_request_id": item.ApprovalRequestId,
			"lease_until": item.LeaseUntil, "created_at": item.CreatedAt,
			"updated_at": item.UpdatedAt, "finished_at": item.FinishedAt,
		})
	}
	var revision interface{}
	if resp.Revision != nil {
		revision = workflowRevisionSummaryToJSON(resp.Revision)
	}
	var snapshot interface{}
	if resp.Snapshot != nil {
		snapshot = gin.H{
			"state_version": resp.Snapshot.StateVersion,
			"snapshot_hash": resp.Snapshot.SnapshotHash,
			"created_at":    resp.Snapshot.CreatedAt,
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"run": workflowRunToJSON(resp.Run), "revision": revision,
		"events": events, "snapshot": snapshot, "compensations": compensations,
		"integrity": gin.H{
			"verified": resp.Integrity.GetVerified(), "state_version": resp.Integrity.GetStateVersion(),
			"event_count": resp.Integrity.GetEventCount(), "last_sequence": resp.Integrity.GetLastSequence(),
			"snapshot_version": resp.Integrity.GetSnapshotVersion(),
		},
	})
}

// GetWorkflowCompensationJournal returns a tenant-scoped, redacted operations
// view of the durable compensation plan.
// GET /api/v1/agent/workflow-runs/:id/compensations
func (h *AgentHandler) GetWorkflowCompensationJournal(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetWorkflowCompensationJournal(ctx, &aiAgentv1.GetWorkflowCompensationJournalRequest{
		UserId: userID, RunId: c.Param("id"),
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	entries := make([]gin.H, 0, len(resp.Entries))
	for _, entry := range resp.Entries {
		entries = append(entries, gin.H{
			"sequence": entry.Sequence, "source_node_id": entry.SourceNodeId,
			"step_id": entry.StepId, "tool_name": entry.ToolName,
			"input_hash": entry.InputHash, "plan_hash": entry.PlanHash,
			"status": entry.Status, "attempt": entry.Attempt,
			"error_message": entry.ErrorMessage, "approval_request_id": entry.ApprovalRequestId,
			"lease_until": entry.LeaseUntil, "created_at": entry.CreatedAt,
			"updated_at": entry.UpdatedAt, "finished_at": entry.FinishedAt,
			"is_next": entry.IsNext,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"run": workflowCompensationRunSummaryToJSON(resp.Run), "entries": entries,
		"next_sequence": resp.NextSequence, "retry_available": resp.RetryAvailable,
	})
}

// RetryWorkflowCompensation explicitly retries the next unfinished journal
// entry. It never replays the main DAG.
// POST /api/v1/agent/workflow-runs/:id/compensations/retry
func (h *AgentHandler) RetryWorkflowCompensation(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	resp, err := h.agentClient.RetryWorkflowCompensation(ctx, &aiAgentv1.RetryWorkflowCompensationRequest{
		UserId: userID, RunId: c.Param("id"),
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"run": workflowRunToJSON(resp.Run), "response": resp.Response, "resume_token": resp.ResumeToken,
	})
}

// ResumeWorkflowRun resumes a suspended run or explicitly retries a durable
// failed compensation journal. Suspended runs still require a one-time token
// at the service boundary.
// POST /api/v1/agent/workflow-runs/:id/resume
func (h *AgentHandler) ResumeWorkflowRun(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req WorkflowResumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	inputJSON, ok := normalizeRawJSON(req.InputJSON, req.Input)
	if !ok && (req.InputJSON != "" || len(req.Input) > 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input or input_json"})
		return
	}
	if inputJSON == "" {
		inputJSON = "{}"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	resp, err := h.agentClient.ResumeWorkflowRun(ctx, &aiAgentv1.ResumeWorkflowRunRequest{
		UserId: userID, RunId: c.Param("id"), ApprovalId: req.ApprovalID,
		ResumeToken: req.ResumeToken, InputJson: inputJSON,
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"run": workflowRunToJSON(resp.Run), "response": resp.Response, "resume_token": resp.ResumeToken,
	})
}

// ListToolApprovals returns tenant-scoped, redacted approval requests.
// GET /api/v1/agent/tool-approvals
func (h *AgentHandler) ListToolApprovals(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListToolApprovals(ctx, &aiAgentv1.ListToolApprovalsRequest{
		UserId: userID, Status: c.Query("status"), Page: uint32(page), PageSize: uint32(pageSize),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(resp.Approvals))
	for _, approval := range resp.Approvals {
		items = append(items, toolApprovalToJSON(approval))
	}
	c.JSON(http.StatusOK, gin.H{"approvals": items, "total": resp.Total})
}

// DecideToolApproval performs an optimistic-lock protected approve/reject transition.
// POST /api/v1/agent/tool-approvals/:id/decision
func (h *AgentHandler) DecideToolApproval(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ToolApprovalDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.DecideToolApproval(ctx, &aiAgentv1.DecideToolApprovalRequest{
		UserId: userID, ApprovalId: c.Param("id"), Decision: req.Decision,
		Reason: req.Reason, ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"approval": toolApprovalToJSON(resp.Approval)})
}

// IssueWorkflowResumeGrant rotates the suspended Run's resume hash and returns
// a short-lived plaintext grant exactly once in this response.
// POST /api/v1/agent/tool-approvals/:id/resume-grant
func (h *AgentHandler) IssueWorkflowResumeGrant(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req WorkflowResumeGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.IssueWorkflowResumeGrant(ctx, &aiAgentv1.IssueWorkflowResumeGrantRequest{
		UserId: userID, ApprovalId: c.Param("id"), ExpectedRunRevision: req.ExpectedRunRevision,
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"run": workflowRunToJSON(resp.Run), "resume_token": resp.ResumeToken, "expires_at": resp.ExpiresAt,
	})
}

// CreateProviderConfig stores a tenant-scoped encrypted provider credential.
func (h *AgentHandler) CreateProviderConfig(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ProviderConfigSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.CreateProviderConfig(ctx, &aiAgentv1.CreateProviderConfigRequest{
		UserId: userID, Kind: req.Kind, Name: req.Name, Provider: req.Provider,
		BaseUrl: req.BaseURL, Model: req.Model, ApiKey: req.APIKey,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider_config": providerConfigToJSON(resp.ProviderConfig)})
}

func (h *AgentHandler) UpdateProviderConfig(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ProviderConfigSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.UpdateProviderConfig(ctx, &aiAgentv1.UpdateProviderConfigRequest{
		UserId: userID, ProviderConfigId: c.Param("id"), Kind: req.Kind, Name: req.Name,
		Provider: req.Provider, BaseUrl: req.BaseURL, Model: req.Model,
		ApiKey: req.APIKey, Revision: req.Revision,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider_config": providerConfigToJSON(resp.ProviderConfig)})
}

func (h *AgentHandler) ListProviderConfigs(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListProviderConfigs(ctx, &aiAgentv1.ListProviderConfigsRequest{
		UserId: userID, Page: uint32(page), PageSize: uint32(pageSize), Kind: c.Query("kind"),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(resp.ProviderConfigs))
	for _, config := range resp.ProviderConfigs {
		items = append(items, providerConfigToJSON(config))
	}
	c.JSON(http.StatusOK, gin.H{"provider_configs": items, "total": resp.Total})
}

func (h *AgentHandler) GetProviderConfig(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetProviderConfig(ctx, &aiAgentv1.GetProviderConfigRequest{
		UserId: userID, ProviderConfigId: c.Param("id"),
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider_config": providerConfigToJSON(resp.ProviderConfig)})
}

func (h *AgentHandler) RevokeProviderConfig(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	revision, _ := strconv.ParseInt(c.DefaultQuery("revision", "0"), 10, 64)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	_, err := h.agentClient.RevokeProviderConfig(ctx, &aiAgentv1.RevokeProviderConfigRequest{
		UserId: userID, ProviderConfigId: c.Param("id"), Revision: revision,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

func (h *AgentHandler) CreateExternalMCPConnection(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ExternalMCPConnectionSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.CreateExternalMCPConnection(ctx, &aiAgentv1.CreateExternalMCPConnectionRequest{
		UserId: userID, Scope: req.Scope, ProjectId: req.ProjectID, Name: req.Name,
		Transport: req.Transport, Endpoint: req.Endpoint, AuthType: req.AuthType, BearerToken: req.BearerToken,
		CredentialSource: req.CredentialSource, ManagedCredentialRef: req.ManagedCredentialRef,
	})
	if err != nil {
		writeExternalMCPError(c, err, http.StatusBadRequest)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"connection": externalMCPConnectionToJSON(resp.Connection)})
}

func (h *AgentHandler) UpdateExternalMCPConnection(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ExternalMCPConnectionSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ExpectedRevision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid expected_revision is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.UpdateExternalMCPConnection(ctx, &aiAgentv1.UpdateExternalMCPConnectionRequest{
		UserId: userID, ConnectionId: c.Param("id"), Scope: req.Scope, ProjectId: req.ProjectID,
		Name: req.Name, Transport: req.Transport, Endpoint: req.Endpoint, AuthType: req.AuthType,
		BearerToken: req.BearerToken, ExpectedRevision: req.ExpectedRevision,
		CredentialSource: req.CredentialSource, ManagedCredentialRef: req.ManagedCredentialRef,
	})
	if err != nil {
		writeExternalMCPError(c, err, http.StatusBadRequest)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"connection": externalMCPConnectionToJSON(resp.Connection)})
}

func (h *AgentHandler) ListExternalMCPConnections(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListExternalMCPConnections(ctx, &aiAgentv1.ListExternalMCPConnectionsRequest{
		UserId: userID, Page: uint32(page), PageSize: uint32(pageSize),
	})
	if err != nil {
		writeExternalMCPError(c, err, http.StatusInternalServerError)
		return
	}
	items := make([]gin.H, 0, len(resp.Connections))
	for _, connection := range resp.Connections {
		items = append(items, externalMCPConnectionToJSON(connection))
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"connections": items, "total": resp.Total})
}

func (h *AgentHandler) GetExternalMCPConnection(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.GetExternalMCPConnection(ctx, &aiAgentv1.GetExternalMCPConnectionRequest{
		UserId: userID, ConnectionId: c.Param("id"),
	})
	if err != nil {
		writeExternalMCPError(c, err, http.StatusNotFound)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"connection": externalMCPConnectionToJSON(resp.Connection)})
}

func (h *AgentHandler) RevokeExternalMCPConnection(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ExternalMCPRevisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid expected_revision is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	_, err := h.agentClient.RevokeExternalMCPConnection(ctx, &aiAgentv1.RevokeExternalMCPConnectionRequest{
		UserId: userID, ConnectionId: c.Param("id"), ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeExternalMCPError(c, err, http.StatusBadRequest)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

func (h *AgentHandler) DiscoverExternalMCPTools(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ExternalMCPRevisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid expected_revision is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	resp, err := h.agentClient.DiscoverExternalMCPTools(ctx, &aiAgentv1.DiscoverExternalMCPToolsRequest{
		UserId: userID, ConnectionId: c.Param("id"), ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeExternalMCPError(c, err, http.StatusBadGateway)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"connection": externalMCPConnectionToJSON(resp.Connection),
		"snapshot":   externalMCPSnapshotToJSON(resp.Snapshot),
	})
}

func (h *AgentHandler) ApproveExternalMCPSnapshot(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ExternalMCPRevisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid expected_revision is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.ApproveExternalMCPSnapshot(ctx, &aiAgentv1.ApproveExternalMCPSnapshotRequest{
		UserId: userID, ConnectionId: c.Param("id"), SnapshotId: c.Param("snapshot_id"),
		ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeExternalMCPError(c, err, http.StatusBadRequest)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"connection": externalMCPConnectionToJSON(resp.Connection),
		"snapshot":   externalMCPSnapshotToJSON(resp.Snapshot),
	})
}

func (h *AgentHandler) ListExternalMCPTools(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resp, err := h.agentClient.ListExternalMCPTools(ctx, &aiAgentv1.ListExternalMCPToolsRequest{
		UserId: userID, ConnectionId: c.Param("id"),
	})
	if err != nil {
		writeExternalMCPError(c, err, http.StatusBadRequest)
		return
	}
	tools := make([]gin.H, 0, len(resp.Tools))
	for _, tool := range resp.Tools {
		tools = append(tools, externalMCPToolViewToJSON(tool))
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"connection": externalMCPConnectionToJSON(resp.Connection),
		"snapshot":   externalMCPSnapshotToJSON(resp.Snapshot),
		"tools":      tools,
	})
}

func (h *AgentHandler) ConfigureExternalMCPTool(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req ExternalMCPToolPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ExpectedRevision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "snapshot_id, category and valid expected_revision are required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	resp, err := h.agentClient.ConfigureExternalMCPTool(ctx, &aiAgentv1.ConfigureExternalMCPToolRequest{
		UserId: userID, ConnectionId: c.Param("id"), SnapshotId: req.SnapshotID,
		ToolName: c.Param("tool_name"), Category: req.Category, Enabled: req.Enabled,
		ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		writeExternalMCPError(c, err, http.StatusBadRequest)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{
		"connection": externalMCPConnectionToJSON(resp.Connection),
		"tool":       externalMCPToolViewToJSON(resp.Tool),
	})
}

// ========================== 对话与历史 ==========================

// GetRepositoryDialogue 获取历史对话列表
// GET /api/v1/agent/dialogues?page=1&page_size=20
func (h *AgentHandler) GetRepositoryDialogue(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetRepositoryDialogue(ctx, &aiAgentv1.GetRepositoryDialogueRequest{
		UserId: userID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	dialogues := make([]gin.H, len(resp.RepositoryDialogueList))
	for i, d := range resp.RepositoryDialogueList {
		dialogueKey := d.DialogueKey
		if dialogueKey == "" {
			dialogueKey = strconv.FormatUint(d.Id, 10)
		}
		dialogues[i] = gin.H{
			"id":           dialogueKey,
			"legacy_id":    strconv.FormatUint(d.Id, 10),
			"dialogue_key": dialogueKey,
			"user_id":      strconv.FormatUint(d.UserId, 10),
			"title":        d.Title,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":                     resp.Code,
		"msg":                      resp.Msg,
		"repository_dialogue_list": dialogues,
	})
}

// GetDialogueDetail 获取特定对话的消息记录
// GET /api/v1/agent/dialogues/:id/messages
func (h *AgentHandler) GetDialogueDetail(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	dialogueIDStr := c.Param("id")
	dialogueID, err := strconv.ParseUint(dialogueIDStr, 10, 64)
	dialogueKey := ""
	if err != nil {
		dialogueID = 0
		dialogueKey = dialogueIDStr
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetDialogueDetail(ctx, &aiAgentv1.GetDialogueDetailRequest{
		UserId:      userID,
		DialogueId:  dialogueID,
		DialogueKey: dialogueKey,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	messages := make([]gin.H, len(resp.Messages))
	for i, m := range resp.Messages {
		messages[i] = gin.H{
			"id":                strconv.FormatUint(m.Id, 10),
			"user_id":           strconv.FormatUint(m.UserId, 10),
			"dialogue_id":       strconv.FormatUint(m.DialogueId, 10),
			"dialogue_key":      m.DialogueKey,
			"question":          m.Question,
			"response":          m.Response,
			"role":              m.Role,
			"content":           m.Content,
			"run_id":            m.RunId,
			"publishable_draft": m.PublishableDraft,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code":     resp.Code,
		"msg":      resp.Msg,
		"messages": messages,
	})
}

// ========================== 模型与文件 ==========================

// GetModelDetailedInformation 获取可用模型及支持的文件类型
// GET /api/v1/agent/models
// EndDialogueSession synchronously finalizes pending long-term memory while
// preserving the dialogue for future history queries.
// POST /api/v1/agent/dialogues/:id/end
func (h *AgentHandler) EndDialogueSession(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	dialogueIDStr := c.Param("id")
	dialogueID, err := strconv.ParseUint(dialogueIDStr, 10, 64)
	dialogueKey := ""
	if err != nil {
		dialogueID = 0
		dialogueKey = dialogueIDStr
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 35*time.Second)
	defer cancel()
	resp, err := h.agentClient.EndDialogueSession(ctx, &aiAgentv1.EndDialogueSessionRequest{
		UserId:      userID,
		DialogueId:  dialogueID,
		DialogueKey: dialogueKey,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": resp.Code, "msg": resp.Msg})
}

func (h *AgentHandler) GetModelDetailedInformation(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.agentClient.GetModelDetailedInformation(ctx, &aiAgentv1.GetModelDetailedInformationRequest{
		UserId: userID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// AnalysisFiles 解析上传的文件
// POST /api/v1/agent/files/analysis
func (h *AgentHandler) AnalysisFiles(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	fileKindIDStr := c.PostForm("file_kind_id")
	fileKindID, _ := strconv.ParseUint(fileKindIDStr, 10, 64)
	if fileKindID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing or invalid file_kind_id"})
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file"})
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "open file failed"})
		return
	}
	defer file.Close()

	fileContent := make([]byte, fileHeader.Size)
	if _, err := file.Read(fileContent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read file failed"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	resp, err := h.agentClient.AnalysisFiles(ctx, &aiAgentv1.AnalysisFilesRequest{
		UserId:      userID,
		FileKindId:  fileKindID,
		FileContent: fileContent,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// AnalyzeAlert 告警根因分析透传
func (h *AgentHandler) AnalyzeAlert(ctx context.Context, req *aiAgentv1.AnalyzeAlertRequest) (*aiAgentv1.AnalyzeAlertResponse, error) {
	return h.agentClient.AnalyzeAlert(ctx, req)
}

func normalizeRawJSON(jsonString string, raw json.RawMessage) (string, bool) {
	if jsonString != "" {
		if !json.Valid([]byte(jsonString)) {
			return "", false
		}
		return jsonString, true
	}
	if len(raw) == 0 {
		return "", false
	}
	if !json.Valid(raw) {
		return "", false
	}
	return string(raw), true
}

func workflowDetailToJSON(workflow *aiAgentv1.WorkflowDetail) gin.H {
	if workflow == nil {
		return gin.H{}
	}

	var dsl any
	if workflow.DslJson != "" {
		_ = json.Unmarshal([]byte(workflow.DslJson), &dsl)
	}
	return gin.H{
		"workflow_id":             workflow.WorkflowId,
		"user_id":                 strconv.FormatUint(workflow.UserId, 10),
		"name":                    workflow.Name,
		"dsl":                     dsl,
		"dsl_json":                workflow.DslJson,
		"created_at":              workflow.CreatedAt,
		"updated_at":              workflow.UpdatedAt,
		"current_revision_id":     workflow.CurrentRevisionId,
		"current_revision_number": workflow.CurrentRevisionNumber,
		"current_dsl_hash":        workflow.CurrentDslHash,
	}
}

func workflowRevisionSummaryToJSON(revision *aiAgentv1.WorkflowRevisionSummary) gin.H {
	if revision == nil {
		return gin.H{}
	}
	return gin.H{
		"revision_id": revision.RevisionId, "workflow_id": revision.WorkflowId,
		"user_id": strconv.FormatUint(revision.UserId, 10), "revision_number": revision.RevisionNumber,
		"dsl_hash": revision.DslHash, "created_at": revision.CreatedAt,
	}
}

func workflowRevisionToJSON(revision *aiAgentv1.WorkflowRevisionDetail) gin.H {
	if revision == nil {
		return gin.H{}
	}
	var dsl any
	if revision.DslJson != "" {
		_ = json.Unmarshal([]byte(revision.DslJson), &dsl)
	}
	return gin.H{
		"revision_id": revision.RevisionId, "workflow_id": revision.WorkflowId,
		"user_id": strconv.FormatUint(revision.UserId, 10), "revision_number": revision.RevisionNumber,
		"dsl": dsl, "dsl_json": revision.DslJson, "dsl_hash": revision.DslHash,
		"created_at": revision.CreatedAt,
	}
}

func workflowToolPublicationToJSON(publication *aiAgentv1.WorkflowToolPublication) gin.H {
	if publication == nil {
		return gin.H{}
	}
	var inputSchema any
	if publication.InputSchemaJson != "" {
		_ = json.Unmarshal([]byte(publication.InputSchemaJson), &inputSchema)
	}
	return gin.H{
		"publication_id":           publication.PublicationId,
		"user_id":                  strconv.FormatUint(publication.UserId, 10),
		"workflow_id":              publication.WorkflowId,
		"workflow_revision_id":     publication.WorkflowRevisionId,
		"workflow_revision_number": publication.WorkflowRevisionNumber,
		"workflow_dsl_hash":        publication.WorkflowDslHash,
		"tool_name":                publication.ToolName,
		"display_name":             publication.DisplayName,
		"description":              publication.Description,
		"input_schema":             inputSchema,
		"input_schema_json":        publication.InputSchemaJson,
		"status":                   publication.Status,
		"revision":                 publication.Revision,
		"created_at":               publication.CreatedAt,
		"updated_at":               publication.UpdatedAt,
	}
}

func workflowRunToJSON(run *aiAgentv1.WorkflowRun) gin.H {
	if run == nil {
		return gin.H{}
	}

	var input any
	var output any
	if run.InputJson != "" {
		_ = json.Unmarshal([]byte(run.InputJson), &input)
	}
	if run.OutputJson != "" {
		_ = json.Unmarshal([]byte(run.OutputJson), &output)
	}
	return gin.H{
		"run_id":                   run.RunId,
		"workflow_id":              run.WorkflowId,
		"user_id":                  strconv.FormatUint(run.UserId, 10),
		"status":                   run.Status,
		"input":                    input,
		"input_json":               run.InputJson,
		"output":                   output,
		"output_json":              run.OutputJson,
		"error_message":            run.ErrorMessage,
		"started_at":               run.StartedAt,
		"finished_at":              run.FinishedAt,
		"waiting_node_id":          run.WaitingNodeId,
		"suspended_at":             run.SuspendedAt,
		"approval_request_id":      run.ApprovalRequestId,
		"revision":                 run.Revision,
		"workflow_revision_id":     run.WorkflowRevisionId,
		"workflow_revision_number": run.WorkflowRevisionNumber,
		"state_version":            run.StateVersion,
		"cancel_requested_at":      run.CancelRequestedAt,
		"cancel_reason":            run.CancelReason,
		"canceled_at":              run.CanceledAt,
		"resume_grant_issued_at":   run.ResumeGrantIssuedAt,
		"resume_grant_expires_at":  run.ResumeGrantExpiresAt,
		"invocation_source":        run.InvocationSource,
		"parent_run_id":            run.ParentRunId,
		"parent_action_id":         run.ParentActionId,
	}
}

func runAgentResponseToJSON(resp *aiAgentv1.RunAgentResponse) gin.H {
	if resp == nil {
		return gin.H{}
	}
	tweets := make([]gin.H, 0, len(resp.TweetList))
	for _, tweet := range resp.TweetList {
		tweets = append(tweets, gin.H{
			"tweet_id": strconv.FormatUint(tweet.TweetId, 10), "url": tweet.Url, "summary": tweet.Summary,
		})
	}
	toolActivities := make([]gin.H, 0, len(resp.ToolActivities))
	for _, activity := range resp.ToolActivities {
		toolActivities = append(toolActivities, gin.H{
			"step_index": activity.StepIndex, "tool_name": activity.ToolName,
			"status": activity.Status, "result_count": activity.ResultCount,
		})
	}
	citations := make([]gin.H, 0, len(resp.Citations))
	for _, citation := range resp.Citations {
		citations = append(citations, gin.H{
			"citation_id": citation.CitationId, "source_type": citation.SourceType,
			"source_id": citation.SourceId, "url": citation.Url,
			"title": citation.Title, "snippet": citation.Snippet,
		})
	}
	artifacts := make([]gin.H, 0, len(resp.Artifacts))
	for _, artifact := range resp.Artifacts {
		artifacts = append(artifacts, gin.H{
			"artifact_id": artifact.ArtifactId, "type": artifact.Type, "status": artifact.Status,
			"content_type": artifact.ContentType, "content": artifact.Content,
			"source_run_id":         artifact.SourceRunId,
			"requires_confirmation": artifact.RequiresConfirmation,
		})
	}
	approvalState := gin.H{
		"status": "", "approval_id": "", "run_id": "", "action": "",
		"revision": int64(0), "expires_at": int64(0), "resume_supported": false,
	}
	if resp.ApprovalState != nil {
		approvalState = gin.H{
			"status": resp.ApprovalState.Status, "approval_id": resp.ApprovalState.ApprovalId,
			"run_id": resp.ApprovalState.RunId, "action": resp.ApprovalState.Action,
			"revision": resp.ApprovalState.Revision, "expires_at": resp.ApprovalState.ExpiresAt,
			"resume_supported": resp.ApprovalState.ResumeSupported,
		}
	}
	return gin.H{
		"response": resp.Response, "dialogue_key": resp.DialogueKey, "run_id": resp.RunId,
		"execution_profile": resp.ExecutionProfile, "capability_ids": resp.CapabilityIds,
		"tweet_list": tweets, "publishable_draft": resp.PublishableDraft,
		"tool_activities": toolActivities, "citations": citations, "artifacts": artifacts,
		"approval_state": approvalState, "run_status": resp.RunStatus,
		"selected_skill_id":               resp.SelectedSkillId,
		"selected_skill_version":          resp.SelectedSkillVersion,
		"selected_task_template_id":       resp.SelectedTaskTemplateId,
		"selected_task_template_revision": resp.SelectedTaskTemplateRevision,
		"execution_strategy_plan":         agentExecutionStrategyPlanToJSON(resp.ExecutionStrategyPlan),
	}
}

func agentExecutionRunToJSON(run *aiAgentv1.AgentExecutionRun) gin.H {
	if run == nil {
		return gin.H{}
	}
	return gin.H{
		"run_id": run.RunId, "dialogue_key": run.DialogueKey,
		"execution_profile": run.ExecutionProfile, "capability_ids": run.CapabilityIds,
		"skill_id": run.SkillId, "skill_version": run.SkillVersion,
		"task_template_id": run.TaskTemplateId, "task_template_revision": run.TaskTemplateRevision,
		"execution_strategy_plan": agentExecutionStrategyPlanToJSON(run.ExecutionStrategyPlan),
		"status":                  run.Status, "revision": run.Revision, "resume_supported": run.ResumeSupported,
		"pending_action_type": run.PendingActionType, "pending_action_name": run.PendingActionName,
		"pending_action_id": run.PendingActionId, "approval_id": run.ApprovalId,
		"approval_expires_at": run.ApprovalExpiresAt,
		"step_count":          run.StepCount, "input_tokens": run.InputTokens,
		"output_tokens": run.OutputTokens, "total_tokens": run.TotalTokens,
		"estimated_cost_micros": run.EstimatedCostMicros, "pricing_version": run.PricingVersion,
		"failure_code": run.FailureCode, "started_at": run.StartedAt, "updated_at": run.UpdatedAt,
		"suspended_at": run.SuspendedAt, "finished_at": run.FinishedAt,
	}
}

func agentExecutionStrategyPlanToJSON(plan *aiAgentv1.AgentExecutionStrategyPlan) interface{} {
	if plan == nil {
		return nil
	}
	roles := make([]gin.H, 0, len(plan.Roles))
	for _, role := range plan.Roles {
		roles = append(roles, gin.H{
			"role_id": role.RoleId, "capability_ids": role.CapabilityIds,
			"allowed_tools": role.AllowedTools, "max_steps": role.MaxSteps,
			"max_total_tokens":          role.MaxTotalTokens,
			"max_estimated_cost_micros": role.MaxEstimatedCostMicros,
			"timeout_millis":            role.TimeoutMillis,
		})
	}
	return gin.H{
		"version": plan.Version, "template_id": plan.TemplateId,
		"candidate_strategy": plan.CandidateStrategy, "selected_strategy": plan.SelectedStrategy,
		"decision": plan.Decision, "reason_code": plan.ReasonCode,
		"complexity_score": plan.ComplexityScore, "complexity_class": plan.ComplexityClass,
		"complexity_signals":       plan.ComplexitySignals,
		"estimated_latency_millis": plan.EstimatedLatencyMillis,
		"estimated_total_tokens":   plan.EstimatedTotalTokens,
		"estimated_cost_micros":    plan.EstimatedCostMicros,
		"max_parallel_roles":       plan.MaxParallelRoles, "roles": roles,
		"plan_digest": plan.PlanDigest,
	}
}

func agentRunAccountingToJSON(accounting *aiAgentv1.AgentRunAccounting) gin.H {
	if accounting == nil {
		return gin.H{}
	}
	children := make([]gin.H, 0, len(accounting.Children))
	for _, child := range accounting.Children {
		children = append(children, gin.H{
			"run_id": child.RunId, "workflow_id": child.WorkflowId,
			"parent_action_id": child.ParentActionId, "status": child.Status,
			"state": child.State, "accounting_version": child.AccountingVersion,
			"usage":         executionTokenUsageToJSON(child.Usage),
			"budget":        executionBudgetToJSON(child.Budget),
			"started_at_ms": child.StartedAtMs, "suspended_at_ms": child.SuspendedAtMs,
			"finished_at_ms": child.FinishedAtMs,
		})
	}
	return gin.H{
		"run_id": accounting.RunId, "run_status": accounting.RunStatus,
		"scope": accounting.Scope, "state": accounting.State,
		"complete": accounting.Complete, "truncated": accounting.Truncated,
		"child_run_count":          accounting.ChildRunCount,
		"included_child_run_count": accounting.IncludedChildRunCount,
		"accounting_version":       accounting.AccountingVersion,
		"parent_usage":             executionTokenUsageToJSON(accounting.ParentUsage),
		"parent_budget":            executionBudgetToJSON(accounting.ParentBudget),
		"child_usage":              executionTokenUsageToJSON(accounting.ChildUsage),
		"total_usage":              executionTokenUsageToJSON(accounting.TotalUsage),
		"children":                 children,
	}
}

func agentTaskTemplateToJSON(template *aiAgentv1.AgentTaskTemplate) gin.H {
	if template == nil {
		return gin.H{}
	}
	return gin.H{
		"contract_version": template.ContractVersion, "template_id": template.TemplateId,
		"name": template.Name, "description": template.Description,
		"instruction_template": template.InstructionTemplate,
		"status":               template.Status, "revision": template.Revision,
		"source_run_id":            template.SourceRunId,
		"source_run_revision":      template.SourceRunRevision,
		"source_result_digest":     template.SourceResultDigest,
		"source_execution_profile": template.SourceExecutionProfile,
		"capability_ids":           template.CapabilityIds,
		"skill_id":                 template.SkillId, "skill_version": template.SkillVersion,
		"source_model":            template.SourceModel,
		"agent_profile_id":        template.AgentProfileId,
		"agent_profile_version":   template.AgentProfileVersion,
		"prompt_template_id":      template.PromptTemplateId,
		"prompt_template_version": template.PromptTemplateVersion,
		"created_at":              template.CreatedAt, "updated_at": template.UpdatedAt,
		"archived_at": template.ArchivedAt,
	}
}

func agentSkillToJSON(version *aiAgentv1.AgentSkill) gin.H {
	if version == nil {
		return gin.H{}
	}
	knowledge := make([]gin.H, 0, len(version.Knowledge))
	for _, binding := range version.Knowledge {
		knowledge = append(knowledge, gin.H{
			"kind": binding.Kind, "reference": binding.Reference, "version": binding.Version,
		})
	}
	profile := gin.H{}
	if version.Profile != nil {
		profile = gin.H{
			"profile_id": version.Profile.ProfileId, "profile_version": version.Profile.ProfileVersion,
			"prompt_id": version.Profile.PromptId, "prompt_version": version.Profile.PromptVersion,
		}
	}
	budget := gin.H{}
	if version.Budget != nil {
		budget = gin.H{
			"max_steps": version.Budget.MaxSteps, "max_input_tokens": version.Budget.MaxInputTokens,
			"max_output_tokens":         version.Budget.MaxOutputTokens,
			"max_total_tokens":          version.Budget.MaxTotalTokens,
			"max_estimated_cost_micros": version.Budget.MaxEstimatedCostMicros,
			"timeout_seconds":           version.Budget.TimeoutSeconds,
		}
	}
	output := gin.H{}
	if version.Output != nil {
		output = gin.H{
			"schema_id": version.Output.SchemaId, "content_type": version.Output.ContentType,
			"schema_json": version.Output.SchemaJson,
		}
	}
	workflow := gin.H{}
	if version.Workflow != nil {
		workflow = gin.H{
			"publication_id":           version.Workflow.PublicationId,
			"publication_revision":     version.Workflow.PublicationRevision,
			"workflow_id":              version.Workflow.WorkflowId,
			"workflow_revision_id":     version.Workflow.WorkflowRevisionId,
			"workflow_revision_number": version.Workflow.WorkflowRevisionNumber,
			"workflow_dsl_hash":        version.Workflow.WorkflowDslHash,
			"tool_name":                version.Workflow.ToolName,
			"input_schema_json":        version.Workflow.InputSchemaJson,
		}
	}
	return gin.H{
		"contract_version": version.ContractVersion, "skill_id": version.SkillId,
		"version": version.Version, "display_name": version.DisplayName,
		"description": version.Description, "instructions": version.Instructions,
		"source": version.Source, "allowed_tools": version.AllowedTools,
		"knowledge": knowledge, "profile": profile, "budget": budget,
		"output": output, "workflow": workflow,
	}
}

func agentExtensionToJSON(item *aiAgentv1.AgentExtension) gin.H {
	if item == nil {
		return gin.H{}
	}
	result := gin.H{
		"contract_version": item.ContractVersion,
		"extension_id":     item.ExtensionId,
		"kind":             item.Kind,
		"name":             item.Name,
		"display_name":     item.DisplayName,
		"description":      item.Description,
		"version":          item.Version,
		"source":           item.Source,
		"capability_id":    item.CapabilityId,
		"category":         item.Category,
		"scope":            item.Scope,
		"status":           item.Status,
		"approval_mode":    item.ApprovalMode,
		"health_status":    item.HealthStatus,
	}
	if item.Skill != nil {
		result["skill"] = gin.H{
			"skill_id": item.Skill.SkillId,
			"version":  item.Skill.Version,
		}
	}
	if item.Mcp != nil {
		result["mcp"] = gin.H{
			"connection_id":       item.Mcp.ConnectionId,
			"server_id":           item.Mcp.ServerId,
			"snapshot_id":         item.Mcp.SnapshotId,
			"qualified_tool_name": item.Mcp.QualifiedToolName,
		}
	}
	return result
}

func agentMarketplaceExtensionToJSON(item *aiAgentv1.AgentMarketplaceExtension) gin.H {
	if item == nil {
		return gin.H{}
	}
	publisher := gin.H{}
	if item.Publisher != nil {
		publisher = gin.H{
			"publisher_id": item.Publisher.PublisherId,
			"display_name": item.Publisher.DisplayName,
			"verification": item.Publisher.Verification,
		}
	}
	return gin.H{
		"contract_version":       item.ContractVersion,
		"release_id":             item.ReleaseId,
		"package_id":             item.PackageId,
		"kind":                   item.Kind,
		"version":                item.Version,
		"display_name":           item.DisplayName,
		"description":            item.Description,
		"publisher":              publisher,
		"artifact_digest_sha256": item.ArtifactDigestSha256,
		"signature_key_id":       item.SignatureKeyId,
		"capability_ids":         item.CapabilityIds,
		"requested_permissions":  item.RequestedPermissions,
		"published_at_unix_ms":   item.PublishedAtUnixMs,
		"signature_verified":     item.SignatureVerified,
	}
}

func writeAgentRunControlError(c *gin.Context, err error) {
	message := status.Convert(err).Message()
	switch status.Code(err) {
	case codes.InvalidArgument:
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
	case codes.NotFound:
		c.JSON(http.StatusNotFound, gin.H{"error": message})
	case codes.Aborted:
		c.JSON(http.StatusConflict, gin.H{"error": message})
	case codes.AlreadyExists:
		c.JSON(http.StatusConflict, gin.H{"error": message})
	case codes.FailedPrecondition:
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": message})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": message})
	}
}

func writeWorkflowToolPublicationError(c *gin.Context, err error) {
	writeAgentRunControlError(c, err)
}

func setSensitiveResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Pragma", "no-cache")
}

func executionRunTraceToJSON(record *aiAgentv1.AgentRunTrace) gin.H {
	if record == nil {
		return gin.H{}
	}
	return gin.H{
		"record_id": record.RecordId, "run_id": record.RunId, "workflow_id": record.WorkflowId,
		"user_id": strconv.FormatUint(record.UserId, 10), "source": record.Source,
		"mode": record.Mode, "strategy": record.Strategy, "status": record.Status,
		"error_class": record.ErrorClass, "usage": executionTokenUsageToJSON(record.Usage),
		"budget": executionBudgetToJSON(record.Budget), "started_at_ms": record.StartedAtMs,
		"finished_at_ms": record.FinishedAtMs, "duration_ms": record.DurationMs, "updated_at_ms": record.UpdatedAtMs,
	}
}

func executionStepTraceToJSON(record *aiAgentv1.AgentStepTrace) gin.H {
	if record == nil {
		return gin.H{}
	}
	return gin.H{
		"record_id": record.RecordId, "run_id": record.RunId, "workflow_id": record.WorkflowId,
		"user_id": strconv.FormatUint(record.UserId, 10), "source": record.Source,
		"step_id": record.StepId, "parent_step_id": record.ParentStepId, "sequence": record.Sequence,
		"step_type": record.StepType, "name": record.Name, "status": record.Status,
		"attempt": record.Attempt, "max_attempts": record.MaxAttempts, "error_class": record.ErrorClass,
		"started_at_ms": record.StartedAtMs, "finished_at_ms": record.FinishedAtMs,
		"duration_ms": record.DurationMs, "updated_at_ms": record.UpdatedAtMs,
	}
}

func executionLLMCallTraceToJSON(record *aiAgentv1.AgentLLMCallTrace) gin.H {
	if record == nil {
		return gin.H{}
	}
	return gin.H{
		"record_id": record.RecordId, "run_id": record.RunId, "workflow_id": record.WorkflowId,
		"user_id": strconv.FormatUint(record.UserId, 10), "source": record.Source,
		"step_id": record.StepId, "sequence": record.Sequence, "model": record.Model,
		"provider": record.Provider, "status": record.Status, "error_class": record.ErrorClass,
		"prompt_hash": record.PromptHash, "prompt_length": record.PromptLength,
		"completion_hash": record.CompletionHash, "completion_length": record.CompletionLength,
		"prompt_template_id": record.PromptTemplateId, "prompt_template_version": record.PromptTemplateVersion,
		"prompt_sample": record.PromptSample, "completion_sample": record.CompletionSample,
		"prompt_sample_status":     record.PromptSampleStatus,
		"completion_sample_status": record.CompletionSampleStatus,
		"content_sample_policy":    record.ContentSamplePolicy,
		"usage":                    executionTokenUsageToJSON(record.Usage), "started_at_ms": record.StartedAtMs,
		"finished_at_ms": record.FinishedAtMs, "duration_ms": record.DurationMs, "updated_at_ms": record.UpdatedAtMs,
	}
}

func executionToolCallTraceToJSON(record *aiAgentv1.AgentToolCallTrace) gin.H {
	if record == nil {
		return gin.H{}
	}
	return gin.H{
		"record_id": record.RecordId, "run_id": record.RunId, "workflow_id": record.WorkflowId,
		"user_id": strconv.FormatUint(record.UserId, 10), "source": record.Source,
		"step_id": record.StepId, "sequence": record.Sequence, "tool_name": record.ToolName,
		"category": record.Category, "status": record.Status, "error_class": record.ErrorClass,
		"attempts": record.Attempts, "arguments_hash": record.ArgumentsHash,
		"arguments_length": record.ArgumentsLength, "output_hash": record.OutputHash,
		"output_length": record.OutputLength, "output_storage": record.OutputStorage,
		"output_reference": record.OutputReference, "output_content_type": record.OutputContentType,
		"started_at_ms":  record.StartedAtMs,
		"finished_at_ms": record.FinishedAtMs, "duration_ms": record.DurationMs, "updated_at_ms": record.UpdatedAtMs,
	}
}

func executionTokenUsageToJSON(usage *aiAgentv1.ExecutionTokenUsage) gin.H {
	if usage == nil {
		return gin.H{}
	}
	return gin.H{
		"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"total_tokens": usage.TotalTokens, "estimated": usage.Estimated,
		"estimated_cost_micros": usage.EstimatedCostMicros,
		"cost_estimated":        usage.CostEstimated, "pricing_version": usage.PricingVersion,
	}
}

func executionBudgetToJSON(budget *aiAgentv1.ExecutionBudgetSnapshot) gin.H {
	if budget == nil {
		return gin.H{}
	}
	return gin.H{
		"max_steps": budget.MaxSteps, "max_total_tokens": budget.MaxTotalTokens,
		"max_estimated_cost_micros": budget.MaxEstimatedCostMicros,
		"consumed_steps":            budget.ConsumedSteps, "consumed_tokens": budget.ConsumedTokens,
		"consumed_cost_micros": budget.ConsumedCostMicros,
	}
}

func workflowRunSummaryToJSON(run *aiAgentv1.WorkflowRunSummary) gin.H {
	if run == nil {
		return gin.H{}
	}
	return gin.H{
		"run_id": run.RunId, "workflow_id": run.WorkflowId, "status": run.Status,
		"error_message": run.ErrorMessage, "started_at": run.StartedAt,
		"finished_at": run.FinishedAt, "waiting_node_id": run.WaitingNodeId,
		"approval_request_id":      run.ApprovalRequestId,
		"workflow_revision_number": run.WorkflowRevisionNumber,
		"state_version":            run.StateVersion,
		"cancel_requested_at":      run.CancelRequestedAt,
		"cancel_reason":            run.CancelReason,
		"canceled_at":              run.CanceledAt,
	}
}

func workflowCompensationRunSummaryToJSON(run *aiAgentv1.WorkflowCompensationRunSummary) gin.H {
	if run == nil {
		return gin.H{}
	}
	return gin.H{
		"run_id": run.RunId, "workflow_id": run.WorkflowId, "status": run.Status,
		"error_message": run.ErrorMessage, "started_at": run.StartedAt,
		"finished_at": run.FinishedAt, "waiting_node_id": run.WaitingNodeId,
		"approval_request_id": run.ApprovalRequestId,
	}
}

func toolApprovalToJSON(approval *aiAgentv1.ToolApprovalRequest) gin.H {
	if approval == nil {
		return gin.H{}
	}
	var inputs any
	if approval.RedactedInputsJson != "" {
		_ = json.Unmarshal([]byte(approval.RedactedInputsJson), &inputs)
	}
	return gin.H{
		"approval_id": approval.ApprovalId, "user_id": strconv.FormatUint(approval.UserId, 10),
		"run_id": approval.RunId, "step_id": approval.StepId, "tool_name": approval.ToolName,
		"source": approval.Source, "category": approval.Category, "status": approval.Status,
		"redacted_inputs": inputs, "idempotency_key": approval.IdempotencyKey,
		"reason": approval.Reason, "revision": approval.Revision,
		"created_at": approval.CreatedAt, "expires_at": approval.ExpiresAt, "decided_at": approval.DecidedAt,
	}
}

func providerConfigToJSON(config *aiAgentv1.ProviderConfig) gin.H {
	if config == nil {
		return gin.H{}
	}
	return gin.H{
		"provider_config_id": config.ProviderConfigId,
		"kind":               config.Kind,
		"name":               config.Name, "provider": config.Provider,
		"base_url": config.BaseUrl, "model": config.Model,
		"status": config.Status, "has_secret": config.HasSecret,
		"credential_version": config.CredentialVersion,
		"revision":           config.Revision,
		"created_at":         config.CreatedAt, "updated_at": config.UpdatedAt,
	}
}

func externalMCPConnectionToJSON(connection *aiAgentv1.ExternalMCPConnection) gin.H {
	if connection == nil {
		return gin.H{}
	}
	return gin.H{
		"connection_id": connection.ConnectionId, "owner_user_id": strconv.FormatUint(connection.UserId, 10),
		"scope": connection.Scope, "project_id": connection.ProjectId,
		"server_id": connection.ServerId, "name": connection.Name, "transport": connection.Transport,
		"endpoint": connection.Endpoint, "auth_type": connection.AuthType,
		"credential_source": connection.CredentialSource, "status": connection.Status,
		"managed_credential_ref":     connection.ManagedCredentialRef,
		"managed_credential_version": connection.ManagedCredentialVersion,
		"has_secret":                 connection.HasSecret, "credential_version": connection.CredentialVersion,
		"latest_snapshot_id": connection.LatestSnapshotId, "pending_snapshot_id": connection.PendingSnapshotId,
		"active_snapshot_id": connection.ActiveSnapshotId, "discovery_status": connection.DiscoveryStatus,
		"last_error_code": connection.LastErrorCode, "last_checked_at": connection.LastCheckedAt,
		"health_status": connection.HealthStatus, "health_error_code": connection.HealthErrorCode,
		"health_failure_count":   connection.HealthFailureCount,
		"last_health_checked_at": connection.LastHealthCheckedAt,
		"last_healthy_at":        connection.LastHealthyAt,
		"next_health_check_at":   connection.NextHealthCheckAt,
		"revision":               connection.Revision, "created_at": connection.CreatedAt, "updated_at": connection.UpdatedAt,
	}
}

func externalMCPSnapshotToJSON(snapshot *aiAgentv1.ExternalMCPToolSnapshot) gin.H {
	if snapshot == nil {
		return gin.H{}
	}
	tools := make([]gin.H, 0, len(snapshot.Tools))
	for _, tool := range snapshot.Tools {
		tools = append(tools, gin.H{
			"name": tool.Name, "qualified_name": tool.QualifiedName, "description": tool.Description,
			"input_schema_json": tool.InputSchemaJson, "output_schema_json": tool.OutputSchemaJson,
			"declared_read_only":         tool.DeclaredReadOnly,
			"declared_idempotent":        tool.DeclaredIdempotent,
			"idempotency_key_argument":   tool.IdempotencyKeyArgument,
			"supports_write_idempotency": tool.SupportsWriteIdempotency,
		})
	}
	return gin.H{
		"snapshot_id": snapshot.SnapshotId, "connection_id": snapshot.ConnectionId,
		"server_id": snapshot.ServerId, "schema_hash": snapshot.SchemaHash,
		"version": snapshot.Version, "tools": tools, "created_at": snapshot.CreatedAt,
	}
}

func externalMCPToolViewToJSON(tool *aiAgentv1.ExternalMCPToolView) gin.H {
	if tool == nil {
		return gin.H{}
	}
	schema := gin.H{}
	if tool.Schema != nil {
		schema = gin.H{
			"name": tool.Schema.Name, "qualified_name": tool.Schema.QualifiedName,
			"description": tool.Schema.Description, "input_schema_json": tool.Schema.InputSchemaJson,
			"output_schema_json":         tool.Schema.OutputSchemaJson,
			"declared_read_only":         tool.Schema.DeclaredReadOnly,
			"declared_idempotent":        tool.Schema.DeclaredIdempotent,
			"idempotency_key_argument":   tool.Schema.IdempotencyKeyArgument,
			"supports_write_idempotency": tool.Schema.SupportsWriteIdempotency,
		}
	}
	policy := gin.H{}
	if tool.Policy != nil {
		policy = gin.H{
			"snapshot_id": tool.Policy.SnapshotId, "tool_name": tool.Policy.ToolName,
			"qualified_name": tool.Policy.QualifiedName, "category": tool.Policy.Category,
			"enabled": tool.Policy.Enabled, "updated_at": tool.Policy.UpdatedAt,
		}
	}
	return gin.H{"schema": schema, "policy": policy}
}

func writeExternalMCPError(c *gin.Context, err error, fallbackStatus int) {
	code := status.Code(err)
	switch code {
	case codes.Aborted:
		fallbackStatus = http.StatusConflict
	case codes.NotFound:
		fallbackStatus = http.StatusNotFound
	case codes.PermissionDenied:
		fallbackStatus = http.StatusForbidden
	case codes.FailedPrecondition:
		fallbackStatus = http.StatusPreconditionFailed
	case codes.Unavailable, codes.DeadlineExceeded:
		fallbackStatus = http.StatusBadGateway
	}
	c.JSON(fallbackStatus, gin.H{"error": status.Convert(err).Message()})
}
