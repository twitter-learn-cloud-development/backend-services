package runtime

import "testing"

func TestBuildObservedTaskOutcomeDoesNotClaimAnAdmittedPlan(t *testing.T) {
	task := TaskSpec{
		ID:           "e2e-05",
		Goal:         "search platform tweets",
		AllowedTools: []string{"hybrid_search_tweets"},
		CompletionCriteria: []CompletionCriterion{{
			ID: "platform-search", Description: "structured result observed", Required: true,
		}},
	}
	evidence := Evidence{
		ID: "tweet-1", Kind: EvidenceToolObservation, Source: "hybrid_search_tweets",
		CriterionIDs: []string{"platform-search"}, Digest: "sha256:tweet-1", Reference: "/tweets/1",
	}
	result := VerifiedRunResult{
		Status: GoalRunVerified,
		Run: RunResult{
			Context: RunContext{RunID: "run-1"}, Status: RunStatusCompleted, FinalAnswer: "private answer",
		},
		Evidence: EvidenceLedger{Items: []Evidence{evidence}},
		Verification: VerificationResult{
			Status: VerificationPassed,
			Checks: []CheckResult{{
				CriterionID: "platform-search", Status: VerificationPassed, EvidenceIDs: []string{"tweet-1"},
			}},
		},
	}

	outcome, err := BuildObservedTaskOutcome(task, result)
	if err != nil {
		t.Fatalf("BuildObservedTaskOutcome() error = %v", err)
	}
	if outcome.ExecutionSource != TaskOutcomeExecutionObserved || len(outcome.PlanDigests) != 0 ||
		len(outcome.RecoveryDecisions) != 0 || outcome.FinalAnswerDigest == "" {
		t.Fatalf("outcome provenance = %+v", outcome)
	}
	if len(outcome.Evidence.Items) != 1 || outcome.Evidence.Items[0].Reference != "/tweets/1" {
		t.Fatalf("outcome evidence = %+v", outcome.Evidence.Items)
	}
	if outcome.Evidence.Items[0].Digest == "private answer" {
		t.Fatal("outcome copied final answer instead of storing a digest")
	}
}

func TestBuildObservedTaskOutcomeRejectsMissingRunIdentity(t *testing.T) {
	task := TaskSpec{
		ID: "e2e-05", Goal: "search platform tweets",
		CompletionCriteria: []CompletionCriterion{{
			ID: "platform-search", Description: "structured result observed", Required: true,
		}},
	}

	if _, err := BuildObservedTaskOutcome(task, VerifiedRunResult{Status: GoalRunBlocked}); err == nil {
		t.Fatal("BuildObservedTaskOutcome() accepted an empty run identity")
	}
}
