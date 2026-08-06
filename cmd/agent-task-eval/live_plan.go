package main

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"twitter-clone/internal/module/agent/eval"
)

const agentTaskLivePlanSchemaVersion = "agent-task-live-plan/v1"

type agentTaskLivePlan struct {
	SchemaVersion         string                      `json:"schema_version"`
	EvaluationMode        string                      `json:"evaluation_mode"`
	Provider              string                      `json:"provider"`
	Model                 string                      `json:"model"`
	PricingVersion        string                      `json:"pricing_version,omitempty"`
	DatasetVersion        string                      `json:"dataset_version"`
	DatasetSHA256         string                      `json:"dataset_sha256"`
	ExecutionConfigSHA256 string                      `json:"execution_config_sha256"`
	Cases                 int                         `json:"cases"`
	Sides                 []agentTaskLivePlanSide     `json:"sides"`
	Preflight             agentTaskLivePlanPreflight  `json:"preflight"`
	Budget                agentTaskLivePlanBudget     `json:"budget"`
	ModelReplacement      agentTaskLiveModelMigration `json:"model_replacement"`
}

type agentTaskLivePlanSide struct {
	Side                     string `json:"side"`
	Strategy                 string `json:"strategy"`
	Cases                    int    `json:"cases"`
	ProviderCallsMinimum     int    `json:"provider_calls_minimum"`
	ProviderCallsUpperBound  int    `json:"provider_calls_upper_bound"`
	TokenBudgetUpperBound    int64  `json:"token_budget_upper_bound"`
	EstimatedCostUpperMicros int64  `json:"estimated_cost_upper_bound_micros"`
}

type agentTaskLivePlanPreflight struct {
	ProviderCalls            int   `json:"provider_calls"`
	InputTokenUpperBound     int   `json:"input_token_upper_bound"`
	OutputTokenUpperBound    int   `json:"output_token_upper_bound"`
	EstimatedCostUpperMicros int64 `json:"estimated_cost_upper_bound_micros"`
}

type agentTaskLivePlanBudget struct {
	MaxRuns                         int   `json:"max_runs"`
	ProviderCallsMinimum            int   `json:"provider_calls_minimum"`
	ProviderCallsUpperBound         int   `json:"provider_calls_upper_bound"`
	TokenBudgetUpperBound           int64 `json:"token_budget_upper_bound"`
	EstimatedCostUpperBoundMicros   int64 `json:"estimated_cost_upper_bound_micros"`
	CapturedOutputsWithoutReview    int   `json:"captured_outputs_without_review"`
	CapturedOutputsWithReviewBundle int   `json:"captured_outputs_with_review_bundle"`
}

type agentTaskLiveModelMigration struct {
	IdentityBinding                       string `json:"identity_binding"`
	ExactModelRequired                    bool   `json:"exact_model_required"`
	ChangeRequiresNewPlan                 bool   `json:"change_requires_new_plan"`
	ChangeRequiresNewAuthorization        bool   `json:"change_requires_new_authorization"`
	ChangeRequiresNewQualificationReport  bool   `json:"change_requires_new_qualification_report"`
	HistoricalReportMustRemainModelScoped bool   `json:"historical_report_must_remain_model_scoped"`
}

type createAgentTaskLivePlanCommand struct {
	OutputPath         string
	DatasetPath        string
	DatasetVersion     string
	RuntimeConfigPath  string
	StrategyConfigPath string
}

func runCreateAgentTaskLivePlan(command createAgentTaskLivePlanCommand) (agentTaskLivePlan, error) {
	command.OutputPath = strings.TrimSpace(command.OutputPath)
	command.DatasetVersion = strings.TrimSpace(command.DatasetVersion)
	command.RuntimeConfigPath = strings.TrimSpace(command.RuntimeConfigPath)
	command.StrategyConfigPath = strings.TrimSpace(command.StrategyConfigPath)
	if command.OutputPath == "" || (command.RuntimeConfigPath == "") == (command.StrategyConfigPath == "") {
		return agentTaskLivePlan{}, errors.New("live evaluation planning requires an output and exactly one runtime config")
	}
	if command.DatasetVersion == "" {
		return agentTaskLivePlan{}, errors.New("live evaluation planning requires a dataset version")
	}
	if err := ensureReviewPathAvailable(command.OutputPath, "live evaluation plan"); err != nil {
		return agentTaskLivePlan{}, err
	}
	dataset, err := loadAgentTaskDataset(command.DatasetPath)
	if err != nil {
		return agentTaskLivePlan{}, err
	}
	var plan agentTaskLivePlan
	if command.RuntimeConfigPath != "" {
		config, loadErr := loadRuntimeEvalConfig(command.RuntimeConfigPath)
		if loadErr != nil {
			return agentTaskLivePlan{}, fmt.Errorf("load runtime evaluation config: %w", loadErr)
		}
		plan, err = buildRuntimeAgentTaskLivePlan(dataset, command.DatasetVersion, config)
	} else {
		config, loadErr := loadStrategyRuntimeEvalConfig(command.StrategyConfigPath)
		if loadErr != nil {
			return agentTaskLivePlan{}, fmt.Errorf("load strategy runtime evaluation config: %w", loadErr)
		}
		plan, err = buildStrategyAgentTaskLivePlan(dataset, command.DatasetVersion, config)
	}
	if err != nil {
		return agentTaskLivePlan{}, err
	}
	if err := writeExclusiveReviewJSON(command.OutputPath, plan, "live evaluation plan"); err != nil {
		return agentTaskLivePlan{}, err
	}
	return plan, nil
}

func buildRuntimeAgentTaskLivePlan(dataset []eval.AgentTaskCase, datasetVersion string, config runtimeEvalConfig) (agentTaskLivePlan, error) {
	config, err := normalizeRuntimeEvalConfig(config)
	if err != nil {
		return agentTaskLivePlan{}, err
	}
	configHash, err := hashRuntimeEvalConfig(config)
	if err != nil {
		return agentTaskLivePlan{}, err
	}
	side := agentTaskLivePlanSide{Side: "candidate", Strategy: eval.AgentStrategySingle, Cases: len(dataset)}
	for _, sample := range dataset {
		side.ProviderCallsMinimum += minimumSingleAgentProviderCalls(sample)
		if err := addLivePlanInt(&side.ProviderCallsUpperBound, config.Profile.MaxSteps, "runtime provider calls"); err != nil {
			return agentTaskLivePlan{}, err
		}
		if err := addLivePlanInt64(&side.TokenBudgetUpperBound, int64(config.Profile.MaxTotalTokens), "runtime token budget"); err != nil {
			return agentTaskLivePlan{}, err
		}
		if err := addLivePlanInt64(&side.EstimatedCostUpperMicros, profileEstimatedCostUpper(
			config.Profile, config.InputMicrosPerMillionTokens, config.OutputMicrosPerMillionTokens,
		), "runtime estimated cost"); err != nil {
			return agentTaskLivePlan{}, err
		}
	}
	return finalizeAgentTaskLivePlan(agentTaskLivePlan{
		SchemaVersion: agentTaskLivePlanSchemaVersion, EvaluationMode: "single_runtime",
		Provider: config.Provider, Model: config.Model, PricingVersion: config.PricingVersion,
		DatasetVersion: strings.TrimSpace(datasetVersion), ExecutionConfigSHA256: configHash,
		Cases: len(dataset), Sides: []agentTaskLivePlanSide{side},
	}, dataset, config.InputMicrosPerMillionTokens, config.OutputMicrosPerMillionTokens, 0)
}

func buildStrategyAgentTaskLivePlan(dataset []eval.AgentTaskCase, datasetVersion string, config strategyRuntimeEvalConfig) (agentTaskLivePlan, error) {
	config, err := normalizeStrategyRuntimeEvalConfig(config)
	if err != nil {
		return agentTaskLivePlan{}, err
	}
	configHash, err := hashStrategyRuntimeEvalConfig(config)
	if err != nil {
		return agentTaskLivePlan{}, err
	}
	templates := make(map[string]strategyRuntimeEvalTemplateConfig, len(config.Templates))
	for _, template := range config.Templates {
		templates[template.TemplateID] = template
	}
	candidate := agentTaskLivePlanSide{Side: "candidate", Strategy: eval.AgentStrategyMulti, Cases: len(dataset)}
	stable := agentTaskLivePlanSide{Side: "stable", Strategy: eval.AgentStrategySingle, Cases: len(dataset)}
	for _, sample := range dataset {
		template, ok := templates[strings.TrimSpace(sample.StrategyTemplateID)]
		if !ok {
			return agentTaskLivePlan{}, fmt.Errorf("live evaluation case %q uses unconfigured strategy template %q", sample.ID, sample.StrategyTemplateID)
		}
		spec, ok := findStrategyRuntimeTemplateSpec(sample.StrategyTemplateID)
		if !ok {
			return agentTaskLivePlan{}, fmt.Errorf("live evaluation case %q uses unsupported strategy template %q", sample.ID, sample.StrategyTemplateID)
		}
		minimumCandidateCalls := 3
		if spec.requiredTool != "" {
			minimumCandidateCalls++
		}
		if err := addLivePlanInt(&candidate.ProviderCallsMinimum, minimumCandidateCalls, "minimum candidate provider calls"); err != nil {
			return agentTaskLivePlan{}, err
		}
		if err := addLivePlanInt(&stable.ProviderCallsMinimum, minimumSingleAgentProviderCalls(sample), "minimum stable provider calls"); err != nil {
			return agentTaskLivePlan{}, err
		}
		for _, profile := range []runtimeEvalProfileConfig{template.ResearcherProfile, template.DrafterProfile, template.ReviewerProfile} {
			if err := addLivePlanProfile(&candidate, profile, config); err != nil {
				return agentTaskLivePlan{}, err
			}
		}
		if err := addLivePlanProfile(&stable, template.SingleProfile, config); err != nil {
			return agentTaskLivePlan{}, err
		}
	}
	return finalizeAgentTaskLivePlan(agentTaskLivePlan{
		SchemaVersion: agentTaskLivePlanSchemaVersion, EvaluationMode: "strategy_comparison",
		Provider: config.Provider, Model: config.Model, PricingVersion: config.PricingVersion,
		DatasetVersion: strings.TrimSpace(datasetVersion), ExecutionConfigSHA256: configHash,
		Cases: len(dataset), Sides: []agentTaskLivePlanSide{candidate, stable},
	}, dataset, config.InputMicrosPerMillionTokens, config.OutputMicrosPerMillionTokens, len(dataset)*2)
}

func addLivePlanProfile(side *agentTaskLivePlanSide, profile runtimeEvalProfileConfig, config strategyRuntimeEvalConfig) error {
	if side == nil {
		return errors.New("live evaluation side plan is nil")
	}
	if err := addLivePlanInt(&side.ProviderCallsUpperBound, profile.MaxSteps, "strategy provider calls"); err != nil {
		return err
	}
	if err := addLivePlanInt64(&side.TokenBudgetUpperBound, int64(profile.MaxTotalTokens), "strategy token budget"); err != nil {
		return err
	}
	return addLivePlanInt64(&side.EstimatedCostUpperMicros, profileEstimatedCostUpper(
		profile, config.InputMicrosPerMillionTokens, config.OutputMicrosPerMillionTokens,
	), "strategy estimated cost")
}

func finalizeAgentTaskLivePlan(
	plan agentTaskLivePlan,
	dataset []eval.AgentTaskCase,
	inputMicrosPerMillion int64,
	outputMicrosPerMillion int64,
	reviewOutputs int,
) (agentTaskLivePlan, error) {
	datasetHash, err := eval.HashAgentTaskDataset(dataset)
	if err != nil {
		return agentTaskLivePlan{}, fmt.Errorf("hash live evaluation plan dataset: %w", err)
	}
	plan.DatasetSHA256 = datasetHash
	preflightRequest := runtimeEvalPreflightRequest(plan.Model)
	plan.Preflight = agentTaskLivePlanPreflight{
		ProviderCalls: 1, InputTokenUpperBound: conservativeAgentTaskLiveRequestTokens(preflightRequest),
		OutputTokenUpperBound: preflightRequest.MaxOutputTokens,
	}
	plan.Preflight.EstimatedCostUpperMicros = estimatedTokenCost(plan.Preflight.InputTokenUpperBound, inputMicrosPerMillion) +
		estimatedTokenCost(plan.Preflight.OutputTokenUpperBound, outputMicrosPerMillion)
	plan.Budget = agentTaskLivePlanBudget{
		MaxRuns: 1, ProviderCallsMinimum: 1, ProviderCallsUpperBound: 1,
		TokenBudgetUpperBound:           int64(plan.Preflight.InputTokenUpperBound + plan.Preflight.OutputTokenUpperBound),
		EstimatedCostUpperBoundMicros:   plan.Preflight.EstimatedCostUpperMicros,
		CapturedOutputsWithReviewBundle: reviewOutputs,
	}
	for _, side := range plan.Sides {
		if err := addLivePlanInt(&plan.Budget.ProviderCallsMinimum, side.ProviderCallsMinimum, "minimum provider calls"); err != nil {
			return agentTaskLivePlan{}, err
		}
		if err := addLivePlanInt(&plan.Budget.ProviderCallsUpperBound, side.ProviderCallsUpperBound, "provider call upper bound"); err != nil {
			return agentTaskLivePlan{}, err
		}
		if err := addLivePlanInt64(&plan.Budget.TokenBudgetUpperBound, side.TokenBudgetUpperBound, "token budget upper bound"); err != nil {
			return agentTaskLivePlan{}, err
		}
		if err := addLivePlanInt64(&plan.Budget.EstimatedCostUpperBoundMicros, side.EstimatedCostUpperMicros, "estimated cost upper bound"); err != nil {
			return agentTaskLivePlan{}, err
		}
	}
	plan.ModelReplacement = agentTaskLiveModelMigration{
		IdentityBinding: "provider+exact_model+execution_config_sha256", ExactModelRequired: true,
		ChangeRequiresNewPlan: true, ChangeRequiresNewAuthorization: true,
		ChangeRequiresNewQualificationReport: true, HistoricalReportMustRemainModelScoped: true,
	}
	return plan, nil
}

func validateAgentTaskLiveAuthorizationPlanCoverage(limits agentTaskLiveAuthorizationLimits, plan agentTaskLivePlan) error {
	if limits.MaxRuns < plan.Budget.MaxRuns {
		return fmt.Errorf("live authorization max runs %d is below planned requirement %d", limits.MaxRuns, plan.Budget.MaxRuns)
	}
	if limits.MaxProviderCalls < plan.Budget.ProviderCallsUpperBound {
		return fmt.Errorf(
			"live authorization provider call limit %d is below full plan upper bound %d; create an offline --plan-live-evaluation artifact before signing",
			limits.MaxProviderCalls, plan.Budget.ProviderCallsUpperBound,
		)
	}
	if limits.MaxEstimatedCostMicros < plan.Budget.EstimatedCostUpperBoundMicros {
		return fmt.Errorf(
			"live authorization estimated cost limit %d is below full plan upper bound %d micros",
			limits.MaxEstimatedCostMicros, plan.Budget.EstimatedCostUpperBoundMicros,
		)
	}
	if limits.MaxCapturedOutputs > 0 && limits.MaxCapturedOutputs < plan.Budget.CapturedOutputsWithReviewBundle {
		return fmt.Errorf(
			"live authorization captured output limit %d cannot cover the planned review bundle upper bound %d; use zero to prohibit capture",
			limits.MaxCapturedOutputs, plan.Budget.CapturedOutputsWithReviewBundle,
		)
	}
	return nil
}

func minimumSingleAgentProviderCalls(sample eval.AgentTaskCase) int {
	if sample.ExpectedOutcome == eval.AgentTaskOutcomeBudgetExceeded {
		return 0
	}
	if len(sample.ExpectedTools) > 0 && sample.ExpectedOutcome == eval.AgentTaskOutcomeCompleted {
		return 2
	}
	return 1
}

func profileEstimatedCostUpper(profile runtimeEvalProfileConfig, inputRate, outputRate int64) int64 {
	if profile.MaxEstimatedCostMicros > 0 {
		return profile.MaxEstimatedCostMicros
	}
	rate := inputRate
	if outputRate > rate {
		rate = outputRate
	}
	return estimatedTokenCost(profile.MaxTotalTokens, rate)
}

func estimatedTokenCost(tokens int, microsPerMillion int64) int64 {
	if tokens <= 0 || microsPerMillion <= 0 {
		return 0
	}
	if int64(tokens) > math.MaxInt64/microsPerMillion {
		return math.MaxInt64
	}
	product := int64(tokens) * microsPerMillion
	if product > math.MaxInt64-999_999 {
		return math.MaxInt64
	}
	return (product + 999_999) / 1_000_000
}

func addLivePlanInt(target *int, delta int, label string) error {
	if target == nil || delta < 0 || *target > math.MaxInt-delta {
		return fmt.Errorf("%s overflow", label)
	}
	*target += delta
	return nil
}

func addLivePlanInt64(target *int64, delta int64, label string) error {
	if target == nil || delta < 0 || *target > math.MaxInt64-delta {
		return fmt.Errorf("%s overflow", label)
	}
	*target += delta
	return nil
}
