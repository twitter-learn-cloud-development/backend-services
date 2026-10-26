package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func (s *AgentService) resumePlannedExecution(
	ctx context.Context,
	run *repository.AgentExecutionRun,
	checkpoint agentRuntime.VerifiedCheckpoint,
	request ResumeAgentExecutionRequest,
	tools []agentRuntime.ToolDefinition,
) (agentRuntime.VerifiedRunResult, error) {
	result := agentRuntime.VerifiedRunResult{}
	if run == nil {
		return result, ErrAgentExecutionRunNotResumable
	}
	var verifier agentRuntime.Verifier
	var collector agentRuntime.EvidenceCollector
	var allowedTools []string
	var environment agentRuntime.Environment
	blockedMessage := "planned completion was not verified"
	switch strings.TrimSpace(run.ExecutionProfile) {
	case ExecutionProfileRuntimePlatformSearch:
		verifier = agentEvidence.PlatformSearchGoalVerifier{}
		collector = agentEvidence.PlatformSearchGoalCollector{}
		allowedTools = []string{"hybrid_search_tweets"}
		blockedMessage = "planned platform search completion was not verified"
	case ExecutionProfileRuntimeWebSearch:
		verifier = agentEvidence.WebResearchGoalVerifier{}
		collector = agentEvidence.WebResearchGoalCollector{}
		allowedTools = []string{"web_search", "page_read"}
		blockedMessage = "planned web research completion was not verified"
	case ExecutionProfileRuntimeExternalMCP:
		if taskDeclaresCriterion(checkpoint.Task, agentEvidence.ExternalMCPGovernedObservedCriterion) {
			verifier = agentEvidence.ExternalMCPGovernedGoalVerifier{}
			collector = agentEvidence.ExternalMCPGovernedGoalCollector{}
			blockedMessage = "planned governed external MCP completion was not verified"
		} else if taskDeclaresCriterion(checkpoint.Task, agentEvidence.ExternalMCPReadObservedCriterion) {
			verifier = agentEvidence.ExternalMCPReadGoalVerifier{}
			collector = agentEvidence.ExternalMCPReadGoalCollector{}
			blockedMessage = "planned external MCP read completion was not verified"
		} else {
			return result, ErrAgentExecutionRunNotResumable
		}
		allowedTools = append([]string(nil), checkpoint.Task.AllowedTools...)
		if len(allowedTools) == 0 {
			return result, ErrAgentExecutionRunNotResumable
		}
		mcpEnvironment, environmentErr := s.newExternalMCPEnvironment(run.UserID)
		if environmentErr != nil {
			return result, environmentErr
		}
		environment = mcpEnvironment
	case ExecutionProfileRuntimeWorkflow:
		resolver := workflowToolRunEvidenceResolver{store: s.repo}
		verifier = agentEvidence.WorkflowToolGoalVerifier{Resolver: resolver}
		collector = agentEvidence.WorkflowToolGoalCollector{Resolver: resolver}
		allowedTools = append([]string(nil), checkpoint.Task.AllowedTools...)
		if len(allowedTools) == 0 {
			return result, ErrAgentExecutionRunNotResumable
		}
		workflowEnvironment, environmentErr := s.newWorkflowToolEnvironment(run.UserID)
		if environmentErr != nil {
			return result, environmentErr
		}
		environment = workflowEnvironment
		blockedMessage = "planned workflow completion was not verified"
	default:
		return result, ErrAgentExecutionRunNotResumable
	}
	// ask_human is a terminal short-plan step, so its verified checkpoint may
	// contain no plan-bound tools. Restore only the original task scope;
	// resolveGoalTools still intersects it with the freshly authorized catalog.
	checkpoint.Task.AllowedTools = allowedTools
	started := time.Now()
	verified := agentRuntime.NewVerifiedRunner(
		serviceCapturedRuntimeRunner{service: s},
		verifier,
		collector,
	)
	result, err := verified.Resume(ctx, agentRuntime.VerifiedResumeRequest{
		Checkpoint: checkpoint, HumanResponse: request.HumanResponse,
		ApprovalID: request.ApprovalID, ResumeToken: request.ResumeToken, Tools: tools,
		Environment: environment,
	})
	s.observePlanningExecuteResult(
		planningExecutePhaseResume, run.ExecutionProfile, 0, 0, 0, result, err, started,
	)
	if err != nil {
		return result, err
	}
	if result.Status == agentRuntime.GoalRunBlocked {
		return result, &agentRuntime.RunError{
			Code:    agentRuntime.ErrorInvalidAction,
			Message: blockedMessage,
		}
	}
	return result, nil
}

func taskDeclaresCriterion(task agentRuntime.TaskSpec, criterionID string) bool {
	criterionID = strings.TrimSpace(criterionID)
	for _, criterion := range task.CompletionCriteria {
		if strings.TrimSpace(criterion.ID) == criterionID {
			return true
		}
	}
	return false
}

func planningExecutionCaptureFromRun(
	run *repository.AgentExecutionRun,
	verified agentRuntime.VerifiedRunResult,
) planningExecutionCapture {
	if run == nil {
		return planningExecutionCapture{}
	}
	planning := planningExecutionCapture{
		attempted: true,
		context: agentRuntime.RunContext{
			RunID: run.ID, UserID: run.UserID, Mode: agentRuntime.Mode(run.Mode),
			AgentProfileID: run.AgentProfileID, AgentProfileVersion: run.AgentProfileVersion,
			PromptTemplateID: run.PromptTemplateID, PromptTemplateVersion: run.PromptTemplateVersion,
			Budget: agentRuntime.Budget{
				MaxSteps: run.MaxSteps, MaxTotalTokens: run.MaxTotalTokens,
				MaxEstimatedCostMicros: run.MaxEstimatedCostMicros,
			},
		},
		planDigest: run.PlanningPlanDigest,
		usage: agentRuntime.TokenUsage{
			InputTokens: run.PlanningInputTokens, OutputTokens: run.PlanningOutputTokens,
			TotalTokens: run.PlanningTotalTokens, Estimated: run.PlanningUsageEstimated,
			EstimatedCostMicros: run.PlanningEstimatedCostMicros,
			CostEstimated:       run.PlanningCostEstimated, PricingVersion: run.PlanningPricingVersion,
		},
		planningCalls: run.PlanningCalls, modelAttempts: run.PlanningModelAttempts,
		admissionAttempts: run.PlanningAdmissionAttempts, admissionRepairs: run.PlanningAdmissionRepairs,
		recoveryAttempts: run.PlanningRecoveryAttempts, model: run.PlanningModel, provider: run.PlanningProvider,
		goalStatus: string(verified.Status), verificationStatus: string(verified.Verification.Status),
		verifiedCheckpointVersion:  run.VerifiedCheckpointVersion,
		verifiedCheckpointRevision: run.VerifiedCheckpointRevision,
		verifiedCheckpointDigest:   run.VerifiedCheckpointDigest,
	}
	if checkpoint := verified.Checkpoint; checkpoint != nil {
		planning.verifiedCheckpoint = checkpoint
		planning.verifiedCheckpointVersion = checkpoint.Version
		planning.verifiedCheckpointRevision = checkpoint.Revision
		planning.verifiedCheckpointDigest = verifiedCheckpointPublicDigest(run.ID, *checkpoint)
	}
	return planning
}

func verifiedCheckpointPublicDigest(runID string, checkpoint agentRuntime.VerifiedCheckpoint) string {
	payload, err := jsonMarshalVerifiedCheckpoint(checkpoint)
	if err != nil {
		return ""
	}
	return agentExecutionScopedDigest(runID, payload)
}

func jsonMarshalVerifiedCheckpoint(checkpoint agentRuntime.VerifiedCheckpoint) (string, error) {
	payload, err := json.Marshal(checkpoint)
	return string(payload), err
}

func isVerifiedAgentCheckpoint(run *repository.AgentExecutionRun) bool {
	return run != nil && run.CheckpointVersion == agentRuntime.VerifiedCheckpointVersion
}
