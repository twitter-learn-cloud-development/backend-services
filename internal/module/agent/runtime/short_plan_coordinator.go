package runtime

import (
	"context"
	"errors"
	"fmt"
)

const maximumShortPlanAdmissionRepairs = 1

type ShortPlanAdmissionPolicy interface {
	Admit(context.Context, ShortPlanRequest, ShortPlanProposal) (AdmittedShortPlan, error)
}

// PlanningResult contains admitted plan evidence and the complete model cost
// of producing it. Rejected proposals are deliberately not retained here.
type PlanningResult struct {
	Plan              AdmittedShortPlan
	Usage             TokenUsage
	PlanningCalls     int
	ModelAttempts     int
	AdmissionAttempts int
	AdmissionRepairs  int
	LastRepairReason  ShortPlanRepairReason
	Model             string
	Provider          string
}

type PlanningCoordinator struct {
	planner             ShortHorizonPlanner
	policy              ShortPlanAdmissionPolicy
	maxAdmissionRepairs int
}

type PlanningCoordinatorOption func(*PlanningCoordinator)

func WithShortPlanAdmissionRepairs(limit int) PlanningCoordinatorOption {
	return func(coordinator *PlanningCoordinator) {
		coordinator.maxAdmissionRepairs = limit
	}
}

func NewPlanningCoordinator(
	planner ShortHorizonPlanner,
	policy ShortPlanAdmissionPolicy,
	options ...PlanningCoordinatorOption,
) (*PlanningCoordinator, error) {
	if planner == nil {
		return nil, errors.New("short plan planner is required")
	}
	if policy == nil {
		return nil, errors.New("short plan admission policy is required")
	}
	coordinator := &PlanningCoordinator{
		planner:             planner,
		policy:              policy,
		maxAdmissionRepairs: maximumShortPlanAdmissionRepairs,
	}
	for _, option := range options {
		if option != nil {
			option(coordinator)
		}
	}
	if coordinator.maxAdmissionRepairs < 0 ||
		coordinator.maxAdmissionRepairs > maximumShortPlanAdmissionRepairs {
		return nil, fmt.Errorf(
			"short plan admission repairs must be between 0 and %d",
			maximumShortPlanAdmissionRepairs,
		)
	}
	return coordinator, nil
}

func (coordinator *PlanningCoordinator) Coordinate(
	ctx context.Context,
	request ShortPlanRequest,
) (PlanningResult, error) {
	result := PlanningResult{}
	if ctx == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "planning context is required"}
	}
	if coordinator == nil || coordinator.planner == nil || coordinator.policy == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "planning coordinator is not configured"}
	}
	if err := ctx.Err(); err != nil {
		return result, contextRunError(err, request.CompletedSteps)
	}
	if request.RepairFeedback != nil || request.RecoveryFeedback != nil {
		return result, &RunError{
			Code: ErrorInvalidRequest, Message: "initial planning request cannot contain repair or recovery feedback",
		}
	}
	if _, err := NewBudgetTracker(request.Budget); err != nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "invalid planning budget", Cause: err}
	}

	current := request
	for {
		proposalResult, planErr := coordinator.planner.Plan(ctx, current)
		result.PlanningCalls++
		result.ModelAttempts += proposalResult.Attempts
		result.Usage.Add(normalizedUsage(proposalResult.Usage))
		if proposalResult.Model != "" {
			result.Model = proposalResult.Model
		}
		if proposalResult.Provider != "" {
			result.Provider = proposalResult.Provider
		}
		if planErr != nil {
			return result, planErr
		}

		result.AdmissionAttempts++
		admitted, admissionErr := coordinator.policy.Admit(ctx, request, proposalResult.Proposal)
		if admissionErr == nil {
			result.Plan = CloneAdmittedShortPlan(admitted)
			return result, nil
		}

		feedback, repairable := shortPlanRepairFeedback(admissionErr)
		if !repairable || result.AdmissionRepairs >= coordinator.maxAdmissionRepairs {
			return result, admissionErr
		}
		remainingBudget, budgetErr := remainingShortPlanBudget(
			request.Budget,
			result.Usage,
			request.CompletedSteps+1,
		)
		if budgetErr != nil {
			return result, budgetErr
		}

		result.AdmissionRepairs++
		result.LastRepairReason = feedback.Reason
		current = request
		current.Budget = remainingBudget
		current.RepairFeedback = &feedback
	}
}

func shortPlanRepairFeedback(err error) (ShortPlanRepairFeedback, bool) {
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr == nil {
		return ShortPlanRepairFeedback{}, false
	}
	switch runErr.Code {
	case ErrorInvalidAction:
		return ShortPlanRepairFeedback{Reason: ShortPlanRepairInvalidAction}, true
	case ErrorUnknownTool:
		return ShortPlanRepairFeedback{Reason: ShortPlanRepairUnknownTool}, true
	default:
		return ShortPlanRepairFeedback{}, false
	}
}

func shortPlanAdmissionRepairInstruction(feedback *ShortPlanRepairFeedback) (string, error) {
	if feedback == nil {
		return "", nil
	}
	switch feedback.Reason {
	case ShortPlanRepairInvalidAction:
		return "The previous proposal failed deterministic admission. Produce a fresh plan that covers all target criteria, uses unique step IDs, respects terminal ordering and fits the remaining step budget. Do not repeat or quote the rejected proposal.", nil
	case ShortPlanRepairUnknownTool:
		return "The previous proposal selected a tool outside the current allowed and available tool set. Produce a fresh plan using only names in available_tools. Do not repeat or quote the rejected proposal.", nil
	default:
		return "", &RunError{
			Code: ErrorInvalidRequest, Message: "short plan repair feedback reason is invalid",
		}
	}
}

func remainingShortPlanBudget(budget Budget, consumed TokenUsage, step int) (Budget, error) {
	consumed = normalizedUsage(consumed)
	remaining := budget
	if budget.MaxTotalTokens > 0 {
		left := budget.MaxTotalTokens - consumed.TotalTokens
		if left <= 0 {
			return Budget{}, &RunError{
				Code: ErrorBudgetExceeded, Step: step,
				Message: "short plan repair requires another model call but the token budget is exhausted",
			}
		}
		remaining.MaxTotalTokens = left
	}
	if budget.MaxEstimatedCostMicros > 0 {
		left := budget.MaxEstimatedCostMicros - consumed.EstimatedCostMicros
		if left <= 0 {
			return Budget{}, &RunError{
				Code: ErrorBudgetExceeded, Step: step,
				Message: "short plan repair requires another model call but the cost budget is exhausted",
			}
		}
		remaining.MaxEstimatedCostMicros = left
	}
	return remaining, nil
}
