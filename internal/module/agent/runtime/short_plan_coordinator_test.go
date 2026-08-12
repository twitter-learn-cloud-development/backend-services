package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPlanningCoordinatorAdmitsModelPlanAndAggregatesUsage(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: validResearchShortPlanProposal(),
		Usage:    TokenUsage{InputTokens: 4, OutputTokens: 6, TotalTokens: 10},
		Attempts: 1,
		Model:    "planner-model",
		Provider: "fake",
	}}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}

	result, err := coordinator.Coordinate(context.Background(), shortPlanTestRequest())
	if err != nil {
		t.Fatalf("Coordinate() error = %v", err)
	}
	if result.Plan.Digest == "" || len(result.Plan.Steps) != 2 || result.PlanningCalls != 1 ||
		result.ModelAttempts != 1 || result.AdmissionAttempts != 1 || result.AdmissionRepairs != 0 {
		t.Fatalf("planning result = %+v", result)
	}
	if result.Usage.TotalTokens != 10 || result.Model != "planner-model" || result.Provider != "fake" {
		t.Fatalf("planning accounting = %+v", result)
	}
}

func TestPlanningCoordinatorRepairsUnknownToolOnceWithoutEchoingRejectedPlan(t *testing.T) {
	model := &scriptedShortPlanModel{responses: []ModelResponse{
		{
			Message: modelResponseMessage(`{"version":"agent.short_plan.v1","steps":[{"id":"unsafe","kind":"tool","objective":"use an invented tool","tool_name":"invented_secret_tool","criterion_ids":["source-found","answer-written"]}]}`),
			Usage:   TokenUsage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
		},
		{
			Message: modelResponseMessage(`{"version":"agent.short_plan.v1","steps":[{"id":"search","kind":"tool","objective":"find evidence","tool_name":"web_search","criterion_ids":["source-found"]},{"id":"answer","kind":"respond","objective":"answer from evidence","criterion_ids":["answer-written"]}]}`),
			Usage:   TokenUsage{InputTokens: 4, OutputTokens: 6, TotalTokens: 10},
		},
	}}
	planner, err := NewModelShortHorizonPlanner(
		model,
		WithShortPlanTokenCounter(fixedShortPlanTokenCounter{requestTokens: 2}),
		WithShortPlanMaxOutputTokens(32),
		WithShortPlanParseRepairs(0),
	)
	if err != nil {
		t.Fatalf("NewModelShortHorizonPlanner() error = %v", err)
	}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.Model = "planner-model"
	request.Budget.MaxTotalTokens = 100

	result, err := coordinator.Coordinate(context.Background(), request)
	if err != nil {
		t.Fatalf("Coordinate() error = %v", err)
	}
	if result.PlanningCalls != 2 || result.ModelAttempts != 2 || result.AdmissionAttempts != 2 ||
		result.AdmissionRepairs != 1 || result.LastRepairReason != ShortPlanRepairUnknownTool ||
		result.Usage.TotalTokens != 15 {
		t.Fatalf("planning result = %+v", result)
	}
	if len(model.requests) != 2 || len(model.requests[1].Messages) != 3 {
		t.Fatalf("model requests = %+v", model.requests)
	}
	for _, message := range model.requests[1].Messages {
		if strings.Contains(message.Content, "invented_secret_tool") || strings.Contains(message.Content, "use an invented tool") {
			t.Fatalf("repair prompt echoed rejected proposal: %q", message.Content)
		}
	}
	if !strings.Contains(model.requests[1].Messages[2].Content, "available_tools") {
		t.Fatalf("repair instruction = %q", model.requests[1].Messages[2].Content)
	}
	if model.requests[1].Context.Budget.MaxTotalTokens != 95 {
		t.Fatalf("repair budget = %+v", model.requests[1].Context.Budget)
	}
}

func TestPlanningCoordinatorDoesNotRepairPolicyOrBudgetFailures(t *testing.T) {
	tests := []struct {
		name     string
		request  ShortPlanRequest
		result   ShortPlanResult
		wantCode ErrorCode
	}{
		{
			name: "invalid catalog policy",
			request: func() ShortPlanRequest {
				request := shortPlanTestRequest()
				request.AvailableTools[0].Category = "unsafe"
				return request
			}(),
			result:   ShortPlanResult{Proposal: validResearchShortPlanProposal(), Attempts: 1},
			wantCode: ErrorInvalidRequest,
		},
		{
			name: "repair token budget exhausted",
			request: func() ShortPlanRequest {
				request := shortPlanTestRequest()
				request.Budget.MaxTotalTokens = 10
				return request
			}(),
			result: ShortPlanResult{
				Proposal: unknownToolShortPlanProposal(),
				Usage:    TokenUsage{InputTokens: 4, OutputTokens: 6, TotalTokens: 10},
				Attempts: 1,
			},
			wantCode: ErrorBudgetExceeded,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{test.result}}
			coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
			if err != nil {
				t.Fatalf("NewPlanningCoordinator() error = %v", err)
			}
			result, err := coordinator.Coordinate(context.Background(), test.request)
			if !HasErrorCode(err, test.wantCode) || len(planner.requests) != 1 || result.PlanningCalls != 1 {
				t.Fatalf("result/error/calls = %+v/%v/%d", result, err, len(planner.requests))
			}
		})
	}
}

func TestPlanningCoordinatorStopsAfterOneAdmissionRepair(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{
		{Proposal: unknownToolShortPlanProposal(), Usage: TokenUsage{TotalTokens: 3}, Attempts: 1},
		{Proposal: unknownToolShortPlanProposal(), Usage: TokenUsage{TotalTokens: 4}, Attempts: 1},
	}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.Budget.MaxTotalTokens = 100

	result, err := coordinator.Coordinate(context.Background(), request)
	if !HasErrorCode(err, ErrorUnknownTool) || len(planner.requests) != 2 || result.AdmissionRepairs != 1 ||
		result.AdmissionAttempts != 2 || result.Usage.TotalTokens != 7 {
		t.Fatalf("result/error/calls = %+v/%v/%d", result, err, len(planner.requests))
	}
	if planner.requests[1].RepairFeedback == nil ||
		planner.requests[1].RepairFeedback.Reason != ShortPlanRepairUnknownTool {
		t.Fatalf("repair feedback = %+v", planner.requests[1].RepairFeedback)
	}
}

func TestPlanningCoordinatorAdmitsClarificationPlan(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: ShortPlanProposal{
			Version: ShortPlanVersionV1,
			Steps: []ShortPlanStep{{
				ID: "clarify", Kind: ShortPlanStepAskHuman, Objective: "clarify the requested source scope",
				CriterionIDs: []string{"source-found", "answer-written"},
			}},
		},
		Attempts: 1,
	}}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}

	result, err := coordinator.Coordinate(context.Background(), shortPlanTestRequest())
	if err != nil || len(result.Plan.Steps) != 1 || result.Plan.Steps[0].Kind != ShortPlanStepAskHuman {
		t.Fatalf("result/error = %+v/%v", result, err)
	}
}

func TestPlanningCoordinatorRejectsCallerSuppliedRepairFeedback(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	request := shortPlanTestRequest()
	request.RepairFeedback = &ShortPlanRepairFeedback{Reason: ShortPlanRepairUnknownTool}

	result, err := coordinator.Coordinate(context.Background(), request)
	if !HasErrorCode(err, ErrorInvalidRequest) || len(planner.requests) != 0 || result.PlanningCalls != 0 {
		t.Fatalf("result/error/calls = %+v/%v/%d", result, err, len(planner.requests))
	}
}

func TestPlanningCoordinatorNormalizesPlannerUsage(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{results: []ShortPlanResult{{
		Proposal: validResearchShortPlanProposal(),
		Usage:    TokenUsage{InputTokens: 2, OutputTokens: 3},
		Attempts: 1,
	}}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}

	result, err := coordinator.Coordinate(context.Background(), shortPlanTestRequest())
	if err != nil {
		t.Fatalf("Coordinate() error = %v", err)
	}
	if result.Usage.TotalTokens != 5 {
		t.Fatalf("normalized usage = %+v", result.Usage)
	}
}

func TestPlanningCoordinatorDoesNotRepairPlannerFailure(t *testing.T) {
	planner := &scriptedPlanningCoordinatorPlanner{errors: []error{&RunError{Code: ErrorModel, Message: "provider unavailable"}}}
	coordinator, err := NewPlanningCoordinator(planner, DeterministicShortPlanPolicy{})
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}

	result, err := coordinator.Coordinate(context.Background(), shortPlanTestRequest())
	if !HasErrorCode(err, ErrorModel) || len(planner.requests) != 1 || result.AdmissionAttempts != 0 {
		t.Fatalf("result/error/calls = %+v/%v/%d", result, err, len(planner.requests))
	}
}

type scriptedPlanningCoordinatorPlanner struct {
	results  []ShortPlanResult
	errors   []error
	requests []ShortPlanRequest
}

func (planner *scriptedPlanningCoordinatorPlanner) Plan(
	_ context.Context,
	request ShortPlanRequest,
) (ShortPlanResult, error) {
	planner.requests = append(planner.requests, request)
	index := len(planner.requests) - 1
	if index < len(planner.errors) && planner.errors[index] != nil {
		return ShortPlanResult{}, planner.errors[index]
	}
	if index >= len(planner.results) {
		return ShortPlanResult{}, errors.New("unexpected planning call")
	}
	return planner.results[index], nil
}

func validResearchShortPlanProposal() ShortPlanProposal {
	return ShortPlanProposal{
		Version: ShortPlanVersionV1,
		Steps: []ShortPlanStep{
			{ID: "search", Kind: ShortPlanStepTool, Objective: "find evidence", ToolName: "web_search", CriterionIDs: []string{"source-found"}},
			{ID: "answer", Kind: ShortPlanStepRespond, Objective: "answer from evidence", CriterionIDs: []string{"answer-written"}},
		},
	}
}

func unknownToolShortPlanProposal() ShortPlanProposal {
	return ShortPlanProposal{
		Version: ShortPlanVersionV1,
		Steps: []ShortPlanStep{{
			ID: "unsafe", Kind: ShortPlanStepTool, Objective: "use unavailable tool",
			ToolName: "invented_tool", CriterionIDs: []string{"source-found", "answer-written"},
		}},
	}
}
