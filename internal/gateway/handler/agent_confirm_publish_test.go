package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
)

type confirmedDraftGatewayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	request *aiAgentv1.ConfirmPublishTwitterRequest
	err     error
}

func (f *confirmedDraftGatewayClientFake) ConfirmPublishTwitter(
	_ context.Context,
	request *aiAgentv1.ConfirmPublishTwitterRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.ConfirmPublishTwitterResponse, error) {
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	return &aiAgentv1.ConfirmPublishTwitterResponse{Response: "发布成功", TweetId: 99}, nil
}

func TestConfirmPublishTwitterForwardsTrustedSourceRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &confirmedDraftGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	body, err := json.Marshal(map[string]string{"content": "edited draft", "source_run_id": "run-assist-1"})
	require.NoError(t, err)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/confirm", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.ConfirmPublishTwitter(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.request)
	require.Equal(t, uint64(42), client.request.UserId)
	require.Equal(t, "edited draft", client.request.Content)
	require.Equal(t, "run-assist-1", client.request.SourceRunId)
	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "99", response["tweet_id"])
}

func TestConfirmPublishTwitterMapsInvalidSourceRunToUnprocessableEntity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &confirmedDraftGatewayClientFake{err: status.Error(codes.FailedPrecondition, "invalid source run")}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/confirm", bytes.NewBufferString(`{"content":"draft","source_run_id":"other-user-run"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.ConfirmPublishTwitter(ctx)

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
}
