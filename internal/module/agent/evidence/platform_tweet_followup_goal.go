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
	PlatformTweetPriorReferenceCriterion = "prior_platform_tweet_reference_bound"
	PlatformTweetDetailResultCriterion   = "platform_tweet_detail_observed"
	platformTweetReferenceSource         = "dialogue.platform_tweet_refs"
	platformTweetDetailTool              = "get_tweets_by_ids"
)

type PlatformTweetFollowUpGoalCollector struct {
	ExpectedTweetID string
	PriorReference  string
}

func (collector PlatformTweetFollowUpGoalCollector) Collect(
	_ context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	expectedID, reference, err := collector.binding()
	if err != nil {
		return nil, err
	}
	if !taskHasCriterion(request.Task, PlatformTweetPriorReferenceCriterion) ||
		!taskHasCriterion(request.Task, PlatformTweetDetailResultCriterion) {
		return nil, fmt.Errorf("platform tweet follow-up criteria are not declared by the task")
	}

	priorDigest := sha256.Sum256([]byte(reference))
	evidence := []agentRuntime.Evidence{{
		ID:           "platform-prior:" + hex.EncodeToString(priorDigest[:12]),
		Kind:         agentRuntime.EvidenceEnvironmentState,
		Source:       platformTweetReferenceSource,
		CriterionIDs: []string{PlatformTweetPriorReferenceCriterion},
		Digest:       "sha256:" + hex.EncodeToString(priorDigest[:]),
		Reference:    reference,
	}}

	seen := make(map[string]struct{})
	for _, step := range request.Run.Steps {
		for _, observation := range step.Observations {
			item, ok := trustedPlatformTweetDetailObservation(step, observation, expectedID)
			if !ok {
				continue
			}
			digest, itemReference := platformSearchEvidenceIdentity(item)
			key := digest + "|" + itemReference
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
				expectedID,
			)))
			evidence = append(evidence, agentRuntime.Evidence{
				ID:           "platform-detail:" + hex.EncodeToString(idDigest[:12]),
				Kind:         agentRuntime.EvidenceToolObservation,
				Source:       platformTweetDetailTool,
				CriterionIDs: []string{PlatformTweetDetailResultCriterion},
				Digest:       digest,
				Reference:    itemReference,
				StepIndex:    step.Index,
				CapturedAt:   step.FinishedAt,
			})
		}
	}
	return evidence, nil
}

type PlatformTweetFollowUpGoalVerifier struct {
	ExpectedTweetID string
	PriorReference  string
}

func (verifier PlatformTweetFollowUpGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	expectedID, reference, err := (PlatformTweetFollowUpGoalCollector{
		ExpectedTweetID: verifier.ExpectedTweetID,
		PriorReference:  verifier.PriorReference,
	}).binding()
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	if err := validatePlatformTweetFollowUpTask(request.Task); err != nil {
		return agentRuntime.VerificationResult{}, err
	}

	priorDigest := sha256.Sum256([]byte(reference))
	expectedPriorDigest := "sha256:" + hex.EncodeToString(priorDigest[:])
	priorEvidenceIDs := make([]string, 0, 1)
	detailEvidenceIDs := make([]string, 0, 1)
	validDetails := validPlatformTweetDetailEvidence(request.Run, expectedID)
	for _, item := range request.Evidence.Items {
		switch {
		case item.Kind == agentRuntime.EvidenceEnvironmentState &&
			item.Source == platformTweetReferenceSource &&
			item.Reference == reference && item.Digest == expectedPriorDigest &&
			containsString(item.CriterionIDs, PlatformTweetPriorReferenceCriterion):
			priorEvidenceIDs = append(priorEvidenceIDs, item.ID)
		case item.Kind == agentRuntime.EvidenceToolObservation &&
			item.Source == platformTweetDetailTool &&
			containsString(item.CriterionIDs, PlatformTweetDetailResultCriterion):
			if _, ok := validDetails[item.Digest+"|"+item.Reference]; ok {
				detailEvidenceIDs = append(detailEvidenceIDs, item.ID)
			}
		}
	}

	replaceCheck(&base, followUpCheck(
		PlatformTweetPriorReferenceCriterion,
		"platform_tweet_prior_reference_verified",
		"platform_tweet_prior_reference_missing",
		priorEvidenceIDs,
	))
	replaceCheck(&base, followUpCheck(
		PlatformTweetDetailResultCriterion,
		"platform_tweet_detail_verified",
		"platform_tweet_detail_missing",
		detailEvidenceIDs,
	))
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

func (collector PlatformTweetFollowUpGoalCollector) binding() (string, string, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(collector.ExpectedTweetID), 10, 64)
	if err != nil || id == 0 {
		return "", "", fmt.Errorf("expected platform tweet ID is invalid")
	}
	expectedID := strconv.FormatUint(id, 10)
	reference := "/tweets/" + expectedID
	if strings.TrimSpace(collector.PriorReference) != reference {
		return "", "", fmt.Errorf("prior platform tweet reference does not match expected tweet ID")
	}
	return expectedID, reference, nil
}

func validatePlatformTweetFollowUpTask(task agentRuntime.TaskSpec) error {
	for _, criterion := range task.CompletionCriteria {
		if !criterion.Required {
			continue
		}
		if criterion.ID != PlatformTweetPriorReferenceCriterion &&
			criterion.ID != PlatformTweetDetailResultCriterion {
			return fmt.Errorf("platform tweet follow-up verifier cannot prove required criterion %q", criterion.ID)
		}
	}
	if !taskHasCriterion(task, PlatformTweetPriorReferenceCriterion) ||
		!taskHasCriterion(task, PlatformTweetDetailResultCriterion) {
		return fmt.Errorf("platform tweet follow-up task is missing required criteria")
	}
	return nil
}

func followUpCheck(criterionID, passCode, failCode string, evidenceIDs []string) agentRuntime.CheckResult {
	check := agentRuntime.CheckResult{
		CriterionID: criterionID,
		Status:      agentRuntime.VerificationPassed,
		Code:        passCode,
		EvidenceIDs: evidenceIDs,
	}
	if len(evidenceIDs) == 0 {
		check.Status = agentRuntime.VerificationFailed
		check.Code = failCode
	}
	return check
}

func validPlatformTweetDetailEvidence(run agentRuntime.RunResult, expectedID string) map[string]struct{} {
	valid := make(map[string]struct{})
	for _, step := range run.Steps {
		for _, observation := range step.Observations {
			item, ok := trustedPlatformTweetDetailObservation(step, observation, expectedID)
			if !ok {
				continue
			}
			digest, reference := platformSearchEvidenceIdentity(item)
			valid[digest+"|"+reference] = struct{}{}
		}
	}
	return valid
}

func trustedPlatformTweetDetailObservation(
	step agentRuntime.Step,
	observation agentRuntime.Observation,
	expectedID string,
) (PlatformTweetSearchEvidence, bool) {
	if observation.IsError || observation.Name != platformTweetDetailTool ||
		len(observation.StructuredContent) == 0 || len(observation.StructuredContent) > maxGoalStructuredEvidenceSize {
		return PlatformTweetSearchEvidence{}, false
	}
	for _, action := range step.Actions {
		if action.ID != observation.ActionID || action.Type != agentRuntime.ActionToolCall ||
			action.Name != platformTweetDetailTool || !detailActionSelectsTweet(action, expectedID) {
			continue
		}
		var result PlatformTweetDetailResult
		if err := json.Unmarshal(observation.StructuredContent, &result); err != nil ||
			result.Schema != PlatformTweetDetailSchema {
			return PlatformTweetSearchEvidence{}, false
		}
		for _, item := range result.Items {
			if strings.TrimSpace(item.TweetID) == expectedID && strings.TrimSpace(item.Content) != "" {
				item.TweetID = expectedID
				item.Content = strings.TrimSpace(item.Content)
				return item, true
			}
		}
	}
	return PlatformTweetSearchEvidence{}, false
}

func detailActionSelectsTweet(action agentRuntime.Action, expectedID string) bool {
	var arguments struct {
		TweetIDs string `json:"tweet_ids"`
	}
	if err := json.Unmarshal(action.Arguments, &arguments); err != nil {
		return false
	}
	values := strings.Split(arguments.TweetIDs, ",")
	return len(values) == 1 && strings.TrimSpace(values[0]) == expectedID
}
