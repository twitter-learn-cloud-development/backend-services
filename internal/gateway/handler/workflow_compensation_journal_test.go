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

type workflowCompensationClientFake struct {
	aiAgentv1.AiAgentServiceClient
	journalRequest *aiAgentv1.GetWorkflowCompensationJournalRequest
	retryRequest   *aiAgentv1.RetryWorkflowCompensationRequest
}

func (f *workflowCompensationClientFake) GetWorkflowCompensationJournal(
	_ context.Context,
	request *aiAgentv1.GetWorkflowCompensationJournalRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.GetWorkflowCompensationJournalResponse, error) {
	f.journalRequest = request
	return &aiAgentv1.GetWorkflowCompensationJournalResponse{
		Run: &aiAgentv1.WorkflowCompensationRunSummary{
			RunId: request.RunId, WorkflowId: "workflow-1", Status: "compensation_failed",
		},
		Entries: []*aiAgentv1.WorkflowCompensationJournalEntry{
			{
				Sequence: 1, SourceNodeId: "reserve", StepId: "reserve$compensate",
				ToolName: "Release", InputHash: "input-hash", PlanHash: "plan-hash",
				Status: "failed", ErrorMessage: "release failed", IsNext: true,
			},
		},
		NextSequence: 1, RetryAvailable: true,
	}, nil
}

func (f *workflowCompensationClientFake) RetryWorkflowCompensation(
	_ context.Context,
	request *aiAgentv1.RetryWorkflowCompensationRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.RetryWorkflowCompensationResponse, error) {
	f.retryRequest = request
	return &aiAgentv1.RetryWorkflowCompensationResponse{
		Run:      &aiAgentv1.WorkflowRun{RunId: request.RunId, UserId: request.UserId, Status: "compensated"},
		Response: "compensation completed",
	}, nil
}

func TestGetWorkflowCompensationJournalReturnsRedactedEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowCompensationClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/workflow-runs/run-1/compensations", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "run-1"}}
	ctx.Set("user_id", uint64(91))

	handler.GetWorkflowCompensationJournal(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.journalRequest)
	require.Equal(t, uint64(91), client.journalRequest.UserId)
	require.Equal(t, "run-1", client.journalRequest.RunId)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, true, body["retry_available"])
	run := body["run"].(map[string]interface{})
	require.NotContains(t, run, "input")
	require.NotContains(t, run, "output")
	entry := body["entries"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "input-hash", entry["input_hash"])
	require.NotContains(t, entry, "input_json")
	require.NotContains(t, entry, "output_json")
	require.NotContains(t, entry, "idempotency_key")
}

func TestRetryWorkflowCompensationUsesDedicatedControlEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowCompensationClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/workflow-runs/run-2/compensations/retry", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "run-2"}}
	ctx.Set("user_id", uint64(92))

	handler.RetryWorkflowCompensation(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.retryRequest)
	require.Equal(t, uint64(92), client.retryRequest.UserId)
	require.Equal(t, "run-2", client.retryRequest.RunId)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "compensated", body["run"].(map[string]interface{})["status"])
}
