package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	WorkflowToolOutputVerifiedCriterion = "workflow_output_verified"
	WorkflowToolResultSchema            = "workflow.run.v1"
	workflowToolSuccessStatus           = "success"
	maxWorkflowToolResultBytes          = 64 << 10
)

// WorkflowToolResult is the model-visible, structured completion envelope.
// The evidence ledger stores only its digests and references.
type WorkflowToolResult struct {
	Schema                 string `json:"schema"`
	PublicationID          string `json:"publication_id"`
	PublicationRevision    int64  `json:"publication_revision"`
	WorkflowID             string `json:"workflow_id"`
	WorkflowRevisionID     string `json:"workflow_revision_id"`
	WorkflowRevisionNumber int64  `json:"workflow_revision_number"`
	WorkflowDSLHash        string `json:"workflow_dsl_hash"`
	BindingDigest          string `json:"binding_digest"`
	WorkflowRunID          string `json:"workflow_run_id"`
	ParentRunID            string `json:"parent_run_id"`
	ParentActionID         string `json:"parent_action_id"`
	Status                 string `json:"status"`
	Response               string `json:"response"`
	ResponseDigest         string `json:"response_digest"`
	RunOutputDigest        string `json:"run_output_digest"`
}

// WorkflowToolRunEvidence is the low-sensitivity projection read from the
// authoritative child Workflow Run store during collection and verification.
type WorkflowToolRunEvidence struct {
	WorkflowRunID          string
	WorkflowID             string
	WorkflowRevisionID     string
	WorkflowRevisionNumber int64
	InvocationSource       string
	ParentRunID            string
	ParentActionID         string
	Status                 string
	RunOutputDigest        string
	FinishedAt             time.Time
}

type WorkflowToolRunEvidenceResolver interface {
	ResolveWorkflowToolRunEvidence(
		ctx context.Context,
		userID uint64,
		workflowRunID string,
	) (WorkflowToolRunEvidence, error)
}

type WorkflowToolGoalCollector struct {
	Resolver WorkflowToolRunEvidenceResolver
}

func (collector WorkflowToolGoalCollector) Collect(
	ctx context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	if err := validateWorkflowToolTask(request.Task); err != nil {
		return nil, err
	}
	candidates, err := workflowToolEvidenceCandidates(
		ctx, collector.Resolver, request.Task, request.Run, request.Before, request.After,
	)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]agentRuntime.Evidence, 0, len(keys))
	for _, key := range keys {
		items = append(items, candidates[key])
	}
	return items, nil
}

type WorkflowToolGoalVerifier struct {
	Resolver WorkflowToolRunEvidenceResolver
}

func (verifier WorkflowToolGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	if err := validateWorkflowToolTask(request.Task); err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	valid, err := workflowToolEvidenceCandidates(
		ctx, verifier.Resolver, request.Task, request.Run, request.Before, request.After,
	)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	matched := make([]string, 0, 1)
	for _, item := range request.Evidence.Items {
		if item.Kind != agentRuntime.EvidenceToolObservation ||
			!containsString(item.CriterionIDs, WorkflowToolOutputVerifiedCriterion) {
			continue
		}
		if _, ok := valid[workflowToolEvidenceKey(item)]; ok {
			matched = append(matched, item.ID)
		}
	}
	check := agentRuntime.CheckResult{
		CriterionID: WorkflowToolOutputVerifiedCriterion,
		Status:      agentRuntime.VerificationPassed,
		Code:        "workflow_tool_output_verified",
		EvidenceIDs: matched,
	}
	if len(matched) != 1 || len(valid) != 1 {
		check.Status = agentRuntime.VerificationFailed
		check.Code = "workflow_tool_output_missing"
	}
	replaceCheck(&base, check)
	base.MissingEvidence = missingRequiredCriteria(request.Task, base.Checks)
	if len(base.MissingEvidence) == 0 {
		base.Status = agentRuntime.VerificationPassed
		base.Retryable = false
	} else {
		base.Status = agentRuntime.VerificationFailed
		base.Retryable = request.RepairAttempts < request.Task.MaxRepairAttempts
	}
	return base, nil
}

func workflowToolEvidenceCandidates(
	ctx context.Context,
	resolver WorkflowToolRunEvidenceResolver,
	task agentRuntime.TaskSpec,
	run agentRuntime.RunResult,
	beforeSnapshot *agentRuntime.EnvironmentSnapshot,
	afterSnapshot *agentRuntime.EnvironmentSnapshot,
) (map[string]agentRuntime.Evidence, error) {
	if resolver == nil {
		return nil, fmt.Errorf("workflow tool run evidence resolver is required")
	}
	before, err := agentEnvironment.DecodeWorkflowToolSnapshot(
		beforeSnapshot, agentRuntime.SnapshotPhaseBefore, run.Context.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("decode workflow tool before snapshot: %w", err)
	}
	after, err := agentEnvironment.DecodeWorkflowToolSnapshot(
		afterSnapshot, agentRuntime.SnapshotPhaseAfter, run.Context.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("decode workflow tool after snapshot: %w", err)
	}
	if beforeSnapshot.Digest != afterSnapshot.Digest || !equalWorkflowToolSnapshotViews(before, after) {
		return map[string]agentRuntime.Evidence{}, nil
	}
	bindings := make(map[string]agentEnvironment.WorkflowToolSnapshotBindingView, len(before.Tools))
	for _, tool := range before.Tools {
		bindings[tool.Name] = tool
	}
	allowed := make(map[string]struct{}, len(task.AllowedTools))
	for _, name := range task.AllowedTools {
		allowed[strings.TrimSpace(name)] = struct{}{}
	}

	items := make(map[string]agentRuntime.Evidence)
	for _, step := range run.Steps {
		actions := make(map[string]agentRuntime.Action, len(step.Actions))
		for _, action := range step.Actions {
			actions[action.ID] = action
		}
		for _, observation := range step.Observations {
			action, paired := actions[observation.ActionID]
			binding, bound := bindings[observation.Name]
			_, admitted := allowed[observation.Name]
			if observation.IsError || !paired || !bound || !admitted ||
				action.Type != agentRuntime.ActionToolCall || action.Name != observation.Name {
				continue
			}
			result, ok := decodeWorkflowToolResult(observation.StructuredContent)
			if !ok || result.ParentRunID != run.Context.RunID ||
				result.ParentActionID != action.ID || result.BindingDigest != binding.BindingDigest {
				continue
			}
			computedBinding, digestErr := agentEnvironment.WorkflowToolBindingDigest(
				agentEnvironment.WorkflowToolBindingIdentity{
					PublicationID: result.PublicationID, PublicationRevision: result.PublicationRevision,
					WorkflowID: result.WorkflowID, WorkflowRevisionID: result.WorkflowRevisionID,
					WorkflowRevisionNumber: result.WorkflowRevisionNumber,
					WorkflowDSLHash:        result.WorkflowDSLHash,
				},
			)
			if digestErr != nil || computedBinding != result.BindingDigest {
				continue
			}
			authoritative, resolveErr := resolver.ResolveWorkflowToolRunEvidence(
				ctx, run.Context.UserID, result.WorkflowRunID,
			)
			if resolveErr != nil {
				return nil, fmt.Errorf("resolve workflow tool child run: %w", resolveErr)
			}
			if !workflowToolRunMatches(result, authoritative) {
				continue
			}
			canonical, _ := json.Marshal(struct {
				BindingDigest   string `json:"binding_digest"`
				WorkflowRunID   string `json:"workflow_run_id"`
				RunOutputDigest string `json:"run_output_digest"`
				ResponseDigest  string `json:"response_digest"`
			}{result.BindingDigest, result.WorkflowRunID, result.RunOutputDigest, result.ResponseDigest})
			digest := sha256.Sum256(canonical)
			item := agentRuntime.Evidence{
				ID:   "workflow-tool:" + hex.EncodeToString(digest[:12]),
				Kind: agentRuntime.EvidenceToolObservation, Source: observation.Name,
				CriterionIDs: []string{WorkflowToolOutputVerifiedCriterion},
				Digest:       "sha256:" + hex.EncodeToString(digest[:]),
				Reference: "agent-workflow-run://" + result.WorkflowRunID +
					"/output/" + strings.TrimPrefix(result.RunOutputDigest, "sha256:"),
				StepIndex: step.Index, CapturedAt: authoritative.FinishedAt,
			}
			items[workflowToolEvidenceKey(item)] = item
		}
	}
	return items, nil
}

func decodeWorkflowToolResult(raw json.RawMessage) (WorkflowToolResult, bool) {
	if len(raw) == 0 || len(raw) > maxWorkflowToolResultBytes || !json.Valid(raw) {
		return WorkflowToolResult{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result WorkflowToolResult
	if decoder.Decode(&result) != nil || result.Schema != WorkflowToolResultSchema ||
		result.Status != workflowToolSuccessStatus || strings.TrimSpace(result.Response) == "" ||
		!validWorkflowToolDigest(result.ResponseDigest) ||
		!validWorkflowToolDigest(result.RunOutputDigest) ||
		!validWorkflowToolDigest(result.BindingDigest) ||
		strings.TrimSpace(result.WorkflowRunID) == "" ||
		strings.TrimSpace(result.ParentRunID) == "" || strings.TrimSpace(result.ParentActionID) == "" {
		return WorkflowToolResult{}, false
	}
	responseDigest := sha256.Sum256([]byte(strings.TrimSpace(result.Response)))
	if result.ResponseDigest != "sha256:"+hex.EncodeToString(responseDigest[:]) {
		return WorkflowToolResult{}, false
	}
	return result, true
}

func workflowToolRunMatches(result WorkflowToolResult, run WorkflowToolRunEvidence) bool {
	return run.WorkflowRunID == result.WorkflowRunID && run.WorkflowID == result.WorkflowID &&
		run.WorkflowRevisionID == result.WorkflowRevisionID &&
		run.WorkflowRevisionNumber == result.WorkflowRevisionNumber &&
		run.InvocationSource == "runtime" && run.ParentRunID == result.ParentRunID &&
		run.ParentActionID == result.ParentActionID && run.Status == workflowToolSuccessStatus &&
		run.RunOutputDigest == result.RunOutputDigest && !run.FinishedAt.IsZero()
}

func validateWorkflowToolTask(task agentRuntime.TaskSpec) error {
	if !taskHasCriterion(task, WorkflowToolOutputVerifiedCriterion) {
		return fmt.Errorf("workflow tool output criterion is not declared by the task")
	}
	for _, criterion := range task.CompletionCriteria {
		if criterion.Required && criterion.ID != WorkflowToolOutputVerifiedCriterion {
			return fmt.Errorf("workflow tool verifier cannot prove required criterion %q", criterion.ID)
		}
	}
	return nil
}

func equalWorkflowToolSnapshotViews(
	left agentEnvironment.WorkflowToolSnapshotView,
	right agentEnvironment.WorkflowToolSnapshotView,
) bool {
	if len(left.Tools) != len(right.Tools) {
		return false
	}
	for index := range left.Tools {
		if left.Tools[index] != right.Tools[index] {
			return false
		}
	}
	return true
}

func validWorkflowToolDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == sha256.Size
}

func workflowToolEvidenceKey(item agentRuntime.Evidence) string {
	return item.Source + "|" + item.Digest + "|" + item.Reference
}

var _ agentRuntime.EvidenceCollector = WorkflowToolGoalCollector{}
var _ agentRuntime.Verifier = WorkflowToolGoalVerifier{}
