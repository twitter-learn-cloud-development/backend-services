package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestTaskSpecValidateRequiresObservableCompletion(t *testing.T) {
	tests := []struct {
		name string
		spec TaskSpec
	}{
		{name: "missing goal", spec: TaskSpec{CompletionCriteria: []CompletionCriterion{{ID: "answer", Description: "answer exists", Required: true}}}},
		{name: "missing criteria", spec: TaskSpec{Goal: "answer the user"}},
		{name: "no required criterion", spec: TaskSpec{Goal: "answer the user", CompletionCriteria: []CompletionCriterion{{ID: "style", Description: "style preference"}}}},
		{name: "duplicate tools", spec: TaskSpec{Goal: "search", CompletionCriteria: []CompletionCriterion{{ID: "result", Description: "result exists", Required: true}}, AllowedTools: []string{"search", "search"}}},
		{name: "negative repair limit", spec: TaskSpec{Goal: "search", CompletionCriteria: []CompletionCriterion{{ID: "result", Description: "result exists", Required: true}}, MaxRepairAttempts: -1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.spec.Validate(); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
	}
}

func TestEvidenceLedgerWithIsAppendOnly(t *testing.T) {
	original := EvidenceLedger{}
	updated, err := original.With(Evidence{
		ID: "evidence-1", Kind: EvidenceToolObservation, Source: "search",
		CriterionIDs: []string{"source-found"}, Digest: "sha256:abc",
	})
	if err != nil {
		t.Fatalf("With() error = %v", err)
	}
	if len(original.Items) != 0 || len(updated.Items) != 1 {
		t.Fatalf("ledger lengths = %d/%d", len(original.Items), len(updated.Items))
	}
	if _, err := updated.With(updated.Items[0]); err == nil {
		t.Fatal("With() allowed duplicate evidence")
	}
}

func TestRequiredEvidenceVerifierControlsRepair(t *testing.T) {
	task := TaskSpec{
		Goal: "return a grounded answer",
		CompletionCriteria: []CompletionCriterion{
			{ID: "source-found", Description: "a source was observed", Required: true},
			{ID: "answer-written", Description: "an answer was produced", Required: true},
		},
		MaxRepairAttempts: 1,
	}
	ledger, err := (EvidenceLedger{}).With(Evidence{
		ID: "source-1", Kind: EvidenceToolObservation, Source: "web_search",
		CriterionIDs: []string{"source-found"}, Reference: "tool-result://source-1",
	})
	if err != nil {
		t.Fatalf("With() error = %v", err)
	}

	verifier := RequiredEvidenceVerifier{}
	result, err := verifier.Verify(context.Background(), VerificationRequest{Task: task, Evidence: ledger})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Status != VerificationFailed || !result.Retryable {
		t.Fatalf("status/retryable = %q/%v", result.Status, result.Retryable)
	}
	if !reflect.DeepEqual(result.MissingEvidence, []string{"answer-written"}) {
		t.Fatalf("missing evidence = %v", result.MissingEvidence)
	}

	result, err = verifier.Verify(context.Background(), VerificationRequest{
		Task: task, Evidence: ledger, RepairAttempts: 1,
	})
	if err != nil {
		t.Fatalf("Verify() after repair error = %v", err)
	}
	if result.Retryable {
		t.Fatal("Verify() allowed repair beyond task limit")
	}
}

func TestRequiredEvidenceVerifierPassesAllRequiredCriteria(t *testing.T) {
	task := TaskSpec{
		Goal: "publish after approval",
		CompletionCriteria: []CompletionCriterion{
			{ID: "approved", Description: "user approved the write", Required: true},
			{ID: "published", Description: "published state was observed", Required: true},
			{ID: "optional-style", Description: "preferred style was used"},
		},
	}
	ledger := EvidenceLedger{}
	var err error
	ledger, err = ledger.With(Evidence{
		ID: "approval-1", Kind: EvidenceApproval, Source: "approval",
		CriterionIDs: []string{"approved"}, Digest: "sha256:approval",
	})
	if err != nil {
		t.Fatalf("With(approval) error = %v", err)
	}
	ledger, err = ledger.With(Evidence{
		ID: "snapshot-1", Kind: EvidenceEnvironmentState, Source: "twitter",
		CriterionIDs: []string{"published"}, Reference: "tweet://42",
	})
	if err != nil {
		t.Fatalf("With(snapshot) error = %v", err)
	}

	result, err := (RequiredEvidenceVerifier{}).Verify(context.Background(), VerificationRequest{Task: task, Evidence: ledger})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Passed() || result.Retryable || len(result.Checks) != 2 {
		t.Fatalf("result = %+v", result)
	}
}
