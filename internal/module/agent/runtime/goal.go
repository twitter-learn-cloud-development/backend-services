package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TaskSpec defines a goal and the observable conditions required to complete it.
// A model final answer is not, by itself, completion evidence.
type TaskSpec struct {
	ID                 string
	Goal               string
	Constraints        []TaskConstraint
	CompletionCriteria []CompletionCriterion
	AllowedTools       []string
	MaxRepairAttempts  int
}

type TaskConstraint struct {
	ID          string
	Description string
}

type CompletionCriterion struct {
	ID          string
	Description string
	Required    bool
}

func (spec TaskSpec) Validate() error {
	if strings.TrimSpace(spec.Goal) == "" {
		return fmt.Errorf("task goal is required")
	}
	if spec.MaxRepairAttempts < 0 {
		return fmt.Errorf("max repair attempts must not be negative")
	}
	if len(spec.CompletionCriteria) == 0 {
		return fmt.Errorf("at least one completion criterion is required")
	}

	criterionIDs := make(map[string]struct{}, len(spec.CompletionCriteria))
	requiredCriteria := 0
	for _, criterion := range spec.CompletionCriteria {
		id := strings.TrimSpace(criterion.ID)
		if id == "" || strings.TrimSpace(criterion.Description) == "" {
			return fmt.Errorf("completion criterion id and description are required")
		}
		if _, exists := criterionIDs[id]; exists {
			return fmt.Errorf("duplicate completion criterion %q", id)
		}
		criterionIDs[id] = struct{}{}
		if criterion.Required {
			requiredCriteria++
		}
	}
	if requiredCriteria == 0 {
		return fmt.Errorf("at least one completion criterion must be required")
	}

	constraintIDs := make(map[string]struct{}, len(spec.Constraints))
	for _, constraint := range spec.Constraints {
		id := strings.TrimSpace(constraint.ID)
		if id == "" || strings.TrimSpace(constraint.Description) == "" {
			return fmt.Errorf("task constraint id and description are required")
		}
		if _, exists := constraintIDs[id]; exists {
			return fmt.Errorf("duplicate task constraint %q", id)
		}
		constraintIDs[id] = struct{}{}
	}

	tools := make(map[string]struct{}, len(spec.AllowedTools))
	for _, tool := range spec.AllowedTools {
		name := strings.TrimSpace(tool)
		if name == "" {
			return fmt.Errorf("allowed tool name is required")
		}
		if _, exists := tools[name]; exists {
			return fmt.Errorf("duplicate allowed tool %q", name)
		}
		tools[name] = struct{}{}
	}
	return nil
}

type SnapshotPhase string

const (
	SnapshotPhaseBefore SnapshotPhase = "before"
	SnapshotPhaseAfter  SnapshotPhase = "after"
)

type SnapshotRequest struct {
	Task  TaskSpec
	Phase SnapshotPhase
	Scope []string
}

// EnvironmentSnapshot is an immutable, low-sensitivity reference to observed
// external state. Large or sensitive state remains in the owning environment.
type EnvironmentSnapshot struct {
	ID          string
	Environment string
	CapturedAt  time.Time
	Digest      string
	Reference   string
	Metadata    json.RawMessage
}

// Environment exposes observable state and governed tool capabilities. Actions
// still execute through ToolExecutor so policy, approval, audit and idempotency
// cannot be bypassed by an environment adapter.
type Environment interface {
	Name() string
	Tools(ctx context.Context, task TaskSpec) ([]ToolDefinition, error)
	Snapshot(ctx context.Context, request SnapshotRequest) (EnvironmentSnapshot, error)
}

type EvidenceKind string

const (
	EvidenceToolObservation  EvidenceKind = "tool_observation"
	EvidenceEnvironmentState EvidenceKind = "environment_state"
	EvidenceApproval         EvidenceKind = "approval"
	EvidenceArtifact         EvidenceKind = "artifact"
	EvidenceCheckpointResume EvidenceKind = "checkpoint_resume"
	EvidenceProviderRouting  EvidenceKind = "provider_routing"
)

// Evidence stores provenance and references, not arbitrary raw tool output.
type Evidence struct {
	ID           string
	Kind         EvidenceKind
	Source       string
	CriterionIDs []string
	Digest       string
	Reference    string
	StepIndex    int
	CapturedAt   time.Time
}

type EvidenceLedger struct {
	Items []Evidence
}

// With returns a new ledger to preserve append-only execution semantics.
func (ledger EvidenceLedger) With(item Evidence) (EvidenceLedger, error) {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Source) == "" {
		return EvidenceLedger{}, fmt.Errorf("evidence id and source are required")
	}
	if item.Kind == "" {
		return EvidenceLedger{}, fmt.Errorf("evidence kind is required")
	}
	if strings.TrimSpace(item.Digest) == "" && strings.TrimSpace(item.Reference) == "" {
		return EvidenceLedger{}, fmt.Errorf("evidence digest or reference is required")
	}
	for _, existing := range ledger.Items {
		if existing.ID == item.ID {
			return EvidenceLedger{}, fmt.Errorf("duplicate evidence %q", item.ID)
		}
	}
	criterionIDs := make(map[string]struct{}, len(item.CriterionIDs))
	for _, criterionID := range item.CriterionIDs {
		criterionID = strings.TrimSpace(criterionID)
		if criterionID == "" {
			return EvidenceLedger{}, fmt.Errorf("evidence criterion id is required")
		}
		if _, exists := criterionIDs[criterionID]; exists {
			return EvidenceLedger{}, fmt.Errorf("duplicate evidence criterion %q", criterionID)
		}
		criterionIDs[criterionID] = struct{}{}
	}

	result := EvidenceLedger{Items: make([]Evidence, len(ledger.Items), len(ledger.Items)+1)}
	copy(result.Items, ledger.Items)
	item.CriterionIDs = append([]string(nil), item.CriterionIDs...)
	result.Items = append(result.Items, item)
	return result, nil
}

type VerificationStatus string

const (
	VerificationPassed       VerificationStatus = "passed"
	VerificationFailed       VerificationStatus = "failed"
	VerificationInconclusive VerificationStatus = "inconclusive"
)

type CheckResult struct {
	CriterionID string
	Status      VerificationStatus
	Code        string
	EvidenceIDs []string
}

type VerificationRequest struct {
	Task           TaskSpec
	Run            RunResult
	Before         *EnvironmentSnapshot
	After          *EnvironmentSnapshot
	Evidence       EvidenceLedger
	RepairAttempts int
}

type VerificationResult struct {
	Status          VerificationStatus
	Checks          []CheckResult
	MissingEvidence []string
	Retryable       bool
}

func (result VerificationResult) Passed() bool {
	return result.Status == VerificationPassed
}

type Verifier interface {
	Verify(ctx context.Context, request VerificationRequest) (VerificationResult, error)
}

// SuspendedRunVerifier is an opt-in extension for tasks whose correct outcome
// is a governed suspension, such as asking a human to resolve conflicting
// evidence. Verifiers that do not implement it preserve the existing behavior:
// suspended runs remain inconclusive and resumable.
type SuspendedRunVerifier interface {
	VerifySuspension(ctx context.Context, request VerificationRequest) (VerificationResult, error)
}

// RequiredEvidenceVerifier is a deterministic lower-bound verifier. It proves
// required evidence is present; domain verifiers must still validate its truth.
type RequiredEvidenceVerifier struct{}

func (RequiredEvidenceVerifier) Verify(_ context.Context, request VerificationRequest) (VerificationResult, error) {
	if err := request.Task.Validate(); err != nil {
		return VerificationResult{}, err
	}

	byCriterion := make(map[string][]string)
	for _, item := range request.Evidence.Items {
		for _, criterionID := range item.CriterionIDs {
			byCriterion[criterionID] = append(byCriterion[criterionID], item.ID)
		}
	}

	result := VerificationResult{Status: VerificationPassed}
	for _, criterion := range request.Task.CompletionCriteria {
		if !criterion.Required {
			continue
		}
		ids := append([]string(nil), byCriterion[criterion.ID]...)
		check := CheckResult{CriterionID: criterion.ID, EvidenceIDs: ids}
		if len(ids) == 0 {
			check.Status = VerificationFailed
			check.Code = "required_evidence_missing"
			result.Status = VerificationFailed
			result.MissingEvidence = append(result.MissingEvidence, criterion.ID)
		} else {
			check.Status = VerificationPassed
			check.Code = "required_evidence_present"
		}
		result.Checks = append(result.Checks, check)
	}
	result.Retryable = result.Status == VerificationFailed && request.RepairAttempts < request.Task.MaxRepairAttempts
	return result, nil
}
