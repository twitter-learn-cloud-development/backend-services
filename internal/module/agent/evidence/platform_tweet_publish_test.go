package evidence

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type tweetPublishCatalogStub struct {
	tools []agentRuntime.ToolDefinition
}

func (stub tweetPublishCatalogStub) ListTools(context.Context) ([]agentRuntime.ToolDefinition, error) {
	return append([]agentRuntime.ToolDefinition(nil), stub.tools...), nil
}

type tweetPublishStateStub struct {
	pages []agentEnvironment.TweetWriteStatePage
	calls int
}

func (stub *tweetPublishStateStub) ListRecentTweetWriteState(
	_ context.Context,
	_ uint64,
	_ int,
) (agentEnvironment.TweetWriteStatePage, error) {
	index := stub.calls
	stub.calls++
	if index >= len(stub.pages) {
		index = len(stub.pages) - 1
	}
	page := stub.pages[index]
	page.Tweets = append([]agentEnvironment.TweetWriteState(nil), page.Tweets...)
	return page, nil
}

func TestTweetPublishGoalVerifierProvesAuthoritativeCommit(t *testing.T) {
	task := tweetPublishTask(TweetPublishCommittedCriterion)
	before, after := tweetPublishSnapshots(t, task,
		[]agentEnvironment.TweetWriteState{{TweetID: 10, AuthorID: 7}},
		[]agentEnvironment.TweetWriteState{{TweetID: 11, AuthorID: 7}, {TweetID: 10, AuthorID: 7}},
	)
	run := tweetPublishRun(t, 11, true)
	collector := TweetPublishGoalCollector{}
	items, err := collector.Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: run, Before: &before, After: &after,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(items) != 1 || items[0].Reference != "/tweets/11" || items[0].Digest != after.Digest {
		t.Fatalf("evidence = %+v", items)
	}
	ledger, err := (agentRuntime.EvidenceLedger{}).With(items[0])
	if err != nil {
		t.Fatal(err)
	}
	result, err := (TweetPublishGoalVerifier{}).Verify(context.Background(), agentRuntime.VerificationRequest{
		Task: task, Run: run, Before: &before, After: &after, Evidence: ledger,
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Passed() {
		t.Fatalf("verification = %+v", result)
	}
}

func TestTweetPublishGoalVerifierProvesIdempotentReplay(t *testing.T) {
	task := tweetPublishTask(TweetPublishIdempotentCriterion)
	state := []agentEnvironment.TweetWriteState{{TweetID: 11, AuthorID: 7}, {TweetID: 10, AuthorID: 7}}
	before, after := tweetPublishSnapshots(t, task, state, state)
	run := tweetPublishRun(t, 11, true)
	items, err := (TweetPublishGoalCollector{}).Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: run, Before: &before, After: &after,
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("Collect() items/error = %+v/%v", items, err)
	}
	ledger, err := (agentRuntime.EvidenceLedger{}).With(items[0])
	if err != nil {
		t.Fatal(err)
	}
	result, err := (TweetPublishGoalVerifier{}).Verify(context.Background(), agentRuntime.VerificationRequest{
		Task: task, Run: run, Before: &before, After: &after, Evidence: ledger,
	})
	if err != nil || !result.Passed() {
		t.Fatalf("Verify() result/error = %+v/%v", result, err)
	}
}

func TestTweetPublishGoalEvidenceFailsClosed(t *testing.T) {
	task := tweetPublishTask(TweetPublishCommittedCriterion)
	before, after := tweetPublishSnapshots(t, task,
		[]agentEnvironment.TweetWriteState{{TweetID: 10, AuthorID: 7}},
		[]agentEnvironment.TweetWriteState{{TweetID: 11, AuthorID: 7}, {TweetID: 12, AuthorID: 7}, {TweetID: 10, AuthorID: 7}},
	)
	tests := []struct {
		name string
		run  agentRuntime.RunResult
	}{
		{name: "unpaired observation", run: tweetPublishRun(t, 11, false)},
		{name: "text only", run: agentRuntime.RunResult{
			Context: agentRuntime.RunContext{RunID: "run-text", UserID: 7},
			Steps: []agentRuntime.Step{{
				Index:        1,
				Actions:      []agentRuntime.Action{{ID: "publish-1", Type: agentRuntime.ActionToolCall, Name: agentEnvironment.TweetPublishToolName}},
				Observations: []agentRuntime.Observation{{ActionID: "publish-1", Name: agentEnvironment.TweetPublishToolName, Content: "published tweet 11"}},
			}},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			items, err := (TweetPublishGoalCollector{}).Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
				Task: task, Run: test.run, Before: &before, After: &after,
			})
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(items) != 0 {
				t.Fatalf("evidence = %+v, want none", items)
			}
		})
	}

	run := tweetPublishRun(t, 11, true)
	forged := agentRuntime.EvidenceLedger{Items: []agentRuntime.Evidence{{
		ID: "forged", Kind: agentRuntime.EvidenceEnvironmentState,
		Source:       agentEnvironment.TweetPublishToolName,
		CriterionIDs: []string{TweetPublishCommittedCriterion},
		Digest:       after.Digest, Reference: "/tweets/11",
	}}}
	verification, err := (TweetPublishGoalVerifier{}).Verify(context.Background(), agentRuntime.VerificationRequest{
		Task: task, Run: run, Before: &before, After: &after, Evidence: forged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if verification.Passed() {
		t.Fatalf("forged verification passed: %+v", verification)
	}
}

func TestTweetPublishGoalVerifierRejectsTamperedSnapshotAndWrongTransition(t *testing.T) {
	commitTask := tweetPublishTask(TweetPublishCommittedCriterion)
	before, after := tweetPublishSnapshots(t, commitTask,
		[]agentEnvironment.TweetWriteState{{TweetID: 10, AuthorID: 7}},
		[]agentEnvironment.TweetWriteState{{TweetID: 11, AuthorID: 7}, {TweetID: 10, AuthorID: 7}},
	)
	run := tweetPublishRun(t, 11, true)
	tampered := after
	var metadata map[string]any
	if err := json.Unmarshal(tampered.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata["tweet_references"] = []string{"/tweets/99"}
	tampered.Metadata, _ = json.Marshal(metadata)
	if _, err := (TweetPublishGoalVerifier{}).Verify(context.Background(), agentRuntime.VerificationRequest{
		Task: commitTask, Run: run, Before: &before, After: &tampered,
	}); err == nil {
		t.Fatal("tampered after snapshot was accepted")
	}

	idempotentTask := tweetPublishTask(TweetPublishIdempotentCriterion)
	newBefore, newAfter := tweetPublishSnapshots(t, idempotentTask,
		[]agentEnvironment.TweetWriteState{{TweetID: 10, AuthorID: 7}},
		[]agentEnvironment.TweetWriteState{{TweetID: 11, AuthorID: 7}, {TweetID: 10, AuthorID: 7}},
	)
	items, err := (TweetPublishGoalCollector{}).Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: idempotentTask, Run: run, Before: &newBefore, After: &newAfter,
	})
	if err != nil || len(items) != 0 {
		t.Fatalf("idempotent new publish evidence/error = %+v/%v", items, err)
	}
}

func tweetPublishTask(criterion string) agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "tweet-publish", Goal: "publish one tweet", AllowedTools: []string{agentEnvironment.TweetPublishToolName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: criterion, Description: "the publish side effect is proven by authoritative state", Required: true,
		}},
		MaxRepairAttempts: 1,
	}
}

func tweetPublishRun(t *testing.T, tweetID uint64, paired bool) agentRuntime.RunResult {
	t.Helper()
	structured, err := json.Marshal(NewPlatformTweetPublishResult(tweetID))
	if err != nil {
		t.Fatal(err)
	}
	actionID := "publish-1"
	if !paired {
		actionID = "different-action"
	}
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-publish", UserID: 7},
		Status:  agentRuntime.RunStatusCompleted,
		Steps: []agentRuntime.Step{{
			Index: 1, FinishedAt: time.Date(2026, 8, 10, 12, 0, 1, 0, time.UTC),
			Actions: []agentRuntime.Action{{ID: actionID, Type: agentRuntime.ActionToolCall, Name: agentEnvironment.TweetPublishToolName}},
			Observations: []agentRuntime.Observation{{
				ActionID: "publish-1", Name: agentEnvironment.TweetPublishToolName, StructuredContent: structured,
			}},
		}},
	}
}

func tweetPublishSnapshots(
	t *testing.T,
	task agentRuntime.TaskSpec,
	beforeState []agentEnvironment.TweetWriteState,
	afterState []agentEnvironment.TweetWriteState,
) (agentRuntime.EnvironmentSnapshot, agentRuntime.EnvironmentSnapshot) {
	t.Helper()
	reader := &tweetPublishStateStub{pages: []agentEnvironment.TweetWriteStatePage{
		{Tweets: beforeState}, {Tweets: afterState},
	}}
	environment, err := agentEnvironment.NewTweetWriteEnvironment(
		tweetPublishCatalogStub{tools: []agentRuntime.ToolDefinition{{
			Name:     agentEnvironment.TweetPublishToolName,
			Category: agentRuntime.ToolCategoryWrite, RequiresApproval: true,
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
		reader,
		7,
		agentEnvironment.WithTweetWriteClock(func() time.Time {
			return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	before, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	return before, after
}
