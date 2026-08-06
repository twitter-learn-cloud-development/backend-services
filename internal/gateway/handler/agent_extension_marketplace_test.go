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

type agentMarketplaceGatewayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	request *aiAgentv1.ListAgentMarketplaceExtensionsRequest
}

func (client *agentMarketplaceGatewayClientFake) ListAgentMarketplaceExtensions(
	_ context.Context,
	request *aiAgentv1.ListAgentMarketplaceExtensionsRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.ListAgentMarketplaceExtensionsResponse, error) {
	client.request = request
	return &aiAgentv1.ListAgentMarketplaceExtensionsResponse{
		Code: 200, Msg: "success", ContractVersion: "agent.extension_marketplace.v1",
		Releases: []*aiAgentv1.AgentMarketplaceExtension{{
			ContractVersion: "agent.extension_marketplace.v1",
			ReleaseId:       "release_deadbeef", PackageId: "publisher.research",
			Kind: "skill", Version: "1.2.3", DisplayName: "Research assistant",
			Description: "Builds source-grounded research drafts.",
			Publisher: &aiAgentv1.AgentMarketplacePublisher{
				PublisherId: "publisher", DisplayName: "Verified Publisher", Verification: "verified",
			},
			ArtifactDigestSha256: strings.Repeat("a", 64), SignatureKeyId: "key-2026",
			CapabilityIds: []string{"web.search"}, RequestedPermissions: []string{"network"},
			PublishedAtUnixMs: 1785722400000, SignatureVerified: true,
		}},
		NextCursor: "cursor-2", HasMore: true,
	}, nil
}

func TestListAgentMarketplaceExtensionsUsesAuthenticatedUserAndRedactsSigningMaterial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentMarketplaceGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/agent/marketplace/extensions?kind=skill&publisher_id=publisher&search=research&after_cursor=cursor-1&page_size=12&user_id=999",
		nil,
	)
	ctx.Set("user_id", uint64(42))

	handler.ListAgentMarketplaceExtensions(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.request)
	require.Equal(t, uint64(42), client.request.UserId)
	require.Equal(t, "skill", client.request.Kind)
	require.Equal(t, "publisher", client.request.PublisherId)
	require.Equal(t, "research", client.request.Search)
	require.Equal(t, "cursor-1", client.request.AfterCursor)
	require.Equal(t, int32(12), client.request.PageSize)
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))

	var response struct {
		ContractVersion string `json:"contract_version"`
		Releases        []struct {
			ReleaseID         string `json:"release_id"`
			SignatureVerified bool   `json:"signature_verified"`
			Publisher         struct {
				PublisherID string `json:"publisher_id"`
			} `json:"publisher"`
		} `json:"releases"`
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "agent.extension_marketplace.v1", response.ContractVersion)
	require.Len(t, response.Releases, 1)
	require.Equal(t, "release_deadbeef", response.Releases[0].ReleaseID)
	require.True(t, response.Releases[0].SignatureVerified)
	require.Equal(t, "publisher", response.Releases[0].Publisher.PublisherID)
	require.Equal(t, "cursor-2", response.NextCursor)
	require.True(t, response.HasMore)
	for _, forbidden := range []string{"public_key", "private_key", "signature_base64", "credential", "endpoint", "artifact_url"} {
		require.NotContains(t, strings.ToLower(recorder.Body.String()), forbidden)
	}
}

func TestListAgentMarketplaceExtensionsRejectsOversizedPageBeforeGRPC(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentMarketplaceGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/marketplace/extensions?page_size=51", nil)
	ctx.Set("user_id", uint64(42))

	handler.ListAgentMarketplaceExtensions(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Nil(t, client.request)
}
