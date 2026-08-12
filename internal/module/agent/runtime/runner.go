package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultMaxSteps = 5
	MaxAllowedSteps = 64

	finalStepSystemInstruction = "The tool execution phase is complete. Do not request or call another tool. " +
		"Using only the existing messages and observations, return the final answer now. " +
		"Never invent missing identities, usernames, URLs, timestamps, metrics, source fields, or full content. " +
		"If the available evidence is insufficient or a requested field is absent, state that plainly without calling a tool."
)

type ReActRunner struct {
	model         ModelClient
	tools         ToolExecutor
	rag           RAGSearcher
	tokenCounter  TokenCounter
	costEstimator CostEstimator
	admission     AdmissionController
	now           func() time.Time
}

type ReActRunnerOption func(*ReActRunner)

func WithTokenCounter(counter TokenCounter) ReActRunnerOption {
	return func(runner *ReActRunner) {
		if counter != nil {
			runner.tokenCounter = counter
		}
	}
}

func WithCostEstimator(estimator CostEstimator) ReActRunnerOption {
	return func(runner *ReActRunner) {
		runner.costEstimator = estimator
	}
}

func WithAdmissionController(controller AdmissionController) ReActRunnerOption {
	return func(runner *ReActRunner) {
		runner.admission = controller
	}
}

func NewReActRunner(model ModelClient, tools ToolExecutor, rag RAGSearcher, options ...ReActRunnerOption) *ReActRunner {
	runner := &ReActRunner{
		model:        model,
		tools:        tools,
		rag:          rag,
		tokenCounter: NewHeuristicTokenCounter(),
		now:          time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(runner)
		}
	}
	return runner
}

func (r *ReActRunner) Run(ctx context.Context, request RunRequest) (RunResult, error) {
	result := RunResult{
		Context:  request.Context,
		Status:   RunStatusRunning,
		Messages: cloneMessages(request.Messages),
	}
	if request.Context.StartedAt.IsZero() && r != nil {
		request.Context.StartedAt = r.now()
	}
	result.Context = request.Context
	return r.runFromState(ctx, request, result, 1)
}

func (r *ReActRunner) Resume(ctx context.Context, request ResumeRequest) (RunResult, error) {
	checkpoint := cloneRunCheckpoint(request.Checkpoint)
	if err := ValidateRunCheckpoint(checkpoint); err != nil {
		return failResult(RunResult{Context: checkpoint.Context, Status: RunStatusRunning}, &RunError{
			Code: ErrorInvalidRequest, Message: "invalid resume checkpoint", Cause: err,
		})
	}
	result := RunResult{
		Context:  checkpoint.Context,
		Status:   RunStatusRunning,
		Messages: cloneMessages(checkpoint.Messages),
		Steps:    cloneSteps(checkpoint.Steps),
		Usage:    checkpoint.Usage,
	}
	runRequest := RunRequest{
		Context: checkpoint.Context,
		Model:   checkpoint.Model,
		Tools:   cloneToolDefinitions(request.Tools),
	}

	switch checkpoint.PendingAction.Type {
	case ActionAskHuman:
		humanResponse := strings.TrimSpace(request.HumanResponse)
		if humanResponse == "" {
			return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "human response is required"})
		}
		if strings.TrimSpace(request.ApprovalID) != "" {
			return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "approval id is not valid for a human response"})
		}
		result.Messages = append(result.Messages, Message{Role: RoleUser, Content: humanResponse})
		return r.runFromState(ctx, runRequest, result, len(result.Steps)+1)
	case ActionToolCall:
		if checkpoint.PendingResumeKind == ResumeKindHumanResponse {
			humanResponse := strings.TrimSpace(request.HumanResponse)
			if humanResponse == "" {
				return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "human response is required"})
			}
			if strings.TrimSpace(request.ApprovalID) != "" {
				return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "approval id is not valid for a suspended tool response"})
			}
			return r.resumeSuspendedTool(
				ctx,
				runRequest,
				result,
				checkpoint.PendingAction,
				*checkpoint.PendingToolContinuation,
				ToolResumeRequest{HumanResponse: humanResponse},
			)
		}
		approvalID := strings.TrimSpace(request.ApprovalID)
		if approvalID == "" || approvalID != checkpoint.PendingApprovalID {
			return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "approval id does not match the pending action"})
		}
		if strings.TrimSpace(request.HumanResponse) != "" {
			return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "human response is not valid for an approval resume"})
		}
		if checkpoint.PendingResumeKind == ResumeKindDelegatedToolApproval {
			return r.resumeSuspendedTool(
				ctx,
				runRequest,
				result,
				checkpoint.PendingAction,
				*checkpoint.PendingToolContinuation,
				ToolResumeRequest{
					ApprovalID:  approvalID,
					ResumeToken: strings.TrimSpace(request.ResumeToken),
				},
			)
		}
		return r.resumeApprovedAction(ctx, runRequest, result, checkpoint.PendingAction)
	default:
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "checkpoint action cannot be resumed"})
	}
}

func (r *ReActRunner) resumeApprovedAction(
	ctx context.Context,
	request RunRequest,
	result RunResult,
	pending Action,
) (RunResult, error) {
	if r == nil {
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "runtime runner is required"})
	}
	toolCatalog, err := buildToolCatalog(request.Tools)
	if err != nil {
		return failResult(result, err)
	}
	if len(result.Steps) == 0 {
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "approval checkpoint has no execution step"})
	}

	stepIndex := len(result.Steps) - 1
	step := &result.Steps[stepIndex]
	actionIndex := -1
	for index, action := range step.Actions {
		if action.ID == pending.ID && action.Type == pending.Type {
			actionIndex = index
			break
		}
	}
	if actionIndex < 0 {
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "pending approval action is missing from the checkpoint step"})
	}
	step.Observations = observationsWithoutAction(step.Observations, pending.ID)

	admittedCtx := ctx
	release := ReleaseFunc(func() {})
	if r != nil && r.admission != nil {
		admittedCtx, release, err = r.admission.Acquire(ctx, AdmissionRequest{
			UserID: request.Context.UserID, WorkflowID: request.Context.WorkflowID,
		})
		if err != nil {
			if errors.Is(err, ErrConcurrencyLimitExceeded) {
				return failResult(result, &RunError{Code: ErrorConcurrencyLimit, Cause: err})
			}
			return failResult(result, contextRunError(err, step.Index))
		}
	}
	runCtx, cancel := withBudgetContext(admittedCtx, request.Context.Budget)

	for _, action := range step.Actions[actionIndex:] {
		observation, execErr := r.executeAction(runCtx, request.Context, toolCatalog, action, step.Index)
		step.Observations = append(step.Observations, observation)
		if execErr != nil {
			step.FinishedAt = r.now()
			cancel()
			release()
			if continuation, suspended := ToolContinuationFromError(execErr); suspended {
				return suspendForToolContinuation(result, action, continuation, step.Index)
			}
			if HasErrorCode(execErr, ErrorApprovalRequired) {
				result.Status = RunStatusApprovalRequired
				pendingAction := action
				result.PendingAction = &pendingAction
				result.PendingResumeKind = ResumeKindToolApproval
				result.ApprovalID = ApprovalIDFromError(execErr)
				return result, execErr
			}
			return failResult(result, execErr)
		}
		result.Messages = append(result.Messages, Message{
			Role:       RoleTool,
			Content:    observation.Content,
			Name:       action.Name,
			ToolCallID: action.ID,
		})
	}
	step.FinishedAt = r.now()
	cancel()
	release()

	return r.runFromState(ctx, request, result, step.Index+1)
}

func (r *ReActRunner) resumeSuspendedTool(
	ctx context.Context,
	request RunRequest,
	result RunResult,
	pending Action,
	continuation ToolContinuation,
	resume ToolResumeRequest,
) (RunResult, error) {
	if r == nil {
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "runtime runner is required"})
	}
	if err := validateToolContinuation(&continuation); err != nil {
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "tool continuation is invalid", Cause: err})
	}
	toolCatalog, err := buildToolCatalog(request.Tools)
	if err != nil {
		return failResult(result, err)
	}
	definition, ok := toolCatalog[pending.Name]
	if !ok || definition.ApprovalRequired() {
		return failResult(result, &RunError{
			Code: ErrorUnknownTool, ActionID: pending.ID,
			Message: fmt.Sprintf("resumable tool %q is no longer available as read-only", pending.Name),
		})
	}
	resumable, ok := r.tools.(ResumableToolExecutor)
	if !ok {
		return failResult(result, &RunError{
			Code: ErrorUnsupported, ActionID: pending.ID,
			Message: "resumable tool executor is not configured",
		})
	}
	if len(result.Steps) == 0 {
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "tool continuation checkpoint has no execution step"})
	}

	stepIndex := len(result.Steps) - 1
	step := &result.Steps[stepIndex]
	actionIndex := -1
	for index, action := range step.Actions {
		if action.ID == pending.ID && action.Type == pending.Type {
			actionIndex = index
			break
		}
	}
	if actionIndex < 0 {
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "suspended tool action is missing from the checkpoint step"})
	}
	step.Observations = observationsWithoutAction(step.Observations, pending.ID)

	admittedCtx := ctx
	release := ReleaseFunc(func() {})
	if r.admission != nil {
		admittedCtx, release, err = r.admission.Acquire(ctx, AdmissionRequest{
			UserID: request.Context.UserID, WorkflowID: request.Context.WorkflowID,
		})
		if err != nil {
			if errors.Is(err, ErrConcurrencyLimitExceeded) {
				return failResult(result, &RunError{Code: ErrorConcurrencyLimit, Cause: err})
			}
			return failResult(result, contextRunError(err, step.Index))
		}
	}
	runCtx, cancel := withBudgetContext(admittedCtx, request.Context.Budget)
	defer cancel()
	defer release()

	call := ToolCall{
		RunContext: request.Context,
		ActionID:   pending.ID,
		Name:       pending.Name,
		Arguments:  cloneRawMessage(pending.Arguments),
	}
	toolResult, execErr := resumable.ResumeTool(runCtx, ToolResumeRequest{
		Call:          call,
		Continuation:  cloneToolContinuation(continuation),
		HumanResponse: strings.TrimSpace(resume.HumanResponse),
		ApprovalID:    strings.TrimSpace(resume.ApprovalID),
		ResumeToken:   strings.TrimSpace(resume.ResumeToken),
	})
	observation := Observation{ActionID: pending.ID, Name: pending.Name}
	if execErr != nil {
		observation.IsError = true
		step.Observations = append(step.Observations, observation)
		step.FinishedAt = r.now()
		if runCtx.Err() != nil {
			return failResult(result, contextRunError(runCtx.Err(), step.Index))
		}
		if next, suspended := ToolContinuationFromError(execErr); suspended {
			return suspendForToolContinuation(result, pending, next, step.Index)
		}
		return failResult(result, &RunError{
			Code: ErrorTool, Step: step.Index, ActionID: pending.ID, Cause: execErr,
		})
	}
	observation.Content = toolResult.Content
	observation.StructuredContent = cloneRawMessage(toolResult.StructuredContent)
	step.Observations = append(step.Observations, observation)
	result.Messages = append(result.Messages, Message{
		Role: RoleTool, Content: observation.Content, Name: pending.Name, ToolCallID: pending.ID,
	})

	for _, action := range step.Actions[actionIndex+1:] {
		observation, actionErr := r.executeAction(runCtx, request.Context, toolCatalog, action, step.Index)
		step.Observations = append(step.Observations, observation)
		if actionErr != nil {
			step.FinishedAt = r.now()
			if next, suspended := ToolContinuationFromError(actionErr); suspended {
				return suspendForToolContinuation(result, action, next, step.Index)
			}
			if HasErrorCode(actionErr, ErrorApprovalRequired) {
				result.Status = RunStatusApprovalRequired
				pendingAction := action
				result.PendingAction = &pendingAction
				result.PendingResumeKind = ResumeKindToolApproval
				result.ApprovalID = ApprovalIDFromError(actionErr)
				return result, actionErr
			}
			return failResult(result, actionErr)
		}
		result.Messages = append(result.Messages, Message{
			Role:       RoleTool,
			Content:    observation.Content,
			Name:       action.Name,
			ToolCallID: action.ID,
		})
	}
	step.FinishedAt = r.now()
	return r.runFromState(ctx, request, result, step.Index+1)
}

func suspendForToolContinuation(
	result RunResult,
	action Action,
	continuation ToolContinuation,
	stepIndex int,
) (RunResult, error) {
	pendingAction := action
	result.PendingAction = &pendingAction
	result.PendingToolContinuation = &continuation
	switch continuation.ResumeKind {
	case ResumeKindDelegatedToolApproval:
		result.Status = RunStatusApprovalRequired
		result.PendingResumeKind = ResumeKindDelegatedToolApproval
		result.ApprovalID = strings.TrimSpace(continuation.ApprovalID)
		return result, &RunError{
			Code: ErrorApprovalRequired, Step: stepIndex, ActionID: action.ID,
			ApprovalID: result.ApprovalID, Message: "delegated tool approval required",
			Cause: ErrApprovalRequired,
		}
	default:
		result.Status = RunStatusAwaitingHuman
		result.PendingResumeKind = ResumeKindHumanResponse
		return result, nil
	}
}

func observationsWithoutAction(observations []Observation, actionID string) []Observation {
	filtered := make([]Observation, 0, len(observations))
	for _, observation := range observations {
		if observation.ActionID != actionID {
			filtered = append(filtered, observation)
		}
	}
	return filtered
}

func (r *ReActRunner) runFromState(
	ctx context.Context,
	request RunRequest,
	result RunResult,
	startStep int,
) (RunResult, error) {
	if r == nil || r.model == nil {
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "model client is required"})
	}

	maxSteps := request.Context.Budget.MaxSteps
	if maxSteps == 0 {
		maxSteps = DefaultMaxSteps
	}
	if maxSteps < 1 || maxSteps > MaxAllowedSteps {
		return failResult(result, &RunError{
			Code:    ErrorInvalidRequest,
			Message: fmt.Sprintf("max steps must be between 1 and %d", MaxAllowedSteps),
		})
	}
	request.Context.Budget.MaxSteps = maxSteps
	result.Context = request.Context
	if startStep < 1 {
		return failResult(result, &RunError{Code: ErrorInvalidRequest, Message: "invalid resume step"})
	}

	toolCatalog, err := buildToolCatalog(request.Tools)
	if err != nil {
		return failResult(result, err)
	}
	if !request.InitialToolChoice.Valid() {
		return failResult(result, &RunError{
			Code: ErrorInvalidRequest, Message: fmt.Sprintf("invalid initial tool choice %q", request.InitialToolChoice),
		})
	}
	if request.InitialToolChoice == ToolChoiceRequired && len(toolCatalog) == 0 {
		return failResult(result, &RunError{
			Code: ErrorInvalidRequest, Message: "required initial tool choice needs at least one tool",
		})
	}
	if request.InitialToolChoice == ToolChoiceRequired && maxSteps < 2 {
		return failResult(result, &RunError{
			Code: ErrorInvalidRequest, Message: "required initial tool choice needs at least two steps",
		})
	}

	admittedCtx := ctx
	release := ReleaseFunc(func() {})
	if r.admission != nil {
		admittedCtx, release, err = r.admission.Acquire(ctx, AdmissionRequest{
			UserID: request.Context.UserID, WorkflowID: request.Context.WorkflowID,
		})
		if err != nil {
			if errors.Is(err, ErrConcurrencyLimitExceeded) {
				return failResult(result, &RunError{Code: ErrorConcurrencyLimit, Cause: err})
			}
			return failResult(result, contextRunError(err, 0))
		}
	}
	defer release()

	runCtx, cancel := withBudgetContext(admittedCtx, request.Context.Budget)
	defer cancel()

	for stepIndex := startStep; stepIndex <= maxSteps; stepIndex++ {
		if err := runCtx.Err(); err != nil {
			return failResult(result, contextRunError(err, stepIndex))
		}

		step := Step{Index: stepIndex, RoleID: request.Context.RoleID, StartedAt: r.now()}
		modelRequest := ModelRequest{
			Context:         request.Context,
			StepIndex:       stepIndex,
			Model:           request.Model,
			Messages:        cloneMessages(result.Messages),
			Tools:           cloneToolDefinitions(request.Tools),
			MaxOutputTokens: request.Context.Budget.MaxOutputTokens,
		}
		if stepIndex == 1 {
			modelRequest.ToolChoice = request.InitialToolChoice
		}
		// A non-terminal action on the last step can never be synthesized into a
		// final answer. Remove the tool catalog instead of relying on providers to
		// honor tool_choice=none, and still reject any non-terminal action below.
		if stepIndex == maxSteps && len(toolCatalog) > 0 {
			modelRequest.Tools = nil
			modelRequest.ToolChoice = ""
			modelRequest.Messages = append(modelRequest.Messages, Message{
				Role: RoleSystem, Content: finalStepSystemInstruction,
			})
		}
		estimatedRequestUsage := r.tokenCounter.EstimateRequest(modelRequest)
		if err := admitModelCall(request.Context.Budget, result.Usage, estimatedRequestUsage, stepIndex); err != nil {
			return failResult(result, err)
		}
		if err := r.admitModelCost(request.Context.Budget, request.Model, result.Usage, estimatedRequestUsage, stepIndex); err != nil {
			return failResult(result, err)
		}
		sharedReservation, err := r.reserveSharedBudget(runCtx, request.Context.Budget, request.Model, estimatedRequestUsage)
		if err != nil {
			return failResult(result, err)
		}
		modelResponse, err := r.model.Complete(runCtx, modelRequest)
		if err != nil {
			sharedReservation.Release()
			if modelResponse.ModelRouting != nil {
				step.ModelRouting = cloneModelRoutingTrace(modelResponse.ModelRouting)
				step.FinishedAt = r.now()
				result.Steps = append(result.Steps, step)
			}
			if runCtx.Err() != nil {
				return failResult(result, contextRunError(runCtx.Err(), stepIndex))
			}
			return failResult(result, &RunError{Code: ErrorModel, Step: stepIndex, Cause: err})
		}

		step.ModelRouting = cloneModelRoutingTrace(modelResponse.ModelRouting)
		actions, err := normalizeActions(modelResponse, stepIndex)
		if err != nil {
			sharedReservation.Release()
			return failResult(result, err)
		}
		step.Model = modelResponse.Model
		step.Provider = modelResponse.Provider
		step.Actions = cloneActions(actions)
		step.Usage = resolvedModelUsage(r.tokenCounter, estimatedRequestUsage, modelResponse)
		if err := r.resolveCompletedCost(request.Model, modelResponse, &step.Usage); err != nil {
			sharedReservation.Release()
			step.FinishedAt = r.now()
			result.Steps = append(result.Steps, step)
			result.Usage.Add(step.Usage)
			return failResult(result, &RunError{Code: ErrorModel, Step: stepIndex, Message: "estimate model cost", Cause: err})
		}
		if err := sharedReservation.Commit(step.Usage); err != nil {
			step.FinishedAt = r.now()
			result.Steps = append(result.Steps, step)
			result.Usage.Add(step.Usage)
			return failResult(result, err)
		}
		if err := enforceCompletedUsage(request.Context.Budget, result.Usage, step.Usage, stepIndex); err != nil {
			step.FinishedAt = r.now()
			result.Steps = append(result.Steps, step)
			result.Usage.Add(step.Usage)
			return failResult(result, err)
		}
		result.Usage.Add(step.Usage)

		assistantMessage := modelResponse.Message
		assistantMessage.Role = RoleAssistant
		assistantMessage.Actions = cloneActions(actions)
		if actions[0].Type == ActionFinalAnswer && strings.TrimSpace(assistantMessage.Content) == "" {
			assistantMessage.Content = actions[0].Content
		}
		result.Messages = append(result.Messages, assistantMessage)

		if actions[0].Type == ActionFinalAnswer {
			step.FinishedAt = r.now()
			result.Steps = append(result.Steps, step)
			result.Status = RunStatusCompleted
			result.FinalAnswer = actions[0].Content
			return result, nil
		}
		if actions[0].Type == ActionAskHuman {
			step.FinishedAt = r.now()
			result.Steps = append(result.Steps, step)
			result.Status = RunStatusAwaitingHuman
			pending := actions[0]
			result.PendingAction = &pending
			result.PendingResumeKind = ResumeKindHumanResponse
			return result, nil
		}
		if stepIndex == maxSteps {
			step.FinishedAt = r.now()
			result.Steps = append(result.Steps, step)
			return failResult(result, &RunError{
				Code:    ErrorMaxSteps,
				Step:    maxSteps,
				Message: fmt.Sprintf("run returned a non-terminal action on reserved final step %d", maxSteps),
			})
		}

		for _, action := range actions {
			observation, execErr := r.executeAction(runCtx, request.Context, toolCatalog, action, stepIndex)
			step.Observations = append(step.Observations, observation)
			if execErr != nil {
				step.FinishedAt = r.now()
				result.Steps = append(result.Steps, step)
				if continuation, suspended := ToolContinuationFromError(execErr); suspended {
					return suspendForToolContinuation(result, action, continuation, step.Index)
				}
				if HasErrorCode(execErr, ErrorApprovalRequired) {
					result.Status = RunStatusApprovalRequired
					pending := action
					result.PendingAction = &pending
					result.PendingResumeKind = ResumeKindToolApproval
					result.ApprovalID = ApprovalIDFromError(execErr)
					return result, execErr
				}
				return failResult(result, execErr)
			}
			result.Messages = append(result.Messages, Message{
				Role:       RoleTool,
				Content:    observation.Content,
				Name:       action.Name,
				ToolCallID: action.ID,
			})
		}

		step.FinishedAt = r.now()
		result.Steps = append(result.Steps, step)
	}

	return failResult(result, &RunError{
		Code:    ErrorMaxSteps,
		Step:    maxSteps,
		Message: fmt.Sprintf("run did not reach a terminal action within %d steps", maxSteps),
	})
}

func (r *ReActRunner) reserveSharedBudget(
	ctx context.Context,
	budget Budget,
	model string,
	requestEstimate TokenUsage,
) (*UsageReservation, error) {
	tracker, ok := BudgetTrackerFromContext(ctx)
	if !ok {
		return &UsageReservation{}, nil
	}
	estimate := requestEstimate
	if budget.MaxOutputTokens > 0 {
		estimate.OutputTokens = budget.MaxOutputTokens
		estimate.TotalTokens = estimate.InputTokens + budget.MaxOutputTokens
	}
	if tracker.Budget().MaxEstimatedCostMicros > 0 {
		if r.costEstimator == nil {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: "shared workflow cost budget requires a cost estimator"}
		}
		cost, err := r.costEstimator.EstimateCost(model, estimate)
		if err != nil {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: "estimate shared workflow model cost", Cause: err}
		}
		estimate.EstimatedCostMicros = cost.Micros
		estimate.CostEstimated = true
		estimate.PricingVersion = cost.PricingVersion
	}
	return tracker.ReserveUsage(estimate)
}

func admitModelCall(budget Budget, consumed, estimated TokenUsage, step int) error {
	if budget.MaxInputTokens > 0 && estimated.InputTokens > budget.MaxInputTokens {
		return budgetExceededError(step, "input", estimated.InputTokens, budget.MaxInputTokens)
	}
	if budget.MaxTotalTokens <= 0 {
		return nil
	}
	reserved := consumed.TotalTokens + estimated.InputTokens
	if budget.MaxOutputTokens > 0 {
		reserved += budget.MaxOutputTokens
	}
	if reserved > budget.MaxTotalTokens {
		return budgetExceededError(step, "run total reservation", reserved, budget.MaxTotalTokens)
	}
	return nil
}

func enforceCompletedUsage(budget Budget, consumed, current TokenUsage, step int) error {
	if budget.MaxOutputTokens > 0 && current.OutputTokens > budget.MaxOutputTokens {
		return budgetExceededError(step, "output", current.OutputTokens, budget.MaxOutputTokens)
	}
	if budget.MaxTotalTokens > 0 && consumed.TotalTokens+current.TotalTokens > budget.MaxTotalTokens {
		return budgetExceededError(step, "run total", consumed.TotalTokens+current.TotalTokens, budget.MaxTotalTokens)
	}
	if budget.MaxEstimatedCostMicros > 0 && consumed.EstimatedCostMicros+current.EstimatedCostMicros > budget.MaxEstimatedCostMicros {
		return costBudgetExceededError(step, consumed.EstimatedCostMicros+current.EstimatedCostMicros, budget.MaxEstimatedCostMicros)
	}
	return nil
}

func (r *ReActRunner) admitModelCost(budget Budget, model string, consumed, requestEstimate TokenUsage, step int) error {
	if budget.MaxEstimatedCostMicros <= 0 {
		return nil
	}
	if r.costEstimator == nil {
		return &RunError{Code: ErrorInvalidRequest, Step: step, Message: "cost budget requires a cost estimator"}
	}
	if budget.MaxOutputTokens <= 0 {
		return &RunError{Code: ErrorInvalidRequest, Step: step, Message: "cost budget requires max output tokens"}
	}
	reservation := TokenUsage{
		InputTokens: requestEstimate.InputTokens, OutputTokens: budget.MaxOutputTokens,
		TotalTokens: requestEstimate.InputTokens + budget.MaxOutputTokens, Estimated: true,
	}
	estimate, err := r.costEstimator.EstimateCost(model, reservation)
	if err != nil {
		return &RunError{Code: ErrorInvalidRequest, Step: step, Message: "estimate model cost reservation", Cause: err}
	}
	if consumed.EstimatedCostMicros+estimate.Micros > budget.MaxEstimatedCostMicros {
		return costBudgetExceededError(step, consumed.EstimatedCostMicros+estimate.Micros, budget.MaxEstimatedCostMicros)
	}
	return nil
}

func (r *ReActRunner) resolveCompletedCost(requestedModel string, response ModelResponse, usage *TokenUsage) error {
	if usage == nil || usage.EstimatedCostMicros > 0 || r.costEstimator == nil {
		return nil
	}
	modelName := strings.TrimSpace(response.Model)
	if modelName == "" {
		modelName = requestedModel
	}
	estimate, err := r.costEstimator.EstimateCost(modelName, *usage)
	if err != nil {
		return err
	}
	usage.EstimatedCostMicros = estimate.Micros
	usage.CostEstimated = usage.Estimated
	usage.PricingVersion = estimate.PricingVersion
	return nil
}

func budgetExceededError(step int, dimension string, actual, limit int) *RunError {
	return &RunError{
		Code:    ErrorBudgetExceeded,
		Step:    step,
		Message: fmt.Sprintf("%s token budget exceeded: required %d, limit %d", dimension, actual, limit),
	}
}

func costBudgetExceededError(step int, actual, limit int64) *RunError {
	return &RunError{
		Code:    ErrorBudgetExceeded,
		Step:    step,
		Message: fmt.Sprintf("run estimated cost budget exceeded: required %d micros, limit %d micros", actual, limit),
	}
}

func resolvedModelUsage(counter TokenCounter, requestEstimate TokenUsage, response ModelResponse) TokenUsage {
	usage := response.Usage
	if usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.TotalTokens > 0 {
		if usage.TotalTokens == 0 {
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}
		return usage
	}
	responseEstimate := counter.EstimateResponse(response)
	usage = TokenUsage{
		InputTokens:  requestEstimate.InputTokens,
		OutputTokens: responseEstimate.OutputTokens,
		Estimated:    true,
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	return usage
}

func (r *ReActRunner) executeAction(
	ctx context.Context,
	runContext RunContext,
	toolCatalog map[string]ToolDefinition,
	action Action,
	step int,
) (Observation, error) {
	observation := Observation{ActionID: action.ID, Name: action.Name}
	if err := ctx.Err(); err != nil {
		observation.IsError = true
		return observation, contextRunError(err, step)
	}

	switch action.Type {
	case ActionToolCall:
		definition, ok := toolCatalog[action.Name]
		if !ok {
			observation.IsError = true
			return observation, &RunError{
				Code: ErrorUnknownTool, Step: step, ActionID: action.ID,
				Message: fmt.Sprintf("tool %q is not available", action.Name),
			}
		}
		if r.tools == nil {
			observation.IsError = true
			return observation, &RunError{
				Code: ErrorUnsupported, Step: step, ActionID: action.ID,
				Message: "tool executor is not configured",
			}
		}
		call := ToolCall{
			RunContext: runContext,
			ActionID:   action.ID,
			Name:       action.Name,
			Arguments:  cloneRawMessage(action.Arguments),
		}
		var toolResult ToolResult
		var err error
		if definition.ApprovalRequired() {
			approvalExecutor, ok := r.tools.(ApprovalToolExecutor)
			if !ok {
				observation.IsError = true
				return observation, &RunError{
					Code: ErrorApprovalRequired, Step: step, ActionID: action.ID,
					Message: fmt.Sprintf("tool %q requires a governed approval executor", action.Name), Cause: ErrApprovalRequired,
				}
			}
			toolResult, err = approvalExecutor.ExecuteApprovalGated(ctx, call)
		} else {
			toolResult, err = r.tools.Execute(ctx, call)
		}
		if err != nil {
			observation.IsError = true
			if ctx.Err() != nil {
				return observation, contextRunError(ctx.Err(), step)
			}
			if HasErrorCode(err, ErrorApprovalRequired) || errors.Is(err, ErrApprovalRequired) {
				return observation, &RunError{
					Code: ErrorApprovalRequired, Step: step, ActionID: action.ID,
					ApprovalID: ApprovalIDFromError(err),
					Message:    fmt.Sprintf("tool %q requires approval", action.Name), Cause: err,
				}
			}
			return observation, &RunError{Code: ErrorTool, Step: step, ActionID: action.ID, Cause: err}
		}
		observation.Content = toolResult.Content
		observation.StructuredContent = cloneRawMessage(toolResult.StructuredContent)
		return observation, nil

	case ActionRAGSearch:
		if r.rag == nil {
			observation.IsError = true
			return observation, &RunError{
				Code: ErrorUnsupported, Step: step, ActionID: action.ID,
				Message: "RAG searcher is not configured",
			}
		}
		ragResult, err := r.rag.Search(ctx, RAGQuery{
			RunContext: runContext,
			ActionID:   action.ID,
			Name:       action.Name,
			Arguments:  cloneRawMessage(action.Arguments),
		})
		if err != nil {
			observation.IsError = true
			if ctx.Err() != nil {
				return observation, contextRunError(ctx.Err(), step)
			}
			return observation, &RunError{Code: ErrorRAG, Step: step, ActionID: action.ID, Cause: err}
		}
		observation.Content = ragResult.Content
		return observation, nil
	default:
		observation.IsError = true
		return observation, &RunError{
			Code: ErrorUnsupported, Step: step, ActionID: action.ID,
			Message: fmt.Sprintf("action type %q cannot be executed", action.Type),
		}
	}
}

func normalizeActions(response ModelResponse, step int) ([]Action, error) {
	actions := response.Actions
	if len(actions) == 0 {
		actions = response.Message.Actions
	}
	if len(actions) == 0 && strings.TrimSpace(response.Message.Content) != "" {
		actions = []Action{{Type: ActionFinalAnswer, Content: response.Message.Content}}
	}
	if len(actions) == 0 {
		return nil, &RunError{Code: ErrorEmptyResponse, Step: step, Message: "model returned no content or actions"}
	}

	terminal := false
	for index := range actions {
		action := &actions[index]
		if action.ID == "" {
			action.ID = fmt.Sprintf("step-%d-action-%d", step, index+1)
		}
		switch action.Type {
		case ActionFinalAnswer:
			if action.Content == "" {
				action.Content = response.Message.Content
			}
			if strings.TrimSpace(action.Content) == "" {
				return nil, invalidActionError(step, action.ID, "final answer is empty")
			}
			terminal = true
		case ActionAskHuman:
			if strings.TrimSpace(action.Content) == "" {
				return nil, invalidActionError(step, action.ID, "human question is empty")
			}
			terminal = true
		case ActionToolCall, ActionRAGSearch:
			if strings.TrimSpace(action.Name) == "" {
				return nil, invalidActionError(step, action.ID, "action name is required")
			}
			if len(action.Arguments) == 0 {
				action.Arguments = json.RawMessage("{}")
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(action.Arguments, &object); err != nil {
				return nil, &RunError{
					Code: ErrorInvalidAction, Step: step, ActionID: action.ID,
					Message: "arguments must be a valid JSON object", Cause: err,
				}
			}
		default:
			return nil, invalidActionError(step, action.ID, fmt.Sprintf("unknown action type %q", action.Type))
		}
	}
	if terminal && len(actions) != 1 {
		return nil, &RunError{
			Code: ErrorInvalidAction, Step: step,
			Message: "terminal actions cannot be combined with other actions",
		}
	}
	return actions, nil
}

func buildToolCatalog(tools []ToolDefinition) (map[string]ToolDefinition, error) {
	catalog := make(map[string]ToolDefinition, len(tools))
	for _, tool := range tools {
		tool.Name = strings.TrimSpace(tool.Name)
		if tool.Name == "" {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: "tool name is required"}
		}
		if _, exists := catalog[tool.Name]; exists {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: fmt.Sprintf("duplicate tool %q", tool.Name)}
		}
		catalog[tool.Name] = tool
	}
	return catalog, nil
}

func withBudgetContext(parent context.Context, budget Budget) (context.Context, context.CancelFunc) {
	deadline := budget.Deadline
	if budget.Timeout > 0 {
		timeoutDeadline := time.Now().Add(budget.Timeout)
		if deadline.IsZero() || timeoutDeadline.Before(deadline) {
			deadline = timeoutDeadline
		}
	}
	if !deadline.IsZero() {
		return context.WithDeadline(parent, deadline)
	}
	return context.WithCancel(parent)
}

// WithBudgetContext exposes the same timeout/deadline semantics to workflow
// schedulers without duplicating Runtime budget logic.
func WithBudgetContext(parent context.Context, budget Budget) (context.Context, context.CancelFunc) {
	return withBudgetContext(parent, budget)
}

func contextRunError(err error, step int) *RunError {
	code := ErrorCanceled
	if errors.Is(err, context.DeadlineExceeded) {
		code = ErrorTimeout
	}
	return &RunError{Code: code, Step: step, Cause: err}
}

func invalidActionError(step int, actionID, message string) *RunError {
	return &RunError{Code: ErrorInvalidAction, Step: step, ActionID: actionID, Message: message}
}

func failResult(result RunResult, err error) (RunResult, error) {
	result.Status = RunStatusFailed
	return result, err
}

func cloneMessages(messages []Message) []Message {
	cloned := make([]Message, len(messages))
	for index, message := range messages {
		cloned[index] = message
		cloned[index].Actions = cloneActions(message.Actions)
	}
	return cloned
}

func cloneActions(actions []Action) []Action {
	cloned := make([]Action, len(actions))
	for index, action := range actions {
		cloned[index] = action
		cloned[index].Arguments = cloneRawMessage(action.Arguments)
	}
	return cloned
}

func cloneToolDefinitions(tools []ToolDefinition) []ToolDefinition {
	cloned := make([]ToolDefinition, len(tools))
	for index, tool := range tools {
		cloned[index] = tool
		cloned[index].InputSchema = cloneRawMessage(tool.InputSchema)
	}
	return cloned
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
