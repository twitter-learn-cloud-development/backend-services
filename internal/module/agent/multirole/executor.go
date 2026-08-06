package multirole

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentMessage "twitter-clone/internal/module/agent/message"
	"twitter-clone/internal/module/agent/profile"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

const (
	RoleResearcher = "researcher"
	RoleDrafter    = "drafter"
	RoleReviewer   = "reviewer"

	DefaultMaxInputTokens = 12000
)

var (
	ErrPlanUnsupported      = errors.New("multi-role execution plan is unsupported")
	ErrRoleExecutionFailed  = errors.New("multi-role execution failed")
	ErrRequiredToolEvidence = errors.New("required read tool produced no successful evidence")
)

type RoleExecutionError struct {
	RoleID string
	Err    error
}

func (e *RoleExecutionError) Error() string {
	if e == nil {
		return ErrRoleExecutionFailed.Error()
	}
	return fmt.Sprintf("%s: role %s: %v", ErrRoleExecutionFailed, e.RoleID, e.Err)
}

func (e *RoleExecutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *RoleExecutionError) Is(target error) bool {
	return target == ErrRoleExecutionFailed
}

type EvidenceHandoffBuilder interface {
	BuildEvidenceHandoff(summary string, research agentRuntime.RunResult) (string, error)
}

type EvidenceHandoffBuilderFunc func(string, agentRuntime.RunResult) (string, error)

func (f EvidenceHandoffBuilderFunc) BuildEvidenceHandoff(
	summary string,
	research agentRuntime.RunResult,
) (string, error) {
	if f == nil {
		return "", errors.New("multi-role evidence handoff builder is nil")
	}
	return f(summary, research)
}

type Profiles struct {
	Parent     profile.AgentProfile
	Researcher profile.AgentProfile
	Drafter    profile.AgentProfile
	Reviewer   profile.AgentProfile
}

type Request struct {
	ParentContext agentRuntime.RunContext
	Plan          agentStrategy.Plan
	Model         string
	Input         string
	History       []agentRuntime.Message
	Tools         []agentRuntime.ToolDefinition
	RequiredTool  string
	Profiles      Profiles
	Handoff       EvidenceHandoffBuilder
}

type RoleResult struct {
	RoleID string
	Result agentRuntime.RunResult
	Build  agentMessage.BuildResult
}

type Result struct {
	Aggregate agentRuntime.RunResult
	Roles     []RoleResult
}

func (r Result) Role(roleID string) (RoleResult, bool) {
	for _, role := range r.Roles {
		if role.RoleID == roleID {
			return role, true
		}
	}
	return RoleResult{}, false
}

func (r Result) EstimatedContextTokens() int {
	total := 0
	for _, role := range r.Roles {
		total += role.Build.EstimatedTokens
	}
	return total
}

type Executor struct {
	runner   agentRuntime.AgentRunner
	messages agentMessage.Builder
	now      func() time.Time
}

func NewExecutor(runner agentRuntime.AgentRunner, messages agentMessage.Builder) *Executor {
	if messages == nil {
		messages = agentMessage.NewBuilder(nil, nil)
	}
	return &Executor{runner: runner, messages: messages, now: time.Now}
}

func (e *Executor) Execute(ctx context.Context, request Request) (Result, error) {
	if e == nil || e.runner == nil {
		return Result{}, errors.New("multi-role runtime runner is not configured")
	}
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	if err := ValidateRoleBudgets(request.Plan, request.Profiles); err != nil {
		return Result{}, err
	}

	researchRole := request.Plan.Roles[0]
	draftRole := request.Plan.Roles[1]
	reviewRole := request.Plan.Roles[2]
	researchTools := filterToolsByNames(
		request.Profiles.Researcher.FilterTools(request.Profiles.Parent.FilterTools(request.Tools)),
		researchRole.AllowedTools,
	)
	if err := ValidateRequiredReadTool(researchTools, request.RequiredTool); err != nil {
		return Result{}, fmt.Errorf("multi-role researcher tool scope: %w", err)
	}

	parentBudget := ParentBudget(request.Plan)
	if err := ValidateParentBudget(parentBudget, request.Profiles.Parent.Budget); err != nil {
		return Result{}, err
	}
	parentTracker, err := agentRuntime.NewBudgetTracker(parentBudget)
	if err != nil {
		return Result{}, fmt.Errorf("create multi-role parent budget: %w", err)
	}
	executionCtx := agentRuntime.ContextWithBudgetTracker(ctx, parentTracker)
	executionCtx, cancel := agentRuntime.WithBudgetContext(executionCtx, parentBudget)
	defer cancel()

	parentContext := request.ParentContext
	parentContext.StrategyPlanDigest = request.Plan.PlanDigest
	parentContext.Budget = parentBudget
	if parentContext.StartedAt.IsZero() {
		parentContext.StartedAt = e.now()
	}
	result := Result{Roles: make([]RoleResult, 0, len(request.Plan.Roles))}
	fail := func(roleID string, roleErr error) (Result, error) {
		typed := NewRoleExecutionError(roleID, roleErr)
		result.Aggregate = aggregate(parentContext, result.Roles, "", agentRuntime.RunStatusFailed)
		return result, typed
	}

	researchResult, researchBuild, err := e.runRole(
		executionCtx, parentContext, request.Model, request.Input, request.History,
		researchTools, researchRole, request.Profiles.Researcher, agentRuntime.ToolChoiceRequired,
	)
	result.Roles = append(result.Roles, RoleResult{RoleID: researchRole.RoleID, Result: researchResult, Build: researchBuild})
	if err != nil {
		return fail(researchRole.RoleID, err)
	}
	if !HasSuccessfulToolEvidence(researchResult, request.RequiredTool) {
		return fail(researchRole.RoleID, fmt.Errorf("%w: %s", ErrRequiredToolEvidence, request.RequiredTool))
	}
	researchSummary, err := CompletedRoleResponse(researchRole.RoleID, researchResult)
	if err != nil {
		return fail(researchRole.RoleID, err)
	}
	handoff, err := request.Handoff.BuildEvidenceHandoff(researchSummary, researchResult)
	if err != nil {
		return fail(researchRole.RoleID, err)
	}
	if strings.TrimSpace(handoff) == "" {
		return fail(researchRole.RoleID, errors.New("multi-role evidence handoff is empty"))
	}

	draftResult, draftBuild, err := e.runRole(
		executionCtx, parentContext, request.Model, DraftInput(request.Input, handoff), nil,
		nil, draftRole, request.Profiles.Drafter, "",
	)
	result.Roles = append(result.Roles, RoleResult{RoleID: draftRole.RoleID, Result: draftResult, Build: draftBuild})
	if err != nil {
		return fail(draftRole.RoleID, err)
	}
	draft, err := CompletedRoleResponse(draftRole.RoleID, draftResult)
	if err != nil {
		return fail(draftRole.RoleID, err)
	}

	reviewResult, reviewBuild, err := e.runRole(
		executionCtx, parentContext, request.Model, ReviewInput(request.Input, handoff, draft), nil,
		nil, reviewRole, request.Profiles.Reviewer, "",
	)
	result.Roles = append(result.Roles, RoleResult{RoleID: reviewRole.RoleID, Result: reviewResult, Build: reviewBuild})
	if err != nil {
		return fail(reviewRole.RoleID, err)
	}
	finalAnswer, err := CompletedRoleResponse(reviewRole.RoleID, reviewResult)
	if err != nil {
		return fail(reviewRole.RoleID, err)
	}
	result.Aggregate = aggregate(parentContext, result.Roles, finalAnswer, agentRuntime.RunStatusCompleted)
	return result, nil
}

func (e *Executor) runRole(
	ctx context.Context,
	parent agentRuntime.RunContext,
	model string,
	input string,
	history []agentRuntime.Message,
	tools []agentRuntime.ToolDefinition,
	role agentStrategy.RolePlan,
	selectedProfile profile.AgentProfile,
	initialToolChoice agentRuntime.ToolChoice,
) (agentRuntime.RunResult, agentMessage.BuildResult, error) {
	budget, err := BoundedRoleBudget(role, selectedProfile.Budget)
	if err != nil {
		return agentRuntime.RunResult{}, agentMessage.BuildResult{}, fmt.Errorf("role %s budget: %w", role.RoleID, err)
	}
	if len(role.AllowedTools) == 0 && len(tools) != 0 {
		return agentRuntime.RunResult{}, agentMessage.BuildResult{}, fmt.Errorf("role %s received tools outside its plan", role.RoleID)
	}
	build, err := buildMessages(e.messages, selectedProfile.Prompt.SystemPrompt, input, history, budget)
	if err != nil {
		return agentRuntime.RunResult{}, agentMessage.BuildResult{}, fmt.Errorf("build role %s messages: %w", role.RoleID, err)
	}
	runContext := agentRuntime.RunContext{
		RunID:                 parent.RunID + ":role:" + role.RoleID,
		ParentRunID:           parent.RunID,
		RoleID:                role.RoleID,
		StrategyPlanDigest:    parent.StrategyPlanDigest,
		UserID:                parent.UserID,
		Mode:                  parent.Mode,
		AgentProfileID:        selectedProfile.ID,
		AgentProfileVersion:   selectedProfile.Version,
		PromptTemplateID:      selectedProfile.Prompt.ID,
		PromptTemplateVersion: selectedProfile.Prompt.Version,
		StartedAt:             e.now(),
		Budget:                budget,
	}
	runResult, runErr := e.runner.Run(ctx, agentRuntime.RunRequest{
		Context:           runContext,
		Model:             model,
		Messages:          append([]agentRuntime.Message(nil), build.Messages...),
		Tools:             cloneTools(tools),
		InitialToolChoice: initialToolChoice,
	})
	if runResult.Context.RunID == "" {
		runResult.Context = runContext
	}
	for index := range runResult.Steps {
		if strings.TrimSpace(runResult.Steps[index].RoleID) == "" {
			runResult.Steps[index].RoleID = role.RoleID
		}
	}
	return runResult, build, runErr
}

func validateRequest(request Request) error {
	if err := ValidateSequentialPlan(request.Plan); err != nil {
		return err
	}
	if strings.TrimSpace(request.ParentContext.RunID) == "" || strings.TrimSpace(request.Model) == "" ||
		strings.TrimSpace(request.Input) == "" || strings.TrimSpace(request.RequiredTool) == "" {
		return errors.New("multi-role run ID, model, input and required tool are required")
	}
	if request.Handoff == nil {
		return errors.New("multi-role evidence handoff builder is required")
	}
	profiles := []profile.AgentProfile{
		request.Profiles.Parent, request.Profiles.Researcher, request.Profiles.Drafter, request.Profiles.Reviewer,
	}
	setVersion := strings.TrimSpace(request.Profiles.Parent.Version)
	for index, selected := range profiles {
		if strings.TrimSpace(selected.ID) == "" || strings.TrimSpace(selected.Version) == "" ||
			strings.TrimSpace(selected.Prompt.ID) == "" || strings.TrimSpace(selected.Prompt.Version) == "" ||
			strings.TrimSpace(selected.Prompt.SystemPrompt) == "" {
			return fmt.Errorf("multi-role profile %d is incomplete", index)
		}
		if selected.Version != setVersion {
			return fmt.Errorf(
				"multi-role profile %d version %q does not match parent profile set version %q",
				index,
				selected.Version,
				setVersion,
			)
		}
	}
	return nil
}

func ValidateSequentialPlan(plan agentStrategy.Plan) error {
	if err := agentStrategy.ValidatePlan(plan); err != nil {
		return fmt.Errorf("%w: %v", ErrPlanUnsupported, err)
	}
	if plan.SelectedStrategy != agentStrategy.KindMultiAgent ||
		plan.Decision != agentStrategy.DecisionSelected ||
		plan.ReasonCode != agentStrategy.ReasonMultiAdmitted ||
		plan.MaxParallelRoles != 1 ||
		len(plan.Roles) != 3 ||
		plan.Roles[0].RoleID != RoleResearcher ||
		plan.Roles[1].RoleID != RoleDrafter ||
		plan.Roles[2].RoleID != RoleReviewer {
		return fmt.Errorf("%w: only the sequential researcher/drafter/reviewer topology is supported", ErrPlanUnsupported)
	}
	return nil
}

func ParentBudget(plan agentStrategy.Plan) agentRuntime.Budget {
	budget := agentRuntime.Budget{}
	for _, role := range plan.Roles {
		budget.MaxSteps += role.MaxSteps
		budget.MaxTotalTokens += role.MaxTotalTokens
		budget.MaxEstimatedCostMicros += role.MaxEstimatedCostMicros
		budget.Timeout += time.Duration(role.TimeoutMillis) * time.Millisecond
	}
	return budget
}

func ValidateParentBudget(planBudget, profileBudget agentRuntime.Budget) error {
	if profileBudget.MaxSteps <= 0 || profileBudget.MaxTotalTokens <= 0 ||
		profileBudget.MaxEstimatedCostMicros <= 0 || profileBudget.Timeout <= 0 {
		return fmt.Errorf("%w: parent profile budget is incomplete", ErrPlanUnsupported)
	}
	if planBudget.MaxSteps > profileBudget.MaxSteps ||
		planBudget.MaxTotalTokens > profileBudget.MaxTotalTokens ||
		planBudget.MaxEstimatedCostMicros > profileBudget.MaxEstimatedCostMicros ||
		planBudget.Timeout > profileBudget.Timeout {
		return fmt.Errorf("%w: role budgets exceed the parent profile", ErrPlanUnsupported)
	}
	return nil
}

func BoundedRoleBudget(role agentStrategy.RolePlan, profileBudget agentRuntime.Budget) (agentRuntime.Budget, error) {
	roleTimeout := time.Duration(role.TimeoutMillis) * time.Millisecond
	if role.MaxSteps <= 0 || role.MaxTotalTokens <= 0 || role.MaxEstimatedCostMicros <= 0 || roleTimeout <= 0 ||
		profileBudget.MaxSteps <= 0 || profileBudget.MaxInputTokens <= 0 || profileBudget.MaxOutputTokens <= 0 ||
		profileBudget.MaxTotalTokens <= 0 || profileBudget.MaxEstimatedCostMicros <= 0 || profileBudget.Timeout <= 0 {
		return agentRuntime.Budget{}, errors.New("multi-role budget is incomplete")
	}
	if role.MaxSteps > profileBudget.MaxSteps || role.MaxTotalTokens > profileBudget.MaxTotalTokens ||
		role.MaxEstimatedCostMicros > profileBudget.MaxEstimatedCostMicros || roleTimeout > profileBudget.Timeout {
		return agentRuntime.Budget{}, errors.New("role plan exceeds its execution profile budget")
	}
	budget := agentRuntime.Budget{
		MaxSteps:               role.MaxSteps,
		MaxInputTokens:         profileBudget.MaxInputTokens,
		MaxOutputTokens:        profileBudget.MaxOutputTokens,
		MaxTotalTokens:         role.MaxTotalTokens,
		MaxEstimatedCostMicros: role.MaxEstimatedCostMicros,
		Timeout:                roleTimeout,
	}
	return budget, nil
}

func ValidateRoleBudgets(plan agentStrategy.Plan, profiles Profiles) error {
	selected := []profile.AgentProfile{profiles.Researcher, profiles.Drafter, profiles.Reviewer}
	if len(plan.Roles) != len(selected) {
		return fmt.Errorf("%w: role profile coverage is incomplete", ErrPlanUnsupported)
	}
	for index, role := range plan.Roles {
		if _, err := BoundedRoleBudget(role, selected[index].Budget); err != nil {
			return fmt.Errorf("%w: role %s budget: %v", ErrPlanUnsupported, role.RoleID, err)
		}
	}
	return nil
}

func ValidateRequiredReadTool(tools []agentRuntime.ToolDefinition, requiredName string) error {
	for _, tool := range tools {
		if tool.Name != requiredName {
			continue
		}
		if tool.Category != agentRuntime.ToolCategoryRead || tool.ApprovalRequired() {
			return fmt.Errorf("required runtime tool %s is not configured as a non-approval read tool", requiredName)
		}
		return nil
	}
	return fmt.Errorf("required runtime tool %s is unavailable", requiredName)
}

func HasSuccessfulToolEvidence(result agentRuntime.RunResult, toolName string) bool {
	for _, step := range result.Steps {
		for _, action := range step.Actions {
			if action.Type != agentRuntime.ActionToolCall || action.Name != toolName {
				continue
			}
			for _, observation := range step.Observations {
				if observation.ActionID == action.ID && !observation.IsError &&
					(strings.TrimSpace(observation.Content) != "" || len(observation.StructuredContent) > 0) {
					return true
				}
			}
		}
	}
	return false
}

func CompletedRoleResponse(roleID string, result agentRuntime.RunResult) (string, error) {
	if result.Status != agentRuntime.RunStatusCompleted || result.PendingAction != nil ||
		result.PendingToolContinuation != nil || strings.TrimSpace(result.ApprovalID) != "" {
		return "", fmt.Errorf("role %s ended with unsupported status %s", roleID, result.Status)
	}
	response := strings.TrimSpace(result.FinalAnswer)
	if response == "" {
		return "", fmt.Errorf("role %s returned no final answer", roleID)
	}
	return response, nil
}

func NewRoleExecutionError(roleID string, err error) error {
	if err == nil {
		err = errors.New("unknown role failure")
	}
	return &RoleExecutionError{RoleID: strings.TrimSpace(roleID), Err: err}
}

func buildMessages(
	builder agentMessage.Builder,
	systemPrompt string,
	input string,
	history []agentRuntime.Message,
	budget agentRuntime.Budget,
) (agentMessage.BuildResult, error) {
	maxInputTokens := budget.MaxInputTokens
	if maxInputTokens <= 0 {
		maxInputTokens = DefaultMaxInputTokens
	}
	return builder.Build(agentMessage.BuildRequest{
		System:  []agentRuntime.Message{{Role: agentRuntime.RoleSystem, Content: systemPrompt}},
		Current: agentRuntime.Message{Role: agentRuntime.RoleUser, Content: input},
		History: append([]agentRuntime.Message(nil), history...),
		Budget: agentMessage.Budget{
			MaxInputTokens:   maxInputTokens,
			HistoryTokens:    maxInputTokens * 60 / 100,
			MemoryTokens:     maxInputTokens * 15 / 100,
			RAGTokens:        maxInputTokens * 20 / 100,
			ToolResultTokens: maxInputTokens * 20 / 100,
			BlackboardTokens: maxInputTokens * 10 / 100,
		},
	})
}

func filterToolsByNames(tools []agentRuntime.ToolDefinition, allowedNames []string) []agentRuntime.ToolDefinition {
	allowed := make(map[string]struct{}, len(allowedNames))
	for _, name := range allowedNames {
		if name = strings.TrimSpace(name); name != "" {
			allowed[name] = struct{}{}
		}
	}
	filtered := make([]agentRuntime.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allowed[tool.Name]; ok {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func aggregate(
	parent agentRuntime.RunContext,
	roles []RoleResult,
	finalAnswer string,
	status agentRuntime.RunStatus,
) agentRuntime.RunResult {
	result := agentRuntime.RunResult{Context: parent, Status: status, FinalAnswer: strings.TrimSpace(finalAnswer)}
	for _, role := range roles {
		result.Usage.Add(role.Result.Usage)
		for _, step := range role.Result.Steps {
			if strings.TrimSpace(step.RoleID) == "" {
				step.RoleID = role.RoleID
			}
			result.Steps = append(result.Steps, step)
		}
	}
	return result
}

func cloneTools(tools []agentRuntime.ToolDefinition) []agentRuntime.ToolDefinition {
	cloned := make([]agentRuntime.ToolDefinition, len(tools))
	for index, tool := range tools {
		cloned[index] = tool
		cloned[index].InputSchema = append([]byte(nil), tool.InputSchema...)
	}
	return cloned
}
