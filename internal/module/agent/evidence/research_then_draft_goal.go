package evidence

import (
	"context"
	"fmt"
	"strings"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	ResearchThenDraftOrderCriterion = "research_before_draft"

	ResearchThenDraftOrderVerifiedCode = "research_before_draft_verified"
	ResearchThenDraftOrderMissingCode  = "research_before_draft_missing"
)

// ResearchThenDraftGoalCollector reuses grounded-draft evidence and adds an
// order claim only when trusted research completed before the terminal draft.
// It never stores source bodies or draft text in the evidence ledger.
type ResearchThenDraftGoalCollector struct {
	Source GroundedDraftSource
}

func (collector ResearchThenDraftGoalCollector) Collect(
	ctx context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	if err := validateResearchThenDraftTask(request.Task, collector.Source); err != nil {
		return nil, err
	}

	groundedRequest := request
	groundedRequest.Task = researchThenDraftGroundedTask(request.Task)
	items, err := (GroundedDraftGoalCollector{Source: collector.Source}).Collect(ctx, groundedRequest)
	if err != nil {
		return nil, err
	}
	finalStep, ok := researchThenDraftFinalStep(request.Run)
	if !ok {
		return items, nil
	}
	records, err := groundedDraftSourceRecords(request.Run, collector.Source)
	if err != nil {
		return nil, err
	}
	orderedSources := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.StepIndex >= finalStep {
			continue
		}
		orderedSources[researchThenDraftSourceKey(
			record.Source, record.Digest, record.Reference, record.StepIndex,
		)] = struct{}{}
	}
	for index := range items {
		item := &items[index]
		if item.Kind != agentRuntime.EvidenceToolObservation {
			continue
		}
		key := researchThenDraftSourceKey(item.Source, item.Digest, item.Reference, item.StepIndex)
		if _, valid := orderedSources[key]; valid {
			item.CriterionIDs = groundedDraftUniqueStrings(append(
				item.CriterionIDs, ResearchThenDraftOrderCriterion,
			))
		}
	}
	return items, nil
}

// ResearchThenDraftGoalVerifier proves both grounding and execution order. A
// final answer alone cannot prove that research happened before drafting.
type ResearchThenDraftGoalVerifier struct {
	Source GroundedDraftSource
}

func (verifier ResearchThenDraftGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	if err := validateResearchThenDraftTask(request.Task, verifier.Source); err != nil {
		return agentRuntime.VerificationResult{}, err
	}

	groundedRequest := request
	groundedRequest.Task = researchThenDraftGroundedTask(request.Task)
	result, err := (GroundedDraftGoalVerifier{Source: verifier.Source}).Verify(ctx, groundedRequest)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}

	orderCheck := agentRuntime.CheckResult{
		CriterionID: ResearchThenDraftOrderCriterion,
		Status:      agentRuntime.VerificationFailed,
		Code:        ResearchThenDraftOrderMissingCode,
	}
	finalStep, hasFinalStep := researchThenDraftFinalStep(request.Run)
	records, err := groundedDraftSourceRecords(request.Run, verifier.Source)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	validSources := make(map[string]struct{}, len(records))
	if hasFinalStep {
		for _, record := range records {
			if record.StepIndex >= finalStep {
				continue
			}
			validSources[researchThenDraftSourceKey(
				record.Source, record.Digest, record.Reference, record.StepIndex,
			)] = struct{}{}
		}
	}
	for _, item := range request.Evidence.Items {
		if item.Kind != agentRuntime.EvidenceToolObservation ||
			!containsString(item.CriterionIDs, ResearchThenDraftOrderCriterion) {
			continue
		}
		key := researchThenDraftSourceKey(item.Source, item.Digest, item.Reference, item.StepIndex)
		if _, valid := validSources[key]; valid {
			orderCheck.EvidenceIDs = append(orderCheck.EvidenceIDs, item.ID)
		}
	}
	orderCheck.EvidenceIDs = groundedDraftUniqueStrings(orderCheck.EvidenceIDs)
	if len(orderCheck.EvidenceIDs) > 0 {
		orderCheck.Status = agentRuntime.VerificationPassed
		orderCheck.Code = ResearchThenDraftOrderVerifiedCode
	}
	replaceCheck(&result, orderCheck)
	result.MissingEvidence = missingRequiredCriteria(request.Task, result.Checks)
	if len(result.MissingEvidence) == 0 {
		result.Status = agentRuntime.VerificationPassed
		result.Retryable = false
	} else {
		result.Status = agentRuntime.VerificationFailed
		result.Retryable = request.RepairAttempts < request.Task.MaxRepairAttempts
	}
	return result, nil
}

func researchThenDraftFinalStep(run agentRuntime.RunResult) (int, bool) {
	answer := strings.TrimSpace(run.FinalAnswer)
	if run.Status != agentRuntime.RunStatusCompleted || answer == "" {
		return 0, false
	}
	for _, step := range run.Steps {
		for _, action := range step.Actions {
			if action.Type == agentRuntime.ActionFinalAnswer &&
				strings.TrimSpace(action.Content) == answer {
				return step.Index, true
			}
		}
	}
	return 0, false
}

func researchThenDraftSourceKey(source, digest, reference string, stepIndex int) string {
	return fmt.Sprintf(
		"%s|%s|%s|%d",
		strings.TrimSpace(source), strings.TrimSpace(digest), strings.TrimSpace(reference), stepIndex,
	)
}

func researchThenDraftGroundedTask(task agentRuntime.TaskSpec) agentRuntime.TaskSpec {
	result := task
	result.CompletionCriteria = make([]agentRuntime.CompletionCriterion, 0, 2)
	for _, criterion := range task.CompletionCriteria {
		if criterion.ID == GroundedDraftSourcesCriterion ||
			criterion.ID == GroundedDraftArtifactCriterion {
			result.CompletionCriteria = append(result.CompletionCriteria, criterion)
		}
	}
	return result
}

func validateResearchThenDraftTask(task agentRuntime.TaskSpec, source GroundedDraftSource) error {
	if err := task.Validate(); err != nil {
		return err
	}
	if source != GroundedDraftSourcePlatform && source != GroundedDraftSourceWeb {
		return fmt.Errorf("research then draft source %q is unsupported", source)
	}
	if !taskHasCriterion(task, GroundedDraftSourcesCriterion) ||
		!taskHasCriterion(task, GroundedDraftArtifactCriterion) ||
		!taskHasCriterion(task, ResearchThenDraftOrderCriterion) {
		return fmt.Errorf("research then draft task requires source, order, and artifact criteria")
	}
	for _, criterion := range task.CompletionCriteria {
		if !criterion.Required {
			continue
		}
		switch criterion.ID {
		case GroundedDraftSourcesCriterion,
			GroundedDraftArtifactCriterion,
			ResearchThenDraftOrderCriterion:
		default:
			return fmt.Errorf("research then draft verifier cannot prove required criterion %q", criterion.ID)
		}
	}
	return nil
}
