package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	aiAgentv1 "twitter-clone/api/aiAgent/v1"
)

type workflowRunsClientFake struct {
	aiAgentv1.AiAgentServiceClient
	request            *aiAgentv1.ListWorkflowRunsRequest
	cancelRequest      *aiAgentv1.CancelWorkflowRunRequest
	traceRequest       *aiAgentv1.GetWorkflowRunTraceRequest
	boardRequest       *aiAgentv1.SearchWorkflowBlackboardRequest
	eventRequest       *aiAgentv1.WatchWorkflowRunEventsRequest
	resumeGrantRequest *aiAgentv1.IssueWorkflowResumeGrantRequest
	runRequest         *aiAgentv1.RunWorkflowRequest
}

type workflowRunEventClientFake struct {
	grpc.ClientStream
	events []*aiAgentv1.WorkflowRunEvent
	index  int
}

func (f *workflowRunEventClientFake) Recv() (*aiAgentv1.WorkflowRunEvent, error) {
	if f.index >= len(f.events) {
		return nil, io.EOF
	}
	event := f.events[f.index]
	f.index++
	return event, nil
}

func (f *workflowRunsClientFake) WatchWorkflowRunEvents(
	_ context.Context,
	request *aiAgentv1.WatchWorkflowRunEventsRequest,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[aiAgentv1.WorkflowRunEvent], error) {
	f.eventRequest = request
	return &workflowRunEventClientFake{events: []*aiAgentv1.WorkflowRunEvent{
		{Cursor: "10-0", Kind: "run", Run: &aiAgentv1.AgentRunTrace{RunId: request.RunId, Status: "running"}},
		{Cursor: "11-0", Kind: "control", Heartbeat: true, Reason: "heartbeat"},
	}}, nil
}

func (f *workflowRunsClientFake) GetWorkflowRunTrace(
	_ context.Context,
	request *aiAgentv1.GetWorkflowRunTraceRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.GetWorkflowRunTraceResponse, error) {
	f.traceRequest = request
	return &aiAgentv1.GetWorkflowRunTraceResponse{
		Run: &aiAgentv1.AgentRunTrace{RunId: request.RunId, UserId: request.UserId, Status: "success"},
		LlmCalls: []*aiAgentv1.AgentLLMCallTrace{{
			RunId: request.RunId, UserId: request.UserId, StepId: "llm", Model: "model",
			PromptHash: "prompt-digest", PromptLength: 14, CompletionHash: "completion-digest",
			PromptTemplateId: "workflow.wf.node.llm", PromptTemplateVersion: "revision-3",
			PromptSample: "redacted preview", PromptSampleStatus: "captured",
			CompletionSampleStatus: "not_selected", ContentSamplePolicy: "redacted_preview_v1",
		}},
	}, nil
}

func (f *workflowRunsClientFake) SearchWorkflowBlackboard(
	_ context.Context,
	request *aiAgentv1.SearchWorkflowBlackboardRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.SearchWorkflowBlackboardResponse, error) {
	f.boardRequest = request
	return &aiAgentv1.SearchWorkflowBlackboardResponse{
		RunId: request.RunId, StateVersion: request.StateVersion,
		BaseSnapshotVersion: 5, BaseSnapshotHash: "base-hash", StateHash: "state-hash", Verified: true,
		Entries: []*aiAgentv1.WorkflowBlackboardEntry{{
			Path: "writer.draft", ValueJson: `"redacted draft"`, ValueType: "string",
			ValueHash: "value-hash", ValueLength: 16,
		}},
		MatchedTotal: 1,
	}, nil
}

func (f *workflowRunsClientFake) CancelWorkflowRun(
	_ context.Context,
	request *aiAgentv1.CancelWorkflowRunRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.CancelWorkflowRunResponse, error) {
	f.cancelRequest = request
	return &aiAgentv1.CancelWorkflowRunResponse{
		Run: &aiAgentv1.WorkflowRunSummary{
			RunId: request.RunId, Status: "canceling", CancelReason: request.Reason,
		},
	}, nil
}

func (f *workflowRunsClientFake) ListWorkflowRuns(
	_ context.Context,
	request *aiAgentv1.ListWorkflowRunsRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.ListWorkflowRunsResponse, error) {
	f.request = request
	return &aiAgentv1.ListWorkflowRunsResponse{
		Runs: []*aiAgentv1.WorkflowRunSummary{
			{
				RunId: "run-1", WorkflowId: request.WorkflowId, Status: "failed",
				ErrorMessage: "node failed", WorkflowRevisionNumber: 3, StateVersion: 5,
			},
		},
		Total: 1,
	}, nil
}

func (f *workflowRunsClientFake) IssueWorkflowResumeGrant(
	_ context.Context,
	request *aiAgentv1.IssueWorkflowResumeGrantRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.IssueWorkflowResumeGrantResponse, error) {
	f.resumeGrantRequest = request
	return &aiAgentv1.IssueWorkflowResumeGrantResponse{
		Run: &aiAgentv1.WorkflowRun{
			RunId: "run-approval", Status: "suspended", Revision: request.ExpectedRunRevision + 1,
			ApprovalRequestId: request.ApprovalId, ResumeGrantIssuedAt: 100, ResumeGrantExpiresAt: 400,
		},
		ResumeToken: "one-time-resume-grant",
		ExpiresAt:   400,
	}, nil
}

func (f *workflowRunsClientFake) RunWorkflow(
	_ context.Context,
	request *aiAgentv1.RunWorkflowRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.RunWorkflowResponse, error) {
	f.runRequest = request
	return &aiAgentv1.RunWorkflowResponse{
		Run: &aiAgentv1.WorkflowRun{
			RunId: "run-new", WorkflowId: request.WorkflowId, Status: "suspended", Revision: 1,
			ApprovalRequestId: "approval-new",
		},
		ResumeToken: "initial-one-time-token",
	}, nil
}

func TestListWorkflowRunsForwardsTenantAndFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowRunsClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/agent/workflow-runs?workflow_id=workflow-1&status=failed&page=2&page_size=25",
		nil,
	)
	ctx.Set("user_id", uint64(103))

	handler.ListWorkflowRuns(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.request)
	require.Equal(t, uint64(103), client.request.UserId)
	require.Equal(t, "workflow-1", client.request.WorkflowId)
	require.Equal(t, "failed", client.request.Status)
	require.Equal(t, uint32(2), client.request.Page)
	require.Equal(t, uint32(25), client.request.PageSize)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, float64(1), body["total"])
	run := body["runs"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "failed", run["status"])
	require.Equal(t, float64(3), run["workflow_revision_number"])
	require.NotContains(t, run, "input_json")
	require.NotContains(t, run, "output_json")
}

func TestRunWorkflowDisablesCachingForOneTimeToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowRunsClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/workflows/workflow-2/run",
		strings.NewReader(`{"workflow_revision_id":"revision-3","input":{"user_input":"hello"}}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "workflow-2"}}
	ctx.Set("user_id", uint64(109))

	handler.RunWorkflow(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.runRequest)
	require.Equal(t, uint64(109), client.runRequest.UserId)
	require.Equal(t, "workflow-2", client.runRequest.WorkflowId)
	require.Equal(t, "revision-3", client.runRequest.WorkflowRevisionId)
	require.JSONEq(t, `{"user_input":"hello"}`, client.runRequest.InputJson)
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", recorder.Header().Get("Pragma"))
	require.Contains(t, recorder.Body.String(), "initial-one-time-token")
}

func TestCancelWorkflowRunForwardsTenantAndReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowRunsClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/workflow-runs/run-2/cancel",
		strings.NewReader(`{"reason":"operator stop"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "run-2"}}
	ctx.Set("user_id", uint64(104))

	handler.CancelWorkflowRun(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.cancelRequest)
	require.Equal(t, uint64(104), client.cancelRequest.UserId)
	require.Equal(t, "run-2", client.cancelRequest.RunId)
	require.Equal(t, "operator stop", client.cancelRequest.Reason)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "canceling", body["run"].(map[string]interface{})["status"])
}

func TestIssueWorkflowResumeGrantForwardsTenantRevisionAndDisablesCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowRunsClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/tool-approvals/approval-1/resume-grant",
		strings.NewReader(`{"expected_run_revision":7}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "approval-1"}}
	ctx.Set("user_id", uint64(108))

	handler.IssueWorkflowResumeGrant(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.resumeGrantRequest)
	require.Equal(t, uint64(108), client.resumeGrantRequest.UserId)
	require.Equal(t, "approval-1", client.resumeGrantRequest.ApprovalId)
	require.Equal(t, int64(7), client.resumeGrantRequest.ExpectedRunRevision)
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", recorder.Header().Get("Pragma"))
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "one-time-resume-grant", body["resume_token"])
	require.Equal(t, float64(400), body["expires_at"])
	run := body["run"].(map[string]interface{})
	require.Equal(t, float64(400), run["resume_grant_expires_at"])
}

func TestGetWorkflowRunTraceForwardsTenantAndReturnsRedactedRecords(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowRunsClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/workflow-runs/run-3/traces", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "run-3"}}
	ctx.Set("user_id", uint64(105))

	handler.GetWorkflowRunTrace(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, uint64(105), client.traceRequest.UserId)
	require.Equal(t, "run-3", client.traceRequest.RunId)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	llmCall := body["llm_calls"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "prompt-digest", llmCall["prompt_hash"])
	require.Equal(t, "workflow.wf.node.llm", llmCall["prompt_template_id"])
	require.Equal(t, "revision-3", llmCall["prompt_template_version"])
	require.Equal(t, "redacted preview", llmCall["prompt_sample"])
	require.NotContains(t, llmCall, "prompt")
	require.NotContains(t, llmCall, "completion")
}

func TestSearchWorkflowBlackboardForwardsStableQueryAndReturnsMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowRunsClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/agent/workflow-runs/run-5/blackboard?state_version=7&query=draft&path_prefix=writer.&after_cursor=cursor-1&page_size=10",
		nil,
	)
	ctx.Params = gin.Params{{Key: "id", Value: "run-5"}}
	ctx.Set("user_id", uint64(107))

	handler.SearchWorkflowBlackboard(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.boardRequest)
	require.Equal(t, uint64(107), client.boardRequest.UserId)
	require.Equal(t, "run-5", client.boardRequest.RunId)
	require.Equal(t, int64(7), client.boardRequest.StateVersion)
	require.Equal(t, "draft", client.boardRequest.Query)
	require.Equal(t, "writer.", client.boardRequest.PathPrefix)
	require.Equal(t, "cursor-1", client.boardRequest.AfterCursor)
	require.Equal(t, uint32(10), client.boardRequest.PageSize)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, true, body["verified"])
	require.Equal(t, "state-hash", body["state_hash"])
	entry := body["entries"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "writer.draft", entry["path"])
	require.NotContains(t, entry, "api_key")
}

func TestWatchWorkflowRunEventsForwardsTenantAndWritesResumableSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &workflowRunsClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/workflow-runs/run-4/events?after_cursor=9-0", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "run-4"}}
	ctx.Set("user_id", uint64(106))

	handler.WatchWorkflowRunEvents(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "text/event-stream; charset=utf-8", recorder.Header().Get("Content-Type"))
	require.Equal(t, uint64(106), client.eventRequest.UserId)
	require.Equal(t, "run-4", client.eventRequest.RunId)
	require.Equal(t, "9-0", client.eventRequest.AfterCursor)
	require.Contains(t, recorder.Body.String(), "id: 10-0\nevent: trace\n")
	require.Contains(t, recorder.Body.String(), `"status":"running"`)
	require.Contains(t, recorder.Body.String(), "id: 11-0\nevent: control\n")
	require.NotContains(t, recorder.Body.String(), "Authorization")
}

func TestWriteWorkflowRunSSEDropsInvalidCursor(t *testing.T) {
	var output strings.Builder
	require.NoError(t, writeWorkflowRunSSE(&output, "1-0\ndata: injected", "trace", gin.H{"ok": true}))
	require.NotContains(t, output.String(), "id:")
	require.Equal(t, "event: trace\ndata: {\"ok\":true}\n\n", output.String())
}
