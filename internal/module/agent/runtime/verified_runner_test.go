package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type scriptedGoalRunner struct {
	results  []RunResult
	errors   []error
	requests []RunRequest
}

func (runner *scriptedGoalRunner) Run(_ context.Context, request RunRequest) (RunResult, error) {
	runner.requests = append(runner.requests, request)
	index := len(runner.requests) - 1
	if index < len(runner.errors) && runner.errors[index] != nil {
		return RunResult{}, runner.errors[index]
	}
	if index >= len(runner.results) {
		return RunResult{}, errors.New("unexpected runner call")
	}
	return runner.results[index], nil
}

type fakeGoalEnvironment struct {
	name          string
	tools         []ToolDefinition
	snapshotCalls []SnapshotPhase
	snapshotErr   error
}

func (environment *fakeGoalEnvironment) Name() string { return environment.name }

func (environment *fakeGoalEnvironment) Tools(context.Context, TaskSpec) ([]ToolDefinition, error) {
	return cloneToolDefinitions(environment.tools), nil
}

func (environment *fakeGoalEnvironment) Snapshot(
	ctx context.Context,
	request SnapshotRequest,
) (EnvironmentSnapshot, error) {
	environment.snapshotCalls = append(environment.snapshotCalls, request.Phase)
	if err := ctx.Err(); err != nil {
		return EnvironmentSnapshot{}, err
	}
	if environment.snapshotErr != nil {
		return EnvironmentSnapshot{}, environment.snapshotErr
	}
	return EnvironmentSnapshot{
		ID: string(request.Phase), Environment: environment.name,
		Digest: "sha256:" + string(request.Phase),
	}, nil
}

type countingVerifier struct {
	delegate Verifier
	calls    int
}

func (verifier *countingVerifier) Verify(
	ctx context.Context,
	request VerificationRequest,
) (VerificationResult, error) {
	verifier.calls++
	return verifier.delegate.Verify(ctx, request)
}

func TestVerifiedRunnerPassesOnlyWithRequiredEvidence(t *testing.T) {
	runID := "run-pass"
	base := goalRunRequest(runID)
	base.Tools = []ToolDefinition{
		{Name: "search", Category: ToolCategoryRead},
		{Name: "publish", Category: ToolCategoryWrite},
	}
	scripted := &scriptedGoalRunner{results: []RunResult{{
		Context: base.Context, Status: RunStatusCompleted, FinalAnswer: "found",
		Messages: []Message{{Role: RoleAssistant, Content: "found"}},
		Steps: []Step{{Index: 1, Observations: []Observation{{
			ActionID: "search-1", Name: "search",
			StructuredContent: json.RawMessage(`{"items":[{"id":"42"}]}`),
		}}}},
	}}}
	environment := &fakeGoalEnvironment{
		name:  "twitter",
		tools: []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
	}
	runner := NewVerifiedRunner(
		scripted,
		RequiredEvidenceVerifier{},
		StructuredObservationEvidenceCollector{Bindings: map[string][]string{"search": {"source-found"}}},
	)

	result, err := runner.Run(context.Background(), VerifiedRunRequest{
		Task: goalTask(0), Run: base, Environment: environment,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != GoalRunVerified || !result.Verification.Passed() {
		t.Fatalf("status/verification = %q/%+v", result.Status, result.Verification)
	}
	if len(result.Evidence.Items) != 1 || result.Evidence.Items[0].Source != "search" {
		t.Fatalf("evidence = %+v", result.Evidence.Items)
	}
	if len(scripted.requests) != 1 || len(scripted.requests[0].Tools) != 1 || scripted.requests[0].Tools[0].Name != "search" {
		t.Fatalf("resolved tools = %+v", scripted.requests)
	}
	if got := environment.snapshotCalls; len(got) != 2 || got[0] != SnapshotPhaseBefore || got[1] != SnapshotPhaseAfter {
		t.Fatalf("snapshot phases = %v", got)
	}
}

func TestVerifiedRunnerRepairsWithinSharedBudget(t *testing.T) {
	base := goalRunRequest("run-repair")
	base.Context.Budget.MaxSteps = 3
	base.Context.Budget.MaxTotalTokens = 10
	base.Tools = []ToolDefinition{{Name: "search", Category: ToolCategoryRead}}
	scripted := &scriptedGoalRunner{results: []RunResult{
		{
			Context: base.Context, Status: RunStatusCompleted, FinalAnswer: "not grounded",
			Messages: []Message{{Role: RoleAssistant, Content: "not grounded"}},
			Steps:    []Step{{Index: 1}}, Usage: TokenUsage{TotalTokens: 3},
		},
		{
			Context: base.Context, Status: RunStatusCompleted, FinalAnswer: "grounded",
			Messages: []Message{{Role: RoleAssistant, Content: "not grounded"}, {Role: RoleAssistant, Content: "grounded"}},
			Steps: []Step{{Index: 1, Observations: []Observation{{
				ActionID: "search-2", Name: "search", StructuredContent: json.RawMessage(`{"id":"42"}`),
			}}}},
			Usage: TokenUsage{TotalTokens: 2},
		},
	}}
	runner := NewVerifiedRunner(
		scripted,
		RequiredEvidenceVerifier{},
		StructuredObservationEvidenceCollector{Bindings: map[string][]string{"search": {"source-found"}}},
	)

	result, err := runner.Run(context.Background(), VerifiedRunRequest{Task: goalTask(1), Run: base})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != GoalRunVerified || result.RepairAttempts != 1 {
		t.Fatalf("status/repairs = %q/%d", result.Status, result.RepairAttempts)
	}
	if len(scripted.requests) != 2 {
		t.Fatalf("runner calls = %d", len(scripted.requests))
	}
	repair := scripted.requests[1]
	if repair.Context.Budget.MaxSteps != 2 || repair.Context.Budget.MaxTotalTokens != 7 {
		t.Fatalf("repair budget = %+v", repair.Context.Budget)
	}
	if last := repair.Messages[len(repair.Messages)-1]; last.Role != RoleDeveloper || !strings.Contains(last.Content, "source-found") {
		t.Fatalf("repair message = %+v", last)
	}
	if len(result.Run.Steps) != 2 || result.Run.Steps[1].Index != 2 || result.Run.Usage.TotalTokens != 5 {
		t.Fatalf("merged run = %+v", result.Run)
	}
}

func TestVerifiedRunnerBlocksWhenEvidenceCannotBeRepaired(t *testing.T) {
	base := goalRunRequest("run-blocked")
	scripted := &scriptedGoalRunner{results: []RunResult{{
		Context: base.Context, Status: RunStatusCompleted, FinalAnswer: "claimed done",
		Messages: []Message{{Role: RoleAssistant, Content: "claimed done"}},
	}}}
	runner := NewVerifiedRunner(scripted, RequiredEvidenceVerifier{}, nil)

	result, err := runner.Run(context.Background(), VerifiedRunRequest{Task: goalTask(0), Run: base})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != GoalRunBlocked || result.Verification.Status != VerificationFailed {
		t.Fatalf("result = %+v", result)
	}
	if len(scripted.requests) != 1 {
		t.Fatalf("runner calls = %d", len(scripted.requests))
	}
}

func TestVerifiedRunnerPreservesApprovalSuspension(t *testing.T) {
	base := goalRunRequest("run-approval")
	scripted := &scriptedGoalRunner{results: []RunResult{{
		Context: base.Context, Status: RunStatusApprovalRequired,
		Messages: []Message{{
			Role:    RoleAssistant,
			Actions: []Action{{ID: "publish-1", Type: ActionToolCall, Name: "search"}},
		}},
		Steps: []Step{{
			Index:   1,
			Actions: []Action{{ID: "publish-1", Type: ActionToolCall, Name: "search"}},
			Observations: []Observation{{
				ActionID: "publish-1", Name: "search", IsError: true,
			}},
		}},
		PendingAction: &Action{ID: "publish-1", Type: ActionToolCall, Name: "search"},
		ApprovalID:    "approval-1",
	}}}
	verifier := &countingVerifier{delegate: RequiredEvidenceVerifier{}}
	runner := NewVerifiedRunner(scripted, verifier, nil)

	result, err := runner.Run(context.Background(), VerifiedRunRequest{Task: goalTask(1), Run: base})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != GoalRunSuspended || result.Run.Status != RunStatusApprovalRequired {
		t.Fatalf("result = %+v", result)
	}
	if verifier.calls != 0 {
		t.Fatalf("verifier calls = %d", verifier.calls)
	}
}

func TestVerifiedRunnerFailsWhenRepairStepBudgetIsExhausted(t *testing.T) {
	base := goalRunRequest("run-budget")
	base.Context.Budget.MaxSteps = 1
	scripted := &scriptedGoalRunner{results: []RunResult{{
		Context: base.Context, Status: RunStatusCompleted,
		Steps: []Step{{Index: 1}},
	}}}
	runner := NewVerifiedRunner(scripted, RequiredEvidenceVerifier{}, nil)

	_, err := runner.Run(context.Background(), VerifiedRunRequest{Task: goalTask(1), Run: base})
	if !HasErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestVerifiedRunnerPropagatesCancellationBeforeExecution(t *testing.T) {
	base := goalRunRequest("run-canceled")
	environment := &fakeGoalEnvironment{
		name: "twitter", tools: []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
	}
	runner := NewVerifiedRunner(&scriptedGoalRunner{}, RequiredEvidenceVerifier{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runner.Run(ctx, VerifiedRunRequest{Task: goalTask(0), Run: base, Environment: environment})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
}

func goalTask(repairs int) TaskSpec {
	return TaskSpec{
		ID: "task-grounded-search", Goal: "return a grounded search result",
		CompletionCriteria: []CompletionCriterion{{
			ID: "source-found", Description: "a structured search result was observed", Required: true,
		}},
		AllowedTools: []string{"search"}, MaxRepairAttempts: repairs,
	}
}

func goalRunRequest(runID string) RunRequest {
	return RunRequest{
		Context:  RunContext{RunID: runID, UserID: 7, Budget: Budget{MaxSteps: 3}},
		Model:    "test-model",
		Messages: []Message{{Role: RoleUser, Content: "find it"}},
		Tools:    []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
	}
}
