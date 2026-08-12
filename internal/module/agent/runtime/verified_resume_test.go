package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type scriptedResumableGoalRunner struct {
	resumeResults  []RunResult
	resumeErrors   []error
	resumeRequests []ResumeRequest
	runResults     []RunResult
	runErrors      []error
	runRequests    []RunRequest
}

func (runner *scriptedResumableGoalRunner) Run(
	_ context.Context,
	request RunRequest,
) (RunResult, error) {
	runner.runRequests = append(runner.runRequests, request)
	index := len(runner.runRequests) - 1
	if index < len(runner.runErrors) && runner.runErrors[index] != nil {
		return RunResult{}, runner.runErrors[index]
	}
	if index >= len(runner.runResults) {
		return RunResult{}, errors.New("unexpected run call")
	}
	return runner.runResults[index], nil
}

func (runner *scriptedResumableGoalRunner) Resume(
	_ context.Context,
	request ResumeRequest,
) (RunResult, error) {
	runner.resumeRequests = append(runner.resumeRequests, request)
	index := len(runner.resumeRequests) - 1
	if index < len(runner.resumeErrors) && runner.resumeErrors[index] != nil {
		return runner.resumeResults[index], runner.resumeErrors[index]
	}
	if index >= len(runner.resumeResults) {
		return RunResult{}, errors.New("unexpected resume call")
	}
	return runner.resumeResults[index], nil
}

func TestVerifiedRunnerResumeUsesPersistedStateAndCurrentTools(t *testing.T) {
	checkpoint := verifiedApprovalCheckpoint("run-resume", 0)
	resumable := &scriptedResumableGoalRunner{resumeResults: []RunResult{{
		Context: checkpoint.Run.Context,
		Status:  RunStatusCompleted,
		Messages: []Message{
			{Role: RoleAssistant, Actions: []Action{checkpoint.Run.PendingAction}},
			{Role: RoleTool, Name: "search", ToolCallID: "search-1"},
			{Role: RoleAssistant, Content: "grounded"},
		},
		Steps: []Step{
			{
				Index:   1,
				Actions: []Action{checkpoint.Run.PendingAction},
				Observations: []Observation{{
					ActionID: "search-1",
					Name:     "search",
					StructuredContent: json.RawMessage(
						`{"items":[{"id":"42"}]}`,
					),
				}},
			},
			{Index: 2},
		},
		Usage: TokenUsage{TotalTokens: 5},
	}}}
	environment := &fakeGoalEnvironment{
		name:  "twitter",
		tools: []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
	}
	runner := NewVerifiedRunner(
		resumable,
		RequiredEvidenceVerifier{},
		StructuredObservationEvidenceCollector{
			Bindings: map[string][]string{"search": {"source-found"}},
		},
	)

	result, err := runner.Resume(context.Background(), VerifiedResumeRequest{
		Checkpoint:  checkpoint,
		ApprovalID:  "approval-1",
		Tools:       []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
		Environment: environment,
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if result.Status != GoalRunVerified || !result.Verification.Passed() {
		t.Fatalf("result = %+v", result)
	}
	if result.Before == nil || result.Before.ID != "before-persisted" {
		t.Fatalf("before snapshot = %+v", result.Before)
	}
	if len(environment.snapshotCalls) != 1 ||
		environment.snapshotCalls[0] != SnapshotPhaseAfter {
		t.Fatalf("snapshot phases = %v", environment.snapshotCalls)
	}
	if len(resumable.resumeRequests) != 1 ||
		len(resumable.resumeRequests[0].Tools) != 1 ||
		resumable.resumeRequests[0].Tools[0].Name != "search" {
		t.Fatalf("resume requests = %+v", resumable.resumeRequests)
	}
	if result.Run.Usage.TotalTokens != 5 || len(result.Run.Steps) != 2 {
		t.Fatalf("cumulative result = %+v", result.Run)
	}
}

func TestVerifiedRunnerResumeFailsClosedWhenToolWasRevoked(t *testing.T) {
	checkpoint := verifiedApprovalCheckpoint("run-revoked", 0)
	resumable := &scriptedResumableGoalRunner{}
	runner := NewVerifiedRunner(resumable, RequiredEvidenceVerifier{}, nil)
	environment := &fakeGoalEnvironment{name: "twitter"}

	_, err := runner.Resume(context.Background(), VerifiedResumeRequest{
		Checkpoint:  checkpoint,
		ApprovalID:  "approval-1",
		Tools:       []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
		Environment: environment,
	})
	if !HasErrorCode(err, ErrorInvalidRequest) {
		t.Fatalf("Resume() error = %v", err)
	}
	if len(resumable.resumeRequests) != 0 {
		t.Fatalf("resume calls = %d, want 0", len(resumable.resumeRequests))
	}
}

func TestVerifiedRunnerResumeRepairsWithCumulativeBudget(t *testing.T) {
	checkpoint := verifiedApprovalCheckpoint("run-resume-repair", 0)
	checkpoint.Run.Context.Budget.MaxSteps = 3
	checkpoint.Run.Context.Budget.MaxTotalTokens = 10
	resumable := &scriptedResumableGoalRunner{
		resumeResults: []RunResult{{
			Context: checkpoint.Run.Context,
			Status:  RunStatusCompleted,
			Messages: []Message{
				{Role: RoleAssistant, Actions: []Action{checkpoint.Run.PendingAction}},
				{Role: RoleAssistant, Content: "not grounded"},
			},
			Steps: []Step{{Index: 1, Actions: []Action{checkpoint.Run.PendingAction}}, {Index: 2}},
			Usage: TokenUsage{TotalTokens: 4},
		}},
		runResults: []RunResult{{
			Context:  checkpoint.Run.Context,
			Status:   RunStatusCompleted,
			Messages: []Message{{Role: RoleAssistant, Content: "grounded"}},
			Steps: []Step{{
				Index: 1,
				Observations: []Observation{{
					ActionID: "search-2", Name: "search",
					StructuredContent: json.RawMessage(`{"id":"42"}`),
				}},
			}},
			Usage: TokenUsage{TotalTokens: 2},
		}},
	}
	runner := NewVerifiedRunner(
		resumable,
		RequiredEvidenceVerifier{},
		StructuredObservationEvidenceCollector{
			Bindings: map[string][]string{"search": {"source-found"}},
		},
	)
	environment := &fakeGoalEnvironment{
		name:  "twitter",
		tools: []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
	}

	result, err := runner.Resume(context.Background(), VerifiedResumeRequest{
		Checkpoint:  checkpoint,
		ApprovalID:  "approval-1",
		Tools:       []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
		Environment: environment,
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if result.Status != GoalRunVerified || result.RepairAttempts != 1 {
		t.Fatalf("status/repairs = %q/%d", result.Status, result.RepairAttempts)
	}
	if len(resumable.runRequests) != 1 {
		t.Fatalf("repair calls = %d", len(resumable.runRequests))
	}
	repairBudget := resumable.runRequests[0].Context.Budget
	if repairBudget.MaxSteps != 1 || repairBudget.MaxTotalTokens != 6 {
		t.Fatalf("repair budget = %+v", repairBudget)
	}
	if len(result.Run.Steps) != 3 || result.Run.Usage.TotalTokens != 6 {
		t.Fatalf("cumulative run = %+v", result.Run)
	}
}

func TestValidateVerifiedCheckpointRejectsUnknownEvidenceCriterion(t *testing.T) {
	checkpoint := verifiedApprovalCheckpoint("run-invalid-evidence", 0)
	checkpoint.Evidence = EvidenceLedger{Items: []Evidence{{
		ID: "forged", Kind: EvidenceToolObservation, Source: "search",
		CriterionIDs: []string{"not-declared"}, Digest: "sha256:forged",
	}}}

	if err := ValidateVerifiedCheckpoint(checkpoint); err == nil {
		t.Fatal("ValidateVerifiedCheckpoint() error = nil")
	}
}

func verifiedApprovalCheckpoint(runID string, repairs int) VerifiedCheckpoint {
	pending := Action{ID: "search-1", Type: ActionToolCall, Name: "search"}
	context := goalRunRequest(runID).Context
	return VerifiedCheckpoint{
		Version:  VerifiedCheckpointVersion,
		Revision: 1,
		Task:     goalTask(1),
		Run: RunCheckpoint{
			Version:  ReActCheckpointVersion,
			Context:  context,
			Model:    "test-model",
			Messages: []Message{{Role: RoleAssistant, Actions: []Action{pending}}},
			Steps: []Step{{
				Index:   1,
				Actions: []Action{pending},
				Observations: []Observation{{
					ActionID: pending.ID, Name: pending.Name, IsError: true,
				}},
			}},
			PendingAction:     pending,
			PendingResumeKind: ResumeKindToolApproval,
			PendingApprovalID: "approval-1",
			Usage:             TokenUsage{TotalTokens: 3},
		},
		Environment: "twitter",
		Before: &EnvironmentSnapshot{
			ID: "before-persisted", Environment: "twitter", Digest: "sha256:before",
		},
		RepairAttempts: repairs,
	}
}
