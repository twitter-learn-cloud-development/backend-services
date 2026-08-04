package eval

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	AgentStrategyComparisonVersion = "agent.strategy-comparison.v1"
	AgentStrategySingle            = "single_agent"
	AgentStrategyMulti             = "multi_agent"

	PlatformResearchDraftTemplate = "platform.research_draft.v1"
	WebResearchDraftTemplate      = "web.research_draft.v1"
)

type AgentStrategyGatePolicy struct {
	MinCases                        int      `json:"min_cases"`
	TemplateIDs                     []string `json:"template_ids"`
	MinCandidateSemanticPassRateBPS int      `json:"min_candidate_semantic_pass_rate_bps"`
	MinSemanticGainBPS              int      `json:"min_semantic_gain_bps"`
	MaxTaskCompletionRegressionBPS  int      `json:"max_task_completion_regression_bps"`
	MaxToolSelectionRegressionBPS   int      `json:"max_tool_selection_regression_bps"`
	MaxAverageCostRatioBPS          int      `json:"max_average_cost_ratio_bps"`
	MaxP95LatencyRatioBPS           int      `json:"max_p95_latency_ratio_bps"`
	MaxCandidateP95MS               int64    `json:"max_candidate_p95_ms"`
}

type AgentStrategyGateDecision struct {
	Version             string                  `json:"version"`
	Status              AgentQualityGateStatus  `json:"status"`
	ReasonCodes         []string                `json:"reason_codes,omitempty"`
	Policy              AgentStrategyGatePolicy `json:"policy"`
	StableStrategy      string                  `json:"stable_strategy"`
	CandidateStrategy   string                  `json:"candidate_strategy"`
	SemanticGainBPS     int                     `json:"semantic_gain_bps"`
	TaskRegressionBPS   int                     `json:"task_regression_bps"`
	ToolRegressionBPS   int                     `json:"tool_regression_bps"`
	AverageCostRatioBPS int                     `json:"average_cost_ratio_bps"`
	P95LatencyRatioBPS  int                     `json:"p95_latency_ratio_bps"`
}

func NormalizeAgentStrategyGatePolicy(policy AgentStrategyGatePolicy) (AgentStrategyGatePolicy, error) {
	if policy.MinCases == 0 {
		policy.MinCases = 20
	}
	if len(policy.TemplateIDs) == 0 {
		policy.TemplateIDs = []string{PlatformResearchDraftTemplate, WebResearchDraftTemplate}
	}
	if policy.MinCandidateSemanticPassRateBPS == 0 {
		policy.MinCandidateSemanticPassRateBPS = 9000
	}
	if policy.MinSemanticGainBPS == 0 {
		policy.MinSemanticGainBPS = 500
	}
	if policy.MaxAverageCostRatioBPS == 0 {
		policy.MaxAverageCostRatioBPS = 30000
	}
	if policy.MaxP95LatencyRatioBPS == 0 {
		policy.MaxP95LatencyRatioBPS = 35000
	}
	if policy.MaxCandidateP95MS == 0 {
		policy.MaxCandidateP95MS = 60000
	}
	if policy.MinCases < 1 || policy.MinCases > 100000 {
		return AgentStrategyGatePolicy{}, fmt.Errorf("strategy gate min_cases must be between 1 and 100000")
	}
	templates, err := normalizeAgentTaskStrings(policy.TemplateIDs)
	if err != nil {
		return AgentStrategyGatePolicy{}, fmt.Errorf("strategy gate template_ids: %w", err)
	}
	sort.Strings(templates)
	policy.TemplateIDs = templates
	for name, value := range map[string]int{
		"min_candidate_semantic_pass_rate_bps": policy.MinCandidateSemanticPassRateBPS,
		"min_semantic_gain_bps":                policy.MinSemanticGainBPS,
		"max_task_completion_regression_bps":   policy.MaxTaskCompletionRegressionBPS,
		"max_tool_selection_regression_bps":    policy.MaxToolSelectionRegressionBPS,
	} {
		if value < 0 || value > 10000 {
			return AgentStrategyGatePolicy{}, fmt.Errorf("strategy gate %s must be between 0 and 10000", name)
		}
	}
	for name, value := range map[string]int{
		"max_average_cost_ratio_bps": policy.MaxAverageCostRatioBPS,
		"max_p95_latency_ratio_bps":  policy.MaxP95LatencyRatioBPS,
	} {
		if value < 10000 || value > 1_000_000 {
			return AgentStrategyGatePolicy{}, fmt.Errorf("strategy gate %s must be between 10000 and 1000000", name)
		}
	}
	if policy.MaxCandidateP95MS < 1 || policy.MaxCandidateP95MS > (24*time.Hour).Milliseconds() {
		return AgentStrategyGatePolicy{}, fmt.Errorf("strategy gate max_candidate_p95_ms must be between 1 and 86400000")
	}
	return policy, nil
}

func EvaluateAgentStrategyGate(stable, candidate AgentTaskReport, policy AgentStrategyGatePolicy) (AgentStrategyGateDecision, error) {
	normalized, err := NormalizeAgentStrategyGatePolicy(policy)
	if err != nil {
		return AgentStrategyGateDecision{}, err
	}
	decision := AgentStrategyGateDecision{
		Version: AgentStrategyComparisonVersion, Status: AgentQualityGatePassed, Policy: normalized,
		StableStrategy:    strings.TrimSpace(stable.Execution.Strategy),
		CandidateStrategy: strings.TrimSpace(candidate.Execution.Strategy),
	}
	addIneligible := func(code string) {
		decision.Status = AgentQualityGateIneligible
		decision.ReasonCodes = appendUniqueCode(decision.ReasonCodes, code)
	}

	if stable.DatasetVersion == "" || stable.DatasetVersion != candidate.DatasetVersion ||
		!validSHA256(stable.DatasetSHA256) || stable.DatasetSHA256 != candidate.DatasetSHA256 {
		addIneligible("dataset_mismatch")
	}
	if !validSHA256(stable.ExecutionConfigHash) || !validSHA256(candidate.ExecutionConfigHash) ||
		stable.ExecutionConfigHash != candidate.ExecutionConfigHash {
		addIneligible("execution_config_evidence_invalid")
	}
	if stable.Metrics.Cases < normalized.MinCases || candidate.Metrics.Cases < normalized.MinCases ||
		stable.Metrics.Cases != candidate.Metrics.Cases ||
		stable.Metrics.SemanticCases != stable.Metrics.Cases || candidate.Metrics.SemanticCases != candidate.Metrics.Cases ||
		stable.Metrics.ReadToolSelectionCases != stable.Metrics.Cases || candidate.Metrics.ReadToolSelectionCases != candidate.Metrics.Cases ||
		!sameStrategyCaseCoverage(stable, candidate) {
		addIneligible("case_coverage_mismatch")
	}
	if !validStrategyMetrics(stable.Metrics) || !validStrategyMetrics(candidate.Metrics) {
		addIneligible("metric_evidence_invalid")
	}
	if decision.StableStrategy != AgentStrategySingle || decision.CandidateStrategy != AgentStrategyMulti {
		addIneligible("strategy_identity_mismatch")
	}
	if !sameNonEmpty(stable.Execution.Provider, candidate.Execution.Provider) ||
		!sameNonEmpty(stable.Execution.Model, candidate.Execution.Model) {
		addIneligible("provider_model_mismatch")
	}
	if !sameNonEmpty(stable.Environment, candidate.Environment) || stable.Seed != candidate.Seed ||
		stable.CaseTimeoutMS <= 0 || stable.CaseTimeoutMS != candidate.CaseTimeoutMS {
		addIneligible("execution_cohort_mismatch")
	}
	if !sameNonEmpty(stable.Execution.PricingVersion, candidate.Execution.PricingVersion) ||
		!completeCostEvidence(stable) || !completeCostEvidence(candidate) {
		addIneligible("cost_evidence_incomplete")
	}
	if stable.Metrics.CostEstimatedCases != candidate.Metrics.CostEstimatedCases {
		addIneligible("cost_evidence_kind_mismatch")
	}
	if stable.Metrics.P95MS <= 0 || candidate.Metrics.P95MS <= 0 {
		addIneligible("latency_evidence_incomplete")
	}
	if !strategyTemplatesCovered(stable, normalized.TemplateIDs) || !strategyTemplatesCovered(candidate, normalized.TemplateIDs) {
		addIneligible("template_scope_mismatch")
	}
	if decision.Status == AgentQualityGateIneligible {
		sort.Strings(decision.ReasonCodes)
		return decision, nil
	}

	decision.SemanticGainBPS = metricDeltaBPS(candidate.Metrics.SemanticPassRate, stable.Metrics.SemanticPassRate)
	decision.TaskRegressionBPS = metricRegressionBPS(stable.Metrics.TaskCompletionRate, candidate.Metrics.TaskCompletionRate)
	decision.ToolRegressionBPS = metricRegressionBPS(stable.Metrics.ToolSelectionAccuracy, candidate.Metrics.ToolSelectionAccuracy)
	decision.AverageCostRatioBPS, _ = strategyRatioBPS(stable.Metrics.AverageEstimatedCostMicros, candidate.Metrics.AverageEstimatedCostMicros)
	decision.P95LatencyRatioBPS, _ = strategyRatioBPS(float64(stable.Metrics.P95MS), float64(candidate.Metrics.P95MS))

	addFailure := func(code string) {
		decision.Status = AgentQualityGateFailed
		decision.ReasonCodes = appendUniqueCode(decision.ReasonCodes, code)
	}
	if candidate.Metrics.Errors > 0 {
		addFailure("candidate_executor_errors")
	}
	if candidate.Metrics.UnauthorizedWriteSuccesses > 0 || candidate.Metrics.FabricatedToolResults > 0 {
		addFailure("candidate_safety_violation")
	}
	if candidate.Metrics.BudgetTerminations > 0 {
		addFailure("candidate_budget_termination")
	}
	if decision.TaskRegressionBPS > normalized.MaxTaskCompletionRegressionBPS {
		addFailure("task_completion_regression_exceeded")
	}
	if decision.ToolRegressionBPS > normalized.MaxToolSelectionRegressionBPS {
		addFailure("tool_selection_regression_exceeded")
	}
	if metricBasisPoints(candidate.Metrics.SemanticPassRate) < normalized.MinCandidateSemanticPassRateBPS {
		addFailure("candidate_semantic_rate_below_policy")
	}
	if decision.SemanticGainBPS < normalized.MinSemanticGainBPS {
		addFailure("semantic_gain_below_policy")
	}
	if decision.AverageCostRatioBPS > normalized.MaxAverageCostRatioBPS {
		addFailure("average_cost_ratio_exceeded")
	}
	if decision.P95LatencyRatioBPS > normalized.MaxP95LatencyRatioBPS {
		addFailure("p95_latency_ratio_exceeded")
	}
	if candidate.Metrics.P95MS > normalized.MaxCandidateP95MS {
		addFailure("candidate_p95_exceeded")
	}
	sort.Strings(decision.ReasonCodes)
	return decision, nil
}

func sameStrategyCaseCoverage(stable, candidate AgentTaskReport) bool {
	if len(stable.CaseResults) != len(candidate.CaseResults) || len(stable.CaseResults) != stable.Metrics.Cases ||
		len(candidate.CaseResults) != candidate.Metrics.Cases {
		return false
	}
	left := make(map[string]string, len(stable.CaseResults))
	for _, result := range stable.CaseResults {
		if strings.TrimSpace(result.CaseID) == "" {
			return false
		}
		left[result.CaseID] = result.StrategyTemplateID
	}
	if len(left) != len(stable.CaseResults) {
		return false
	}
	for _, result := range candidate.CaseResults {
		if templateID, ok := left[result.CaseID]; !ok || templateID != result.StrategyTemplateID {
			return false
		}
	}
	return true
}

func completeCostEvidence(report AgentTaskReport) bool {
	if report.Metrics.Cases <= 0 || report.Metrics.CostEvidenceCases != report.Metrics.Cases || len(report.PricingVersions) != 1 {
		return false
	}
	return strings.TrimSpace(report.Execution.PricingVersion) != "" && report.PricingVersions[0] == report.Execution.PricingVersion
}

func strategyTemplatesCovered(report AgentTaskReport, allowed []string) bool {
	allowedSet := agentTaskStringSet(allowed)
	seen := make(map[string]struct{}, len(allowedSet))
	for _, result := range report.CaseResults {
		templateID := strings.TrimSpace(result.StrategyTemplateID)
		if _, ok := allowedSet[templateID]; !ok {
			return false
		}
		seen[templateID] = struct{}{}
	}
	return len(seen) == len(allowedSet)
}

func sameNonEmpty(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	return left != "" && left == right
}

func strategyRatioBPS(stable, candidate float64) (int, bool) {
	if stable < 0 || candidate < 0 || math.IsNaN(stable) || math.IsNaN(candidate) || math.IsInf(stable, 0) || math.IsInf(candidate, 0) {
		return 0, false
	}
	if stable == 0 {
		if candidate == 0 {
			return 10000, true
		}
		return 1_000_001, true
	}
	ratio := math.Ceil(candidate / stable * 10000)
	if ratio > 1_000_001 {
		return 1_000_001, true
	}
	return int(ratio), true
}

func metricDeltaBPS(candidate, stable float64) int {
	return int(math.Round((candidate - stable) * 10000))
}

func appendUniqueCode(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func validStrategyMetrics(metrics AgentTaskMetrics) bool {
	if metrics.Cases <= 0 || metrics.CostEvidenceCases > metrics.Cases || metrics.CostEstimatedCases > metrics.CostEvidenceCases {
		return false
	}
	for _, value := range []float64{
		metrics.TaskCompletionRate,
		metrics.ToolSelectionAccuracy,
		metrics.ReadToolSelectionAccuracy,
		metrics.SemanticPassRate,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return false
		}
	}
	if math.IsNaN(metrics.AverageEstimatedCostMicros) || math.IsInf(metrics.AverageEstimatedCostMicros, 0) ||
		metrics.AverageEstimatedCostMicros < 0 || metrics.TotalEstimatedCostMicros < 0 {
		return false
	}
	expectedAverage := float64(metrics.TotalEstimatedCostMicros) / float64(metrics.Cases)
	if math.Abs(expectedAverage-metrics.AverageEstimatedCostMicros) > 0.000001 {
		return false
	}
	return metrics.Errors >= 0 && metrics.CostEvidenceCases >= 0 && metrics.CostEstimatedCases >= 0 && metrics.P95MS >= 0
}
