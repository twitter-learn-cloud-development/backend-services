package runtime

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDecodeShortPlanProposalRejectsAmbiguousModelOutput(t *testing.T) {
	valid := `{"version":"agent.short_plan.v1","steps":[{"id":"answer","kind":"respond","objective":"answer","criterion_ids":["answer-written"]}]}`
	tests := []struct {
		name    string
		payload []byte
		want    string
	}{
		{name: "markdown fence", payload: []byte("```json\n" + valid + "\n```"), want: "invalid character"},
		{name: "trailing value", payload: []byte(valid + ` {}`), want: "multiple JSON values"},
		{name: "unknown envelope field", payload: []byte(`{"version":"agent.short_plan.v1","steps":[{"id":"answer","kind":"respond","objective":"answer","criterion_ids":["answer-written"]}],"approved":true}`), want: "unknown field"},
		{name: "unknown step field", payload: []byte(`{"version":"agent.short_plan.v1","steps":[{"id":"answer","kind":"respond","objective":"answer","criterion_ids":["answer-written"],"approval_required":false}]}`), want: "unknown field"},
		{name: "duplicate key", payload: []byte(`{"version":"agent.short_plan.v1","version":"other","steps":[{"id":"answer","kind":"respond","objective":"answer","criterion_ids":["answer-written"]}]}`), want: "duplicate JSON object key"},
		{name: "missing field", payload: []byte(`{"version":"agent.short_plan.v1","steps":[{"id":"answer","kind":"respond","criterion_ids":["answer-written"]}]}`), want: "missing required field"},
		{name: "invalid utf8", payload: []byte{'{', '"', 0xff, '"', '}'}, want: "not valid UTF-8"},
		{name: "oversized", payload: bytes.Repeat([]byte{' '}, maxShortPlanResponseBytes+1), want: "exceeds"},
		{name: "too deep", payload: []byte(strings.Repeat("[", maxShortPlanJSONDepth+2) + "0" + strings.Repeat("]", maxShortPlanJSONDepth+2)), want: "nesting exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeShortPlanProposal(test.payload)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeShortPlanProposal() error = %v, want containing %q", err, test.want)
			}
		})
	}
	proposal, err := DecodeShortPlanProposal([]byte(valid))
	if err != nil || proposal.Version != ShortPlanVersionV1 || len(proposal.Steps) != 1 {
		t.Fatalf("valid proposal/error = %+v/%v", proposal, err)
	}
}

func TestModelShortHorizonPlannerRepairsMalformedJSONOnceAndAccountsUsage(t *testing.T) {
	model := &scriptedShortPlanModel{responses: []ModelResponse{
		{
			Message: modelResponseMessage("not json"),
			Usage:   TokenUsage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
			Model:   "planner-model", Provider: "fake",
		},
		{
			Message: modelResponseMessage(`{"version":"agent.short_plan.v1","steps":[{"id":"search","kind":"tool","objective":"find evidence","tool_name":"web_search","criterion_ids":["source-found"]},{"id":"answer","kind":"respond","objective":"answer from evidence","criterion_ids":["answer-written"]}]}`),
			Usage:   TokenUsage{InputTokens: 4, OutputTokens: 6, TotalTokens: 10},
			Model:   "planner-model", Provider: "fake",
		},
	}}
	planner, err := NewModelShortHorizonPlanner(
		model,
		WithShortPlanTokenCounter(fixedShortPlanTokenCounter{requestTokens: 2}),
		WithShortPlanMaxOutputTokens(64),
	)
	if err != nil {
		t.Fatalf("NewModelShortHorizonPlanner() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.Model = "planner-model"
	request.Budget.MaxOutputTokens = 32
	request.Budget.MaxTotalTokens = 100

	result, err := planner.Plan(context.Background(), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Attempts != 2 || result.Usage.TotalTokens != 15 || len(result.Proposal.Steps) != 2 {
		t.Fatalf("plan result = %+v", result)
	}
	if len(model.requests) != 2 || model.requests[0].ToolChoice != ToolChoiceNone ||
		len(model.requests[0].Tools) != 0 || model.requests[0].MaxOutputTokens != 32 {
		t.Fatalf("model requests = %+v", model.requests)
	}
	if len(model.requests[1].Messages) != 3 ||
		strings.Contains(model.requests[1].Messages[2].Content, "not json") {
		t.Fatalf("repair messages = %+v", model.requests[1].Messages)
	}
	admitted, err := (DeterministicShortPlanPolicy{}).Admit(context.Background(), request, result.Proposal)
	if err != nil || len(admitted.Steps) != 2 {
		t.Fatalf("Admit(result) = %+v/%v", admitted, err)
	}
}

func TestModelShortHorizonPlannerDoesNotRetryProviderFailure(t *testing.T) {
	model := &scriptedShortPlanModel{errors: []error{errors.New("provider unavailable")}}
	planner, err := NewModelShortHorizonPlanner(model)
	if err != nil {
		t.Fatalf("NewModelShortHorizonPlanner() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.Model = "planner-model"
	request.Budget.MaxTotalTokens = 10_000
	result, err := planner.Plan(context.Background(), request)
	if !HasErrorCode(err, ErrorModel) || result.Attempts != 1 || len(model.requests) != 1 {
		t.Fatalf("result/error/requests = %+v/%v/%d", result, err, len(model.requests))
	}
}

func TestModelShortHorizonPlannerFailsBeforeCallWhenBudgetCannotReserveOutput(t *testing.T) {
	model := &scriptedShortPlanModel{}
	planner, err := NewModelShortHorizonPlanner(
		model,
		WithShortPlanTokenCounter(fixedShortPlanTokenCounter{requestTokens: 2}),
		WithShortPlanMaxOutputTokens(16),
	)
	if err != nil {
		t.Fatalf("NewModelShortHorizonPlanner() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.Model = "planner-model"
	request.Budget.MaxTotalTokens = 10
	result, err := planner.Plan(context.Background(), request)
	if !HasErrorCode(err, ErrorBudgetExceeded) || result.Attempts != 0 || len(model.requests) != 0 {
		t.Fatalf("result/error/requests = %+v/%v/%d", result, err, len(model.requests))
	}
}

func TestModelShortHorizonPlannerRequiresEstimatorForCostBudget(t *testing.T) {
	model := &scriptedShortPlanModel{}
	planner, err := NewModelShortHorizonPlanner(
		model,
		WithShortPlanTokenCounter(fixedShortPlanTokenCounter{requestTokens: 2}),
		WithShortPlanMaxOutputTokens(16),
	)
	if err != nil {
		t.Fatalf("NewModelShortHorizonPlanner() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.Model = "planner-model"
	request.Budget.MaxTotalTokens = 100
	request.Budget.MaxEstimatedCostMicros = 100
	_, err = planner.Plan(context.Background(), request)
	if !HasErrorCode(err, ErrorInvalidRequest) || len(model.requests) != 0 {
		t.Fatalf("Plan() error/requests = %v/%d", err, len(model.requests))
	}
}

func TestModelShortHorizonPlannerReservesSharedBudgetBeforeCall(t *testing.T) {
	model := &scriptedShortPlanModel{}
	planner, err := NewModelShortHorizonPlanner(
		model,
		WithShortPlanTokenCounter(fixedShortPlanTokenCounter{requestTokens: 10}),
		WithShortPlanMaxOutputTokens(20),
	)
	if err != nil {
		t.Fatalf("NewModelShortHorizonPlanner() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.Model = "planner-model"
	request.Budget.MaxTotalTokens = 100
	tracker, err := NewBudgetTracker(Budget{MaxTotalTokens: 29})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}

	result, err := planner.Plan(ContextWithBudgetTracker(context.Background(), tracker), request)
	if !HasErrorCode(err, ErrorBudgetExceeded) || result.Attempts != 0 || len(model.requests) != 0 {
		t.Fatalf("result/error/requests = %+v/%v/%d", result, err, len(model.requests))
	}
	if snapshot := tracker.Snapshot(); snapshot.Usage.TotalTokens != 0 || snapshot.Reserved.TotalTokens != 0 {
		t.Fatalf("shared budget snapshot = %+v", snapshot)
	}
}

func TestModelShortHorizonPlannerCommitsActualUsageAndCostToSharedBudget(t *testing.T) {
	model := &scriptedShortPlanModel{responses: []ModelResponse{{
		Message: modelResponseMessage(`{"version":"agent.short_plan.v1","steps":[{"id":"answer","kind":"respond","objective":"answer","criterion_ids":["answer-written"]}]}`),
		Usage:   TokenUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		Model:   "priced-planner",
	}}}
	planner, err := NewModelShortHorizonPlanner(
		model,
		WithShortPlanTokenCounter(fixedShortPlanTokenCounter{requestTokens: 10}),
		WithShortPlanCostEstimator(fixedCostEstimator{microsPerToken: 3, version: "pricing-v1"}),
		WithShortPlanMaxOutputTokens(20),
	)
	if err != nil {
		t.Fatalf("NewModelShortHorizonPlanner() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.Model = "priced-planner"
	request.Budget.MaxTotalTokens = 100
	request.Budget.MaxEstimatedCostMicros = 100
	tracker, err := NewBudgetTracker(Budget{MaxTotalTokens: 100, MaxEstimatedCostMicros: 100})
	if err != nil {
		t.Fatalf("NewBudgetTracker() error = %v", err)
	}

	result, err := planner.Plan(ContextWithBudgetTracker(context.Background(), tracker), request)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if result.Usage.TotalTokens != 15 || result.Usage.EstimatedCostMicros != 45 ||
		result.Usage.PricingVersion != "pricing-v1" || result.Usage.CostEstimated {
		t.Fatalf("Plan() usage = %+v", result.Usage)
	}
	snapshot := tracker.Snapshot()
	if snapshot.Usage.TotalTokens != 15 || snapshot.Usage.EstimatedCostMicros != 45 ||
		snapshot.Reserved.TotalTokens != 0 || snapshot.Reserved.EstimatedCostMicros != 0 {
		t.Fatalf("shared budget snapshot = %+v", snapshot)
	}
}

type scriptedShortPlanModel struct {
	responses []ModelResponse
	errors    []error
	requests  []ModelRequest
}

func (model *scriptedShortPlanModel) Complete(_ context.Context, request ModelRequest) (ModelResponse, error) {
	model.requests = append(model.requests, request)
	index := len(model.requests) - 1
	if index < len(model.errors) && model.errors[index] != nil {
		return ModelResponse{}, model.errors[index]
	}
	if index >= len(model.responses) {
		return ModelResponse{}, errors.New("unexpected model call")
	}
	return model.responses[index], nil
}

func modelResponseMessage(content string) Message {
	return Message{Role: RoleAssistant, Content: content}
}

type fixedShortPlanTokenCounter struct {
	requestTokens int
}

func (fixedShortPlanTokenCounter) CountText(string) int { return 0 }

func (fixedShortPlanTokenCounter) CountMessages([]Message) int { return 0 }

func (counter fixedShortPlanTokenCounter) EstimateRequest(ModelRequest) TokenUsage {
	return TokenUsage{InputTokens: counter.requestTokens, TotalTokens: counter.requestTokens, Estimated: true}
}

func (fixedShortPlanTokenCounter) EstimateResponse(ModelResponse) TokenUsage {
	return TokenUsage{OutputTokens: 1, TotalTokens: 1, Estimated: true}
}
