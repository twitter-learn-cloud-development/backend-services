package evidence

import (
	"context"
	"encoding/json"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestPlatformTweetFollowUpGoalRequiresPriorReferenceAndBoundDetailObservation(t *testing.T) {
	t.Parallel()
	task := platformTweetFollowUpTask()
	run := platformTweetFollowUpRun(true)
	collector := PlatformTweetFollowUpGoalCollector{
		ExpectedTweetID: "9007199254740993",
		PriorReference:  "/tweets/9007199254740993",
	}
	items, err := collector.Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: run,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("evidence = %+v", items)
	}
	ledger := agentRuntime.EvidenceLedger{}
	for _, item := range items {
		ledger, err = ledger.With(item)
		if err != nil {
			t.Fatalf("ledger.With() error = %v", err)
		}
	}
	verification, err := (PlatformTweetFollowUpGoalVerifier{
		ExpectedTweetID: "9007199254740993",
		PriorReference:  "/tweets/9007199254740993",
	}).Verify(context.Background(), agentRuntime.VerificationRequest{
		Task: task, Run: run, Evidence: ledger,
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !verification.Passed() || len(verification.Checks) != 2 {
		t.Fatalf("verification = %+v", verification)
	}
}

func TestPlatformTweetFollowUpGoalRejectsTextOnlyDetail(t *testing.T) {
	t.Parallel()
	task := platformTweetFollowUpTask()
	run := platformTweetFollowUpRun(false)
	collector := PlatformTweetFollowUpGoalCollector{
		ExpectedTweetID: "9007199254740993",
		PriorReference:  "/tweets/9007199254740993",
	}
	items, err := collector.Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: run,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(items) != 1 || items[0].Kind != agentRuntime.EvidenceEnvironmentState {
		t.Fatalf("text-only evidence = %+v", items)
	}
}

func platformTweetFollowUpTask() agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "e2e-06", Goal: "read the selected prior platform result",
		AllowedTools: []string{platformTweetDetailTool},
		CompletionCriteria: []agentRuntime.CompletionCriterion{
			{ID: PlatformTweetPriorReferenceCriterion, Description: "Prior result reference is bound.", Required: true},
			{ID: PlatformTweetDetailResultCriterion, Description: "Authoritative detail is observed.", Required: true},
		},
	}
}

func platformTweetFollowUpRun(structured bool) agentRuntime.RunResult {
	observation := agentRuntime.Observation{
		ActionID: "detail-1", Name: platformTweetDetailTool, Content: "display text",
	}
	if structured {
		observation.StructuredContent = json.RawMessage(`{"schema":"platform.tweet_detail.v1","items":[{"tweet_id":"9007199254740993","content":"authoritative detail"}]}`)
	}
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-e2e-06"},
		Status:  agentRuntime.RunStatusCompleted,
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "detail-1", Type: agentRuntime.ActionToolCall, Name: platformTweetDetailTool,
				Arguments: json.RawMessage(`{"tweet_ids":"9007199254740993"}`),
			}},
			Observations: []agentRuntime.Observation{observation},
		}},
	}
}
