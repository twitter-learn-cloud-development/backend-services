package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/eval"
)

func TestContentReviewDecisionTemplateIsBoundAndFailClosed(t *testing.T) {
	output, _, _, _ := contentQualifiedArchiveFixture(t, time.Now().UTC())
	binding := eval.AgentTaskContentReviewBundleBinding{
		SchemaVersion: eval.AgentTaskReviewBundleSchemaVersion,
		KeyID:         "review-key-v1",
		FileSHA256:    strings.Repeat("d", 64),
	}
	template, err := buildAgentTaskContentReviewDecisionTemplate(output, binding)
	if err != nil {
		t.Fatalf("build decision template: %v", err)
	}
	if template.ReportPayloadSHA256 != output.Integrity.PayloadSHA256 ||
		template.ReviewBundleSHA256 != binding.FileSHA256 || len(template.Cases) != 2 {
		t.Fatalf("decision template identity = %+v", template)
	}
	if template.CandidateVerdict != eval.AgentTaskContentReviewRejected ||
		template.StableVerdict != eval.AgentTaskContentReviewRejected ||
		template.Cases[0].Candidate.FactualCorrectness != eval.AgentTaskContentReviewFailed {
		t.Fatalf("decision template is not fail closed: %+v", template)
	}
	if _, err := eval.BuildAndSignAgentTaskContentReviewSignoff(
		output, binding, template,
		[]byte("independent-content-signoff-key-v1"), "signoff-key-v1", time.Now().UTC(),
	); err == nil || !strings.Contains(err.Error(), "decision time") {
		t.Fatalf("incomplete template was accepted for signoff: %v", err)
	}
	path := filepath.Join(t.TempDir(), "decision-template.json")
	if err := writeAgentTaskContentReviewDecisionTemplate(path, output, binding); err != nil {
		t.Fatalf("write decision template: %v", err)
	}
	payload, err := readBoundedReviewFile(path, 1<<20)
	if err != nil {
		t.Fatalf("read decision template: %v", err)
	}
	if strings.Contains(string(payload), strings.Repeat("1", 64)) || strings.Contains(string(payload), strings.Repeat("3", 64)) {
		t.Fatalf("decision template leaked output hashes: %s", payload)
	}
	var decoded eval.AgentTaskContentReviewDecision
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode decision template: %v", err)
	}
}
