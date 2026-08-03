package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const PlanVersionV1 = "agent.execution_strategy.v1"

type Kind string

const (
	KindSingleAgent Kind = "single_agent"
	KindMultiAgent  Kind = "multi_agent"
)

type Decision string

const (
	DecisionSelected Decision = "selected"
	DecisionFallback Decision = "fallback"
	DecisionDisabled Decision = "disabled"
)

type ReasonCode string

const (
	ReasonSingleCapabilityScope      ReasonCode = "single_capability_scope"
	ReasonSingleComplexityBelowLimit ReasonCode = "single_complexity_below_threshold"
	ReasonMultiFeatureDisabled       ReasonCode = "multi_feature_disabled"
	ReasonMultiRoleLimit             ReasonCode = "multi_role_limit"
	ReasonMultiParallelLimit         ReasonCode = "multi_parallel_limit"
	ReasonMultiToolScopeUnavailable  ReasonCode = "multi_tool_scope_unavailable"
	ReasonMultiBudgetUnbounded       ReasonCode = "multi_budget_unbounded"
	ReasonMultiStepBudget            ReasonCode = "multi_step_budget_insufficient"
	ReasonMultiTokenBudget           ReasonCode = "multi_token_budget_insufficient"
	ReasonMultiCostBudget            ReasonCode = "multi_cost_budget_insufficient"
	ReasonMultiLatencyBudget         ReasonCode = "multi_latency_budget_insufficient"
	ReasonMultiPolicyLatency         ReasonCode = "multi_policy_latency_exceeded"
	ReasonMultiExecutorUnavailable   ReasonCode = "multi_executor_unavailable"
	ReasonMultiAdmitted              ReasonCode = "multi_admitted"
)

type ComplexityClass string

const (
	ComplexityLow    ComplexityClass = "low"
	ComplexityMedium ComplexityClass = "medium"
	ComplexityHigh   ComplexityClass = "high"
)

// RolePlan is an immutable, sanitized role budget and scope. AllowedTools is
// planning evidence only; the execution Profile and governed ToolExecutor
// remain the authorization boundary.
type RolePlan struct {
	RoleID                 string   `bson:"role_id" json:"role_id"`
	CapabilityIDs          []string `bson:"capability_ids,omitempty" json:"capability_ids,omitempty"`
	AllowedTools           []string `bson:"allowed_tools,omitempty" json:"allowed_tools,omitempty"`
	MaxSteps               int      `bson:"max_steps" json:"max_steps"`
	MaxTotalTokens         int      `bson:"max_total_tokens" json:"max_total_tokens"`
	MaxEstimatedCostMicros int64    `bson:"max_estimated_cost_micros" json:"max_estimated_cost_micros"`
	TimeoutMillis          int64    `bson:"timeout_millis" json:"timeout_millis"`
}

// Plan contains only bounded enums, budgets, identifiers and digests. It must
// never include the raw query, prompts, credentials or tool arguments.
type Plan struct {
	Version                string          `bson:"version" json:"version"`
	TemplateID             string          `bson:"template_id,omitempty" json:"template_id,omitempty"`
	CandidateStrategy      Kind            `bson:"candidate_strategy" json:"candidate_strategy"`
	SelectedStrategy       Kind            `bson:"selected_strategy" json:"selected_strategy"`
	Decision               Decision        `bson:"decision" json:"decision"`
	ReasonCode             ReasonCode      `bson:"reason_code" json:"reason_code"`
	ComplexityScore        int             `bson:"complexity_score" json:"complexity_score"`
	ComplexityClass        ComplexityClass `bson:"complexity_class" json:"complexity_class"`
	ComplexitySignals      []string        `bson:"complexity_signals,omitempty" json:"complexity_signals,omitempty"`
	EstimatedLatencyMillis int64           `bson:"estimated_latency_millis" json:"estimated_latency_millis"`
	EstimatedTotalTokens   int             `bson:"estimated_total_tokens" json:"estimated_total_tokens"`
	EstimatedCostMicros    int64           `bson:"estimated_cost_micros" json:"estimated_cost_micros"`
	MaxParallelRoles       int             `bson:"max_parallel_roles" json:"max_parallel_roles"`
	Roles                  []RolePlan      `bson:"roles,omitempty" json:"roles,omitempty"`
	ProfileSetAnchor       string          `bson:"profile_set_anchor,omitempty" json:"profile_set_anchor,omitempty"`
	ProfileSetVersion      string          `bson:"profile_set_version,omitempty" json:"profile_set_version,omitempty"`
	PlanDigest             string          `bson:"plan_digest" json:"plan_digest"`
}

type RoleTemplate struct {
	RoleID              string
	CapabilityIDs       []string
	AllowedTools        []string
	MaxSteps            int
	RequiredTotalTokens int
	RequiredCostMicros  int64
	EstimatedLatency    time.Duration
}

type Template struct {
	ID                    string
	ExecutionProfile      string
	RequiredCapabilityIDs []string
	MaxParallelRoles      int
	Roles                 []RoleTemplate
}

type Policy struct {
	Enabled                bool
	ExecutorAvailable      bool
	MinimumComplexityScore int
	MaxRoles               int
	MaxParallelRoles       int
	MaxEstimatedLatency    time.Duration
}

type Request struct {
	Query            string
	ExecutionProfile string
	CapabilityIDs    []string
	Budget           agentRuntime.Budget
	AllowedTools     []string
}

type Planner interface {
	Plan(context.Context, Request) (Plan, error)
}

type DeterministicPlanner struct {
	policy    Policy
	templates map[string]Template
}

func NewDeterministicPlanner(policy Policy, templates []Template) (*DeterministicPlanner, error) {
	if policy.MinimumComplexityScore <= 0 {
		policy.MinimumComplexityScore = 6
	}
	if policy.MinimumComplexityScore > 10 {
		return nil, errors.New("multi-agent minimum complexity score cannot exceed 10")
	}
	if policy.MaxRoles <= 0 {
		policy.MaxRoles = 3
	}
	if policy.MaxParallelRoles <= 0 {
		policy.MaxParallelRoles = 1
	}
	if policy.MaxEstimatedLatency < 0 {
		return nil, errors.New("multi-agent maximum estimated latency cannot be negative")
	}

	planner := &DeterministicPlanner{
		policy:    policy,
		templates: make(map[string]Template, len(templates)),
	}
	for index, candidate := range templates {
		normalized, err := normalizeTemplate(candidate)
		if err != nil {
			return nil, fmt.Errorf("multi-agent template %d: %w", index, err)
		}
		key := templateKey(normalized.ExecutionProfile, normalized.RequiredCapabilityIDs)
		if _, exists := planner.templates[key]; exists {
			return nil, fmt.Errorf("duplicate multi-agent template for %q", key)
		}
		planner.templates[key] = normalized
	}
	return planner, nil
}

func (p *DeterministicPlanner) Plan(ctx context.Context, request Request) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	request.Query = strings.TrimSpace(request.Query)
	request.ExecutionProfile = strings.TrimSpace(request.ExecutionProfile)
	request.CapabilityIDs = uniqueSorted(request.CapabilityIDs)
	request.AllowedTools = uniqueSorted(request.AllowedTools)
	if request.Query == "" || request.ExecutionProfile == "" || len(request.CapabilityIDs) == 0 {
		return Plan{}, errors.New("agent execution strategy request is incomplete")
	}

	score, signals := complexityScore(request.Query, len(request.CapabilityIDs))
	plan := Plan{
		Version:           PlanVersionV1,
		CandidateStrategy: KindSingleAgent,
		SelectedStrategy:  KindSingleAgent,
		Decision:          DecisionSelected,
		ReasonCode:        ReasonSingleCapabilityScope,
		ComplexityScore:   score,
		ComplexityClass:   classifyComplexity(score),
		ComplexitySignals: signals,
	}
	template, eligible := p.templates[templateKey(request.ExecutionProfile, request.CapabilityIDs)]
	if !eligible {
		return finalizePlan(plan)
	}
	if score < p.policy.MinimumComplexityScore {
		plan.ReasonCode = ReasonSingleComplexityBelowLimit
		return finalizePlan(plan)
	}

	plan.TemplateID = template.ID
	plan.CandidateStrategy = KindMultiAgent
	plan.Decision = DecisionFallback
	plan.Roles = rolePlans(template.Roles)
	plan.MaxParallelRoles = template.MaxParallelRoles
	for _, role := range plan.Roles {
		plan.EstimatedLatencyMillis += role.TimeoutMillis
		plan.EstimatedTotalTokens += role.MaxTotalTokens
		plan.EstimatedCostMicros += role.MaxEstimatedCostMicros
	}
	if !p.policy.Enabled {
		plan.Decision = DecisionDisabled
		plan.ReasonCode = ReasonMultiFeatureDisabled
		return finalizePlan(plan)
	}
	if len(plan.Roles) > p.policy.MaxRoles {
		plan.ReasonCode = ReasonMultiRoleLimit
		return finalizePlan(plan)
	}
	if plan.MaxParallelRoles > p.policy.MaxParallelRoles {
		plan.ReasonCode = ReasonMultiParallelLimit
		return finalizePlan(plan)
	}
	if !roleToolsAreSubset(plan.Roles, request.AllowedTools) {
		plan.ReasonCode = ReasonMultiToolScopeUnavailable
		return finalizePlan(plan)
	}
	if request.Budget.MaxSteps <= 0 || request.Budget.MaxTotalTokens <= 0 ||
		request.Budget.MaxEstimatedCostMicros <= 0 || request.Budget.Timeout <= 0 {
		plan.ReasonCode = ReasonMultiBudgetUnbounded
		return finalizePlan(plan)
	}
	if totalRoleSteps(plan.Roles) > request.Budget.MaxSteps {
		plan.ReasonCode = ReasonMultiStepBudget
		return finalizePlan(plan)
	}
	if plan.EstimatedTotalTokens > request.Budget.MaxTotalTokens {
		plan.ReasonCode = ReasonMultiTokenBudget
		return finalizePlan(plan)
	}
	if plan.EstimatedCostMicros > request.Budget.MaxEstimatedCostMicros {
		plan.ReasonCode = ReasonMultiCostBudget
		return finalizePlan(plan)
	}
	estimatedLatency := time.Duration(plan.EstimatedLatencyMillis) * time.Millisecond
	if estimatedLatency > request.Budget.Timeout {
		plan.ReasonCode = ReasonMultiLatencyBudget
		return finalizePlan(plan)
	}
	if p.policy.MaxEstimatedLatency > 0 && estimatedLatency > p.policy.MaxEstimatedLatency {
		plan.ReasonCode = ReasonMultiPolicyLatency
		return finalizePlan(plan)
	}
	if !p.policy.ExecutorAvailable {
		plan.ReasonCode = ReasonMultiExecutorUnavailable
		return finalizePlan(plan)
	}

	plan.SelectedStrategy = KindMultiAgent
	plan.Decision = DecisionSelected
	plan.ReasonCode = ReasonMultiAdmitted
	return finalizePlan(plan)
}

func ClonePlan(source Plan) Plan {
	clone := source
	clone.ComplexitySignals = append([]string(nil), source.ComplexitySignals...)
	clone.Roles = make([]RolePlan, 0, len(source.Roles))
	for _, role := range source.Roles {
		role.CapabilityIDs = append([]string(nil), role.CapabilityIDs...)
		role.AllowedTools = append([]string(nil), role.AllowedTools...)
		clone.Roles = append(clone.Roles, role)
	}
	return clone
}

func ValidatePlan(plan Plan) error {
	if strings.TrimSpace(plan.Version) != PlanVersionV1 ||
		!validKind(plan.CandidateStrategy) || !validKind(plan.SelectedStrategy) ||
		!validDecision(plan.Decision) || strings.TrimSpace(string(plan.ReasonCode)) == "" {
		return errors.New("agent execution strategy plan identity is invalid")
	}
	if plan.ComplexityScore < 0 || plan.ComplexityScore > 10 ||
		plan.ComplexityClass != classifyComplexity(plan.ComplexityScore) {
		return errors.New("agent execution strategy complexity evidence is invalid")
	}
	if plan.EstimatedLatencyMillis < 0 || plan.EstimatedTotalTokens < 0 ||
		plan.EstimatedCostMicros < 0 || plan.MaxParallelRoles < 0 {
		return errors.New("agent execution strategy estimates cannot be negative")
	}
	if err := validateProfileSetBinding(plan.ProfileSetAnchor, plan.ProfileSetVersion); err != nil {
		return err
	}
	seenRoles := make(map[string]struct{}, len(plan.Roles))
	for _, role := range plan.Roles {
		roleID := strings.TrimSpace(role.RoleID)
		if roleID == "" || role.MaxSteps <= 0 || role.MaxTotalTokens <= 0 ||
			role.MaxEstimatedCostMicros <= 0 || role.TimeoutMillis <= 0 {
			return errors.New("agent execution strategy role evidence is incomplete")
		}
		if _, exists := seenRoles[roleID]; exists {
			return errors.New("agent execution strategy role IDs must be unique")
		}
		seenRoles[roleID] = struct{}{}
	}
	expected, err := digestPlan(plan)
	if err != nil {
		return err
	}
	if strings.TrimSpace(plan.PlanDigest) == "" || plan.PlanDigest != expected {
		return errors.New("agent execution strategy plan digest is invalid")
	}
	return nil
}

// BindProfileSet pins a coordinated profile version into the signed plan
// evidence after deterministic strategy admission and before execution.
func BindProfileSet(plan Plan, anchorID, version string) (Plan, error) {
	if err := ValidatePlan(plan); err != nil {
		return Plan{}, fmt.Errorf("bind profile set to invalid plan: %w", err)
	}
	plan.ProfileSetAnchor = strings.TrimSpace(anchorID)
	plan.ProfileSetVersion = strings.TrimSpace(version)
	return finalizePlan(plan)
}

func finalizePlan(plan Plan) (Plan, error) {
	digest, err := digestPlan(plan)
	if err != nil {
		return Plan{}, err
	}
	plan.PlanDigest = digest
	if err := ValidatePlan(plan); err != nil {
		return Plan{}, err
	}
	return ClonePlan(plan), nil
}

func digestPlan(plan Plan) (string, error) {
	canonical := ClonePlan(plan)
	canonical.PlanDigest = ""
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal agent execution strategy plan: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func validateProfileSetBinding(anchorID, version string) error {
	anchorID = strings.TrimSpace(anchorID)
	version = strings.TrimSpace(version)
	if (anchorID == "") != (version == "") {
		return errors.New("agent execution strategy profile set binding is incomplete")
	}
	if len(anchorID) > 128 || len(version) > 128 || strings.ContainsAny(anchorID, " \t\r\n") ||
		strings.ContainsAny(version, " \t\r\n") {
		return errors.New("agent execution strategy profile set binding is invalid")
	}
	return nil
}

func normalizeTemplate(template Template) (Template, error) {
	template.ID = strings.TrimSpace(template.ID)
	template.ExecutionProfile = strings.TrimSpace(template.ExecutionProfile)
	template.RequiredCapabilityIDs = uniqueSorted(template.RequiredCapabilityIDs)
	if template.ID == "" || template.ExecutionProfile == "" || len(template.RequiredCapabilityIDs) == 0 ||
		template.MaxParallelRoles <= 0 || len(template.Roles) == 0 {
		return Template{}, errors.New("multi-agent template identity and roles are required")
	}
	seenRoles := make(map[string]struct{}, len(template.Roles))
	roles := make([]RoleTemplate, 0, len(template.Roles))
	for _, role := range template.Roles {
		role.RoleID = strings.TrimSpace(role.RoleID)
		role.CapabilityIDs = uniqueSorted(role.CapabilityIDs)
		role.AllowedTools = uniqueSorted(role.AllowedTools)
		if role.RoleID == "" || role.MaxSteps <= 0 || role.RequiredTotalTokens <= 0 ||
			role.RequiredCostMicros <= 0 || role.EstimatedLatency <= 0 {
			return Template{}, errors.New("multi-agent role template is incomplete")
		}
		if _, exists := seenRoles[role.RoleID]; exists {
			return Template{}, fmt.Errorf("duplicate multi-agent role %q", role.RoleID)
		}
		seenRoles[role.RoleID] = struct{}{}
		roles = append(roles, role)
	}
	if template.MaxParallelRoles > len(roles) {
		return Template{}, errors.New("multi-agent template parallelism exceeds role count")
	}
	template.Roles = roles
	return template, nil
}

func rolePlans(templates []RoleTemplate) []RolePlan {
	roles := make([]RolePlan, 0, len(templates))
	for _, template := range templates {
		roles = append(roles, RolePlan{
			RoleID: template.RoleID, CapabilityIDs: append([]string(nil), template.CapabilityIDs...),
			AllowedTools: append([]string(nil), template.AllowedTools...), MaxSteps: template.MaxSteps,
			MaxTotalTokens: template.RequiredTotalTokens, MaxEstimatedCostMicros: template.RequiredCostMicros,
			TimeoutMillis: template.EstimatedLatency.Milliseconds(),
		})
	}
	return roles
}

func roleToolsAreSubset(roles []RolePlan, allowed []string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	for _, role := range roles {
		for _, name := range role.AllowedTools {
			if _, ok := allowedSet[name]; !ok {
				return false
			}
		}
	}
	return true
}

func totalRoleSteps(roles []RolePlan) int {
	total := 0
	for _, role := range roles {
		total += role.MaxSteps
	}
	return total
}

func complexityScore(query string, capabilityCount int) (int, []string) {
	query = strings.ToLower(strings.TrimSpace(query))
	score := 0
	signals := make([]string, 0, 6)
	if capabilityCount > 1 {
		score += 2
		signals = append(signals, "capability_composition")
	}
	if containsAny(query, analysisTerms) {
		score += 2
		signals = append(signals, "analysis_request")
	}
	if containsAny(query, multipleOutputTerms) {
		score += 2
		signals = append(signals, "multiple_outputs")
	}
	if containsAny(query, reviewTerms) {
		score += 2
		signals = append(signals, "quality_review")
	}
	if containsAny(query, explicitStepTerms) ||
		(strings.Contains(query, "先") && strings.Contains(query, "再")) {
		score++
		signals = append(signals, "explicit_steps")
	}
	if utf8.RuneCountInString(query) >= 160 {
		score++
		signals = append(signals, "long_request")
	}
	if containsAny(query, briefTerms) {
		score -= 2
		signals = append(signals, "brief_request")
	}
	if score < 0 {
		score = 0
	}
	if score > 10 {
		score = 10
	}
	return score, signals
}

func classifyComplexity(score int) ComplexityClass {
	switch {
	case score >= 6:
		return ComplexityHigh
	case score >= 3:
		return ComplexityMedium
	default:
		return ComplexityLow
	}
}

func templateKey(executionProfile string, capabilityIDs []string) string {
	return strings.TrimSpace(executionProfile) + "\x00" + strings.Join(uniqueSorted(capabilityIDs), "\x00")
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsAny(value string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func validKind(kind Kind) bool {
	return kind == KindSingleAgent || kind == KindMultiAgent
}

func validDecision(decision Decision) bool {
	return decision == DecisionSelected || decision == DecisionFallback || decision == DecisionDisabled
}

var analysisTerms = []string{
	"深入", "研究", "调研", "分析", "比较", "对比", "综合", "权衡", "论证",
	"research", "analyze", "analyse", "compare", "synthesize", "investigate",
}

var multipleOutputTerms = []string{
	"多个", "多种", "三条", "三个", "候选", "方案", "系列", "线程", "分别",
	"multiple", "alternatives", "options", "variants", "thread",
}

var reviewTerms = []string{
	"审查", "审核", "校对", "验证", "核验", "事实检查", "挑错", "复盘",
	"review", "verify", "validate", "fact-check", "critique",
}

var explicitStepTerms = []string{
	"分步骤", "逐步", "分阶段", "第一步", "第二步", "step by step", "in stages",
}

var briefTerms = []string{
	"一句话", "简短", "简洁", "快速回答", "只要答案", "brief", "concise", "one sentence",
}
