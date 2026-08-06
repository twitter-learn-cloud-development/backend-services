package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type agentSkillGatewayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	listRequest *aiAgentv1.ListAgentSkillsRequest
	getRequest  *aiAgentv1.GetAgentSkillRequest
}

func (f *agentSkillGatewayClientFake) ListAgentSkills(
	_ context.Context,
	request *aiAgentv1.ListAgentSkillsRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.ListAgentSkillsResponse, error) {
	f.listRequest = request
	return &aiAgentv1.ListAgentSkillsResponse{
		Code: 200,
		Skills: []*aiAgentv1.AgentSkill{{
			ContractVersion: "skill.v1",
			SkillId:         "workflow_aaaaaaaaaaaaaaaaaaaaaaaa",
			Version:         "v1-deadbeef",
			DisplayName:     "Digest",
			AllowedTools:    []string{"workflow_aaaaaaaaaaaaaaaaaaaaaaaa"},
			Profile: &aiAgentv1.AgentSkillProfileBinding{
				ProfileId: "unified.workflow", ProfileVersion: "v1",
			},
			Budget: &aiAgentv1.AgentSkillBudget{MaxSteps: 5, TimeoutSeconds: 75},
			Output: &aiAgentv1.AgentSkillOutputContract{SchemaId: "workflow.run.v1"},
			Workflow: &aiAgentv1.AgentSkillWorkflowBinding{
				WorkflowId: "aaaaaaaaaaaaaaaaaaaaaaaa",
			},
		}},
	}, nil
}

func (f *agentSkillGatewayClientFake) GetAgentSkill(
	_ context.Context,
	request *aiAgentv1.GetAgentSkillRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.GetAgentSkillResponse, error) {
	f.getRequest = request
	return &aiAgentv1.GetAgentSkillResponse{
		Code: 200,
		Skill: &aiAgentv1.AgentSkill{
			ContractVersion: "skill.v1",
			SkillId:         request.SkillId,
			Version:         request.Version,
		},
	}, nil
}

func TestListAgentSkillsUsesAuthenticatedTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentSkillGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/skills?limit=10", nil)
	ctx.Set("user_id", uint64(42))

	handler.ListAgentSkills(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.listRequest)
	require.Equal(t, uint64(42), client.listRequest.UserId)
	require.Equal(t, int32(10), client.listRequest.Limit)
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))
	var response struct {
		Skills []struct {
			SkillID string `json:"skill_id"`
			Version string `json:"version"`
		} `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Skills, 1)
	require.Equal(t, "workflow_aaaaaaaaaaaaaaaaaaaaaaaa", response.Skills[0].SkillID)
	require.Equal(t, "v1-deadbeef", response.Skills[0].Version)
}

func TestGetAgentSkillRequiresExactVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentSkillGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "workflow_aaaaaaaaaaaaaaaaaaaaaaaa"}}
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/agent/skills/workflow_aaaaaaaaaaaaaaaaaaaaaaaa?version=v1-deadbeef",
		nil,
	)
	ctx.Set("user_id", uint64(42))

	handler.GetAgentSkill(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.getRequest)
	require.Equal(t, uint64(42), client.getRequest.UserId)
	require.Equal(t, "workflow_aaaaaaaaaaaaaaaaaaaaaaaa", client.getRequest.SkillId)
	require.Equal(t, "v1-deadbeef", client.getRequest.Version)
}
