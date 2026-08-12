package runtime

import (
	"context"
	"strings"
)

const (
	admittedPlanMessageName        = "runtime.admitted_short_plan"
	sanitizedToolFailureMessage    = "Governed tool action failed; no result is available."
	maximumExecutionPlanRecoveries = 1
)

type plannedVerifiedRepairBuilder struct {
	coordinator   *PlanningCoordinator
	task          TaskSpec
	base          RunRequest
	environment   Environment
	evidence      EvidenceLedger
	recoveryPlans []PlanningResult
	lastReason    ShortPlanRecoveryReason
	used          int
}

func (builder *plannedVerifiedRepairBuilder) BuildRepair(
	ctx context.Context,
	request VerifiedRepairRequest,
) (RunRequest, error) {
	if builder == nil || builder.coordinator == nil {
		return RunRequest{}, &RunError{Code: ErrorInvalidRequest, Message: "planned recovery is not configured"}
	}
	if builder.used >= maximumExecutionPlanRecoveries {
		return RunRequest{}, &RunError{Code: ErrorInvalidAction, Message: "planned recovery limit is exhausted"}
	}
	reason, err := recoveryReason(request.Signal)
	if err != nil {
		return RunRequest{}, err
	}
	builder.used++
	builder.lastReason = reason

	planningBudget, err := recoveryPlanningBudget(builder.base.Context.Budget, request.Previous)
	if err != nil {
		return RunRequest{}, err
	}
	targets := cloneStrings(request.Signal.MissingCriterionIDs)
	if len(targets) == 0 {
		targets = requiredCriterionIDs(builder.task)
	}
	planning, err := builder.coordinator.CoordinateRecovery(ctx, ShortPlanRequest{
		Context: builder.base.Context, Model: builder.base.Model,
		Task: cloneTaskSpec(builder.task), AvailableTools: cloneToolDefinitions(builder.base.Tools),
		Budget: planningBudget, CompletedSteps: len(request.Previous.Steps),
		TargetCriterionIDs: sortedUniqueStrings(targets),
	}, ShortPlanRecoveryFeedback{Reason: reason})
	builder.recoveryPlans = append(builder.recoveryPlans, clonePlanningResult(planning))
	if err != nil {
		return RunRequest{}, err
	}

	messages := cloneMessages(request.Previous.Messages)
	if reason == ShortPlanRecoveryExecutionFailed {
		if failed := sanitizedFailedToolMessage(request.Previous); failed != nil {
			messages = append(messages, *failed)
		}
	}
	replanned := PlannedVerifiedRunRequest{
		Task: cloneTaskSpec(builder.task), Environment: builder.environment,
		Evidence: cloneEvidenceLedger(builder.evidence),
		Run:      builder.base,
	}
	replanned.Run.Context.Budget = planningBudget
	replanned.Run.Context.StrategyPlanDigest = ""
	replanned.Run.Messages = messages
	replanned.Run.Tools = cloneToolDefinitions(builder.base.Tools)
	bound, err := bindAdmittedPlanToVerifiedRequest(replanned, planning)
	if err != nil {
		return RunRequest{}, err
	}
	remaining, err := remainingGoalBudget(builder.base.Context.Budget, request.Previous)
	if err != nil {
		return RunRequest{}, err
	}
	bound.Run.Context.Budget.MaxSteps = remaining.MaxSteps
	return bound.Run, nil
}

func recoveryReason(signal VerifiedRepairSignal) (ShortPlanRecoveryReason, error) {
	switch signal.Reason {
	case VerifiedRepairExecutionFailed:
		if signal.ErrorCode != ErrorTool {
			return "", &RunError{Code: ErrorInvalidRequest, Message: "execution recovery requires a tool error"}
		}
		return ShortPlanRecoveryExecutionFailed, nil
	case VerifiedRepairEvidenceMissing:
		return ShortPlanRecoveryEvidenceMissing, nil
	default:
		return "", &RunError{Code: ErrorInvalidRequest, Message: "verified recovery reason is invalid"}
	}
}

func recoveryPlanningBudget(base Budget, previous RunResult) (Budget, error) {
	remaining, err := remainingGoalBudget(base, previous)
	if err != nil {
		return Budget{}, err
	}
	maxSteps := base.MaxSteps
	if maxSteps == 0 {
		maxSteps = DefaultMaxSteps
	}
	remaining.MaxSteps = maxSteps
	return remaining, nil
}

func requiredCriterionIDs(task TaskSpec) []string {
	criteria := make([]string, 0, len(task.CompletionCriteria))
	for _, criterion := range task.CompletionCriteria {
		if criterion.Required && strings.TrimSpace(criterion.ID) != "" {
			criteria = append(criteria, criterion.ID)
		}
	}
	return sortedUniqueStrings(criteria)
}

func sanitizedFailedToolMessage(result RunResult) *Message {
	for stepIndex := len(result.Steps) - 1; stepIndex >= 0; stepIndex-- {
		step := result.Steps[stepIndex]
		for observationIndex := len(step.Observations) - 1; observationIndex >= 0; observationIndex-- {
			observation := step.Observations[observationIndex]
			if observation.IsError && strings.TrimSpace(observation.ActionID) != "" {
				return &Message{
					Role: RoleTool, Content: sanitizedToolFailureMessage,
					Name: observation.Name, ToolCallID: observation.ActionID,
				}
			}
		}
	}
	return nil
}

func clonePlanningResult(result PlanningResult) PlanningResult {
	result.Plan = CloneAdmittedShortPlan(result.Plan)
	return result
}
