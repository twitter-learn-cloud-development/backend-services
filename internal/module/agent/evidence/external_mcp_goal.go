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

const ExternalMCPReadObservedCriterion = "external_mcp_read_observed"

// ExternalMCPReadGoalCollector accepts only successful, paired read
// observations that were executed against the same tenant-bound catalog
// snapshot before and after the run.
type ExternalMCPReadGoalCollector struct{}

func (ExternalMCPReadGoalCollector) Collect(
	_ context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	if !taskHasCriterion(request.Task, ExternalMCPReadObservedCriterion) {
		return nil, fmt.Errorf("external MCP read criterion is not declared by the task")
	}
	candidates, err := externalMCPReadEvidenceCandidates(request.Task, request.Run, request.Before, request.After)
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

// ExternalMCPReadGoalVerifier recomputes every accepted evidence identity from
// the Runtime action/observation pair and the authenticated environment
// snapshots. Caller-supplied ledger claims cannot satisfy the criterion alone.
type ExternalMCPReadGoalVerifier struct{}

func (ExternalMCPReadGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	if !taskHasCriterion(request.Task, ExternalMCPReadObservedCriterion) {
		return agentRuntime.VerificationResult{}, fmt.Errorf("external MCP read criterion is not declared by the task")
	}
	valid, err := externalMCPReadEvidenceCandidates(request.Task, request.Run, request.Before, request.After)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	matched := make([]string, 0)
	for _, item := range request.Evidence.Items {
		if item.Kind != agentRuntime.EvidenceToolObservation ||
			!containsString(item.CriterionIDs, ExternalMCPReadObservedCriterion) {
			continue
		}
		key := externalMCPEvidenceKey(item)
		if _, ok := valid[key]; ok {
			matched = append(matched, item.ID)
		}
	}
	check := agentRuntime.CheckResult{
		CriterionID: ExternalMCPReadObservedCriterion,
		Status:      agentRuntime.VerificationPassed,
		Code:        "external_mcp_read_evidence_verified",
		EvidenceIDs: matched,
	}
	if len(matched) == 0 {
		check.Status = agentRuntime.VerificationFailed
		check.Code = "external_mcp_read_evidence_missing"
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

func externalMCPReadEvidenceCandidates(
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
		if tool.Category == agentRuntime.ToolCategoryRead && !tool.RequiresApproval {
			bindings[tool.Name] = tool
		}
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
				BindingDigest      string `json:"binding_digest"`
				ConnectionRevision int64  `json:"connection_revision"`
				ResultDigest       string `json:"result_digest"`
			}{binding.BindingDigest, binding.ConnectionRevision, resultDigest})
			digest := sha256.Sum256(canonical)
			toolDigest := sha256.Sum256([]byte(observation.Name))
			item := agentRuntime.Evidence{
				ID:   "external-mcp-read:" + hex.EncodeToString(digest[:12]),
				Kind: agentRuntime.EvidenceToolObservation, Source: observation.Name,
				CriterionIDs: []string{ExternalMCPReadObservedCriterion},
				Digest:       "sha256:" + hex.EncodeToString(digest[:]),
				Reference:    beforeSnapshot.Reference + "/tool/sha256/" + hex.EncodeToString(toolDigest[:]),
				StepIndex:    step.Index, CapturedAt: step.FinishedAt,
			}
			items[externalMCPEvidenceKey(item)] = item
		}
	}
	return items, nil
}

func externalMCPObservationDigest(observation agentRuntime.Observation) string {
	if len(observation.StructuredContent) > 0 && len(observation.StructuredContent) <= maxGoalStructuredEvidenceSize {
		var value interface{}
		if json.Unmarshal(observation.StructuredContent, &value) == nil {
			canonical, err := json.Marshal(value)
			if err == nil {
				digest := sha256.Sum256(canonical)
				return "sha256:" + hex.EncodeToString(digest[:])
			}
		}
	}
	content := strings.TrimSpace(observation.Content)
	if content == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func equalExternalMCPViews(left, right agentEnvironment.ExternalMCPSnapshotView) bool {
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

func externalMCPEvidenceKey(item agentRuntime.Evidence) string {
	return item.Source + "|" + item.Digest + "|" + item.Reference
}

var _ agentRuntime.EvidenceCollector = ExternalMCPReadGoalCollector{}
var _ agentRuntime.Verifier = ExternalMCPReadGoalVerifier{}
