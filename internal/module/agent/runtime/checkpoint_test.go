package runtime

import (
	"context"
	"encoding/json"
	"testing"
)

type checkpointApprovalExecutor struct {
	approvalID      string
	approved        bool
	readExecutions  int
	writeAttempts   int
	writeExecutions int
}

type checkpointResumableToolExecutor struct {
	executeCalls int
	resumeCalls  int
	resume       ToolResumeRequest
	continuation ToolContinuation
}

func (e *checkpointResumableToolExecutor) Execute(
	_ context.Context,
	_ ToolCall,
) (ToolResult, error) {
	e.executeCalls++
	if e.continuation.Version != "" {
		return ToolResult{}, &ToolSuspensionError{Continuation: e.continuation}
	}
	return ToolResult{}, &ToolSuspensionError{Continuation: ToolContinuation{
		Version: "test.continuation.v1",
		Prompt:  "Which repository should the workflow inspect?",
		State:   json.RawMessage(`{"child_run_id":"child-1"}`),
	}}
}

func (e *checkpointResumableToolExecutor) ResumeTool(
	_ context.Context,
	request ToolResumeRequest,
) (ToolResult, error) {
	e.resumeCalls++
	e.resume = request
	return ToolResult{Content: "workflow inspected repository"}, nil
}

func (e *checkpointApprovalExecutor) Execute(_ context.Context, call ToolCall) (ToolResult, error) {
	if call.Name == "lookup" {
		e.readExecutions++
		return ToolResult{Content: "lookup result"}, nil
	}
	return ToolResult{}, &RunError{Code: ErrorTool, ActionID: call.ActionID, Message: "unexpected non-governed tool"}
}

func (e *checkpointApprovalExecutor) ExecuteApprovalGated(_ context.Context, call ToolCall) (ToolResult, error) {
	e.writeAttempts++
	if !e.approved {
		return ToolResult{}, &RunError{
			Code: ErrorApprovalRequired, ActionID: call.ActionID, ApprovalID: e.approvalID,
			Message: "approval required", Cause: ErrApprovalRequired,
		}
	}
	e.writeExecutions++
	return ToolResult{Content: "published"}, nil
}

func TestReActRunnerResumesSuspendedToolWithoutReplayingInitialCall(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{Actions: []Action{{
			ID: "workflow-1", Type: ActionToolCall, Name: "workflow_read",
			Arguments: json.RawMessage(`{"user_input":"inspect"}`),
		}}},
		{Message: Message{Content: "The workflow finished after your response."}},
	}}
	executor := &checkpointResumableToolExecutor{}
	runner := NewReActRunner(model, executor, nil)
	request := baseRunRequest()
	request.Model = "test-model"
	request.Tools = []ToolDefinition{{Name: "workflow_read", Category: ToolCategoryRead}}

	suspended, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if suspended.Status != RunStatusAwaitingHuman ||
		suspended.PendingResumeKind != ResumeKindHumanResponse ||
		suspended.PendingToolContinuation == nil {
		t.Fatalf("Run() suspended result = %+v", suspended)
	}
	checkpoint, err := NewRunCheckpoint(request, suspended)
	if err != nil {
		t.Fatalf("NewRunCheckpoint() error = %v", err)
	}

	resumed, err := runner.Resume(context.Background(), ResumeRequest{
		Checkpoint:    checkpoint,
		HumanResponse: "twitter-clone",
		Tools:         request.Tools,
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Status != RunStatusCompleted ||
		resumed.FinalAnswer != "The workflow finished after your response." {
		t.Fatalf("Resume() result = %+v", resumed)
	}
	if executor.executeCalls != 1 || executor.resumeCalls != 1 ||
		executor.resume.HumanResponse != "twitter-clone" ||
		string(executor.resume.Continuation.State) != `{"child_run_id":"child-1"}` {
		t.Fatalf("executor calls/request = %d/%d/%+v", executor.executeCalls, executor.resumeCalls, executor.resume)
	}
	if len(model.requests) != 2 || model.requests[1].StepIndex != 2 {
		t.Fatalf("model requests = %+v", model.requests)
	}
}

func TestReActRunnerResumesDelegatedToolApprovalWithChildGrant(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{Actions: []Action{{
			ID: "workflow-approval-1", Type: ActionToolCall, Name: "workflow_governed",
			Arguments: json.RawMessage(`{"user_input":"publish the approved draft"}`),
		}}},
		{Message: Message{Content: "The governed workflow finished."}},
	}}
	executor := &checkpointResumableToolExecutor{continuation: ToolContinuation{
		Version:    "test.continuation.v1",
		Prompt:     "Approve the child workflow action.",
		ResumeKind: ResumeKindDelegatedToolApproval,
		ApprovalID: "child-approval-1",
		State:      json.RawMessage(`{"child_run_id":"child-1"}`),
	}}
	runner := NewReActRunner(model, executor, nil)
	request := baseRunRequest()
	request.Model = "test-model"
	request.Tools = []ToolDefinition{{Name: "workflow_governed", Category: ToolCategoryRead}}

	suspended, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorApprovalRequired) ||
		suspended.Status != RunStatusApprovalRequired ||
		suspended.PendingResumeKind != ResumeKindDelegatedToolApproval ||
		suspended.ApprovalID != "child-approval-1" {
		t.Fatalf("Run() delegated suspension/result = %v/%+v", err, suspended)
	}
	checkpoint, err := NewRunCheckpoint(request, suspended)
	if err != nil {
		t.Fatalf("NewRunCheckpoint() error = %v", err)
	}

	resumed, err := runner.Resume(context.Background(), ResumeRequest{
		Checkpoint:  checkpoint,
		ApprovalID:  "child-approval-1",
		ResumeToken: "child-resume-token",
		Tools:       request.Tools,
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Status != RunStatusCompleted || resumed.FinalAnswer != "The governed workflow finished." {
		t.Fatalf("Resume() result = %+v", resumed)
	}
	if executor.executeCalls != 1 || executor.resumeCalls != 1 ||
		executor.resume.ApprovalID != "child-approval-1" ||
		executor.resume.ResumeToken != "child-resume-token" ||
		executor.resume.HumanResponse != "" {
		t.Fatalf("executor calls/request = %d/%d/%+v", executor.executeCalls, executor.resumeCalls, executor.resume)
	}
}

func TestReActRunnerResumesAskHumanCheckpointWithoutRepeatingPriorSteps(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{Actions: []Action{{ID: "question-1", Type: ActionAskHuman, Content: "Which scope?"}}},
		{Message: Message{Content: "Using the repository scope."}},
	}}
	runner := NewReActRunner(model, nil, nil)
	request := baseRunRequest()
	request.Model = "test-model"

	suspended, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	checkpoint, err := NewRunCheckpoint(request, suspended)
	if err != nil {
		t.Fatalf("NewRunCheckpoint() error = %v", err)
	}
	resumed, err := runner.Resume(context.Background(), ResumeRequest{
		Checkpoint: checkpoint, HumanResponse: "repository",
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Status != RunStatusCompleted || resumed.FinalAnswer != "Using the repository scope." {
		t.Fatalf("Resume() result = %+v", resumed)
	}
	if len(resumed.Steps) != 2 || len(model.requests) != 2 || model.requests[1].StepIndex != 2 {
		t.Fatalf("steps/requests = %d/%d, second request = %+v", len(resumed.Steps), len(model.requests), model.requests[1])
	}
	lastInput := model.requests[1].Messages[len(model.requests[1].Messages)-1]
	if lastInput.Role != RoleUser || lastInput.Content != "repository" {
		t.Fatalf("resume human input = %+v", lastInput)
	}
}

func TestReActRunnerResumeUsesFreshToolAuthorization(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{Actions: []Action{{ID: "question-1", Type: ActionAskHuman, Content: "Search now?"}}},
		{Actions: []Action{{ID: "search-1", Type: ActionToolCall, Name: "search"}}},
	}}
	runner := NewReActRunner(model, &fakeToolExecutor{}, nil)
	request := baseRunRequest()
	request.Model = "test-model"
	request.Tools = []ToolDefinition{{Name: "search", Category: ToolCategoryRead}}
	suspended, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	checkpoint, err := NewRunCheckpoint(request, suspended)
	if err != nil {
		t.Fatalf("NewRunCheckpoint() error = %v", err)
	}

	result, err := runner.Resume(context.Background(), ResumeRequest{
		Checkpoint: checkpoint, HumanResponse: "yes", Tools: nil,
	})
	if !HasErrorCode(err, ErrorUnknownTool) || result.Status != RunStatusFailed {
		t.Fatalf("Resume() result/error = %+v/%v, want fresh catalog denial", result, err)
	}
}

func TestReActRunnerResumesApprovedToolWithoutRepeatingModelOrPriorAction(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{Actions: []Action{
			{ID: "lookup-1", Type: ActionToolCall, Name: "lookup", Arguments: json.RawMessage(`{"query":"go"}`)},
			{ID: "publish-1", Type: ActionToolCall, Name: "publish", Arguments: json.RawMessage(`{"content":"hello"}`)},
		}},
		{Message: Message{Content: "Published after approval."}},
	}}
	executor := &checkpointApprovalExecutor{approvalID: "approval-1"}
	runner := NewReActRunner(model, executor, nil)
	request := baseRunRequest()
	request.Model = "test-model"
	request.Tools = []ToolDefinition{
		{Name: "lookup", Category: ToolCategoryRead},
		{Name: "publish", Category: ToolCategoryWrite},
	}

	suspended, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorApprovalRequired) {
		t.Fatalf("Run() error = %v, want approval required", err)
	}
	if suspended.Status != RunStatusApprovalRequired || suspended.ApprovalID != "approval-1" {
		t.Fatalf("Run() suspended result = %+v", suspended)
	}
	if len(model.requests) != 1 || executor.readExecutions != 1 || executor.writeExecutions != 0 {
		t.Fatalf("pre-resume model/read/write = %d/%d/%d", len(model.requests), executor.readExecutions, executor.writeExecutions)
	}
	checkpoint, err := NewRunCheckpoint(request, suspended)
	if err != nil {
		t.Fatalf("NewRunCheckpoint() error = %v", err)
	}

	executor.approved = true
	resumed, err := runner.Resume(context.Background(), ResumeRequest{
		Checkpoint: checkpoint,
		ApprovalID: "approval-1",
		Tools:      request.Tools,
	})
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Status != RunStatusCompleted || resumed.FinalAnswer != "Published after approval." {
		t.Fatalf("Resume() result = %+v", resumed)
	}
	if len(model.requests) != 2 || model.requests[1].StepIndex != 2 {
		t.Fatalf("model requests = %+v", model.requests)
	}
	if executor.readExecutions != 1 || executor.writeAttempts != 2 || executor.writeExecutions != 1 {
		t.Fatalf("post-resume read/attempt/write = %d/%d/%d", executor.readExecutions, executor.writeAttempts, executor.writeExecutions)
	}
}

func TestReActRunnerApprovalResumeRejectsMismatchedApprovalAndRevokedTool(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Actions: []Action{{
		ID: "publish-1", Type: ActionToolCall, Name: "publish", Arguments: json.RawMessage(`{"content":"hello"}`),
	}}}}}
	executor := &checkpointApprovalExecutor{approvalID: "approval-1"}
	runner := NewReActRunner(model, executor, nil)
	request := baseRunRequest()
	request.Model = "test-model"
	request.Tools = []ToolDefinition{{Name: "publish", Category: ToolCategoryWrite}}
	suspended, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorApprovalRequired) {
		t.Fatalf("Run() error = %v", err)
	}
	checkpoint, err := NewRunCheckpoint(request, suspended)
	if err != nil {
		t.Fatalf("NewRunCheckpoint() error = %v", err)
	}

	executor.approved = true
	result, err := runner.Resume(context.Background(), ResumeRequest{
		Checkpoint: checkpoint, ApprovalID: "approval-other", Tools: request.Tools,
	})
	if !HasErrorCode(err, ErrorInvalidRequest) || result.Status != RunStatusFailed || executor.writeExecutions != 0 {
		t.Fatalf("mismatched Resume() result/error/executions = %+v/%v/%d", result, err, executor.writeExecutions)
	}

	result, err = runner.Resume(context.Background(), ResumeRequest{
		Checkpoint: checkpoint, ApprovalID: "approval-1", Tools: nil,
	})
	if !HasErrorCode(err, ErrorUnknownTool) || result.Status != RunStatusFailed || executor.writeExecutions != 0 {
		t.Fatalf("revoked Resume() result/error/executions = %+v/%v/%d", result, err, executor.writeExecutions)
	}
}

func TestRunCheckpointRejectsUnpairedPendingAction(t *testing.T) {
	checkpoint := RunCheckpoint{
		Version: ReActCheckpointVersion,
		Context: RunContext{RunID: "run-1", UserID: 42},
		Model:   "test-model",
		Messages: []Message{{Role: RoleAssistant, Actions: []Action{{
			ID: "different", Type: ActionAskHuman, Content: "Question?",
		}}}},
		Steps: []Step{{Index: 1, Actions: []Action{{
			ID: "question-1", Type: ActionAskHuman, Content: "Question?",
		}}}},
		PendingAction: Action{ID: "question-1", Type: ActionAskHuman, Content: "Question?"},
	}
	if err := ValidateRunCheckpoint(checkpoint); err == nil {
		t.Fatal("ValidateRunCheckpoint() error = nil")
	}
}
