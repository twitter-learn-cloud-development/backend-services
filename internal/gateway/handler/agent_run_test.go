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

type runAgentGatewayClientFake struct {
	aiAgentv1.AiAgentServiceClient
	request *aiAgentv1.RunAgentRequest
}

func (f *runAgentGatewayClientFake) RunAgent(
	_ context.Context,
	request *aiAgentv1.RunAgentRequest,
	_ ...grpc.CallOption,
) (*aiAgentv1.RunAgentResponse, error) {
	f.request = request
	return &aiAgentv1.RunAgentResponse{
		Response:         "answer",
		DialogueKey:      "dialogue-1",
		RunId:            "run-1",
		RunStatus:        "completed",
		ExecutionProfile: "runtime.research_draft",
		CapabilityIds:    []string{"platform.search", "content.draft"},
		ToolActivities: []*aiAgentv1.AgentToolActivity{{
			StepIndex: 2, ToolName: "hybrid_search_tweets",
			Status: "succeeded", ResultCount: 1,
		}},
		Citations: []*aiAgentv1.AgentCitation{{
			CitationId: "platform_tweet:9007199254740993",
			SourceType: "platform_tweet",
			SourceId:   "9007199254740993",
			Url:        "/tweets/9007199254740993",
			Snippet:    "source",
		}},
		Artifacts: []*aiAgentv1.AgentArtifact{{
			ArtifactId: "content.draft:run-1", Type: "content.draft",
			Status: "ready", ContentType: "text/markdown", Content: "answer",
			SourceRunId: "run-1", RequiresConfirmation: true,
		}},
		ApprovalState: &aiAgentv1.AgentApprovalState{
			Status: "not_required", RunId: "run-1",
		},
		SelectedSkillId:      "workflow_aaaaaaaaaaaaaaaaaaaaaaaa",
		SelectedSkillVersion: "v1-deadbeef",
		ExecutionStrategyPlan: &aiAgentv1.AgentExecutionStrategyPlan{
			Version: "agent.execution_strategy.v1", CandidateStrategy: "multi_agent",
			SelectedStrategy: "single_agent", Decision: "fallback",
			ReasonCode: "multi_executor_unavailable", ComplexityScore: 8,
			PlanDigest: "digest-1", Roles: []*aiAgentv1.AgentExecutionStrategyRole{{
				RoleId: "researcher", AllowedTools: []string{"hybrid_search_tweets"},
			}},
		},
	}, nil
}

func TestRunAgentReturnsSanitizedStructuredEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := &runAgentGatewayClientFake{}
	handler := NewAgentHandler(client)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/run",
		bytes.NewBufferString(`{
			"content":"search and draft",
			"dialogue_id":"0",
			"model_kind_id":"1",
			"web_search_provider_config_id":"config-1",
			"skill_id":"workflow_aaaaaaaaaaaaaaaaaaaaaaaa",
			"skill_version":"v1-deadbeef"
		}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("user_id", uint64(42))

	handler.RunAgent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotNil(t, client.request)
	require.Equal(t, uint64(42), client.request.UserId)
	require.Equal(t, "config-1", client.request.WebSearchProviderConfigId)
	require.Equal(t, "workflow_aaaaaaaaaaaaaaaaaaaaaaaa", client.request.SkillId)
	require.Equal(t, "v1-deadbeef", client.request.SkillVersion)

	var response struct {
		ToolActivities []struct {
			ToolName    string `json:"tool_name"`
			Status      string `json:"status"`
			ResultCount int32  `json:"result_count"`
		} `json:"tool_activities"`
		Citations []struct {
			SourceID string `json:"source_id"`
			URL      string `json:"url"`
		} `json:"citations"`
		RunStatus            string `json:"run_status"`
		SelectedSkillID      string `json:"selected_skill_id"`
		SelectedSkillVersion string `json:"selected_skill_version"`
		Artifacts            []struct {
			SourceRunID          string `json:"source_run_id"`
			RequiresConfirmation bool   `json:"requires_confirmation"`
		} `json:"artifacts"`
		ApprovalState struct {
			Status      string `json:"status"`
			RunID       string `json:"run_id"`
			ResumeToken string `json:"resume_token"`
		} `json:"approval_state"`
		ExecutionStrategyPlan struct {
			CandidateStrategy string `json:"candidate_strategy"`
			SelectedStrategy  string `json:"selected_strategy"`
			ReasonCode        string `json:"reason_code"`
			PlanDigest        string `json:"plan_digest"`
			Roles             []struct {
				RoleID       string   `json:"role_id"`
				AllowedTools []string `json:"allowed_tools"`
			} `json:"roles"`
		} `json:"execution_strategy_plan"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.ToolActivities, 1)
	require.Equal(t, "hybrid_search_tweets", response.ToolActivities[0].ToolName)
	require.Equal(t, "succeeded", response.ToolActivities[0].Status)
	require.Equal(t, int32(1), response.ToolActivities[0].ResultCount)
	require.Len(t, response.Citations, 1)
	require.Equal(t, "9007199254740993", response.Citations[0].SourceID)
	require.Equal(t, "/tweets/9007199254740993", response.Citations[0].URL)
	require.Equal(t, "completed", response.RunStatus)
	require.Equal(t, "workflow_aaaaaaaaaaaaaaaaaaaaaaaa", response.SelectedSkillID)
	require.Equal(t, "v1-deadbeef", response.SelectedSkillVersion)
	require.Len(t, response.Artifacts, 1)
	require.Equal(t, "run-1", response.Artifacts[0].SourceRunID)
	require.True(t, response.Artifacts[0].RequiresConfirmation)
	require.Equal(t, "not_required", response.ApprovalState.Status)
	require.Equal(t, "run-1", response.ApprovalState.RunID)
	require.Empty(t, response.ApprovalState.ResumeToken)
	require.Equal(t, "multi_agent", response.ExecutionStrategyPlan.CandidateStrategy)
	require.Equal(t, "single_agent", response.ExecutionStrategyPlan.SelectedStrategy)
	require.Equal(t, "multi_executor_unavailable", response.ExecutionStrategyPlan.ReasonCode)
	require.Equal(t, "digest-1", response.ExecutionStrategyPlan.PlanDigest)
	require.Len(t, response.ExecutionStrategyPlan.Roles, 1)
	require.Equal(t, []string{"hybrid_search_tweets"}, response.ExecutionStrategyPlan.Roles[0].AllowedTools)
	require.NotContains(t, recorder.Body.String(), "resume_token")
}
