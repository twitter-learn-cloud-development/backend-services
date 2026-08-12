package evidence

import (
	"context"
	"encoding/json"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestPlatformSearchGoalEvidenceRequiresTrustedStructuredObservation(t *testing.T) {
	task := platformSearchTask()
	run := agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-search", UserID: 7},
		Steps: []agentRuntime.Step{{
			Index: 3,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{
				{
					ActionID: "search-1",
					Name:     "hybrid_search_tweets",
					StructuredContent: json.RawMessage(`{
						"schema":"platform.tweet_search.v1",
						"query":"Go Agent",
						"items":[
							{"tweet_id":"42","content":" grounded result "},
							{"tweet_id":"42","content":"duplicate"},
							{"tweet_id":"not-a-number","content":"invalid"}
						]
					}`),
				},
				{
					ActionID: "other-1",
					Name:     "other_tool",
					StructuredContent: json.RawMessage(
						`{"schema":"platform.tweet_search.v1","items":[{"tweet_id":"99"}]}`,
					),
				},
			},
		}},
	}
	collector := PlatformSearchGoalCollector{}
	items, err := collector.Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: run, Attempt: 0,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("evidence = %+v", items)
	}
	if items[0].Source != "hybrid_search_tweets" ||
		items[0].Reference != "/tweets/42" ||
		items[0].CriterionIDs[0] != PlatformSearchResultCriterion {
		t.Fatalf("evidence item = %+v", items[0])
	}

	ledger, err := (agentRuntime.EvidenceLedger{}).With(items[0])
	if err != nil {
		t.Fatalf("With() error = %v", err)
	}
	verification, err := (PlatformSearchGoalVerifier{}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{Task: task, Run: run, Evidence: ledger},
	)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !verification.Passed() {
		t.Fatalf("verification = %+v", verification)
	}
}

func TestPlatformSearchGoalVerifierRejectsLedgerWithoutMatchingRuntimeEvidence(t *testing.T) {
	task := platformSearchTask()
	forged := agentRuntime.EvidenceLedger{Items: []agentRuntime.Evidence{{
		ID:           "forged",
		Kind:         agentRuntime.EvidenceToolObservation,
		Source:       "hybrid_search_tweets",
		CriterionIDs: []string{PlatformSearchResultCriterion},
		Digest:       "sha256:forged",
		Reference:    "/tweets/42",
	}}}

	verification, err := (PlatformSearchGoalVerifier{}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{
			Task: task,
			Run: agentRuntime.RunResult{Context: agentRuntime.RunContext{
				RunID: "run-forged", UserID: 7,
			}},
			Evidence: forged,
		},
	)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verification.Passed() ||
		verification.Status != agentRuntime.VerificationFailed ||
		len(verification.MissingEvidence) != 1 {
		t.Fatalf("verification = %+v", verification)
	}
}

func TestPlatformSearchGoalCollectorIgnoresWrongSchemaAndUnpairedAction(t *testing.T) {
	task := platformSearchTask()
	collector := PlatformSearchGoalCollector{}
	items, err := collector.Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: task,
		Run: agentRuntime.RunResult{
			Context: agentRuntime.RunContext{RunID: "run-invalid", UserID: 7},
			Steps: []agentRuntime.Step{{
				Index: 1,
				Actions: []agentRuntime.Action{{
					ID: "different-action", Type: agentRuntime.ActionToolCall,
					Name: "hybrid_search_tweets",
				}},
				Observations: []agentRuntime.Observation{
					{
						ActionID: "search-1", Name: "hybrid_search_tweets",
						StructuredContent: json.RawMessage(
							`{"schema":"platform.tweet_search.v1","items":[{"tweet_id":"42"}]}`,
						),
					},
					{
						ActionID: "different-action", Name: "hybrid_search_tweets",
						StructuredContent: json.RawMessage(
							`{"schema":"wrong.v1","items":[{"tweet_id":"42"}]}`,
						),
					},
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("evidence = %+v, want none", items)
	}
}

func platformSearchTask() agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID:   "platform-search",
		Goal: "return grounded platform search results",
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          PlatformSearchResultCriterion,
			Description: "at least one trusted platform search result was observed",
			Required:    true,
		}},
		AllowedTools:      []string{"hybrid_search_tweets"},
		MaxRepairAttempts: 1,
	}
}
