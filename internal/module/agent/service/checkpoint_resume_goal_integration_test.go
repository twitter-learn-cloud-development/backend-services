package service

import (
	"context"
	"encoding/json"
	"fmt"
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
)

func TestCheckpointResumePreservesCompletedWriteAndEvidence(t *testing.T) {
	const (
		userID  = uint64(7)
		tweetID = uint64(9002)
	)
	client := &controlledTweetClient{
		nextID: tweetID,
		tweets: []*tweetv1.Tweet{{Id: 100, UserId: userID, Content: "existing"}},
	}
	mcpServer := server.NewMCPServer("controlled-checkpoint-resume", "v1")
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
			return time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	model := &controlledModel{responses: []agentRuntime.ModelResponse{
		{Actions: []agentRuntime.Action{{
			ID: "publish-1", Type: agentRuntime.ActionToolCall,
			Name:      agentEnvironment.TweetPublishToolName,
			Arguments: json.RawMessage(`{"content":"需要确认范围的受控正文"}`),
		}}},
		{Actions: []agentRuntime.Action{{
			ID: "confirm-scope-2", Type: agentRuntime.ActionAskHuman,
			Content: "请确认后续分析范围。",
		}}},
		{Message: agentRuntime.Message{Content: "发布保持不变，后续范围已确认。"}},
	}}
	verified := agentRuntime.NewVerifiedRunner(
		agentRuntime.NewReActRunner(model, runtimeExecutor, nil),
		agentEvidence.TweetPublishGoalVerifier{},
		agentEvidence.TweetPublishGoalCollector{},
	)
	task := controlledTweetTask(agentEvidence.TweetPublishCommittedCriterion)
	request := controlledTweetRunRequest(definitions)

	approvalSuspended, err := verified.Run(context.Background(), agentRuntime.VerifiedRunRequest{
		Task: task, Run: request, Environment: environment,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if approvalSuspended.Status != agentRuntime.GoalRunSuspended ||
		approvalSuspended.Checkpoint == nil || approvalSuspended.Checkpoint.Revision != 1 ||
		approvalSuspended.Run.ApprovalID != "approval-tweet-1" {
		t.Fatalf("approval checkpoint = %+v", approvalSuspended)
	}
	if writes, _ := client.snapshot(); writes != 0 {
		t.Fatalf("writes before approval = %d", writes)
	}

	approvalCheckpoint := roundTripVerifiedCheckpoint(t, *approvalSuspended.Checkpoint)
	approval.approve()
	humanSuspended, err := verified.Resume(context.Background(), agentRuntime.VerifiedResumeRequest{
		Checkpoint: approvalCheckpoint, ApprovalID: "approval-tweet-1",
		Tools: definitions, Environment: environment,
	})
	if err != nil {
		t.Fatalf("approval Resume() error = %v", err)
	}
	if humanSuspended.Status != agentRuntime.GoalRunSuspended || humanSuspended.Checkpoint == nil ||
		humanSuspended.Checkpoint.Revision != 2 ||
		humanSuspended.Run.Status != agentRuntime.RunStatusAwaitingHuman {
		t.Fatalf("human checkpoint = %+v", humanSuspended)
	}
	if writes, writeRequest := client.snapshot(); writes != 1 || writeRequest == nil ||
		writeRequest.IdempotencyKey != "run-tweet:publish-1:create_tweet" {
		t.Fatalf("write after approval = %d/%+v", writes, writeRequest)
	}
	assertCheckpointResumeEvidence(t, humanSuspended.Evidence, request.Context.RunID, []int64{1})
	if countEvidence(humanSuspended.Evidence, agentRuntime.EvidenceEnvironmentState) != 1 {
		t.Fatalf("suspended evidence = %+v", humanSuspended.Evidence)
	}

	humanCheckpoint := roundTripVerifiedCheckpoint(t, *humanSuspended.Checkpoint)
	completed, err := verified.Resume(context.Background(), agentRuntime.VerifiedResumeRequest{
		Checkpoint: humanCheckpoint, HumanResponse: "仅分析当前仓库",
		Tools: definitions, Environment: environment,
	})
	if err != nil {
		t.Fatalf("human Resume() error = %v", err)
	}
	if completed.Status != agentRuntime.GoalRunVerified || !completed.Verification.Passed() ||
		completed.Run.Context.RunID != request.Context.RunID || len(completed.Run.Steps) != 3 {
		t.Fatalf("completed result = %+v", completed)
	}
	assertCheckpointResumeEvidence(t, completed.Evidence, request.Context.RunID, []int64{1, 2})
	if countEvidence(completed.Evidence, agentRuntime.EvidenceEnvironmentState) != 1 {
		t.Fatalf("completed evidence = %+v", completed.Evidence)
	}
	if writes, _ := client.snapshot(); writes != 1 || idempotency.completed != 1 ||
		len(idempotency.pending) != 0 || approval.completed != 1 || model.calls != 3 {
		t.Fatalf(
			"writes/idempotency/pending/approvals/model = %d/%d/%d/%d/%d",
			writes, idempotency.completed, len(idempotency.pending), approval.completed, model.calls,
		)
	}
}

func roundTripVerifiedCheckpoint(
	t *testing.T,
	checkpoint agentRuntime.VerifiedCheckpoint,
) agentRuntime.VerifiedCheckpoint {
	t.Helper()
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	var restored agentRuntime.VerifiedCheckpoint
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatalf("unmarshal checkpoint: %v", err)
	}
	if err := agentRuntime.ValidateVerifiedCheckpoint(restored); err != nil {
		t.Fatalf("validate restored checkpoint: %v", err)
	}
	return restored
}

func assertCheckpointResumeEvidence(
	t *testing.T,
	ledger agentRuntime.EvidenceLedger,
	runID string,
	revisions []int64,
) {
	t.Helper()
	actual := make(map[string]struct{})
	for _, item := range ledger.Items {
		if item.Kind != agentRuntime.EvidenceCheckpointResume {
			continue
		}
		if item.Source != agentRuntime.CheckpointResumeEvidenceSource || item.Digest == "" {
			t.Fatalf("invalid checkpoint resume evidence = %+v", item)
		}
		actual[item.Reference] = struct{}{}
	}
	if len(actual) != len(revisions) {
		t.Fatalf("checkpoint resume evidence = %+v", ledger.Items)
	}
	for _, revision := range revisions {
		reference := fmt.Sprintf("agent-run://%s/checkpoints/%d/resume", runID, revision)
		if _, ok := actual[reference]; !ok {
			t.Fatalf("missing checkpoint resume reference %q in %+v", reference, ledger.Items)
		}
	}
}

func countEvidence(ledger agentRuntime.EvidenceLedger, kind agentRuntime.EvidenceKind) int {
	count := 0
	for _, item := range ledger.Items {
		if item.Kind == kind {
			count++
		}
	}
	return count
}
