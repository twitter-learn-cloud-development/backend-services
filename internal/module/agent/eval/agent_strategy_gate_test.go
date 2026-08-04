package eval

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAgentStrategyContractFixturesPassComparisonGate(t *testing.T) {
	datasetFile, err := os.Open("testdata/agent_strategy_cases.json")
	if err != nil {
		t.Fatalf("open strategy dataset: %v", err)
	}
	dataset, err := LoadAgentTaskDataset(datasetFile)
	_ = datasetFile.Close()
	if err != nil {
		t.Fatalf("load strategy dataset: %v", err)
	}
	if len(dataset) != 20 {
		t.Fatalf("strategy dataset cases = %d, want 20", len(dataset))
	}
	templates := make(map[string]int)
	for _, sample := range dataset {
		templates[sample.StrategyTemplateID]++
		if sample.StrategyTemplateID == WebResearchDraftTemplate &&
			(!equalAgentTaskStringSet(sample.ExpectedTools, []string{"web_search"}) ||
				!equalAgentTaskStringSet(sample.AllowedTools, []string{"web_search", "page_read"})) {
			t.Fatalf("web strategy case %q tool contract = required %v allowed %v", sample.ID, sample.ExpectedTools, sample.AllowedTools)
		}
	}
	if templates[PlatformResearchDraftTemplate] != 10 || templates[WebResearchDraftTemplate] != 10 {
		t.Fatalf("strategy template coverage = %v", templates)
	}

	loadResults := func(path string) RecordedAgentTaskResultSet {
		file, openErr := os.Open(path)
		if openErr != nil {
			t.Fatalf("open %s: %v", path, openErr)
		}
		set, loadErr := LoadRecordedAgentTaskResults(file)
		_ = file.Close()
		if loadErr != nil {
			t.Fatalf("load %s: %v", path, loadErr)
		}
		return set
	}
	runResults := func(set RecordedAgentTaskResultSet) AgentTaskReport {
		executor, executorErr := NewRecordedAgentTaskExecutor(set)
		if executorErr != nil {
			t.Fatalf("new recorded executor: %v", executorErr)
		}
		report, runErr := RunAgentTasks(context.Background(), dataset, executor, AgentTaskRunnerConfig{
			DatasetVersion: "agent-strategy-cases-v2", Environment: "fixture", CaseTimeout: 15 * time.Second,
			Execution: set.Descriptor(), ExecutionConfigHash: set.ExecutionConfigHash,
		})
		if runErr != nil {
			t.Fatalf("run strategy fixture: %v", runErr)
		}
		return report
	}
	stable := runResults(loadResults("testdata/agent_strategy_single_results.json"))
	candidate := runResults(loadResults("testdata/agent_strategy_multi_results.json"))
	decision, err := EvaluateAgentStrategyGate(stable, candidate, AgentStrategyGatePolicy{})
	if err != nil {
		t.Fatalf("EvaluateAgentStrategyGate() error = %v", err)
	}
	if decision.Status != AgentQualityGatePassed || stable.Metrics.SemanticPassRate != 0.8 || candidate.Metrics.SemanticPassRate != 1 {
		t.Fatalf("strategy fixture decision = %+v, stable = %+v, candidate = %+v", decision, stable.Metrics, candidate.Metrics)
	}
}

func TestRunAgentTasksCollectsComparableResourceEvidence(t *testing.T) {
	report, err := RunAgentTasks(context.Background(), []AgentTaskCase{{
		ID: "resource", Category: "platform_research_draft", Mode: "assist",
		StrategyTemplateID: PlatformResearchDraftTemplate,
		Input:              "research", ExpectedOutcome: AgentTaskOutcomeCompleted,
	}}, agentTaskExecutorFunc(func(context.Context, AgentTaskCase) (AgentTaskExecution, error) {
		return AgentTaskExecution{
			Outcome: AgentTaskOutcomeCompleted, Output: "result",
			DurationMS: 123, EstimatedCostMicros: 42, CostEstimated: true, PricingVersion: "pricing-v1",
		}, nil
	}), AgentTaskRunnerConfig{
		DatasetVersion: "resource-v1", CaseTimeout: 15 * time.Second,
		Execution: AgentTaskExecutionDescriptor{Strategy: AgentStrategySingle, PricingVersion: "pricing-v1"},
	})
	if err != nil {
		t.Fatalf("RunAgentTasks() error = %v", err)
	}
	if report.Metrics.P95MS != 123 || report.Metrics.CostEvidenceCases != 1 ||
		report.Metrics.TotalEstimatedCostMicros != 42 || report.Metrics.AverageEstimatedCostMicros != 42 {
		t.Fatalf("resource metrics = %+v", report.Metrics)
	}
	if !slices.Equal(report.PricingVersions, []string{"pricing-v1"}) {
		t.Fatalf("pricing versions = %v", report.PricingVersions)
	}
}

func TestEvaluateAgentStrategyGatePassesBoundedImprovement(t *testing.T) {
	stable, candidate := strategyGateReports()

	decision, err := EvaluateAgentStrategyGate(stable, candidate, AgentStrategyGatePolicy{})
	if err != nil {
		t.Fatalf("EvaluateAgentStrategyGate() error = %v", err)
	}
	if decision.Status != AgentQualityGatePassed {
		t.Fatalf("decision = %+v, want passed", decision)
	}
	if decision.SemanticGainBPS != 1500 || decision.AverageCostRatioBPS != 25000 || decision.P95LatencyRatioBPS != 30000 {
		t.Fatalf("decision metrics = %+v", decision)
	}
}

func TestEvaluateAgentStrategyGateRejectsUnsafeExpensiveSlowCandidate(t *testing.T) {
	stable, candidate := strategyGateReports()
	candidate.Metrics.SemanticPassRate = 0.82
	candidate.Metrics.TotalEstimatedCostMicros = 8000
	candidate.Metrics.AverageEstimatedCostMicros = 400
	candidate.Metrics.P95MS = 4000
	candidate.Metrics.UnauthorizedWriteSuccesses = 1

	decision, err := EvaluateAgentStrategyGate(stable, candidate, AgentStrategyGatePolicy{})
	if err != nil {
		t.Fatalf("EvaluateAgentStrategyGate() error = %v", err)
	}
	for _, code := range []string{
		"average_cost_ratio_exceeded",
		"candidate_safety_violation",
		"p95_latency_ratio_exceeded",
		"semantic_gain_below_policy",
	} {
		if !slices.Contains(decision.ReasonCodes, code) {
			t.Fatalf("decision reason codes = %v, missing %q", decision.ReasonCodes, code)
		}
	}
	if decision.Status != AgentQualityGateFailed {
		t.Fatalf("decision = %+v, want failed", decision)
	}
}

func TestEvaluateAgentStrategyGateRejectsIncomparableEvidence(t *testing.T) {
	stable, candidate := strategyGateReports()
	candidate.Execution.Provider = "other-provider"
	candidate.Metrics.CostEvidenceCases--
	candidate.CaseResults[0].StrategyTemplateID = "unsupported.template.v1"

	decision, err := EvaluateAgentStrategyGate(stable, candidate, AgentStrategyGatePolicy{})
	if err != nil {
		t.Fatalf("EvaluateAgentStrategyGate() error = %v", err)
	}
	for _, code := range []string{"cost_evidence_incomplete", "provider_model_mismatch", "template_scope_mismatch"} {
		if !slices.Contains(decision.ReasonCodes, code) {
			t.Fatalf("decision reason codes = %v, missing %q", decision.ReasonCodes, code)
		}
	}
	if decision.Status != AgentQualityGateIneligible {
		t.Fatalf("decision = %+v, want ineligible", decision)
	}
}

func TestEvaluateAgentStrategyGateRejectsDifferentExecutionConfig(t *testing.T) {
	stable, candidate := strategyGateReports()
	candidate.ExecutionConfigHash = strings.Repeat("c", 64)

	decision, err := EvaluateAgentStrategyGate(stable, candidate, AgentStrategyGatePolicy{})
	if err != nil {
		t.Fatalf("EvaluateAgentStrategyGate() error = %v", err)
	}
	if decision.Status != AgentQualityGateIneligible ||
		!slices.Contains(decision.ReasonCodes, "execution_config_evidence_invalid") {
		t.Fatalf("decision = %+v, want incomparable execution config", decision)
	}
}

func TestNormalizeAgentStrategyGatePolicyRejectsInvalidRatios(t *testing.T) {
	_, err := NormalizeAgentStrategyGatePolicy(AgentStrategyGatePolicy{MaxAverageCostRatioBPS: 9999})
	if err == nil || !strings.Contains(err.Error(), "max_average_cost_ratio_bps") {
		t.Fatalf("NormalizeAgentStrategyGatePolicy() error = %v", err)
	}
}

func strategyGateReports() (AgentTaskReport, AgentTaskReport) {
	caseResults := make([]AgentTaskCaseResult, 0, 20)
	for index := 0; index < 20; index++ {
		templateID := PlatformResearchDraftTemplate
		if index >= 10 {
			templateID = WebResearchDraftTemplate
		}
		caseResults = append(caseResults, AgentTaskCaseResult{
			CaseID: "case-" + string(rune('a'+index)), StrategyTemplateID: templateID,
		})
	}
	base := AgentTaskReport{
		DatasetVersion: "strategy-cases-v1",
		DatasetSHA256:  strings.Repeat("a", 64), ExecutionConfigHash: strings.Repeat("b", 64),
		Environment: "controlled", Seed: 7, CaseTimeoutMS: 15000,
		Execution: AgentTaskExecutionDescriptor{
			Kind: "recorded_fixture", Version: "single-v1", Strategy: AgentStrategySingle,
			Provider: "fixed-provider", Model: "fixed-model", PricingVersion: "pricing-v1",
		},
		PricingVersions: []string{"pricing-v1"},
		Metrics: AgentTaskMetrics{
			Cases: 20, TaskCompletionRate: 1, ToolSelectionAccuracy: 1,
			ReadToolSelectionCases: 20, ReadToolSelectionAccuracy: 1,
			SemanticCases: 20, SemanticPassRate: 0.80,
			CostEvidenceCases: 20, CostEstimatedCases: 20,
			TotalEstimatedCostMicros: 2000, AverageEstimatedCostMicros: 100, P95MS: 1000,
		},
		CaseResults: caseResults,
	}
	candidate := base
	candidate.Execution.Version = "multi-v1"
	candidate.Execution.Strategy = AgentStrategyMulti
	candidate.Metrics.SemanticPassRate = 0.95
	candidate.Metrics.TotalEstimatedCostMicros = 5000
	candidate.Metrics.AverageEstimatedCostMicros = 250
	candidate.Metrics.P95MS = 3000
	candidate.CaseResults = append([]AgentTaskCaseResult(nil), caseResults...)
	return base, candidate
}
