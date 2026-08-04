package eval

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestLoadAgentTaskDatasetNormalizesEvidenceContract(t *testing.T) {
	dataset, err := LoadAgentTaskDataset(strings.NewReader(`[
  {
    "id": "grounded-search",
    "category": "external_search",
    "mode": "consult",
    "input": "summarize the release",
    "expected_outcome": "completed",
    "expected_tools": ["web_search"],
    "read_tool_case": true,
    "evidence": {
      "status": "sufficient",
      "items": [{
        "citation_id": "REL-1",
        "source_id": "release-1",
        "url": "https://example.com/release",
        "title": " Release notes ",
        "content": "Version 3.2 adds audit replay and policy snapshots."
      }],
      "required_claims": [{
        "id": "release-capabilities",
        "terms": ["audit replay", "policy snapshots"],
        "evidence_ids": ["rel-1"]
      }]
    }
  }
]`))
	if err != nil {
		t.Fatalf("LoadAgentTaskDataset() error = %v", err)
	}
	if len(dataset) != 1 || dataset[0].Evidence == nil {
		t.Fatalf("dataset = %+v", dataset)
	}
	if dataset[0].Evidence.Items[0].Title != "Release notes" {
		t.Fatalf("title = %q", dataset[0].Evidence.Items[0].Title)
	}
	if dataset[0].Evidence.RequiredClaims[0].EvidenceIDs[0] != "REL-1" {
		t.Fatalf("canonical evidence ID = %q", dataset[0].Evidence.RequiredClaims[0].EvidenceIDs[0])
	}
}

func TestLoadAgentTaskDatasetRejectsInvalidEvidenceContract(t *testing.T) {
	tests := []struct {
		name      string
		evidence  string
		readCase  bool
		wantError string
	}{
		{
			name: "unknown citation",
			evidence: `{
        "status":"sufficient",
        "items":[{"citation_id":"SRC-1","source_id":"1","content":"audit replay"}],
        "required_claims":[{"id":"claim-1","terms":["audit replay"],"evidence_ids":["SRC-2"]}]
      }`,
			readCase:  true,
			wantError: "unknown evidence_id",
		},
		{
			name: "ungrounded claim",
			evidence: `{
        "status":"sufficient",
        "items":[{"citation_id":"SRC-1","source_id":"1","content":"audit replay"}],
        "required_claims":[{"id":"claim-1","terms":["policy snapshots"],"evidence_ids":["SRC-1"]}]
      }`,
			readCase:  true,
			wantError: "not jointly grounded",
		},
		{
			name: "insufficient with evidence",
			evidence: `{
        "status":"insufficient",
        "items":[{"citation_id":"SRC-1","source_id":"1","content":"audit replay"}],
        "insufficient_output_any_of":["insufficient evidence"]
      }`,
			readCase:  true,
			wantError: "cannot define items",
		},
		{
			name: "evidence on non read case",
			evidence: `{
        "status":"insufficient",
        "insufficient_output_any_of":["insufficient evidence"]
      }`,
			wantError: "without read_tool_case",
		},
		{
			name: "web evidence missing URL",
			evidence: `{
        "status":"sufficient",
        "items":[{"citation_id":"SRC-1","source_id":"1","content":"audit replay"}],
        "required_claims":[{"id":"claim-1","terms":["audit replay"],"evidence_ids":["SRC-1"]}]
      }`,
			readCase:  true,
			wantError: "requires a URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readToolCase := "false"
			if tt.readCase {
				readToolCase = "true"
			}
			payload := `[{"id":"case","category":"search","mode":"consult","input":"query","expected_outcome":"completed","expected_tools":["web_search"],"read_tool_case":` + readToolCase + `,"evidence":` + tt.evidence + `}]`
			_, err := LoadAgentTaskDataset(strings.NewReader(payload))
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("LoadAgentTaskDataset() error = %v, want containing %q", err, tt.wantError)
			}
		})
	}
}

func TestLoadAgentTaskDatasetRejectsNonNumericPlatformEvidenceSource(t *testing.T) {
	_, err := LoadAgentTaskDataset(strings.NewReader(`[{
    "id":"case","category":"search","mode":"assist","input":"query",
    "expected_outcome":"completed","expected_tools":["hybrid_search_tweets"],"read_tool_case":true,
    "evidence":{
      "status":"sufficient",
      "items":[{"citation_id":"SRC-1","source_id":"tweet-alpha","content":"audit replay"}],
      "required_claims":[{"id":"claim-1","terms":["audit replay"],"evidence_ids":["SRC-1"]}]
    }
  }]`))
	if err == nil || !strings.Contains(err.Error(), "numeric source_id") {
		t.Fatalf("LoadAgentTaskDataset() error = %v", err)
	}
}

func TestEvaluateAgentTaskEvidenceAssertionsForSufficientEvidence(t *testing.T) {
	contract := mustNormalizeAgentTaskEvidenceContract(t, &AgentTaskEvidenceContract{
		Status: AgentTaskEvidenceSufficient,
		Items: []AgentTaskEvidenceItem{{
			CitationID: "OBS-1", SourceID: "1",
			Content: "OpenTelemetry correlates traces with policy decisions.",
		}},
		RequiredClaims: []AgentTaskRequiredClaim{{
			ID: "observability", Terms: []string{"OpenTelemetry", "policy decisions"}, EvidenceIDs: []string{"OBS-1"},
		}},
		RefusalPhrases:          []string{"无法回答"},
		UnsupportedClaimPhrases: []string{"guarantees zero incidents"},
		ForbiddenMetadata:       []string{"controlled-eval"},
	})

	tests := []struct {
		name        string
		output      string
		wantFailure string
	}{
		{name: "grounded", output: "OpenTelemetry 可关联 policy decisions。[OBS-1]"},
		{name: "missing claim", output: "只有引用。[OBS-1]", wantFailure: "missing_required_claim"},
		{name: "missing citation", output: "OpenTelemetry 可关联 policy decisions。", wantFailure: "missing_required_citation"},
		{name: "unlinked citation", output: "[OBS-1]" + strings.Repeat("填", 300) + "OpenTelemetry 可关联 policy decisions。", wantFailure: "claim_not_linked_to_citation"},
		{name: "refuses sufficient evidence", output: "无法回答。OpenTelemetry 可关联 policy decisions。[OBS-1]", wantFailure: "rejects_sufficient_evidence"},
		{name: "unsupported claim", output: "OpenTelemetry guarantees zero incidents and supports policy decisions。[OBS-1]", wantFailure: "contains_unsupported_claim"},
		{name: "metadata leak", output: "controlled-eval: OpenTelemetry supports policy decisions。[OBS-1]", wantFailure: "contains_internal_metadata"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failures := evaluateAgentTaskEvidenceAssertions(*contract, tt.output)
			if tt.wantFailure == "" {
				if len(failures) != 0 {
					t.Fatalf("failures = %v", failures)
				}
				return
			}
			if !containsAgentTaskFailure(failures, tt.wantFailure) {
				t.Fatalf("failures = %v, want %q", failures, tt.wantFailure)
			}
		})
	}
}

func TestEvaluateAgentTaskEvidenceAssertionsForInsufficientEvidence(t *testing.T) {
	contract := mustNormalizeAgentTaskEvidenceContract(t, &AgentTaskEvidenceContract{
		Status:                  AgentTaskEvidenceInsufficient,
		InsufficientOutputAnyOf: []string{"未检索到可靠证据", "insufficient evidence"},
		UnsupportedClaimPhrases: []string{"confirmed release date"},
	})
	if failures := evaluateAgentTaskEvidenceAssertions(*contract, "未检索到可靠证据，暂时无法给出结论。"); len(failures) != 0 {
		t.Fatalf("failures = %v", failures)
	}
	failures := evaluateAgentTaskEvidenceAssertions(*contract, "The confirmed release date is tomorrow.")
	if !containsAgentTaskFailure(failures, "missing_insufficient_evidence_notice") || !containsAgentTaskFailure(failures, "contains_unsupported_claim") {
		t.Fatalf("failures = %v", failures)
	}
}

func TestAgentStrategyV3DatasetContainsSubstantiveAndInsufficientEvidence(t *testing.T) {
	file, err := os.Open("testdata/agent_strategy_cases_v3.json")
	if err != nil {
		t.Fatalf("open v3 dataset: %v", err)
	}
	defer file.Close()
	dataset, err := LoadAgentTaskDataset(file)
	if err != nil {
		t.Fatalf("LoadAgentTaskDataset() error = %v", err)
	}
	if len(dataset) != 20 {
		t.Fatalf("dataset size = %d, want 20", len(dataset))
	}
	sufficient := 0
	insufficient := 0
	for _, sample := range dataset {
		if sample.Evidence == nil {
			t.Fatalf("case %q has no evidence contract", sample.ID)
		}
		switch sample.Evidence.Status {
		case AgentTaskEvidenceSufficient:
			sufficient++
			if len(sample.Evidence.Items) == 0 || len(sample.Evidence.RequiredClaims) < 2 {
				t.Fatalf("case %q has weak sufficient evidence", sample.ID)
			}
		case AgentTaskEvidenceInsufficient:
			insufficient++
		default:
			t.Fatalf("case %q status = %q", sample.ID, sample.Evidence.Status)
		}
	}
	if sufficient != 16 || insufficient != 4 {
		t.Fatalf("sufficient = %d, insufficient = %d", sufficient, insufficient)
	}
}

func TestAgentStrategyV3DatasetPassesWithGroundedFakeExecutor(t *testing.T) {
	file, err := os.Open("testdata/agent_strategy_cases_v3.json")
	if err != nil {
		t.Fatalf("open v3 dataset: %v", err)
	}
	defer file.Close()
	dataset, err := LoadAgentTaskDataset(file)
	if err != nil {
		t.Fatalf("LoadAgentTaskDataset() error = %v", err)
	}
	report, err := RunAgentTasks(context.Background(), dataset, agentTaskExecutorFunc(func(_ context.Context, sample AgentTaskCase) (AgentTaskExecution, error) {
		output := groundedAgentTaskFakeOutput(sample)
		calls := make([]AgentTaskToolCall, 0, len(sample.ExpectedTools))
		for _, tool := range sample.ExpectedTools {
			calls = append(calls, AgentTaskToolCall{Name: tool, Status: AgentToolCallSucceeded})
		}
		return AgentTaskExecution{
			Outcome: sample.ExpectedOutcome, Output: output,
			SelectedTools: append([]string(nil), sample.ExpectedTools...), ToolCalls: calls,
			ClaimedExecutedTools: append([]string(nil), sample.ExpectedTools...),
			Steps:                1, InputTokens: 100, OutputTokens: 100, DurationMS: 10,
		}, nil
	}), AgentTaskRunnerConfig{
		DatasetVersion: "agent-strategy-cases-v3", CaseTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("RunAgentTasks() error = %v", err)
	}
	if report.Metrics.Cases != 20 || report.Metrics.Passed != 20 || report.Metrics.SemanticPassed != 20 {
		t.Fatalf("metrics = %+v", report.Metrics)
	}
}

func groundedAgentTaskFakeOutput(sample AgentTaskCase) string {
	if sample.Evidence.Status == AgentTaskEvidenceInsufficient {
		return sample.Evidence.InsufficientOutputAnyOf[0] + "，因此当前不能给出确定结论。"
	}
	parts := make([]string, 0, len(sample.Evidence.RequiredClaims)+2)
	for _, claim := range sample.Evidence.RequiredClaims {
		parts = append(parts, strings.Join(claim.Terms, "、")+"。 ["+claim.EvidenceIDs[0]+"]")
	}
	output := strings.Join(parts, " ")
	for utf8.RuneCountInString(output) < sample.MinOutputCharacters {
		output += " 以上结论仅依据当前可核验材料，并保留其指标、单位与适用边界。"
	}
	return output
}

func mustNormalizeAgentTaskEvidenceContract(t *testing.T, input *AgentTaskEvidenceContract) *AgentTaskEvidenceContract {
	t.Helper()
	contract, err := normalizeAgentTaskEvidenceContract(input)
	if err != nil {
		t.Fatalf("normalizeAgentTaskEvidenceContract() error = %v", err)
	}
	return contract
}

func containsAgentTaskFailure(failures []string, expected string) bool {
	for _, failure := range failures {
		if failure == expected {
			return true
		}
	}
	return false
}
