package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	agentObservability "twitter-clone/internal/module/agent/observability"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/engine"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

const tracePersistenceTimeout = 2 * time.Second

type tracingModelClient struct {
	delegate agentRuntime.ModelClient
	recorder agentObservability.Recorder
	sampler  agentObservability.ContentSampler
	now      func() time.Time
}

func (c *tracingModelClient) Complete(ctx context.Context, request agentRuntime.ModelRequest) (agentRuntime.ModelResponse, error) {
	if c == nil || c.delegate == nil {
		return agentRuntime.ModelResponse{}, errors.New("tracing model client delegate is not configured")
	}
	now := c.now
	if now == nil {
		now = time.Now
	}
	startedAt := now()
	response, callErr := c.delegate.Complete(ctx, request)
	finishedAt := now()
	traceRunID := runtimeTraceRunID(request.Context)
	if c.recorder != nil && traceRunID != "" && request.Context.UserID > 0 {
		stepID := runtimeTraceStepID(request.Context.RoleID, request.StepIndex)
		promptContent := encodeRuntimeMessages(request.Messages)
		promptHash, promptLength := hashText(promptContent)
		completionHash, completionLength := hashText(response.Message.Content)
		promptSample := sampleTraceContent(c.sampler, "prompt:"+traceRunID+":"+stepID+":"+promptHash, promptContent)
		completionSample := sampleTraceContent(c.sampler, "completion:"+traceRunID+":"+stepID+":"+completionHash, response.Message.Content)
		status := "success"
		if callErr != nil {
			status = "failed"
		}
		record := agentObservability.LLMCallRecord{
			RecordID: traceRunID + ":llm:" + stepID, RunID: traceRunID,
			WorkflowID: request.Context.WorkflowID, UserID: request.Context.UserID,
			Source: agentObservability.SourceRuntime, StepID: stepID, Sequence: request.StepIndex,
			Model: firstWorkflowString(response.Model, request.Model), Provider: response.Provider,
			Status: status, ErrorClass: classifyTraceError(callErr),
			PromptHash: promptHash, PromptLength: promptLength,
			PromptTemplateID: request.Context.PromptTemplateID, PromptTemplateVersion: request.Context.PromptTemplateVersion,
			PromptSample: promptSample.Value, PromptSampleStatus: promptSample.Status,
			CompletionHash: completionHash, CompletionLength: completionLength,
			CompletionSample: completionSample.Value, CompletionSampleStatus: completionSample.Status,
			ContentSamplePolicy: promptSample.Policy,
			Usage:               traceTokenUsage(response.Usage), StartedAt: startedAt, FinishedAt: finishedAt,
			DurationMS: finishedAt.Sub(startedAt).Milliseconds(), UpdatedAt: finishedAt,
		}
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracePersistenceTimeout)
		defer cancel()
		if err := c.recorder.RecordLLMCall(persistCtx, record); err != nil {
			slog.WarnContext(ctx, "persist agent LLM trace failed", "run_id", request.Context.RunID, "error", err)
		}
	}
	return response, callErr
}

type traceToolAuditSink struct {
	recorder  agentObservability.Recorder
	delegates []workflowTool.AuditSink
}

func NewToolTraceAuditSink(recorder agentObservability.Recorder, delegates ...workflowTool.AuditSink) workflowTool.AuditSink {
	return &traceToolAuditSink{recorder: recorder, delegates: delegates}
}

func (s *traceToolAuditSink) Record(ctx context.Context, event workflowTool.AuditEvent) {
	for _, delegate := range s.delegates {
		if delegate != nil {
			delegate.Record(ctx, event)
		}
	}
	if s == nil || s.recorder == nil || event.RunID == "" || event.UserID == 0 {
		return
	}
	finishedAt := time.Now()
	startedAt := finishedAt.Add(-event.Duration)
	argumentsHash, argumentsLength := event.InputDigest, event.InputLength
	if argumentsHash == "" {
		argumentsHash, argumentsLength = hashJSON(event.Inputs)
	}
	status := event.Decision
	if status == "" {
		status = "unknown"
	}
	record := agentObservability.ToolCallRecord{
		RecordID: event.RunID + ":tool:" + string(event.Source) + ":" + event.StepID + ":" + event.ToolName,
		RunID:    event.RunID, UserID: event.UserID, Source: string(event.Source), StepID: event.StepID,
		ToolName: event.ToolName, Category: string(event.Category), Status: status,
		ErrorClass: string(event.ErrorCode), Attempts: event.Attempts,
		ArgumentsHash: argumentsHash, ArgumentsLength: argumentsLength,
		OutputHash: event.OutputDigest, OutputLength: event.OutputLength,
		StartedAt: startedAt, FinishedAt: finishedAt, DurationMS: event.Duration.Milliseconds(), UpdatedAt: finishedAt,
	}
	if event.OutputReference != nil {
		record.OutputStorage = event.OutputReference.Storage
		record.OutputReference = event.OutputReference.URI()
		record.OutputContentType = event.OutputReference.ContentType
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracePersistenceTimeout)
	defer cancel()
	if err := s.recorder.RecordToolCall(persistCtx, record); err != nil {
		slog.WarnContext(ctx, "persist agent tool trace failed", "run_id", event.RunID, "tool", event.ToolName, "error", err)
	}
}

func (s *AgentService) recordRuntimeResult(ctx context.Context, result agentRuntime.RunResult, runErr error, strategy string) {
	if s == nil || s.traceRecorder == nil || result.Context.RunID == "" || result.Context.UserID == 0 {
		return
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracePersistenceTimeout)
	defer cancel()
	finishedAt := time.Now()
	startedAt := result.Context.StartedAt
	if startedAt.IsZero() {
		startedAt = finishedAt
	}
	status := string(result.Status)
	if runErr != nil {
		status = string(agentRuntime.RunStatusFailed)
	}
	run := agentObservability.RunRecord{
		RecordID: result.Context.RunID, RunID: result.Context.RunID, WorkflowID: result.Context.WorkflowID,
		UserID: result.Context.UserID, Source: agentObservability.SourceRuntime,
		AgentProfileID: result.Context.AgentProfileID, AgentProfileVersion: result.Context.AgentProfileVersion,
		Mode: string(result.Context.Mode), Strategy: strategy, Status: status,
		ErrorClass: classifyTraceError(runErr), Usage: traceTokenUsage(result.Usage),
		Budget: agentObservability.BudgetSnapshot{
			MaxSteps: result.Context.Budget.MaxSteps, MaxTotalTokens: result.Context.Budget.MaxTotalTokens,
			MaxEstimatedCostMicros: result.Context.Budget.MaxEstimatedCostMicros,
			ConsumedSteps:          len(result.Steps), ConsumedTokens: result.Usage.TotalTokens,
			ConsumedCostMicros: result.Usage.EstimatedCostMicros,
		},
		StartedAt: startedAt, FinishedAt: finishedAt, DurationMS: finishedAt.Sub(startedAt).Milliseconds(), UpdatedAt: finishedAt,
	}
	s.recordRunTrace(persistCtx, run)
	failedStep := 0
	failedRoleID := ""
	var typedRunErr *agentRuntime.RunError
	if errors.As(runErr, &typedRunErr) {
		failedStep = typedRunErr.Step
	}
	var roleErr *multiAgentRoleExecutionError
	if errors.As(runErr, &roleErr) {
		failedRoleID = roleErr.RoleID
	}
	for _, step := range result.Steps {
		stepStatus := "success"
		stepErrorClass := ""
		if failedStep == step.Index && (failedRoleID == "" || failedRoleID == step.RoleID) {
			stepStatus = "failed"
			stepErrorClass = classifyTraceError(runErr)
		} else if result.Status == agentRuntime.RunStatusAwaitingHuman || result.Status == agentRuntime.RunStatusApprovalRequired {
			if step.Index == len(result.Steps) {
				stepStatus = string(result.Status)
			}
		}
		stepID := runtimeTraceStepID(step.RoleID, step.Index)
		stepType := "agent_step"
		stepName := strategy
		if strings.TrimSpace(step.RoleID) != "" {
			stepType = "agent_role_step"
			stepName = step.RoleID
		}
		record := agentObservability.StepRecord{
			RecordID: result.Context.RunID + ":step:" + stepID, RunID: result.Context.RunID,
			WorkflowID: result.Context.WorkflowID, UserID: result.Context.UserID,
			Source: agentObservability.SourceRuntime, StepID: stepID, Sequence: step.Index,
			StepType: stepType, Name: stepName, Status: stepStatus, ErrorClass: stepErrorClass,
			StartedAt: step.StartedAt, FinishedAt: step.FinishedAt,
			DurationMS: traceDurationMS(step.StartedAt, step.FinishedAt), UpdatedAt: finishedAt,
		}
		s.recordStepTrace(persistCtx, record)
	}
}

func (s *AgentService) recordWorkflowExecution(ctx context.Context, run *repository.WorkflowRunRecord, traces []engine.NodeTrace, budget agentRuntime.BudgetSnapshot) {
	if s == nil || s.traceRecorder == nil || run == nil || run.ID.IsZero() || run.UserID == 0 {
		return
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracePersistenceTimeout)
	defer cancel()
	now := time.Now()
	finishedAt := run.FinishedAt
	if finishedAt.IsZero() && run.Status != WorkflowRunStatusRunning {
		finishedAt = now
	}
	runRecord := agentObservability.RunRecord{
		RecordID: run.ID.Hex(), RunID: run.ID.Hex(), WorkflowID: run.WorkflowID.Hex(), UserID: run.UserID,
		Source: agentObservability.SourceWorkflow, Mode: string(agentRuntime.ModeWorkflow), Strategy: "dag",
		Status: run.Status, ErrorClass: workflowStatusErrorClass(run.Status),
		Usage: traceTokenUsage(budget.Usage),
		Budget: agentObservability.BudgetSnapshot{
			ConsumedSteps: budget.NodeExecutions, ConsumedTokens: budget.Usage.TotalTokens,
			ConsumedCostMicros: budget.Usage.EstimatedCostMicros,
		},
		StartedAt: run.StartedAt, FinishedAt: finishedAt, DurationMS: traceDurationMS(run.StartedAt, finishedAt), UpdatedAt: now,
	}
	s.recordRunTrace(persistCtx, runRecord)
	for index, trace := range traces {
		startedAt := unixMilliTime(trace.StartedAt)
		finishedAt := unixMilliTime(trace.FinishedAt)
		record := agentObservability.StepRecord{
			RecordID: run.ID.Hex() + ":step:" + trace.NodeID, RunID: run.ID.Hex(), WorkflowID: run.WorkflowID.Hex(),
			UserID: run.UserID, Source: agentObservability.SourceWorkflow,
			StepID: trace.NodeID, Sequence: index + 1, StepType: trace.NodeType, Name: trace.NodeID,
			Status: trace.Status, Attempt: trace.Attempt, MaxAttempts: trace.MaxAttempts,
			ErrorClass: workflowStatusErrorClass(trace.Status), StartedAt: startedAt, FinishedAt: finishedAt,
			DurationMS: trace.DurationMs, UpdatedAt: now,
		}
		s.recordStepTrace(persistCtx, record)
	}
}

func (s *AgentService) recordRunTrace(ctx context.Context, record agentObservability.RunRecord) {
	if err := s.traceRecorder.RecordRun(ctx, record); err != nil {
		slog.WarnContext(ctx, "persist agent run trace failed", "run_id", record.RunID, "error", err)
	}
}

func (s *AgentService) recordStepTrace(ctx context.Context, record agentObservability.StepRecord) {
	if err := s.traceRecorder.RecordStep(ctx, record); err != nil {
		slog.WarnContext(ctx, "persist agent step trace failed", "run_id", record.RunID, "step_id", record.StepID, "error", err)
	}
}

func runtimeStepID(index int) string {
	if index < 1 {
		return "step-unknown"
	}
	return fmt.Sprintf("step-%04d", index)
}

func runtimeTraceRunID(runContext agentRuntime.RunContext) string {
	if parentRunID := strings.TrimSpace(runContext.ParentRunID); parentRunID != "" {
		return parentRunID
	}
	return strings.TrimSpace(runContext.RunID)
}

func runtimeTraceStepID(roleID string, index int) string {
	return runtimeRoleScopedID(roleID, runtimeStepID(index))
}

func runtimeRoleScopedID(roleID, value string) string {
	roleID = strings.TrimSpace(roleID)
	value = strings.TrimSpace(value)
	if roleID == "" {
		return value
	}
	if value == "" {
		return roleID
	}
	return roleID + ":" + value
}

func classifyTraceError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var runErr *agentRuntime.RunError
	if errors.As(err, &runErr) {
		return string(runErr.Code)
	}
	return "internal_error"
}

func workflowStatusErrorClass(status string) string {
	switch status {
	case "failed":
		return "execution_failed"
	case "canceled":
		return "canceled"
	case "timed_out":
		return "timeout"
	case "suspended":
		return "suspended"
	case "compensation_failed":
		return "compensation_failed"
	default:
		return ""
	}
}

func traceTokenUsage(usage agentRuntime.TokenUsage) agentObservability.TokenUsage {
	return agentObservability.TokenUsage{
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, TotalTokens: usage.TotalTokens,
		Estimated: usage.Estimated, EstimatedCostMicros: usage.EstimatedCostMicros,
		CostEstimated: usage.CostEstimated, PricingVersion: usage.PricingVersion,
	}
}

func encodeRuntimeMessages(messages []agentRuntime.Message) string {
	encoded, err := json.Marshal(messages)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func sampleTraceContent(sampler agentObservability.ContentSampler, key, content string) agentObservability.ContentSample {
	if sampler == nil {
		return agentObservability.ContentSample{
			Status: agentObservability.ContentSampleStatusDisabled,
			Policy: agentObservability.ContentSamplePolicyDisabled,
		}
	}
	return sampler.Sample(key, content)
}

func hashJSON(value interface{}) (string, int) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", 0
	}
	return hashBytes(encoded), len(encoded)
}

func hashText(value string) (string, int) {
	if value == "" {
		return "", 0
	}
	return hashBytes([]byte(value)), len(value)
}

func hashBytes(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func unixMilliTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value)
}

func traceDurationMS(startedAt, finishedAt time.Time) int64 {
	if startedAt.IsZero() || finishedAt.IsZero() || finishedAt.Before(startedAt) {
		return 0
	}
	return finishedAt.Sub(startedAt).Milliseconds()
}

func workflowExecutionMetadata(run *repository.WorkflowRunRecord) workflowTool.ExecutionMetadata {
	if run == nil {
		return workflowTool.ExecutionMetadata{Source: workflowTool.SourceWorkflow}
	}
	revisionID := ""
	if !run.WorkflowRevisionID.IsZero() {
		revisionID = run.WorkflowRevisionID.Hex()
	}
	return workflowTool.ExecutionMetadata{
		RunID: run.ID.Hex(), WorkflowID: run.WorkflowID.Hex(),
		WorkflowRevisionID: revisionID, WorkflowRevisionNumber: run.WorkflowRevisionNumber,
		Source: workflowTool.SourceWorkflow,
	}
}
