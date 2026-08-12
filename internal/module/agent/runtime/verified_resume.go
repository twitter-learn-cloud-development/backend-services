package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Resume continues a suspended verified run without trusting the old tool
// catalog. The underlying ReAct runner restores cumulative execution usage;
// this layer restores task evidence, snapshots and the repair budget.
func (runner *VerifiedRunner) Resume(
	ctx context.Context,
	request VerifiedResumeRequest,
) (VerifiedRunResult, error) {
	checkpoint := cloneVerifiedCheckpoint(request.Checkpoint)
	result := VerifiedRunResult{
		Evidence:       cloneEvidenceLedger(checkpoint.Evidence),
		Before:         cloneEnvironmentSnapshot(checkpoint.Before),
		RepairAttempts: checkpoint.RepairAttempts,
	}
	if runner == nil || runner.runner == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "agent runner is required"}
	}
	if runner.verifier == nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: "task verifier is required"}
	}
	if err := ValidateVerifiedCheckpoint(checkpoint); err != nil {
		return result, &RunError{
			Code: ErrorInvalidRequest, Message: "invalid verified checkpoint", Cause: err,
		}
	}
	if err := validateVerifiedEnvironment(checkpoint.Environment, request.Environment); err != nil {
		return result, &RunError{Code: ErrorInvalidRequest, Message: err.Error()}
	}
	resumable, ok := runner.runner.(ResumableAgentRunner)
	if !ok {
		return result, &RunError{
			Code: ErrorUnsupported, Message: "agent runner does not support verified resume",
		}
	}

	goalCtx, cancel := WithBudgetContext(ctx, checkpoint.Run.Context.Budget)
	defer cancel()

	tools, err := resolveGoalTools(goalCtx, checkpoint.Task, request.Tools, request.Environment)
	if err != nil {
		return result, err
	}
	base := RunRequest{
		Context: checkpoint.Run.Context,
		Model:   checkpoint.Run.Model,
		Tools:   tools,
	}
	resumed, runErr := resumable.Resume(goalCtx, ResumeRequest{
		Checkpoint:    checkpoint.Run,
		HumanResponse: request.HumanResponse,
		ApprovalID:    request.ApprovalID,
		ResumeToken:   request.ResumeToken,
		Tools:         tools,
	})
	if runErr != nil && !isGoalSuspendedStatus(resumed.Status) {
		return result, runErr
	}
	if err := validateResumedGoalResult(checkpoint.Run, resumed); err != nil {
		return result, &RunError{
			Code: ErrorInvalidRequest, Message: "invalid resumed run result", Cause: err,
		}
	}

	result.Evidence, err = appendCheckpointResumeEvidence(result.Evidence, checkpoint)
	if err != nil {
		return result, fmt.Errorf("append checkpoint resume evidence: %w", err)
	}

	cumulative := resumed
	evidenceRun := resumedGoalSegment(checkpoint.Run, resumed)
	for {
		result.Run = cumulative
		if cumulative.Status != RunStatusCompleted {
			var suspendedAfter *EnvironmentSnapshot
			if request.Environment != nil {
				snapshot, snapshotErr := request.Environment.Snapshot(goalCtx, SnapshotRequest{
					Task: checkpoint.Task, Phase: SnapshotPhaseAfter,
				})
				if snapshotErr != nil {
					return result, fmt.Errorf("capture suspended environment snapshot: %w", snapshotErr)
				}
				suspendedAfter = cloneEnvironmentSnapshot(&snapshot)
				result.After = suspendedAfter
			}

			if runner.collector != nil {
				items, collectErr := runner.collector.Collect(goalCtx, EvidenceCollectionRequest{
					Task: checkpoint.Task, Run: evidenceRun, Before: result.Before,
					After: suspendedAfter, Attempt: result.RepairAttempts,
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

			result.Status = GoalRunSuspended
			result.Verification = VerificationResult{Status: VerificationInconclusive}
			next, checkpointErr := buildVerifiedCheckpoint(
				checkpoint.Task, base, request.Environment, result, checkpoint.Revision,
			)
			if checkpointErr != nil {
				return result, checkpointErr
			}
			result.Checkpoint = &next
			return result, nil
		}

		var after *EnvironmentSnapshot
		if request.Environment != nil {
			snapshot, snapshotErr := request.Environment.Snapshot(goalCtx, SnapshotRequest{
				Task: checkpoint.Task, Phase: SnapshotPhaseAfter,
			})
			if snapshotErr != nil {
				return result, fmt.Errorf("capture environment after snapshot: %w", snapshotErr)
			}
			after = cloneEnvironmentSnapshot(&snapshot)
			result.After = after
		}

		if runner.collector != nil {
			items, collectErr := runner.collector.Collect(goalCtx, EvidenceCollectionRequest{
				Task: checkpoint.Task, Run: evidenceRun, Before: result.Before,
				After: after, Attempt: result.RepairAttempts,
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
			Task: checkpoint.Task, Run: cumulative, Before: result.Before, After: after,
			Evidence: result.Evidence, RepairAttempts: result.RepairAttempts,
		})
		if verifyErr != nil {
			return result, fmt.Errorf("verify task completion: %w", verifyErr)
		}
		result.Verification = cloneVerificationResult(verification)
		if verification.Passed() {
			result.Status = GoalRunVerified
			return result, nil
		}
		if !verification.Retryable ||
			result.RepairAttempts >= checkpoint.Task.MaxRepairAttempts {
			result.Status = GoalRunBlocked
			return result, nil
		}

		result.RepairAttempts++
		repairRequest, repairErr := buildGoalRepairRequest(base, cumulative, verification)
		if repairErr != nil {
			return result, repairErr
		}
		repairResult, repairRunErr := runner.runner.Run(goalCtx, repairRequest)
		if repairRunErr != nil && !isGoalSuspendedStatus(repairResult.Status) {
			return result, repairRunErr
		}
		cumulative = mergeGoalRunResults(cumulative, repairResult, base.Context)
		evidenceRun = repairResult
	}
}

func buildVerifiedCheckpoint(
	task TaskSpec,
	base RunRequest,
	environment Environment,
	result VerifiedRunResult,
	previousRevision int64,
) (VerifiedCheckpoint, error) {
	if !isGoalSuspendedStatus(result.Run.Status) {
		return VerifiedCheckpoint{}, &RunError{
			Code: ErrorInvalidRequest, Message: "verified checkpoint requires a suspended run",
		}
	}
	runCheckpoint, err := NewRunCheckpoint(base, result.Run)
	if err != nil {
		return VerifiedCheckpoint{}, fmt.Errorf("build verified run checkpoint: %w", err)
	}
	environmentName := ""
	if environment != nil {
		environmentName = strings.TrimSpace(environment.Name())
	}
	checkpoint := VerifiedCheckpoint{
		Revision:       previousRevision + 1,
		Version:        VerifiedCheckpointVersion,
		Task:           cloneTaskSpec(task),
		Run:            runCheckpoint,
		Environment:    environmentName,
		Evidence:       cloneEvidenceLedger(result.Evidence),
		Before:         cloneEnvironmentSnapshot(result.Before),
		RepairAttempts: result.RepairAttempts,
	}
	if err := ValidateVerifiedCheckpoint(checkpoint); err != nil {
		return VerifiedCheckpoint{}, fmt.Errorf("validate verified checkpoint: %w", err)
	}
	return checkpoint, nil
}

func appendCheckpointResumeEvidence(
	ledger EvidenceLedger,
	checkpoint VerifiedCheckpoint,
) (EvidenceLedger, error) {
	runID := strings.TrimSpace(checkpoint.Run.Context.RunID)
	identity := fmt.Sprintf(
		"%s|%d|%s|%s|%s|%s|%d",
		checkpoint.Version, checkpoint.Revision, checkpoint.Run.Version, runID,
		checkpoint.Run.PendingAction.ID, checkpoint.Run.PendingResumeKind, len(checkpoint.Run.Steps),
	)
	digest := sha256.Sum256([]byte(identity))
	stepIndex := 0
	var capturedAt time.Time
	if count := len(checkpoint.Run.Steps); count > 0 {
		stepIndex = checkpoint.Run.Steps[count-1].Index
		capturedAt = checkpoint.Run.Steps[count-1].FinishedAt
	}
	return ledger.With(Evidence{
		ID:   fmt.Sprintf("checkpoint-resume:%s:%d", runID, checkpoint.Revision),
		Kind: EvidenceCheckpointResume, Source: CheckpointResumeEvidenceSource,
		Digest: "sha256:" + hex.EncodeToString(digest[:]),
		Reference: fmt.Sprintf(
			"agent-run://%s/checkpoints/%d/resume", runID, checkpoint.Revision,
		),
		StepIndex: stepIndex, CapturedAt: capturedAt,
	})
}

func ValidateVerifiedCheckpoint(checkpoint VerifiedCheckpoint) error {
	if checkpoint.Version != VerifiedCheckpointVersion {
		return fmt.Errorf("unsupported verified checkpoint version")
	}
	if checkpoint.Revision <= 0 {
		return fmt.Errorf("verified checkpoint revision must be positive")
	}

	if err := checkpoint.Task.Validate(); err != nil {
		return fmt.Errorf("invalid checkpoint task: %w", err)
	}
	if strings.TrimSpace(checkpoint.Task.ID) == "" {
		return fmt.Errorf("verified checkpoint task id is required")
	}
	if err := ValidateRunCheckpoint(checkpoint.Run); err != nil {
		return fmt.Errorf("invalid checkpoint run: %w", err)
	}
	if checkpoint.RepairAttempts < 0 ||
		checkpoint.RepairAttempts > checkpoint.Task.MaxRepairAttempts {
		return fmt.Errorf("verified checkpoint repair attempts are invalid")
	}
	if checkpoint.Before != nil &&
		strings.TrimSpace(checkpoint.Before.Environment) != strings.TrimSpace(checkpoint.Environment) {
		return fmt.Errorf("verified checkpoint environment snapshot is not bound to the environment")
	}

	criteria := make(map[string]struct{}, len(checkpoint.Task.CompletionCriteria))
	for _, criterion := range checkpoint.Task.CompletionCriteria {
		criteria[strings.TrimSpace(criterion.ID)] = struct{}{}
	}
	ledger := EvidenceLedger{}
	for _, item := range checkpoint.Evidence.Items {
		for _, criterionID := range item.CriterionIDs {
			if _, ok := criteria[strings.TrimSpace(criterionID)]; !ok {
				return fmt.Errorf("evidence references unknown criterion %q", criterionID)
			}
		}
		var err error
		ledger, err = ledger.With(item)
		if err != nil {
			return fmt.Errorf("invalid checkpoint evidence: %w", err)
		}
	}
	return nil
}

func validateVerifiedEnvironment(expected string, environment Environment) error {
	actual := ""
	if environment != nil {
		actual = strings.TrimSpace(environment.Name())
	}
	if strings.TrimSpace(expected) != actual {
		return fmt.Errorf("verified checkpoint environment does not match the current environment")
	}
	return nil
}

func resumedGoalSegment(checkpoint RunCheckpoint, resumed RunResult) RunResult {
	segment := resumed
	start := len(checkpoint.Steps) - 1
	if start < 0 {
		start = 0
	}
	if start > len(resumed.Steps) {
		start = len(resumed.Steps)
	}
	segment.Steps = cloneSteps(resumed.Steps[start:])
	return segment
}

func isGoalSuspendedStatus(status RunStatus) bool {
	return status == RunStatusAwaitingHuman || status == RunStatusApprovalRequired
}

func cloneVerifiedCheckpoint(checkpoint VerifiedCheckpoint) VerifiedCheckpoint {
	checkpoint.Task = cloneTaskSpec(checkpoint.Task)
	checkpoint.Run = cloneRunCheckpoint(checkpoint.Run)
	checkpoint.Evidence = cloneEvidenceLedger(checkpoint.Evidence)
	checkpoint.Before = cloneEnvironmentSnapshot(checkpoint.Before)
	return checkpoint
}

func cloneTaskSpec(task TaskSpec) TaskSpec {
	task.Constraints = append([]TaskConstraint(nil), task.Constraints...)
	task.CompletionCriteria = append([]CompletionCriterion(nil), task.CompletionCriteria...)
	task.AllowedTools = cloneStrings(task.AllowedTools)
	return task
}
