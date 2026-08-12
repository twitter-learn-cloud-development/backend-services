package runtime

import "context"

type recoveryShortHorizonPlanner struct {
	delegate ShortHorizonPlanner
	feedback ShortPlanRecoveryFeedback
}

func (planner recoveryShortHorizonPlanner) Plan(
	ctx context.Context,
	request ShortPlanRequest,
) (ShortPlanResult, error) {
	feedback := planner.feedback
	request.RecoveryFeedback = &feedback
	return planner.delegate.Plan(ctx, request)
}

// CoordinateRecovery is the only supported entry for execution-time
// replanning. It keeps the normal admission policy and its one bounded repair,
// while supplying only a sanitized recovery reason to the model planner.
func (coordinator *PlanningCoordinator) CoordinateRecovery(
	ctx context.Context,
	request ShortPlanRequest,
	feedback ShortPlanRecoveryFeedback,
) (PlanningResult, error) {
	if coordinator == nil || coordinator.planner == nil || coordinator.policy == nil {
		return PlanningResult{}, &RunError{
			Code: ErrorInvalidRequest, Message: "planning coordinator is not configured",
		}
	}
	if request.RepairFeedback != nil || request.RecoveryFeedback != nil {
		return PlanningResult{}, &RunError{
			Code: ErrorInvalidRequest, Message: "recovery planning request already contains feedback",
		}
	}
	if _, err := shortPlanRecoveryInstruction(&feedback); err != nil {
		return PlanningResult{}, err
	}
	isolated := *coordinator
	isolated.planner = recoveryShortHorizonPlanner{
		delegate: coordinator.planner,
		feedback: feedback,
	}
	return isolated.Coordinate(ctx, request)
}

func shortPlanRecoveryInstruction(feedback *ShortPlanRecoveryFeedback) (string, error) {
	if feedback == nil {
		return "", nil
	}
	switch feedback.Reason {
	case ShortPlanRecoveryExecutionFailed:
		return "A governed tool action failed without usable output. Produce one fresh bounded plan using only available_tools and target criteria. Do not infer, repeat or quote the failure, provider response, tool output or previous proposal.", nil
	case ShortPlanRecoveryEvidenceMissing:
		return "Completion verification is missing evidence for the target criteria. Produce one fresh bounded plan that obtains observable evidence using only available_tools. Do not repeat or quote prior answers, tool output or verifier details.", nil
	default:
		return "", &RunError{
			Code: ErrorInvalidRequest, Message: "short plan recovery feedback reason is invalid",
		}
	}
}
