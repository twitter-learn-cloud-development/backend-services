package eval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const AgentTaskContentQualifiedEvidenceSchemaVersion = "agent-task-content-qualified-evidence/v1"

// AgentTaskContentQualifiedEvidence is the immutable release evidence stored in
// WORM storage. The encrypted review bundle stays outside the serving process;
// its exact file digest and key identity are bound by ContentReviewSignoff.
type AgentTaskContentQualifiedEvidence struct {
	SchemaVersion        string                        `json:"schema_version"`
	Report               AgentTaskEvaluationOutput     `json:"report"`
	ContentReviewSignoff AgentTaskContentReviewSignoff `json:"content_review_signoff"`
}

func BuildAgentTaskContentQualifiedEvidence(
	output AgentTaskEvaluationOutput,
	signoff AgentTaskContentReviewSignoff,
	reportKey []byte,
	reportKeyID string,
	signoffKey []byte,
	signoffKeyID string,
) (AgentTaskContentQualifiedEvidence, error) {
	evidence := AgentTaskContentQualifiedEvidence{
		SchemaVersion:        AgentTaskContentQualifiedEvidenceSchemaVersion,
		Report:               output,
		ContentReviewSignoff: signoff,
	}
	if err := VerifyAgentTaskContentQualifiedEvidence(
		evidence, reportKey, reportKeyID, signoffKey, signoffKeyID,
	); err != nil {
		return AgentTaskContentQualifiedEvidence{}, err
	}
	return evidence, nil
}

func VerifyAgentTaskContentQualifiedEvidence(
	evidence AgentTaskContentQualifiedEvidence,
	reportKey []byte,
	reportKeyID string,
	signoffKey []byte,
	signoffKeyID string,
) error {
	if evidence.SchemaVersion != AgentTaskContentQualifiedEvidenceSchemaVersion {
		return fmt.Errorf("unsupported content-qualified evidence schema version %q", evidence.SchemaVersion)
	}
	reportKeyID = strings.TrimSpace(reportKeyID)
	signoffKeyID = strings.TrimSpace(signoffKeyID)
	if reportKeyID == "" || signoffKeyID == "" {
		return errors.New("content-qualified evidence trusted key IDs are required")
	}
	if reportKeyID == signoffKeyID || bytes.Equal(reportKey, signoffKey) {
		return errors.New("content-qualified evidence report and signoff keys must be independent")
	}
	if err := VerifyAgentTaskEvaluationOutput(evidence.Report, reportKey, reportKeyID); err != nil {
		return fmt.Errorf("verify content-qualified evaluation report: %w", err)
	}
	binding := AgentTaskContentReviewBundleBinding{
		SchemaVersion: evidence.ContentReviewSignoff.ReviewBundleSchemaVersion,
		KeyID:         evidence.ContentReviewSignoff.ReviewBundleKeyID,
		FileSHA256:    evidence.ContentReviewSignoff.ReviewBundleSHA256,
	}
	if err := VerifyAgentTaskContentReviewSignoff(
		evidence.ContentReviewSignoff,
		evidence.Report,
		binding,
		signoffKey,
		signoffKeyID,
	); err != nil {
		return fmt.Errorf("verify content-qualified review signoff: %w", err)
	}
	if !AgentTaskContentReviewHasApprovedExternalHumanSignoff(evidence.ContentReviewSignoff) {
		return errors.New("content-qualified evidence requires an approved external human signoff")
	}
	return nil
}

func MarshalAgentTaskContentQualifiedEvidence(evidence AgentTaskContentQualifiedEvidence) ([]byte, error) {
	payload, err := json.Marshal(evidence)
	if err != nil {
		return nil, fmt.Errorf("encode content-qualified evidence: %w", err)
	}
	return payload, nil
}

func DecodeAgentTaskContentQualifiedEvidence(payload []byte) (AgentTaskContentQualifiedEvidence, error) {
	var evidence AgentTaskContentQualifiedEvidence
	if err := decodeBoundedEvaluationJSONPayload(payload, &evidence, "content-qualified evidence"); err != nil {
		return AgentTaskContentQualifiedEvidence{}, err
	}
	return evidence, nil
}

func DecodeAndVerifyAgentTaskContentQualifiedEvidence(
	payload []byte,
	reportKey []byte,
	reportKeyID string,
	signoffKey []byte,
	signoffKeyID string,
) (AgentTaskContentQualifiedEvidence, error) {
	evidence, err := DecodeAgentTaskContentQualifiedEvidence(payload)
	if err != nil {
		return AgentTaskContentQualifiedEvidence{}, err
	}
	if err := VerifyAgentTaskContentQualifiedEvidence(
		evidence, reportKey, reportKeyID, signoffKey, signoffKeyID,
	); err != nil {
		return AgentTaskContentQualifiedEvidence{}, err
	}
	return evidence, nil
}
