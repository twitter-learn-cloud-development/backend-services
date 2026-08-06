package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type agentExtensionGatewayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	request *aiAgentv1.ListAgentExtensionsRequest
}

func (client *agentExtensionGatewayClientFake) ListAgentExtensions(
	_ context.Context,
	request *aiAgentv1.ListAgentExtensionsRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.ListAgentExtensionsResponse, error) {
	client.request = request
	return &aiAgentv1.ListAgentExtensionsResponse{
		Code: 200, Msg: "success", ContractVersion: "agent.extension.v1",
		Extensions: []*aiAgentv1.AgentExtension{{
			ContractVersion: "agent.extension.v1", ExtensionId: "mcp_tool_deadbeef",
			Kind: "mcp_tool", Name: "crm.create_record", DisplayName: "CRM / create_record",
			Description: "Create a CRM record.", Version: "snapshot-3-aaaaaaaaaaaa",
			Source: "external_mcp", CapabilityId: "connector.mcp", Category: "write",
			Scope: "project", Status: "available", ApprovalMode: "required", HealthStatus: "healthy",
			Mcp: &aiAgentv1.AgentExtensionMCPReference{
				ConnectionId: "connection-1", ServerId: "crm", SnapshotId: "snapshot-1",
				QualifiedToolName: "crm.create_record",
			},
		}},
		Sources: []*aiAgentv1.AgentExtensionSourceStatus{{
			Source: "external_mcp", State: "ready", EntryCount: 1,
		}},
		NextCursor: "cursor-2", HasMore: true,
	}, nil
}

func TestListAgentExtensionsUsesAuthenticatedTenantAndStableCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentExtensionGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/agent/extensions?kind=mcp_tool&category=write&scope=project&status=available&search=crm&after_cursor=cursor-1&page_size=12&user_id=999",
		nil,
	)
	ctx.Set("user_id", uint64(42))

	handler.ListAgentExtensions(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.request)
	require.Equal(t, uint64(42), client.request.UserId)
	require.Equal(t, "mcp_tool", client.request.Kind)
	require.Equal(t, "write", client.request.Category)
	require.Equal(t, "project", client.request.Scope)
	require.Equal(t, "available", client.request.Status)
	require.Equal(t, "crm", client.request.Search)
	require.Equal(t, "cursor-1", client.request.AfterCursor)
	require.Equal(t, int32(12), client.request.PageSize)
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))

	var response struct {
		ContractVersion string `json:"contract_version"`
		Extensions      []struct {
			ExtensionID  string `json:"extension_id"`
			ApprovalMode string `json:"approval_mode"`
			MCP          struct {
				ConnectionID      string `json:"connection_id"`
				QualifiedToolName string `json:"qualified_tool_name"`
			} `json:"mcp"`
		} `json:"extensions"`
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "agent.extension.v1", response.ContractVersion)
	require.Len(t, response.Extensions, 1)
	require.Equal(t, "mcp_tool_deadbeef", response.Extensions[0].ExtensionID)
	require.Equal(t, "required", response.Extensions[0].ApprovalMode)
	require.Equal(t, "connection-1", response.Extensions[0].MCP.ConnectionID)
	require.Equal(t, "crm.create_record", response.Extensions[0].MCP.QualifiedToolName)
	require.Equal(t, "cursor-2", response.NextCursor)
	require.True(t, response.HasMore)
	for _, forbidden := range []string{"endpoint", "credential", "api_key", "input_schema"} {
		require.NotContains(t, strings.ToLower(recorder.Body.String()), forbidden)
	}
}

func TestListAgentExtensionsRejectsOversizedPageBeforeGRPC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentExtensionGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/agent/extensions?page_size=51",
		nil,
	)
	ctx.Set("user_id", uint64(42))

	handler.ListAgentExtensions(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Nil(t, client.request)
}
