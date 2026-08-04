package eval

import (
	"context"
	"fmt"
	"io"
	"strings"
)

type RecordedAgentTaskResult struct {
	CaseID    string             `json:"case_id"`
	Execution AgentTaskExecution `json:"execution"`
}

type RecordedAgentTaskResultSet struct {
	Version             string                    `json:"version"`
	Strategy            string                    `json:"strategy,omitempty"`
	Provider            string                    `json:"provider,omitempty"`
	Model               string                    `json:"model,omitempty"`
	ProfileID           string                    `json:"profile_id,omitempty"`
	ProfileVersion      string                    `json:"profile_version,omitempty"`
	PricingVersion      string                    `json:"pricing_version,omitempty"`
	ExecutionConfigHash string                    `json:"execution_config_sha256,omitempty"`
	Results             []RecordedAgentTaskResult `json:"results"`
}

func LoadRecordedAgentTaskResults(reader io.Reader) (RecordedAgentTaskResultSet, error) {
	if reader == nil {
		return RecordedAgentTaskResultSet{}, fmt.Errorf("recorded agent task result reader is nil")
	}
	var resultSet RecordedAgentTaskResultSet
	if _, err := decodeBoundedEvaluationJSON(reader, &resultSet, "recorded agent task results"); err != nil {
		return RecordedAgentTaskResultSet{}, err
	}
	if err := validateEvaluationCaseCount(len(resultSet.Results), "recorded agent task results"); err != nil {
		return RecordedAgentTaskResultSet{}, err
	}
	resultSet.Version = strings.TrimSpace(resultSet.Version)
	resultSet.Strategy = strings.TrimSpace(resultSet.Strategy)
	resultSet.Provider = strings.TrimSpace(resultSet.Provider)
	resultSet.Model = strings.TrimSpace(resultSet.Model)
	resultSet.ProfileID = strings.TrimSpace(resultSet.ProfileID)
	resultSet.ProfileVersion = strings.TrimSpace(resultSet.ProfileVersion)
	resultSet.PricingVersion = strings.TrimSpace(resultSet.PricingVersion)
	resultSet.ExecutionConfigHash = strings.ToLower(strings.TrimSpace(resultSet.ExecutionConfigHash))
	if resultSet.Version == "" {
		return RecordedAgentTaskResultSet{}, fmt.Errorf("recorded agent task result version is required")
	}
	for label, value := range map[string]string{
		"version": resultSet.Version, "strategy": resultSet.Strategy, "provider": resultSet.Provider,
		"model": resultSet.Model, "profile_id": resultSet.ProfileID,
		"profile_version": resultSet.ProfileVersion, "pricing_version": resultSet.PricingVersion,
	} {
		if value != "" && len([]rune(value)) > maxEvaluationIdentifierRunes {
			return RecordedAgentTaskResultSet{}, fmt.Errorf("recorded agent task result %s exceeds %d characters", label, maxEvaluationIdentifierRunes)
		}
	}
	if resultSet.ExecutionConfigHash != "" && !validSHA256(resultSet.ExecutionConfigHash) {
		return RecordedAgentTaskResultSet{}, fmt.Errorf("recorded agent task execution_config_sha256 must be a SHA-256 digest")
	}
	seen := make(map[string]struct{}, len(resultSet.Results))
	for index := range resultSet.Results {
		item := &resultSet.Results[index]
		item.CaseID = strings.TrimSpace(item.CaseID)
		if item.CaseID == "" {
			return RecordedAgentTaskResultSet{}, fmt.Errorf("recorded agent task result %d has empty case_id", index)
		}
		if len([]rune(item.CaseID)) > maxEvaluationIdentifierRunes {
			return RecordedAgentTaskResultSet{}, fmt.Errorf("recorded agent task result %d case_id exceeds %d characters", index, maxEvaluationIdentifierRunes)
		}
		if _, ok := seen[item.CaseID]; ok {
			return RecordedAgentTaskResultSet{}, fmt.Errorf("recorded agent task results contain duplicate case_id %q", item.CaseID)
		}
		seen[item.CaseID] = struct{}{}
		normalized, err := normalizeAgentTaskExecution(item.Execution)
		if err != nil {
			return RecordedAgentTaskResultSet{}, fmt.Errorf("recorded agent task result %q: %w", item.CaseID, err)
		}
		if resultSet.PricingVersion != "" && normalized.PricingVersion != "" && normalized.PricingVersion != resultSet.PricingVersion {
			return RecordedAgentTaskResultSet{}, fmt.Errorf("recorded agent task result %q pricing version %q does not match result set %q", item.CaseID, normalized.PricingVersion, resultSet.PricingVersion)
		}
		item.Execution = normalized
	}
	return resultSet, nil
}

type RecordedAgentTaskExecutor struct {
	results map[string]AgentTaskExecution
}

func NewRecordedAgentTaskExecutor(resultSet RecordedAgentTaskResultSet) (*RecordedAgentTaskExecutor, error) {
	results := make(map[string]AgentTaskExecution, len(resultSet.Results))
	for _, item := range resultSet.Results {
		if _, ok := results[item.CaseID]; ok {
			return nil, fmt.Errorf("duplicate recorded agent task result %q", item.CaseID)
		}
		results[item.CaseID] = cloneAgentTaskExecution(item.Execution)
	}
	return &RecordedAgentTaskExecutor{results: results}, nil
}

func (e *RecordedAgentTaskExecutor) Execute(ctx context.Context, sample AgentTaskCase) (AgentTaskExecution, error) {
	if err := ctx.Err(); err != nil {
		return AgentTaskExecution{}, err
	}
	if e == nil {
		return AgentTaskExecution{}, fmt.Errorf("recorded agent task executor is nil")
	}
	result, ok := e.results[sample.ID]
	if !ok {
		return AgentTaskExecution{}, fmt.Errorf("recorded agent task result %q not found", sample.ID)
	}
	return cloneAgentTaskExecution(result), nil
}

func (s RecordedAgentTaskResultSet) Descriptor() AgentTaskExecutionDescriptor {
	return AgentTaskExecutionDescriptor{
		Kind:           "recorded_fixture",
		Version:        s.Version,
		Strategy:       strings.TrimSpace(s.Strategy),
		Provider:       strings.TrimSpace(s.Provider),
		Model:          strings.TrimSpace(s.Model),
		ProfileID:      strings.TrimSpace(s.ProfileID),
		ProfileVersion: strings.TrimSpace(s.ProfileVersion),
		PricingVersion: strings.TrimSpace(s.PricingVersion),
	}
}

func cloneAgentTaskExecution(input AgentTaskExecution) AgentTaskExecution {
	result := input
	result.SelectedTools = append([]string(nil), input.SelectedTools...)
	result.ToolCalls = append([]AgentTaskToolCall(nil), input.ToolCalls...)
	result.ClaimedExecutedTools = append([]string(nil), input.ClaimedExecutedTools...)
	return result
}

func validAgentToolCallStatus(status AgentToolCallStatus) bool {
	switch status {
	case AgentToolCallSucceeded, AgentToolCallFailed, AgentToolCallApprovalRequired, AgentToolCallDenied:
		return true
	default:
		return false
	}
}
