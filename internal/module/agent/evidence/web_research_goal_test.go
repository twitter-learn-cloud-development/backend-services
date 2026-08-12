package evidence

import (
	"context"
	"encoding/json"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestWebResearchGoalVerifiesSearchToPageEvidenceChain(t *testing.T) {
	t.Parallel()
	task := webResearchTask()
	run := webResearchRun("https://go.dev/doc/devel/release?stable=1#top", true)
	collector := WebResearchGoalCollector{}
	items, err := collector.Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: run,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(items) != 2 || items[0].Reference != "https://go.dev/doc/devel/release?stable=1" ||
		items[1].Reference != items[0].Reference {
		t.Fatalf("evidence = %+v", items)
	}
	verification := verifyWebResearch(t, task, run, items)
	if !verification.Passed() || len(verification.Checks) != 2 {
		t.Fatalf("verification = %+v", verification)
	}
}

func TestWebResearchGoalBlocksSearchWithoutPageRead(t *testing.T) {
	t.Parallel()
	task := webResearchTask()
	run := webResearchRun("", false)
	items, err := (WebResearchGoalCollector{}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: run},
	)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(items) != 1 || items[0].Source != webSearchTool {
		t.Fatalf("evidence = %+v", items)
	}
	verification := verifyWebResearch(t, task, run, items)
	if verification.Passed() || len(verification.MissingEvidence) != 1 ||
		verification.MissingEvidence[0] != WebPageContentCriterion {
		t.Fatalf("verification = %+v", verification)
	}
}

func TestWebResearchGoalRejectsPageOutsideSearchResults(t *testing.T) {
	t.Parallel()
	task := webResearchTask()
	run := webResearchRun("https://example.com/forged", true)
	items, err := (WebResearchGoalCollector{}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: run},
	)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(items) != 2 || items[0].Source != webSearchTool || items[1].Source != webPageTool {
		t.Fatalf("forged page evidence = %+v", items)
	}
	verification := verifyWebResearch(t, task, run, items)
	if verification.Passed() {
		t.Fatalf("forged page verification = %+v", verification)
	}
	assertWebResearchCheck(
		t, verification, WebPageContentCriterion, WebPageReferenceInvalidCode, 1,
	)
}

func TestWebResearchGoalRejectsForgedLedgerReference(t *testing.T) {
	t.Parallel()
	task := webResearchTask()
	run := webResearchRun("https://go.dev/doc/devel/release?stable=1", true)
	items, err := (WebResearchGoalCollector{}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: run},
	)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	items[1].Reference = "https://example.com/forged"
	if verification := verifyWebResearch(t, task, run, items); verification.Passed() {
		t.Fatalf("forged ledger verification = %+v", verification)
	}
}

func verifyWebResearch(
	t *testing.T,
	task agentRuntime.TaskSpec,
	run agentRuntime.RunResult,
	items []agentRuntime.Evidence,
) agentRuntime.VerificationResult {
	t.Helper()
	ledger := agentRuntime.EvidenceLedger{}
	var err error
	for _, item := range items {
		ledger, err = ledger.With(item)
		if err != nil {
			t.Fatalf("ledger.With() error = %v", err)
		}
	}
	verification, err := (WebResearchGoalVerifier{}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{Task: task, Run: run, Evidence: ledger},
	)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	return verification
}

func webResearchTask() agentRuntime.TaskSpec {
	return agentRuntime.TaskSpec{
		ID: "e2e-07", Goal: "research the public web with a resolvable citation",
		AllowedTools: []string{webSearchTool, webPageTool},
		CompletionCriteria: []agentRuntime.CompletionCriterion{
			{ID: WebSearchSourcesCriterion, Description: "A configured provider returned public sources.", Required: true},
			{ID: WebPageContentCriterion, Description: "A discovered source page was read.", Required: true},
		},
	}
}

func webResearchRun(pageURL string, includePage bool) agentRuntime.RunResult {
	steps := []agentRuntime.Step{{
		Index: 1,
		Actions: []agentRuntime.Action{{
			ID: "search-1", Type: agentRuntime.ActionToolCall, Name: webSearchTool,
			Arguments: json.RawMessage(`{"query":"Go release","count":3}`),
		}},
		Observations: []agentRuntime.Observation{{
			ActionID: "search-1", Name: webSearchTool,
			StructuredContent: json.RawMessage(`{
				"schema":"web.search.v1","provider":"brave","query":"Go release",
				"items":[{"rank":1,"url":"https://go.dev/doc/devel/release?stable=1#top","title":"Go releases","snippet":"Official history"}]
			}`),
		}},
	}}
	if includePage {
		steps = append(steps, agentRuntime.Step{
			Index: 2,
			Actions: []agentRuntime.Action{{
				ID: "page-1", Type: agentRuntime.ActionToolCall, Name: webPageTool,
				Arguments: json.RawMessage(`{"url":"` + pageURL + `"}`),
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "page-1", Name: webPageTool,
				StructuredContent: json.RawMessage(`{
					"schema":"web.page.v1","url":"` + pageURL + `","title":"Official Go releases",
					"content_type":"text/html","content":"Verified bounded page content",
					"excerpt":"Verified page excerpt","truncated":false,"safety":{}
				}`),
			}},
		})
	}
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-e2e-07", UserID: 42},
		Status:  agentRuntime.RunStatusCompleted, FinalAnswer: "Grounded answer with citation.", Steps: steps,
	}
}
