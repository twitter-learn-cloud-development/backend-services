package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	PlatformSearchResultCriterion = "platform_search_results_observed"
	maxGoalStructuredEvidenceSize = 1 << 20
)

var platformSearchToolNames = map[string]struct{}{
	"hybrid_search_tweets":      {},
	"search_tweets_by_semantic": {},
}

// PlatformSearchGoalCollector converts only trusted first-party search schemas
// into digest-only goal evidence. Display text is intentionally ignored.
type PlatformSearchGoalCollector struct {
	CriterionID string
}

func (collector PlatformSearchGoalCollector) Collect(
	_ context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	criterionID := collector.criterionID()
	if !taskHasCriterion(request.Task, criterionID) {
		return nil, fmt.Errorf("platform search criterion %q is not declared by the task", criterionID)
	}

	items := make([]agentRuntime.Evidence, 0)
	seen := make(map[string]struct{})
	for _, step := range request.Run.Steps {
		for _, observation := range step.Observations {
			if observation.IsError ||
				!trustedPlatformSearchObservation(step, observation) {
				continue
			}
			for _, item := range decodeGoalPlatformSearchItems(observation.StructuredContent) {
				digest, reference := platformSearchEvidenceIdentity(item)
				key := observation.Name + "|" + digest + "|" + reference
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				idDigest := sha256.Sum256([]byte(fmt.Sprintf(
					"%s|%d|%d|%s|%s",
					request.Run.Context.RunID,
					request.Attempt,
					step.Index,
					observation.ActionID,
					item.TweetID,
				)))
				items = append(items, agentRuntime.Evidence{
					ID:           "platform-search:" + hex.EncodeToString(idDigest[:12]),
					Kind:         agentRuntime.EvidenceToolObservation,
					Source:       observation.Name,
					CriterionIDs: []string{criterionID},
					Digest:       digest,
					Reference:    reference,
					StepIndex:    step.Index,
					CapturedAt:   step.FinishedAt,
				})
			}
		}
	}
	return items, nil
}

// PlatformSearchGoalVerifier proves platform-search evidence against the
// structured observations in the cumulative Runtime result. A caller-provided
// ledger entry cannot pass merely by claiming the right criterion.
type PlatformSearchGoalVerifier struct {
	CriterionID string
}

func (verifier PlatformSearchGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	criterionID := verifier.criterionID()
	if !taskHasCriterion(request.Task, criterionID) {
		return agentRuntime.VerificationResult{}, fmt.Errorf(
			"platform search criterion %q is not declared by the task",
			criterionID,
		)
	}
	if err := validatePlatformSearchVerifierTask(request.Task, criterionID); err != nil {
		return agentRuntime.VerificationResult{}, err
	}

	valid := validPlatformSearchEvidence(request.Run)
	matched := make([]string, 0)
	for _, item := range request.Evidence.Items {
		if item.Kind != agentRuntime.EvidenceToolObservation ||
			!containsString(item.CriterionIDs, criterionID) {
			continue
		}
		key := item.Source + "|" + item.Digest + "|" + item.Reference
		if _, ok := valid[key]; ok {
			matched = append(matched, item.ID)
		}
	}

	check := agentRuntime.CheckResult{
		CriterionID: criterionID,
		Status:      agentRuntime.VerificationPassed,
		Code:        "platform_search_evidence_verified",
		EvidenceIDs: matched,
	}
	if len(matched) == 0 {
		check.Status = agentRuntime.VerificationFailed
		check.Code = "platform_search_evidence_missing"
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

func (collector PlatformSearchGoalCollector) criterionID() string {
	if value := strings.TrimSpace(collector.CriterionID); value != "" {
		return value
	}
	return PlatformSearchResultCriterion
}

func (verifier PlatformSearchGoalVerifier) criterionID() string {
	if value := strings.TrimSpace(verifier.CriterionID); value != "" {
		return value
	}
	return PlatformSearchResultCriterion
}

func validPlatformSearchEvidence(run agentRuntime.RunResult) map[string]struct{} {
	valid := make(map[string]struct{})
	for _, step := range run.Steps {
		for _, observation := range step.Observations {
			if observation.IsError ||
				!trustedPlatformSearchObservation(step, observation) {
				continue
			}
			for _, item := range decodeGoalPlatformSearchItems(observation.StructuredContent) {
				digest, reference := platformSearchEvidenceIdentity(item)
				valid[observation.Name+"|"+digest+"|"+reference] = struct{}{}
			}
		}
	}
	return valid
}

func trustedPlatformSearchObservation(
	step agentRuntime.Step,
	observation agentRuntime.Observation,
) bool {
	if _, ok := platformSearchToolNames[strings.TrimSpace(observation.Name)]; !ok {
		return false
	}
	for _, action := range step.Actions {
		if action.ID == observation.ActionID &&
			action.Type == agentRuntime.ActionToolCall &&
			action.Name == observation.Name {
			return true
		}
	}
	return false
}

func decodeGoalPlatformSearchItems(raw json.RawMessage) []PlatformTweetSearchEvidence {
	if len(raw) == 0 || len(raw) > maxGoalStructuredEvidenceSize {
		return nil
	}
	var result PlatformTweetSearchResult
	if err := json.Unmarshal(raw, &result); err != nil ||
		result.Schema != PlatformTweetSearchSchema {
		return nil
	}
	items := make([]PlatformTweetSearchEvidence, 0, len(result.Items))
	seen := make(map[string]struct{})
	for _, item := range result.Items {
		id, err := strconv.ParseUint(strings.TrimSpace(item.TweetID), 10, 64)
		if err != nil || id == 0 {
			continue
		}
		item.TweetID = strconv.FormatUint(id, 10)
		item.Content = strings.TrimSpace(item.Content)
		if _, exists := seen[item.TweetID]; exists {
			continue
		}
		seen[item.TweetID] = struct{}{}
		items = append(items, item)
	}
	return items
}

func platformSearchEvidenceIdentity(item PlatformTweetSearchEvidence) (string, string) {
	canonical, _ := json.Marshal(item)
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), "/tweets/" + item.TweetID
}

func replaceCheck(result *agentRuntime.VerificationResult, replacement agentRuntime.CheckResult) {
	for index := range result.Checks {
		if result.Checks[index].CriterionID == replacement.CriterionID {
			result.Checks[index] = replacement
			return
		}
	}
	result.Checks = append(result.Checks, replacement)
}

func missingRequiredCriteria(
	task agentRuntime.TaskSpec,
	checks []agentRuntime.CheckResult,
) []string {
	status := make(map[string]agentRuntime.VerificationStatus, len(checks))
	for _, check := range checks {
		status[check.CriterionID] = check.Status
	}
	missing := make([]string, 0)
	for _, criterion := range task.CompletionCriteria {
		if criterion.Required && status[criterion.ID] != agentRuntime.VerificationPassed {
			missing = append(missing, criterion.ID)
		}
	}
	return missing
}

func taskHasCriterion(task agentRuntime.TaskSpec, criterionID string) bool {
	for _, criterion := range task.CompletionCriteria {
		if strings.TrimSpace(criterion.ID) == criterionID {
			return true
		}
	}
	return false
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == expected {
			return true
		}
	}
	return false
}
