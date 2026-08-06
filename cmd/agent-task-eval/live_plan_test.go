package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildStrategyAgentTaskLivePlanUsesFixedProfileBudgets(t *testing.T) {
	base := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata")
	dataset, err := loadAgentTaskDataset(filepath.Join(base, "agent_strategy_cases_v3.json"))
	if err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	config, err := loadStrategyRuntimeEvalConfig(filepath.Join(base, "agent_strategy_runtime_config.qwen37-v5.example.json"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	plan, err := buildStrategyAgentTaskLivePlan(dataset, "agent-strategy-cases-v3", config)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if plan.SchemaVersion != agentTaskLivePlanSchemaVersion || plan.EvaluationMode != "strategy_comparison" ||
		plan.Provider != "dashscope" || plan.Model != "qwen3.7-plus-2026-05-26" || plan.Cases != 20 {
		t.Fatalf("plan identity = %#v", plan)
	}
	if len(plan.Sides) != 2 {
		t.Fatalf("plan sides = %#v", plan.Sides)
	}
	candidate, stable := plan.Sides[0], plan.Sides[1]
	if candidate.ProviderCallsMinimum != 80 || candidate.ProviderCallsUpperBound != 100 ||
		candidate.TokenBudgetUpperBound != 480_000 || candidate.EstimatedCostUpperMicros != 2_000_000 {
		t.Fatalf("candidate plan = %#v", candidate)
	}
	if stable.ProviderCallsMinimum != 40 || stable.ProviderCallsUpperBound != 140 ||
		stable.TokenBudgetUpperBound != 760_000 || stable.EstimatedCostUpperMicros != 2_700_000 {
		t.Fatalf("stable plan = %#v", stable)
	}
	wantTokenUpper := int64(1_240_000 + plan.Preflight.InputTokenUpperBound + plan.Preflight.OutputTokenUpperBound)
	wantCostUpper := int64(4_700_000) + plan.Preflight.EstimatedCostUpperMicros
	if plan.Budget.ProviderCallsMinimum != 121 || plan.Budget.ProviderCallsUpperBound != 241 ||
		plan.Budget.TokenBudgetUpperBound != wantTokenUpper || plan.Budget.EstimatedCostUpperBoundMicros != wantCostUpper ||
		plan.Budget.CapturedOutputsWithoutReview != 0 || plan.Budget.CapturedOutputsWithReviewBundle != 40 {
		t.Fatalf("aggregate budget = %#v", plan.Budget)
	}
	if plan.Preflight.ProviderCalls != 1 || plan.Preflight.InputTokenUpperBound <= 0 || plan.Preflight.OutputTokenUpperBound != 64 {
		t.Fatalf("preflight plan = %#v", plan.Preflight)
	}
	if !plan.ModelReplacement.ExactModelRequired || !plan.ModelReplacement.ChangeRequiresNewPlan ||
		!plan.ModelReplacement.ChangeRequiresNewAuthorization || !plan.ModelReplacement.ChangeRequiresNewQualificationReport {
		t.Fatalf("model replacement policy = %#v", plan.ModelReplacement)
	}
}

func TestRunCreatesOfflineLivePlanWithoutCredentialOrProviderCall(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY", "")
	base := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata")
	out := filepath.Join(t.TempDir(), "qwen37-plan.json")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{
		"--plan-live-evaluation", out,
		"--dataset", filepath.Join(base, "agent_strategy_cases_v3.json"),
		"--dataset-version", "agent-strategy-cases-v3",
		"--strategy-runtime-config", filepath.Join(base, "agent_strategy_runtime_config.qwen37-v5.example.json"),
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("plan command: code=%d stderr=%q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "qwen3.7-plus-2026-05-26") || !strings.Contains(stdout.String(), "calls=121..241") {
		t.Fatalf("plan summary = %q", stdout.String())
	}
	payload, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	var plan agentTaskLivePlan
	if err := json.Unmarshal(payload, &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if plan.Model != "qwen3.7-plus-2026-05-26" || plan.Budget.ProviderCallsUpperBound != 241 {
		t.Fatalf("persisted plan = %#v", plan)
	}
	stdout.Reset()
	stderr.Reset()
	if exitCode = run([]string{
		"--plan-live-evaluation", out,
		"--dataset", filepath.Join(base, "agent_strategy_cases_v3.json"),
		"--dataset-version", "agent-strategy-cases-v3",
		"--strategy-runtime-config", filepath.Join(base, "agent_strategy_runtime_config.qwen37-v5.example.json"),
	}, &stdout, &stderr); exitCode != 2 || !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("plan overwrite was not rejected: code=%d stderr=%q", exitCode, stderr.String())
	}
}

func TestLiveAuthorizationPlanCoverageRejectsPartialBudgets(t *testing.T) {
	plan := agentTaskLivePlan{Budget: agentTaskLivePlanBudget{
		MaxRuns: 1, ProviderCallsUpperBound: 241,
		EstimatedCostUpperBoundMicros: 4_701_000, CapturedOutputsWithReviewBundle: 40,
	}}
	full := agentTaskLiveAuthorizationLimits{
		MaxRuns: 1, MaxProviderCalls: 241, MaxCapturedOutputs: 40, MaxEstimatedCostMicros: 4_701_000,
	}
	if err := validateAgentTaskLiveAuthorizationPlanCoverage(full, plan); err != nil {
		t.Fatalf("full plan coverage: %v", err)
	}
	withoutReview := full
	withoutReview.MaxCapturedOutputs = 0
	if err := validateAgentTaskLiveAuthorizationPlanCoverage(withoutReview, plan); err != nil {
		t.Fatalf("zero capture must remain a valid prohibition: %v", err)
	}
	underCalls := full
	underCalls.MaxProviderCalls--
	if err := validateAgentTaskLiveAuthorizationPlanCoverage(underCalls, plan); err == nil || !strings.Contains(err.Error(), "provider call limit") {
		t.Fatalf("underplanned calls were accepted: %v", err)
	}
	underCost := full
	underCost.MaxEstimatedCostMicros--
	if err := validateAgentTaskLiveAuthorizationPlanCoverage(underCost, plan); err == nil || !strings.Contains(err.Error(), "estimated cost limit") {
		t.Fatalf("underplanned cost was accepted: %v", err)
	}
	partialReview := full
	partialReview.MaxCapturedOutputs = 39
	if err := validateAgentTaskLiveAuthorizationPlanCoverage(partialReview, plan); err == nil || !strings.Contains(err.Error(), "captured output limit") {
		t.Fatalf("partial review capture was accepted: %v", err)
	}
}

func TestRunCreateLiveAuthorizationRequiresFullOfflinePlanCoverage(t *testing.T) {
	const key = "planned-live-authorization-key-material-v1"
	t.Setenv("TEST_PLANNED_LIVE_AUTHORIZATION_KEY", key)
	t.Setenv("DASHSCOPE_API_KEY", "")
	base := filepath.Join("..", "..", "internal", "module", "agent", "eval", "testdata")
	root := t.TempDir()
	common := []string{
		"--live-authorization-id", "qwen37-planned-authorization-v1",
		"--live-authorization-ttl", "1h",
		"--live-authorization-max-runs", "1",
		"--live-authorization-max-captured-outputs", "40",
		"--live-authorization-max-estimated-cost-micros", "5000000",
		"--dataset", filepath.Join(base, "agent_strategy_cases_v3.json"),
		"--dataset-version", "agent-strategy-cases-v3",
		"--strategy-runtime-config", filepath.Join(base, "agent_strategy_runtime_config.qwen37-v5.example.json"),
		"--live-authorization-key-env", "TEST_PLANNED_LIVE_AUTHORIZATION_KEY",
		"--live-authorization-key-id", "planned-live-key-v1",
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	underplanned := append([]string{
		"--create-live-authorization", filepath.Join(root, "underplanned.json"),
		"--live-authorization-max-provider-calls", "240",
	}, common...)
	if exitCode := run(underplanned, &stdout, &stderr); exitCode != 2 || !strings.Contains(stderr.String(), "full plan upper bound 241") {
		t.Fatalf("underplanned authorization was accepted: code=%d stderr=%q", exitCode, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	plannedPath := filepath.Join(root, "planned.json")
	planned := append([]string{
		"--create-live-authorization", plannedPath,
		"--live-authorization-max-provider-calls", "241",
	}, common...)
	if exitCode := run(planned, &stdout, &stderr); exitCode != 0 {
		t.Fatalf("planned authorization: code=%d stderr=%q", exitCode, stderr.String())
	}
	if _, err := os.Stat(plannedPath); err != nil {
		t.Fatalf("planned authorization not written: %v", err)
	}
}

func TestModelReplacementChangesPlanIdentity(t *testing.T) {
	dataset := []byte(`[{"id":"case-1","category":"chat","mode":"chat","input":"hello","expected_outcome":"completed"}]`)
	root := t.TempDir()
	datasetPath := filepath.Join(root, "dataset.json")
	if err := os.WriteFile(datasetPath, dataset, 0o600); err != nil {
		t.Fatalf("write dataset: %v", err)
	}
	cases, err := loadAgentTaskDataset(datasetPath)
	if err != nil {
		t.Fatalf("load dataset: %v", err)
	}
	config := validRuntimeEvalConfig()
	config.Model = "qwen3.7-plus-2026-05-26"
	fixed, err := buildRuntimeAgentTaskLivePlan(cases, "cases-v1", config)
	if err != nil {
		t.Fatalf("build fixed plan: %v", err)
	}
	config.Model = "qwen3.7-plus"
	rolling, err := buildRuntimeAgentTaskLivePlan(cases, "cases-v1", config)
	if err != nil {
		t.Fatalf("build rolling plan: %v", err)
	}
	if fixed.ExecutionConfigSHA256 == rolling.ExecutionConfigSHA256 || fixed.Model == rolling.Model {
		t.Fatalf("model replacement did not change plan identity: fixed=%#v rolling=%#v", fixed, rolling)
	}
}
