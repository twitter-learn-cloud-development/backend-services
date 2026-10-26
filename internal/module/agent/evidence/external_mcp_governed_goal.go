package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const ExternalMCPGovernedObservedCriterion = "external_mcp_governed_observed"

// ExternalMCPGovernedGoalCollector accepts successful observations only when
// the tenant-bound connection, schema snapshot and tool policy remain stable
// across the execution. Approval and idempotency are enforced by ToolExecutor;
// this collector binds their resulting observation to the current catalog.
type ExternalMCPGovernedGoalCollector struct{}

func (ExternalMCPGovernedGoalCollector) Collect(
	_ context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	if !taskHasCriterion(request.Task, ExternalMCPGovernedObservedCriterion) {
		return nil, fmt.Errorf("external MCP governed criterion is not declared by the task")
	}
	candidates, err := externalMCPGovernedEvidenceCandidates(
		request.Task, request.Run, request.Before, request.After,
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

type ExternalMCPGovernedGoalVerifier struct{}

func (ExternalMCPGovernedGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	if !taskHasCriterion(request.Task, ExternalMCPGovernedObservedCriterion) {
		return agentRuntime.VerificationResult{}, fmt.Errorf("external MCP governed criterion is not declared by the task")
	}
	valid, err := externalMCPGovernedEvidenceCandidates(
		request.Task, request.Run, request.Before, request.After,
	)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	matched := make([]string, 0)
	for _, item := range request.Evidence.Items {
		if item.Kind != agentRuntime.EvidenceToolObservation ||
			!containsString(item.CriterionIDs, ExternalMCPGovernedObservedCriterion) {
			continue
		}
		if _, ok := valid[externalMCPEvidenceKey(item)]; ok {
			matched = append(matched, item.ID)
		}
	}
	check := agentRuntime.CheckResult{
		CriterionID: ExternalMCPGovernedObservedCriterion,
		Status:      agentRuntime.VerificationPassed,
		Code:        "external_mcp_governed_evidence_verified",
		EvidenceIDs: matched,
	}
	if len(matched) == 0 {
		check.Status = agentRuntime.VerificationFailed
		check.Code = "external_mcp_governed_evidence_missing"
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

func externalMCPGovernedEvidenceCandidates(
	task agentRuntime.TaskSpec,
	run agentRuntime.RunResult,
	beforeSnapshot *agentRuntime.EnvironmentSnapshot,
	afterSnapshot *agentRuntime.EnvironmentSnapshot,
) (map[string]agentRuntime.Evidence, error) {
	before, err := agentEnvironment.DecodeExternalMCPSnapshot(
		beforeSnapshot, agentRuntime.SnapshotPhaseBefore, run.Context.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("decode external MCP before snapshot: %w", err)
	}
	after, err := agentEnvironment.DecodeExternalMCPSnapshot(
		afterSnapshot, agentRuntime.SnapshotPhaseAfter, run.Context.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("decode external MCP after snapshot: %w", err)
	}
	if beforeSnapshot.Digest != afterSnapshot.Digest || !equalExternalMCPViews(before, after) {
		return map[string]agentRuntime.Evidence{}, nil
	}
	bindings := make(map[string]agentEnvironment.ExternalMCPSnapshotToolView, len(before.Tools))
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
			resultDigest := externalMCPObservationDigest(observation)
			if resultDigest == "" {
				continue
			}
			canonical, _ := json.Marshal(struct {
				BindingDigest      string                    `json:"binding_digest"`
				ConnectionRevision int64                     `json:"connection_revision"`
				Category           agentRuntime.ToolCategory `json:"category"`
				RequiresApproval   bool                      `json:"requires_approval"`
				ResultDigest       string                    `json:"result_digest"`
			}{
				binding.BindingDigest, binding.ConnectionRevision, binding.Category,
				binding.RequiresApproval, resultDigest,
			})
			digest := sha256.Sum256(canonical)
			toolDigest := sha256.Sum256([]byte(observation.Name))
			item := agentRuntime.Evidence{
				ID:   "external-mcp-governed:" + hex.EncodeToString(digest[:12]),
				Kind: agentRuntime.EvidenceToolObservation, Source: observation.Name,
				CriterionIDs: []string{ExternalMCPGovernedObservedCriterion},
				Digest:       "sha256:" + hex.EncodeToString(digest[:]),
				Reference:    beforeSnapshot.Reference + "/tool/sha256/" + hex.EncodeToString(toolDigest[:]),
				StepIndex:    step.Index, CapturedAt: step.FinishedAt,
			}
			items[externalMCPEvidenceKey(item)] = item
		}
	}
	return items, nil
}

var _ agentRuntime.EvidenceCollector = ExternalMCPGovernedGoalCollector{}
var _ agentRuntime.Verifier = ExternalMCPGovernedGoalVerifier{}
