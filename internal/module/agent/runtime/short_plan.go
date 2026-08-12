package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	ShortPlanVersionV1 = "agent.short_plan.v1"
	MaxShortPlanSteps  = 3
)

type ShortPlanStepKind string

const (
	ShortPlanStepTool     ShortPlanStepKind = "tool"
	ShortPlanStepAskHuman ShortPlanStepKind = "ask_human"
	ShortPlanStepRespond  ShortPlanStepKind = "respond"
)

// ShortPlanStep is an untrusted model proposal. Authorization-derived fields
// are deliberately absent: a model can request a tool, but cannot authorize it.
type ShortPlanStep struct {
	ID           string            `json:"id"`
	Kind         ShortPlanStepKind `json:"kind"`
	Objective    string            `json:"objective"`
	ToolName     string            `json:"tool_name,omitempty"`
	CriterionIDs []string          `json:"criterion_ids"`
}

// ShortPlanProposal contains only the next one to three actions. It is not an
// execution grant and must pass DeterministicShortPlanPolicy before use.
type ShortPlanProposal struct {
	Version string          `json:"version"`
	Steps   []ShortPlanStep `json:"steps"`
}

// ShortPlanRequest is the bounded context exposed to a planner. AvailableTools
// must already be resolved for the current task, user/project and Environment.
type ShortPlanRepairReason string

const (
	ShortPlanRepairInvalidAction ShortPlanRepairReason = "invalid_action"
	ShortPlanRepairUnknownTool   ShortPlanRepairReason = "unknown_tool"
)

type ShortPlanRepairFeedback struct {
	Reason ShortPlanRepairReason
}

type ShortPlanRecoveryReason string

const (
	ShortPlanRecoveryExecutionFailed ShortPlanRecoveryReason = "execution_failed"
	ShortPlanRecoveryEvidenceMissing ShortPlanRecoveryReason = "evidence_missing"
)

// ShortPlanRecoveryFeedback contains only a fixed reason code. Missing
// criteria travel through TargetCriterionIDs; raw failures are never exposed.
type ShortPlanRecoveryFeedback struct {
	Reason ShortPlanRecoveryReason
}

type ShortPlanRequest struct {
	Context            RunContext
	Model              string
	Task               TaskSpec
	AvailableTools     []ToolDefinition
	Budget             Budget
	CompletedSteps     int
	TargetCriterionIDs []string
	RepairFeedback     *ShortPlanRepairFeedback
	RecoveryFeedback   *ShortPlanRecoveryFeedback
}

// ShortPlanResult accounts for every model attempt, including a bounded
// structured-output repair. The admitted plan remains a separate policy result.
type ShortPlanResult struct {
	Proposal ShortPlanProposal
	Usage    TokenUsage
	Attempts int
	Model    string
	Provider string
}

type ShortHorizonPlanner interface {
	Plan(context.Context, ShortPlanRequest) (ShortPlanResult, error)
}

type AdmittedShortPlanStep struct {
	ID               string            `json:"id"`
	Kind             ShortPlanStepKind `json:"kind"`
	Objective        string            `json:"objective"`
	ToolName         string            `json:"tool_name,omitempty"`
	ToolCategory     ToolCategory      `json:"tool_category,omitempty"`
	ApprovalRequired bool              `json:"approval_required,omitempty"`
	CriterionIDs     []string          `json:"criterion_ids"`
}

// AdmittedShortPlan is immutable planning evidence. Tool category and approval
// are copied from the governed catalog, never from model output.
type AdmittedShortPlan struct {
	Version string                  `json:"version"`
	Steps   []AdmittedShortPlanStep `json:"steps"`
	Digest  string                  `json:"digest"`
}

// DeterministicShortPlanPolicy validates a model proposal against current
// task, catalog and budget state. Tool execution still re-checks policy,
// approval, idempotency and connection authorization at call time.
type DeterministicShortPlanPolicy struct{}

func (DeterministicShortPlanPolicy) Admit(
	ctx context.Context,
	request ShortPlanRequest,
	proposal ShortPlanProposal,
) (AdmittedShortPlan, error) {
	if err := ctx.Err(); err != nil {
		return AdmittedShortPlan{}, contextRunError(err, request.CompletedSteps)
	}
	if err := request.Task.Validate(); err != nil {
		return AdmittedShortPlan{}, &RunError{
			Code: ErrorInvalidRequest, Message: "invalid planning task", Cause: err,
		}
	}
	if request.CompletedSteps < 0 {
		return AdmittedShortPlan{}, &RunError{
			Code: ErrorInvalidRequest, Message: "completed planning steps cannot be negative",
		}
	}
	if strings.TrimSpace(proposal.Version) != ShortPlanVersionV1 {
		return AdmittedShortPlan{}, &RunError{
			Code: ErrorInvalidAction, Message: "short plan version is invalid",
		}
	}
	if len(proposal.Steps) == 0 || len(proposal.Steps) > MaxShortPlanSteps {
		return AdmittedShortPlan{}, &RunError{
			Code:    ErrorInvalidAction,
			Message: fmt.Sprintf("short plan must contain between 1 and %d steps", MaxShortPlanSteps),
		}
	}

	maxSteps := request.Budget.MaxSteps
	if maxSteps == 0 {
		maxSteps = DefaultMaxSteps
	}
	if maxSteps < 1 || maxSteps > MaxAllowedSteps {
		return AdmittedShortPlan{}, &RunError{
			Code:    ErrorInvalidRequest,
			Message: fmt.Sprintf("max steps must be between 1 and %d", MaxAllowedSteps),
		}
	}
	if request.CompletedSteps >= maxSteps || len(proposal.Steps) > maxSteps-request.CompletedSteps {
		return AdmittedShortPlan{}, &RunError{
			Code: ErrorBudgetExceeded, Step: request.CompletedSteps + 1,
			Message: fmt.Sprintf("short plan requires %d steps but only %d remain", len(proposal.Steps), maxSteps-request.CompletedSteps),
		}
	}

	toolCatalog, err := buildToolCatalog(request.AvailableTools)
	if err != nil {
		return AdmittedShortPlan{}, err
	}
	allowedTools := make(map[string]struct{}, len(request.Task.AllowedTools))
	for _, name := range request.Task.AllowedTools {
		allowedTools[strings.TrimSpace(name)] = struct{}{}
	}
	targetCriteria, criterionCatalog, err := planningCriteria(request.Task, request.TargetCriterionIDs)
	if err != nil {
		return AdmittedShortPlan{}, err
	}

	admitted := AdmittedShortPlan{
		Version: ShortPlanVersionV1,
		Steps:   make([]AdmittedShortPlanStep, 0, len(proposal.Steps)),
	}
	seenSteps := make(map[string]struct{}, len(proposal.Steps))
	coveredCriteria := make(map[string]struct{}, len(targetCriteria))
	for index, proposed := range proposal.Steps {
		step, admitErr := admitShortPlanStep(
			proposed,
			index,
			len(proposal.Steps),
			toolCatalog,
			allowedTools,
			criterionCatalog,
			seenSteps,
			coveredCriteria,
		)
		if admitErr != nil {
			return AdmittedShortPlan{}, admitErr
		}
		admitted.Steps = append(admitted.Steps, step)
	}
	for _, criterionID := range targetCriteria {
		if _, covered := coveredCriteria[criterionID]; !covered {
			return AdmittedShortPlan{}, &RunError{
				Code:    ErrorInvalidAction,
				Message: fmt.Sprintf("short plan does not address target criterion %q", criterionID),
			}
		}
	}

	admitted.Digest, err = shortPlanDigest(admitted)
	if err != nil {
		return AdmittedShortPlan{}, err
	}
	return CloneAdmittedShortPlan(admitted), nil
}

func admitShortPlanStep(
	proposed ShortPlanStep,
	index int,
	total int,
	toolCatalog map[string]ToolDefinition,
	allowedTools map[string]struct{},
	criterionCatalog map[string]struct{},
	seenSteps map[string]struct{},
	coveredCriteria map[string]struct{},
) (AdmittedShortPlanStep, error) {
	proposed.ID = strings.TrimSpace(proposed.ID)
	proposed.Objective = strings.TrimSpace(proposed.Objective)
	proposed.ToolName = strings.TrimSpace(proposed.ToolName)
	stepNumber := index + 1
	if proposed.ID == "" || proposed.Objective == "" {
		return AdmittedShortPlanStep{}, &RunError{
			Code: ErrorInvalidAction, Step: stepNumber,
			Message: "short plan step id and objective are required",
		}
	}
	if len(proposed.ID) > 64 || len(proposed.Objective) > 512 {
		return AdmittedShortPlanStep{}, &RunError{
			Code: ErrorInvalidAction, Step: stepNumber,
			Message: "short plan step id or objective exceeds its limit",
		}
	}
	if _, exists := seenSteps[proposed.ID]; exists {
		return AdmittedShortPlanStep{}, &RunError{
			Code: ErrorInvalidAction, Step: stepNumber,
			Message: fmt.Sprintf("duplicate short plan step %q", proposed.ID),
		}
	}
	seenSteps[proposed.ID] = struct{}{}

	criterionIDs, err := normalizePlanCriterionIDs(proposed.CriterionIDs, criterionCatalog, stepNumber)
	if err != nil {
		return AdmittedShortPlanStep{}, err
	}
	for _, criterionID := range criterionIDs {
		coveredCriteria[criterionID] = struct{}{}
	}

	admitted := AdmittedShortPlanStep{
		ID: proposed.ID, Kind: proposed.Kind, Objective: proposed.Objective,
		CriterionIDs: criterionIDs,
	}
	switch proposed.Kind {
	case ShortPlanStepTool:
		if proposed.ToolName == "" {
			return AdmittedShortPlanStep{}, &RunError{
				Code: ErrorInvalidAction, Step: stepNumber, Message: "tool plan step requires a tool name",
			}
		}
		if _, allowed := allowedTools[proposed.ToolName]; !allowed {
			return AdmittedShortPlanStep{}, &RunError{
				Code: ErrorUnknownTool, Step: stepNumber,
				Message: fmt.Sprintf("tool %q is not allowed by the task", proposed.ToolName),
			}
		}
		definition, available := toolCatalog[proposed.ToolName]
		if !available {
			return AdmittedShortPlanStep{}, &RunError{
				Code: ErrorUnknownTool, Step: stepNumber,
				Message: fmt.Sprintf("tool %q is not available in the current environment", proposed.ToolName),
			}
		}
		if !validToolCategory(definition.Category) {
			return AdmittedShortPlanStep{}, &RunError{
				Code: ErrorInvalidRequest, Step: stepNumber,
				Message: fmt.Sprintf("tool %q has an invalid policy category", proposed.ToolName),
			}
		}
		admitted.ToolName = proposed.ToolName
		admitted.ToolCategory = definition.Category
		admitted.ApprovalRequired = definition.ApprovalRequired()
	case ShortPlanStepAskHuman, ShortPlanStepRespond:
		if proposed.ToolName != "" {
			return AdmittedShortPlanStep{}, &RunError{
				Code: ErrorInvalidAction, Step: stepNumber,
				Message: fmt.Sprintf("%s plan step cannot name a tool", proposed.Kind),
			}
		}
		if index != total-1 {
			return AdmittedShortPlanStep{}, &RunError{
				Code: ErrorInvalidAction, Step: stepNumber,
				Message: fmt.Sprintf("%s plan step must be terminal", proposed.Kind),
			}
		}
	default:
		return AdmittedShortPlanStep{}, &RunError{
			Code: ErrorInvalidAction, Step: stepNumber,
			Message: fmt.Sprintf("unknown short plan step kind %q", proposed.Kind),
		}
	}
	return admitted, nil
}

func planningCriteria(task TaskSpec, requested []string) ([]string, map[string]struct{}, error) {
	catalog := make(map[string]struct{}, len(task.CompletionCriteria))
	required := make([]string, 0, len(task.CompletionCriteria))
	for _, criterion := range task.CompletionCriteria {
		criterionID := strings.TrimSpace(criterion.ID)
		catalog[criterionID] = struct{}{}
		if criterion.Required {
			required = append(required, criterionID)
		}
	}
	targets := requested
	if len(targets) == 0 {
		targets = required
	}
	normalized := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, raw := range targets {
		criterionID := strings.TrimSpace(raw)
		if _, exists := catalog[criterionID]; !exists {
			return nil, nil, &RunError{
				Code:    ErrorInvalidRequest,
				Message: fmt.Sprintf("unknown target criterion %q", criterionID),
			}
		}
		if _, duplicate := seen[criterionID]; duplicate {
			return nil, nil, &RunError{
				Code:    ErrorInvalidRequest,
				Message: fmt.Sprintf("duplicate target criterion %q", criterionID),
			}
		}
		seen[criterionID] = struct{}{}
		normalized = append(normalized, criterionID)
	}
	sort.Strings(normalized)
	return normalized, catalog, nil
}

func normalizePlanCriterionIDs(
	values []string,
	catalog map[string]struct{},
	step int,
) ([]string, error) {
	if len(values) == 0 {
		return nil, &RunError{
			Code: ErrorInvalidAction, Step: step, Message: "short plan step must address a completion criterion",
		}
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		criterionID := strings.TrimSpace(raw)
		if _, exists := catalog[criterionID]; !exists {
			return nil, &RunError{
				Code: ErrorInvalidAction, Step: step,
				Message: fmt.Sprintf("short plan step references unknown criterion %q", criterionID),
			}
		}
		if _, duplicate := seen[criterionID]; duplicate {
			return nil, &RunError{
				Code: ErrorInvalidAction, Step: step,
				Message: fmt.Sprintf("short plan step repeats criterion %q", criterionID),
			}
		}
		seen[criterionID] = struct{}{}
		normalized = append(normalized, criterionID)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func validToolCategory(category ToolCategory) bool {
	switch category {
	case ToolCategoryRead, ToolCategoryWrite, ToolCategoryRisky:
		return true
	default:
		return false
	}
}

func shortPlanDigest(plan AdmittedShortPlan) (string, error) {
	canonical := CloneAdmittedShortPlan(plan)
	canonical.Digest = ""
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", &RunError{Code: ErrorInvalidAction, Message: "marshal admitted short plan", Cause: err}
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func CloneAdmittedShortPlan(plan AdmittedShortPlan) AdmittedShortPlan {
	cloned := plan
	cloned.Steps = make([]AdmittedShortPlanStep, len(plan.Steps))
	for index, step := range plan.Steps {
		step.CriterionIDs = append([]string(nil), step.CriterionIDs...)
		cloned.Steps[index] = step
	}
	return cloned
}
