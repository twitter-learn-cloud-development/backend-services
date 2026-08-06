package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
	"twitter-clone/internal/module/agent/marketplace"
)

type extensionMarketplaceAdminGatewayFake struct {
	aiAgentv1.AiAgentServiceClient
	accessRequest  *aiAgentv1.GetAgentMarketplaceManagementAccessRequest
	publishRequest *aiAgentv1.PublishAgentMarketplaceReleaseRequest
	token          string
}

func (f *extensionMarketplaceAdminGatewayFake) GetAgentMarketplaceManagementAccess(
	ctx context.Context,
	req *aiAgentv1.GetAgentMarketplaceManagementAccessRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.GetAgentMarketplaceManagementAccessResponse, error) {
	f.accessRequest = req
	values, _ := metadata.FromOutgoingContext(ctx)
	tokens := values.Get(marketplace.AdminTokenMetadataKey)
	if len(tokens) == 1 {
		f.token = tokens[0]
	}
	return &aiAgentv1.GetAgentMarketplaceManagementAccessResponse{Code: 200, Msg: "success", Access: &aiAgentv1.AgentMarketplaceManagementAccess{
		ContractVersion: marketplace.ControlContractVersion, Enabled: true,
		OwnedPublisherIds: []string{"publisher"},
	}}, nil
}

func (f *extensionMarketplaceAdminGatewayFake) PublishAgentMarketplaceRelease(
	_ context.Context,
	req *aiAgentv1.PublishAgentMarketplaceReleaseRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.PublishAgentMarketplaceReleaseResponse, error) {
	f.publishRequest = req
	return &aiAgentv1.PublishAgentMarketplaceReleaseResponse{Code: 200, Msg: "success", Release: &aiAgentv1.AgentMarketplaceManagedRelease{
		ContractVersion: marketplace.ControlContractVersion, ReleaseId: "release_1",
		Manifest: req.Manifest, SignatureKeyId: req.SignatureKeyId,
		Status: marketplace.ReleasePublished, Revision: 1, PublishedBy: req.ActorUserId,
	}}, nil
}

func TestExtensionMarketplaceManagementAccessUsesJWTActorAndInternalToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &extensionMarketplaceAdminGatewayFake{}
	handler := NewAgentHandler(client, WithExtensionMarketplaceAdministration(strings.Repeat("t", 32)))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/marketplace/manage/access", nil)
	ctx.Set("user_id", uint64(42))

	handler.GetAgentMarketplaceManagementAccess(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, uint64(42), client.accessRequest.ActorUserId)
	require.Equal(t, strings.Repeat("t", 32), client.token)
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))
}

func TestPublishExtensionMarketplaceReleaseNeverEchoesSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &extensionMarketplaceAdminGatewayFake{}
	handler := NewAgentHandler(client, WithExtensionMarketplaceAdministration(strings.Repeat("t", 32)))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	body := `{
		"manifest":{"contract_version":"agent.extension_manifest.v1","package_id":"publisher.package","kind":"skill","version":"1.0.0","publisher_id":"publisher","display_name":"Package","artifact_digest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","capability_ids":["web.search"]},
		"signature_key_id":"key-1","signature_base64":"sensitive-signature","expected_publisher_revision":1
	}`
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/marketplace/manage/releases", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.PublishAgentMarketplaceRelease(ctx)

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.NotNil(t, client.publishRequest)
	require.Equal(t, "sensitive-signature", client.publishRequest.SignatureBase64)
	require.NotContains(t, recorder.Body.String(), "sensitive-signature")
	require.NotContains(t, recorder.Body.String(), "signature_base64")
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
}
