package evidence

import (
	"context"
	"fmt"
	"strings"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestE2E10RewriteConstraintsVerified(t *testing.T) {
	tests := []struct {
		name   string
		spec   RewriteConstraintSpec
		answer string
	}{
		{
			name: "Chinese markdown list",
			spec: RewriteConstraintSpec{
				Language: RewriteLanguageChinese, Format: RewriteFormatMarkdownList,
				MinCharacters: 20, MaxCharacters: 100,
			},
			answer: "- 系统先观察真实环境，再决定下一步行动。\n- 每次行动完成后，都使用测试证据验证结果。",
		},
		{
			name: "English JSON",
			spec: RewriteConstraintSpec{
				Language: RewriteLanguageEnglish, Format: RewriteFormatJSON,
				MinCharacters: 30, MaxCharacters: 160,
			},
			answer: `{"title":"Verified execution","summary":"Reliable agents validate real outcomes before claiming completion."}`,
		},
		{
			name: "English plain text",
			spec: RewriteConstraintSpec{
				Language: RewriteLanguageEnglish, Format: RewriteFormatPlainText,
				MinCharacters: 30, MaxCharacters: 120,
			},
			answer: "Reliable systems validate observable outcomes before claiming that a task is complete.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			task, outcome := evaluateE2E10Rewrite(t, test.spec, test.answer, 0)
			if outcome.Status != agentRuntime.GoalRunVerified || !outcome.Verification.Passed() {
				t.Fatalf("expected verified outcome, got status=%s verification=%+v", outcome.Status, outcome.Verification)
			}
			if len(outcome.Artifacts) != 1 {
				t.Fatalf("expected one rewrite artifact, got %+v", outcome.Artifacts)
			}
			artifact := outcome.Artifacts[0]
			if artifact.Type != RewriteArtifactType || artifact.Digest != rewriteAnswerDigest(test.answer) {
				t.Fatalf("unexpected artifact: %+v", artifact)
			}
			if len(artifact.SupportingEvidence) != 2 {
				t.Fatalf("expected artifact and constraint evidence, got %+v", artifact.SupportingEvidence)
			}
			if outcome.ExecutionSource != agentRuntime.TaskOutcomeExecutionObserved || outcome.TaskID != task.ID {
				t.Fatalf("unexpected outcome identity: %+v", outcome)
			}
			serialized := fmt.Sprintf("%+v", outcome)
			if strings.Contains(serialized, test.answer) {
				t.Fatal("task outcome leaked rewrite body")
			}
		})
	}
}

func TestE2E10RewriteConstraintsRejectMismatches(t *testing.T) {
	tests := []struct {
		name         string
		spec         RewriteConstraintSpec
		answer       string
		criterionID  string
		expectedCode string
	}{
		{
			name: "language mismatch",
			spec: RewriteConstraintSpec{
				Language: RewriteLanguageChinese, Format: RewriteFormatPlainText,
				MinCharacters: 20, MaxCharacters: 120,
			},
			answer:      "Reliable agents validate observable outcomes before claiming completion.",
			criterionID: RewriteLanguageCriterion, expectedCode: RewriteLanguageMismatchCode,
		},
		{
			name: "format mismatch",
			spec: RewriteConstraintSpec{
				Language: RewriteLanguageEnglish, Format: RewriteFormatJSON,
				MinCharacters: 20, MaxCharacters: 120,
			},
			answer:      "Reliable agents validate observable outcomes before claiming completion.",
			criterionID: RewriteFormatCriterion, expectedCode: RewriteFormatMismatchCode,
		},
		{
			name: "too short",
			spec: RewriteConstraintSpec{
				Language: RewriteLanguageEnglish, Format: RewriteFormatPlainText,
				MinCharacters: 40, MaxCharacters: 100,
			},
			answer:      "Short rewrite.",
			criterionID: RewriteLengthCriterion, expectedCode: RewriteLengthMismatchCode,
		},
		{
			name: "too long",
			spec: RewriteConstraintSpec{
				Language: RewriteLanguageEnglish, Format: RewriteFormatPlainText,
				MinCharacters: 5, MaxCharacters: 20,
			},
			answer:      "This rewrite is deliberately longer than the configured limit.",
			criterionID: RewriteLengthCriterion, expectedCode: RewriteLengthMismatchCode,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, outcome := evaluateE2E10Rewrite(t, test.spec, test.answer, 1)
			if outcome.Status != agentRuntime.GoalRunBlocked || outcome.Verification.Status != agentRuntime.VerificationFailed {
				t.Fatalf("expected blocked outcome, got %+v", outcome)
			}
			check := rewriteCheckByCriterion(t, outcome.Verification, test.criterionID)
			if check.Code != test.expectedCode || check.Status != agentRuntime.VerificationFailed {
				t.Fatalf("unexpected failed check: %+v", check)
			}
			if !outcome.Verification.Retryable {
				t.Fatal("expected failed first attempt to remain repairable")
			}
			if len(outcome.Artifacts) != 1 || outcome.Artifacts[0].Status != agentRuntime.VerificationFailed {
				t.Fatalf("failed constraint must fail the artifact: %+v", outcome.Artifacts)
			}
		})
	}
}

func TestE2E10RewriteConstraintsRejectMissingOrForgedArtifact(t *testing.T) {
	spec := RewriteConstraintSpec{
		Language: RewriteLanguageEnglish, Format: RewriteFormatPlainText,
		MinCharacters: 10, MaxCharacters: 100,
	}
	task, err := BuildRewriteConstraintTask("e2e-10-forged", "Rewrite the supplied content.", spec)
	if err != nil {
		t.Fatal(err)
	}
	run := agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-e2e-10-forged"},
		Status:  agentRuntime.RunStatusCompleted, FinalAnswer: "A valid English rewrite with enough detail.",
	}
	items, err := (RewriteConstraintGoalCollector{Constraints: spec}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: run},
	)
	if err != nil {
		t.Fatal(err)
	}
	for index := range items {
		if items[index].Kind == agentRuntime.EvidenceArtifact {
			items[index].Digest = "sha256:forged"
		}
	}
	ledger := rewriteLedger(t, items)
	verification, err := (RewriteConstraintGoalVerifier{Constraints: spec}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{Task: task, Run: run, Evidence: ledger},
	)
	if err != nil {
		t.Fatal(err)
	}
	check := rewriteCheckByCriterion(t, verification, RewriteArtifactCriterion)
	if check.Code != RewriteArtifactMissingCode || verification.Status != agentRuntime.VerificationFailed {
		t.Fatalf("forged artifact was accepted: %+v", verification)
	}

	emptyRun := run
	emptyRun.FinalAnswer = "   "
	emptyItems, err := (RewriteConstraintGoalCollector{Constraints: spec}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: emptyRun},
	)
	if err != nil {
		t.Fatal(err)
	}
	emptyVerification, err := (RewriteConstraintGoalVerifier{Constraints: spec}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{Task: task, Run: emptyRun, Evidence: rewriteLedger(t, emptyItems)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if check := rewriteCheckByCriterion(t, emptyVerification, RewriteArtifactCriterion); check.Code != RewriteArtifactMissingCode {
		t.Fatalf("empty answer did not fail artifact criterion: %+v", emptyVerification)
	}
}

func TestE2E10RewriteConstraintTaskIsCanonicalAndToolFree(t *testing.T) {
	spec := RewriteConstraintSpec{
		Language: RewriteLanguageChinese, Format: RewriteFormatPlainText,
		MinCharacters: 20, MaxCharacters: 80,
	}
	task, err := BuildRewriteConstraintTask("e2e-10-task", "改写内容。", spec)
	if err != nil {
		t.Fatal(err)
	}

	withTool := task
	withTool.AllowedTools = []string{"web_search"}
	if _, err := (RewriteConstraintGoalCollector{Constraints: spec}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: withTool},
	); err == nil || !strings.Contains(err.Error(), "must not allow tools") {
		t.Fatalf("expected tool-bearing task rejection, got %v", err)
	}

	drifted := task
	drifted.Constraints = append([]agentRuntime.TaskConstraint(nil), task.Constraints...)
	drifted.Constraints[2].Description = "Final output must be short."
	if _, err := (RewriteConstraintGoalCollector{Constraints: spec}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: drifted},
	); err == nil || !strings.Contains(err.Error(), "does not match verifier policy") {
		t.Fatalf("expected constraint drift rejection, got %v", err)
	}

	extraCriterion := task
	extraCriterion.CompletionCriteria = append(extraCriterion.CompletionCriteria, agentRuntime.CompletionCriterion{
		ID: "model_claimed_done", Description: "The model claimed completion.", Required: true,
	})
	if _, err := (RewriteConstraintGoalCollector{Constraints: spec}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: extraCriterion},
	); err == nil || !strings.Contains(err.Error(), "exactly four") {
		t.Fatalf("expected unknown completion criterion rejection, got %v", err)
	}

	invalid := spec
	invalid.MaxCharacters = rewriteMaxCharacters + 1
	if _, err := BuildRewriteConstraintTask("e2e-10-invalid", "Rewrite.", invalid); err == nil {
		t.Fatal("expected oversized rewrite constraint rejection")
	}
}

func evaluateE2E10Rewrite(
	t *testing.T,
	spec RewriteConstraintSpec,
	answer string,
	maxRepairAttempts int,
) (agentRuntime.TaskSpec, agentRuntime.TaskOutcome) {
	t.Helper()
	task, err := BuildRewriteConstraintTask("e2e-10:"+strings.ReplaceAll(t.Name(), "/", ":"), "Rewrite the supplied content.", spec)
	if err != nil {
		t.Fatal(err)
	}
	task.MaxRepairAttempts = maxRepairAttempts
	run := agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-e2e-10:" + strings.ReplaceAll(t.Name(), "/", ":")},
		Status:  agentRuntime.RunStatusCompleted, FinalAnswer: answer,
	}
	items, err := (RewriteConstraintGoalCollector{Constraints: spec}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: run, Attempt: 0},
	)
	if err != nil {
		t.Fatal(err)
	}
	ledger := rewriteLedger(t, items)
	verification, err := (RewriteConstraintGoalVerifier{Constraints: spec}).Verify(
		context.Background(),
		agentRuntime.VerificationRequest{
			Task: task, Run: run, Evidence: ledger, RepairAttempts: 0,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	status := agentRuntime.GoalRunBlocked
	if verification.Passed() {
		status = agentRuntime.GoalRunVerified
	}
	outcome, err := agentRuntime.BuildObservedTaskOutcome(task, agentRuntime.VerifiedRunResult{
		Status: status, Run: run, Evidence: ledger, Verification: verification,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task, outcome
}

func rewriteLedger(t *testing.T, items []agentRuntime.Evidence) agentRuntime.EvidenceLedger {
	t.Helper()
	ledger := agentRuntime.EvidenceLedger{}
	for _, item := range items {
		var err error
		ledger, err = ledger.With(item)
		if err != nil {
			t.Fatal(err)
		}
	}
	return ledger
}

func rewriteCheckByCriterion(
	t *testing.T,
	verification agentRuntime.VerificationResult,
	criterionID string,
) agentRuntime.CheckResult {
	t.Helper()
	for _, check := range verification.Checks {
		if check.CriterionID == criterionID {
			return check
		}
	}
	t.Fatalf("criterion %s was not checked: %+v", criterionID, verification)
	return agentRuntime.CheckResult{}
}
