package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	PlatformTweetPublishSchema       = "platform.tweet_publish.v1"
	TweetPublishCommittedCriterion   = "tweet_publish_committed"
	TweetPublishIdempotentCriterion  = "tweet_publish_idempotent"
	maxTweetPublishStructuredContent = 64 << 10
)

type PlatformTweetPublishResult struct {
	Schema  string `json:"schema"`
	TweetID string `json:"tweet_id"`
}

func NewPlatformTweetPublishResult(tweetID uint64) PlatformTweetPublishResult {
	return PlatformTweetPublishResult{
		Schema: PlatformTweetPublishSchema, TweetID: strconv.FormatUint(tweetID, 10),
	}
}

// TweetPublishGoalCollector emits evidence only after the structured tool
// result is visible in the authoritative after snapshot.
type TweetPublishGoalCollector struct{}

func (TweetPublishGoalCollector) Collect(
	_ context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	criteria, err := validateTweetPublishTask(request.Task)
	if err != nil {
		return nil, err
	}
	before, after, err := decodeTweetPublishSnapshots(request.Before, request.After, request.Run.Context.UserID)
	if err != nil {
		return nil, err
	}
	results := trustedTweetPublishResults(request.Run)
	if len(results) != 1 {
		return nil, nil
	}
	result := results[0]
	evidence := make([]agentRuntime.Evidence, 0, len(criteria))
	for _, criterionID := range criteria {
		if !tweetPublishTransitionValid(criterionID, before, after, result.Reference) {
			continue
		}
		identity := sha256.Sum256([]byte(fmt.Sprintf(
			"%s|%d|%d|%s|%s|%s",
			request.Run.Context.RunID, request.Attempt, result.StepIndex,
			result.ActionID, result.Reference, criterionID,
		)))
		evidence = append(evidence, agentRuntime.Evidence{
			ID:           "tweet-publish:" + hex.EncodeToString(identity[:12]),
			Kind:         agentRuntime.EvidenceEnvironmentState,
			Source:       agentEnvironment.TweetPublishToolName,
			CriterionIDs: []string{criterionID},
			Digest:       request.After.Digest,
			Reference:    result.Reference,
			StepIndex:    result.StepIndex,
			CapturedAt:   result.CapturedAt,
		})
	}
	return evidence, nil
}

type TweetPublishGoalVerifier struct{}

func (TweetPublishGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	criteria, err := validateTweetPublishTask(request.Task)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	before, after, err := decodeTweetPublishSnapshots(request.Before, request.After, request.Run.Context.UserID)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	if request.After.CapturedAt.Before(request.Before.CapturedAt) {
		return agentRuntime.VerificationResult{}, fmt.Errorf("tweet publish snapshots are out of order")
	}
	results := trustedTweetPublishResults(request.Run)
	for _, criterionID := range criteria {
		matched := make([]string, 0)
		if len(results) == 1 && tweetPublishTransitionValid(criterionID, before, after, results[0].Reference) {
			for _, item := range request.Evidence.Items {
				if item.Kind == agentRuntime.EvidenceEnvironmentState &&
					item.Source == agentEnvironment.TweetPublishToolName &&
					item.Digest == request.After.Digest && item.Reference == results[0].Reference &&
					containsString(item.CriterionIDs, criterionID) {
					matched = append(matched, item.ID)
				}
			}
		}
		check := agentRuntime.CheckResult{
			CriterionID: criterionID, Status: agentRuntime.VerificationPassed,
			Code: "tweet_publish_state_verified", EvidenceIDs: matched,
		}
		if len(matched) == 0 {
			check.Status = agentRuntime.VerificationFailed
			check.Code = "tweet_publish_state_missing"
		}
		replaceCheck(&base, check)
	}
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

func trustedTweetPublishResults(run agentRuntime.RunResult) []tweetPublishObservation {
	results := make([]tweetPublishObservation, 0)
	seen := make(map[string]struct{})
	for _, step := range run.Steps {
		for _, observation := range step.Observations {
			if observation.IsError || observation.Name != agentEnvironment.TweetPublishToolName {
				continue
			}
			trusted := false
			for _, action := range step.Actions {
				if action.ID == observation.ActionID && action.Type == agentRuntime.ActionToolCall && action.Name == observation.Name {
					trusted = true
					break
				}
			}
			if !trusted {
				continue
			}
			result, ok := decodeTweetPublishResult(observation.StructuredContent)
			if !ok {
				continue
			}
			reference := agentEnvironment.TweetReference(result)
			key := observation.ActionID + "|" + reference
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			results = append(results, tweetPublishObservation{
				ActionID: observation.ActionID, Reference: reference,
				StepIndex: step.Index, CapturedAt: step.FinishedAt,
			})
		}
	}
	return results
}

type tweetPublishObservation struct {
	ActionID   string
	Reference  string
	StepIndex  int
	CapturedAt time.Time
}

func decodeTweetPublishResult(raw json.RawMessage) (uint64, bool) {
	if len(raw) == 0 || len(raw) > maxTweetPublishStructuredContent || !json.Valid(raw) {
		return 0, false
	}
	var result PlatformTweetPublishResult
	if err := json.Unmarshal(raw, &result); err != nil || result.Schema != PlatformTweetPublishSchema {
		return 0, false
	}
	id, err := strconv.ParseUint(strings.TrimSpace(result.TweetID), 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != result.TweetID {
		return 0, false
	}
	return id, true
}

func validateTweetPublishTask(task agentRuntime.TaskSpec) ([]string, error) {
	criteria := make([]string, 0, 2)
	for _, criterion := range task.CompletionCriteria {
		id := strings.TrimSpace(criterion.ID)
		switch id {
		case TweetPublishCommittedCriterion, TweetPublishIdempotentCriterion:
			criteria = append(criteria, id)
		default:
			if criterion.Required {
				return nil, fmt.Errorf("tweet publish verifier cannot prove required criterion %q", criterion.ID)
			}
		}
	}
	if len(criteria) == 0 {
		return nil, fmt.Errorf("tweet publish task has no supported criterion")
	}
	return criteria, nil
}

func decodeTweetPublishSnapshots(
	beforeSnapshot *agentRuntime.EnvironmentSnapshot,
	afterSnapshot *agentRuntime.EnvironmentSnapshot,
	userID uint64,
) (agentEnvironment.TweetWriteSnapshotView, agentEnvironment.TweetWriteSnapshotView, error) {
	before, err := agentEnvironment.DecodeTweetWriteSnapshot(beforeSnapshot, agentRuntime.SnapshotPhaseBefore, userID)
	if err != nil {
		return agentEnvironment.TweetWriteSnapshotView{}, agentEnvironment.TweetWriteSnapshotView{}, fmt.Errorf("decode tweet publish before snapshot: %w", err)
	}
	after, err := agentEnvironment.DecodeTweetWriteSnapshot(afterSnapshot, agentRuntime.SnapshotPhaseAfter, userID)
	if err != nil {
		return agentEnvironment.TweetWriteSnapshotView{}, agentEnvironment.TweetWriteSnapshotView{}, fmt.Errorf("decode tweet publish after snapshot: %w", err)
	}
	return before, after, nil
}

func tweetPublishTransitionValid(
	criterionID string,
	before agentEnvironment.TweetWriteSnapshotView,
	after agentEnvironment.TweetWriteSnapshotView,
	target string,
) bool {
	beforeSet := stringSet(before.TweetReferences)
	afterSet := stringSet(after.TweetReferences)
	if _, present := afterSet[target]; !present {
		return false
	}
	switch criterionID {
	case TweetPublishCommittedCriterion:
		if _, replayed := beforeSet[target]; replayed {
			return equalStringSets(beforeSet, afterSet) && before.HasMore == after.HasMore
		}
		added := 0
		for reference := range afterSet {
			if _, existed := beforeSet[reference]; !existed {
				if reference != target {
					return false
				}
				added++
			}
		}
		return added == 1
	case TweetPublishIdempotentCriterion:
		_, existed := beforeSet[target]
		return existed && equalStringSets(beforeSet, afterSet) && before.HasMore == after.HasMore
	default:
		return false
	}
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func equalStringSets(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if _, exists := right[value]; !exists {
			return false
		}
	}
	return true
}

var _ agentRuntime.EvidenceCollector = TweetPublishGoalCollector{}
var _ agentRuntime.Verifier = TweetPublishGoalVerifier{}
