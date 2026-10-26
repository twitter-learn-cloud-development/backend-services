package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type serviceShortPlanPlanner struct {
	requests []agentRuntime.ShortPlanRequest
	result   agentRuntime.ShortPlanResult
	err      error
}

func (planner *serviceShortPlanPlanner) Plan(
	_ context.Context,
	request agentRuntime.ShortPlanRequest,
) (agentRuntime.ShortPlanResult, error) {
	planner.requests = append(planner.requests, request)
	return planner.result, planner.err
}

func TestPlanningExecuteConfigAndCanaryAreFailClosedAndStable(t *testing.T) {
	invalid := PlanningExecuteConfig{Enabled: true, CanaryPercent: 101, MaxOutputTokens: 1}
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() allowed an out-of-range canary")
	}

	service := &AgentService{
		planningExecute: PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 37, PlatformSearchEnabled: true,
		},
		planningCoordinator: new(agentRuntime.PlanningCoordinator),
	}
	first := planningCanaryBucket(42, ExecutionProfileRuntimePlatformSearch)
	second := planningCanaryBucket(42, ExecutionProfileRuntimePlatformSearch)
	if first != second || first < 0 || first >= planningCanaryBuckets {
		t.Fatalf("stable bucket = %d/%d", first, second)
	}
	want := first < service.planningExecute.CanaryPercent
	if got := service.planningExecuteEnabled(42, ExecutionProfileRuntimePlatformSearch); got != want {
		t.Fatalf("planningExecuteEnabled() = %t, want %t", got, want)
	}
	if service.planningExecuteEnabled(42, ExecutionProfileRuntimeWebSearch) {
		t.Fatal("web search entered planning without its dedicated flag")
	}
	service.planningExecute.WebResearchEnabled = true
	webWant := planningCanaryBucket(42, ExecutionProfileRuntimeWebSearch) <
		service.planningExecute.CanaryPercent
	if got := service.planningExecuteEnabled(42, ExecutionProfileRuntimeWebSearch); got != webWant {
		t.Fatalf("web planningExecuteEnabled() = %t, want %t", got, webWant)
	}
	if service.planningExecuteEnabled(42, ExecutionProfileRuntimeWebDraft) {
		t.Fatal("web draft entered the read-only web research canary")
	}
	service.planningExecute.CanaryPercent = 0
	if service.planningExecuteEnabled(42, ExecutionProfileRuntimePlatformSearch) {
		t.Fatal("zero-percent canary enabled planning")
	}
}

func TestRunAgentPlannedWebResearchRequiresBoundSearchAndPageEvidence(t *testing.T) {
	planner, coordinator := newWebResearchPlanningCoordinator(t)
	runner := &capturingRuntimeRunner{result: webResearchShadowRun(
		true, "https://go.dev/doc/devel/release",
	)}
	catalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWebSearchCapability())
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithRuntimeToolCatalog(webResearchRuntimeCatalog()),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, WebResearchEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "research the latest Go release",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Response != "Grounded web answer" || len(result.Citations) != 1 ||
		result.Citations[0].URL != "https://go.dev/doc/devel/release" {
		t.Fatalf("result = %+v", result)
	}
	if len(planner.requests) != 1 || runner.calls != 1 ||
		runner.request.InitialToolChoice != agentRuntime.ToolChoiceRequired ||
		len(runner.request.Tools) != 2 || runner.request.Context.StrategyPlanDigest == "" {
		t.Fatalf("planner/runtime request = %d/%+v", len(planner.requests), runner.request)
	}
}

func TestRunAgentPlannedWebResearchBlocksSearchWithoutPageEvidence(t *testing.T) {
	_, coordinator := newWebResearchPlanningCoordinator(t)
	runner := &capturingRuntimeRunner{result: webResearchShadowRun(false, "")}
	catalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWebSearchCapability())
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithRuntimeToolCatalog(webResearchRuntimeCatalog()),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, WebResearchEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	_, err = service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "research the latest Go release",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if err == nil || !strings.Contains(err.Error(), "admitted tool step did not produce a successful observation") {
		t.Fatalf("RunAgent() error = %v", err)
	}
}

func TestRunAgentWebPlanningFlagOffPreservesLegacySearchPath(t *testing.T) {
	planner, coordinator := newWebResearchPlanningCoordinator(t)
	runner := &capturingRuntimeRunner{result: webResearchShadowRun(false, "")}
	catalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWebSearchCapability())
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithRuntimeToolCatalog(webResearchRuntimeCatalog()),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, WebResearchEnabled: false,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "search the web for the latest Go release",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() legacy fallback error = %v", err)
	}
	if result.Response != "Grounded web answer" || len(planner.requests) != 0 ||
		runner.calls != 1 || runner.request.Context.StrategyPlanDigest != "" {
		t.Fatalf("legacy fallback result/planner/runtime = %+v/%d/%+v", result, len(planner.requests), runner.request)
	}
}

func TestRunAgentPlanningCanaryZeroPreservesCurrentRuntimePath(t *testing.T) {
	planner, coordinator := newPlatformSearchPlanningCoordinator(t)
	runner := newGroundedPlatformSearchRunner()
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(platformSearchRuntimeCatalog()),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 0, PlatformSearchEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	if _, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "search platform posts about cloud native",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch},
	}); err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if len(planner.requests) != 0 || runner.calls != 1 || runner.request.Context.StrategyPlanDigest != "" {
		t.Fatalf("planner/runtime calls or digest = %d/%d/%q", len(planner.requests), runner.calls, runner.request.Context.StrategyPlanDigest)
	}
}

func TestRunAgentPlannedPlatformSearchPersistsLowSensitivityEvidence(t *testing.T) {
	planner, coordinator := newPlatformSearchPlanningCoordinator(t)
	runner := newGroundedPlatformSearchRunner()
	store := &memoryAgentExecutionRunStore{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(platformSearchRuntimeCatalog()),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, PlatformSearchEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "search platform posts about cloud native",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Response != "one grounded platform result" || len(result.Citations) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(planner.requests) != 1 || runner.calls != 1 {
		t.Fatalf("planner/runtime calls = %d/%d", len(planner.requests), runner.calls)
	}
	if runner.request.InitialToolChoice != agentRuntime.ToolChoiceRequired ||
		len(runner.request.Tools) != 1 || runner.request.Tools[0].Name != "hybrid_search_tweets" ||
		runner.request.Context.StrategyPlanDigest == "" {
		t.Fatalf("planned runtime request = %+v", runner.request)
	}

	store.mu.Lock()
	persisted := *store.run
	store.mu.Unlock()
	if persisted.PlanningPlanDigest != runner.request.Context.StrategyPlanDigest ||
		persisted.PlanningCalls != 1 || persisted.PlanningModelAttempts != 1 ||
		persisted.PlanningAdmissionAttempts != 1 || persisted.PlanningTotalTokens != 5 ||
		persisted.TotalTokens != 12 || persisted.EstimatedCostMicros != 18 ||
		persisted.GoalRunStatus != string(agentRuntime.GoalRunVerified) ||
		persisted.GoalVerificationStatus != string(agentRuntime.VerificationPassed) {
		t.Fatalf("persisted planning evidence = %+v", persisted)
	}
	if persisted.PlanningPlanDigest == result.Response || persisted.FailureDigest != "" {
		t.Fatalf("unsafe or failed persisted state = %+v", persisted)
	}
}

func TestPlanningExecutionErrorOverridesSuspendedRuntimeStatus(t *testing.T) {
	captured := capturedAgentExecution{
		called:   true,
		result:   agentRuntime.RunResult{Status: agentRuntime.RunStatusAwaitingHuman},
		planning: planningExecutionCapture{attempted: true},
	}
	status, code, err := classifyAgentExecutionOutcome(
		context.Background(),
		captured,
		&agentRuntime.RunError{
			Code:    agentRuntime.ErrorUnsupported,
			Message: "planned execution suspension is not enabled",
		},
	)
	if err != nil || status != repository.AgentExecutionRunFailed ||
		code != string(agentRuntime.ErrorUnsupported) {
		t.Fatalf("classified planning suspension = %q/%q/%v", status, code, err)
	}
}

func newPlatformSearchPlanningCoordinator(
	t *testing.T,
) (*serviceShortPlanPlanner, *agentRuntime.PlanningCoordinator) {
	t.Helper()
	planner := &serviceShortPlanPlanner{result: agentRuntime.ShortPlanResult{
		Proposal: agentRuntime.ShortPlanProposal{
			Version: agentRuntime.ShortPlanVersionV1,
			Steps: []agentRuntime.ShortPlanStep{
				{
					ID: "search", Kind: agentRuntime.ShortPlanStepTool,
					Objective: "find trusted first-party results", ToolName: "hybrid_search_tweets",
					CriterionIDs: []string{agentEvidence.PlatformSearchResultCriterion},
				},
				{
					ID: "respond", Kind: agentRuntime.ShortPlanStepRespond,
					Objective:    "answer only from observed results",
					CriterionIDs: []string{agentEvidence.PlatformSearchResultCriterion},
				},
			},
		},
		Usage: agentRuntime.TokenUsage{
			InputTokens: 3, OutputTokens: 2, TotalTokens: 5,
			EstimatedCostMicros: 7, CostEstimated: true,
		},
		Attempts: 1,
		Model:    "default-model",
		Provider: "test-provider",
	}}
	coordinator, err := agentRuntime.NewPlanningCoordinator(
		planner,
		agentRuntime.DeterministicShortPlanPolicy{},
	)
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	return planner, coordinator
}

func newWebResearchPlanningCoordinator(
	t *testing.T,
) (*serviceShortPlanPlanner, *agentRuntime.PlanningCoordinator) {
	t.Helper()
	planner := &serviceShortPlanPlanner{result: agentRuntime.ShortPlanResult{
		Proposal: agentRuntime.ShortPlanProposal{
			Version: agentRuntime.ShortPlanVersionV1,
			Steps: []agentRuntime.ShortPlanStep{
				{
					ID: "search", Kind: agentRuntime.ShortPlanStepTool,
					Objective: "find trusted public sources", ToolName: "web_search",
					CriterionIDs: []string{agentEvidence.WebSearchSourcesCriterion},
				},
				{
					ID: "read", Kind: agentRuntime.ShortPlanStepTool,
					Objective: "read a discovered public source", ToolName: "page_read",
					CriterionIDs: []string{agentEvidence.WebPageContentCriterion},
				},
				{
					ID: "respond", Kind: agentRuntime.ShortPlanStepRespond,
					Objective: "answer only from observed public sources",
					CriterionIDs: []string{
						agentEvidence.WebSearchSourcesCriterion,
						agentEvidence.WebPageContentCriterion,
					},
				},
			},
		},
		Usage: agentRuntime.TokenUsage{
			InputTokens: 4, OutputTokens: 3, TotalTokens: 7,
			EstimatedCostMicros: 9, CostEstimated: true,
		},
		Attempts: 1, Model: "default-model", Provider: "test-provider",
	}}
	coordinator, err := agentRuntime.NewPlanningCoordinator(
		planner, agentRuntime.DeterministicShortPlanPolicy{},
	)
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	return planner, coordinator
}

func webResearchRuntimeCatalog() staticRuntimeToolCatalog {
	return staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{
		{Name: "web_search", Category: agentRuntime.ToolCategoryRead},
		{Name: "page_read", Category: agentRuntime.ToolCategoryRead},
	}}
}

func newGroundedPlatformSearchRunner() *capturingRuntimeRunner {
	return &capturingRuntimeRunner{result: agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "one grounded platform result",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "search-1", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "search-1", Name: "hybrid_search_tweets",
				StructuredContent: json.RawMessage(`{"schema":"platform.tweet_search.v1","items":[{"tweet_id":"2084827196752420864","content":"verified platform evidence"}]}`),
			}},
		}},
		Usage: agentRuntime.TokenUsage{
			InputTokens: 4, OutputTokens: 3, TotalTokens: 7,
			EstimatedCostMicros: 11, CostEstimated: true,
		},
	}}
}

func platformSearchRuntimeCatalog() staticRuntimeToolCatalog {
	return staticRuntimeToolCatalog{tools: []agentRuntime.ToolDefinition{{
		Name: "hybrid_search_tweets", Category: agentRuntime.ToolCategoryRead,
	}}}
}

type plannedSuspendingRuntimeRunner struct {
	runCalls    int
	resumeCalls int
	resume      agentRuntime.ResumeRequest
}

type plannedWebSuspendingRuntimeRunner struct {
	runCalls    int
	resumeCalls int
	resume      agentRuntime.ResumeRequest
}

func (runner *plannedWebSuspendingRuntimeRunner) Run(
	_ context.Context,
	request agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	runner.runCalls++
	action := agentRuntime.Action{
		ID: "question-web-1", Type: agentRuntime.ActionAskHuman,
		Content: "Which Go release topic should be researched?",
	}
	return agentRuntime.RunResult{
		Context: request.Context, Status: agentRuntime.RunStatusAwaitingHuman,
		Messages: []agentRuntime.Message{{
			Role: agentRuntime.RoleAssistant, Content: action.Content,
			Actions: []agentRuntime.Action{action},
		}},
		Steps:         []agentRuntime.Step{{Index: 1, Actions: []agentRuntime.Action{action}}},
		PendingAction: &action,
		Usage:         agentRuntime.TokenUsage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6},
	}, nil
}

func (runner *plannedWebSuspendingRuntimeRunner) Resume(
	_ context.Context,
	request agentRuntime.ResumeRequest,
) (agentRuntime.RunResult, error) {
	runner.resumeCalls++
	runner.resume = request
	fixture := webResearchShadowRun(true, "https://go.dev/doc/devel/release")
	steps := append([]agentRuntime.Step(nil), request.Checkpoint.Steps...)
	for _, step := range fixture.Steps {
		step.Index += len(request.Checkpoint.Steps)
		steps = append(steps, step)
	}
	messages := append([]agentRuntime.Message(nil), request.Checkpoint.Messages...)
	messages = append(messages,
		agentRuntime.Message{Role: agentRuntime.RoleUser, Content: request.HumanResponse},
		agentRuntime.Message{Role: agentRuntime.RoleAssistant, Content: fixture.FinalAnswer},
	)
	usage := request.Checkpoint.Usage
	usage.Add(agentRuntime.TokenUsage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8})
	return agentRuntime.RunResult{
		Context: request.Checkpoint.Context, Status: agentRuntime.RunStatusCompleted,
		FinalAnswer: fixture.FinalAnswer, Messages: messages, Steps: steps, Usage: usage,
	}, nil
}

func (runner *plannedSuspendingRuntimeRunner) Run(
	_ context.Context,
	request agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	runner.runCalls++
	action := agentRuntime.Action{
		ID: "question-1", Type: agentRuntime.ActionAskHuman,
		Content: "Which cloud-native topic should be searched?",
	}
	return agentRuntime.RunResult{
		Context: request.Context, Status: agentRuntime.RunStatusAwaitingHuman,
		Messages: []agentRuntime.Message{{
			Role: agentRuntime.RoleAssistant, Content: action.Content,
			Actions: []agentRuntime.Action{action},
		}},
		Steps:         []agentRuntime.Step{{Index: 1, Actions: []agentRuntime.Action{action}}},
		PendingAction: &action,
		Usage:         agentRuntime.TokenUsage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6},
	}, nil
}

func (runner *plannedSuspendingRuntimeRunner) Resume(
	_ context.Context,
	request agentRuntime.ResumeRequest,
) (agentRuntime.RunResult, error) {
	runner.resumeCalls++
	runner.resume = request
	search := agentRuntime.Action{
		ID: "search-2", Type: agentRuntime.ActionToolCall, Name: "hybrid_search_tweets",
	}
	steps := append([]agentRuntime.Step(nil), request.Checkpoint.Steps...)
	steps = append(steps, agentRuntime.Step{
		Index: 2, Actions: []agentRuntime.Action{search},
		Observations: []agentRuntime.Observation{{
			ActionID: search.ID, Name: search.Name,
			StructuredContent: json.RawMessage(`{"schema":"platform.tweet_search.v1","items":[{"tweet_id":"2084827196752420864","content":"verified cloud native evidence"}]}`),
		}},
	})
	messages := append([]agentRuntime.Message(nil), request.Checkpoint.Messages...)
	messages = append(messages,
		agentRuntime.Message{Role: agentRuntime.RoleUser, Content: request.HumanResponse},
		agentRuntime.Message{Role: agentRuntime.RoleAssistant, Content: "One verified cloud-native result."},
	)
	usage := request.Checkpoint.Usage
	usage.Add(agentRuntime.TokenUsage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8})
	return agentRuntime.RunResult{
		Context: request.Checkpoint.Context, Status: agentRuntime.RunStatusCompleted,
		FinalAnswer: "One verified cloud-native result.", Messages: messages, Steps: steps, Usage: usage,
	}, nil
}

func TestPlannedPlatformSearchPersistsAndResumesVerifiedCheckpoint(t *testing.T) {
	planner := &serviceShortPlanPlanner{result: agentRuntime.ShortPlanResult{
		Proposal: agentRuntime.ShortPlanProposal{
			Version: agentRuntime.ShortPlanVersionV1,
			Steps: []agentRuntime.ShortPlanStep{{
				ID: "clarify", Kind: agentRuntime.ShortPlanStepAskHuman,
				Objective:    "clarify the cloud-native search topic",
				CriterionIDs: []string{agentEvidence.PlatformSearchResultCriterion},
			}},
		},
		Usage:    agentRuntime.TokenUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
		Attempts: 1, Model: "default-model", Provider: "test-provider",
	}}
	coordinator, err := agentRuntime.NewPlanningCoordinator(
		planner, agentRuntime.DeterministicShortPlanPolicy{},
	)
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	runner := &plannedSuspendingRuntimeRunner{}
	store := &memoryAgentExecutionRunStore{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithRuntimeToolCatalog(platformSearchRuntimeCatalog()),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
		WithAgentRunRecovery(testAgentCheckpointCipher(t), 64*1024, time.Minute),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, PlatformSearchEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	suspended, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "search platform posts about cloud native",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if suspended.RunStatus != UnifiedAgentRunStatusAwaitingHuman ||
		suspended.Response != "Which cloud-native topic should be searched?" {
		t.Fatalf("suspended result = %+v", suspended)
	}
	store.mu.Lock()
	persisted := *store.run
	store.mu.Unlock()
	if !persisted.ResumeSupported || persisted.CheckpointVersion != agentRuntime.VerifiedCheckpointVersion ||
		persisted.VerifiedCheckpointVersion != agentRuntime.VerifiedCheckpointVersion ||
		persisted.VerifiedCheckpointRevision != 1 || persisted.VerifiedCheckpointDigest == "" ||
		strings.Contains(persisted.CheckpointCiphertext, "cloud-native topic") {
		t.Fatalf("persisted verified checkpoint = %+v", persisted)
	}
	if _, openErr := service.openVerifiedAgentRunCheckpoint(&persisted); openErr != nil {
		t.Fatalf("openVerifiedAgentRunCheckpoint() error = %v", openErr)
	}
	tampered := persisted
	tampered.VerifiedCheckpointDigest = strings.Repeat("0", 64)
	if _, openErr := service.openVerifiedAgentRunCheckpoint(&tampered); openErr == nil {
		t.Fatal("openVerifiedAgentRunCheckpoint() accepted a tampered public digest")
	}
	tampered = persisted
	tampered.VerifiedCheckpointVersion = agentRuntime.ReActCheckpointVersion
	if _, openErr := service.openVerifiedAgentRunCheckpoint(&tampered); openErr == nil {
		t.Fatal("openVerifiedAgentRunCheckpoint() accepted a mismatched checkpoint version")
	}

	resumed, err := service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: 42, RunID: suspended.RunID, ExpectedRevision: suspended.ApprovalState.Revision,
		HumanResponse: "Kubernetes",
	})
	if err != nil {
		t.Fatalf("ResumeAgentExecutionRun() error = %v", err)
	}
	if resumed.RunStatus != UnifiedAgentRunStatusCompleted ||
		resumed.Response != "One verified cloud-native result." || runner.resumeCalls != 1 ||
		runner.resume.HumanResponse != "Kubernetes" || len(runner.resume.Tools) != 1 {
		t.Fatalf("resumed result/request = %+v/%+v", resumed, runner.resume)
	}
	store.mu.Lock()
	completed := *store.run
	store.mu.Unlock()
	if completed.Status != repository.AgentExecutionRunCompleted || completed.CheckpointCiphertext != "" ||
		completed.PlanningCalls != 1 || completed.PlanningPlanDigest == "" ||
		completed.GoalRunStatus != string(agentRuntime.GoalRunVerified) ||
		completed.GoalVerificationStatus != string(agentRuntime.VerificationPassed) ||
		completed.VerifiedCheckpointVersion != agentRuntime.VerifiedCheckpointVersion {
		t.Fatalf("completed planned run = %+v", completed)
	}
}

func TestPlannedWebResearchResumesWithFreshReadToolAuthorization(t *testing.T) {
	planner := &serviceShortPlanPlanner{result: agentRuntime.ShortPlanResult{
		Proposal: agentRuntime.ShortPlanProposal{
			Version: agentRuntime.ShortPlanVersionV1,
			Steps: []agentRuntime.ShortPlanStep{{
				ID: "clarify", Kind: agentRuntime.ShortPlanStepAskHuman,
				Objective: "clarify the web research topic",
				CriterionIDs: []string{
					agentEvidence.WebSearchSourcesCriterion,
					agentEvidence.WebPageContentCriterion,
				},
			}},
		},
		Usage:    agentRuntime.TokenUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
		Attempts: 1, Model: "default-model", Provider: "test-provider",
	}}
	coordinator, err := agentRuntime.NewPlanningCoordinator(
		planner, agentRuntime.DeterministicShortPlanPolicy{},
	)
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	catalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWebSearchCapability())
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	runner := &plannedWebSuspendingRuntimeRunner{}
	store := &memoryAgentExecutionRunStore{}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithRuntimeToolCatalog(webResearchRuntimeCatalog()),
		WithAgentExecutionRunStore(store),
		WithRecoverableAgentRuns(true),
		WithAgentRunRecovery(testAgentCheckpointCipher(t), 64*1024, time.Minute),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, WebResearchEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	suspended, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "research the latest Go release",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if suspended.RunStatus != UnifiedAgentRunStatusAwaitingHuman {
		t.Fatalf("suspended result = %+v", suspended)
	}

	resumed, err := service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: 42, RunID: suspended.RunID, ExpectedRevision: suspended.ApprovalState.Revision,
		HumanResponse: "Go 1.24 release notes",
	})
	if err != nil {
		t.Fatalf("ResumeAgentExecutionRun() error = %v", err)
	}
	if resumed.RunStatus != UnifiedAgentRunStatusCompleted ||
		resumed.Response != "Grounded web answer" || runner.resumeCalls != 1 ||
		len(runner.resume.Tools) != 2 ||
		runner.resume.Tools[0].Name != "web_search" ||
		runner.resume.Tools[1].Name != "page_read" {
		t.Fatalf("resumed result/request = %+v/%+v", resumed, runner.resume)
	}
	store.mu.Lock()
	completed := *store.run
	store.mu.Unlock()
	if completed.Status != repository.AgentExecutionRunCompleted ||
		completed.GoalRunStatus != string(agentRuntime.GoalRunVerified) ||
		completed.GoalVerificationStatus != string(agentRuntime.VerificationPassed) ||
		completed.CheckpointCiphertext != "" {
		t.Fatalf("completed planned web run = %+v", completed)
	}
}
