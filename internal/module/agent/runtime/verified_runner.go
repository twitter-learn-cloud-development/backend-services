package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

type GoalRunStatus string

const (
	GoalRunVerified                GoalRunStatus = "verified"
	GoalRunBlocked                 GoalRunStatus = "blocked"
	GoalRunSuspended               GoalRunStatus = "suspended"
	VerifiedCheckpointVersion                    = "goal.v1"
	CheckpointResumeEvidenceSource               = "agent_runtime.checkpoint_resume"
)

type VerifiedRunRequest struct {
	Task          TaskSpec
	Run           RunRequest
	Environment   Environment
	Evidence      EvidenceLedger
	RepairBuilder VerifiedRepairBuilder
}

type VerifiedRunResult struct {
	Status         GoalRunStatus
	Run            RunResult
	Verification   VerificationResult
	Evidence       EvidenceLedger
	Before         *EnvironmentSnapshot
	After          *EnvironmentSnapshot
	RepairAttempts int
	Checkpoint     *VerifiedCheckpoint
}

// VerifiedCheckpoint binds the resumable ReAct state to the task contract and
// accumulated verification state. Callers must persist the complete value in
// the same authenticated, encrypted store used for RunCheckpoint.
type VerifiedCheckpoint struct {
	Version        string               `json:"version"`
	Revision       int64                `json:"revision"`
	Task           TaskSpec             `json:"task"`
	Run            RunCheckpoint        `json:"run"`
	Environment    string               `json:"environment,omitempty"`
	Evidence       EvidenceLedger       `json:"evidence"`
	Before         *EnvironmentSnapshot `json:"before,omitempty"`
	RepairAttempts int                  `json:"repair_attempts"`
}

// VerifiedResumeRequest intentionally excludes old tool definitions. The
// current catalog must be supplied and authorized again on every resume.
type VerifiedResumeRequest struct {
	Checkpoint    VerifiedCheckpoint
	HumanResponse string
	ApprovalID    string
	ResumeToken   string
	Tools         []ToolDefinition
	Environment   Environment
}

type EvidenceCollectionRequest struct {
	Task    TaskSpec
	Run     RunResult
	Before  *EnvironmentSnapshot
	After   *EnvironmentSnapshot
	Attempt int
}

type EvidenceCollector interface {
	Collect(ctx context.Context, request EvidenceCollectionRequest) ([]Evidence, error)
}

// StructuredObservationEvidenceCollector turns successful structured tool
// observations into digest-only evidence. Bindings are configured by a domain
// adapter; the collector never infers that an observation proves a criterion.
type StructuredObservationEvidenceCollector struct {
	Bindings map[string][]string
}

func (collector StructuredObservationEvidenceCollector) Collect(
	_ context.Context,
	request EvidenceCollectionRequest,
) ([]Evidence, error) {
	evidence := make([]Evidence, 0)
	for _, step := range request.Run.Steps {
		for _, observation := range step.Observations {
			if observation.IsError || len(observation.StructuredContent) == 0 {
				continue
			}
			criterionIDs := cloneStrings(collector.Bindings[observation.Name])
			if len(criterionIDs) == 0 {
				continue
			}
			digest := sha256.Sum256(observation.StructuredContent)
			actionID := strings.TrimSpace(observation.ActionID)
			if actionID == "" {
				actionID = "unknown"
			}
			evidence = append(evidence, Evidence{
				ID: fmt.Sprintf(
					"tool:%s:%d:%d:%s",
					request.Run.Context.RunID,
					request.Attempt,
					step.Index,
					actionID,
				),
				Kind:         EvidenceToolObservation,
				Source:       observation.Name,
				CriterionIDs: criterionIDs,
				Digest:       "sha256:" + hex.EncodeToString(digest[:]),
				Reference: fmt.Sprintf(
					"agent-run://%s/attempt/%d/step/%d/action/%s",
					request.Run.Context.RunID,
					request.Attempt,
					step.Index,
					actionID,
				),
				StepIndex:  step.Index,
				CapturedAt: step.FinishedAt,
			})
		}
	}
	return evidence, nil
}

// VerifiedRunner adds completion verification around an existing AgentRunner.
// It is opt-in and does not replace or duplicate the underlying ReAct loop.
type VerifiedRunner struct {
	runner    AgentRunner
	verifier  Verifier
	collector EvidenceCollector
}

func NewVerifiedRunner(runner AgentRunner, verifier Verifier, collector EvidenceCollector) *VerifiedRunner {
	return &VerifiedRunner{runner: runner, verifier: verifier, collector: collector}
}

func (runner *VerifiedRunner) Run(ctx context.Context, request VerifiedRunRequest) (VerifiedRunResult, error) {
	result := VerifiedRunResult{Evidence: cloneEvidenceLedger(request.Evidence)}
	if runner == nil || runner.runner == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "agent runner is required"}
	}
	if runner.verifier == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "task verifier is required"}
	}
	if err := request.Task.Validate(); err != nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "invalid task specification", Cause: err}
	}

	goalCtx, cancel := WithBudgetContext(ctx, request.Run.Context.Budget)
	defer cancel()

	tools, err := resolveGoalTools(goalCtx, request.Task, request.Run.Tools, request.Environment)
	if err != nil {
		return result, err
	}
	request.Run.Tools = tools

	if request.Environment != nil {
		before, snapshotErr := request.Environment.Snapshot(goalCtx, SnapshotRequest{
			Task: request.Task, Phase: SnapshotPhaseBefore,
		})
		if snapshotErr != nil {
			return result, fmt.Errorf("capture environment before snapshot: %w", snapshotErr)
		}
		result.Before = cloneEnvironmentSnapshot(&before)
	}

	currentRequest := request.Run
	var cumulative RunResult
	for attempt := 0; ; attempt++ {
		attemptResult, runErr := runner.runner.Run(goalCtx, currentRequest)
		cumulative = mergeGoalRunResults(cumulative, attemptResult, request.Run.Context)
		result.Run = cumulative
		result.RepairAttempts = attempt
		if runErr != nil && !isGoalSuspendedStatus(attemptResult.Status) {
			if HasErrorCode(runErr, ErrorModel) {
				blocked, blockErr := projectProviderRoutingBlock(request.Task, &result)
				if blockErr != nil {
					return result, blockErr
				}
				if blocked {
					return result, nil
				}
			}
			if request.RepairBuilder == nil || !HasErrorCode(runErr, ErrorTool) ||
				attempt >= request.Task.MaxRepairAttempts {
				return result, runErr
			}
			currentRequest, err = request.RepairBuilder.BuildRepair(goalCtx, VerifiedRepairRequest{
				Task: request.Task, Base: request.Run, Previous: cumulative,
				Evidence: cloneEvidenceLedger(result.Evidence), Attempt: attempt + 1,
				Signal: VerifiedRepairSignal{
					Reason: VerifiedRepairExecutionFailed, ErrorCode: ErrorTool,
				},
			})
			if err != nil {
				return result, err
			}
			if err = validateVerifiedRepairRequest(request.Run, cumulative, currentRequest); err != nil {
				return result, err
			}
			continue
		}

		if attemptResult.Status != RunStatusCompleted {
			var suspendedAfter *EnvironmentSnapshot
			if request.Environment != nil {
				snapshot, snapshotErr := request.Environment.Snapshot(goalCtx, SnapshotRequest{
					Task: request.Task, Phase: SnapshotPhaseAfter,
				})
				if snapshotErr != nil {
					return result, fmt.Errorf("capture suspended environment snapshot: %w", snapshotErr)
				}
				suspendedAfter = cloneEnvironmentSnapshot(&snapshot)
				result.After = suspendedAfter
			}

			if runner.collector != nil {
				items, collectErr := runner.collector.Collect(goalCtx, EvidenceCollectionRequest{
					Task: request.Task, Run: attemptResult, Before: result.Before,
					After: suspendedAfter, Attempt: attempt,
				})
				if collectErr != nil {
					return result, fmt.Errorf("collect suspended task evidence: %w", collectErr)
				}
				for _, item := range items {
					result.Evidence, err = result.Evidence.With(item)
					if err != nil {
						return result, fmt.Errorf("append suspended task evidence: %w", err)
					}
				}
			}

			if suspensionVerifier, supported := runner.verifier.(SuspendedRunVerifier); supported {
				verification, verifyErr := suspensionVerifier.VerifySuspension(goalCtx, VerificationRequest{
					Task: request.Task, Run: cumulative, Before: result.Before, After: suspendedAfter,
					Evidence: result.Evidence, RepairAttempts: attempt,
				})
				if verifyErr != nil {
					return result, fmt.Errorf("verify task suspension: %w", verifyErr)
				}
				result.Verification = cloneVerificationResult(verification)
				if !verification.Passed() {
					result.Status = GoalRunBlocked
					return result, nil
				}
			} else {
				result.Verification = VerificationResult{Status: VerificationInconclusive}
			}
			result.Status = GoalRunSuspended
			checkpoint, checkpointErr := buildVerifiedCheckpoint(
				request.Task, request.Run, request.Environment, result, 0,
			)
			if checkpointErr != nil {
				return result, checkpointErr
			}
			result.Checkpoint = &checkpoint
			return result, nil
		}

		var after *EnvironmentSnapshot
		if request.Environment != nil {
			snapshot, snapshotErr := request.Environment.Snapshot(goalCtx, SnapshotRequest{
				Task: request.Task, Phase: SnapshotPhaseAfter,
			})
			if snapshotErr != nil {
				return result, fmt.Errorf("capture environment after snapshot: %w", snapshotErr)
			}
			after = cloneEnvironmentSnapshot(&snapshot)
			result.After = after
		}

		if runner.collector != nil {
			items, collectErr := runner.collector.Collect(goalCtx, EvidenceCollectionRequest{
				Task: request.Task, Run: attemptResult, Before: result.Before, After: after, Attempt: attempt,
			})
			if collectErr != nil {
				return result, fmt.Errorf("collect task evidence: %w", collectErr)
			}
			for _, item := range items {
				result.Evidence, err = result.Evidence.With(item)
				if err != nil {
					return result, fmt.Errorf("append task evidence: %w", err)
				}
			}
		}

		verification, verifyErr := runner.verifier.Verify(goalCtx, VerificationRequest{
			Task: request.Task, Run: cumulative, Before: result.Before, After: after,
			Evidence: result.Evidence, RepairAttempts: attempt,
		})
		if verifyErr != nil {
			return result, fmt.Errorf("verify task completion: %w", verifyErr)
		}
		result.Verification = cloneVerificationResult(verification)
		if verification.Passed() {
			result.Status = GoalRunVerified
			return result, nil
		}
		if !verification.Retryable || attempt >= request.Task.MaxRepairAttempts {
			result.Status = GoalRunBlocked
			return result, nil
		}

		if request.RepairBuilder == nil {
			currentRequest, err = buildGoalRepairRequest(request.Run, cumulative, verification)
		} else {
			missing := sortedUniqueStrings(verification.MissingEvidence)
			currentRequest, err = request.RepairBuilder.BuildRepair(goalCtx, VerifiedRepairRequest{
				Task: request.Task, Base: request.Run, Previous: cumulative,
				Verification: cloneVerificationResult(verification),
				Evidence:     cloneEvidenceLedger(result.Evidence), Attempt: attempt + 1,
				Signal: VerifiedRepairSignal{
					Reason: VerifiedRepairEvidenceMissing, MissingCriterionIDs: missing,
				},
			})
			if err == nil {
				err = validateVerifiedRepairRequest(request.Run, cumulative, currentRequest)
			}
		}
		if err != nil {
			return result, err
		}
	}
}

func resolveGoalTools(
	ctx context.Context,
	task TaskSpec,
	requestTools []ToolDefinition,
	environment Environment,
) ([]ToolDefinition, error) {
	requestCatalog := make(map[string]ToolDefinition, len(requestTools))
	for _, tool := range requestTools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: "tool name is required"}
		}
		if _, exists := requestCatalog[name]; exists {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: "duplicate request tool " + name}
		}
		requestCatalog[name] = tool
	}

	environmentCatalog := requestCatalog
	if environment != nil {
		if strings.TrimSpace(environment.Name()) == "" {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: "environment name is required"}
		}
		available, err := environment.Tools(ctx, task)
		if err != nil {
			return nil, fmt.Errorf("resolve environment tools: %w", err)
		}
		environmentCatalog = make(map[string]ToolDefinition, len(available))
		for _, tool := range available {
			name := strings.TrimSpace(tool.Name)
			if name == "" {
				return nil, &RunError{Code: ErrorInvalidRequest, Message: "environment tool name is required"}
			}
			if _, exists := environmentCatalog[name]; exists {
				return nil, &RunError{Code: ErrorInvalidRequest, Message: "duplicate environment tool " + name}
			}
			environmentCatalog[name] = tool
		}
	}

	resolved := make([]ToolDefinition, 0, len(task.AllowedTools))
	for _, name := range task.AllowedTools {
		requestTool, requestOK := requestCatalog[name]
		_, environmentOK := environmentCatalog[name]
		if !requestOK || !environmentOK {
			return nil, &RunError{Code: ErrorInvalidRequest, Message: "task tool is unavailable: " + name}
		}
		resolved = append(resolved, requestTool)
	}
	return resolved, nil
}

func buildGoalRepairRequest(base RunRequest, previous RunResult, verification VerificationResult) (RunRequest, error) {
	request := base
	request.InitialToolChoice = ToolChoiceAuto
	request.Messages = cloneMessages(previous.Messages)
	request.Messages = append(request.Messages, Message{
		Role: RoleDeveloper,
		Content: "Completion verification failed. Continue the same task and repair only the missing required criteria. " +
			"Do not claim completion until new observable evidence is available. Missing criteria: " +
			strings.Join(sortedUniqueStrings(verification.MissingEvidence), ", "),
	})

	budget := base.Context.Budget
	maxSteps := budget.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}
	remainingSteps := maxSteps - len(previous.Steps)
	if remainingSteps <= 0 {
		return RunRequest{}, &RunError{
			Code: ErrorBudgetExceeded, Step: len(previous.Steps),
			Message: "repair requires another step but the run step budget is exhausted",
		}
	}
	budget.MaxSteps = remainingSteps
	if budget.MaxTotalTokens > 0 {
		budget.MaxTotalTokens -= previous.Usage.TotalTokens
		if budget.MaxTotalTokens <= 0 {
			return RunRequest{}, &RunError{Code: ErrorBudgetExceeded, Message: "repair token budget is exhausted"}
		}
	}
	if budget.MaxEstimatedCostMicros > 0 {
		budget.MaxEstimatedCostMicros -= previous.Usage.EstimatedCostMicros
		if budget.MaxEstimatedCostMicros <= 0 {
			return RunRequest{}, &RunError{Code: ErrorBudgetExceeded, Message: "repair cost budget is exhausted"}
		}
	}
	request.Context.Budget = budget
	return request, nil
}

func mergeGoalRunResults(current, next RunResult, originalContext RunContext) RunResult {
	if len(current.Messages) == 0 && len(current.Steps) == 0 {
		next.Context = originalContext
		return next
	}
	merged := next
	merged.Context = originalContext
	merged.Steps = append(cloneSteps(current.Steps), cloneSteps(next.Steps)...)
	for index := len(current.Steps); index < len(merged.Steps); index++ {
		merged.Steps[index].Index = index + 1
	}
	merged.Usage = current.Usage
	merged.Usage.Add(next.Usage)
	return merged
}

func cloneEvidenceLedger(ledger EvidenceLedger) EvidenceLedger {
	result := EvidenceLedger{Items: make([]Evidence, len(ledger.Items))}
	for index, item := range ledger.Items {
		item.CriterionIDs = cloneStrings(item.CriterionIDs)
		result.Items[index] = item
	}
	return result
}

func cloneEnvironmentSnapshot(snapshot *EnvironmentSnapshot) *EnvironmentSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.Metadata = append([]byte(nil), snapshot.Metadata...)
	return &cloned
}

func cloneVerificationResult(result VerificationResult) VerificationResult {
	cloned := result
	cloned.MissingEvidence = cloneStrings(result.MissingEvidence)
	cloned.Checks = make([]CheckResult, len(result.Checks))
	for index, check := range result.Checks {
		check.EvidenceIDs = cloneStrings(check.EvidenceIDs)
		cloned.Checks[index] = check
	}
	return cloned
}

func sortedUniqueStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			unique[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}
