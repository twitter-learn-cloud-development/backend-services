package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AgentTaskEvaluationSchemaVersion        = "agent-task-eval-report/v2"
	AgentTaskLiveAuthorizationSchemaVersion = "agent-task-live-authorization/v1"
	maxAgentTaskLiveAuthorizationEvents     = 100_000
	maxAgentTaskLiveAuthorizationCostMicros = int64(1_000_000_000_000)
)

type AgentTaskLiveAuthorizationLimits struct {
	MaxRuns                int   `json:"max_runs"`
	MaxProviderCalls       int   `json:"max_provider_calls"`
	MaxCapturedOutputs     int   `json:"max_captured_outputs"`
	MaxEstimatedCostMicros int64 `json:"max_estimated_cost_micros"`
}

type AgentTaskLiveAuthorizationEvidence struct {
	SchemaVersion              string                           `json:"schema_version"`
	AuthorizationID            string                           `json:"authorization_id"`
	AuthorizationPayloadSHA256 string                           `json:"authorization_payload_sha256"`
	AuthorizationKeyID         string                           `json:"authorization_key_id"`
	InvocationSHA256           string                           `json:"invocation_sha256"`
	StateBackend               string                           `json:"state_backend,omitempty"`
	StateNamespaceSHA256       string                           `json:"state_namespace_sha256,omitempty"`
	Limits                     AgentTaskLiveAuthorizationLimits `json:"limits"`
}

type AgentTaskEvaluationOutput struct {
	SchemaVersion     string                              `json:"schema_version"`
	Candidate         AgentTaskReport                     `json:"candidate"`
	Stable            *AgentTaskReport                    `json:"stable,omitempty"`
	Gate              *AgentQualityGateDecision           `json:"gate,omitempty"`
	StrategyGate      *AgentStrategyGateDecision          `json:"strategy_gate,omitempty"`
	LiveAuthorization *AgentTaskLiveAuthorizationEvidence `json:"live_authorization,omitempty"`
	Integrity         *AgentTaskReportIntegrity           `json:"integrity,omitempty"`
}

func SignAgentTaskEvaluationOutput(output *AgentTaskEvaluationOutput, key []byte, keyID string, signedAt time.Time) error {
	if output == nil {
		return errors.New("agent task evaluation output is nil")
	}
	if err := validateAgentTaskLiveAuthorizationEvidence(output.LiveAuthorization); err != nil {
		return err
	}
	payload, err := unsignedAgentTaskEvaluationPayload(*output)
	if err != nil {
		return err
	}
	integrity, err := SignAgentTaskPayload(payload, key, keyID, signedAt)
	if err != nil {
		return err
	}
	output.Integrity = &integrity
	return nil
}

func VerifyAgentTaskEvaluationOutput(output AgentTaskEvaluationOutput, key []byte, trustedKeyID string) error {
	if output.SchemaVersion != AgentTaskEvaluationSchemaVersion {
		return fmt.Errorf("unsupported report schema version %q", output.SchemaVersion)
	}
	if output.Integrity == nil {
		return errors.New("agent task evaluation report is unsigned")
	}
	if err := validateAgentTaskLiveAuthorizationEvidence(output.LiveAuthorization); err != nil {
		return err
	}
	if trustedKeyID = strings.TrimSpace(trustedKeyID); trustedKeyID != "" && output.Integrity.KeyID != trustedKeyID {
		return fmt.Errorf("report key ID %q does not match trusted key ID %q", output.Integrity.KeyID, trustedKeyID)
	}
	payload, err := unsignedAgentTaskEvaluationPayload(output)
	if err != nil {
		return err
	}
	return VerifyAgentTaskPayload(payload, key, *output.Integrity)
}

func validateAgentTaskLiveAuthorizationEvidence(evidence *AgentTaskLiveAuthorizationEvidence) error {
	if evidence == nil {
		return nil
	}
	if evidence.SchemaVersion != AgentTaskLiveAuthorizationSchemaVersion {
		return fmt.Errorf("unsupported live authorization evidence schema version %q", evidence.SchemaVersion)
	}
	authorizationID := strings.TrimSpace(evidence.AuthorizationID)
	keyID := strings.TrimSpace(evidence.AuthorizationKeyID)
	authorizationHash := strings.ToLower(strings.TrimSpace(evidence.AuthorizationPayloadSHA256))
	invocationHash := strings.ToLower(strings.TrimSpace(evidence.InvocationSHA256))
	if authorizationID == "" || authorizationID != evidence.AuthorizationID || len(authorizationID) > 128 ||
		keyID == "" || keyID != evidence.AuthorizationKeyID || len(keyID) > 128 ||
		authorizationHash != evidence.AuthorizationPayloadSHA256 || !validSHA256(authorizationHash) ||
		invocationHash != evidence.InvocationSHA256 || !validSHA256(invocationHash) {
		return errors.New("live authorization evidence identity is invalid")
	}
	stateBackend := strings.TrimSpace(evidence.StateBackend)
	stateNamespaceHash := strings.ToLower(strings.TrimSpace(evidence.StateNamespaceSHA256))
	if (stateBackend == "") != (stateNamespaceHash == "") {
		return errors.New("live authorization state evidence must provide both backend and namespace identity")
	}
	if stateBackend != "" && (stateBackend != "redis" || stateBackend != evidence.StateBackend ||
		stateNamespaceHash != evidence.StateNamespaceSHA256 || !validSHA256(stateNamespaceHash)) {
		return errors.New("live authorization state evidence is invalid")
	}
	limits := evidence.Limits
	if limits.MaxRuns < 1 || limits.MaxRuns > 100 ||
		limits.MaxProviderCalls < 1 || limits.MaxProviderCalls > maxAgentTaskLiveAuthorizationEvents ||
		limits.MaxCapturedOutputs < 0 || limits.MaxCapturedOutputs > maxAgentTaskLiveAuthorizationEvents ||
		limits.MaxEstimatedCostMicros < 0 || limits.MaxEstimatedCostMicros > maxAgentTaskLiveAuthorizationCostMicros {
		return errors.New("live authorization evidence limits are invalid")
	}
	return nil
}

func MarshalAgentTaskEvaluationOutput(output AgentTaskEvaluationOutput) ([]byte, error) {
	payload, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("encode agent task evaluation report: %w", err)
	}
	return payload, nil
}

func DecodeAgentTaskEvaluationOutput(payload []byte) (AgentTaskEvaluationOutput, error) {
	var output AgentTaskEvaluationOutput
	if err := decodeBoundedEvaluationJSONPayload(payload, &output, "evaluation report"); err != nil {
		return AgentTaskEvaluationOutput{}, err
	}
	return output, nil
}

func DecodeAndVerifyAgentTaskEvaluationOutput(payload, key []byte, trustedKeyID string) (AgentTaskEvaluationOutput, error) {
	output, err := DecodeAgentTaskEvaluationOutput(payload)
	if err != nil {
		return AgentTaskEvaluationOutput{}, err
	}
	if err := VerifyAgentTaskEvaluationOutput(output, key, trustedKeyID); err != nil {
		return AgentTaskEvaluationOutput{}, err
	}
	return output, nil
}

func unsignedAgentTaskEvaluationPayload(output AgentTaskEvaluationOutput) ([]byte, error) {
	output.Integrity = nil
	payload, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("encode unsigned evaluation report: %w", err)
	}
	return payload, nil
}
