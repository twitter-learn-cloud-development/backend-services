package evidence

import (
	"context"
	"encoding/json"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestEvidenceConflictGoalVerifierRequiresConflictAndExactClarification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		run              agentRuntime.RunResult
		wantPass         bool
		wantConflictCode string
		wantClarifyCode  string
	}{
		{
			name:             "conflict suspends for exact clarification",
			run:              evidenceConflictRun("active", "disabled", evidenceConflictSpec().ClarificationPrompt),
			wantPass:         true,
			wantConflictCode: EvidenceConflictDetectedCode,
			wantClarifyCode:  EvidenceConflictClarificationCode,
		},
		{
			name:             "matching values are not a conflict",
			run:              evidenceConflictRun("active", " ACTIVE ", evidenceConflictSpec().ClarificationPrompt),
			wantConflictCode: EvidenceConflictMissingCode,
			wantClarifyCode:  EvidenceConflictClarificationMissingCode,
		},
		{
			name:             "fragments do not create distinct sources",
			run:              evidenceConflictFragmentAliasRun(),
			wantConflictCode: EvidenceConflictMissingCode,
			wantClarifyCode:  EvidenceConflictClarificationMissingCode,
		},
		{
			name:             "unrelated question cannot satisfy clarification",
			run:              evidenceConflictRun("active", "disabled", "Which account should I use?"),
			wantConflictCode: EvidenceConflictDetectedCode,
			wantClarifyCode:  EvidenceConflictClarificationMissingCode,
		},
		{
			name:             "clarification before evidence is rejected",
			run:              evidenceConflictEarlyClarificationRun(),
			wantConflictCode: EvidenceConflictDetectedCode,
			wantClarifyCode:  EvidenceConflictClarificationMissingCode,
		},
		{
			name:             "silent final answer is rejected",
			run:              evidenceConflictCompletedRun("active", "disabled"),
			wantConflictCode: EvidenceConflictDetectedCode,
			wantClarifyCode:  EvidenceConflictClarificationMissingCode,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			task := evidenceConflictTask(t)
			ledger := collectConflictEvidence(t, task, test.run)
			result, err := (EvidenceConflictGoalVerifier{Spec: evidenceConflictSpec()}).Verify(
				context.Background(),
				agentRuntime.VerificationRequest{Task: task, Run: test.run, Evidence: ledger},
			)
			if err != nil {
				t.Fatalf("Verify() error = %v", err)
			}
			if result.Passed() != test.wantPass {
				t.Fatalf("Verify() status = %q, checks = %+v", result.Status, result.Checks)
			}
			if checkCode(result, EvidenceConflictDetectedCriterion) != test.wantConflictCode ||
				checkCode(result, EvidenceConflictClarificationCriterion) != test.wantClarifyCode {
				t.Fatalf("Verify() checks = %+v", result.Checks)
			}
		})
	}
}

func TestEvidenceConflictGoalVerifierRejectsUnpairedAndForgedEvidence(t *testing.T) {
	t.Parallel()
	task := evidenceConflictTask(t)
	spec := evidenceConflictSpec()

	t.Run("unpaired structured observations are ignored", func(t *testing.T) {
		run := evidenceConflictRun("active", "disabled", spec.ClarificationPrompt)
		run.Steps[0].Actions = nil
		ledger := collectConflictEvidence(t, task, run)
		result, err := (EvidenceConflictGoalVerifier{Spec: spec}).VerifySuspension(
			context.Background(),
			agentRuntime.VerificationRequest{Task: task, Run: run, Evidence: ledger},
		)
		if err != nil {
			t.Fatalf("VerifySuspension() error = %v", err)
		}
		if result.Passed() || checkCode(result, EvidenceConflictDetectedCriterion) != EvidenceConflictMissingCode {
			t.Fatalf("VerifySuspension() result = %+v", result)
		}
	})

	t.Run("ledger copied from another run cannot manufacture conflict", func(t *testing.T) {
		conflicting := evidenceConflictRun("active", "disabled", spec.ClarificationPrompt)
		forged := collectConflictEvidence(t, task, conflicting)
		actual := evidenceConflictRun("active", "active", spec.ClarificationPrompt)
		result, err := (EvidenceConflictGoalVerifier{Spec: spec}).VerifySuspension(
			context.Background(),
			agentRuntime.VerificationRequest{Task: task, Run: actual, Evidence: forged},
		)
		if err != nil {
			t.Fatalf("VerifySuspension() error = %v", err)
		}
		if result.Passed() || checkCode(result, EvidenceConflictDetectedCriterion) != EvidenceConflictMissingCode {
			t.Fatalf("VerifySuspension() result = %+v", result)
		}
	})
}

func TestEvidenceConflictVerifiedRunnerProducesVerifiedSuspensionOutcome(t *testing.T) {
	t.Parallel()
	task := evidenceConflictTask(t)
	run := evidenceConflictRun("active", "disabled", evidenceConflictSpec().ClarificationPrompt)
	base := agentRuntime.RunRequest{
		Context: agentRuntime.RunContext{
			RunID:  run.Context.RunID,
			UserID: 7,
			Budget: agentRuntime.Budget{MaxSteps: 4},
		},
		Model:    "test-model",
		Messages: []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "Resolve the deployment state."}},
		Tools: []agentRuntime.ToolDefinition{
			{Name: "source_a", Category: agentRuntime.ToolCategoryRead},
			{Name: "source_b", Category: agentRuntime.ToolCategoryRead},
		},
	}
	runner := agentRuntime.NewVerifiedRunner(
		&fixedConflictRunner{result: run},
		EvidenceConflictGoalVerifier{Spec: evidenceConflictSpec()},
		EvidenceConflictGoalCollector{Spec: evidenceConflictSpec()},
	)

	result, err := runner.Run(context.Background(), agentRuntime.VerifiedRunRequest{
		Task: task,
		Run:  base,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != agentRuntime.GoalRunSuspended || !result.Verification.Passed() ||
		result.Checkpoint == nil || len(result.Evidence.Items) != 3 {
		t.Fatalf("Run() result = %+v", result)
	}
	outcome, err := agentRuntime.BuildObservedTaskOutcome(task, result)
	if err != nil {
		t.Fatalf("BuildObservedTaskOutcome() error = %v", err)
	}
	if outcome.Status != agentRuntime.GoalRunSuspended || !outcome.Verification.Passed() ||
		len(outcome.Artifacts) != 1 || outcome.FinalAnswerDigest != "" {
		t.Fatalf("BuildObservedTaskOutcome() = %+v", outcome)
	}
}

func TestEvidenceConflictVerifiedRunnerBlocksSilentSelection(t *testing.T) {
	t.Parallel()
	task := evidenceConflictTask(t)
	run := evidenceConflictCompletedRun("active", "disabled")
	runner := agentRuntime.NewVerifiedRunner(
		&fixedConflictRunner{result: run},
		EvidenceConflictGoalVerifier{Spec: evidenceConflictSpec()},
		EvidenceConflictGoalCollector{Spec: evidenceConflictSpec()},
	)
	result, err := runner.Run(context.Background(), agentRuntime.VerifiedRunRequest{
		Task: task,
		Run: agentRuntime.RunRequest{
			Context: agentRuntime.RunContext{RunID: run.Context.RunID, UserID: 7,
				Budget: agentRuntime.Budget{MaxSteps: 4}},
			Model: "test-model",
			Tools: []agentRuntime.ToolDefinition{
				{Name: "source_a", Category: agentRuntime.ToolCategoryRead},
				{Name: "source_b", Category: agentRuntime.ToolCategoryRead},
			},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != agentRuntime.GoalRunBlocked || result.Verification.Passed() ||
		checkCode(result.Verification, EvidenceConflictClarificationCriterion) !=
			EvidenceConflictClarificationMissingCode {
		t.Fatalf("Run() result = %+v", result)
	}
}

func TestEvidenceConflictTaskRejectsPolicyDrift(t *testing.T) {
	t.Parallel()
	task := evidenceConflictTask(t)
	task.AllowedTools[0] = "untrusted_tool"
	_, err := (EvidenceConflictGoalCollector{Spec: evidenceConflictSpec()}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task},
	)
	if err == nil {
		t.Fatal("Collect() error = nil")
	}
}

type fixedConflictRunner struct {
	result agentRuntime.RunResult
}

func (runner *fixedConflictRunner) Run(
	_ context.Context,
	_ agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	return runner.result, nil
}

func evidenceConflictSpec() EvidenceConflictSpec {
	return EvidenceConflictSpec{
		ClaimID:             "deployment.production.state",
		ClarificationPrompt: "Sources disagree about the production state. Which source should govern the answer?",
		TrustedTools:        []string{"source_a", "source_b"},
	}
}

func evidenceConflictTask(t *testing.T) agentRuntime.TaskSpec {
	t.Helper()
	task, err := BuildEvidenceConflictTask(
		"e2e-12-evidence-conflict",
		"Resolve conflicting production-state evidence without silently choosing a source.",
		evidenceConflictSpec(),
	)
	if err != nil {
		t.Fatalf("BuildEvidenceConflictTask() error = %v", err)
	}
	return task
}

func evidenceConflictRun(left, right, prompt string) agentRuntime.RunResult {
	pending := agentRuntime.Action{
		ID: "clarify-1", Type: agentRuntime.ActionAskHuman, Content: prompt,
	}
	return agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-e2e-12", UserID: 7},
		Status:  agentRuntime.RunStatusAwaitingHuman,
		Messages: []agentRuntime.Message{
			{Role: agentRuntime.RoleAssistant, Actions: []agentRuntime.Action{pending}},
		},
		Steps: []agentRuntime.Step{
			evidenceAssertionStep(1, "source_a", "lookup-a", left, "https://a.example/state"),
			evidenceAssertionStep(2, "source_b", "lookup-b", right, "https://b.example/state"),
			{Index: 3, Actions: []agentRuntime.Action{pending}},
		},
		PendingAction:     &pending,
		PendingResumeKind: agentRuntime.ResumeKindHumanResponse,
	}
}

func evidenceConflictCompletedRun(left, right string) agentRuntime.RunResult {
	run := evidenceConflictRun(left, right, evidenceConflictSpec().ClarificationPrompt)
	answer := agentRuntime.Action{
		ID: "answer-1", Type: agentRuntime.ActionFinalAnswer,
		Content: "Production is active.",
	}
	run.Status = agentRuntime.RunStatusCompleted
	run.FinalAnswer = answer.Content
	run.Messages = []agentRuntime.Message{{Role: agentRuntime.RoleAssistant, Content: answer.Content,
		Actions: []agentRuntime.Action{answer}}}
	run.Steps[2] = agentRuntime.Step{Index: 3, Actions: []agentRuntime.Action{answer}}
	run.PendingAction = nil
	run.PendingResumeKind = ""
	return run
}

func evidenceConflictEarlyClarificationRun() agentRuntime.RunResult {
	run := evidenceConflictRun("active", "disabled", evidenceConflictSpec().ClarificationPrompt)
	run.Steps[0].Index = 2
	run.Steps[1].Index = 3
	run.Steps[2].Index = 1
	return run
}

func evidenceConflictFragmentAliasRun() agentRuntime.RunResult {
	run := evidenceConflictRun("active", "disabled", evidenceConflictSpec().ClarificationPrompt)
	run.Steps[0] = evidenceAssertionStep(1, "source_a", "lookup-a", "active", "https://same.example/state#one")
	run.Steps[1] = evidenceAssertionStep(2, "source_b", "lookup-b", "disabled", "https://same.example/state#two")
	return run
}

func evidenceAssertionStep(index int, tool, actionID, value, reference string) agentRuntime.Step {
	action := agentRuntime.Action{ID: actionID, Type: agentRuntime.ActionToolCall, Name: tool}
	payload, _ := json.Marshal(EvidenceAssertionResult{
		Schema: EvidenceAssertionSchema,
		Assertions: []EvidenceAssertion{{
			ClaimID: evidenceConflictSpec().ClaimID, Value: value, Reference: reference,
		}},
	})
	return agentRuntime.Step{
		Index:   index,
		Actions: []agentRuntime.Action{action},
		Observations: []agentRuntime.Observation{{
			ActionID: actionID, Name: tool, StructuredContent: payload,
		}},
	}
}

func collectConflictEvidence(
	t *testing.T,
	task agentRuntime.TaskSpec,
	run agentRuntime.RunResult,
) agentRuntime.EvidenceLedger {
	t.Helper()
	items, err := (EvidenceConflictGoalCollector{Spec: evidenceConflictSpec()}).Collect(
		context.Background(),
		agentRuntime.EvidenceCollectionRequest{Task: task, Run: run},
	)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var ledger agentRuntime.EvidenceLedger
	for _, item := range items {
		ledger, err = ledger.With(item)
		if err != nil {
			t.Fatalf("EvidenceLedger.With() error = %v", err)
		}
	}
	return ledger
}

func checkCode(result agentRuntime.VerificationResult, criterionID string) string {
	for _, check := range result.Checks {
		if check.CriterionID == criterionID {
			return check.Code
		}
	}
	return ""
}
