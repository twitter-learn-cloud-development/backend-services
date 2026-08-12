package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	RewriteArtifactCriterion = "rewrite_artifact_produced"
	RewriteLanguageCriterion = "rewrite_language_satisfied"
	RewriteFormatCriterion   = "rewrite_format_satisfied"
	RewriteLengthCriterion   = "rewrite_length_satisfied"

	RewriteArtifactType             = "content.rewrite"
	RewriteConstraintEvidenceSource = "content.rewrite.constraints.v1"

	RewriteArtifactVerifiedCode   = "rewrite_artifact_verified"
	RewriteArtifactMissingCode    = "rewrite_artifact_missing"
	RewriteConstraintsMissingCode = "rewrite_constraints_missing"
	RewriteLanguageVerifiedCode   = "rewrite_language_verified"
	RewriteLanguageMismatchCode   = "rewrite_language_mismatch"
	RewriteFormatVerifiedCode     = "rewrite_format_verified"
	RewriteFormatMismatchCode     = "rewrite_format_mismatch"
	RewriteLengthVerifiedCode     = "rewrite_length_verified"
	RewriteLengthMismatchCode     = "rewrite_length_mismatch"

	rewriteLanguageConstraintID = "rewrite_language"
	rewriteFormatConstraintID   = "rewrite_format"
	rewriteLengthConstraintID   = "rewrite_length"
	rewriteMaxCharacters        = 10_000
	rewriteDominantScriptRatio  = 7
)

type RewriteLanguage string

const (
	RewriteLanguageChinese RewriteLanguage = "zh"
	RewriteLanguageEnglish RewriteLanguage = "en"
)

type RewriteFormat string

const (
	RewriteFormatPlainText    RewriteFormat = "plain_text"
	RewriteFormatMarkdownList RewriteFormat = "markdown_list"
	RewriteFormatJSON         RewriteFormat = "json"
)

// RewriteConstraintSpec is explicit policy input. It is never inferred from a
// prompt by this verifier, so routing and constraint interpretation remain
// outside the deterministic completion boundary.
type RewriteConstraintSpec struct {
	Language      RewriteLanguage
	Format        RewriteFormat
	MinCharacters int
	MaxCharacters int
}

func (spec RewriteConstraintSpec) Validate() error {
	spec = spec.normalized()
	switch spec.Language {
	case RewriteLanguageChinese, RewriteLanguageEnglish:
	default:
		return fmt.Errorf("rewrite language %q is unsupported", spec.Language)
	}
	switch spec.Format {
	case RewriteFormatPlainText, RewriteFormatMarkdownList, RewriteFormatJSON:
	default:
		return fmt.Errorf("rewrite format %q is unsupported", spec.Format)
	}
	if spec.MinCharacters <= 0 {
		return fmt.Errorf("rewrite minimum characters must be positive")
	}
	if spec.MaxCharacters < spec.MinCharacters {
		return fmt.Errorf("rewrite maximum characters must be at least the minimum")
	}
	if spec.MaxCharacters > rewriteMaxCharacters {
		return fmt.Errorf("rewrite maximum characters exceeds %d", rewriteMaxCharacters)
	}
	return nil
}

func (spec RewriteConstraintSpec) normalized() RewriteConstraintSpec {
	spec.Language = RewriteLanguage(strings.ToLower(strings.TrimSpace(string(spec.Language))))
	spec.Format = RewriteFormat(strings.ToLower(strings.TrimSpace(string(spec.Format))))
	return spec
}

// BuildRewriteConstraintTask binds the verifier configuration to canonical
// Task constraints. Callers may set MaxRepairAttempts on the returned value.
func BuildRewriteConstraintTask(
	taskID string,
	goal string,
	spec RewriteConstraintSpec,
) (agentRuntime.TaskSpec, error) {
	taskID = strings.TrimSpace(taskID)
	goal = strings.TrimSpace(goal)
	spec = spec.normalized()
	if taskID == "" {
		return agentRuntime.TaskSpec{}, fmt.Errorf("rewrite task ID is required")
	}
	if goal == "" {
		return agentRuntime.TaskSpec{}, fmt.Errorf("rewrite goal is required")
	}
	if err := spec.Validate(); err != nil {
		return agentRuntime.TaskSpec{}, err
	}
	task := agentRuntime.TaskSpec{
		ID:                 taskID,
		Goal:               goal,
		Constraints:        rewriteTaskConstraints(spec),
		CompletionCriteria: rewriteCompletionCriteria(),
	}
	if err := task.Validate(); err != nil {
		return agentRuntime.TaskSpec{}, err
	}
	return task, nil
}

type RewriteConstraintGoalCollector struct {
	Constraints RewriteConstraintSpec
}

func (collector RewriteConstraintGoalCollector) Collect(
	ctx context.Context,
	request agentRuntime.EvidenceCollectionRequest,
) ([]agentRuntime.Evidence, error) {
	spec := collector.Constraints.normalized()
	if err := validateRewriteConstraintTask(request.Task, spec); err != nil {
		return nil, err
	}
	digest, reference := rewriteConstraintIdentity(spec)
	items := []agentRuntime.Evidence{{
		ID:           "rewrite-constraints:" + strings.TrimPrefix(digest, "sha256:")[:24],
		Kind:         agentRuntime.EvidenceEnvironmentState,
		Source:       RewriteConstraintEvidenceSource,
		CriterionIDs: rewriteConstraintCriterionIDs(),
		Digest:       digest,
		Reference:    reference,
	}}
	artifact, err := (agentRuntime.FinalAnswerArtifactEvidenceCollector{
		ArtifactType: RewriteArtifactType,
		CriterionIDs: rewriteAllCriterionIDs(),
	}).Collect(ctx, request)
	if err != nil {
		return nil, err
	}
	return append(items, artifact...), nil
}

type RewriteConstraintGoalVerifier struct {
	Constraints RewriteConstraintSpec
}

func (verifier RewriteConstraintGoalVerifier) Verify(
	ctx context.Context,
	request agentRuntime.VerificationRequest,
) (agentRuntime.VerificationResult, error) {
	spec := verifier.Constraints.normalized()
	if err := validateRewriteConstraintTask(request.Task, spec); err != nil {
		return agentRuntime.VerificationResult{}, err
	}
	base, err := (agentRuntime.RequiredEvidenceVerifier{}).Verify(ctx, request)
	if err != nil {
		return agentRuntime.VerificationResult{}, err
	}

	constraintDigest, constraintReference := rewriteConstraintIdentity(spec)
	constraintEvidence := make([]string, 0, 1)
	artifactEvidence := make([]string, 0, 1)
	answerDigest := rewriteAnswerDigest(request.Run.FinalAnswer)
	artifactPrefix := "agent-run://" + strings.TrimSpace(request.Run.Context.RunID) + "/attempt/"
	for _, item := range request.Evidence.Items {
		switch {
		case item.Kind == agentRuntime.EvidenceEnvironmentState &&
			item.Source == RewriteConstraintEvidenceSource &&
			containsAllStrings(item.CriterionIDs, rewriteConstraintCriterionIDs()) &&
			item.Digest == constraintDigest && item.Reference == constraintReference:
			constraintEvidence = append(constraintEvidence, item.ID)
		case item.Kind == agentRuntime.EvidenceArtifact &&
			item.Source == RewriteArtifactType &&
			containsAllStrings(item.CriterionIDs, rewriteAllCriterionIDs()) &&
			item.Digest == answerDigest &&
			validRewriteArtifactReference(item.Reference, artifactPrefix):
			artifactEvidence = append(artifactEvidence, item.ID)
		}
	}
	constraintEvidence = rewriteUniqueStrings(constraintEvidence)
	artifactEvidence = rewriteUniqueStrings(artifactEvidence)

	artifactCheck := agentRuntime.CheckResult{
		CriterionID: RewriteArtifactCriterion,
		Status:      agentRuntime.VerificationFailed,
		Code:        RewriteArtifactMissingCode,
	}
	if len(artifactEvidence) == 1 && strings.TrimSpace(request.Run.FinalAnswer) != "" {
		artifactCheck.Status = agentRuntime.VerificationPassed
		artifactCheck.Code = RewriteArtifactVerifiedCode
		artifactCheck.EvidenceIDs = append([]string(nil), artifactEvidence...)
	}
	replaceCheck(&base, artifactCheck)

	replaceCheck(&base, rewriteConstraintCheck(
		RewriteLanguageCriterion,
		RewriteLanguageVerifiedCode,
		RewriteLanguageMismatchCode,
		len(constraintEvidence) == 1 && len(artifactEvidence) == 1 &&
			rewriteLanguageMatches(request.Run.FinalAnswer, spec.Language),
		constraintEvidence,
		artifactEvidence,
	))
	replaceCheck(&base, rewriteConstraintCheck(
		RewriteFormatCriterion,
		RewriteFormatVerifiedCode,
		RewriteFormatMismatchCode,
		len(constraintEvidence) == 1 && len(artifactEvidence) == 1 &&
			rewriteFormatMatches(request.Run.FinalAnswer, spec.Format),
		constraintEvidence,
		artifactEvidence,
	))
	replaceCheck(&base, rewriteConstraintCheck(
		RewriteLengthCriterion,
		RewriteLengthVerifiedCode,
		RewriteLengthMismatchCode,
		len(constraintEvidence) == 1 && len(artifactEvidence) == 1 &&
			rewriteLengthMatches(request.Run.FinalAnswer, spec),
		constraintEvidence,
		artifactEvidence,
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

func rewriteConstraintCheck(
	criterionID string,
	verifiedCode string,
	mismatchCode string,
	passed bool,
	constraintEvidence []string,
	artifactEvidence []string,
) agentRuntime.CheckResult {
	check := agentRuntime.CheckResult{
		CriterionID: criterionID,
		Status:      agentRuntime.VerificationFailed,
		Code:        mismatchCode,
		EvidenceIDs: rewriteUniqueStrings(append(
			append([]string(nil), constraintEvidence...),
			artifactEvidence...,
		)),
	}
	if len(constraintEvidence) != 1 {
		check.Code = RewriteConstraintsMissingCode
		return check
	}
	if len(artifactEvidence) != 1 {
		check.Code = RewriteArtifactMissingCode
		return check
	}
	if passed {
		check.Status = agentRuntime.VerificationPassed
		check.Code = verifiedCode
	}
	return check
}

func rewriteLanguageMatches(answer string, language RewriteLanguage) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	letters := 0
	dominant := 0
	for _, value := range answer {
		if !unicode.IsLetter(value) {
			continue
		}
		letters++
		switch language {
		case RewriteLanguageChinese:
			if unicode.Is(unicode.Han, value) {
				dominant++
			}
		case RewriteLanguageEnglish:
			if unicode.Is(unicode.Latin, value) {
				dominant++
			}
		}
	}
	return letters > 0 && dominant*10 >= letters*rewriteDominantScriptRatio
}

func rewriteFormatMatches(answer string, format RewriteFormat) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	switch format {
	case RewriteFormatJSON:
		var value any
		if err := json.Unmarshal([]byte(answer), &value); err != nil {
			return false
		}
		switch value.(type) {
		case map[string]any, []any:
			return true
		default:
			return false
		}
	case RewriteFormatMarkdownList:
		items := 0
		for _, line := range strings.Split(answer, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") &&
				!hasOrderedListPrefix(line) {
				return false
			}
			items++
		}
		return items >= 2
	case RewriteFormatPlainText:
		if json.Valid([]byte(answer)) {
			return false
		}
		for _, line := range strings.Split(answer, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "- ") ||
				strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "```") ||
				hasOrderedListPrefix(line) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func hasOrderedListPrefix(value string) bool {
	dot := strings.Index(value, ". ")
	if dot <= 0 {
		return false
	}
	_, err := strconv.Atoi(value[:dot])
	return err == nil
}

func rewriteLengthMatches(answer string, spec RewriteConstraintSpec) bool {
	length := utf8.RuneCountInString(strings.TrimSpace(answer))
	return length >= spec.MinCharacters && length <= spec.MaxCharacters
}

func validateRewriteConstraintTask(task agentRuntime.TaskSpec, spec RewriteConstraintSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := task.Validate(); err != nil {
		return err
	}
	if len(task.AllowedTools) != 0 {
		return fmt.Errorf("rewrite constraint task must not allow tools")
	}
	expectedConstraints := rewriteTaskConstraints(spec)
	if len(task.Constraints) != len(expectedConstraints) {
		return fmt.Errorf("rewrite constraint task requires canonical language, format and length constraints")
	}
	for index, expected := range expectedConstraints {
		actual := task.Constraints[index]
		if strings.TrimSpace(actual.ID) != expected.ID ||
			strings.TrimSpace(actual.Description) != expected.Description {
			return fmt.Errorf("rewrite task constraint %q does not match verifier policy", actual.ID)
		}
	}
	expectedCriteria := rewriteCompletionCriteria()
	if len(task.CompletionCriteria) != len(expectedCriteria) {
		return fmt.Errorf("rewrite constraint task requires exactly four completion criteria")
	}
	for index, expected := range expectedCriteria {
		actual := task.CompletionCriteria[index]
		if strings.TrimSpace(actual.ID) != expected.ID ||
			strings.TrimSpace(actual.Description) != expected.Description ||
			!actual.Required {
			return fmt.Errorf("rewrite completion criterion %q does not match verifier policy", actual.ID)
		}
	}
	return nil
}

func rewriteTaskConstraints(spec RewriteConstraintSpec) []agentRuntime.TaskConstraint {
	return []agentRuntime.TaskConstraint{
		{
			ID: rewriteLanguageConstraintID,
			Description: fmt.Sprintf(
				"Final output language must be %s with at least 70 percent of letters in the expected script.",
				spec.Language,
			),
		},
		{
			ID:          rewriteFormatConstraintID,
			Description: fmt.Sprintf("Final output format must be %s.", spec.Format),
		},
		{
			ID: rewriteLengthConstraintID,
			Description: fmt.Sprintf(
				"Final output must contain between %d and %d Unicode characters after outer whitespace trimming.",
				spec.MinCharacters,
				spec.MaxCharacters,
			),
		},
	}
}

func rewriteCompletionCriteria() []agentRuntime.CompletionCriterion {
	return []agentRuntime.CompletionCriterion{
		{ID: RewriteArtifactCriterion, Description: "A rewrite artifact was produced.", Required: true},
		{ID: RewriteLanguageCriterion, Description: "The rewrite uses the requested language.", Required: true},
		{ID: RewriteFormatCriterion, Description: "The rewrite uses the requested format.", Required: true},
		{ID: RewriteLengthCriterion, Description: "The rewrite stays within the configured character range.", Required: true},
	}
}

func rewriteConstraintCriterionIDs() []string {
	return []string{RewriteLanguageCriterion, RewriteFormatCriterion, RewriteLengthCriterion}
}

func rewriteAllCriterionIDs() []string {
	return []string{
		RewriteArtifactCriterion,
		RewriteLanguageCriterion,
		RewriteFormatCriterion,
		RewriteLengthCriterion,
	}
}

func rewriteConstraintIdentity(spec RewriteConstraintSpec) (string, string) {
	payload := struct {
		Version       string          `json:"version"`
		Language      RewriteLanguage `json:"language"`
		Format        RewriteFormat   `json:"format"`
		MinCharacters int             `json:"min_characters"`
		MaxCharacters int             `json:"max_characters"`
	}{
		Version:       "content.rewrite.constraints.v1",
		Language:      spec.Language,
		Format:        spec.Format,
		MinCharacters: spec.MinCharacters,
		MaxCharacters: spec.MaxCharacters,
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	digestValue := hex.EncodeToString(digest[:])
	return "sha256:" + digestValue, "agent-policy://content.rewrite.constraints.v1/" + digestValue
}

func rewriteAnswerDigest(answer string) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(answer))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func validRewriteArtifactReference(reference, prefix string) bool {
	if !strings.HasPrefix(reference, prefix) || !strings.HasSuffix(reference, "/final-answer") {
		return false
	}
	attempt := strings.TrimSuffix(strings.TrimPrefix(reference, prefix), "/final-answer")
	value, err := strconv.Atoi(attempt)
	return err == nil && value >= 0
}

func containsAllStrings(values, expected []string) bool {
	for _, candidate := range expected {
		if !containsString(values, candidate) {
			return false
		}
	}
	return len(rewriteUniqueStrings(values)) == len(rewriteUniqueStrings(expected))
}

func rewriteUniqueStrings(values []string) []string {
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
