package eval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	maxEvaluationJSONBytes       = 8 << 20
	maxEvaluationJSONDepth       = 128
	maxEvaluationCases           = 10_000
	maxEvaluationIdentifierRunes = 256
	maxEvaluationTextRunes       = 65_536
	maxEvaluationListItems       = 256
	maxEvaluationListValueRunes  = 4_096
	maxAgentTaskOutputRunes      = 1_000_000
	maxAgentTaskSteps            = 100_000
	maxAgentTaskTokens           = 10_000_000
	maxAgentTaskToolCalls        = 10_000
)

func decodeBoundedEvaluationJSON(reader io.Reader, target any, label string) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("%s reader is nil", label)
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxEvaluationJSONBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if err := decodeBoundedEvaluationJSONPayload(payload, target, label); err != nil {
		return nil, err
	}
	return payload, nil
}

func decodeBoundedEvaluationJSONPayload(payload []byte, target any, label string) error {
	if len(payload) > maxEvaluationJSONBytes {
		return fmt.Errorf("%s exceeds %d bytes", label, maxEvaluationJSONBytes)
	}
	if !utf8.Valid(payload) {
		return fmt.Errorf("%s is not valid UTF-8", label)
	}
	if err := validateUniqueAgentTaskJSONKeys(payload); err != nil {
		return fmt.Errorf("validate %s JSON: %w", label, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%s contains multiple JSON values", label)
		}
		return fmt.Errorf("decode %s trailer: %w", label, err)
	}
	return nil
}

func validateEvaluationCaseCount(count int, label string) error {
	if count < 1 || count > maxEvaluationCases {
		return fmt.Errorf("%s must contain 1..%d cases", label, maxEvaluationCases)
	}
	return nil
}

func validateEvaluationString(value, label string, maxRunes int) error {
	if value == "" {
		return errors.New(label + " is required")
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s exceeds %d characters", label, maxRunes)
	}
	return nil
}
