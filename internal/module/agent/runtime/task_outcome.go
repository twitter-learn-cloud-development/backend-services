package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const TaskOutcomeVersionV1 = "goal.task_outcome.v1"

type TaskOutcomeExecutionSource string

const (
	TaskOutcomeExecutionPlanned  TaskOutcomeExecutionSource = "planned_execution"
	TaskOutcomeExecutionObserved TaskOutcomeExecutionSource = "observed_execution"
)

// TaskOutcome is the low-sensitivity result used by evaluation and future
// product projections. It binds artifacts and verification to an explicit
// execution source without embedding model or tool bodies.
type TaskOutcome struct {
	Version           string
	ExecutionSource   TaskOutcomeExecutionSource
	TaskID            string
	RunID             string
	Status            GoalRunStatus
	PlanDigests       []string
	RecoveryDecisions []RecoveryDecision
	Artifacts         []VerifiedArtifact
	Evidence          EvidenceLedger
	Verification      VerificationResult
	RepairAttempts    int
	FinalAnswerDigest string
}

type RecoveryDecision struct {
	Attempt    int
	Reason     ShortPlanRecoveryReason
	PlanDigest string
}

type VerifiedArtifact struct {
	ID                 string
	Type               string
	Digest             string
	Reference          string
	CriterionIDs       []string
	SupportingEvidence []string
	Status             VerificationStatus
}

func BuildTaskOutcome(task TaskSpec, result PlannedVerifiedRunResult) (TaskOutcome, error) {
	outcome, err := buildTaskOutcome(task, result.Verified, TaskOutcomeExecutionPlanned)
	if err != nil {
		return TaskOutcome{}, err
	}
	if result.Planning.Plan.Digest == "" {
		return TaskOutcome{}, fmt.Errorf("initial admitted plan digest is required")
	}
	outcome.PlanDigests = append(outcome.PlanDigests, result.Planning.Plan.Digest)
	for index, recovery := range result.RecoveryPlans {
		if recovery.Plan.Digest == "" {
			return TaskOutcome{}, fmt.Errorf("recovery plan %d digest is required", index+1)
		}
		outcome.PlanDigests = append(outcome.PlanDigests, recovery.Plan.Digest)
		reason := ShortPlanRecoveryReason("")
		if index == len(result.RecoveryPlans)-1 {
			reason = result.LastRecoveryReason
		}
		outcome.RecoveryDecisions = append(outcome.RecoveryDecisions, RecoveryDecision{
			Attempt: index + 1, Reason: reason, PlanDigest: recovery.Plan.Digest,
		})
	}
	if len(outcome.RecoveryDecisions) != result.RecoveryAttempts {
		return TaskOutcome{}, fmt.Errorf("recovery attempt count does not match admitted recovery plans")
	}
	return outcome, nil
}

// BuildObservedTaskOutcome projects an already executed result into the Goal
// outcome contract. It is intended for side-effect-free migration comparison:
// callers may collect and verify existing observations, but must not claim that
// a short plan drove the original execution.
func BuildObservedTaskOutcome(task TaskSpec, result VerifiedRunResult) (TaskOutcome, error) {
	return buildTaskOutcome(task, result, TaskOutcomeExecutionObserved)
}

func buildTaskOutcome(
	task TaskSpec,
	result VerifiedRunResult,
	source TaskOutcomeExecutionSource,
) (TaskOutcome, error) {
	outcome := TaskOutcome{
		Version:         TaskOutcomeVersionV1,
		ExecutionSource: source,
		TaskID:          strings.TrimSpace(task.ID),
		RunID:           strings.TrimSpace(result.Run.Context.RunID),
		Status:          result.Status,
		Evidence:        cloneEvidenceLedger(result.Evidence),
		Verification:    cloneVerificationResult(result.Verification),
		RepairAttempts:  result.RepairAttempts,
	}
	if err := task.Validate(); err != nil {
		return TaskOutcome{}, fmt.Errorf("invalid task outcome specification: %w", err)
	}
	if source != TaskOutcomeExecutionPlanned && source != TaskOutcomeExecutionObserved {
		return TaskOutcome{}, fmt.Errorf("task outcome execution source is invalid")
	}
	if outcome.TaskID == "" || outcome.RunID == "" {
		return TaskOutcome{}, fmt.Errorf("task and run identity are required")
	}
	if answer := strings.TrimSpace(result.Run.FinalAnswer); answer != "" {
		digest := sha256.Sum256([]byte(answer))
		outcome.FinalAnswerDigest = "sha256:" + hex.EncodeToString(digest[:])
	}

	criteria := make(map[string]struct{}, len(task.CompletionCriteria))
	checks := make(map[string]CheckResult, len(outcome.Verification.Checks))
	for _, criterion := range task.CompletionCriteria {
		criteria[criterion.ID] = struct{}{}
	}
	for _, check := range outcome.Verification.Checks {
		checks[check.CriterionID] = check
	}
	for _, item := range outcome.Evidence.Items {
		if item.Kind != EvidenceArtifact {
			continue
		}
		artifact := VerifiedArtifact{
			ID: item.ID, Type: item.Source, Digest: item.Digest, Reference: item.Reference,
			CriterionIDs: sortedUniqueStrings(item.CriterionIDs), Status: VerificationPassed,
		}
		if len(artifact.CriterionIDs) == 0 {
			return TaskOutcome{}, fmt.Errorf("artifact evidence %q has no completion criterion", item.ID)
		}
		for _, criterionID := range artifact.CriterionIDs {
			if _, exists := criteria[criterionID]; !exists {
				return TaskOutcome{}, fmt.Errorf("artifact evidence %q references unknown criterion %q", item.ID, criterionID)
			}
			check, exists := checks[criterionID]
			if !exists {
				artifact.Status = VerificationInconclusive
				continue
			}
			artifact.SupportingEvidence = append(artifact.SupportingEvidence, check.EvidenceIDs...)
			if check.Status != VerificationPassed || !stringSliceContains(check.EvidenceIDs, item.ID) {
				artifact.Status = VerificationFailed
			}
		}
		artifact.SupportingEvidence = sortedUniqueStrings(artifact.SupportingEvidence)
		outcome.Artifacts = append(outcome.Artifacts, artifact)
	}
	sort.Slice(outcome.Artifacts, func(i, j int) bool { return outcome.Artifacts[i].ID < outcome.Artifacts[j].ID })
	return outcome, nil
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
