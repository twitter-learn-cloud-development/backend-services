package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	GroundedDraftSourcesCriterion  = "grounded_draft_sources_observed"
	GroundedDraftArtifactCriterion = "grounded_draft_artifact_produced"
	GroundedDraftArtifactType      = "content.draft"

	GroundedDraftSourcesVerifiedCode  = "grounded_draft_sources_verified"
	GroundedDraftSourcesMissingCode   = "grounded_draft_sources_missing"
	GroundedDraftArtifactVerifiedCode = "grounded_draft_artifact_verified"
	GroundedDraftArtifactMissingCode  = "grounded_draft_artifact_missing"
	GroundedDraftCitationMissingCode  = "grounded_draft_citation_missing"
	GroundedDraftCitationInvalidCode  = "grounded_draft_citation_invalid"
)

type GroundedDraftSource string

const (
	GroundedDraftSourcePlatform GroundedDraftSource = "platform"
	GroundedDraftSourceWeb      GroundedDraftSource = "web"
)

type groundedDraftSourceRecord struct {
	Source     string
	Digest     string
	Reference  string
	StepIndex  int
	ActionID   string
	CapturedAt time.Time
}

// GroundedDraftGoalCollector projects trusted source observations and the
// final draft into a digest/reference-only ledger. Draft text and source bodies
// remain in the owning Runtime result.
type GroundedDraftGoalCollector struct {
	Source GroundedDraftSource
}

func (collector GroundedDraftGoalCollector) Collect(
	ctx context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	if err := validateGroundedDraftTask(request.Task, collector.Source); err != nil {
		return nil, err
	}
	records, err := groundedDraftSourceRecords(request.Run, collector.Source)
	if err != nil {
		return nil, err
	}
	items := make([]agentRuntime.Evidence, 0, len(records)+1)
	for _, record := range records {
		identity := sha256.Sum256([]byte(fmt.Sprintf(
			"%s|%d|%d|%s|%s|%s",
			request.Run.Context.RunID,
			request.Attempt,
			record.StepIndex,
			record.ActionID,
			record.Source,
			record.Reference,
		)))
		items = append(items, agentRuntime.Evidence{
			ID:           "grounded-draft-source:" + hex.EncodeToString(identity[:12]),
			Kind:         agentRuntime.EvidenceToolObservation,
			Source:       record.Source,
			CriterionIDs: []string{GroundedDraftSourcesCriterion},
			Digest:       record.Digest,
			Reference:    record.Reference,
			StepIndex:    record.StepIndex,
			CapturedAt:   record.CapturedAt,
		})
	}
	artifact, err := (agentRuntime.FinalAnswerArtifactEvidenceCollector{
		ArtifactType: GroundedDraftArtifactType,
		CriterionIDs: []string{GroundedDraftArtifactCriterion},
	}).Collect(ctx, request)
	if err != nil {
		return nil, err
	}
	return append(items, artifact...), nil
}

// GroundedDraftGoalVerifier requires a real draft artifact and at least one
// nearby exact source marker that resolves to trusted structured evidence.
type GroundedDraftGoalVerifier struct {
	Source GroundedDraftSource
}

func (verifier GroundedDraftGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	if err := validateGroundedDraftTask(request.Task, verifier.Source); err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	records, err := groundedDraftSourceRecords(request.Run, verifier.Source)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	validSources := make(map[string]groundedDraftSourceRecord, len(records))
	for _, record := range records {
		validSources[groundedDraftSourceKey(record.Source, record.Digest, record.Reference)] = record
	}

	sourceEvidenceByReference := make(map[string][]string)
	allSourceEvidence := make([]string, 0)
	artifactEvidence := make([]string, 0, 1)
	answerDigest := groundedDraftAnswerDigest(request.Run.FinalAnswer)
	artifactPrefix := "agent-run://" + strings.TrimSpace(request.Run.Context.RunID) + "/attempt/"
	for _, item := range request.Evidence.Items {
		switch {
		case item.Kind == agentRuntime.EvidenceToolObservation &&
			containsString(item.CriterionIDs, GroundedDraftSourcesCriterion):
			if _, ok := validSources[groundedDraftSourceKey(item.Source, item.Digest, item.Reference)]; !ok {
				continue
			}
			sourceEvidenceByReference[item.Reference] = append(sourceEvidenceByReference[item.Reference], item.ID)
			allSourceEvidence = append(allSourceEvidence, item.ID)
		case item.Kind == agentRuntime.EvidenceArtifact &&
			item.Source == GroundedDraftArtifactType &&
			containsString(item.CriterionIDs, GroundedDraftArtifactCriterion) &&
			item.Digest == answerDigest &&
			validGroundedDraftArtifactReference(item.Reference, artifactPrefix):
			artifactEvidence = append(artifactEvidence, item.ID)
		}
	}
	allSourceEvidence = groundedDraftUniqueStrings(allSourceEvidence)
	artifactEvidence = groundedDraftUniqueStrings(artifactEvidence)

	sourceCheck := agentRuntime.CheckResult{
		CriterionID: GroundedDraftSourcesCriterion,
		Status:      agentRuntime.VerificationPassed,
		Code:        GroundedDraftSourcesVerifiedCode,
		EvidenceIDs: allSourceEvidence,
	}
	if len(allSourceEvidence) == 0 {
		sourceCheck.Status = agentRuntime.VerificationFailed
		sourceCheck.Code = GroundedDraftSourcesMissingCode
	}
	replaceCheck(&base, sourceCheck)

	artifactCheck := groundedDraftArtifactCheck(
		request.Run.FinalAnswer,
		verifier.Source,
		sourceEvidenceByReference,
		artifactEvidence,
	)
	replaceCheck(&base, artifactCheck)
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

func groundedDraftArtifactCheck(
	answer string,
	source GroundedDraftSource,
	sourceEvidenceByReference map[string][]string,
	artifactEvidence []string,
) agentRuntime.CheckResult {
	check := agentRuntime.CheckResult{
		CriterionID: GroundedDraftArtifactCriterion,
		Status:      agentRuntime.VerificationFailed,
		Code:        GroundedDraftArtifactMissingCode,
	}
	if len(artifactEvidence) != 1 || strings.TrimSpace(answer) == "" {
		return check
	}
	markers := groundedDraftReferenceMarkers(answer)
	if len(markers) == 0 {
		check.Code = GroundedDraftCitationMissingCode
		check.EvidenceIDs = append([]string(nil), artifactEvidence...)
		return check
	}

	linkedEvidence := make([]string, 0)
	for _, marker := range markers {
		reference, kind, valid := canonicalGroundedDraftReference(marker.Value)
		if !valid || kind != source || !groundedDraftClaimPrecedesMarker(answer, marker.Start) {
			check.Code = GroundedDraftCitationInvalidCode
			check.EvidenceIDs = append([]string(nil), artifactEvidence...)
			return check
		}
		ids := sourceEvidenceByReference[reference]
		if len(ids) == 0 {
			check.Code = GroundedDraftCitationInvalidCode
			check.EvidenceIDs = append([]string(nil), artifactEvidence...)
			return check
		}
		linkedEvidence = append(linkedEvidence, ids...)
	}
	linkedEvidence = groundedDraftUniqueStrings(linkedEvidence)
	if len(linkedEvidence) == 0 {
		check.Code = GroundedDraftCitationMissingCode
		check.EvidenceIDs = append([]string(nil), artifactEvidence...)
		return check
	}
	check.Status = agentRuntime.VerificationPassed
	check.Code = GroundedDraftArtifactVerifiedCode
	check.EvidenceIDs = groundedDraftUniqueStrings(append(artifactEvidence, linkedEvidence...))
	return check
}

type groundedDraftMarker struct {
	Value string
	Start int
}

func groundedDraftReferenceMarkers(answer string) []groundedDraftMarker {
	markers := make([]groundedDraftMarker, 0)
	for offset := 0; offset < len(answer); {
		open := strings.IndexByte(answer[offset:], '[')
		if open < 0 {
			break
		}
		open += offset
		close := strings.IndexByte(answer[open+1:], ']')
		if close < 0 {
			break
		}
		close += open + 1
		value := strings.TrimSpace(answer[open+1 : close])
		lower := strings.ToLower(value)
		if strings.HasPrefix(value, "/tweet/") || strings.HasPrefix(value, "/tweets/") ||
			strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			markers = append(markers, groundedDraftMarker{Value: value, Start: open})
		}
		offset = close + 1
	}
	return markers
}

func canonicalGroundedDraftReference(value string) (string, GroundedDraftSource, bool) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "/tweets/") {
		id, err := strconv.ParseUint(strings.TrimPrefix(value, "/tweets/"), 10, 64)
		if err != nil || id == 0 {
			return "", "", false
		}
		return "/tweets/" + strconv.FormatUint(id, 10), GroundedDraftSourcePlatform, true
	}
	if reference, ok := canonicalPublicWebURL(value); ok {
		return reference, GroundedDraftSourceWeb, true
	}
	return "", "", false
}

func groundedDraftClaimPrecedesMarker(answer string, markerStart int) bool {
	if markerStart <= 0 || markerStart > len(answer) {
		return false
	}
	prefix := answer[:markerStart]
	if separator := strings.LastIndex(prefix, "\n\n"); separator >= 0 {
		prefix = prefix[separator+2:]
	}
	letters := 0
	for _, value := range strings.TrimSpace(prefix) {
		if unicode.IsLetter(value) || unicode.IsNumber(value) {
			letters++
		}
	}
	return letters >= 4
}

func groundedDraftSourceRecords(
	run agentRuntime.RunResult,
	source GroundedDraftSource,
) ([]groundedDraftSourceRecord, error) {
	if source != GroundedDraftSourcePlatform && source != GroundedDraftSourceWeb {
		return nil, fmt.Errorf("grounded draft source %q is unsupported", source)
	}
	records := make([]groundedDraftSourceRecord, 0)
	if source == GroundedDraftSourcePlatform {
		for _, step := range run.Steps {
			for _, observation := range step.Observations {
				if observation.IsError || !trustedPlatformSearchObservation(step, observation) {
					continue
				}
				for _, item := range decodeGoalPlatformSearchItems(observation.StructuredContent) {
					if strings.TrimSpace(item.Content) == "" {
						continue
					}
					digest, reference := platformSearchEvidenceIdentity(item)
					records = append(records, groundedDraftSourceRecord{
						Source: observation.Name, Digest: digest, Reference: reference,
						StepIndex: step.Index, ActionID: observation.ActionID, CapturedAt: step.FinishedAt,
					})
				}
			}
		}
	} else {
		searches, pages := trustedWebResearchRecords(run)
		for _, record := range append(searches, pages...) {
			if record.Source == webSearchTool && !webSearchRecordHasDraftContent(run, record) {
				continue
			}
			records = append(records, groundedDraftSourceRecord{
				Source: record.Source, Digest: record.Digest, Reference: record.Reference,
				StepIndex: record.StepIndex, ActionID: record.ActionID, CapturedAt: record.CapturedAt,
			})
		}
	}
	return uniqueGroundedDraftSourceRecords(records), nil
}

func webSearchRecordHasDraftContent(run agentRuntime.RunResult, record webResearchRecord) bool {
	for _, step := range run.Steps {
		for _, observation := range step.Observations {
			result, action, ok := trustedWebSearchObservation(step, observation)
			if !ok || action.ID != record.ActionID {
				continue
			}
			for _, item := range result.Items {
				reference, valid := canonicalPublicWebURL(item.URL)
				if valid && reference == record.Reference &&
					(strings.TrimSpace(item.Title) != "" || strings.TrimSpace(item.Snippet) != "") {
					return true
				}
			}
		}
	}
	return false
}

func uniqueGroundedDraftSourceRecords(records []groundedDraftSourceRecord) []groundedDraftSourceRecord {
	seen := make(map[string]struct{}, len(records))
	result := make([]groundedDraftSourceRecord, 0, len(records))
	for _, record := range records {
		key := groundedDraftSourceKey(record.Source, record.Digest, record.Reference)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, record)
	}
	return result
}

func groundedDraftSourceKey(source, digest, reference string) string {
	return strings.TrimSpace(source) + "|" + strings.TrimSpace(digest) + "|" + strings.TrimSpace(reference)
}

func validGroundedDraftArtifactReference(reference, prefix string) bool {
	if !strings.HasPrefix(reference, prefix) || !strings.HasSuffix(reference, "/final-answer") {
		return false
	}
	attempt := strings.TrimSuffix(strings.TrimPrefix(reference, prefix), "/final-answer")
	value, err := strconv.Atoi(attempt)
	return err == nil && value >= 0
}

func groundedDraftAnswerDigest(answer string) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(answer))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func groundedDraftUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validateGroundedDraftTask(task agentRuntime.TaskSpec, source GroundedDraftSource) error {
	if source != GroundedDraftSourcePlatform && source != GroundedDraftSourceWeb {
		return fmt.Errorf("grounded draft source %q is unsupported", source)
	}
	if !taskHasCriterion(task, GroundedDraftSourcesCriterion) ||
		!taskHasCriterion(task, GroundedDraftArtifactCriterion) {
		return fmt.Errorf("grounded draft task requires source and artifact criteria")
	}
	for _, criterion := range task.CompletionCriteria {
		if !criterion.Required {
			continue
		}
		if criterion.ID != GroundedDraftSourcesCriterion &&
			criterion.ID != GroundedDraftArtifactCriterion {
			return fmt.Errorf("grounded draft verifier cannot prove required criterion %q", criterion.ID)
		}
	}
	return nil
}
