package evidence

import (
	"context"
	"encoding/json"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestResearchThenDraftGoalVerifierRequiresResearchBeforeFinalDraft(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		run      agentRuntime.RunResult
		wantPass bool
		wantCode string
	}{
		{
			name:     "research precedes draft",
			run:      researchThenDraftPlatformRun(1, 2),
			wantPass: true,
			wantCode: ResearchThenDraftOrderVerifiedCode,
		},
		{
			name:     "research follows draft",
			run:      researchThenDraftPlatformRun(2, 1),
			wantPass: false,
			wantCode: ResearchThenDraftOrderMissingCode,
		},
		{
			name: "top-level answer without terminal action",
			run: func() agentRuntime.RunResult {
				run := researchThenDraftPlatformRun(1, 2)
				run.Steps = run.Steps[:1]
				return run
			}(),
			wantPass: false,
			wantCode: ResearchThenDraftOrderMissingCode,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ledger, verification := collectAndVerifyResearchThenDraft(t, test.run)
			if verification.Passed() != test.wantPass {
				t.Fatalf("verification = %+v", verification)
			}
			if got := researchThenDraftCheckCode(verification, ResearchThenDraftOrderCriterion); got != test.wantCode {
				t.Fatalf("order code = %q, want %q; ledger = %+v", got, test.wantCode, ledger)
			}
		})
	}
}

func TestResearchThenDraftGoalVerifierRejectsInjectedOrderEvidence(t *testing.T) {
	t.Parallel()
	run := researchThenDraftPlatformRun(2, 1)
	task := researchThenDraftPlatformTask()
	items, err := (ResearchThenDraftGoalCollector{Source: GroundedDraftSourcePlatform}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: run},
	)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for index := range items {
		if items[index].Kind == agentRuntime.EvidenceToolObservation {
			items[index].CriterionIDs = append(items[index].CriterionIDs, ResearchThenDraftOrderCriterion)
		}
	}
	ledger := researchThenDraftLedger(t, items)
	verification, err := (ResearchThenDraftGoalVerifier{Source: GroundedDraftSourcePlatform}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{Task: task, Run: run, Evidence: ledger},
	)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verification.Passed() ||
		researchThenDraftCheckCode(verification, ResearchThenDraftOrderCriterion) != ResearchThenDraftOrderMissingCode {
		t.Fatalf("verification = %+v", verification)
	}
}

func collectAndVerifyResearchThenDraft(
	t *testing.T,
	run agentRuntime.RunResult,
) (agentRuntime.EvidenceLedger, agentRuntime.VerificationResult) {
	t.Helper()
	task := researchThenDraftPlatformTask()
	items, err := (ResearchThenDraftGoalCollector{Source: GroundedDraftSourcePlatform}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: run},
	)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	ledger := researchThenDraftLedger(t, items)
	verification, err := (ResearchThenDraftGoalVerifier{Source: GroundedDraftSourcePlatform}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{Task: task, Run: run, Evidence: ledger},
	)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	return ledger, verification
}

func researchThenDraftPlatformTask() agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID:           "E2E-11",
		Goal:         "research platform evidence before producing a grounded draft",
		AllowedTools: []string{"hybrid_search_tweets"},
		CompletionCriteria: []agentRuntime.CompletionCriterion{
			{ID: GroundedDraftSourcesCriterion, Description: "trusted sources observed", Required: true},
			{ID: ResearchThenDraftOrderCriterion, Description: "research preceded draft", Required: true},
			{ID: GroundedDraftArtifactCriterion, Description: "grounded draft produced", Required: true},
		},
	}
}

func researchThenDraftPlatformRun(researchStep, finalStep int) agentRuntime.RunResult {
	answer := "Go concurrency can organize cloud-native workloads. [/tweets/2084827196752420864]"
	steps := []agentRuntime.Step{
		{
			Index: researchStep,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets", Content: "platform results",
				StructuredContent: mustResearchThenDraftJSON(PlatformTweetSearchResult{
					Schema: PlatformTweetSearchSchema, Query: "cloud native Go",
					Items: []PlatformTweetSearchEvidence{{
						TweetID: "2084827196752420864",
						Content: "Go concurrency can organize cloud-native workloads.",
					}},
				}),
			}},
		},
		{
			Index: finalStep,
			Actions: []agentRuntime.Action{{
				ID: "final-1", Type: agentRuntime.ActionFinalAnswer, Content: answer,
			}},
		},
	}
	if researchStep > finalStep {
		steps[0], steps[1] = steps[1], steps[0]
	}
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "e2e-11-run"},
		Status:  agentRuntime.RunStatusCompleted, FinalAnswer: answer, Steps: steps,
	}
}

func researchThenDraftLedger(t *testing.T, items []agentRuntime.Evidence) agentRuntime.EvidenceLedger {
	t.Helper()
	ledger := agentRuntime.EvidenceLedger{}
	for _, item := range items {
		var err error
		ledger, err = ledger.With(item)
		if err != nil {
			t.Fatalf("ledger.With() error = %v", err)
		}
	}
	return ledger
}

func researchThenDraftCheckCode(result agentRuntime.VerificationResult, criterionID string) string {
	for _, check := range result.Checks {
		if check.CriterionID == criterionID {
			return check.Code
		}
	}
	return ""
}

func mustResearchThenDraftJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
