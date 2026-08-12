package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestWebResearchGoalClassifiesEmptySearchResult(t *testing.T) {
	t.Parallel()
	run := webResearchMissingSearchRun(false, false)
	items := collectWebResearchEvidence(t, run)
	if len(items) != 1 || items[0].Source != webSearchTool || items[0].Reference != "" ||
		!strings.HasPrefix(items[0].Digest, "sha256:") {
		t.Fatalf("diagnostic evidence = %+v", items)
	}
	verification := verifyWebResearch(t, webResearchTask(), run, items)
	assertWebResearchCheck(t, verification, WebSearchSourcesCriterion, WebSearchEmptyResultCode, 1)
	assertWebResearchCheck(t, verification, WebPageContentCriterion, WebPageBlockedBySearchCode, 0)
}

func TestWebResearchGoalClassifiesProviderErrorWithoutLeakingText(t *testing.T) {
	t.Parallel()
	run := webResearchMissingSearchRun(true, false)
	items := collectWebResearchEvidence(t, run)
	if strings.Contains(fmt.Sprint(items), "credential=secret") {
		t.Fatalf("diagnostic leaked provider error: %+v", items)
	}
	verification := verifyWebResearch(t, webResearchTask(), run, items)
	assertWebResearchCheck(t, verification, WebSearchSourcesCriterion, WebSearchProviderErrorCode, 1)
	assertWebResearchCheck(t, verification, WebPageContentCriterion, WebPageBlockedBySearchCode, 0)
}

func TestWebResearchGoalClassifiesInvalidSearchReference(t *testing.T) {
	t.Parallel()
	run := webResearchMissingSearchRun(false, true)
	items := collectWebResearchEvidence(t, run)
	verification := verifyWebResearch(t, webResearchTask(), run, items)
	assertWebResearchCheck(t, verification, WebSearchSourcesCriterion, WebSearchReferenceInvalidCode, 1)
	assertWebResearchCheck(t, verification, WebPageContentCriterion, WebPageBlockedBySearchCode, 0)
}

func TestWebResearchGoalClassifiesPageReadError(t *testing.T) {
	t.Parallel()
	run := webResearchRun("", false)
	run.Status = agentRuntime.RunStatusFailed
	run.Steps = append(run.Steps, agentRuntime.Step{
		Index: 2,
		Actions: []agentRuntime.Action{{
			ID: "page-1", Type: agentRuntime.ActionToolCall, Name: webPageTool,
			Arguments: json.RawMessage(`{"url":"https://go.dev/doc/devel/release?stable=1"}`),
		}},
		Observations: []agentRuntime.Observation{{
			ActionID: "page-1", Name: webPageTool, IsError: true,
			Content: "upstream page error credential=secret",
		}},
	})
	items := collectWebResearchEvidence(t, run)
	if len(items) != 2 || strings.Contains(fmt.Sprint(items), "credential=secret") {
		t.Fatalf("evidence = %+v", items)
	}
	verification := verifyWebResearch(t, webResearchTask(), run, items)
	assertWebResearchCheck(t, verification, WebSearchSourcesCriterion, "web_search_source_verified", 1)
	assertWebResearchCheck(t, verification, WebPageContentCriterion, WebPageReadErrorCode, 1)
}

func collectWebResearchEvidence(
	t *testing.T,
	run agentRuntime.RunResult,
) []agentRuntime.Evidence {
	t.Helper()
	items, err := (WebResearchGoalCollector{}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: webResearchTask(), Run: run},
	)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return items
}

func assertWebResearchCheck(
	t *testing.T,
	verification agentRuntime.VerificationResult,
	criterionID string,
	code string,
	evidenceCount int,
) {
	t.Helper()
	if verification.Passed() {
		t.Fatalf("verification unexpectedly passed: %+v", verification)
	}
	for _, check := range verification.Checks {
		if check.CriterionID != criterionID {
			continue
		}
		if check.Code != code || len(check.EvidenceIDs) != evidenceCount {
			t.Fatalf("check = %+v, want code %q and %d evidence IDs", check, code, evidenceCount)
		}
		return
	}
	t.Fatalf("criterion %q not found in %+v", criterionID, verification.Checks)
}

func mustWebResearchJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
func webResearchMissingSearchRun(providerError bool, invalidReference bool) agentRuntime.RunResult {
	result := WebSearchResult{
		Schema: WebSearchSchema, Provider: "brave", Query: "Go release",
		Items: []WebSearchEvidence{},
	}
	if invalidReference {
		result.Items = []WebSearchEvidence{{
			Rank: 1, URL: "http://127.0.0.1/private", Title: "invalid private source",
		}}
	}
	observation := agentRuntime.Observation{
		ActionID: "search-1", Name: webSearchTool,
		StructuredContent: mustWebResearchJSON(result),
	}
	status := agentRuntime.RunStatusCompleted
	if providerError {
		status = agentRuntime.RunStatusFailed
		observation.IsError = true
		observation.Content = "provider request failed credential=secret"
		observation.StructuredContent = nil
	}
	return agentRuntime.RunResult{
		Context:     agentRuntime.RunContext{RunID: "run-web-missing", UserID: 42},
		Status:      status,
		FinalAnswer: "正在获取资料，稍后返回完整结果。",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: webSearchTool,
				Arguments: json.RawMessage(`{"query":"Go release","count":3}`),
			}},
			Observations: []agentRuntime.Observation{observation},
		}},
	}
}
