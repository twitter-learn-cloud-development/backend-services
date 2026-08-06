package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"twitter-clone/internal/module/agent/eval"
	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentMultiRole "twitter-clone/internal/module/agent/multirole"
	"twitter-clone/internal/module/agent/profile"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

const (
	strategyRuntimeEvalConfigVersion = "agent-strategy-runtime-config/v1"
	strategyRuntimeExecutorVersion   = "agent-strategy-runtime/v5"
	strategyRuntimeMinimumCaseTime   = 50 * time.Second
)

type strategyRuntimeEvalConfig struct {
	Version                      string                              `json:"version"`
	Provider                     string                              `json:"provider"`
	BaseURL                      string                              `json:"base_url"`
	Model                        string                              `json:"model"`
	CredentialEnv                string                              `json:"credential_env"`
	ReasoningMode                string                              `json:"reasoning_mode,omitempty"`
	ProfileSetVersion            string                              `json:"profile_set_version,omitempty"`
	ContextWindow                int                                 `json:"context_window"`
	MaxOutputTokens              int                                 `json:"max_output_tokens"`
	InputMicrosPerMillionTokens  int64                               `json:"input_micros_per_million_tokens,omitempty"`
	OutputMicrosPerMillionTokens int64                               `json:"output_micros_per_million_tokens,omitempty"`
	PricingVersion               string                              `json:"pricing_version"`
	ProviderTimeoutMS            int64                               `json:"provider_timeout_ms"`
	Templates                    []strategyRuntimeEvalTemplateConfig `json:"templates"`
}

type strategyRuntimeEvalTemplateConfig struct {
	TemplateID        string                   `json:"template_id"`
	SingleProfile     runtimeEvalProfileConfig `json:"single_profile"`
	ResearcherProfile runtimeEvalProfileConfig `json:"researcher_profile"`
	DrafterProfile    runtimeEvalProfileConfig `json:"drafter_profile"`
	ReviewerProfile   runtimeEvalProfileConfig `json:"reviewer_profile"`
}

type strategyRuntimeTemplateSpec struct {
	templateID           string
	executionProfile     string
	researchCapabilityID string
	draftCapabilityID    string
	requiredTool         string
	researchTools        []string
	singleProfileID      string
	researcherProfileID  string
	drafterProfileID     string
	reviewerProfileID    string
}

var strategyRuntimeTemplateSpecs = []strategyRuntimeTemplateSpec{
	{
		templateID: "platform.research_draft.v1", executionProfile: "runtime.research_draft",
		researchCapabilityID: "platform.search", draftCapabilityID: "content.draft",
		requiredTool: "hybrid_search_tweets", researchTools: []string{"hybrid_search_tweets"},
		singleProfileID: "unified.research_draft", researcherProfileID: "multi.runtime.platform_researcher",
		drafterProfileID: "multi.runtime.drafter", reviewerProfileID: "multi.runtime.reviewer",
	},
	{
		templateID: "web.research_draft.v1", executionProfile: "runtime.web_research_draft",
		researchCapabilityID: "web.search", draftCapabilityID: "content.draft",
		requiredTool: "web_search", researchTools: []string{"web_search", "page_read"},
		singleProfileID: "unified.web_research_draft", researcherProfileID: "multi.runtime.web_researcher",
		drafterProfileID: "multi.runtime.drafter", reviewerProfileID: "multi.runtime.reviewer",
	},
}

type runtimeStrategyExecutors struct {
	single     *runtimeStrategyAgentTaskExecutor
	multi      *runtimeStrategyAgentTaskExecutor
	configHash string
}

type runtimeStrategyAgentTaskExecutor struct {
	strategy       string
	provider       string
	model          string
	profileVersion string
	pricingVersion string
	modelClient    agentRuntime.ModelClient
	costEstimator  agentRuntime.CostEstimator
	planner        agentStrategy.Planner
	templates      map[string]runtimeStrategyTemplate
}

type runtimeStrategyTemplate struct {
	spec       strategyRuntimeTemplateSpec
	single     profile.AgentProfile
	researcher profile.AgentProfile
	drafter    profile.AgentProfile
	reviewer   profile.AgentProfile
}

func loadStrategyRuntimeEvalConfig(path string) (strategyRuntimeEvalConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return strategyRuntimeEvalConfig{}, fmt.Errorf("open strategy runtime evaluation config: %w", err)
	}
	defer file.Close()
	return decodeStrategyRuntimeEvalConfig(file)
}

func decodeStrategyRuntimeEvalConfig(reader io.Reader) (strategyRuntimeEvalConfig, error) {
	if reader == nil {
		return strategyRuntimeEvalConfig{}, errors.New("strategy runtime evaluation config reader is nil")
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var config strategyRuntimeEvalConfig
	if err := decoder.Decode(&config); err != nil {
		return strategyRuntimeEvalConfig{}, fmt.Errorf("decode strategy runtime evaluation config: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return strategyRuntimeEvalConfig{}, errors.New("strategy runtime evaluation config contains multiple JSON values")
		}
		return strategyRuntimeEvalConfig{}, fmt.Errorf("decode strategy runtime evaluation config trailer: %w", err)
	}
	return normalizeStrategyRuntimeEvalConfig(config)
}

func normalizeStrategyRuntimeEvalConfig(config strategyRuntimeEvalConfig) (strategyRuntimeEvalConfig, error) {
	config.Version = strings.TrimSpace(config.Version)
	config.ProfileSetVersion = strings.TrimSpace(config.ProfileSetVersion)
	if config.Version != strategyRuntimeEvalConfigVersion {
		return strategyRuntimeEvalConfig{}, fmt.Errorf("strategy runtime evaluation config version must be %q", strategyRuntimeEvalConfigVersion)
	}
	if len(config.ProfileSetVersion) > 128 || strings.ContainsAny(config.ProfileSetVersion, " \t\r\n") {
		return strategyRuntimeEvalConfig{}, errors.New("strategy runtime profile_set_version must be at most 128 non-whitespace characters")
	}
	if len(config.Templates) != len(strategyRuntimeTemplateSpecs) {
		return strategyRuntimeEvalConfig{}, fmt.Errorf("strategy runtime evaluation config requires exactly %d templates", len(strategyRuntimeTemplateSpecs))
	}

	seen := make(map[string]struct{}, len(config.Templates))
	for index := range config.Templates {
		candidate := &config.Templates[index]
		candidate.TemplateID = strings.TrimSpace(candidate.TemplateID)
		spec, ok := findStrategyRuntimeTemplateSpec(candidate.TemplateID)
		if !ok {
			return strategyRuntimeEvalConfig{}, fmt.Errorf("strategy runtime template %q is unsupported", candidate.TemplateID)
		}
		if _, duplicate := seen[candidate.TemplateID]; duplicate {
			return strategyRuntimeEvalConfig{}, fmt.Errorf("strategy runtime template %q is duplicated", candidate.TemplateID)
		}
		seen[candidate.TemplateID] = struct{}{}

		profiles := []*runtimeEvalProfileConfig{
			&candidate.SingleProfile,
			&candidate.ResearcherProfile,
			&candidate.DrafterProfile,
			&candidate.ReviewerProfile,
		}
		for profileIndex, profileConfig := range profiles {
			normalized, err := normalizeStrategyRuntimeProfile(config, *profileConfig)
			if err != nil {
				return strategyRuntimeEvalConfig{}, fmt.Errorf("strategy runtime template %s profile %d: %w", candidate.TemplateID, profileIndex, err)
			}
			*profileConfig = normalized
		}
		if err := validateStrategyRuntimeProfileScopes(*candidate, spec); err != nil {
			return strategyRuntimeEvalConfig{}, err
		}
	}
	if len(seen) != len(strategyRuntimeTemplateSpecs) {
		return strategyRuntimeEvalConfig{}, errors.New("strategy runtime evaluation config does not cover every required template")
	}

	first := config.Templates[0].SingleProfile
	base, err := normalizeRuntimeEvalConfig(runtimeEvalConfigFromStrategy(config, first))
	if err != nil {
		return strategyRuntimeEvalConfig{}, err
	}
	config.Provider = base.Provider
	config.BaseURL = base.BaseURL
	config.Model = base.Model
	config.CredentialEnv = base.CredentialEnv
	config.ReasoningMode = base.ReasoningMode
	config.PricingVersion = base.PricingVersion
	sort.Slice(config.Templates, func(i, j int) bool {
		return config.Templates[i].TemplateID < config.Templates[j].TemplateID
	})
	return config, nil
}

func normalizeStrategyRuntimeProfile(
	config strategyRuntimeEvalConfig,
	profileConfig runtimeEvalProfileConfig,
) (runtimeEvalProfileConfig, error) {
	normalized, err := normalizeRuntimeEvalConfig(runtimeEvalConfigFromStrategy(config, profileConfig))
	if err != nil {
		return runtimeEvalProfileConfig{}, err
	}
	return normalized.Profile, nil
}

func runtimeEvalConfigFromStrategy(
	config strategyRuntimeEvalConfig,
	profileConfig runtimeEvalProfileConfig,
) runtimeEvalConfig {
	return runtimeEvalConfig{
		Version:  runtimeEvalConfigVersion,
		Provider: config.Provider, BaseURL: config.BaseURL, Model: config.Model,
		CredentialEnv: config.CredentialEnv, ReasoningMode: config.ReasoningMode,
		ContextWindow:                config.ContextWindow,
		MaxOutputTokens:              config.MaxOutputTokens,
		InputMicrosPerMillionTokens:  config.InputMicrosPerMillionTokens,
		OutputMicrosPerMillionTokens: config.OutputMicrosPerMillionTokens,
		PricingVersion:               config.PricingVersion, ProviderTimeoutMS: config.ProviderTimeoutMS,
		Profile: profileConfig,
	}
}

func validateStrategyRuntimeProfileScopes(
	config strategyRuntimeEvalTemplateConfig,
	spec strategyRuntimeTemplateSpec,
) error {
	checks := []struct {
		label    string
		profile  runtimeEvalProfileConfig
		wantID   string
		wantTool []string
	}{
		{label: "single", profile: config.SingleProfile, wantID: spec.singleProfileID, wantTool: spec.researchTools},
		{label: "researcher", profile: config.ResearcherProfile, wantID: spec.researcherProfileID, wantTool: spec.researchTools},
		{label: "drafter", profile: config.DrafterProfile, wantID: spec.drafterProfileID},
		{label: "reviewer", profile: config.ReviewerProfile, wantID: spec.reviewerProfileID},
	}
	for _, check := range checks {
		if check.profile.ID != check.wantID {
			return fmt.Errorf("strategy runtime template %s %s profile_id must be %q", config.TemplateID, check.label, check.wantID)
		}
		if !sameStringSet(check.profile.AllowedTools, check.wantTool) {
			return fmt.Errorf("strategy runtime template %s %s tool scope must be %v", config.TemplateID, check.label, check.wantTool)
		}
	}
	return nil
}

func newLiveRuntimeStrategyExecutors(config strategyRuntimeEvalConfig) (runtimeStrategyExecutors, error) {
	config, err := normalizeStrategyRuntimeEvalConfig(config)
	if err != nil {
		return runtimeStrategyExecutors{}, err
	}
	settings := runtimeEvalConfigFromStrategy(config, config.Templates[0].SingleProfile).providerSettings()
	router, err := newRuntimeEvalProvider(settings)
	if err != nil {
		return runtimeStrategyExecutors{}, err
	}
	templates := make(map[string]runtimeStrategyTemplate, len(config.Templates))
	plannerTemplates := make([]agentStrategy.Template, 0, len(config.Templates))
	for _, candidate := range config.Templates {
		spec, _ := findStrategyRuntimeTemplateSpec(candidate.TemplateID)
		templates[candidate.TemplateID] = runtimeStrategyTemplate{
			spec: spec, single: runtimeEvalAgentProfile(candidate.SingleProfile),
			researcher: runtimeEvalAgentProfile(candidate.ResearcherProfile),
			drafter:    runtimeEvalAgentProfile(candidate.DrafterProfile),
			reviewer:   runtimeEvalAgentProfile(candidate.ReviewerProfile),
		}
		plannerTemplates = append(plannerTemplates, agentMultiRole.ResearchDraftTemplate(
			spec.templateID, spec.executionProfile, spec.researchCapabilityID,
			spec.draftCapabilityID, spec.researchTools,
		))
	}
	planner, err := agentStrategy.NewDeterministicPlanner(agentStrategy.Policy{
		Enabled: true, ExecutorAvailable: true, MinimumComplexityScore: 1,
		MaxRoles: 3, MaxParallelRoles: 1, MaxEstimatedLatency: strategyRuntimeMinimumCaseTime,
	}, plannerTemplates)
	if err != nil {
		return runtimeStrategyExecutors{}, err
	}
	configHash, err := hashStrategyRuntimeEvalConfig(config)
	if err != nil {
		return runtimeStrategyExecutors{}, err
	}
	common := runtimeStrategyAgentTaskExecutor{
		provider: config.Provider, model: config.Model,
		profileVersion: strategyProfileSetVersion(config), pricingVersion: config.PricingVersion,
		modelClient:   fixedRuntimeEvalModelClient{delegate: router, provider: config.Provider, model: config.Model},
		costEstimator: router, planner: planner, templates: templates,
	}
	single := common
	single.strategy = eval.AgentStrategySingle
	multi := common
	multi.strategy = eval.AgentStrategyMulti
	return runtimeStrategyExecutors{single: &single, multi: &multi, configHash: configHash}, nil
}

func hashStrategyRuntimeEvalConfig(config strategyRuntimeEvalConfig) (string, error) {
	config, err := normalizeStrategyRuntimeEvalConfig(config)
	if err != nil {
		return "", err
	}
	configHash, err := eval.HashCanonicalJSON(struct {
		Config          strategyRuntimeEvalConfig `json:"config"`
		ExecutorVersion string                    `json:"executor_version"`
	}{Config: config, ExecutorVersion: strategyRuntimeExecutorVersion})
	if err != nil {
		return "", fmt.Errorf("hash strategy runtime evaluation config: %w", err)
	}
	return configHash, nil
}

func strategyProfileSetVersion(config strategyRuntimeEvalConfig) string {
	if version := strings.TrimSpace(config.ProfileSetVersion); version != "" {
		return version
	}
	return "v1"
}

func (executor *runtimeStrategyAgentTaskExecutor) Descriptor() eval.AgentTaskExecutionDescriptor {
	if executor == nil {
		return eval.AgentTaskExecutionDescriptor{}
	}
	return eval.AgentTaskExecutionDescriptor{
		Kind: "runtime_live", Version: strategyRuntimeExecutorVersion, Strategy: executor.strategy,
		Provider: executor.provider, Model: executor.model,
		ProfileID: "agent.strategy-profile-set", ProfileVersion: executor.profileVersion,
		PricingVersion: executor.pricingVersion,
	}
}

func (executor *runtimeStrategyAgentTaskExecutor) Preflight(ctx context.Context) error {
	if executor == nil || executor.modelClient == nil {
		return errors.New("strategy runtime executor is not configured")
	}
	return preflightRuntimeEvalModel(ctx, executor.modelClient, executor.model)
}

func (executor *runtimeStrategyAgentTaskExecutor) Execute(
	ctx context.Context,
	sample eval.AgentTaskCase,
) (eval.AgentTaskExecution, error) {
	if executor == nil || executor.modelClient == nil || executor.costEstimator == nil {
		return eval.AgentTaskExecution{}, errors.New("strategy runtime executor is not configured")
	}
	template, ok := executor.templates[strings.TrimSpace(sample.StrategyTemplateID)]
	if !ok {
		return eval.AgentTaskExecution{}, fmt.Errorf("strategy runtime template %q is not configured", sample.StrategyTemplateID)
	}
	if executor.strategy == eval.AgentStrategySingle {
		return executor.executeSingle(ctx, sample, template)
	}
	if executor.strategy == eval.AgentStrategyMulti {
		return executor.executeMulti(ctx, sample, template)
	}
	return eval.AgentTaskExecution{}, fmt.Errorf("strategy runtime executor strategy %q is unsupported", executor.strategy)
}

func (executor *runtimeStrategyAgentTaskExecutor) executeSingle(
	ctx context.Context,
	sample eval.AgentTaskCase,
	template runtimeStrategyTemplate,
) (eval.AgentTaskExecution, error) {
	runContext := agentRuntime.RunContext{
		RunID: "agent-strategy-eval:single:" + sample.ID, UserID: 1, Mode: agentRuntime.Mode(sample.Mode),
		AgentProfileID: template.single.ID, AgentProfileVersion: template.single.Version,
		PromptTemplateID: template.single.Prompt.ID, PromptTemplateVersion: template.single.Prompt.Version,
		StartedAt: time.Now().UTC(), Budget: template.single.Budget,
	}
	runner := agentRuntime.NewReActRunner(
		executor.modelClient,
		runtimeEvalToolSandbox{sample: sample},
		nil,
		agentRuntime.WithCostEstimator(executor.costEstimator),
	)
	result, runErr := runner.Run(ctx, agentRuntime.RunRequest{
		Context: runContext, Model: executor.model, InitialToolChoice: agentRuntime.ToolChoiceRequired,
		Messages: []agentRuntime.Message{
			{Role: agentRuntime.RoleSystem, Content: template.single.Prompt.SystemPrompt},
			{Role: agentRuntime.RoleUser, Content: sample.Input},
		},
		Tools: template.single.FilterTools(runtimeEvalToolDefinitions(sample)),
	})
	execution := runtimeResultToAgentTaskExecution(result, runErr)
	if runErr != nil {
		return execution, fmt.Errorf("controlled single-agent strategy evaluation failed: %w", runErr)
	}
	return execution, nil
}

func (executor *runtimeStrategyAgentTaskExecutor) executeMulti(
	ctx context.Context,
	sample eval.AgentTaskCase,
	template runtimeStrategyTemplate,
) (eval.AgentTaskExecution, error) {
	plan, err := executor.planner.Plan(ctx, agentStrategy.Request{
		Query: sample.Input, ExecutionProfile: template.spec.executionProfile,
		CapabilityIDs: []string{template.spec.researchCapabilityID, template.spec.draftCapabilityID},
		Budget:        template.single.Budget, AllowedTools: append([]string(nil), template.single.AllowedTools...),
	})
	if err != nil {
		return eval.AgentTaskExecution{}, fmt.Errorf("plan controlled multi-role evaluation: %w", err)
	}
	if plan.SelectedStrategy != agentStrategy.KindMultiAgent {
		return eval.AgentTaskExecution{}, fmt.Errorf("controlled multi-role plan was not admitted: %s", plan.ReasonCode)
	}
	runner := agentRuntime.NewReActRunner(
		executor.modelClient,
		runtimeEvalToolSandbox{sample: sample},
		nil,
		agentRuntime.WithCostEstimator(executor.costEstimator),
	)
	handoff := agentMultiRole.EvidenceHandoffBuilderFunc(func(
		summary string,
		research agentRuntime.RunResult,
	) (string, error) {
		citations, collectErr := strategyRuntimeCitations(template.spec.requiredTool, research, sample.Evidence)
		if collectErr != nil {
			return "", collectErr
		}
		return agentMultiRole.EncodeEvidenceHandoff(summary, citations)
	})
	multiExecutor := agentMultiRole.NewExecutor(runner, nil)
	result, runErr := multiExecutor.Execute(ctx, agentMultiRole.Request{
		ParentContext: agentRuntime.RunContext{
			RunID: "agent-strategy-eval:multi:" + sample.ID, UserID: 1,
			Mode: agentRuntime.Mode(sample.Mode), AgentProfileID: "multi.runtime.aggregate",
			AgentProfileVersion: "v1", PromptTemplateID: "multi.runtime.aggregate",
			PromptTemplateVersion: "v1",
		},
		Plan: plan, Model: executor.model, Input: sample.Input,
		Tools: runtimeEvalToolDefinitions(sample), RequiredTool: template.spec.requiredTool,
		Profiles: agentMultiRole.Profiles{
			Parent: template.single, Researcher: template.researcher,
			Drafter: template.drafter, Reviewer: template.reviewer,
		},
		Handoff: handoff,
	})
	execution := runtimeResultToAgentTaskExecution(result.Aggregate, runErr)
	if runErr != nil {
		return execution, fmt.Errorf("controlled multi-role strategy evaluation failed: %w", runErr)
	}
	return execution, nil
}

func strategyRuntimeCitations(
	requiredTool string,
	result agentRuntime.RunResult,
	contract *eval.AgentTaskEvidenceContract,
) ([]agentMultiRole.Citation, error) {
	citations := make([]agentMultiRole.Citation, 0, 4)
	for _, step := range result.Steps {
		for _, action := range step.Actions {
			if action.Type != agentRuntime.ActionToolCall {
				continue
			}
			for _, observation := range step.Observations {
				if observation.ActionID != action.ID || observation.IsError || len(observation.StructuredContent) == 0 {
					continue
				}
				switch action.Name {
				case "hybrid_search_tweets":
					var evidence agentEvidence.PlatformTweetSearchResult
					if json.Unmarshal(observation.StructuredContent, &evidence) != nil || evidence.Schema != agentEvidence.PlatformTweetSearchSchema {
						continue
					}
					for _, item := range evidence.Items {
						if _, err := strconv.ParseUint(strings.TrimSpace(item.TweetID), 10, 64); err != nil {
							continue
						}
						citationID := strategyRuntimeCitationID(contract, item.TweetID, "")
						if citationID == "" {
							citationID = "platform_tweet:" + item.TweetID
						}
						citations = append(citations, agentMultiRole.Citation{
							CitationID: citationID, SourceType: "platform_tweet",
							SourceID: item.TweetID, URL: "/tweet/" + item.TweetID,
							Snippet: agentMultiRole.BoundedRunes(item.Content, 280),
						})
					}
				case "web_search":
					var evidence agentEvidence.WebSearchResult
					if json.Unmarshal(observation.StructuredContent, &evidence) != nil ||
						evidence.Schema != agentEvidence.WebSearchSchema || strings.TrimSpace(evidence.Provider) == "" {
						continue
					}
					for _, item := range evidence.Items {
						if strings.TrimSpace(item.URL) == "" {
							continue
						}
						citationID := strategyRuntimeCitationID(contract, strconv.Itoa(item.Rank), item.URL)
						if citationID == "" {
							citationID = "web_page:" + strconv.Itoa(item.Rank)
						}
						citations = append(citations, agentMultiRole.Citation{
							CitationID: citationID, SourceType: "web_page",
							SourceID: strconv.Itoa(item.Rank), URL: item.URL, Title: item.Title,
							Snippet: agentMultiRole.BoundedRunes(item.Snippet, 280),
						})
					}
				}
			}
		}
	}
	if len(citations) == 0 && contract != nil && contract.Status == eval.AgentTaskEvidenceInsufficient {
		return []agentMultiRole.Citation{{
			CitationID: "no-evidence", SourceType: "control", SourceID: "none",
			Title: "No evidence returned", Snippet: "The required search returned no evidence. State that reliable evidence is insufficient and do not add claims from prior knowledge.",
		}}, nil
	}
	if len(citations) == 0 {
		return nil, fmt.Errorf("%w: %s returned no valid structured citations", agentMultiRole.ErrRequiredToolEvidence, requiredTool)
	}
	return citations, nil
}

func strategyRuntimeCitationID(contract *eval.AgentTaskEvidenceContract, sourceID, sourceURL string) string {
	if contract == nil {
		return ""
	}
	for _, item := range contract.Items {
		if sourceURL != "" && item.URL == sourceURL {
			return item.CitationID
		}
		if sourceURL == "" && item.SourceID == sourceID {
			return item.CitationID
		}
	}
	return ""
}

func findStrategyRuntimeTemplateSpec(templateID string) (strategyRuntimeTemplateSpec, bool) {
	for _, spec := range strategyRuntimeTemplateSpecs {
		if spec.templateID == templateID {
			return spec, true
		}
	}
	return strategyRuntimeTemplateSpec{}, false
}

func sameStringSet(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
