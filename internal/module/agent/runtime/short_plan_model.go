package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxShortPlanResponseBytes     = 32 << 10
	maxShortPlanPromptBytes       = 64 << 10
	maxShortPlanJSONDepth         = 16
	defaultShortPlanOutputTokens  = 768
	defaultShortPlanParseRepairs  = 1
	maximumShortPlanParseRepairs  = 1
	shortPlanStructuredOutputHint = "Return exactly one JSON object and nothing else. Do not use Markdown fences or commentary."
)

type ModelShortHorizonPlanner struct {
	model           ModelClient
	tokenCounter    TokenCounter
	costEstimator   CostEstimator
	maxOutputTokens int
	maxParseRepairs int
}

type ModelShortHorizonPlannerOption func(*ModelShortHorizonPlanner)

func WithShortPlanTokenCounter(counter TokenCounter) ModelShortHorizonPlannerOption {
	return func(planner *ModelShortHorizonPlanner) {
		if counter != nil {
			planner.tokenCounter = counter
		}
	}
}

func WithShortPlanCostEstimator(estimator CostEstimator) ModelShortHorizonPlannerOption {
	return func(planner *ModelShortHorizonPlanner) {
		planner.costEstimator = estimator
	}
}

func WithShortPlanMaxOutputTokens(limit int) ModelShortHorizonPlannerOption {
	return func(planner *ModelShortHorizonPlanner) {
		planner.maxOutputTokens = limit
	}
}

func WithShortPlanParseRepairs(limit int) ModelShortHorizonPlannerOption {
	return func(planner *ModelShortHorizonPlanner) {
		planner.maxParseRepairs = limit
	}
}

func NewModelShortHorizonPlanner(
	model ModelClient,
	options ...ModelShortHorizonPlannerOption,
) (*ModelShortHorizonPlanner, error) {
	if model == nil {
		return nil, errors.New("short plan model client is required")
	}
	planner := &ModelShortHorizonPlanner{
		model:           model,
		tokenCounter:    NewHeuristicTokenCounter(),
		maxOutputTokens: defaultShortPlanOutputTokens,
		maxParseRepairs: defaultShortPlanParseRepairs,
	}
	for _, option := range options {
		if option != nil {
			option(planner)
		}
	}
	if planner.tokenCounter == nil {
		return nil, errors.New("short plan token counter is required")
	}
	if planner.maxOutputTokens <= 0 {
		return nil, errors.New("short plan maximum output tokens must be positive")
	}
	if planner.maxParseRepairs < 0 || planner.maxParseRepairs > maximumShortPlanParseRepairs {
		return nil, fmt.Errorf("short plan parse repairs must be between 0 and %d", maximumShortPlanParseRepairs)
	}
	return planner, nil
}

func (planner *ModelShortHorizonPlanner) Plan(
	ctx context.Context,
	request ShortPlanRequest,
) (ShortPlanResult, error) {
	result := ShortPlanResult{}
	if ctx == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "short plan context is required"}
	}
	if planner == nil || planner.model == nil || planner.tokenCounter == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "short plan model planner is not configured"}
	}
	if err := ctx.Err(); err != nil {
		return result, contextRunError(err, request.CompletedSteps)
	}
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "short plan model is required"}
	}
	if err := request.Task.Validate(); err != nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "invalid planning task", Cause: err}
	}
	if request.CompletedSteps < 0 {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "completed planning steps cannot be negative"}
	}

	maxSteps := request.Budget.MaxSteps
	if maxSteps == 0 {
		maxSteps = DefaultMaxSteps
	}
	if maxSteps < 1 || maxSteps > MaxAllowedSteps {
		return result, &RunError{
			Code:    ErrorInvalidRequest,
			Message: fmt.Sprintf("max steps must be between 1 and %d", MaxAllowedSteps),
		}
	}
	if request.CompletedSteps >= maxSteps {
		return result, &RunError{
			Code: ErrorBudgetExceeded, Step: request.CompletedSteps + 1,
			Message: "short plan requires another step but the run step budget is exhausted",
		}
	}

	messages, err := buildShortPlanModelMessages(request, maxSteps)
	if err != nil {
		return result, err
	}
	callBudget := request.Budget
	callBudget.MaxSteps = maxSteps
	callBudget.MaxOutputTokens = planner.maxOutputTokens
	if request.Budget.MaxOutputTokens > 0 && request.Budget.MaxOutputTokens < callBudget.MaxOutputTokens {
		callBudget.MaxOutputTokens = request.Budget.MaxOutputTokens
	}

	for attempt := 0; attempt <= planner.maxParseRepairs; attempt++ {
		step := request.CompletedSteps + attempt + 1
		attemptMessages := cloneMessages(messages)
		if attempt > 0 {
			attemptMessages = append(attemptMessages, Message{
				Role: RoleDeveloper,
				Content: "The previous response did not satisfy the required JSON contract. " +
					shortPlanStructuredOutputHint + " Use only the supplied tool names and criterion IDs.",
			})
		}
		modelRequest := ModelRequest{
			Context:         request.Context,
			StepIndex:       step,
			Model:           request.Model,
			Messages:        attemptMessages,
			ToolChoice:      ToolChoiceNone,
			MaxOutputTokens: callBudget.MaxOutputTokens,
		}
		modelRequest.Context.Budget = callBudget
		estimatedRequestUsage := planner.tokenCounter.EstimateRequest(modelRequest)
		if err := admitModelCall(callBudget, result.Usage, estimatedRequestUsage, step); err != nil {
			return result, err
		}
		if err := admitShortPlanModelCost(
			callBudget, planner.costEstimator, request.Model, result.Usage, estimatedRequestUsage, step,
		); err != nil {
			return result, err
		}
		reservation, err := reserveShortPlanSharedBudget(
			ctx, callBudget, planner.costEstimator, request.Model, estimatedRequestUsage,
		)
		if err != nil {
			return result, err
		}

		result.Attempts++
		response, callErr := planner.model.Complete(ctx, modelRequest)
		if callErr != nil {
			reservation.Release()
			if ctx.Err() != nil {
				return result, contextRunError(ctx.Err(), step)
			}
			return result, &RunError{Code: ErrorModel, Step: step, Cause: callErr}
		}
		usage := resolvedModelUsage(planner.tokenCounter, estimatedRequestUsage, response)
		if err := resolveShortPlanModelCost(planner.costEstimator, request.Model, response, &usage); err != nil {
			reservation.Release()
			return result, &RunError{Code: ErrorModel, Step: step, Message: "estimate short plan model cost", Cause: err}
		}
		if err := reservation.Commit(usage); err != nil {
			return result, err
		}
		if err := enforceCompletedUsage(callBudget, result.Usage, usage, step); err != nil {
			result.Usage.Add(usage)
			return result, err
		}
		result.Usage.Add(usage)
		result.Model = strings.TrimSpace(response.Model)
		if result.Model == "" {
			result.Model = request.Model
		}
		result.Provider = strings.TrimSpace(response.Provider)

		content, contentErr := shortPlanModelContent(response)
		if contentErr == nil {
			result.Proposal, contentErr = DecodeShortPlanProposal([]byte(content))
		}
		if contentErr == nil {
			return result, nil
		}
		if attempt == planner.maxParseRepairs {
			return result, &RunError{
				Code: ErrorInvalidAction, Step: step,
				Message: fmt.Sprintf("model did not return a valid short plan after %d attempt(s)", result.Attempts),
				Cause:   contentErr,
			}
		}
	}
	return result, &RunError{Code: ErrorInvalidAction, Message: "short plan model attempts exhausted"}
}

func buildShortPlanModelMessages(request ShortPlanRequest, maxSteps int) ([]Message, error) {
	targetCriteria, _, err := planningCriteria(request.Task, request.TargetCriterionIDs)
	if err != nil {
		return nil, err
	}
	recoveryInstruction, err := shortPlanRecoveryInstruction(request.RecoveryFeedback)
	if err != nil {
		return nil, err
	}
	repairInstruction, err := shortPlanAdmissionRepairInstruction(request.RepairFeedback)
	if err != nil {
		return nil, err
	}
	type promptConstraint struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	}
	type promptCriterion struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Required    bool   `json:"required"`
	}
	type promptTool struct {
		Name             string       `json:"name"`
		Description      string       `json:"description,omitempty"`
		Category         ToolCategory `json:"category"`
		RequiresApproval bool         `json:"requires_approval"`
	}
	type promptContext struct {
		Goal               string             `json:"goal"`
		Constraints        []promptConstraint `json:"constraints"`
		CompletionCriteria []promptCriterion  `json:"completion_criteria"`
		TargetCriterionIDs []string           `json:"target_criterion_ids"`
		AvailableTools     []promptTool       `json:"available_tools"`
		CompletedSteps     int                `json:"completed_steps"`
		MaximumPlanSteps   int                `json:"maximum_plan_steps"`
	}

	catalog, err := buildToolCatalog(request.AvailableTools)
	if err != nil {
		return nil, err
	}
	toolNames := make([]string, 0, len(catalog))
	for name, definition := range catalog {
		if !validToolCategory(definition.Category) {
			return nil, &RunError{
				Code:    ErrorInvalidRequest,
				Message: fmt.Sprintf("tool %q has an invalid policy category", name),
			}
		}
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	contextPayload := promptContext{
		Goal:               strings.TrimSpace(request.Task.Goal),
		TargetCriterionIDs: targetCriteria,
		CompletedSteps:     request.CompletedSteps,
		MaximumPlanSteps:   min(MaxShortPlanSteps, maxSteps-request.CompletedSteps),
	}
	for _, constraint := range request.Task.Constraints {
		contextPayload.Constraints = append(contextPayload.Constraints, promptConstraint{
			ID: strings.TrimSpace(constraint.ID), Description: strings.TrimSpace(constraint.Description),
		})
	}
	for _, criterion := range request.Task.CompletionCriteria {
		contextPayload.CompletionCriteria = append(contextPayload.CompletionCriteria, promptCriterion{
			ID: strings.TrimSpace(criterion.ID), Description: strings.TrimSpace(criterion.Description), Required: criterion.Required,
		})
	}
	for _, name := range toolNames {
		definition := catalog[name]
		contextPayload.AvailableTools = append(contextPayload.AvailableTools, promptTool{
			Name: name, Description: strings.TrimSpace(definition.Description), Category: definition.Category,
			RequiresApproval: definition.ApprovalRequired(),
		})
	}
	payload, err := json.Marshal(contextPayload)
	if err != nil {
		return nil, &RunError{Code: ErrorInvalidRequest, Message: "marshal short plan context", Cause: err}
	}
	if len(payload) > maxShortPlanPromptBytes {
		return nil, &RunError{
			Code:    ErrorInvalidRequest,
			Message: fmt.Sprintf("short plan context exceeds %d bytes", maxShortPlanPromptBytes),
		}
	}

	systemPrompt := `You are a short-horizon task planner. Propose only the next one to three actions.
The response schema is:
{"version":"agent.short_plan.v1","steps":[{"id":"stable-short-id","kind":"tool|ask_human|respond","objective":"bounded action objective","tool_name":"required only for tool","criterion_ids":["supplied-criterion-id"]}]}
Every step must address at least one supplied completion criterion. Use a tool only when it is listed in available_tools. ask_human and respond are terminal and may appear only as the final step. Authorization, approval and completion are decided by deterministic runtime policy, not by you. ` + shortPlanStructuredOutputHint
	messages := []Message{
		{Role: RoleSystem, Content: systemPrompt},
		{Role: RoleUser, Content: string(payload)},
	}
	if recoveryInstruction != "" {
		messages = append(messages, Message{Role: RoleDeveloper, Content: recoveryInstruction})
	}
	if repairInstruction != "" {
		messages = append(messages, Message{Role: RoleDeveloper, Content: repairInstruction})
	}
	return messages, nil
}

func shortPlanModelContent(response ModelResponse) (string, error) {
	actions := response.Actions
	if len(actions) == 0 {
		actions = response.Message.Actions
	}
	if len(actions) > 0 {
		if len(actions) != 1 || actions[0].Type != ActionFinalAnswer {
			return "", errors.New("short plan model returned a non-text action")
		}
	}
	content := strings.TrimSpace(response.Message.Content)
	if content == "" && len(actions) == 1 {
		content = strings.TrimSpace(actions[0].Content)
	}
	if content == "" {
		return "", errors.New("short plan model returned empty content")
	}
	return content, nil
}

func DecodeShortPlanProposal(payload []byte) (ShortPlanProposal, error) {
	if len(payload) > maxShortPlanResponseBytes {
		return ShortPlanProposal{}, fmt.Errorf("short plan response exceeds %d bytes", maxShortPlanResponseBytes)
	}
	if !utf8.Valid(payload) {
		return ShortPlanProposal{}, errors.New("short plan response is not valid UTF-8")
	}
	if err := validateUniqueShortPlanJSONKeys(payload); err != nil {
		return ShortPlanProposal{}, fmt.Errorf("validate short plan JSON: %w", err)
	}
	if err := validateExactShortPlanFields(payload); err != nil {
		return ShortPlanProposal{}, err
	}
	var proposal ShortPlanProposal
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return ShortPlanProposal{}, fmt.Errorf("decode short plan: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ShortPlanProposal{}, errors.New("short plan response contains multiple JSON values")
		}
		return ShortPlanProposal{}, fmt.Errorf("decode short plan trailer: %w", err)
	}
	return proposal, nil
}

func validateExactShortPlanFields(payload []byte) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("decode short plan envelope: %w", err)
	}
	if err := requireExactJSONFields(envelope, []string{"steps", "version"}, "short plan"); err != nil {
		return err
	}
	var steps []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["steps"], &steps); err != nil {
		return fmt.Errorf("decode short plan steps: %w", err)
	}
	if len(steps) == 0 || len(steps) > MaxShortPlanSteps {
		return fmt.Errorf("short plan must contain between 1 and %d steps", MaxShortPlanSteps)
	}
	for index, step := range steps {
		if step == nil {
			return fmt.Errorf("short plan step %d must be an object", index+1)
		}
		if err := requireExactJSONFieldsWithOptional(
			step,
			[]string{"criterion_ids", "id", "kind", "objective"},
			[]string{"tool_name"},
			fmt.Sprintf("short plan step %d", index+1),
		); err != nil {
			return err
		}
	}
	return nil
}

func requireExactJSONFields(value map[string]json.RawMessage, required []string, label string) error {
	return requireExactJSONFieldsWithOptional(value, required, nil, label)
}

func requireExactJSONFieldsWithOptional(
	value map[string]json.RawMessage,
	required []string,
	optional []string,
	label string,
) error {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, field := range required {
		allowed[field] = struct{}{}
		if _, exists := value[field]; !exists {
			return fmt.Errorf("%s is missing required field %q", label, field)
		}
	}
	for _, field := range optional {
		allowed[field] = struct{}{}
	}
	for field := range value {
		if _, exists := allowed[field]; !exists {
			return fmt.Errorf("%s contains unknown field %q", label, field)
		}
	}
	return nil
}

func validateUniqueShortPlanJSONKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := consumeShortPlanJSONValue(decoder, "$", 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("contains multiple JSON values")
		}
		return fmt.Errorf("decode JSON trailer: %w", err)
	}
	return nil
}

func consumeShortPlanJSONValue(decoder *json.Decoder, path string, depth int) error {
	if depth > maxShortPlanJSONDepth {
		return fmt.Errorf("JSON nesting exceeds %d levels at %s", maxShortPlanJSONDepth, path)
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return fmt.Errorf("decode object key at %s: %w", path, keyErr)
			}
			key, keyOK := keyToken.(string)
			if !keyOK {
				return fmt.Errorf("object key at %s is not a string", path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object key %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := consumeShortPlanJSONValue(decoder, path+"."+key, depth+1); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil {
			return fmt.Errorf("close object at %s: %w", path, closeErr)
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("object at %s has invalid closing delimiter", path)
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := consumeShortPlanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil {
			return fmt.Errorf("close array at %s: %w", path, closeErr)
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("array at %s has invalid closing delimiter", path)
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, path)
	}
	return nil
}

func admitShortPlanModelCost(
	budget Budget,
	estimator CostEstimator,
	model string,
	consumed TokenUsage,
	requestEstimate TokenUsage,
	step int,
) error {
	if budget.MaxEstimatedCostMicros <= 0 {
		return nil
	}
	if estimator == nil {
		return &RunError{Code: ErrorInvalidRequest, Step: step, Message: "short plan cost budget requires a cost estimator"}
	}
	reservation := TokenUsage{
		InputTokens: requestEstimate.InputTokens, OutputTokens: budget.MaxOutputTokens,
		TotalTokens: requestEstimate.InputTokens + budget.MaxOutputTokens, Estimated: true,
	}
	estimate, err := estimator.EstimateCost(model, reservation)
	if err != nil {
		return &RunError{Code: ErrorInvalidRequest, Step: step, Message: "estimate short plan model cost reservation", Cause: err}
	}
	if consumed.EstimatedCostMicros+estimate.Micros > budget.MaxEstimatedCostMicros {
		return costBudgetExceededError(step, consumed.EstimatedCostMicros+estimate.Micros, budget.MaxEstimatedCostMicros)
	}
	return nil
}

func resolveShortPlanModelCost(
	estimator CostEstimator,
	requestedModel string,
	response ModelResponse,
	usage *TokenUsage,
) error {
	if usage == nil || usage.EstimatedCostMicros > 0 || estimator == nil {
		return nil
	}
	modelName := strings.TrimSpace(response.Model)
	if modelName == "" {
		modelName = requestedModel
	}
	estimate, err := estimator.EstimateCost(modelName, *usage)
	if err != nil {
		return err
	}
	usage.EstimatedCostMicros = estimate.Micros
	usage.CostEstimated = usage.Estimated
	usage.PricingVersion = estimate.PricingVersion
	return nil
}

func reserveShortPlanSharedBudget(
	ctx context.Context,
	budget Budget,
	estimator CostEstimator,
	model string,
	requestEstimate TokenUsage,
) (*UsageReservation, error) {
	tracker, ok := BudgetTrackerFromContext(ctx)
	if !ok {
		return &UsageReservation{}, nil
	}
	estimate := requestEstimate
	estimate.OutputTokens = budget.MaxOutputTokens
	estimate.TotalTokens = estimate.InputTokens + estimate.OutputTokens
	if tracker.Budget().MaxEstimatedCostMicros > 0 {
		if estimator == nil {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: "shared short plan cost budget requires a cost estimator"}
		}
		cost, err := estimator.EstimateCost(model, estimate)
		if err != nil {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: "estimate shared short plan model cost", Cause: err}
		}
		estimate.EstimatedCostMicros = cost.Micros
		estimate.CostEstimated = true
		estimate.PricingVersion = cost.PricingVersion
	}
	return tracker.ReserveUsage(estimate)
}
