package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestE2E08EmptyWebSearchBlocksPendingClaimWithSingleExecution(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: webMissingEvidenceRun("empty_search")}
	observer := &goalRuntimeShadowObserverFake{}
	service := newWebMissingEvidenceService(t, repo, runner, observer)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "research the latest Go release",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if !errors.Is(err, ErrRequiredCapabilityEvidence) {
		t.Fatalf("RunAgent() error = %v, want ErrRequiredCapabilityEvidence", err)
	}
	if runner.calls != 1 || len(observer.observations) != 1 || len(repo.saved) != 0 {
		t.Fatalf("calls/observations/saved = %d/%d/%d", runner.calls, len(observer.observations), len(repo.saved))
	}
	observation := observer.observations[0]
	assertBlockedWebShadowCheck(
		t, observation, agentEvidence.WebSearchSourcesCriterion,
		agentEvidence.WebSearchEmptyResultCode, 1,
	)
	assertBlockedWebShadowCheck(
		t, observation, agentEvidence.WebPageContentCriterion,
		agentEvidence.WebPageBlockedBySearchCode, 0,
	)
	if strings.Contains(fmt.Sprint(observation.TaskOutcome), "稍后返回") {
		t.Fatalf("blocked outcome leaked pending claim: %+v", observation.TaskOutcome)
	}
}

func TestE2E08ProviderErrorProducesBlockedDiagnosticWithoutRawError(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{
		result: webMissingEvidenceRun("provider_error"),
		err: &agentRuntime.RunError{
			Code: agentRuntime.ErrorTool, Message: "provider credential=secret unavailable",
		},
	}
	observer := &goalRuntimeShadowObserverFake{}
	service := newWebMissingEvidenceService(t, repo, runner, observer)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "research Go",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if err == nil || runner.calls != 1 || len(observer.observations) != 1 || len(repo.saved) != 0 {
		t.Fatalf("error/calls/observations/saved = %v/%d/%d/%d", err, runner.calls, len(observer.observations), len(repo.saved))
	}
	observation := observer.observations[0]
	if observation.LegacyOutcome != GoalShadowLegacyFailed ||
		observation.EvidenceComparison != GoalShadowComparisonMissingBoth {
		t.Fatalf("observation = %+v", observation)
	}
	assertBlockedWebShadowCheck(
		t, observation, agentEvidence.WebSearchSourcesCriterion,
		agentEvidence.WebSearchProviderErrorCode, 1,
	)
	if strings.Contains(fmt.Sprint(observation.TaskOutcome), "credential=secret") {
		t.Fatalf("blocked outcome leaked provider error: %+v", observation.TaskOutcome)
	}
}

func TestE2E08InvalidWebCitationFailsLegacyCompletionGate(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: webMissingEvidenceRun("invalid_reference")}
	observer := &goalRuntimeShadowObserverFake{}
	service := newWebMissingEvidenceService(t, repo, runner, observer)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "research Go",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if !errors.Is(err, ErrRequiredCapabilityEvidence) || runner.calls != 1 || len(repo.saved) != 0 {
		t.Fatalf("error/calls/saved = %v/%d/%d", err, runner.calls, len(repo.saved))
	}
	assertBlockedWebShadowCheck(
		t, observer.observations[0], agentEvidence.WebSearchSourcesCriterion,
		agentEvidence.WebSearchReferenceInvalidCode, 1,
	)
}

func TestE2E08PageErrorPreservesSearchEvidenceAndBlocksCompletion(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{
		result: webMissingEvidenceRun("page_error"),
		err: &agentRuntime.RunError{
			Code: agentRuntime.ErrorTool, Message: "page reader credential=secret failed",
		},
	}
	observer := &goalRuntimeShadowObserverFake{}
	service := newWebMissingEvidenceService(t, repo, runner, observer)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "research Go",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if err == nil || runner.calls != 1 || len(observer.observations) != 1 || len(repo.saved) != 0 {
		t.Fatalf("error/calls/observations/saved = %v/%d/%d/%d", err, runner.calls, len(observer.observations), len(repo.saved))
	}
	observation := observer.observations[0]
	assertBlockedWebShadowCheck(
		t, observation, agentEvidence.WebSearchSourcesCriterion,
		"web_search_source_verified", 1,
	)
	assertBlockedWebShadowCheck(
		t, observation, agentEvidence.WebPageContentCriterion,
		agentEvidence.WebPageReadErrorCode, 1,
	)
}

func newWebMissingEvidenceService(
	t *testing.T,
	repo *assistRuntimeRepository,
	runner *capturingRuntimeRunner,
	observer *goalRuntimeShadowObserverFake,
) *AgentService {
	t.Helper()
	catalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWebSearchCapability())
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	return NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithRuntimeToolCatalog(staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
			{Name: "web_search", Category: agentRuntime.ToolCategoryRead},
			{Name: "page_read", Category: agentRuntime.ToolCategoryRead},
		}}),
		WithGoalRuntimeShadow(GoalRuntimeShadowConfig{
			Enabled: true, WebResearchEnabled: true,
		}, observer),
	)
}

func assertBlockedWebShadowCheck(
	t *testing.T,
	observation GoalRuntimeShadowObservation,
	criterionID string,
	code string,
	evidenceCount int,
) {
	t.Helper()
	if observation.GoalOutcome != agentRuntime.VerificationFailed ||
		observation.TaskOutcome == nil ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunBlocked ||
		observation.TaskOutcome.ExecutionSource != agentRuntime.TaskOutcomeExecutionObserved {
		t.Fatalf("observation = %+v", observation)
	}
	for _, check := range observation.TaskOutcome.Verification.Checks {
		if check.CriterionID != criterionID {
			continue
		}
		if check.Code != code || len(check.EvidenceIDs) != evidenceCount {
			t.Fatalf("check = %+v, want code %q and %d evidence IDs", check, code, evidenceCount)
		}
		return
	}
	t.Fatalf("criterion %q not found in %+v", criterionID, observation.TaskOutcome.Verification.Checks)
}

func webMissingEvidenceRun(kind string) agentRuntime.RunResult {
	searchResult := agentEvidence.WebSearchResult{
		Schema: agentEvidence.WebSearchSchema, Provider: "brave", Query: "Go release",
	}
	if kind == "invalid_reference" {
		searchResult.Items = []agentEvidence.WebSearchEvidence{{
			Rank: 1, URL: "http://127.0.0.1/private", Title: "private source",
		}}
	}
	searchObservation := agentRuntime.Observation{
		ActionID: "search-1", Name: "web_search", Content: "search results",
		StructuredContent: mustJSONRaw(searchResult),
	}
	status := agentRuntime.RunStatusCompleted
	if kind == "provider_error" {
		status = agentRuntime.RunStatusFailed
		searchObservation.IsError = true
		searchObservation.Content = "provider credential=secret failed"
		searchObservation.StructuredContent = nil
	}
	if kind == "page_error" {
		status = agentRuntime.RunStatusFailed
		searchResult.Items = []agentEvidence.WebSearchEvidence{{
			Rank: 1, URL: "https://go.dev/doc/devel/release", Title: "Go releases",
		}}
		searchObservation.StructuredContent = mustJSONRaw(searchResult)
	}
	steps := []agentRuntime.Step{{
		Index: 1,
		Actions: []agentRuntime.Action{{
			ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "web_search",
			Arguments: json.RawMessage(`{"query":"Go release","count":3}`),
		}},
		Observations: []agentRuntime.Observation{searchObservation},
	}}
	if kind == "page_error" {
		steps = append(steps, agentRuntime.Step{
			Index: 2,
			Actions: []agentRuntime.Action{{
				ID: "page-1", Type: agentRuntime.ActionToolCall, Name: "page_read",
				Arguments: json.RawMessage(`{"url":"https://go.dev/doc/devel/release"}`),
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "page-1", Name: "page_read", IsError: true,
				Content: "page reader credential=secret failed",
			}},
		})
	}
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-web-missing", UserID: 42},
		Status:  status, FinalAnswer: "正在获取资料，稍后返回完整结果。", Steps: steps,
	}
}
