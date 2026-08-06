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

type agentTaskTemplateGatewayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	createRequest *aiAgentv1.CreateAgentTaskTemplateRequest
	runRequest    *aiAgentv1.RunAgentTaskTemplateRequest
}

func (f *agentTaskTemplateGatewayClientFake) CreateAgentTaskTemplate(
	_ context.Context,
	request *aiAgentv1.CreateAgentTaskTemplateRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.CreateAgentTaskTemplateResponse, error) {
	f.createRequest = request
	return &aiAgentv1.CreateAgentTaskTemplateResponse{
		Code: 200,
		TaskTemplate: &aiAgentv1.AgentTaskTemplate{
			ContractVersion: "agent.task_template.v1",
			TemplateId:      "aaaaaaaaaaaaaaaaaaaaaaaa",
			Name:            request.Name,
			Status:          "active",
			Revision:        1,
			SourceRunId:     request.SourceRunId,
		},
	}, nil
}

func (f *agentTaskTemplateGatewayClientFake) RunAgentTaskTemplate(
	_ context.Context,
	request *aiAgentv1.RunAgentTaskTemplateRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.RunAgentResponse, error) {
	f.runRequest = request
	return &aiAgentv1.RunAgentResponse{
		Code: 200, Response: "templated answer", DialogueKey: "dialogue-1",
		RunId: "run-2", RunStatus: "completed",
		ExecutionProfile:             "runtime.chat",
		CapabilityIds:                []string{"conversation.reply"},
		SelectedTaskTemplateId:       request.TemplateId,
		SelectedTaskTemplateRevision: request.ExpectedRevision,
	}, nil
}

func TestCreateAgentTaskTemplateUsesAuthenticatedTenantAndRunPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentTaskTemplateGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "source-run"}}
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/runs/source-run/task-templates",
		bytes.NewBufferString(`{
			"expected_source_run_revision":2,
			"name":"Summary",
			"description":"Reusable",
			"instruction_template":"Summarize: {{input}}",
			"idempotency_key":"request-1"
		}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.CreateAgentTaskTemplate(ctx)

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.NotNil(t, client.createRequest)
	require.Equal(t, uint64(42), client.createRequest.UserId)
	require.Equal(t, "source-run", client.createRequest.SourceRunId)
	require.Equal(t, int64(2), client.createRequest.ExpectedSourceRunRevision)
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))
}

func TestRunAgentTaskTemplateReturnsTemplateAudit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &agentTaskTemplateGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "aaaaaaaaaaaaaaaaaaaaaaaa"}}
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/task-templates/aaaaaaaaaaaaaaaaaaaaaaaa/run",
		bytes.NewBufferString(`{
			"expected_revision":1,
			"input":"Go Agent Runtime",
			"dialogue_id":"0",
			"model_kind_id":"1"
		}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.RunAgentTaskTemplate(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.runRequest)
	require.Equal(t, uint64(42), client.runRequest.UserId)
	require.Equal(t, "Go Agent Runtime", client.runRequest.MainContent.Content)
	var response struct {
		TemplateID       string `json:"selected_task_template_id"`
		TemplateRevision int64  `json:"selected_task_template_revision"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaa", response.TemplateID)
	require.Equal(t, int64(1), response.TemplateRevision)
}
