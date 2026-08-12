package service

import (
	"context"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestE2E07WebResearchMigrationDualRecordsSingleExecution(t *testing.T) {
	repo := &assistRuntimeRepository{}
	runner := &capturingRuntimeRunner{result: webResearchShadowRun(
		true, "https://go.dev/doc/devel/release",
	)}
	observer := &goalRuntimeShadowObserverFake{}
	catalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWebSearchCapability())
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	service := NewAgentService(
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
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "research the latest Go release using public sources",
		PreferredCapabilityIDs: []string{CapabilityWebSearch},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if runner.calls != 1 || len(observer.observations) != 1 {
		t.Fatalf("runtime calls = %d shadow observations = %d", runner.calls, len(observer.observations))
	}
	observation := observer.observations[0]
	if observation.TaskOutcome == nil ||
		observation.TaskOutcome.ExecutionSource != agentRuntime.TaskOutcomeExecutionObserved ||
		observation.TaskOutcome.Status != agentRuntime.GoalRunVerified ||
		observation.EvidenceComparison != GoalShadowComparisonConsistent ||
		len(observation.TaskOutcome.PlanDigests) != 0 {
		t.Fatalf("shadow observation = %+v", observation)
	}
	if len(result.Citations) != 1 || result.Citations[0].URL != "https://go.dev/doc/devel/release" {
		t.Fatalf("legacy citations = %+v", result.Citations)
	}
	if len(observation.TaskOutcome.Evidence.Items) != 2 ||
		observation.TaskOutcome.Evidence.Items[0].Reference != result.Citations[0].URL ||
		observation.TaskOutcome.Evidence.Items[1].Reference != result.Citations[0].URL {
		t.Fatalf("goal evidence = %+v", observation.TaskOutcome.Evidence.Items)
	}
	if len(runner.request.Tools) != 2 {
		t.Fatalf("runtime tools = %+v", runner.request.Tools)
	}
}
