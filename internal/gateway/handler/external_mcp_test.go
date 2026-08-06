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
)

type externalMCPGatewayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	createRequest        *aiAgentv1.CreateExternalMCPConnectionRequest
	discoverRequest      *aiAgentv1.DiscoverExternalMCPToolsRequest
	listToolsRequest     *aiAgentv1.ListExternalMCPToolsRequest
	configureToolRequest *aiAgentv1.ConfigureExternalMCPToolRequest
}

func (fake *externalMCPGatewayClientFake) ListExternalMCPTools(
	_ context.Context,
	request *aiAgentv1.ListExternalMCPToolsRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.ListExternalMCPToolsResponse, error) {
	fake.listToolsRequest = request
	return &aiAgentv1.ListExternalMCPToolsResponse{
		Connection: &aiAgentv1.ExternalMCPConnection{
			ConnectionId: request.ConnectionId, ServerId: "mcp_server", ActiveSnapshotId: "mcpsnap_1",
			DiscoveryStatus: "ready", Revision: 3, HasSecret: true,
			HealthStatus: "degraded", HealthErrorCode: "timeout", HealthFailureCount: 1,
			LastHealthCheckedAt: 1_721_000_000,
		},
		Snapshot: &aiAgentv1.ExternalMCPToolSnapshot{
			SnapshotId: "mcpsnap_1", ConnectionId: request.ConnectionId, ServerId: "mcp_server",
		},
		Tools: []*aiAgentv1.ExternalMCPToolView{{
			Schema: &aiAgentv1.ExternalMCPToolSchema{
				Name: "lookup", QualifiedName: "mcp_server.lookup",
				InputSchemaJson: `{"type":"object"}`, DeclaredReadOnly: true,
				DeclaredIdempotent: true, IdempotencyKeyArgument: "idempotency_key",
				SupportsWriteIdempotency: true,
			},
			Policy: &aiAgentv1.ExternalMCPToolPolicy{
				SnapshotId: "mcpsnap_1", ToolName: "lookup", QualifiedName: "mcp_server.lookup",
				Category: "read", Enabled: true,
			},
		}},
	}, nil
}

func (fake *externalMCPGatewayClientFake) ConfigureExternalMCPTool(
	_ context.Context,
	request *aiAgentv1.ConfigureExternalMCPToolRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.ConfigureExternalMCPToolResponse, error) {
	fake.configureToolRequest = request
	return &aiAgentv1.ConfigureExternalMCPToolResponse{
		Connection: &aiAgentv1.ExternalMCPConnection{
			ConnectionId: request.ConnectionId, ServerId: "mcp_server", ActiveSnapshotId: request.SnapshotId,
			DiscoveryStatus: "ready", Revision: request.ExpectedRevision + 1,
		},
		Tool: &aiAgentv1.ExternalMCPToolView{
			Schema: &aiAgentv1.ExternalMCPToolSchema{
				Name: "lookup", QualifiedName: request.ToolName,
				InputSchemaJson: `{"type":"object"}`, DeclaredReadOnly: true,
			},
			Policy: &aiAgentv1.ExternalMCPToolPolicy{
				SnapshotId: request.SnapshotId, ToolName: "lookup", QualifiedName: request.ToolName,
				Category: request.Category, Enabled: request.Enabled,
			},
		},
	}, nil
}

func (fake *externalMCPGatewayClientFake) CreateExternalMCPConnection(
	_ context.Context,
	request *aiAgentv1.CreateExternalMCPConnectionRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.CreateExternalMCPConnectionResponse, error) {
	fake.createRequest = request
	credentialSource := request.CredentialSource
	if credentialSource == "" {
		credentialSource = "user"
	}
	return &aiAgentv1.CreateExternalMCPConnectionResponse{Connection: &aiAgentv1.ExternalMCPConnection{
		ConnectionId: "mcpconn_1", ServerId: "mcp_server", Name: request.Name,
		Scope: request.Scope, ProjectId: request.ProjectId,
		Transport: request.Transport, Endpoint: request.Endpoint, AuthType: request.AuthType,
		CredentialSource: credentialSource, ManagedCredentialRef: request.ManagedCredentialRef,
		ManagedCredentialVersion: 3, Status: "active", HasSecret: true, Revision: 1,
	}}, nil
}

func (fake *externalMCPGatewayClientFake) DiscoverExternalMCPTools(
	_ context.Context,
	request *aiAgentv1.DiscoverExternalMCPToolsRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.DiscoverExternalMCPToolsResponse, error) {
	fake.discoverRequest = request
	return &aiAgentv1.DiscoverExternalMCPToolsResponse{
		Connection: &aiAgentv1.ExternalMCPConnection{
			ConnectionId: request.ConnectionId, ServerId: "mcp_server", DiscoveryStatus: "review_required",
			PendingSnapshotId: "mcpsnap_1", Revision: 2,
		},
		Snapshot: &aiAgentv1.ExternalMCPToolSnapshot{
			SnapshotId: "mcpsnap_1", ConnectionId: request.ConnectionId, ServerId: "mcp_server",
			SchemaHash: "hash", Tools: []*aiAgentv1.ExternalMCPToolSchema{{
				Name: "lookup", QualifiedName: "mcp_server.lookup", InputSchemaJson: `{"type":"object"}`,
			}},
		},
	}, nil
}

func TestCreateExternalMCPConnectionUsesAuthenticatedTenantAndDoesNotEchoSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &externalMCPGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/mcp-connections", bytes.NewBufferString(`{
		"name":"Research","transport":"streamable_http","endpoint":"https://mcp.example.com/mcp",
		"auth_type":"bearer","bearer_token":"top-secret"
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.CreateExternalMCPConnection(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.createRequest)
	require.Equal(t, uint64(42), client.createRequest.UserId)
	require.Equal(t, "top-secret", client.createRequest.BearerToken)
	require.NotContains(t, recorder.Body.String(), "top-secret")
	require.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
}

func TestCreateManagedExternalMCPConnectionForwardsReferenceWithoutBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &externalMCPGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/mcp-connections", bytes.NewBufferString(`{
		"scope":"project","project_id":"agentproj_0123456789abcdef0123456789abcdef",
		"name":"Managed Research","transport":"streamable_http","endpoint":"https://mcp.example.com/mcp",
		"auth_type":"bearer","credential_source":"managed","managed_credential_ref":"team.research"
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.CreateExternalMCPConnection(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.createRequest)
	require.Equal(t, "managed", client.createRequest.CredentialSource)
	require.Equal(t, "team.research", client.createRequest.ManagedCredentialRef)
	require.Empty(t, client.createRequest.BearerToken)
	require.Contains(t, recorder.Body.String(), `"managed_credential_ref":"team.research"`)
	require.NotContains(t, recorder.Body.String(), "secret_key")
	require.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
}

func TestDiscoverExternalMCPToolsReturnsReviewableSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &externalMCPGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "mcpconn_1"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/mcp-connections/mcpconn_1/discover",
		bytes.NewBufferString(`{"expected_revision":1}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(77))

	handler.DiscoverExternalMCPTools(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, uint64(77), client.discoverRequest.UserId)
	require.Equal(t, int64(1), client.discoverRequest.ExpectedRevision)
	var response struct {
		Connection struct {
			DiscoveryStatus   string `json:"discovery_status"`
			PendingSnapshotID string `json:"pending_snapshot_id"`
		} `json:"connection"`
		Snapshot struct {
			Tools []struct {
				QualifiedName string `json:"qualified_name"`
			} `json:"tools"`
		} `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "review_required", response.Connection.DiscoveryStatus)
	require.Equal(t, "mcpsnap_1", response.Connection.PendingSnapshotID)
	require.Equal(t, "mcp_server.lookup", response.Snapshot.Tools[0].QualifiedName)
}

func TestListExternalMCPToolsUsesAuthenticatedTenantAndExposesReviewedContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &externalMCPGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "mcpconn_1"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/mcp-connections/mcpconn_1/tools", nil)
	ctx.Set("user_id", uint64(88))

	handler.ListExternalMCPTools(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.listToolsRequest)
	require.Equal(t, uint64(88), client.listToolsRequest.UserId)
	require.Equal(t, "mcpconn_1", client.listToolsRequest.ConnectionId)
	require.NotContains(t, recorder.Body.String(), "bearer")
	require.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
	var response struct {
		Connection struct {
			HealthStatus        string `json:"health_status"`
			HealthErrorCode     string `json:"health_error_code"`
			HealthFailureCount  int64  `json:"health_failure_count"`
			LastHealthCheckedAt int64  `json:"last_health_checked_at"`
		} `json:"connection"`
		Tools []struct {
			Schema struct {
				DeclaredReadOnly         bool   `json:"declared_read_only"`
				DeclaredIdempotent       bool   `json:"declared_idempotent"`
				IdempotencyKeyArgument   string `json:"idempotency_key_argument"`
				SupportsWriteIdempotency bool   `json:"supports_write_idempotency"`
			} `json:"schema"`
			Policy struct {
				Enabled bool `json:"enabled"`
			} `json:"policy"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "degraded", response.Connection.HealthStatus)
	require.Equal(t, "timeout", response.Connection.HealthErrorCode)
	require.Equal(t, int64(1), response.Connection.HealthFailureCount)
	require.Equal(t, int64(1_721_000_000), response.Connection.LastHealthCheckedAt)
	require.True(t, response.Tools[0].Schema.DeclaredReadOnly)
	require.True(t, response.Tools[0].Schema.DeclaredIdempotent)
	require.Equal(t, "idempotency_key", response.Tools[0].Schema.IdempotencyKeyArgument)
	require.True(t, response.Tools[0].Schema.SupportsWriteIdempotency)
	require.True(t, response.Tools[0].Policy.Enabled)
}

func TestConfigureExternalMCPToolUsesAuthenticatedTenantAndCASRevision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &externalMCPGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{
		{Key: "id", Value: "mcpconn_1"},
		{Key: "tool_name", Value: "mcp_server.lookup"},
	}
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/v1/agent/mcp-connections/mcpconn_1/tools/mcp_server.lookup/policy",
		bytes.NewBufferString(`{"snapshot_id":"mcpsnap_1","category":"read","enabled":true,"expected_revision":3}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(99))

	handler.ConfigureExternalMCPTool(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.configureToolRequest)
	require.Equal(t, uint64(99), client.configureToolRequest.UserId)
	require.Equal(t, "mcpconn_1", client.configureToolRequest.ConnectionId)
	require.Equal(t, "mcp_server.lookup", client.configureToolRequest.ToolName)
	require.Equal(t, "mcpsnap_1", client.configureToolRequest.SnapshotId)
	require.Equal(t, "read", client.configureToolRequest.Category)
	require.True(t, client.configureToolRequest.Enabled)
	require.Equal(t, int64(3), client.configureToolRequest.ExpectedRevision)
	require.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
}
