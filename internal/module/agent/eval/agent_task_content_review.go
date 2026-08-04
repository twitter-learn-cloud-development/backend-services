package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	AgentTaskContentReviewDecisionSchemaVersion = "agent-task-content-review-decision/v1"
	AgentTaskContentReviewSignoffSchemaVersion  = "agent-task-content-review-signoff/v1"
	AgentTaskContentReviewRuleVersion           = "agent-task-content-review-rules/v1"
	AgentTaskReviewBundleSchemaVersion          = "agent-task-review-bundle/v1"
)

type AgentTaskContentReviewStatus string

const (
	AgentTaskContentReviewPassed AgentTaskContentReviewStatus = "pass"
	AgentTaskContentReviewFailed AgentTaskContentReviewStatus = "fail"
)

type AgentTaskContentReviewVerdict string

const (
	AgentTaskContentReviewApproved AgentTaskContentReviewVerdict = "approved"
	AgentTaskContentReviewRejected AgentTaskContentReviewVerdict = "rejected"
)

type AgentTaskContentReviewerKind string

const (
	AgentTaskContentReviewerExternalHuman AgentTaskContentReviewerKind = "external_human"
	AgentTaskContentReviewerJudge         AgentTaskContentReviewerKind = "judge"
)

type AgentTaskContentReviewerAssurance string

const (
	AgentTaskContentReviewerAssertedExternal AgentTaskContentReviewerAssurance = "asserted_external"
	AgentTaskContentReviewerModelConfigBound AgentTaskContentReviewerAssurance = "model_config_bound"
)

type AgentTaskContentReviewAssessment struct {
	FactualCorrectness AgentTaskContentReviewStatus `json:"factual_correctness"`
	Relevance          AgentTaskContentReviewStatus `json:"relevance"`
	EvidenceFidelity   AgentTaskContentReviewStatus `json:"evidence_fidelity"`
	WritingQuality     AgentTaskContentReviewStatus `json:"writing_quality"`
}

type AgentTaskContentReviewCaseDecision struct {
	CaseID    string                           `json:"case_id"`
	Candidate AgentTaskContentReviewAssessment `json:"candidate"`
	Stable    AgentTaskContentReviewAssessment `json:"stable"`
}

type AgentTaskContentReviewJudgeIdentity struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	PromptID      string `json:"prompt_id"`
	PromptVersion string `json:"prompt_version"`
	ConfigSHA256  string `json:"config_sha256"`
}

type AgentTaskContentReviewer struct {
	Kind                 AgentTaskContentReviewerKind         `json:"kind"`
	ID                   string                               `json:"id"`
	IdentityAssurance    AgentTaskContentReviewerAssurance    `json:"identity_assurance"`
	ExternalRecordSHA256 string                               `json:"external_record_sha256,omitempty"`
	Judge                *AgentTaskContentReviewJudgeIdentity `json:"judge,omitempty"`
}

type AgentTaskContentReviewDecision struct {
	SchemaVersion       string                               `json:"schema_version"`
	ReportPayloadSHA256 string                               `json:"report_payload_sha256"`
	ReviewBundleSHA256  string                               `json:"review_bundle_sha256"`
	RuleVersion         string                               `json:"rule_version"`
	Reviewer            AgentTaskContentReviewer             `json:"reviewer"`
	ReviewedAt          time.Time                            `json:"reviewed_at"`
	CandidateVerdict    AgentTaskContentReviewVerdict        `json:"candidate_verdict"`
	StableVerdict       AgentTaskContentReviewVerdict        `json:"stable_verdict"`
	Cases               []AgentTaskContentReviewCaseDecision `json:"cases"`
	ReviewNotesSHA256   string                               `json:"review_notes_sha256,omitempty"`
}

type AgentTaskContentReviewBundleBinding struct {
	SchemaVersion string
	KeyID         string
	FileSHA256    string
}

type AgentTaskContentReviewCaseSignoff struct {
	CaseID                string                           `json:"case_id"`
	CandidateOutputSHA256 string                           `json:"candidate_output_sha256,omitempty"`
	StableOutputSHA256    string                           `json:"stable_output_sha256,omitempty"`
	Candidate             AgentTaskContentReviewAssessment `json:"candidate"`
	Stable                AgentTaskContentReviewAssessment `json:"stable"`
}

type AgentTaskContentReviewSignoff struct {
	SchemaVersion                  string                              `json:"schema_version"`
	CreatedAt                      time.Time                           `json:"created_at"`
	ReportPayloadSHA256            string                              `json:"report_payload_sha256"`
	ReportIntegrityKeyID           string                              `json:"report_integrity_key_id"`
	ReviewBundleSchemaVersion      string                              `json:"review_bundle_schema_version"`
	ReviewBundleKeyID              string                              `json:"review_bundle_key_id"`
	ReviewBundleSHA256             string                              `json:"review_bundle_sha256"`
	DatasetVersion                 string                              `json:"dataset_version"`
	DatasetSHA256                  string                              `json:"dataset_sha256"`
	CandidateExecutionConfigSHA256 string                              `json:"candidate_execution_config_sha256"`
	StableExecutionConfigSHA256    string                              `json:"stable_execution_config_sha256"`
	RuleVersion                    string                              `json:"rule_version"`
	Reviewer                       AgentTaskContentReviewer            `json:"reviewer"`
	ReviewedAt                     time.Time                           `json:"reviewed_at"`
	CandidateVerdict               AgentTaskContentReviewVerdict       `json:"candidate_verdict"`
	StableVerdict                  AgentTaskContentReviewVerdict       `json:"stable_verdict"`
	Cases                          []AgentTaskContentReviewCaseSignoff `json:"cases"`
	DecisionSHA256                 string                              `json:"decision_sha256"`
	ReviewNotesSHA256              string                              `json:"review_notes_sha256,omitempty"`
	Integrity                      *AgentTaskReportIntegrity           `json:"integrity,omitempty"`
}

func BuildAndSignAgentTaskContentReviewSignoff(
	output AgentTaskEvaluationOutput,
	binding AgentTaskContentReviewBundleBinding,
	decision AgentTaskContentReviewDecision,
	key []byte,
	keyID string,
	createdAt time.Time,
) (AgentTaskContentReviewSignoff, error) {
	if createdAt.IsZero() {
		return AgentTaskContentReviewSignoff{}, errors.New("content review signoff creation time is required")
	}
	if err := validateAgentTaskContentReviewReport(output); err != nil {
		return AgentTaskContentReviewSignoff{}, err
	}
	if err := validateAgentTaskContentReviewBundleBinding(binding); err != nil {
		return AgentTaskContentReviewSignoff{}, err
	}
	decision = normalizeAgentTaskContentReviewDecision(decision)
	if err := validateAgentTaskContentReviewDecision(decision, output, binding, createdAt); err != nil {
		return AgentTaskContentReviewSignoff{}, err
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == output.Integrity.KeyID {
		return AgentTaskContentReviewSignoff{}, errors.New("content review signoff key ID must differ from report integrity key ID")
	}
	decisionSHA256, err := HashCanonicalJSON(decision)
	if err != nil {
		return AgentTaskContentReviewSignoff{}, fmt.Errorf("hash content review decision: %w", err)
	}
	cases := make([]AgentTaskContentReviewCaseSignoff, len(decision.Cases))
	for index, reviewed := range decision.Cases {
		cases[index] = AgentTaskContentReviewCaseSignoff{
			CaseID:                reviewed.CaseID,
			CandidateOutputSHA256: output.Candidate.CaseResults[index].OutputSHA256,
			StableOutputSHA256:    output.Stable.CaseResults[index].OutputSHA256,
			Candidate:             reviewed.Candidate,
			Stable:                reviewed.Stable,
		}
	}
	signoff := AgentTaskContentReviewSignoff{
		SchemaVersion:                  AgentTaskContentReviewSignoffSchemaVersion,
		CreatedAt:                      createdAt.UTC(),
		ReportPayloadSHA256:            strings.ToLower(output.Integrity.PayloadSHA256),
		ReportIntegrityKeyID:           output.Integrity.KeyID,
		ReviewBundleSchemaVersion:      binding.SchemaVersion,
		ReviewBundleKeyID:              binding.KeyID,
		ReviewBundleSHA256:             strings.ToLower(binding.FileSHA256),
		DatasetVersion:                 output.Candidate.DatasetVersion,
		DatasetSHA256:                  strings.ToLower(output.Candidate.DatasetSHA256),
		CandidateExecutionConfigSHA256: strings.ToLower(output.Candidate.ExecutionConfigHash),
		StableExecutionConfigSHA256:    strings.ToLower(output.Stable.ExecutionConfigHash),
		RuleVersion:                    decision.RuleVersion,
		Reviewer:                       decision.Reviewer,
		ReviewedAt:                     decision.ReviewedAt.UTC(),
		CandidateVerdict:               decision.CandidateVerdict,
		StableVerdict:                  decision.StableVerdict,
		Cases:                          cases,
		DecisionSHA256:                 decisionSHA256,
		ReviewNotesSHA256:              decision.ReviewNotesSHA256,
	}
	payload, err := unsignedAgentTaskContentReviewSignoffPayload(signoff)
	if err != nil {
		return AgentTaskContentReviewSignoff{}, err
	}
	integrity, err := SignAgentTaskPayload(payload, key, keyID, createdAt)
	if err != nil {
		return AgentTaskContentReviewSignoff{}, fmt.Errorf("sign content review signoff: %w", err)
	}
	signoff.Integrity = &integrity
	return signoff, nil
}

// VerifyAgentTaskContentReviewSignoff assumes the caller has already verified
// the report HMAC and decrypted/validated the review bundle.
func VerifyAgentTaskContentReviewSignoff(
	signoff AgentTaskContentReviewSignoff,
	output AgentTaskEvaluationOutput,
	binding AgentTaskContentReviewBundleBinding,
	key []byte,
	trustedKeyID string,
) error {
	if signoff.SchemaVersion != AgentTaskContentReviewSignoffSchemaVersion {
		return fmt.Errorf("unsupported content review signoff schema version %q", signoff.SchemaVersion)
	}
	if signoff.Integrity == nil {
		return errors.New("content review signoff is unsigned")
	}
	if signoff.CreatedAt.IsZero() || !signoff.CreatedAt.Equal(signoff.Integrity.SignedAt) {
		return errors.New("content review signoff and integrity times differ")
	}
	trustedKeyID = strings.TrimSpace(trustedKeyID)
	if trustedKeyID != "" && signoff.Integrity.KeyID != trustedKeyID {
		return fmt.Errorf("content review signoff key ID %q does not match trusted key ID %q", signoff.Integrity.KeyID, trustedKeyID)
	}
	if err := validateAgentTaskContentReviewReport(output); err != nil {
		return err
	}
	if err := validateAgentTaskContentReviewBundleBinding(binding); err != nil {
		return err
	}
	if signoff.Integrity.KeyID == output.Integrity.KeyID {
		return errors.New("content review signoff key ID must differ from report integrity key ID")
	}
	payload, err := unsignedAgentTaskContentReviewSignoffPayload(signoff)
	if err != nil {
		return err
	}
	if err := VerifyAgentTaskPayload(payload, key, *signoff.Integrity); err != nil {
		return fmt.Errorf("verify content review signoff integrity: %w", err)
	}
	decision, err := decisionFromAgentTaskContentReviewSignoff(signoff)
	if err != nil {
		return err
	}
	if err := validateAgentTaskContentReviewDecision(decision, output, binding, signoff.CreatedAt); err != nil {
		return err
	}
	decisionSHA256, err := HashCanonicalJSON(decision)
	if err != nil {
		return fmt.Errorf("hash content review decision: %w", err)
	}
	if decisionSHA256 != strings.ToLower(signoff.DecisionSHA256) {
		return errors.New("content review decision hash mismatch")
	}
	if signoff.ReportPayloadSHA256 != strings.ToLower(output.Integrity.PayloadSHA256) ||
		signoff.ReportIntegrityKeyID != output.Integrity.KeyID ||
		signoff.ReviewBundleSchemaVersion != binding.SchemaVersion ||
		signoff.ReviewBundleKeyID != binding.KeyID ||
		signoff.ReviewBundleSHA256 != strings.ToLower(binding.FileSHA256) ||
		signoff.DatasetVersion != output.Candidate.DatasetVersion ||
		signoff.DatasetSHA256 != strings.ToLower(output.Candidate.DatasetSHA256) ||
		signoff.CandidateExecutionConfigSHA256 != strings.ToLower(output.Candidate.ExecutionConfigHash) ||
		signoff.StableExecutionConfigSHA256 != strings.ToLower(output.Stable.ExecutionConfigHash) {
		return errors.New("content review signoff does not match report or review bundle identity")
	}
	for index, reviewed := range signoff.Cases {
		if reviewed.CandidateOutputSHA256 != output.Candidate.CaseResults[index].OutputSHA256 ||
			reviewed.StableOutputSHA256 != output.Stable.CaseResults[index].OutputSHA256 {
			return fmt.Errorf("content review output digest mismatch for case %q", reviewed.CaseID)
		}
	}
	return nil
}

// AgentTaskContentReviewHasApprovedExternalHumanSignoff must only be called
// after the signoff, signed report and encrypted review bundle are verified.
// It is a necessary content-review signal, not a complete production gate.
func AgentTaskContentReviewHasApprovedExternalHumanSignoff(signoff AgentTaskContentReviewSignoff) bool {
	return signoff.RuleVersion == AgentTaskContentReviewRuleVersion &&
		signoff.Reviewer.Kind == AgentTaskContentReviewerExternalHuman &&
		signoff.Reviewer.IdentityAssurance == AgentTaskContentReviewerAssertedExternal &&
		signoff.CandidateVerdict == AgentTaskContentReviewApproved
}

func MarshalAgentTaskContentReviewSignoff(signoff AgentTaskContentReviewSignoff) ([]byte, error) {
	payload, err := json.Marshal(signoff)
	if err != nil {
		return nil, fmt.Errorf("encode content review signoff: %w", err)
	}
	return payload, nil
}

func DecodeAgentTaskContentReviewDecision(payload []byte) (AgentTaskContentReviewDecision, error) {
	var decision AgentTaskContentReviewDecision
	if err := decodeStrictAgentTaskContentReviewJSON(payload, &decision, "content review decision"); err != nil {
		return AgentTaskContentReviewDecision{}, err
	}
	return decision, nil
}

func DecodeAgentTaskContentReviewSignoff(payload []byte) (AgentTaskContentReviewSignoff, error) {
	var signoff AgentTaskContentReviewSignoff
	if err := decodeStrictAgentTaskContentReviewJSON(payload, &signoff, "content review signoff"); err != nil {
		return AgentTaskContentReviewSignoff{}, err
	}
	return signoff, nil
}

func validateAgentTaskContentReviewReport(output AgentTaskEvaluationOutput) error {
	if output.SchemaVersion != AgentTaskEvaluationSchemaVersion || output.Integrity == nil || output.Stable == nil {
		return errors.New("content review requires a signed stable/candidate evaluation report")
	}
	if output.Gate == nil || output.Gate.Status != AgentQualityGatePassed ||
		output.StrategyGate == nil || output.StrategyGate.Status != AgentQualityGatePassed {
		return errors.New("content review requires both automatic quality gates to pass")
	}
	if !validSHA256(output.Integrity.PayloadSHA256) || strings.TrimSpace(output.Integrity.KeyID) == "" {
		return errors.New("content review report integrity identity is invalid")
	}
	if strings.TrimSpace(output.Candidate.DatasetVersion) == "" ||
		output.Candidate.DatasetVersion != output.Stable.DatasetVersion ||
		!validSHA256(output.Candidate.DatasetSHA256) ||
		output.Candidate.DatasetSHA256 != output.Stable.DatasetSHA256 {
		return errors.New("content review report dataset identity is invalid")
	}
	if !validSHA256(output.Candidate.ExecutionConfigHash) || !validSHA256(output.Stable.ExecutionConfigHash) {
		return errors.New("content review report execution config identity is invalid")
	}
	if len(output.Candidate.CaseResults) == 0 || len(output.Candidate.CaseResults) != len(output.Stable.CaseResults) {
		return errors.New("content review report sides have invalid case coverage")
	}
	seen := make(map[string]struct{}, len(output.Candidate.CaseResults))
	for index, candidate := range output.Candidate.CaseResults {
		stable := output.Stable.CaseResults[index]
		if strings.TrimSpace(candidate.CaseID) == "" || candidate.CaseID != stable.CaseID {
			return fmt.Errorf("content review report side mismatch at case %d", index)
		}
		if _, exists := seen[candidate.CaseID]; exists {
			return fmt.Errorf("content review report contains duplicate case id %q", candidate.CaseID)
		}
		seen[candidate.CaseID] = struct{}{}
		if (candidate.OutputSHA256 != "" && !validSHA256(candidate.OutputSHA256)) ||
			(stable.OutputSHA256 != "" && !validSHA256(stable.OutputSHA256)) {
			return fmt.Errorf("content review report contains invalid output digest for case %q", candidate.CaseID)
		}
	}
	return nil
}

func validateAgentTaskContentReviewBundleBinding(binding AgentTaskContentReviewBundleBinding) error {
	if strings.TrimSpace(binding.SchemaVersion) != AgentTaskReviewBundleSchemaVersion {
		return fmt.Errorf("unsupported content review bundle schema version %q", binding.SchemaVersion)
	}
	if strings.TrimSpace(binding.KeyID) == "" || !validAgentTaskContentReviewDigest(binding.FileSHA256) {
		return errors.New("content review bundle binding is incomplete")
	}
	return nil
}

func validateAgentTaskContentReviewDecision(
	decision AgentTaskContentReviewDecision,
	output AgentTaskEvaluationOutput,
	binding AgentTaskContentReviewBundleBinding,
	createdAt time.Time,
) error {
	if decision.SchemaVersion != AgentTaskContentReviewDecisionSchemaVersion {
		return fmt.Errorf("unsupported content review decision schema version %q", decision.SchemaVersion)
	}
	if decision.RuleVersion != AgentTaskContentReviewRuleVersion {
		return fmt.Errorf("unsupported content review rule version %q", decision.RuleVersion)
	}
	if decision.ReportPayloadSHA256 != strings.ToLower(output.Integrity.PayloadSHA256) ||
		decision.ReviewBundleSHA256 != strings.ToLower(binding.FileSHA256) {
		return errors.New("content review decision does not match report or review bundle")
	}
	if decision.ReviewedAt.IsZero() || decision.ReviewedAt.Before(output.Integrity.SignedAt) || decision.ReviewedAt.After(createdAt) {
		return errors.New("content review decision time is outside the signed report interval")
	}
	if err := validateAgentTaskContentReviewer(decision.Reviewer); err != nil {
		return err
	}
	if decision.ReviewNotesSHA256 != "" && !validAgentTaskContentReviewDigest(decision.ReviewNotesSHA256) {
		return errors.New("content review notes digest is invalid")
	}
	if len(decision.Cases) != len(output.Candidate.CaseResults) {
		return fmt.Errorf("content review decision contains %d cases, want %d", len(decision.Cases), len(output.Candidate.CaseResults))
	}
	seen := make(map[string]struct{}, len(decision.Cases))
	for index, reviewed := range decision.Cases {
		expectedID := output.Candidate.CaseResults[index].CaseID
		if reviewed.CaseID != expectedID {
			return fmt.Errorf("content review case %d id %q does not match report id %q", index, reviewed.CaseID, expectedID)
		}
		if _, exists := seen[reviewed.CaseID]; exists {
			return fmt.Errorf("content review decision contains duplicate case id %q", reviewed.CaseID)
		}
		seen[reviewed.CaseID] = struct{}{}
		if err := validateAgentTaskContentReviewAssessment(reviewed.Candidate); err != nil {
			return fmt.Errorf("content review candidate case %q: %w", reviewed.CaseID, err)
		}
		if err := validateAgentTaskContentReviewAssessment(reviewed.Stable); err != nil {
			return fmt.Errorf("content review stable case %q: %w", reviewed.CaseID, err)
		}
	}
	candidateVerdict := deriveAgentTaskContentReviewVerdict(decision.Cases, true)
	stableVerdict := deriveAgentTaskContentReviewVerdict(decision.Cases, false)
	if decision.CandidateVerdict != candidateVerdict || decision.StableVerdict != stableVerdict {
		return errors.New("content review verdict does not match per-case dimension results")
	}
	return nil
}

func validateAgentTaskContentReviewer(reviewer AgentTaskContentReviewer) error {
	if !validAgentTaskContentReviewIdentifier(reviewer.ID) {
		return errors.New("content reviewer id must be a 1-128 character pseudonymous ASCII identifier")
	}
	switch reviewer.Kind {
	case AgentTaskContentReviewerExternalHuman:
		if reviewer.IdentityAssurance != AgentTaskContentReviewerAssertedExternal ||
			!validAgentTaskContentReviewDigest(reviewer.ExternalRecordSHA256) || reviewer.Judge != nil {
			return errors.New("external human reviewer requires asserted_external assurance and an external record digest")
		}
	case AgentTaskContentReviewerJudge:
		if reviewer.IdentityAssurance != AgentTaskContentReviewerModelConfigBound ||
			reviewer.ExternalRecordSHA256 != "" || reviewer.Judge == nil {
			return errors.New("judge reviewer requires model_config_bound assurance and judge identity")
		}
		if !validAgentTaskContentReviewLabel(reviewer.Judge.Provider) ||
			!validAgentTaskContentReviewLabel(reviewer.Judge.Model) ||
			!validAgentTaskContentReviewLabel(reviewer.Judge.PromptID) ||
			!validAgentTaskContentReviewLabel(reviewer.Judge.PromptVersion) ||
			!validAgentTaskContentReviewDigest(reviewer.Judge.ConfigSHA256) {
			return errors.New("judge reviewer identity is incomplete")
		}
	default:
		return fmt.Errorf("unsupported content reviewer kind %q", reviewer.Kind)
	}
	return nil
}

func validateAgentTaskContentReviewAssessment(assessment AgentTaskContentReviewAssessment) error {
	statuses := []AgentTaskContentReviewStatus{
		assessment.FactualCorrectness,
		assessment.Relevance,
		assessment.EvidenceFidelity,
		assessment.WritingQuality,
	}
	for _, status := range statuses {
		if status != AgentTaskContentReviewPassed && status != AgentTaskContentReviewFailed {
			return fmt.Errorf("unsupported dimension status %q", status)
		}
	}
	return nil
}

func deriveAgentTaskContentReviewVerdict(cases []AgentTaskContentReviewCaseDecision, candidate bool) AgentTaskContentReviewVerdict {
	for _, reviewed := range cases {
		assessment := reviewed.Stable
		if candidate {
			assessment = reviewed.Candidate
		}
		if assessment.FactualCorrectness != AgentTaskContentReviewPassed ||
			assessment.Relevance != AgentTaskContentReviewPassed ||
			assessment.EvidenceFidelity != AgentTaskContentReviewPassed ||
			assessment.WritingQuality != AgentTaskContentReviewPassed {
			return AgentTaskContentReviewRejected
		}
	}
	return AgentTaskContentReviewApproved
}

func decisionFromAgentTaskContentReviewSignoff(signoff AgentTaskContentReviewSignoff) (AgentTaskContentReviewDecision, error) {
	if signoff.CreatedAt.IsZero() || !validSHA256(signoff.DecisionSHA256) {
		return AgentTaskContentReviewDecision{}, errors.New("content review signoff identity is incomplete")
	}
	cases := make([]AgentTaskContentReviewCaseDecision, len(signoff.Cases))
	for index, reviewed := range signoff.Cases {
		cases[index] = AgentTaskContentReviewCaseDecision{
			CaseID: reviewed.CaseID, Candidate: reviewed.Candidate, Stable: reviewed.Stable,
		}
	}
	return normalizeAgentTaskContentReviewDecision(AgentTaskContentReviewDecision{
		SchemaVersion:       AgentTaskContentReviewDecisionSchemaVersion,
		ReportPayloadSHA256: signoff.ReportPayloadSHA256,
		ReviewBundleSHA256:  signoff.ReviewBundleSHA256,
		RuleVersion:         signoff.RuleVersion,
		Reviewer:            signoff.Reviewer,
		ReviewedAt:          signoff.ReviewedAt,
		CandidateVerdict:    signoff.CandidateVerdict,
		StableVerdict:       signoff.StableVerdict,
		Cases:               cases,
		ReviewNotesSHA256:   signoff.ReviewNotesSHA256,
	}), nil
}

func normalizeAgentTaskContentReviewDecision(decision AgentTaskContentReviewDecision) AgentTaskContentReviewDecision {
	decision.SchemaVersion = strings.TrimSpace(decision.SchemaVersion)
	decision.ReportPayloadSHA256 = strings.ToLower(strings.TrimSpace(decision.ReportPayloadSHA256))
	decision.ReviewBundleSHA256 = strings.ToLower(strings.TrimSpace(decision.ReviewBundleSHA256))
	decision.RuleVersion = strings.TrimSpace(decision.RuleVersion)
	decision.Reviewer.Kind = AgentTaskContentReviewerKind(strings.TrimSpace(string(decision.Reviewer.Kind)))
	decision.Reviewer.ID = strings.TrimSpace(decision.Reviewer.ID)
	decision.Reviewer.IdentityAssurance = AgentTaskContentReviewerAssurance(strings.TrimSpace(string(decision.Reviewer.IdentityAssurance)))
	decision.Reviewer.ExternalRecordSHA256 = strings.ToLower(strings.TrimSpace(decision.Reviewer.ExternalRecordSHA256))
	if decision.Reviewer.Judge != nil {
		judge := *decision.Reviewer.Judge
		judge.Provider = strings.TrimSpace(judge.Provider)
		judge.Model = strings.TrimSpace(judge.Model)
		judge.PromptID = strings.TrimSpace(judge.PromptID)
		judge.PromptVersion = strings.TrimSpace(judge.PromptVersion)
		judge.ConfigSHA256 = strings.ToLower(strings.TrimSpace(judge.ConfigSHA256))
		decision.Reviewer.Judge = &judge
	}
	decision.ReviewedAt = decision.ReviewedAt.UTC()
	decision.CandidateVerdict = AgentTaskContentReviewVerdict(strings.TrimSpace(string(decision.CandidateVerdict)))
	decision.StableVerdict = AgentTaskContentReviewVerdict(strings.TrimSpace(string(decision.StableVerdict)))
	decision.ReviewNotesSHA256 = strings.ToLower(strings.TrimSpace(decision.ReviewNotesSHA256))
	decision.Cases = append([]AgentTaskContentReviewCaseDecision(nil), decision.Cases...)
	for index := range decision.Cases {
		decision.Cases[index].CaseID = strings.TrimSpace(decision.Cases[index].CaseID)
	}
	return decision
}

func unsignedAgentTaskContentReviewSignoffPayload(signoff AgentTaskContentReviewSignoff) ([]byte, error) {
	signoff.Integrity = nil
	payload, err := json.Marshal(signoff)
	if err != nil {
		return nil, fmt.Errorf("encode unsigned content review signoff: %w", err)
	}
	return payload, nil
}

func decodeStrictAgentTaskContentReviewJSON(payload []byte, target any, label string) error {
	return decodeBoundedEvaluationJSONPayload(payload, target, label)
}

func validAgentTaskContentReviewIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("._:/-", char) {
			continue
		}
		return false
	}
	return true
}

func validAgentTaskContentReviewLabel(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func validAgentTaskContentReviewDigest(value string) bool {
	value = strings.TrimSpace(value)
	return validSHA256(value) && strings.Trim(value, "0") != ""
}
