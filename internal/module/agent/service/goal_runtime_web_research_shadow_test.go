package service

import (
	"context"
	"encoding/json"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestEvaluateWebResearchGoalShadowVerifiesBoundSearchAndPage(t *testing.T) {
	t.Parallel()
	observation := evaluateWebResearchGoalShadow(
		context.Background(), "research Go releases", webResearchShadowRun(true, "https://go.dev/doc/devel/release"), nil,
	)
	if observation.LegacyOutcome != GoalShadowLegacyCompleted ||
		observation.GoalOutcome != agentRuntime.VerificationPassed ||
		observation.EvidenceComparison != GoalShadowComparisonConsistent ||
		observation.TaskOutcome == nil ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunVerified ||
		len(observation.TaskOutcome.Evidence.Items) != 2 {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestEvaluateWebResearchGoalShadowBlocksSearchWithoutPage(t *testing.T) {
	t.Parallel()
	observation := evaluateWebResearchGoalShadow(
		context.Background(), "research Go releases", webResearchShadowRun(false, ""), nil,
	)
	if observation.LegacyOutcome != GoalShadowLegacyCompleted ||
		observation.GoalOutcome != agentRuntime.VerificationFailed ||
		observation.EvidenceComparison != GoalShadowComparisonLegacyOnly ||
		observation.TaskOutcome == nil ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunBlocked {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestEvaluateWebResearchGoalShadowRejectsForgedPageURL(t *testing.T) {
	t.Parallel()
	observation := evaluateWebResearchGoalShadow(
		context.Background(), "research Go releases", webResearchShadowRun(true, "https://example.com/forged"), nil,
	)
	if observation.GoalOutcome != agentRuntime.VerificationFailed ||
		observation.EvidenceComparison != GoalShadowComparisonLegacyOnly {
		t.Fatalf("observation = %+v", observation)
	}
}

func TestObserveWebResearchGoalShadowHonorsDedicatedFlag(t *testing.T) {
	t.Parallel()
	observer := &goalRuntimeShadowObserverFake{}
	service := &AgentService{goalRuntimeShadowObserver: observer}
	run := webResearchShadowRun(true, "https://go.dev/doc/devel/release")

	service.goalRuntimeShadow = GoalRuntimeShadowConfig{Enabled: true, PlatformSearchEnabled: true}
	service.observeWebResearchGoalShadow(context.Background(), "research Go", run, nil)
	if len(observer.observations) != 0 {
		t.Fatalf("disabled web shadow emitted %d observations", len(observer.observations))
	}
	service.goalRuntimeShadow.WebResearchEnabled = true
	service.observeWebResearchGoalShadow(context.Background(), "research Go", run, nil)
	if len(observer.observations) != 1 {
		t.Fatalf("enabled web shadow emitted %d observations", len(observer.observations))
	}
}

func webResearchShadowRun(includePage bool, pageURL string) agentRuntime.RunResult {
	steps := []agentRuntime.Step{{
		Index: 1,
		Actions: []agentRuntime.Action{{
			ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "web_search",
			Arguments: json.RawMessage(`{"query":"Go release","count":3}`),
		}},
		Observations: []agentRuntime.Observation{{
			ActionID: "search-1", Name: "web_search", Content: "search results",
			StructuredContent: mustJSONRaw(agentEvidence.WebSearchResult{
				Schema: agentEvidence.WebSearchSchema, Provider: "brave", Query: "Go release",
				Items: []agentEvidence.WebSearchEvidence{{
					Rank: 1, URL: "https://go.dev/doc/devel/release", Title: "Go releases", Snippet: "Official history",
				}},
			}),
		}},
	}}
	if includePage {
		steps = append(steps, agentRuntime.Step{
			Index: 2,
			Actions: []agentRuntime.Action{{
				ID: "page-1", Type: agentRuntime.ActionToolCall, Name: "page_read",
				Arguments: json.RawMessage(`{"url":"` + pageURL + `"}`),
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "page-1", Name: "page_read", Content: "page content",
				StructuredContent: mustJSONRaw(agentEvidence.WebPageResult{
					Schema: agentEvidence.WebPageSchema, URL: pageURL, Title: "Official Go releases",
					ContentType: "text/html", Content: "Verified page content", Excerpt: "Verified excerpt",
				}),
			}},
		})
	}
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-web-shadow", UserID: 42},
		Status:  agentRuntime.RunStatusCompleted, FinalAnswer: "Grounded web answer", Steps: steps,
	}
}

func mustJSONRaw(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
