package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	tweetv1 "twitter-clone/api/tweet/v1"
	agentEnvironment "twitter-clone/internal/module/agent/environment"
	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentMCPTools "twitter-clone/internal/module/agent/mcp/tools"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"google.golang.org/grpc"
)

type controlledTweetClient struct {
	tweetv1.TweetServiceClient
	mu        sync.Mutex
	tweets    []*tweetv1.Tweet
	writes    int
	nextID    uint64
	lastWrite *tweetv1.CreateTweetRequest
}

func (client *controlledTweetClient) CreateTweet(
	_ context.Context,
	request *tweetv1.CreateTweetRequest,
	_ ...grpc.CallOption,
) (*tweetv1.CreateTweetResponse, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.writes++
	client.lastWrite = &tweetv1.CreateTweetRequest{
		UserId: request.UserId, Content: request.Content, IdempotencyKey: request.IdempotencyKey,
	}
	tweet := &tweetv1.Tweet{Id: client.nextID, UserId: request.UserId, Content: request.Content}
	client.tweets = append([]*tweetv1.Tweet{tweet}, client.tweets...)
	return &tweetv1.CreateTweetResponse{Tweet: tweet}, nil
}

func (client *controlledTweetClient) GetUserTimeline(
	_ context.Context,
	request *tweetv1.GetUserTimelineRequest,
	_ ...grpc.CallOption,
) (*tweetv1.GetUserTimelineResponse, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	response := &tweetv1.GetUserTimelineResponse{}
	for _, tweet := range client.tweets {
		if tweet.UserId != request.UserId {
			continue
		}
		response.Tweets = append(response.Tweets, &tweetv1.Tweet{
			Id: tweet.Id, UserId: tweet.UserId, Content: tweet.Content,
		})
	}
	return response, nil
}

func (client *controlledTweetClient) snapshot() (int, *tweetv1.CreateTweetRequest) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.lastWrite == nil {
		return client.writes, nil
	}
	return client.writes, &tweetv1.CreateTweetRequest{
		UserId: client.lastWrite.UserId, Content: client.lastWrite.Content,
		IdempotencyKey: client.lastWrite.IdempotencyKey,
	}
}

type controlledApprovalGate struct {
	mu        sync.Mutex
	approved  bool
	completed int
}

func (gate *controlledApprovalGate) Authorize(
	_ context.Context,
	_ workflowTool.ApprovalCheck,
) (workflowTool.ApprovalGrant, error) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if !gate.approved {
		return workflowTool.ApprovalGrant{}, &workflowTool.ApprovalPendingError{ApprovalID: "approval-tweet-1"}
	}
	return workflowTool.ApprovalGrant{ApprovalID: "approval-tweet-1", AttemptID: "attempt-tweet-1"}, nil
}

func (gate *controlledApprovalGate) Complete(context.Context, workflowTool.ApprovalGrant) error {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.completed++
	return nil
}

func (gate *controlledApprovalGate) Release(context.Context, workflowTool.ApprovalGrant, error) error {
	return nil
}

func (gate *controlledApprovalGate) approve() {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.approved = true
}

type controlledIdempotencyRecord struct {
	key     string
	digest  string
	outputs map[string]interface{}
}

type controlledIdempotencyStore struct {
	mu        sync.Mutex
	records   map[string]controlledIdempotencyRecord
	pending   map[string]controlledIdempotencyRecord
	completed int
}

func (store *controlledIdempotencyStore) Claim(
	_ context.Context,
	check workflowTool.IdempotencyCheck,
) (workflowTool.IdempotencyClaim, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := fmt.Sprintf("%d:%s:%s", check.UserID, check.ToolName, check.IdempotencyKey)
	if record, ok := store.records[key]; ok {
		if record.digest != check.InputDigest {
			return workflowTool.IdempotencyClaim{}, workflowTool.ErrIdempotencyConflict
		}
		return workflowTool.IdempotencyClaim{
			ExecutionID: "execution-tweet-1", AttemptID: "attempt-replay", UserID: check.UserID,
			Replayed: true, Outputs: cloneControlledOutputs(record.outputs),
		}, nil
	}
	store.pending["execution-tweet-1"] = controlledIdempotencyRecord{key: key, digest: check.InputDigest}
	return workflowTool.IdempotencyClaim{
		ExecutionID: "execution-tweet-1", AttemptID: "attempt-first", UserID: check.UserID,
	}, nil
}

func (store *controlledIdempotencyStore) Complete(
	_ context.Context,
	claim workflowTool.IdempotencyClaim,
	result workflowTool.ResultSnapshot,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	pending, ok := store.pending[claim.ExecutionID]
	if !ok {
		return errors.New("idempotency claim is missing")
	}
	pending.outputs = cloneControlledOutputs(result.Outputs)
	store.records[pending.key] = pending
	delete(store.pending, claim.ExecutionID)
	store.completed++
	return nil
}

func (store *controlledIdempotencyStore) Fail(context.Context, workflowTool.IdempotencyClaim, error) error {
	return nil
}

func cloneControlledOutputs(outputs map[string]interface{}) map[string]interface{} {
	encoded, _ := json.Marshal(outputs)
	var cloned map[string]interface{}
	_ = json.Unmarshal(encoded, &cloned)
	return cloned
}

type controlledMCPCaller struct {
	handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}

func (caller controlledMCPCaller) CallTool(
	ctx context.Context,
	request mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return caller.handler(ctx, request)
}

type controlledRuntimeExecutor struct {
	delegate *mcpRuntimeToolExecutor
	caller   mcpToolCaller
}

func (executor controlledRuntimeExecutor) Execute(
	ctx context.Context,
	call agentRuntime.ToolCall,
) (agentRuntime.ToolResult, error) {
	return executor.ExecuteApprovalGated(ctx, call)
}

func (executor controlledRuntimeExecutor) ExecuteApprovalGated(
	ctx context.Context,
	call agentRuntime.ToolCall,
) (agentRuntime.ToolResult, error) {
	var arguments map[string]any
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return agentRuntime.ToolResult{}, err
	}
	return executor.delegate.executeBuiltInMCP(ctx, call, arguments, executor.caller)
}

type controlledModel struct {
	responses []agentRuntime.ModelResponse
	calls     int
}

func (model *controlledModel) Complete(
	_ context.Context,
	_ agentRuntime.ModelRequest,
) (agentRuntime.ModelResponse, error) {
	if model.calls >= len(model.responses) {
		return agentRuntime.ModelResponse{}, errors.New("unexpected model call")
	}
	response := model.responses[model.calls]
	model.calls++
	return response, nil
}

type controlledToolCatalog struct {
	tools []agentRuntime.ToolDefinition
}

func (catalog controlledToolCatalog) ListTools(context.Context) ([]agentRuntime.ToolDefinition, error) {
	return append([]agentRuntime.ToolDefinition(nil), catalog.tools...), nil
}

func TestTweetPublishGoalApprovalAndIdempotentReplay(t *testing.T) {
	const (
		userID  = uint64(7)
		tweetID = uint64(9001)
	)
	client := &controlledTweetClient{
		nextID: tweetID,
		tweets: []*tweetv1.Tweet{{Id: 100, UserId: userID, Content: "existing"}},
	}
	mcpServer := server.NewMCPServer("controlled-tweet", "v1")
	agentMCPTools.RegisterCreateTweet(mcpServer, client)
	registered := mcpServer.GetTool(agentEnvironment.TweetPublishToolName)
	if registered == nil {
		t.Fatal("create_tweet tool is not registered")
	}
	definitions := mcpToolsToRuntime([]mcp.Tool{registered.Tool})
	if len(definitions) != 1 {
		t.Fatalf("runtime definitions = %d", len(definitions))
	}

	approval := &controlledApprovalGate{}
	idempotency := &controlledIdempotencyStore{
		records: make(map[string]controlledIdempotencyRecord),
		pending: make(map[string]controlledIdempotencyRecord),
	}
	service := &AgentService{
		workflowToolExecutor: workflowTool.NewExecutor(
			workflowTool.NewRegistry(),
			workflowTool.WithApprovalGate(approval),
			workflowTool.WithIdempotencyStore(idempotency),
		),
		mcpTools: []mcp.Tool{registered.Tool},
	}
	runtimeExecutor := controlledRuntimeExecutor{
		delegate: &mcpRuntimeToolExecutor{service: service},
		caller:   controlledMCPCaller{handler: registered.Handler},
	}
	environment, err := agentEnvironment.NewTweetWriteEnvironment(
		controlledToolCatalog{tools: definitions},
		tweetServiceWriteStateReader{client: client},
		userID,
		agentEnvironment.WithTweetWriteClock(func() time.Time {
			return time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	commitTask := controlledTweetTask(agentEvidence.TweetPublishCommittedCriterion)
	model := newControlledTweetModel()
	verified := agentRuntime.NewVerifiedRunner(
		agentRuntime.NewReActRunner(model, runtimeExecutor, nil),
		agentEvidence.TweetPublishGoalVerifier{},
		agentEvidence.TweetPublishGoalCollector{},
	)
	request := controlledTweetRunRequest(definitions)
	suspended, err := verified.Run(context.Background(), agentRuntime.VerifiedRunRequest{
		Task: commitTask, Run: request, Environment: environment,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if suspended.Status != agentRuntime.GoalRunSuspended || suspended.Checkpoint == nil ||
		suspended.Run.ApprovalID != "approval-tweet-1" {
		t.Fatalf("suspended result = %+v", suspended)
	}
	if writes, _ := client.snapshot(); writes != 0 {
		t.Fatalf("writes before approval = %d", writes)
	}

	approval.approve()
	resumed, err := verified.Resume(context.Background(), agentRuntime.VerifiedResumeRequest{
		Checkpoint: *suspended.Checkpoint, ApprovalID: "approval-tweet-1",
		Tools: definitions, Environment: environment,
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Status != agentRuntime.GoalRunVerified || !resumed.Verification.Passed() {
		t.Fatalf("resumed result = %+v", resumed)
	}
	writes, writeRequest := client.snapshot()
	if writes != 1 || writeRequest == nil || writeRequest.UserId != userID ||
		writeRequest.IdempotencyKey != "run-tweet:publish-1:create_tweet" {
		t.Fatalf("first write = %d/%+v", writes, writeRequest)
	}

	replayModel := newControlledTweetModel()
	replayRunner := agentRuntime.NewVerifiedRunner(
		agentRuntime.NewReActRunner(replayModel, runtimeExecutor, nil),
		agentEvidence.TweetPublishGoalVerifier{},
		agentEvidence.TweetPublishGoalCollector{},
	)
	replayed, err := replayRunner.Run(context.Background(), agentRuntime.VerifiedRunRequest{
		Task: controlledTweetTask(agentEvidence.TweetPublishIdempotentCriterion),
		Run:  request, Environment: environment,
	})
	if err != nil {
		t.Fatalf("replay Run() error = %v", err)
	}
	if replayed.Status != agentRuntime.GoalRunVerified || !replayed.Verification.Passed() {
		t.Fatalf("replayed result = %+v", replayed)
	}
	if writes, _ := client.snapshot(); writes != 1 || idempotency.completed != 1 || approval.completed != 2 {
		t.Fatalf("replay writes/completions/approvals = %d/%d/%d", writes, idempotency.completed, approval.completed)
	}
}

func newControlledTweetModel() *controlledModel {
	return &controlledModel{responses: []agentRuntime.ModelResponse{
		{Actions: []agentRuntime.Action{{
			ID: "publish-1", Type: agentRuntime.ActionToolCall,
			Name:      agentEnvironment.TweetPublishToolName,
			Arguments: json.RawMessage(`{"content":"受控发布正文"}`),
		}}},
		{Message: agentRuntime.Message{Content: "发布操作已由权威状态验证。"}},
	}}
}

func controlledTweetTask(criterion string) agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "controlled-tweet-publish", Goal: "publish exactly one approved tweet",
		AllowedTools: []string{agentEnvironment.TweetPublishToolName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: criterion, Description: "authoritative timeline proves the publish transition", Required: true,
		}},
	}
}

func controlledTweetRunRequest(tools []agentRuntime.ToolDefinition) agentRuntime.RunRequest {
	return agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID: "run-tweet", UserID: 7, Mode: agentRuntime.ModeConsult,
			Budget: agentRuntime.Budget{MaxSteps: 4},
		},
		Model:    "controlled-model",
		Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "发布受控测试推文"}},
		Tools:    tools, InitialToolChoice: agentRuntime.ToolChoiceRequired,
	}
}
