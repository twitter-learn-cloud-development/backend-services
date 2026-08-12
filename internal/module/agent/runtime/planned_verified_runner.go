package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PlannedVerifiedRunRequest opts a task into model-driven short planning before
// the existing verified ReAct execution path. Production callers remain
// unchanged until they explicitly compose this runner.
type PlannedVerifiedRunRequest struct {
	Task               TaskSpec
	Run                RunRequest
	Environment        Environment
	Evidence           EvidenceLedger
	TargetCriterionIDs []string
}

// PlannedVerifiedRunResult keeps planning and execution evidence separate
// while exposing their aggregate model usage for the owning request.
type PlannedVerifiedRunResult struct {
	Planning           PlanningResult
	Verified           VerifiedRunResult
	Usage              TokenUsage
	RecoveryPlans      []PlanningResult
	RecoveryAttempts   int
	LastRecoveryReason ShortPlanRecoveryReason
}

// PlannedVerifiedRunner constrains VerifiedRunner with an admitted short plan.
// It never executes tools itself; ReAct and ToolExecutor remain the only action
// path, preserving policy, approval, audit and idempotency enforcement.
type PlannedVerifiedRunner struct {
	planning *PlanningCoordinator
	verified *VerifiedRunner
}

func NewPlannedVerifiedRunner(
	planning *PlanningCoordinator,
	verified *VerifiedRunner,
) *PlannedVerifiedRunner {
	return &PlannedVerifiedRunner{planning: planning, verified: verified}
}

func (runner *PlannedVerifiedRunner) Run(
	ctx context.Context,
	request PlannedVerifiedRunRequest,
) (PlannedVerifiedRunResult, error) {
	result := PlannedVerifiedRunResult{}
	if ctx == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "planned verified context is required"}
	}
	if runner == nil || runner.planning == nil || runner.verified == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "planned verified runner is not configured"}
	}
	if err := request.Task.Validate(); err != nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "invalid planned task", Cause: err}
	}

	runCtx, cancel := WithBudgetContext(ctx, request.Run.Context.Budget)
	defer cancel()

	availableTools, err := resolveGoalTools(runCtx, request.Task, request.Run.Tools, request.Environment)
	if err != nil {
		return result, err
	}
	result.Planning, err = runner.planning.Coordinate(runCtx, ShortPlanRequest{
		Context:            request.Run.Context,
		Model:              request.Run.Model,
		Task:               request.Task,
		AvailableTools:     availableTools,
		Budget:             request.Run.Context.Budget,
		TargetCriterionIDs: cloneStrings(request.TargetCriterionIDs),
	})
	result.Usage = normalizedUsage(result.Planning.Usage)
	if err != nil {
		return result, err
	}

	verifiedRequest, err := bindAdmittedPlanToVerifiedRequest(request, result.Planning)
	if err != nil {
		return result, err
	}
	repairBuilder := &plannedVerifiedRepairBuilder{
		coordinator: runner.planning,
		task:        cloneTaskSpec(verifiedRequest.Task),
		base:        verifiedRequest.Run,
		environment: request.Environment,
		evidence:    cloneEvidenceLedger(verifiedRequest.Evidence),
	}
	verifiedRequest.RepairBuilder = repairBuilder
	result.Verified, err = runner.verified.Run(runCtx, verifiedRequest)
	result.RecoveryPlans = make([]PlanningResult, 0, len(repairBuilder.recoveryPlans))
	for _, recovery := range repairBuilder.recoveryPlans {
		result.RecoveryPlans = append(result.RecoveryPlans, clonePlanningResult(recovery))
	}
	result.RecoveryAttempts = repairBuilder.used
	result.LastRecoveryReason = repairBuilder.lastReason
	for _, recovery := range result.RecoveryPlans {
		result.Usage.Add(normalizedUsage(recovery.Usage))
	}
	result.Usage.Add(normalizedUsage(result.Verified.Run.Usage))
	if err != nil {
		return result, err
	}
	planToValidate := result.Planning.Plan
	if len(result.RecoveryPlans) > 0 {
		planToValidate = result.RecoveryPlans[len(result.RecoveryPlans)-1].Plan
	}
	if err := validateAdmittedPlanExecution(planToValidate, result.Verified); err != nil {
		return result, err
	}
	return result, nil
}

func bindAdmittedPlanToVerifiedRequest(
	request PlannedVerifiedRunRequest,
	planning PlanningResult,
) (VerifiedRunRequest, error) {
	plan := CloneAdmittedShortPlan(planning.Plan)
	digest, err := shortPlanDigest(plan)
	if err != nil {
		return VerifiedRunRequest{}, err
	}
	if strings.TrimSpace(plan.Digest) == "" || plan.Digest != digest {
		return VerifiedRunRequest{}, &RunError{
			Code: ErrorInvalidAction, Message: "admitted short plan digest is invalid",
		}
	}
	existingDigest := strings.TrimSpace(request.Run.Context.StrategyPlanDigest)
	if existingDigest != "" && existingDigest != plan.Digest {
		return VerifiedRunRequest{}, &RunError{
			Code: ErrorInvalidRequest, Message: "run context is bound to a different strategy plan",
		}
	}

	executionBudget, err := remainingExecutionBudget(request.Run.Context.Budget, planning.Usage)
	if err != nil {
		return VerifiedRunRequest{}, err
	}

	catalog, err := buildToolCatalog(request.Run.Tools)
	if err != nil {
		return VerifiedRunRequest{}, err
	}
	plannedToolNames := make([]string, 0, len(plan.Steps))
	plannedTools := make([]ToolDefinition, 0, len(plan.Steps))
	seenTools := make(map[string]struct{}, len(plan.Steps))
	for index, step := range plan.Steps {
		if step.Kind != ShortPlanStepTool {
			continue
		}
		if step.ToolCategory != ToolCategoryRead || step.ApprovalRequired {
			return VerifiedRunRequest{}, &RunError{
				Code: ErrorUnsupported, Step: index + 1,
				Message: "planned verified execution currently supports read tools only",
			}
		}
		if _, duplicate := seenTools[step.ToolName]; duplicate {
			continue
		}
		definition, ok := catalog[step.ToolName]
		if !ok {
			return VerifiedRunRequest{}, &RunError{
				Code: ErrorUnknownTool, Step: index + 1,
				Message: fmt.Sprintf("planned tool %q is no longer in the request catalog", step.ToolName),
			}
		}
		if definition.Category != ToolCategoryRead || definition.ApprovalRequired() {
			return VerifiedRunRequest{}, &RunError{
				Code: ErrorInvalidRequest, Step: index + 1,
				Message: fmt.Sprintf("planned tool %q policy changed before execution", step.ToolName),
			}
		}
		seenTools[step.ToolName] = struct{}{}
		plannedToolNames = append(plannedToolNames, step.ToolName)
		plannedTools = append(plannedTools, definition)
	}

	if len(plannedTools) > 0 {
		maxSteps := executionBudget.MaxSteps
		if maxSteps == 0 {
			maxSteps = DefaultMaxSteps
		}
		if maxSteps < 2 {
			return VerifiedRunRequest{}, &RunError{
				Code: ErrorBudgetExceeded, Step: 1,
				Message: "planned tool execution needs a tool step and a terminal response step",
			}
		}
	}

	boundTask := cloneTaskSpec(request.Task)
	boundTask.AllowedTools = cloneStrings(plannedToolNames)
	boundRun := request.Run
	boundRun.Context.StrategyPlanDigest = plan.Digest
	boundRun.Context.Budget = executionBudget
	boundRun.Tools = cloneToolDefinitions(plannedTools)
	boundRun.Messages = cloneMessages(request.Run.Messages)
	boundRun.Messages = append(boundRun.Messages, Message{
		Role: RoleDeveloper, Name: admittedPlanMessageName, Content: admittedPlanExecutionInstruction(plan),
	})
	boundRun.InitialToolChoice = ToolChoiceNone
	if len(plannedTools) > 0 {
		boundRun.InitialToolChoice = ToolChoiceRequired
	}

	return VerifiedRunRequest{
		Task: boundTask, Run: boundRun, Environment: request.Environment,
		Evidence: cloneEvidenceLedger(request.Evidence),
	}, nil
}

func remainingExecutionBudget(budget Budget, planningUsage TokenUsage) (Budget, error) {
	planningUsage = normalizedUsage(planningUsage)
	remaining := budget
	if budget.MaxTotalTokens > 0 {
		remaining.MaxTotalTokens -= planningUsage.TotalTokens
		if remaining.MaxTotalTokens <= 0 {
			return Budget{}, &RunError{
				Code:    ErrorBudgetExceeded,
				Message: "short planning exhausted the token budget before execution",
			}
		}
	}
	if budget.MaxEstimatedCostMicros > 0 {
		remaining.MaxEstimatedCostMicros -= planningUsage.EstimatedCostMicros
		if remaining.MaxEstimatedCostMicros <= 0 {
			return Budget{}, &RunError{
				Code:    ErrorBudgetExceeded,
				Message: "short planning exhausted the cost budget before execution",
			}
		}
	}
	return remaining, nil
}

type admittedExecutionPlan struct {
	Digest string                      `json:"digest"`
	Steps  []admittedExecutionPlanStep `json:"steps"`
}

type admittedExecutionPlanStep struct {
	Kind         ShortPlanStepKind `json:"kind"`
	ToolName     string            `json:"tool_name,omitempty"`
	CriterionIDs []string          `json:"criterion_ids"`
}

func admittedPlanExecutionInstruction(plan AdmittedShortPlan) string {
	projection := admittedExecutionPlan{Digest: plan.Digest}
	for _, step := range plan.Steps {
		projection.Steps = append(projection.Steps, admittedExecutionPlanStep{
			Kind: step.Kind, ToolName: step.ToolName, CriterionIDs: cloneStrings(step.CriterionIDs),
		})
	}
	payload, _ := json.Marshal(projection)
	return "Execute the admitted short plan below in order. The plan is guidance, not authorization; " +
		"runtime tool policy remains authoritative. Derive tool arguments only from the task and conversation, " +
		"never invent missing values, and call only the listed read tools. For an ask_human terminal step, ask one " +
		"useful clarification question. For a respond terminal step, return the answer using only observed evidence. " +
		"Admitted plan: " + string(payload)
}

func validateAdmittedPlanExecution(plan AdmittedShortPlan, result VerifiedRunResult) error {
	stepCursor := 0
	for index, planned := range plan.Steps {
		switch planned.Kind {
		case ShortPlanStepTool:
			found := false
			for ; stepCursor < len(result.Run.Steps); stepCursor++ {
				for _, observation := range result.Run.Steps[stepCursor].Observations {
					if observation.Name == planned.ToolName && !observation.IsError {
						found = true
						stepCursor++
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				return &RunError{
					Code: ErrorInvalidAction, Step: index + 1,
					Message: "admitted tool step did not produce a successful observation",
				}
			}
		case ShortPlanStepAskHuman:
			if result.Status != GoalRunSuspended || result.Run.Status != RunStatusAwaitingHuman ||
				result.Run.PendingAction == nil || result.Run.PendingAction.Type != ActionAskHuman {
				return &RunError{
					Code: ErrorInvalidAction, Step: index + 1,
					Message: "admitted clarification step did not suspend for human input",
				}
			}
		case ShortPlanStepRespond:
			if result.Run.Status != RunStatusCompleted {
				return &RunError{
					Code: ErrorInvalidAction, Step: index + 1,
					Message: "admitted response step did not complete with a final answer",
				}
			}
		}
	}
	return nil
}
