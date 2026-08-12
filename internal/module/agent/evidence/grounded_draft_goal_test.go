package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestGroundedDraftGoalVerifierAcceptsPlatformAndWebReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source GroundedDraftSource
		run    agentRuntime.RunResult
	}{
		{
			name:   "platform",
			source: GroundedDraftSourcePlatform,
			run: groundedDraftPlatformRun(
				"Go 的并发模型适合组织云原生任务。 [/tweets/2084827196752420864]",
			),
		},
		{
			name:   "web",
			source: GroundedDraftSourceWeb,
			run: groundedDraftWebRun(
				"Go 官方发布页持续记录版本演进。 [https://go.dev/doc/devel/release]",
			),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ledger, verification := collectAndVerifyGroundedDraft(t, test.source, test.run)
			if !verification.Passed() || len(ledger.Items) != 2 {
				t.Fatalf("verification/ledger = %+v/%+v", verification, ledger)
			}
			assertGroundedDraftCheck(
				t, verification, GroundedDraftSourcesCriterion,
				GroundedDraftSourcesVerifiedCode, 1,
			)
			assertGroundedDraftCheck(
				t, verification, GroundedDraftArtifactCriterion,
				GroundedDraftArtifactVerifiedCode, 2,
			)
			if strings.Contains(fmt.Sprint(ledger), test.run.FinalAnswer) {
				t.Fatalf("ledger leaked draft body: %+v", ledger)
			}
		})
	}
}

func TestGroundedDraftGoalVerifierRejectsMissingForgedAndUnlinkedReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		answer   string
		wantCode string
		wantIDs  int
	}{
		{
			name:     "missing",
			answer:   "Go 的并发模型适合组织云原生任务。",
			wantCode: GroundedDraftCitationMissingCode,
			wantIDs:  1,
		},
		{
			name:     "forged",
			answer:   "Go 的并发模型适合组织云原生任务。 [/tweets/2084827196752420865]",
			wantCode: GroundedDraftCitationInvalidCode,
			wantIDs:  1,
		},
		{
			name:     "unlinked",
			answer:   "[/tweets/2084827196752420864]\n\nGo 的并发模型适合组织云原生任务。",
			wantCode: GroundedDraftCitationInvalidCode,
			wantIDs:  1,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, verification := collectAndVerifyGroundedDraft(
				t, GroundedDraftSourcePlatform, groundedDraftPlatformRun(test.answer),
			)
			if verification.Passed() {
				t.Fatalf("verification unexpectedly passed: %+v", verification)
			}
			assertGroundedDraftCheck(
				t, verification, GroundedDraftArtifactCriterion, test.wantCode, test.wantIDs,
			)
		})
	}
}

func TestGroundedDraftGoalVerifierRejectsCrossSourceReference(t *testing.T) {
	t.Parallel()
	run := groundedDraftWebRun(
		"这段草稿伪造了站内来源。 [/tweets/2084827196752420864]",
	)
	_, verification := collectAndVerifyGroundedDraft(t, GroundedDraftSourceWeb, run)
	assertGroundedDraftCheck(
		t, verification, GroundedDraftArtifactCriterion,
		GroundedDraftCitationInvalidCode, 1,
	)
}

func collectAndVerifyGroundedDraft(
	t *testing.T,
	source GroundedDraftSource,
	run agentRuntime.RunResult,
) (agentRuntime.EvidenceLedger, agentRuntime.VerificationResult) {
	t.Helper()
	task := groundedDraftTask(source)
	items, err := (GroundedDraftGoalCollector{Source: source}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: run, Attempt: 0},
	)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	ledger := agentRuntime.EvidenceLedger{}
	for _, item := range items {
		ledger, err = ledger.With(item)
		if err != nil {
			t.Fatalf("ledger.With() error = %v", err)
		}
	}
	verification, err := (GroundedDraftGoalVerifier{Source: source}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{Task: task, Run: run, Evidence: ledger},
	)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	return ledger, verification
}

func groundedDraftTask(source GroundedDraftSource) agentRuntime.TaskSpec {
	allowedTools := []string{"hybrid_search_tweets"}
	if source == GroundedDraftSourceWeb {
		allowedTools = []string{"web_search", "page_read"}
	}
	return agentRuntime.TaskSpec{
		ID: "E2E-09", Goal: "produce a grounded content draft",
		AllowedTools: allowedTools,
		CompletionCriteria: []agentRuntime.CompletionCriterion{
			{ID: GroundedDraftSourcesCriterion, Description: "Trusted source evidence is observed.", Required: true},
			{ID: GroundedDraftArtifactCriterion, Description: "A draft artifact links claims to source evidence.", Required: true},
		},
	}
}

func groundedDraftPlatformRun(answer string) agentRuntime.RunResult {
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-grounded-platform", UserID: 42},
		Status:  agentRuntime.RunStatusCompleted, FinalAnswer: answer,
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets", Content: "platform results",
				StructuredContent: groundedDraftJSON(agentRuntimePlatformSearchResult()),
			}},
		}},
	}
}

func groundedDraftWebRun(answer string) agentRuntime.RunResult {
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-grounded-web", UserID: 42},
		Status:  agentRuntime.RunStatusCompleted, FinalAnswer: answer,
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "web_search",
				Arguments: json.RawMessage(`{"query":"Go release","count":3}`),
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "web_search", Content: "web results",
				StructuredContent: groundedDraftJSON(WebSearchResult{
					Schema: WebSearchSchema, Provider: "brave", Query: "Go release",
					Items: []WebSearchEvidence{{
						Rank: 1, URL: "https://go.dev/doc/devel/release",
						Title: "Go release history", Snippet: "Official release history",
					}},
				}),
			}},
		}},
	}
}

func agentRuntimePlatformSearchResult() PlatformTweetSearchResult {
	return PlatformTweetSearchResult{
		Schema: PlatformTweetSearchSchema, Query: "cloud native Go",
		Items: []PlatformTweetSearchEvidence{{
			TweetID: "2084827196752420864",
			Content: "Go concurrency can organize cloud-native workloads.",
		}},
	}
}

func groundedDraftJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func assertGroundedDraftCheck(
	t *testing.T,
	verification agentRuntime.VerificationResult,
	criterionID string,
	wantCode string,
	wantEvidence int,
) {
	t.Helper()
	for _, check := range verification.Checks {
		if check.CriterionID != criterionID {
			continue
		}
		if check.Code != wantCode || len(check.EvidenceIDs) != wantEvidence {
			t.Fatalf("check = %+v, want code %q and %d evidence IDs", check, wantCode, wantEvidence)
		}
		return
	}
	t.Fatalf("criterion %q missing from %+v", criterionID, verification.Checks)
}
