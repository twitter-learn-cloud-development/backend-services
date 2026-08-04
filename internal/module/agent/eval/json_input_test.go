package eval

import (
	"bytes"
	"strings"
	"testing"
)

func TestEvaluationJSONLoadersRejectAmbiguousOrUnboundedInputs(t *testing.T) {
	tests := []struct {
		name    string
		load    func(string) error
		payload string
		want    string
	}{
		{
			name: "agent task unknown field",
			load: func(payload string) error {
				_, err := LoadAgentTaskDataset(strings.NewReader(payload))
				return err
			},
			payload: `[{"id":"case-1","category":"chat","mode":"chat","input":"hello","expected_outcome":"completed","expected_outcome_typo":"failed"}]`,
			want:    "unknown field",
		},
		{
			name: "recorded results trailing value",
			load: func(payload string) error {
				_, err := LoadRecordedAgentTaskResults(strings.NewReader(payload))
				return err
			},
			payload: `{"version":"v1","results":[{"case_id":"case-1","execution":{"outcome":"completed"}}]} {}`,
			want:    "multiple JSON values",
		},
		{
			name: "retrieval duplicate ID",
			load: func(payload string) error {
				_, err := LoadDataset(strings.NewReader(payload))
				return err
			},
			payload: `[{"id":"same","query":"one","category":"a","relevant_ids":[]},{"id":"same","query":"two","category":"b","relevant_ids":[]}]`,
			want:    "duplicate id",
		},
		{
			name: "router unknown field",
			load: func(payload string) error {
				_, err := LoadRouterDataset(strings.NewReader(payload))
				return err
			},
			payload: `[{"id":"case-1","query":"hello","expected_intent":"default","expected_intent_typo":"episodic"}]`,
			want:    "unknown field",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.load(test.payload)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("load error = %v, want containing %q", err, test.want)
			}
		})
	}

	_, err := LoadAgentTaskDataset(strings.NewReader(strings.Repeat(" ", maxEvaluationJSONBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized dataset error = %v, want size rejection", err)
	}

	deeplyNested := strings.Repeat("[", maxEvaluationJSONDepth+2) + "0" + strings.Repeat("]", maxEvaluationJSONDepth+2)
	_, err = LoadAgentTaskDataset(strings.NewReader(deeplyNested))
	if err == nil || !strings.Contains(err.Error(), "nesting exceeds") {
		t.Fatalf("deeply nested dataset error = %v, want depth rejection", err)
	}
}

func TestNormalizeAgentTaskExecutionRejectsUnboundedEvidence(t *testing.T) {
	_, err := normalizeAgentTaskExecution(AgentTaskExecution{
		Outcome: AgentTaskOutcomeCompleted,
		Output:  strings.Repeat("x", maxAgentTaskOutputRunes+1),
	})
	if err == nil || !strings.Contains(err.Error(), "output exceeds") {
		t.Fatalf("oversized output error = %v, want size rejection", err)
	}

	_, err = normalizeAgentTaskExecution(AgentTaskExecution{
		Outcome:     AgentTaskOutcomeCompleted,
		InputTokens: maxAgentTaskTokens + 1,
	})
	if err == nil || !strings.Contains(err.Error(), "hard limits") {
		t.Fatalf("oversized usage error = %v, want hard-limit rejection", err)
	}
}

func TestEvaluationEvidenceDecodersShareStrictPayloadBounds(t *testing.T) {
	oversized := bytes.Repeat([]byte{' '}, maxEvaluationJSONBytes+1)
	decoders := []struct {
		name   string
		decode func([]byte) error
	}{
		{
			name: "evaluation report",
			decode: func(payload []byte) error {
				_, err := DecodeAgentTaskEvaluationOutput(payload)
				return err
			},
		},
		{
			name: "content review decision",
			decode: func(payload []byte) error {
				_, err := DecodeAgentTaskContentReviewDecision(payload)
				return err
			},
		},
		{
			name: "content review signoff",
			decode: func(payload []byte) error {
				_, err := DecodeAgentTaskContentReviewSignoff(payload)
				return err
			},
		},
		{
			name: "content-qualified evidence",
			decode: func(payload []byte) error {
				_, err := DecodeAgentTaskContentQualifiedEvidence(payload)
				return err
			},
		},
	}

	for _, decoder := range decoders {
		t.Run(decoder.name, func(t *testing.T) {
			err := decoder.decode(oversized)
			if err == nil || !strings.Contains(err.Error(), "exceeds") {
				t.Fatalf("oversized payload error = %v, want size rejection", err)
			}
		})
	}

	_, err := DecodeAgentTaskEvaluationOutput([]byte(`{"schema_version":"v1","schema_version":"v2"}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("duplicate evaluation report key error = %v, want duplicate-key rejection", err)
	}

	_, err = DecodeAgentTaskContentReviewDecision([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})
	if err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Fatalf("invalid UTF-8 content review error = %v, want UTF-8 rejection", err)
	}
}
