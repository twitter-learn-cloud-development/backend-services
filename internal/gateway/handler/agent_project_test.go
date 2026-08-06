package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type agentProjectGatewayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	createRequest *aiAgentv1.CreateAgentProjectRequest
	upsertRequest *aiAgentv1.UpsertAgentProjectMemberRequest
	upsertError   error
}

func (fake *agentProjectGatewayClientFake) CreateAgentProject(
	_ context.Context,
	request *aiAgentv1.CreateAgentProjectRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.CreateAgentProjectResponse, error) {
	fake.createRequest = request
	return &aiAgentv1.CreateAgentProjectResponse{Project: &aiAgentv1.AgentProject{
		ProjectId: "agentproj_0123456789abcdef0123456789abcdef",
		Name:      request.Name,
		OwnerId:   request.ActorUserId,
		Members: []*aiAgentv1.AgentProjectMember{{
			UserId: request.ActorUserId, Role: "owner", AddedBy: request.ActorUserId,
		}},
		Revision: 1, CurrentRole: "owner",
	}}, nil
}

func (fake *agentProjectGatewayClientFake) UpsertAgentProjectMember(
	_ context.Context,
	request *aiAgentv1.UpsertAgentProjectMemberRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.UpsertAgentProjectMemberResponse, error) {
	fake.upsertRequest = request
	if fake.upsertError != nil {
		return nil, fake.upsertError
	}
	return &aiAgentv1.UpsertAgentProjectMemberResponse{Project: &aiAgentv1.AgentProject{
		ProjectId: request.ProjectId,
		OwnerId:   request.ActorUserId,
		Members: []*aiAgentv1.AgentProjectMember{{
			UserId: request.TargetUserId, Role: request.Role, AddedBy: request.ActorUserId,
		}},
		Revision: request.ExpectedRevision + 1, CurrentRole: "owner",
	}}, nil
}

func TestCreateAgentProjectUsesAuthenticatedActorAndStringifiesUserIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentProjectGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/projects",
		bytes.NewBufferString(`{"name":"Platform Research","actor_user_id":"7"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(9_007_199_254_740_993))

	handler.CreateAgentProject(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.createRequest)
	require.Equal(t, uint64(9_007_199_254_740_993), client.createRequest.ActorUserId)
	require.Equal(t, "Platform Research", client.createRequest.Name)
	require.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
	var response struct {
		Project struct {
			OwnerID string `json:"owner_id"`
			Members []struct {
				UserID  string `json:"user_id"`
				AddedBy string `json:"added_by"`
			} `json:"members"`
		} `json:"project"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "9007199254740993", response.Project.OwnerID)
	require.Equal(t, "9007199254740993", response.Project.Members[0].UserID)
	require.Equal(t, "9007199254740993", response.Project.Members[0].AddedBy)
}

func TestUpsertAgentProjectMemberUsesJWTActorPathTargetAndCASRevision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentProjectGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{
		{Key: "project_id", Value: "agentproj_0123456789abcdef0123456789abcdef"},
		{Key: "user_id", Value: "42"},
	}
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/agent/projects/agentproj_0123456789abcdef0123456789abcdef/members/42",
		bytes.NewBufferString(`{"role":"editor","expected_revision":3,"actor_user_id":"1","target_user_id":"2"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(7))

	handler.UpsertAgentProjectMember(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.upsertRequest)
	require.Equal(t, uint64(7), client.upsertRequest.ActorUserId)
	require.Equal(t, uint64(42), client.upsertRequest.TargetUserId)
	require.Equal(t, "editor", client.upsertRequest.Role)
	require.Equal(t, int64(3), client.upsertRequest.ExpectedRevision)
}

func TestUpsertAgentProjectMemberMapsPermissionDeniedToForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentProjectGatewayClientFake{upsertError: status.Error(codes.PermissionDenied, "project access denied")}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{
		{Key: "project_id", Value: "agentproj_0123456789abcdef0123456789abcdef"},
		{Key: "user_id", Value: "42"},
	}
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/agent/projects/agentproj_0123456789abcdef0123456789abcdef/members/42",
		bytes.NewBufferString(`{"role":"viewer","expected_revision":1}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(8))

	handler.UpsertAgentProjectMember(ctx)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "project access denied")
}

func TestListAgentProjectsRejectsInvalidPaginationBeforeGRPC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAgentHandler(&agentProjectGatewayClientFake{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/projects?page=-1&page_size=101", nil)
	ctx.Set("user_id", uint64(7))

	handler.ListAgentProjects(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "page must be positive")
}
