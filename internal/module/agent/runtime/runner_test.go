package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

type scriptedModel struct {
	responses []ModelResponse
	errors    []error
	requests  []ModelRequest
	complete  func(context.Context, ModelRequest) (ModelResponse, error)
}

func (m *scriptedModel) Complete(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	m.requests = append(m.requests, request)
	if m.complete != nil {
		return m.complete(ctx, request)
	}
	index := len(m.requests) - 1
	if index < len(m.errors) && m.errors[index] != nil {
		return ModelResponse{}, m.errors[index]
	}
	if index >= len(m.responses) {
		return ModelResponse{}, nil
	}
	return m.responses[index], nil
}

type fakeToolExecutor struct {
	calls      []ToolCall
	results    map[string]string
	structured map[string]json.RawMessage
	err        error
}

func (e *fakeToolExecutor) Execute(_ context.Context, call ToolCall) (ToolResult, error) {
	e.calls = append(e.calls, call)
	if e.err != nil {
		return ToolResult{}, e.err
	}
	return ToolResult{
		Content:           e.results[call.Name],
		StructuredContent: e.structured[call.Name],
	}, nil
}

type fakeRAGSearcher struct {
	queries []RAGQuery
	result  string
}

func (s *fakeRAGSearcher) Search(_ context.Context, query RAGQuery) (RAGResult, error) {
	s.queries = append(s.queries, query)
	return RAGResult{Content: s.result}, nil
}

func TestReActRunnerDirectFinalAnswer(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{
		Message: Message{Content: "direct answer"},
		Usage:   TokenUsage{InputTokens: 10, OutputTokens: 3, TotalTokens: 13},
		Model:   "fake-model",
	}}}
	runner := NewReActRunner(model, nil, nil)

	result, err := runner.Run(context.Background(), baseRunRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != RunStatusCompleted || result.FinalAnswer != "direct answer" {
		t.Fatalf("Run() result = status %q answer %q", result.Status, result.FinalAnswer)
	}
	if len(result.Steps) != 1 || result.Usage.TotalTokens != 13 {
		t.Fatalf("Run() steps/usage = %d/%d", len(result.Steps), result.Usage.TotalTokens)
	}
}

func TestReActRunnerForwardsRequestModel(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Message: Message{Content: "answer"}}}}
	runner := NewReActRunner(model, nil, nil)
	request := baseRunRequest()
	request.Model = "tenant-model"

	if _, err := runner.Run(context.Background(), request); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(model.requests) != 1 || model.requests[0].Model != "tenant-model" {
		t.Fatalf("forwarded model requests = %+v", model.requests)
	}
}

func TestReActRunnerForwardsRequiredToolChoiceOnlyOnInitialStep(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{Actions: []Action{{ID: "call-1", Type: ActionToolCall, Name: "search", Arguments: json.RawMessage(`{"query":"go"}`)}}},
		{Message: Message{Content: "grounded answer"}},
	}}
	runner := NewReActRunner(model, &fakeToolExecutor{results: map[string]string{"search": "result"}}, nil)
	request := baseRunRequest()
	request.Tools = []ToolDefinition{{Name: "search", Category: ToolCategoryRead}}
	request.InitialToolChoice = ToolChoiceRequired

	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalAnswer != "grounded answer" || len(model.requests) != 2 {
		t.Fatalf("Run() result/requests = %+v/%d", result, len(model.requests))
	}
	if model.requests[0].ToolChoice != ToolChoiceRequired || model.requests[1].ToolChoice != "" {
		t.Fatalf("model tool choices = %q, %q", model.requests[0].ToolChoice, model.requests[1].ToolChoice)
	}
}

func TestReActRunnerRejectsRequiredToolChoiceWithoutTools(t *testing.T) {
	model := &scriptedModel{}
	runner := NewReActRunner(model, nil, nil)
	request := baseRunRequest()
	request.InitialToolChoice = ToolChoiceRequired

	_, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorInvalidRequest) || len(model.requests) != 0 {
		t.Fatalf("Run() error/requests = %v/%d", err, len(model.requests))
	}
}

func TestReActRunnerRejectsRequiredToolChoiceWithoutAnswerStep(t *testing.T) {
	model := &scriptedModel{}
	runner := NewReActRunner(model, &fakeToolExecutor{}, nil)
	request := baseRunRequest()
	request.Context.Budget.MaxSteps = 1
	request.Tools = []ToolDefinition{{Name: "search", Category: ToolCategoryRead}}
	request.InitialToolChoice = ToolChoiceRequired

	_, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorInvalidRequest) || len(model.requests) != 0 {
		t.Fatalf("Run() error/requests = %v/%d", err, len(model.requests))
	}
}

func TestReActRunnerCopiesExplicitFinalActionIntoMessageTrace(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Actions: []Action{{
		Type: ActionFinalAnswer, Content: "explicit final",
	}}}}}
	runner := NewReActRunner(model, nil, nil)

	result, err := runner.Run(context.Background(), baseRunRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	lastMessage := result.Messages[len(result.Messages)-1]
	if lastMessage.Role != RoleAssistant || lastMessage.Content != "explicit final" {
		t.Fatalf("final trace message = %+v", lastMessage)
	}
}

func TestReActRunnerToolCallThenFinalAnswer(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{
			Actions: []Action{{
				ID: "call-1", Type: ActionToolCall, Name: "search",
				Arguments: json.RawMessage(`{"query":"golang"}`),
			}},
		},
		{Message: Message{Content: "found it"}},
	}}
	executor := &fakeToolExecutor{
		results:    map[string]string{"search": "tweet-42"},
		structured: map[string]json.RawMessage{"search": json.RawMessage(`{"items":[{"id":"42"}]}`)},
	}
	runner := NewReActRunner(model, executor, nil)
	request := baseRunRequest()
	request.Context.Budget.MaxSteps = 2
	request.Tools = []ToolDefinition{{Name: "search", Category: ToolCategoryRead}}

	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalAnswer != "found it" || len(result.Steps) != 2 {
		t.Fatalf("Run() answer/steps = %q/%d", result.FinalAnswer, len(result.Steps))
	}
	if len(executor.calls) != 1 || executor.calls[0].RunContext.UserID != 7 {
		t.Fatalf("tool calls = %+v", executor.calls)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model requests = %d, want 2", len(model.requests))
	}
	if len(model.requests[1].Tools) != 0 || model.requests[1].ToolChoice != "" {
		t.Fatalf("final model request tools/tool choice = %d/%q", len(model.requests[1].Tools), model.requests[1].ToolChoice)
	}
	lastFinalMessage := model.requests[1].Messages[len(model.requests[1].Messages)-1]
	if lastFinalMessage.Role != RoleSystem || lastFinalMessage.Content != finalStepSystemInstruction {
		t.Fatalf("final model request instruction = %+v", lastFinalMessage)
	}
	secondMessages := model.requests[1].Messages
	toolMessage := secondMessages[len(secondMessages)-2]
	if toolMessage.Role != RoleTool || toolMessage.ToolCallID != "call-1" || toolMessage.Content != "tweet-42" {
		t.Fatalf("tool observation message = %+v", toolMessage)
	}
	if got := string(result.Steps[0].Observations[0].StructuredContent); got != `{"items":[{"id":"42"}]}` {
		t.Fatalf("structured observation = %s", got)
	}
}

func TestReActRunnerMultipleToolCallsPreservePairing(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{
		{Actions: []Action{
			{ID: "call-a", Type: ActionToolCall, Name: "alpha", Arguments: json.RawMessage(`{"id":1}`)},
			{ID: "call-b", Type: ActionToolCall, Name: "beta", Arguments: json.RawMessage(`{"id":2}`)},
		}},
		{Message: Message{Content: "combined"}},
	}}
	executor := &fakeToolExecutor{results: map[string]string{"alpha": "A", "beta": "B"}}
	runner := NewReActRunner(model, executor, nil)
	request := baseRunRequest()
	request.Tools = []ToolDefinition{
		{Name: "alpha", Category: ToolCategoryRead},
		{Name: "beta", Category: ToolCategoryRead},
	}

	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalAnswer != "combined" || len(executor.calls) != 2 {
		t.Fatalf("answer/calls = %q/%d", result.FinalAnswer, len(executor.calls))
	}
	messages := model.requests[1].Messages
	if len(messages) < 2 {
		t.Fatalf("second model request has %d messages", len(messages))
	}
	first := messages[len(messages)-2]
	second := messages[len(messages)-1]
	if first.ToolCallID != "call-a" || first.Content != "A" {
		t.Fatalf("first observation = %+v", first)
	}
	if second.ToolCallID != "call-b" || second.Content != "B" {
		t.Fatalf("second observation = %+v", second)
	}
}

func TestReActRunnerActionFailures(t *testing.T) {
	tests := []struct {
		name     string
		response ModelResponse
		tools    []ToolDefinition
		executor *fakeToolExecutor
		wantCode ErrorCode
	}{
		{
			name: "invalid json arguments",
			response: ModelResponse{Actions: []Action{{
				ID: "bad-json", Type: ActionToolCall, Name: "search", Arguments: json.RawMessage(`{"query":`),
			}}},
			tools:    []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
			executor: &fakeToolExecutor{},
			wantCode: ErrorInvalidAction,
		},
		{
			name: "unknown tool",
			response: ModelResponse{Actions: []Action{{
				ID: "unknown", Type: ActionToolCall, Name: "missing", Arguments: json.RawMessage(`{}`),
			}}},
			executor: &fakeToolExecutor{},
			wantCode: ErrorUnknownTool,
		},
		{
			name: "tool error",
			response: ModelResponse{Actions: []Action{{
				ID: "tool-error", Type: ActionToolCall, Name: "search", Arguments: json.RawMessage(`{}`),
			}}},
			tools:    []ToolDefinition{{Name: "search", Category: ToolCategoryRead}},
			executor: &fakeToolExecutor{err: errors.New("backend unavailable")},
			wantCode: ErrorTool,
		},
		{
			name:     "empty model response",
			response: ModelResponse{},
			executor: &fakeToolExecutor{},
			wantCode: ErrorEmptyResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &scriptedModel{responses: []ModelResponse{tt.response}}
			runner := NewReActRunner(model, tt.executor, nil)
			request := baseRunRequest()
			request.Tools = tt.tools
			result, err := runner.Run(context.Background(), request)
			if !HasErrorCode(err, tt.wantCode) {
				t.Fatalf("Run() error = %v, want code %s", err, tt.wantCode)
			}
			if result.Status != RunStatusFailed {
				t.Fatalf("Run() status = %q, want failed", result.Status)
			}
		})
	}
}

func TestReActRunnerMaxSteps(t *testing.T) {
	model := &scriptedModel{complete: func(_ context.Context, _ ModelRequest) (ModelResponse, error) {
		return ModelResponse{Actions: []Action{{
			ID: "again", Type: ActionToolCall, Name: "search", Arguments: json.RawMessage(`{}`),
		}}}, nil
	}}
	executor := &fakeToolExecutor{results: map[string]string{"search": "continue"}}
	runner := NewReActRunner(model, executor, nil)
	request := baseRunRequest()
	request.Context.Budget.MaxSteps = 2
	request.Tools = []ToolDefinition{{Name: "search", Category: ToolCategoryRead}}

	result, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorMaxSteps) {
		t.Fatalf("Run() error = %v, want max steps", err)
	}
	if len(result.Steps) != 2 || len(executor.calls) != 1 {
		t.Fatalf("steps/calls = %d/%d", len(result.Steps), len(executor.calls))
	}
	if len(model.requests) != 2 || len(model.requests[1].Tools) != 0 || model.requests[1].ToolChoice != "" {
		t.Fatalf("model requests/final tools/tool choice = %d/%d/%q", len(model.requests), len(model.requests[1].Tools), model.requests[1].ToolChoice)
	}
	lastFinalMessage := model.requests[1].Messages[len(model.requests[1].Messages)-1]
	if lastFinalMessage.Role != RoleSystem || lastFinalMessage.Content != finalStepSystemInstruction {
		t.Fatalf("final model request instruction = %+v", lastFinalMessage)
	}
}

func TestReActRunnerTimeoutAndCancellation(t *testing.T) {
	t.Run("run timeout", func(t *testing.T) {
		model := &scriptedModel{complete: func(ctx context.Context, _ ModelRequest) (ModelResponse, error) {
			<-ctx.Done()
			return ModelResponse{}, ctx.Err()
		}}
		runner := NewReActRunner(model, nil, nil)
		request := baseRunRequest()
		request.Context.Budget.Timeout = 20 * time.Millisecond

		started := time.Now()
		_, err := runner.Run(context.Background(), request)
		if !HasErrorCode(err, ErrorTimeout) {
			t.Fatalf("Run() error = %v, want timeout", err)
		}
		if time.Since(started) > time.Second {
			t.Fatalf("timeout took too long: %v", time.Since(started))
		}
	})

	t.Run("upstream cancellation", func(t *testing.T) {
		model := &scriptedModel{}
		runner := NewReActRunner(model, nil, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := runner.Run(ctx, baseRunRequest())
		if !HasErrorCode(err, ErrorCanceled) {
			t.Fatalf("Run() error = %v, want canceled", err)
		}
		if len(model.requests) != 0 {
			t.Fatalf("model was called %d times after cancellation", len(model.requests))
		}
	})
}

func TestReActRunnerWriteToolRequiresApproval(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Actions: []Action{{
		ID: "publish", Type: ActionToolCall, Name: "create_tweet", Arguments: json.RawMessage(`{"content":"hello"}`),
	}}}}}
	executor := &fakeToolExecutor{}
	runner := NewReActRunner(model, executor, nil)
	request := baseRunRequest()
	request.Tools = []ToolDefinition{{Name: "create_tweet", Category: ToolCategoryWrite}}

	result, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorApprovalRequired) || !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("Run() error = %v, want approval required", err)
	}
	if result.Status != RunStatusApprovalRequired || result.PendingAction == nil {
		t.Fatalf("Run() status/pending = %q/%+v", result.Status, result.PendingAction)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("write tool executed without approval: %+v", executor.calls)
	}
}

func TestReActRunnerRAGAndAskHumanActions(t *testing.T) {
	t.Run("rag search", func(t *testing.T) {
		model := &scriptedModel{responses: []ModelResponse{
			{Actions: []Action{{ID: "rag", Type: ActionRAGSearch, Name: "memory", Arguments: json.RawMessage(`{"query":"context"}`)}}},
			{Message: Message{Content: "grounded answer"}},
		}}
		rag := &fakeRAGSearcher{result: "retrieved context"}
		runner := NewReActRunner(model, nil, rag)
		result, err := runner.Run(context.Background(), baseRunRequest())
		if err != nil || result.FinalAnswer != "grounded answer" || len(rag.queries) != 1 {
			t.Fatalf("Run() result/error/queries = %+v/%v/%d", result, err, len(rag.queries))
		}
	})

	t.Run("ask human", func(t *testing.T) {
		model := &scriptedModel{responses: []ModelResponse{{Actions: []Action{{
			ID: "human", Type: ActionAskHuman, Content: "请选择发布版本",
		}}}}}
		runner := NewReActRunner(model, nil, nil)
		result, err := runner.Run(context.Background(), baseRunRequest())
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if result.Status != RunStatusAwaitingHuman || result.PendingAction == nil {
			t.Fatalf("Run() status/pending = %q/%+v", result.Status, result.PendingAction)
		}
	})
}

type fixedTokenCounter struct {
	request  TokenUsage
	response TokenUsage
}

func (c fixedTokenCounter) CountText(string) int { return 1 }

func (c fixedTokenCounter) CountMessages([]Message) int { return c.request.InputTokens }

func (c fixedTokenCounter) EstimateRequest(ModelRequest) TokenUsage { return c.request }

func (c fixedTokenCounter) EstimateResponse(ModelResponse) TokenUsage { return c.response }

type fixedCostEstimator struct {
	microsPerToken int64
	version        string
}

func (e fixedCostEstimator) EstimateCost(_ string, usage TokenUsage) (CostEstimate, error) {
	return CostEstimate{
		Micros:         int64(usage.InputTokens+usage.OutputTokens) * e.microsPerToken,
		PricingVersion: e.version,
	}, nil
}

func TestReActRunnerRejectsModelCallBeforeInputBudgetIsExceeded(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Message: Message{Content: "should not run"}}}}
	runner := NewReActRunner(model, nil, nil, WithTokenCounter(fixedTokenCounter{
		request: TokenUsage{InputTokens: 51, TotalTokens: 51, Estimated: true},
	}))
	request := baseRunRequest()
	request.Context.Budget.MaxInputTokens = 50

	result, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("Run() error = %v, want budget exceeded", err)
	}
	if len(model.requests) != 0 || result.Status != RunStatusFailed {
		t.Fatalf("model calls/status = %d/%q", len(model.requests), result.Status)
	}
}

func TestReActRunnerReservesOutputBeforeTotalBudgetAdmission(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Message: Message{Content: "should not run"}}}}
	runner := NewReActRunner(model, nil, nil, WithTokenCounter(fixedTokenCounter{
		request: TokenUsage{InputTokens: 40, TotalTokens: 40, Estimated: true},
	}))
	request := baseRunRequest()
	request.Context.Budget.MaxOutputTokens = 20
	request.Context.Budget.MaxTotalTokens = 59

	_, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorBudgetExceeded) || len(model.requests) != 0 {
		t.Fatalf("Run() error/model calls = %v/%d", err, len(model.requests))
	}
}

func TestReActRunnerUsesEstimatedUsageWhenProviderOmitsUsage(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Message: Message{Content: "answer"}}}}
	runner := NewReActRunner(model, nil, nil, WithTokenCounter(fixedTokenCounter{
		request:  TokenUsage{InputTokens: 12, TotalTokens: 12, Estimated: true},
		response: TokenUsage{OutputTokens: 7, TotalTokens: 7, Estimated: true},
	}))

	result, err := runner.Run(context.Background(), baseRunRequest())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 7 || result.Usage.TotalTokens != 19 || !result.Usage.Estimated {
		t.Fatalf("Run() usage = %+v", result.Usage)
	}
	if len(result.Steps) != 1 || !result.Steps[0].Usage.Estimated {
		t.Fatalf("Run() step usage = %+v", result.Steps)
	}
}

func TestReActRunnerRecordsProviderUsageBeforePostCallBudgetFailure(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{
		Message: Message{Content: "too long"},
		Usage:   TokenUsage{InputTokens: 10, OutputTokens: 21, TotalTokens: 31},
	}}}
	runner := NewReActRunner(model, nil, nil, WithTokenCounter(fixedTokenCounter{
		request: TokenUsage{InputTokens: 10, TotalTokens: 10, Estimated: true},
	}))
	request := baseRunRequest()
	request.Context.Budget.MaxOutputTokens = 20

	result, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("Run() error = %v, want budget exceeded", err)
	}
	if len(result.Steps) != 1 || result.Usage.TotalTokens != 31 || result.Usage.Estimated {
		t.Fatalf("failed run usage/steps = %+v/%+v", result.Usage, result.Steps)
	}
}

func TestReActRunnerRejectsCostReservationBeforeModelCall(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Message: Message{Content: "should not run"}}}}
	runner := NewReActRunner(model, nil, nil,
		WithTokenCounter(fixedTokenCounter{request: TokenUsage{InputTokens: 10, TotalTokens: 10, Estimated: true}}),
		WithCostEstimator(fixedCostEstimator{microsPerToken: 2, version: "pricing-v1"}),
	)
	request := baseRunRequest()
	request.Model = "priced-model"
	request.Context.Budget.MaxOutputTokens = 20
	request.Context.Budget.MaxEstimatedCostMicros = 59

	result, err := runner.Run(context.Background(), request)
	if !HasErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("Run() error = %v, want budget exceeded", err)
	}
	if len(model.requests) != 0 || result.Usage.EstimatedCostMicros != 0 {
		t.Fatalf("model calls/usage = %d/%+v", len(model.requests), result.Usage)
	}
}

func TestReActRunnerRecordsCompletedEstimatedCost(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{
		Message: Message{Content: "answer"},
		Usage:   TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		Model:   "priced-model",
	}}}
	runner := NewReActRunner(model, nil, nil,
		WithTokenCounter(fixedTokenCounter{request: TokenUsage{InputTokens: 10, TotalTokens: 10, Estimated: true}}),
		WithCostEstimator(fixedCostEstimator{microsPerToken: 3, version: "pricing-v1"}),
	)
	request := baseRunRequest()
	request.Model = "priced-model"
	request.Context.Budget.MaxOutputTokens = 20
	request.Context.Budget.MaxEstimatedCostMicros = 100

	result, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Usage.EstimatedCostMicros != 45 || result.Usage.PricingVersion != "pricing-v1" || result.Usage.CostEstimated {
		t.Fatalf("Run() cost usage = %+v", result.Usage)
	}
}

func TestReActRunnerReservesSharedWorkflowBudgetBeforeModelCall(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{Message: Message{Content: "should not run"}}}}
	runner := NewReActRunner(model, nil, nil, WithTokenCounter(fixedTokenCounter{
		request: TokenUsage{InputTokens: 10, TotalTokens: 10, Estimated: true},
	}))
	request := baseRunRequest()
	request.Context.Budget.MaxOutputTokens = 20
	tracker, err := NewBudgetTracker(Budget{MaxTotalTokens: 29})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}
	ctx := ContextWithBudgetTracker(context.Background(), tracker)

	result, err := runner.Run(ctx, request)
	if !HasErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("Run() error = %v, want budget exceeded", err)
	}
	if len(model.requests) != 0 || result.Status != RunStatusFailed {
		t.Fatalf("model calls/status = %d/%q", len(model.requests), result.Status)
	}
	if snapshot := tracker.Snapshot(); snapshot.Usage.TotalTokens != 0 || snapshot.Reserved.TotalTokens != 0 {
		t.Fatalf("shared budget snapshot = %+v", snapshot)
	}
}

func TestReActRunnerCommitsProviderUsageToSharedWorkflowBudget(t *testing.T) {
	model := &scriptedModel{responses: []ModelResponse{{
		Message: Message{Content: "answer"},
		Usage:   TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}}}
	runner := NewReActRunner(model, nil, nil, WithTokenCounter(fixedTokenCounter{
		request: TokenUsage{InputTokens: 10, TotalTokens: 10, Estimated: true},
	}))
	request := baseRunRequest()
	request.Context.Budget.MaxOutputTokens = 20
	tracker, err := NewBudgetTracker(Budget{MaxTotalTokens: 100})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}
	ctx := ContextWithBudgetTracker(context.Background(), tracker)

	result, err := runner.Run(ctx, request)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalAnswer != "answer" || len(model.requests) != 1 {
		t.Fatalf("Run() answer/model calls = %q/%d", result.FinalAnswer, len(model.requests))
	}
	snapshot := tracker.Snapshot()
	if snapshot.Usage.TotalTokens != 15 || snapshot.Usage.InputTokens != 10 || snapshot.Usage.OutputTokens != 5 {
		t.Fatalf("shared budget usage = %+v", snapshot.Usage)
	}
	if snapshot.Reserved.TotalTokens != 0 {
		t.Fatalf("shared budget reservation = %+v", snapshot.Reserved)
	}
}

func TestReActRunnerConcurrencyReservationRollsBackAfterFailure(t *testing.T) {
	limiter := NewInMemoryConcurrencyLimiter(ConcurrencyLimits{MaxPerUser: 1})
	model := &scriptedModel{
		errors:    []error{errors.New("provider unavailable")},
		responses: []ModelResponse{{}, {Message: Message{Content: "recovered"}}},
	}
	runner := NewReActRunner(model, nil, nil, WithAdmissionController(limiter))

	if _, err := runner.Run(context.Background(), baseRunRequest()); !HasErrorCode(err, ErrorModel) {
		t.Fatalf("first Run() error = %v, want model error", err)
	}
	result, err := runner.Run(context.Background(), baseRunRequest())
	if err != nil || result.FinalAnswer != "recovered" {
		t.Fatalf("second Run() result/error = %+v/%v", result, err)
	}
}

func TestReActRunnerRejectsConcurrentRunForSameUser(t *testing.T) {
	started := make(chan struct{})
	unblock := make(chan struct{})
	model := &scriptedModel{complete: func(_ context.Context, _ ModelRequest) (ModelResponse, error) {
		close(started)
		<-unblock
		return ModelResponse{Message: Message{Content: "first"}}, nil
	}}
	runner := NewReActRunner(model, nil, nil, WithAdmissionController(
		NewInMemoryConcurrencyLimiter(ConcurrencyLimits{MaxPerUser: 1}),
	))
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(context.Background(), baseRunRequest())
		done <- err
	}()
	<-started

	if _, err := runner.Run(context.Background(), baseRunRequest()); !HasErrorCode(err, ErrorConcurrencyLimit) {
		t.Fatalf("concurrent Run() error = %v, want concurrency limit", err)
	}
	close(unblock)
	if err := <-done; err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
}

func baseRunRequest() RunRequest {
	return RunRequest{
		Context: RunContext{
			RunID:  "run-test",
			UserID: 7,
			Mode:   ModeConsult,
			Budget: Budget{MaxSteps: 5},
		},
		Messages: []Message{{Role: RoleUser, Content: fmt.Sprintf("question-%d", 7)}},
	}
}
