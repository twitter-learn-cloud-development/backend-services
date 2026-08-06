package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/engine"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type traceModelClientFake struct {
	response agentRuntime.ModelResponse
	err      error
}

func (f traceModelClientFake) Complete(context.Context, agentRuntime.ModelRequest) (agentRuntime.ModelResponse, error) {
	return f.response, f.err
}

func TestTracingModelClientStoresHashesWithoutContent(t *testing.T) {
	recorder := agentObservability.NewInMemoryRecorder()
	times := []time.Time{time.Unix(100, 0), time.Unix(100, int64(250*time.Millisecond))}
	index := 0
	client := &tracingModelClient{
		delegate: traceModelClientFake{response: agentRuntime.ModelResponse{
			Message: agentRuntime.Message{Content: "private completion"}, Model: "qwen-test", Provider: "local",
			Usage: agentRuntime.TokenUsage{InputTokens: 12, OutputTokens: 5, TotalTokens: 17},
		}},
		recorder: recorder,
		now:      func() time.Time { value := times[index]; index++; return value },
	}
	_, err := client.Complete(context.Background(), agentRuntime.ModelRequest{
		Context: agentRuntime.RunContext{
			RunID: "runtime-run", UserID: 8, Mode: agentRuntime.ModeConsult,
			PromptTemplateID: "consult.search.system", PromptTemplateVersion: "v1",
		},
		StepIndex: 1, Model: "requested", Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "private prompt"}},
	})
	require.NoError(t, err)
	bundle, err := recorder.GetTraceBundle(context.Background(), 8, "runtime-run")
	require.NoError(t, err)
	require.Len(t, bundle.LLMCalls, 1)
	call := bundle.LLMCalls[0]
	require.Equal(t, "qwen-test", call.Model)
	require.Equal(t, "local", call.Provider)
	require.EqualValues(t, 250, call.DurationMS)
	require.NotEmpty(t, call.PromptHash)
	require.NotEmpty(t, call.CompletionHash)
	require.Equal(t, "consult.search.system", call.PromptTemplateID)
	require.Equal(t, "v1", call.PromptTemplateVersion)
	require.Equal(t, agentObservability.ContentSampleStatusDisabled, call.PromptSampleStatus)
	encoded, err := json.Marshal(bundle)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private prompt")
	require.NotContains(t, string(encoded), "private completion")
}

func TestTracingModelClientCorrelatesRoleCallWithParentRun(t *testing.T) {
	recorder := agentObservability.NewInMemoryRecorder()
	client := &tracingModelClient{
		delegate: traceModelClientFake{response: agentRuntime.ModelResponse{
			Message: agentRuntime.Message{Content: "evidence brief"}, Model: "qwen-test", Provider: "local",
		}},
		recorder: recorder,
	}

	_, err := client.Complete(context.Background(), agentRuntime.ModelRequest{
		Context: agentRuntime.RunContext{
			RunID: "parent-run:role:researcher", ParentRunID: "parent-run", RoleID: "researcher", UserID: 8,
			PromptTemplateID: "multi.runtime.platform_researcher.system", PromptTemplateVersion: "v1",
		},
		StepIndex: 1,
	})
	require.NoError(t, err)

	bundle, err := recorder.GetTraceBundle(context.Background(), 8, "parent-run")
	require.NoError(t, err)
	require.Len(t, bundle.LLMCalls, 1)
	require.Equal(t, "researcher:step-0001", bundle.LLMCalls[0].StepID)
	require.Equal(t, "parent-run:llm:researcher:step-0001", bundle.LLMCalls[0].RecordID)
	childBundle, err := recorder.GetTraceBundle(context.Background(), 8, "parent-run:role:researcher")
	require.NoError(t, err)
	require.Empty(t, childBundle.LLMCalls)
}

func TestRecordRuntimeResultPreservesMultiAgentRoleSteps(t *testing.T) {
	recorder := agentObservability.NewInMemoryRecorder()
	svc := &AgentService{traceRecorder: recorder}
	startedAt := time.Now().Add(-time.Second)
	svc.recordRuntimeResult(context.Background(), agentRuntime.RunResult{
		Context: agentRuntime.RunContext{
			RunID: "parent-run", UserID: 8, Mode: agentRuntime.ModeAssist, StartedAt: startedAt,
			Budget: agentRuntime.Budget{MaxSteps: 5, MaxTotalTokens: 24000},
		},
		Status: agentRuntime.RunStatusCompleted,
		Steps: []agentRuntime.Step{
			{Index: 1, RoleID: "researcher", StartedAt: startedAt, FinishedAt: startedAt.Add(time.Millisecond)},
			{Index: 1, RoleID: "drafter", StartedAt: startedAt, FinishedAt: startedAt.Add(2 * time.Millisecond)},
		},
	}, nil, "multi_agent:platform.research_draft.v1")

	bundle, err := recorder.GetTraceBundle(context.Background(), 8, "parent-run")
	require.NoError(t, err)
	require.Len(t, bundle.Steps, 2)
	require.Equal(t, "drafter:step-0001", bundle.Steps[0].StepID)
	require.Equal(t, "drafter", bundle.Steps[0].Name)
	require.Equal(t, "agent_role_step", bundle.Steps[0].StepType)
	require.Equal(t, "researcher:step-0001", bundle.Steps[1].StepID)
}

func TestTracingModelClientSamplesSafeContentAndRejectsSecrets(t *testing.T) {
	recorder := agentObservability.NewInMemoryRecorder()
	sampler, err := agentObservability.NewSafeContentSampler(agentObservability.ContentSamplingConfig{
		Enabled: true, Ratio: 1, MaxBytes: 128,
	})
	require.NoError(t, err)
	client := &tracingModelClient{
		delegate: traceModelClientFake{response: agentRuntime.ModelResponse{
			Message: agentRuntime.Message{Content: "safe completion"}, Model: "qwen", Provider: "custom",
		}},
		recorder: recorder, sampler: sampler,
	}

	_, err = client.Complete(context.Background(), agentRuntime.ModelRequest{
		Context: agentRuntime.RunContext{RunID: "safe-run", UserID: 8}, StepIndex: 1,
		Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "safe prompt"}},
	})
	require.NoError(t, err)
	safeBundle, err := recorder.GetTraceBundle(context.Background(), 8, "safe-run")
	require.NoError(t, err)
	require.Equal(t, agentObservability.ContentSampleStatusCaptured, safeBundle.LLMCalls[0].PromptSampleStatus)
	require.Contains(t, safeBundle.LLMCalls[0].PromptSample, "safe prompt")
	require.Equal(t, "safe completion", safeBundle.LLMCalls[0].CompletionSample)

	_, err = client.Complete(context.Background(), agentRuntime.ModelRequest{
		Context: agentRuntime.RunContext{RunID: "secret-run", UserID: 8}, StepIndex: 1,
		Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "api_key=must-not-leak"}},
	})
	require.NoError(t, err)
	secretBundle, err := recorder.GetTraceBundle(context.Background(), 8, "secret-run")
	require.NoError(t, err)
	require.Equal(t, agentObservability.ContentSampleStatusSensitive, secretBundle.LLMCalls[0].PromptSampleStatus)
	require.Empty(t, secretBundle.LLMCalls[0].PromptSample)
	encoded, err := json.Marshal(secretBundle)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "must-not-leak")
}

func TestAgentServiceRecordsRuntimeAndWorkflowSteps(t *testing.T) {
	recorder := agentObservability.NewInMemoryRecorder()
	svc := &AgentService{traceRecorder: recorder}
	startedAt := time.Now().Add(-time.Second)
	svc.recordRuntimeResult(context.Background(), agentRuntime.RunResult{
		Context: agentRuntime.RunContext{
			RunID: "runtime-1", UserID: 21, Mode: agentRuntime.ModeAssist, StartedAt: startedAt,
			Budget: agentRuntime.Budget{MaxSteps: 4, MaxTotalTokens: 1000},
		},
		Status: agentRuntime.RunStatusCompleted,
		Steps:  []agentRuntime.Step{{Index: 1, StartedAt: startedAt, FinishedAt: startedAt.Add(200 * time.Millisecond)}},
		Usage:  agentRuntime.TokenUsage{TotalTokens: 30},
	}, nil, "assist.draft")
	runtimeBundle, err := recorder.GetTraceBundle(context.Background(), 21, "runtime-1")
	require.NoError(t, err)
	require.Equal(t, "assist.draft", runtimeBundle.Run.Strategy)
	require.Len(t, runtimeBundle.Steps, 1)
	require.Equal(t, "step-0001", runtimeBundle.Steps[0].StepID)

	workflowRun := &repository.WorkflowRunRecord{
		ID: primitive.NewObjectID(), WorkflowID: primitive.NewObjectID(), UserID: 21,
		Status: WorkflowRunStatusSuccess, StartedAt: startedAt, FinishedAt: startedAt.Add(time.Second),
	}
	svc.recordWorkflowExecution(context.Background(), workflowRun, []engine.NodeTrace{{
		NodeID: "llm", NodeType: "llm", Status: engine.NodeStatusSuccess,
		Attempt: 1, MaxAttempts: 2, StartedAt: startedAt.UnixMilli(), FinishedAt: startedAt.Add(300 * time.Millisecond).UnixMilli(), DurationMs: 300,
	}}, agentRuntime.BudgetSnapshot{NodeExecutions: 1, Usage: agentRuntime.TokenUsage{TotalTokens: 44}})
	workflowBundle, err := recorder.GetTraceBundle(context.Background(), 21, workflowRun.ID.Hex())
	require.NoError(t, err)
	require.Equal(t, "dag", workflowBundle.Run.Strategy)
	require.EqualValues(t, 44, workflowBundle.Run.Budget.ConsumedTokens)
	require.Len(t, workflowBundle.Steps, 1)
	require.Equal(t, "llm", workflowBundle.Steps[0].StepType)
}

func TestToolTraceAuditSinkStoresDigestInsteadOfArguments(t *testing.T) {
	recorder := agentObservability.NewInMemoryRecorder()
	sink := NewToolTraceAuditSink(recorder)
	sink.Record(context.Background(), workflowTool.AuditEvent{
		ToolName: "WebSearch", Category: workflowTool.CategoryRead, UserID: 31,
		RunID: "workflow-1", StepID: "search", Source: workflowTool.SourceWorkflow,
		Decision: "executed", Duration: 20 * time.Millisecond, Attempts: 1,
		Inputs:       map[string]interface{}{"query": "sensitive query"},
		OutputDigest: "result-digest", OutputLength: 123,
		OutputReference: &workflowTool.ResultReference{
			Storage: "minio", Bucket: "agent-tool-results", Key: "tool-results/a/b/c.json",
			Digest: "result-digest", Length: 123, ContentType: "application/json",
		},
	})
	bundle, err := recorder.GetTraceBundle(context.Background(), 31, "workflow-1")
	require.NoError(t, err)
	require.Len(t, bundle.ToolCalls, 1)
	require.NotEmpty(t, bundle.ToolCalls[0].ArgumentsHash)
	require.Equal(t, "result-digest", bundle.ToolCalls[0].OutputHash)
	require.Equal(t, 123, bundle.ToolCalls[0].OutputLength)
	require.Equal(t, "minio", bundle.ToolCalls[0].OutputStorage)
	require.Equal(t, "minio://agent-tool-results/tool-results/a/b/c.json", bundle.ToolCalls[0].OutputReference)
	require.Equal(t, "application/json", bundle.ToolCalls[0].OutputContentType)
	encoded, err := json.Marshal(bundle)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sensitive query")
}

func TestTracingModelClientPreservesDelegateError(t *testing.T) {
	want := errors.New("provider unavailable")
	client := &tracingModelClient{delegate: traceModelClientFake{err: want}, recorder: agentObservability.NoopRecorder{}}
	_, err := client.Complete(context.Background(), agentRuntime.ModelRequest{})
	require.ErrorIs(t, err, want)
}
