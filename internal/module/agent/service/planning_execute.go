package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	DefaultPlanningMaxOutputTokens = 768
	planningCanaryBuckets          = 100
)

// PlanningExecuteConfig is an immutable production rollout snapshot. Every
// route is independently gated; governed MCP writes additionally require the
// existing approval-recovery path.
type PlanningExecuteConfig struct {
	Enabled                 bool
	CanaryPercent           int
	PlatformSearchEnabled   bool
	WebResearchEnabled      bool
	ExternalMCPReadEnabled  bool
	ExternalMCPWriteEnabled bool
	WorkflowReadEnabled     bool
	MaxOutputTokens         int
	ParseRepairs            int
	AdmissionRepairs        int
}

func (config PlanningExecuteConfig) Validate() error {
	if config.CanaryPercent < 0 || config.CanaryPercent > planningCanaryBuckets {
		return fmt.Errorf("planning canary percent must be between 0 and %d", planningCanaryBuckets)
	}
	if !config.Enabled {
		return nil
	}
	if config.MaxOutputTokens <= 0 {
		return errors.New("planning maximum output tokens must be positive")
	}
	if config.ParseRepairs < 0 || config.ParseRepairs > 1 {
		return errors.New("planning parse repairs must be between 0 and 1")
	}
	if config.AdmissionRepairs < 0 || config.AdmissionRepairs > 1 {
		return errors.New("planning admission repairs must be between 0 and 1")
	}
	return nil
}

// WithPlanningExecute configures the production planning canary. Tests may
// inject a deterministic coordinator; production builds one from the same
// governed model client and accounting policy as the ReAct runner.
func WithPlanningExecute(
	config PlanningExecuteConfig,
	coordinator *agentRuntime.PlanningCoordinator,
) Option {
	return func(service *AgentService) {
		service.planningExecute = config
		service.planningCoordinator = coordinator
	}
}

func (s *AgentService) configurePlanningCoordinator(model agentRuntime.ModelClient) error {
	if s == nil {
		return errors.New("agent service is required")
	}
	if err := s.planningExecute.Validate(); err != nil {
		return err
	}
	if !s.planningExecute.Enabled || s.planningCoordinator != nil {
		return nil
	}
	if model == nil {
		return errors.New("planning model client is required")
	}
	planner, err := agentRuntime.NewModelShortHorizonPlanner(
		model,
		agentRuntime.WithShortPlanTokenCounter(s.runtimeTokens),
		agentRuntime.WithShortPlanCostEstimator(s.runtimeCostEstimator),
		agentRuntime.WithShortPlanMaxOutputTokens(s.planningExecute.MaxOutputTokens),
		agentRuntime.WithShortPlanParseRepairs(s.planningExecute.ParseRepairs),
	)
	if err != nil {
		return fmt.Errorf("create short-horizon planner: %w", err)
	}
	coordinator, err := agentRuntime.NewPlanningCoordinator(
		planner,
		agentRuntime.DeterministicShortPlanPolicy{},
		agentRuntime.WithShortPlanAdmissionRepairs(s.planningExecute.AdmissionRepairs),
	)
	if err != nil {
		return fmt.Errorf("create planning coordinator: %w", err)
	}
	s.planningCoordinator = coordinator
	return nil
}

func (s *AgentService) planningExecuteEnabled(userID uint64, executionProfile string) bool {
	if s == nil || !s.planningExecute.Enabled || s.planningCoordinator == nil ||
		!s.planningExecuteProfileEnabled(executionProfile) || userID == 0 ||
		s.planningExecute.CanaryPercent <= 0 {
		return false
	}
	if s.planningExecute.CanaryPercent >= planningCanaryBuckets {
		return true
	}
	return planningCanaryBucket(userID, executionProfile) < s.planningExecute.CanaryPercent
}

func (s *AgentService) planningExecuteExternalMCPWriteEnabled(userID uint64) bool {
	if s == nil || !s.planningExecute.Enabled || s.planningCoordinator == nil ||
		!s.planningExecute.ExternalMCPWriteEnabled || !s.unifiedAgentApprovalRecovery ||
		userID == 0 || s.planningExecute.CanaryPercent <= 0 {
		return false
	}
	if s.planningExecute.CanaryPercent >= planningCanaryBuckets {
		return true
	}
	return planningCanaryBucket(userID, ExecutionProfileRuntimeExternalMCP) <
		s.planningExecute.CanaryPercent
}

func (s *AgentService) planningExecuteProfileEnabled(executionProfile string) bool {
	if s == nil {
		return false
	}
	switch strings.TrimSpace(executionProfile) {
	case ExecutionProfileRuntimePlatformSearch:
		return s.planningExecute.PlatformSearchEnabled
	case ExecutionProfileRuntimeWebSearch:
		return s.planningExecute.WebResearchEnabled
	case ExecutionProfileRuntimeExternalMCP:
		return s.planningExecute.ExternalMCPReadEnabled
	case ExecutionProfileRuntimeWorkflow:
		return s.planningExecute.WorkflowReadEnabled
	default:
		return false
	}
}

func planningCanaryBucket(userID uint64, executionProfile string) int {
	payload := fmt.Sprintf("agent-planning-v1:%d:%s", userID, strings.TrimSpace(executionProfile))
	digest := sha256.Sum256([]byte(payload))
	return int(binary.BigEndian.Uint32(digest[:4]) % planningCanaryBuckets)
}

type serviceCapturedRuntimeRunner struct {
	service *AgentService
}

func (runner serviceCapturedRuntimeRunner) Run(
	ctx context.Context,
	request agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	if runner.service == nil {
		return agentRuntime.RunResult{}, errors.New("agent service is required")
	}
	return runner.service.runRuntime(ctx, request)
}

func (runner serviceCapturedRuntimeRunner) Resume(
	ctx context.Context,
	request agentRuntime.ResumeRequest,
) (agentRuntime.RunResult, error) {
	if runner.service == nil {
		return agentRuntime.RunResult{}, errors.New("agent service is required")
	}
	resumable, ok := runner.service.runtimeRunner.(agentRuntime.ResumableAgentRunner)
	if !ok {
		return agentRuntime.RunResult{}, &agentRuntime.RunError{
			Code: agentRuntime.ErrorUnsupported, Message: "agent runtime does not support resume",
		}
	}
	return resumable.Resume(ctx, request)
}

func (s *AgentService) runPlannedPlatformSearch(
	ctx context.Context,
	goal string,
	request agentRuntime.RunRequest,
) (result agentRuntime.PlannedVerifiedRunResult, returnErr error) {
	started := time.Now()
	defer func() {
		planningCalls := result.Planning.PlanningCalls
		admissionRepairs := result.Planning.AdmissionRepairs
		for _, recovery := range result.RecoveryPlans {
			planningCalls += recovery.PlanningCalls
			admissionRepairs += recovery.AdmissionRepairs
		}
		s.observePlanningExecuteResult(
			planningExecutePhaseInitial, ExecutionProfileRuntimePlatformSearch, planningCalls,
			admissionRepairs, result.RecoveryAttempts, result.Verified, returnErr, started,
		)
	}()
	if s == nil || s.planningCoordinator == nil {
		return result, errors.New("planning coordinator is not configured")
	}
	goal = strings.TrimSpace(goal)
	task := agentRuntime.TaskSpec{
		ID:           "planned-platform-search:" + request.Context.RunID,
		Goal:         goal,
		AllowedTools: []string{"hybrid_search_tweets"},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          agentEvidence.PlatformSearchResultCriterion,
			Description: "A trusted first-party platform search result was observed.",
			Required:    true,
		}},
		MaxRepairAttempts: 1,
	}
	verified := agentRuntime.NewVerifiedRunner(
		serviceCapturedRuntimeRunner{service: s},
		agentEvidence.PlatformSearchGoalVerifier{},
		agentEvidence.PlatformSearchGoalCollector{},
	)
	runner := agentRuntime.NewPlannedVerifiedRunner(s.planningCoordinator, verified)
	result, err := runner.Run(ctx, agentRuntime.PlannedVerifiedRunRequest{
		Task: task,
		Run:  request,
	})
	capturePlannedVerifiedExecution(ctx, request.Context, result)
	if err != nil {
		return result, err
	}
	if result.Verified.Status == agentRuntime.GoalRunBlocked {
		return result, &agentRuntime.RunError{
			Code:    agentRuntime.ErrorInvalidAction,
			Message: "planned platform search completion was not verified",
		}
	}
	return result, nil
}

func (s *AgentService) runPlannedWebResearch(
	ctx context.Context,
	goal string,
	request agentRuntime.RunRequest,
) (result agentRuntime.PlannedVerifiedRunResult, returnErr error) {
	started := time.Now()
	defer func() {
		planningCalls := result.Planning.PlanningCalls
		admissionRepairs := result.Planning.AdmissionRepairs
		for _, recovery := range result.RecoveryPlans {
			planningCalls += recovery.PlanningCalls
			admissionRepairs += recovery.AdmissionRepairs
		}
		s.observePlanningExecuteResult(
			planningExecutePhaseInitial, ExecutionProfileRuntimeWebSearch, planningCalls,
			admissionRepairs, result.RecoveryAttempts, result.Verified, returnErr, started,
		)
	}()
	if s == nil || s.planningCoordinator == nil {
		return result, errors.New("planning coordinator is not configured")
	}
	task := agentRuntime.TaskSpec{
		ID:           "planned-web-research:" + request.Context.RunID,
		Goal:         strings.TrimSpace(goal),
		AllowedTools: []string{"web_search", "page_read"},
		CompletionCriteria: []agentRuntime.CompletionCriterion{
			{
				ID:          agentEvidence.WebSearchSourcesCriterion,
				Description: "A configured web provider returned a public source.",
				Required:    true,
			},
			{
				ID:          agentEvidence.WebPageContentCriterion,
				Description: "A page discovered by search was read as structured evidence.",
				Required:    true,
			},
		},
		MaxRepairAttempts: 1,
	}
	verified := agentRuntime.NewVerifiedRunner(
		serviceCapturedRuntimeRunner{service: s},
		agentEvidence.WebResearchGoalVerifier{},
		agentEvidence.WebResearchGoalCollector{},
	)
	runner := agentRuntime.NewPlannedVerifiedRunner(s.planningCoordinator, verified)
	result, err := runner.Run(ctx, agentRuntime.PlannedVerifiedRunRequest{Task: task, Run: request})
	capturePlannedVerifiedExecution(ctx, request.Context, result)
	if err != nil {
		return result, err
	}
	if result.Verified.Status == agentRuntime.GoalRunBlocked {
		return result, &agentRuntime.RunError{
			Code:    agentRuntime.ErrorInvalidAction,
			Message: "planned web research completion was not verified",
		}
	}
	return result, nil
}

func (s *AgentService) runPlannedExternalMCPRead(
	ctx context.Context,
	goal string,
	request agentRuntime.RunRequest,
	environment agentRuntime.Environment,
	allowedTools []string,
) (result agentRuntime.PlannedVerifiedRunResult, returnErr error) {
	started := time.Now()
	defer func() {
		planningCalls := result.Planning.PlanningCalls
		admissionRepairs := result.Planning.AdmissionRepairs
		for _, recovery := range result.RecoveryPlans {
			planningCalls += recovery.PlanningCalls
			admissionRepairs += recovery.AdmissionRepairs
		}
		s.observePlanningExecuteResult(
			planningExecutePhaseInitial, ExecutionProfileRuntimeExternalMCP, planningCalls,
			admissionRepairs, result.RecoveryAttempts, result.Verified, returnErr, started,
		)
	}()
	if s == nil || s.planningCoordinator == nil {
		return result, errors.New("planning coordinator is not configured")
	}
	if environment == nil {
		return result, errors.New("external MCP environment is not configured")
	}
	task := agentRuntime.TaskSpec{
		ID:           "planned-external-mcp-read:" + request.Context.RunID,
		Goal:         strings.TrimSpace(goal),
		AllowedTools: append([]string(nil), allowedTools...),
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          agentEvidence.ExternalMCPReadObservedCriterion,
			Description: "A currently authorized external MCP read observation was verified.",
			Required:    true,
		}},
		MaxRepairAttempts: 1,
	}
	verified := agentRuntime.NewVerifiedRunner(
		serviceCapturedRuntimeRunner{service: s},
		agentEvidence.ExternalMCPReadGoalVerifier{},
		agentEvidence.ExternalMCPReadGoalCollector{},
	)
	runner := agentRuntime.NewPlannedVerifiedRunner(s.planningCoordinator, verified)
	result, err := runner.Run(ctx, agentRuntime.PlannedVerifiedRunRequest{
		Task: task, Run: request, Environment: environment,
	})
	capturePlannedVerifiedExecution(ctx, request.Context, result)
	if err != nil {
		return result, err
	}
	if result.Verified.Status == agentRuntime.GoalRunBlocked {
		return result, &agentRuntime.RunError{
			Code:    agentRuntime.ErrorInvalidAction,
			Message: "planned external MCP read completion was not verified",
		}
	}
	return result, nil
}

func (s *AgentService) runPlannedExternalMCPGoverned(
	ctx context.Context,
	goal string,
	request agentRuntime.RunRequest,
	environment agentRuntime.Environment,
	allowedTools []string,
) (result agentRuntime.PlannedVerifiedRunResult, returnErr error) {
	started := time.Now()
	defer func() {
		planningCalls := result.Planning.PlanningCalls
		admissionRepairs := result.Planning.AdmissionRepairs
		for _, recovery := range result.RecoveryPlans {
			planningCalls += recovery.PlanningCalls
			admissionRepairs += recovery.AdmissionRepairs
		}
		s.observePlanningExecuteResult(
			planningExecutePhaseInitial, ExecutionProfileRuntimeExternalMCP, planningCalls,
			admissionRepairs, result.RecoveryAttempts, result.Verified, returnErr, started,
		)
	}()
	if s == nil || s.planningCoordinator == nil {
		return result, errors.New("planning coordinator is not configured")
	}
	if environment == nil {
		return result, errors.New("external MCP environment is not configured")
	}
	if len(allowedTools) == 0 {
		return result, errors.New("planned governed external MCP tool scope is empty")
	}
	task := agentRuntime.TaskSpec{
		ID:           "planned-external-mcp-governed:" + request.Context.RunID,
		Goal:         strings.TrimSpace(goal),
		AllowedTools: append([]string(nil), allowedTools...),
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          agentEvidence.ExternalMCPGovernedObservedCriterion,
			Description: "A currently authorized external MCP observation was verified through the governed runtime.",
			Required:    true,
		}},
		MaxRepairAttempts: 1,
	}
	verified := agentRuntime.NewVerifiedRunner(
		serviceCapturedRuntimeRunner{service: s},
		agentEvidence.ExternalMCPGovernedGoalVerifier{},
		agentEvidence.ExternalMCPGovernedGoalCollector{},
	)
	runner := agentRuntime.NewPlannedVerifiedRunner(s.planningCoordinator, verified)
	result, err := runner.Run(ctx, agentRuntime.PlannedVerifiedRunRequest{
		Task: task, Run: request, Environment: environment,
	})
	capturePlannedVerifiedExecution(ctx, request.Context, result)
	if err != nil {
		return result, err
	}
	if result.Verified.Status == agentRuntime.GoalRunBlocked {
		return result, &agentRuntime.RunError{
			Code:    agentRuntime.ErrorInvalidAction,
			Message: "planned governed external MCP completion was not verified",
		}
	}
	return result, nil
}

func (s *AgentService) runPlannedWorkflowRead(
	ctx context.Context,
	goal string,
	request agentRuntime.RunRequest,
	environment agentRuntime.Environment,
	allowedTools []string,
) (result agentRuntime.PlannedVerifiedRunResult, returnErr error) {
	started := time.Now()
	defer func() {
		planningCalls := result.Planning.PlanningCalls
		admissionRepairs := result.Planning.AdmissionRepairs
		for _, recovery := range result.RecoveryPlans {
			planningCalls += recovery.PlanningCalls
			admissionRepairs += recovery.AdmissionRepairs
		}
		s.observePlanningExecuteResult(
			planningExecutePhaseInitial, ExecutionProfileRuntimeWorkflow, planningCalls,
			admissionRepairs, result.RecoveryAttempts, result.Verified, returnErr, started,
		)
	}()
	if s == nil || s.planningCoordinator == nil {
		return result, errors.New("planning coordinator is not configured")
	}
	if environment == nil {
		return result, errors.New("workflow tool environment is not configured")
	}
	if len(allowedTools) == 0 {
		return result, errors.New("planned workflow tool scope is empty")
	}
	resolver := workflowToolRunEvidenceResolver{store: s.repo}
	task := agentRuntime.TaskSpec{
		ID:           "planned-workflow-read:" + request.Context.RunID,
		Goal:         strings.TrimSpace(goal),
		AllowedTools: append([]string(nil), allowedTools...),
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          agentEvidence.WorkflowToolOutputVerifiedCriterion,
			Description: "An immutable published workflow child run and authoritative output were verified.",
			Required:    true,
		}},
		MaxRepairAttempts: 1,
	}
	verified := agentRuntime.NewVerifiedRunner(
		serviceCapturedRuntimeRunner{service: s},
		agentEvidence.WorkflowToolGoalVerifier{Resolver: resolver},
		agentEvidence.WorkflowToolGoalCollector{Resolver: resolver},
	)
	runner := agentRuntime.NewPlannedVerifiedRunner(s.planningCoordinator, verified)
	result, err := runner.Run(ctx, agentRuntime.PlannedVerifiedRunRequest{
		Task: task, Run: request, Environment: environment,
	})
	capturePlannedVerifiedExecution(ctx, request.Context, result)
	if err != nil {
		return result, err
	}
	if result.Verified.Status == agentRuntime.GoalRunBlocked {
		return result, &agentRuntime.RunError{
			Code:    agentRuntime.ErrorInvalidAction,
			Message: "planned workflow completion was not verified",
		}
	}
	return result, nil
}

func addPlanningExecutionMetadata(
	metadata map[string]interface{},
	result agentRuntime.PlannedVerifiedRunResult,
) {
	if metadata == nil {
		return
	}
	usage := result.Planning.Usage
	planDigest := result.Planning.Plan.Digest
	planningCalls := result.Planning.PlanningCalls
	modelAttempts := result.Planning.ModelAttempts
	admissionAttempts := result.Planning.AdmissionAttempts
	admissionRepairs := result.Planning.AdmissionRepairs
	for _, recovery := range result.RecoveryPlans {
		usage.Add(recovery.Usage)
		planningCalls += recovery.PlanningCalls
		modelAttempts += recovery.ModelAttempts
		admissionAttempts += recovery.AdmissionAttempts
		admissionRepairs += recovery.AdmissionRepairs
		if recovery.Plan.Digest != "" {
			planDigest = recovery.Plan.Digest
		}
	}
	metadata["planning_execute"] = true
	metadata["planning_plan_digest"] = planDigest
	metadata["planning_calls"] = planningCalls
	metadata["planning_model_attempts"] = modelAttempts
	metadata["planning_admission_attempts"] = admissionAttempts
	metadata["planning_admission_repairs"] = admissionRepairs
	metadata["planning_recovery_attempts"] = result.RecoveryAttempts
	metadata["planning_total_tokens"] = usage.TotalTokens
	metadata["planning_estimated_cost_micros"] = usage.EstimatedCostMicros
	metadata["goal_run_status"] = string(result.Verified.Status)
	metadata["goal_verification_status"] = string(result.Verified.Verification.Status)
}
