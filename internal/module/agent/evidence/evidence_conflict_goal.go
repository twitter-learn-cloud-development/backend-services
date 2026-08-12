package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	EvidenceConflictDetectedCriterion      = "evidence_conflict_detected"
	EvidenceConflictClarificationCriterion = "evidence_conflict_clarification_requested"

	EvidenceAssertionSchema                  = "agent.evidence_assertions.v1"
	EvidenceConflictClarificationArtifact    = "agent.evidence_conflict.clarification.v1"
	EvidenceConflictDetectedCode             = "evidence_conflict_detected"
	EvidenceConflictMissingCode              = "evidence_conflict_missing"
	EvidenceConflictClarificationCode        = "evidence_conflict_clarification_verified"
	EvidenceConflictClarificationMissingCode = "evidence_conflict_clarification_missing"

	evidenceConflictClaimConstraintID  = "evidence_conflict_claim"
	evidenceConflictActionConstraintID = "evidence_conflict_action"
	maxEvidenceAssertionPayload        = 64 << 10
	maxEvidenceAssertions              = 32
	maxEvidenceClaimRunes              = 128
	maxEvidenceValueRunes              = 256
	maxEvidencePromptRunes             = 1_000
	maxEvidenceReferenceRunes          = 2_048
)

// EvidenceAssertionResult is the versioned, low-ambiguity schema emitted by a
// trusted read tool. Free-form tool text is never interpreted as a fact.
type EvidenceAssertionResult struct {
	Schema     string              `json:"schema"`
	Assertions []EvidenceAssertion `json:"assertions"`
}

type EvidenceAssertion struct {
	ClaimID   string `json:"claim_id"`
	Value     string `json:"value"`
	Reference string `json:"reference"`
}

// EvidenceConflictSpec is explicit policy input. It defines the canonical
// claim, the exact safe suspension prompt, and the tools trusted to assert it.
type EvidenceConflictSpec struct {
	ClaimID             string
	ClarificationPrompt string
	TrustedTools        []string
}

func (spec EvidenceConflictSpec) normalized() EvidenceConflictSpec {
	spec.ClaimID = strings.TrimSpace(spec.ClaimID)
	spec.ClarificationPrompt = strings.TrimSpace(spec.ClarificationPrompt)
	spec.TrustedTools = append([]string(nil), spec.TrustedTools...)
	for index := range spec.TrustedTools {
		spec.TrustedTools[index] = strings.TrimSpace(spec.TrustedTools[index])
	}
	sort.Strings(spec.TrustedTools)
	return spec
}

func (spec EvidenceConflictSpec) Validate() error {
	spec = spec.normalized()
	if spec.ClaimID == "" || utf8.RuneCountInString(spec.ClaimID) > maxEvidenceClaimRunes {
		return fmt.Errorf("evidence conflict claim ID is required and must not exceed %d characters", maxEvidenceClaimRunes)
	}
	for _, value := range spec.ClaimID {
		if !unicode.IsLetter(value) && !unicode.IsDigit(value) &&
			!strings.ContainsRune("._:/-", value) {
			return fmt.Errorf("evidence conflict claim ID contains unsupported characters")
		}
	}
	if spec.ClarificationPrompt == "" ||
		utf8.RuneCountInString(spec.ClarificationPrompt) > maxEvidencePromptRunes {
		return fmt.Errorf("evidence conflict clarification prompt is required and must not exceed %d characters", maxEvidencePromptRunes)
	}
	if strings.ContainsAny(spec.ClarificationPrompt, "\r\n") {
		return fmt.Errorf("evidence conflict clarification prompt must be a single line")
	}
	if len(spec.TrustedTools) == 0 || len(spec.TrustedTools) > 16 {
		return fmt.Errorf("evidence conflict requires between one and 16 trusted tools")
	}
	for index, tool := range spec.TrustedTools {
		if tool == "" || utf8.RuneCountInString(tool) > 128 {
			return fmt.Errorf("evidence conflict trusted tool name is invalid")
		}
		if index > 0 && tool == spec.TrustedTools[index-1] {
			return fmt.Errorf("duplicate evidence conflict trusted tool %q", tool)
		}
	}
	return nil
}

func BuildEvidenceConflictTask(
	taskID string,
	goal string,
	spec EvidenceConflictSpec,
) (agentRuntime.TaskSpec, error) {
	taskID = strings.TrimSpace(taskID)
	goal = strings.TrimSpace(goal)
	spec = spec.normalized()
	if taskID == "" {
		return agentRuntime.TaskSpec{}, fmt.Errorf("evidence conflict task ID is required")
	}
	if goal == "" {
		return agentRuntime.TaskSpec{}, fmt.Errorf("evidence conflict goal is required")
	}
	if err := spec.Validate(); err != nil {
		return agentRuntime.TaskSpec{}, err
	}
	task := agentRuntime.TaskSpec{
		ID:                 taskID,
		Goal:               goal,
		Constraints:        evidenceConflictConstraints(spec),
		CompletionCriteria: evidenceConflictCriteria(),
		AllowedTools:       append([]string(nil), spec.TrustedTools...),
	}
	if err := task.Validate(); err != nil {
		return agentRuntime.TaskSpec{}, err
	}
	return task, nil
}

type EvidenceConflictGoalCollector struct {
	Spec EvidenceConflictSpec
}

func (collector EvidenceConflictGoalCollector) Collect(
	_ context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	spec := collector.Spec.normalized()
	if err := validateEvidenceConflictTask(request.Task, spec); err != nil {
		return nil, err
	}
	facts := trustedEvidenceAssertions(request.Run, spec)
	items := make([]agentRuntime.Evidence, 0, len(facts)+1)
	for _, fact := range facts {
		idDigest := sha256.Sum256([]byte(fact.key()))
		items = append(items, agentRuntime.Evidence{
			ID:           "conflict-source:" + hex.EncodeToString(idDigest[:])[:24],
			Kind:         agentRuntime.EvidenceToolObservation,
			Source:       fact.Source,
			CriterionIDs: []string{EvidenceConflictDetectedCriterion},
			Digest:       fact.Digest,
			Reference:    fact.Reference,
			StepIndex:    fact.StepIndex,
		})
	}
	if clarification, ok := conflictClarificationIdentity(request.Run, spec); ok {
		items = append(items, agentRuntime.Evidence{
			ID:           "conflict-clarification:" + strings.TrimPrefix(clarification.Digest, "sha256:")[:24],
			Kind:         agentRuntime.EvidenceArtifact,
			Source:       EvidenceConflictClarificationArtifact,
			CriterionIDs: []string{EvidenceConflictClarificationCriterion},
			Digest:       clarification.Digest,
			Reference:    clarification.Reference,
			StepIndex:    clarification.StepIndex,
		})
	}
	return items, nil
}

type EvidenceConflictGoalVerifier struct {
	Spec EvidenceConflictSpec
}

func (verifier EvidenceConflictGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	return verifier.verify(ctx, request)
}

func (verifier EvidenceConflictGoalVerifier) VerifySuspension(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	return verifier.verify(ctx, request)
}

func (verifier EvidenceConflictGoalVerifier) verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	spec := verifier.Spec.normalized()
	if err := validateEvidenceConflictTask(request.Task, spec); err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}

	facts := trustedEvidenceAssertions(request.Run, spec)
	trustedFacts := make(map[string]evidenceAssertionFact, len(facts))
	for _, fact := range facts {
		trustedFacts[fact.key()] = fact
	}
	matchedFacts := make([]evidenceAssertionFact, 0, len(facts))
	conflictEvidenceIDs := make([]string, 0, len(facts))
	for _, item := range request.Evidence.Items {
		if item.Kind != agentRuntime.EvidenceToolObservation ||
			len(item.CriterionIDs) != 1 ||
			item.CriterionIDs[0] != EvidenceConflictDetectedCriterion {
			continue
		}
		key := evidenceAssertionKey(item.Source, item.Digest, item.Reference, item.StepIndex)
		fact, exists := trustedFacts[key]
		if !exists {
			continue
		}
		matchedFacts = append(matchedFacts, fact)
		conflictEvidenceIDs = append(conflictEvidenceIDs, item.ID)
	}
	conflictEvidenceIDs = rewriteUniqueStrings(conflictEvidenceIDs)
	conflictDetected := hasConflictingFacts(matchedFacts)
	lastConflictStep := -1
	if conflictDetected {
		for _, fact := range matchedFacts {
			if fact.StepIndex > lastConflictStep {
				lastConflictStep = fact.StepIndex
			}
		}
	}
	conflictCheck := agentRuntime.CheckResult{
		CriterionID: EvidenceConflictDetectedCriterion,
		Status:      agentRuntime.VerificationFailed,
		Code:        EvidenceConflictMissingCode,
		EvidenceIDs: conflictEvidenceIDs,
	}
	if conflictDetected {
		conflictCheck.Status = agentRuntime.VerificationPassed
		conflictCheck.Code = EvidenceConflictDetectedCode
	}
	replaceCheck(&base, conflictCheck)

	clarificationEvidenceIDs := make([]string, 0, 1)
	if expected, ok := conflictClarificationIdentity(request.Run, spec); ok {
		for _, item := range request.Evidence.Items {
			if item.Kind == agentRuntime.EvidenceArtifact &&
				item.Source == EvidenceConflictClarificationArtifact &&
				len(item.CriterionIDs) == 1 &&
				item.CriterionIDs[0] == EvidenceConflictClarificationCriterion &&
				item.Digest == expected.Digest && item.Reference == expected.Reference &&
				item.StepIndex == expected.StepIndex {
				clarificationEvidenceIDs = append(clarificationEvidenceIDs, item.ID)
			}
		}
		if expected.StepIndex <= lastConflictStep {
			clarificationEvidenceIDs = nil
		}
	}
	clarificationEvidenceIDs = rewriteUniqueStrings(clarificationEvidenceIDs)
	clarificationCheck := agentRuntime.CheckResult{
		CriterionID: EvidenceConflictClarificationCriterion,
		Status:      agentRuntime.VerificationFailed,
		Code:        EvidenceConflictClarificationMissingCode,
		EvidenceIDs: clarificationEvidenceIDs,
	}
	if conflictDetected && len(clarificationEvidenceIDs) == 1 {
		clarificationCheck.Status = agentRuntime.VerificationPassed
		clarificationCheck.Code = EvidenceConflictClarificationCode
	}
	replaceCheck(&base, clarificationCheck)

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

type evidenceAssertionFact struct {
	Source    string
	Value     string
	Digest    string
	Reference string
	StepIndex int
}

func (fact evidenceAssertionFact) key() string {
	return evidenceAssertionKey(fact.Source, fact.Digest, fact.Reference, fact.StepIndex)
}

func evidenceAssertionKey(source, digest, reference string, stepIndex int) string {
	return fmt.Sprintf("%s|%s|%s|%d", source, digest, reference, stepIndex)
}

func trustedEvidenceAssertions(
	run agentRuntime.RunResult,
	spec EvidenceConflictSpec,
) []evidenceAssertionFact {
	trustedTools := make(map[string]struct{}, len(spec.TrustedTools))
	for _, tool := range spec.TrustedTools {
		trustedTools[tool] = struct{}{}
	}
	seen := make(map[string]struct{})
	facts := make([]evidenceAssertionFact, 0)
	for _, step := range run.Steps {
		for _, observation := range step.Observations {
			if observation.IsError || !pairedTrustedAssertionObservation(step, observation, trustedTools) {
				continue
			}
			for _, assertion := range decodeEvidenceAssertions(observation.StructuredContent, spec.ClaimID) {
				digest := evidenceAssertionDigest(spec.ClaimID, assertion.Value, assertion.Reference)
				fact := evidenceAssertionFact{
					Source: observation.Name, Value: assertion.Value, Digest: digest,
					Reference: assertion.Reference, StepIndex: step.Index,
				}
				if _, exists := seen[fact.key()]; exists {
					continue
				}
				seen[fact.key()] = struct{}{}
				facts = append(facts, fact)
			}
		}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].key() < facts[j].key() })
	return facts
}

func pairedTrustedAssertionObservation(
	step agentRuntime.Step,
	observation agentRuntime.Observation,
	trustedTools map[string]struct{},
) bool {
	name := strings.TrimSpace(observation.Name)
	if _, trusted := trustedTools[name]; !trusted {
		return false
	}
	for _, action := range step.Actions {
		if action.ID == observation.ActionID && action.Type == agentRuntime.ActionToolCall &&
			strings.TrimSpace(action.Name) == name {
			return true
		}
	}
	return false
}

func decodeEvidenceAssertions(raw json.RawMessage, claimID string) []EvidenceAssertion {
	if len(raw) == 0 || len(raw) > maxEvidenceAssertionPayload {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result EvidenceAssertionResult
	if err := decoder.Decode(&result); err != nil || result.Schema != EvidenceAssertionSchema ||
		len(result.Assertions) == 0 || len(result.Assertions) > maxEvidenceAssertions {
		return nil
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil
	}
	seen := make(map[string]struct{})
	assertions := make([]EvidenceAssertion, 0, len(result.Assertions))
	for _, assertion := range result.Assertions {
		assertion.ClaimID = strings.TrimSpace(assertion.ClaimID)
		assertion.Value = normalizeEvidenceValue(assertion.Value)
		reference, referenceOK := canonicalEvidenceReference(assertion.Reference)
		if assertion.ClaimID != claimID || assertion.Value == "" ||
			utf8.RuneCountInString(assertion.Value) > maxEvidenceValueRunes ||
			!referenceOK {
			continue
		}
		assertion.Reference = reference
		key := assertion.Value + "|" + assertion.Reference
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		assertions = append(assertions, assertion)
	}
	return assertions
}

func normalizeEvidenceValue(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func canonicalEvidenceReference(reference string) (string, bool) {
	if reference == "" || utf8.RuneCountInString(reference) > maxEvidenceReferenceRunes {
		return "", false
	}
	return canonicalPublicWebURL(reference)
}

func evidenceAssertionDigest(claimID, value, reference string) string {
	payload := struct {
		Schema    string `json:"schema"`
		ClaimID   string `json:"claim_id"`
		Value     string `json:"value"`
		Reference string `json:"reference"`
	}{EvidenceAssertionSchema, claimID, value, reference}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func hasConflictingFacts(facts []evidenceAssertionFact) bool {
	for left := 0; left < len(facts); left++ {
		for right := left + 1; right < len(facts); right++ {
			if facts[left].Value != facts[right].Value &&
				facts[left].Reference != facts[right].Reference {
				return true
			}
		}
	}
	return false
}

type conflictClarification struct {
	Digest    string
	Reference string
	StepIndex int
}

func conflictClarificationIdentity(
	run agentRuntime.RunResult,
	spec EvidenceConflictSpec,
) (conflictClarification, bool) {
	if run.Status != agentRuntime.RunStatusAwaitingHuman ||
		strings.TrimSpace(run.FinalAnswer) != "" || run.PendingAction == nil ||
		run.PendingAction.Type != agentRuntime.ActionAskHuman ||
		run.PendingResumeKind != agentRuntime.ResumeKindHumanResponse ||
		strings.TrimSpace(run.PendingAction.Content) != spec.ClarificationPrompt {
		return conflictClarification{}, false
	}
	stepIndex := -1
	for _, step := range run.Steps {
		for _, action := range step.Actions {
			if action.ID == run.PendingAction.ID && action.Type == agentRuntime.ActionAskHuman &&
				strings.TrimSpace(action.Content) == spec.ClarificationPrompt {
				stepIndex = step.Index
			}
		}
	}
	if stepIndex < 0 {
		return conflictClarification{}, false
	}
	payload := fmt.Sprintf("%s|%d|%s|%s", run.Context.RunID, stepIndex,
		run.PendingAction.ID, spec.ClarificationPrompt)
	digest := sha256.Sum256([]byte(payload))
	digestValue := hex.EncodeToString(digest[:])
	return conflictClarification{
		Digest: "sha256:" + digestValue,
		Reference: fmt.Sprintf("agent-run://%s/step/%d/ask-human/%s",
			run.Context.RunID, stepIndex, digestValue[:24]),
		StepIndex: stepIndex,
	}, true
}

func validateEvidenceConflictTask(task agentRuntime.TaskSpec, spec EvidenceConflictSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := task.Validate(); err != nil {
		return err
	}
	expectedConstraints := evidenceConflictConstraints(spec)
	if len(task.Constraints) != len(expectedConstraints) {
		return fmt.Errorf("evidence conflict task requires canonical constraints")
	}
	for index, expected := range expectedConstraints {
		actual := task.Constraints[index]
		if strings.TrimSpace(actual.ID) != expected.ID ||
			strings.TrimSpace(actual.Description) != expected.Description {
			return fmt.Errorf("evidence conflict task constraint %q does not match verifier policy", actual.ID)
		}
	}
	expectedCriteria := evidenceConflictCriteria()
	if len(task.CompletionCriteria) != len(expectedCriteria) {
		return fmt.Errorf("evidence conflict task requires exactly two completion criteria")
	}
	for index, expected := range expectedCriteria {
		actual := task.CompletionCriteria[index]
		if strings.TrimSpace(actual.ID) != expected.ID ||
			strings.TrimSpace(actual.Description) != expected.Description || !actual.Required {
			return fmt.Errorf("evidence conflict completion criterion %q does not match verifier policy", actual.ID)
		}
	}
	if len(task.AllowedTools) != len(spec.TrustedTools) {
		return fmt.Errorf("evidence conflict task allowed tools do not match verifier policy")
	}
	for index, expected := range spec.TrustedTools {
		if strings.TrimSpace(task.AllowedTools[index]) != expected {
			return fmt.Errorf("evidence conflict task allowed tools do not match verifier policy")
		}
	}
	return nil
}

func evidenceConflictConstraints(spec EvidenceConflictSpec) []agentRuntime.TaskConstraint {
	promptDigest := sha256.Sum256([]byte(spec.ClarificationPrompt))
	return []agentRuntime.TaskConstraint{
		{
			ID:          evidenceConflictClaimConstraintID,
			Description: fmt.Sprintf("Detect distinct canonical values for trusted claim %q.", spec.ClaimID),
		},
		{
			ID: evidenceConflictActionConstraintID,
			Description: "On conflict, suspend for the configured clarification prompt with digest sha256:" +
				hex.EncodeToString(promptDigest[:]) + ".",
		},
	}
}

func evidenceConflictCriteria() []agentRuntime.CompletionCriterion {
	return []agentRuntime.CompletionCriterion{
		{
			ID:          EvidenceConflictDetectedCriterion,
			Description: "At least two trusted references assert different canonical values for the claim.",
			Required:    true,
		},
		{
			ID:          EvidenceConflictClarificationCriterion,
			Description: "The run suspends with the configured clarification request instead of choosing silently.",
			Required:    true,
		},
	}
}
