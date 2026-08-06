package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
)

type workflowReplayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	request *aiAgentv1.GetWorkflowRunReplayRequest
}

func (f *workflowReplayClientFake) GetWorkflowRunReplay(
	_ context.Context,
	request *aiAgentv1.GetWorkflowRunReplayRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.GetWorkflowRunReplayResponse, error) {
	f.request = request
	return &aiAgentv1.GetWorkflowRunReplayResponse{
		Run: &aiAgentv1.WorkflowRun{RunId: request.RunId, UserId: request.UserId, Status: "compensated"},
		Events: []*aiAgentv1.WorkflowReplayStateEvent{
			{Sequence: 1, NodeId: "start", DeltaJson: `{"user_input":"hello"}`, EventHash: "event-hash", AppliedAt: 10},
		},
		Compensations: []*aiAgentv1.WorkflowReplayCompensation{
			{Sequence: 1, ToolName: "Release", InputHash: "input-hash", PlanHash: "plan-hash", Status: "succeeded"},
		},
		Integrity: &aiAgentv1.WorkflowReplayIntegrity{
			Verified: true, StateVersion: 1, EventCount: 1, LastSequence: 1,
		},
	}, nil
}

func TestGetWorkflowRunReplayReturnsStructuredEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowReplayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/workflow-runs/run-1/replay", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "run-1"}}
	ctx.Set("user_id", uint64(42))

	handler.GetWorkflowRunReplay(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.request)
	require.Equal(t, uint64(42), client.request.UserId)
	require.Equal(t, "run-1", client.request.RunId)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, true, body["integrity"].(map[string]interface{})["verified"])
	events := body["events"].([]interface{})
	delta := events[0].(map[string]interface{})["delta"].(map[string]interface{})
	require.Equal(t, "hello", delta["user_input"])
	compensation := body["compensations"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "input-hash", compensation["input_hash"])
	require.NotContains(t, compensation, "input_json")
}
