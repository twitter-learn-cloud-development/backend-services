package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	tweetv1 "twitter-clone/api/tweet/v1"
	"twitter-clone/internal/module/agent/workflow/dsl"
	"twitter-clone/internal/module/agent/workflow/engine"
	"twitter-clone/internal/module/agent/workflow/guardrails"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type recordingTweetClient struct {
	tweetv1.TweetServiceClient
	calls       int
	lastRequest *tweetv1.CreateTweetRequest
}

func (c *recordingTweetClient) CreateTweet(ctx context.Context, request *tweetv1.CreateTweetRequest, options ...grpc.CallOption) (*tweetv1.CreateTweetResponse, error) {
	c.calls++
	c.lastRequest = request
	return &tweetv1.CreateTweetResponse{Tweet: &tweetv1.Tweet{Id: 9001}}, nil
}

type approvedToolGate struct{}

func (approvedToolGate) Authorize(context.Context, workflowTool.ApprovalCheck) (workflowTool.ApprovalGrant, error) {
	return workflowTool.ApprovalGrant{ApprovalID: "approval-1", AttemptID: "attempt-1"}, nil
}

func TestWorkflowPublishTweetRequiresApprovalWithoutWaitNode(t *testing.T) {
	client := &recordingTweetClient{}
	registry := workflowTool.NewRegistry()
	require.NoError(t, registry.Register(workflowTool.NewPublishTweetTool(client)))
	executor := workflowTool.NewExecutor(registry)

	properties, err := json.Marshal(map[string]interface{}{
		"tool_name": "PublishTweet",
		"content":   "must not be published",
	})
	require.NoError(t, err)
	definition := &dsl.WorkflowDSL{
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "publish", Type: "tool", Properties: properties},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{
			{ID: "start-publish", Source: "start", Target: "publish"},
			{ID: "publish-end", Source: "publish", Target: "end"},
		},
	}
	nodes, err := buildWorkflowNodes(definition, executor)
	require.NoError(t, err)
	scheduler, err := engine.NewScheduler(definition, nodes)
	require.NoError(t, err)

	ctx := guardrails.InjectUserContext(context.Background(), 42)
	ctx = workflowTool.InjectExecutionMetadata(ctx, workflowTool.ExecutionMetadata{
		RunID: "run-no-wait", Source: workflowTool.SourceWorkflow,
	})
	err = scheduler.Execute(ctx, map[string]interface{}{})

	require.Error(t, err)
	require.True(t, errors.Is(err, workflowTool.ErrApprovalRequired), "unexpected error: %v", err)
	require.Zero(t, client.calls)
}

func TestLegacyMCPWriteFailsBeforeTransportCall(t *testing.T) {
	service := &AgentService{workflowToolExecutor: workflowTool.NewExecutor(workflowTool.NewRegistry())}
	_, err := service.executeMCPToolGoverned(context.Background(), nil, workflowTool.ExecutionRequest{
		ToolName: "create_tweet",
		Inputs:   map[string]interface{}{"content": "must not publish"},
		Identity: workflowTool.CallerIdentity{UserID: 42},
		RunID:    "legacy-run", StepID: "tool-call-1", Source: workflowTool.SourceLegacy,
		IdempotencyKey: "legacy-run:tool-call-1:create_tweet",
	}, mcp.CallToolRequest{Params: mcp.CallToolParams{
		Name: "create_tweet", Arguments: map[string]interface{}{"content": "must not publish"},
	}})

	require.ErrorIs(t, err, workflowTool.ErrApprovalRequired)
}

func TestPublishTweetPropagatesExecutorIdempotencyKey(t *testing.T) {
	client := &recordingTweetClient{}
	registry := workflowTool.NewRegistry()
	require.NoError(t, registry.Register(workflowTool.NewPublishTweetTool(client)))
	executor := workflowTool.NewExecutor(registry, workflowTool.WithApprovalGate(approvedToolGate{}))

	result, err := executor.ExecuteRegistered(context.Background(), workflowTool.ExecutionRequest{
		ToolName: "PublishTweet", Inputs: map[string]interface{}{"content": "publish once"},
		Identity: workflowTool.CallerIdentity{UserID: 42}, RunID: "run-1", StepID: "publish",
		Source: workflowTool.SourceWorkflow, IdempotencyKey: "run-1:publish:PublishTweet",
	})

	require.NoError(t, err)
	require.Equal(t, uint64(9001), result["tweet_id"])
	require.NotNil(t, client.lastRequest)
	require.Equal(t, "run-1:publish:PublishTweet", client.lastRequest.IdempotencyKey)
}
