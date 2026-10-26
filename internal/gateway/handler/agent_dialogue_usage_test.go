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

type agentDialogueUsageClientFake struct {
	aiAgentv1.AiAgentServiceClient
	request *aiAgentv1.GetDialogueDetailRequest
}

func (f *agentDialogueUsageClientFake) GetDialogueDetail(
	_ context.Context,
	request *aiAgentv1.GetDialogueDetailRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.GetDialogueDetailResponse, error) {
	f.request = request
	return &aiAgentv1.GetDialogueDetailResponse{
		Code: 200,
		Messages: []*aiAgentv1.RepositoryContentList{{
			Role: "assistant", Content: "你好", RunId: "chat-run",
			HasRuntimeUsage: true, RuntimeTotalTokens: 42, RuntimeTokensEstimated: true,
		}},
	}, nil
}

func TestGetDialogueDetailExposesRecordedUsageWithoutRawMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentDialogueUsageClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/dialogues/dialogue-1/messages", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "dialogue-1"}}
	ctx.Set("user_id", uint64(51))

	handler.GetDialogueDetail(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, uint64(51), client.request.UserId)
	require.Equal(t, "dialogue-1", client.request.DialogueKey)
	var response struct {
		Messages []struct {
			HasRuntimeUsage        bool  `json:"has_runtime_usage"`
			RuntimeTotalTokens     int64 `json:"runtime_total_tokens"`
			RuntimeTokensEstimated bool  `json:"runtime_tokens_estimated"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Messages[0].HasRuntimeUsage)
	require.Equal(t, int64(42), response.Messages[0].RuntimeTotalTokens)
	require.True(t, response.Messages[0].RuntimeTokensEstimated)
	require.NotContains(t, recorder.Body.String(), "metadata")
}
