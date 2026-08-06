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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type agentExecutionRunGatewayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	getRequest        *aiAgentv1.GetAgentRunRequest
	accountingRequest *aiAgentv1.GetAgentRunAccountingRequest
	resumeRequest     *aiAgentv1.ResumeAgentRunRequest
	grantRequest      *aiAgentv1.IssueAgentResumeGrantRequest
	resumeErr         error
}

func (f *agentExecutionRunGatewayClientFake) GetAgentRunAccounting(
	_ context.Context,
	request *aiAgentv1.GetAgentRunAccountingRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.GetAgentRunAccountingResponse, error) {
	f.accountingRequest = request
	return &aiAgentv1.GetAgentRunAccountingResponse{Accounting: &aiAgentv1.AgentRunAccounting{
		RunId: "run-1", RunStatus: "completed", Scope: "direct_children.v1",
		State: "complete", Complete: true, ChildRunCount: 1, IncludedChildRunCount: 1,
		TotalUsage: &aiAgentv1.ExecutionTokenUsage{
			TotalTokens: 42, EstimatedCostMicros: 88, PricingVersion: "pricing-v1",
		},
		Children: []*aiAgentv1.WorkflowRunAccounting{{
			RunId: "child-1", WorkflowId: "workflow-1", ParentActionId: "action-1",
			Status: "success", State: "complete",
		}},
	}}, nil
}

func (f *agentExecutionRunGatewayClientFake) GetAgentRun(
	_ context.Context,
	request *aiAgentv1.GetAgentRunRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.GetAgentRunResponse, error) {
	f.getRequest = request
	return &aiAgentv1.GetAgentRunResponse{Run: &aiAgentv1.AgentExecutionRun{
		RunId: "run-1", DialogueKey: "dialogue-1", Status: "approval_required",
		Revision: 2, ResumeSupported: true, PendingActionType: "tool_call",
		PendingActionId: "action-1", ApprovalId: "approval-1", ApprovalExpiresAt: 1234,
	}}, nil
}

func (f *agentExecutionRunGatewayClientFake) ResumeAgentRun(
	_ context.Context,
	request *aiAgentv1.ResumeAgentRunRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.RunAgentResponse, error) {
	f.resumeRequest = request
	if f.resumeErr != nil {
		return nil, f.resumeErr
	}
	return &aiAgentv1.RunAgentResponse{
		RunId: "run-1", DialogueKey: "dialogue-1", Response: "continued",
		RunStatus: "completed", ApprovalState: &aiAgentv1.AgentApprovalState{
			Status: "not_required", RunId: "run-1", Revision: 4,
		},
	}, nil
}

func (f *agentExecutionRunGatewayClientFake) IssueAgentResumeGrant(
	_ context.Context,
	request *aiAgentv1.IssueAgentResumeGrantRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.IssueAgentResumeGrantResponse, error) {
	f.grantRequest = request
	return &aiAgentv1.IssueAgentResumeGrantResponse{
		Run: &aiAgentv1.AgentExecutionRun{
			RunId: "run-1", Status: "approval_required", Revision: 3,
			ApprovalId: "approval-1", PendingActionId: "action-1", ResumeSupported: true,
		},
		ResumeToken: "one-time-token",
		ExpiresAt:   5678,
	}, nil
}

func TestGetAgentRunReturnsOnlySanitizedLifecycleProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentExecutionRunGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/runs/run-1", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "run-1"}}
	ctx.Set("user_id", uint64(42))

	handler.GetAgentRun(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.getRequest)
	require.Equal(t, uint64(42), client.getRequest.UserId)
	require.Equal(t, "run-1", client.getRequest.RunId)
	require.NotContains(t, recorder.Body.String(), "ciphertext")
	require.NotContains(t, recorder.Body.String(), "attempt_id")
	var response struct {
		Status            string `json:"status"`
		Revision          int64  `json:"revision"`
		ResumeSupported   bool   `json:"resume_supported"`
		PendingActionID   string `json:"pending_action_id"`
		ApprovalID        string `json:"approval_id"`
		ApprovalExpiresAt int64  `json:"approval_expires_at"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "approval_required", response.Status)
	require.Equal(t, int64(2), response.Revision)
	require.True(t, response.ResumeSupported)
	require.Equal(t, "action-1", response.PendingActionID)
	require.Equal(t, "approval-1", response.ApprovalID)
	require.Equal(t, int64(1234), response.ApprovalExpiresAt)
}

func TestGetAgentRunAccountingReturnsBoundedSanitizedProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentExecutionRunGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/agent/runs/run-1/accounting?child_limit=25", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "run-1"}}
	ctx.Set("user_id", uint64(42))

	handler.GetAgentRunAccounting(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.accountingRequest)
	require.Equal(t, uint64(42), client.accountingRequest.UserId)
	require.Equal(t, int32(25), client.accountingRequest.ChildLimit)
	require.NotContains(t, recorder.Body.String(), "output_json")
	require.NotContains(t, recorder.Body.String(), "checkpoint")
	var response struct {
		State      string `json:"state"`
		Complete   bool   `json:"complete"`
		TotalUsage struct {
			TotalTokens int32 `json:"total_tokens"`
		} `json:"total_usage"`
		Children []struct {
			RunID string `json:"run_id"`
		} `json:"children"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "complete", response.State)
	require.True(t, response.Complete)
	require.Equal(t, int32(42), response.TotalUsage.TotalTokens)
	require.Equal(t, "child-1", response.Children[0].RunID)
}

func TestResumeAgentRunForwardsApprovalGrantWithoutEchoingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentExecutionRunGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/runs/run-1/resume",
		bytes.NewBufferString(`{"expected_revision":3,"approval_id":"approval-1","resume_token":"one-time-token"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "run-1"}}
	ctx.Set("user_id", uint64(42))

	handler.ResumeAgentRun(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))
	require.NotNil(t, client.resumeRequest)
	require.Equal(t, "approval-1", client.resumeRequest.ApprovalId)
	require.Equal(t, "one-time-token", client.resumeRequest.ResumeToken)
	require.Empty(t, client.resumeRequest.HumanResponse)
	require.NotContains(t, recorder.Body.String(), "one-time-token")
}

func TestIssueAgentResumeGrantIsNoStoreAndBindsApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentExecutionRunGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/tool-approvals/approval-1/agent-resume-grant",
		bytes.NewBufferString(`{"expected_run_revision":2}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "approval-1"}}
	ctx.Set("user_id", uint64(42))

	handler.IssueAgentResumeGrant(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", recorder.Header().Get("Pragma"))
	require.NotNil(t, client.grantRequest)
	require.Equal(t, uint64(42), client.grantRequest.UserId)
	require.Equal(t, "approval-1", client.grantRequest.ApprovalId)
	require.Equal(t, int64(2), client.grantRequest.ExpectedRunRevision)
	require.Contains(t, recorder.Body.String(), "one-time-token")
}

func TestResumeAgentRunForwardsRevisionAndMapsClaimConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentExecutionRunGatewayClientFake{resumeErr: status.Error(codes.Aborted, "revision changed")}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/runs/run-1/resume",
		bytes.NewBufferString(`{"expected_revision":2,"human_response":"repository"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: "run-1"}}
	ctx.Set("user_id", uint64(42))

	handler.ResumeAgentRun(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.NotNil(t, client.resumeRequest)
	require.Equal(t, uint64(42), client.resumeRequest.UserId)
	require.Equal(t, int64(2), client.resumeRequest.ExpectedRevision)
	require.Equal(t, "repository", client.resumeRequest.HumanResponse)
}
