package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/gateway/middleware"
)

type agentProjectCreateRequest struct {
	Name string `json:"name" binding:"required"`
}

type agentProjectMemberRequest struct {
	Role             string `json:"role"`
	ExpectedRevision int64  `json:"expected_revision" binding:"required"`
}

func (h *AgentHandler) CreateAgentProject(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var request agentProjectCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	response, err := h.agentClient.CreateAgentProject(ctx, &aiAgentv1.CreateAgentProjectRequest{
		ActorUserId: userID, Name: request.Name,
	})
	if err != nil {
		writeAgentProjectError(c, err, http.StatusBadRequest)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"project": agentProjectToJSON(response.Project)})
}

func (h *AgentHandler) ListAgentProjects(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageErr != nil || pageSizeErr != nil || page < 1 || pageSize < 1 || pageSize > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page must be positive and page_size must be between 1 and 100"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	response, err := h.agentClient.ListAgentProjects(ctx, &aiAgentv1.ListAgentProjectsRequest{
		UserId: userID, Page: uint32(page), PageSize: uint32(pageSize),
	})
	if err != nil {
		writeAgentProjectError(c, err, http.StatusInternalServerError)
		return
	}
	projects := make([]gin.H, 0, len(response.Projects))
	for _, project := range response.Projects {
		projects = append(projects, agentProjectToJSON(project))
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"projects": projects, "total": response.Total})
}

func (h *AgentHandler) GetAgentProject(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	response, err := h.agentClient.GetAgentProject(ctx, &aiAgentv1.GetAgentProjectRequest{
		UserId: userID, ProjectId: c.Param("project_id"),
	})
	if err != nil {
		writeAgentProjectError(c, err, http.StatusNotFound)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"project": agentProjectToJSON(response.Project)})
}

func (h *AgentHandler) UpsertAgentProjectMember(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	targetUserID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil || targetUserID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid member user_id is required"})
		return
	}
	var request agentProjectMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.ExpectedRevision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role and valid expected_revision are required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	response, err := h.agentClient.UpsertAgentProjectMember(ctx, &aiAgentv1.UpsertAgentProjectMemberRequest{
		ActorUserId: userID, ProjectId: c.Param("project_id"), TargetUserId: targetUserID,
		Role: request.Role, ExpectedRevision: request.ExpectedRevision,
	})
	if err != nil {
		writeAgentProjectError(c, err, http.StatusBadRequest)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"project": agentProjectToJSON(response.Project)})
}

func (h *AgentHandler) RemoveAgentProjectMember(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	targetUserID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil || targetUserID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid member user_id is required"})
		return
	}
	var request agentProjectMemberRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.ExpectedRevision <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid expected_revision is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	response, err := h.agentClient.RemoveAgentProjectMember(ctx, &aiAgentv1.RemoveAgentProjectMemberRequest{
		ActorUserId: userID, ProjectId: c.Param("project_id"), TargetUserId: targetUserID,
		ExpectedRevision: request.ExpectedRevision,
	})
	if err != nil {
		writeAgentProjectError(c, err, http.StatusBadRequest)
		return
	}
	setSensitiveResponseHeaders(c)
	c.JSON(http.StatusOK, gin.H{"project": agentProjectToJSON(response.Project)})
}

func agentProjectToJSON(project *aiAgentv1.AgentProject) gin.H {
	if project == nil {
		return gin.H{}
	}
	members := make([]gin.H, 0, len(project.Members))
	for _, member := range project.Members {
		members = append(members, gin.H{
			"user_id": strconv.FormatUint(member.UserId, 10), "role": member.Role,
			"added_by":   strconv.FormatUint(member.AddedBy, 10),
			"created_at": member.CreatedAt, "updated_at": member.UpdatedAt,
		})
	}
	return gin.H{
		"project_id": project.ProjectId, "name": project.Name,
		"owner_id": strconv.FormatUint(project.OwnerId, 10), "members": members,
		"revision": project.Revision, "created_at": project.CreatedAt, "updated_at": project.UpdatedAt,
		"current_role": project.CurrentRole,
	}
}

func writeAgentProjectError(c *gin.Context, err error, fallbackStatus int) {
	switch status.Code(err) {
	case codes.PermissionDenied:
		fallbackStatus = http.StatusForbidden
	case codes.NotFound:
		fallbackStatus = http.StatusNotFound
	case codes.Aborted:
		fallbackStatus = http.StatusConflict
	case codes.FailedPrecondition:
		fallbackStatus = http.StatusPreconditionFailed
	case codes.Unavailable, codes.DeadlineExceeded:
		fallbackStatus = http.StatusServiceUnavailable
	}
	c.JSON(fallbackStatus, gin.H{"error": status.Convert(err).Message()})
}
