package runtime

import (
	"context"
	"testing"
)

func TestDeterministicShortPlanPolicyAdmitsCurrentToolsAndCanonicalizesEvidence(t *testing.T) {
	request := shortPlanTestRequest()
	proposal := ShortPlanProposal{
		Version: ShortPlanVersionV1,
		Steps: []ShortPlanStep{
			{
				ID: " search ", Kind: ShortPlanStepTool, Objective: " find grounded sources ",
				ToolName: "web_search", CriterionIDs: []string{"source-found"},
			},
			{
				ID: "answer", Kind: ShortPlanStepRespond, Objective: "answer from the observed sources",
				CriterionIDs: []string{"answer-written"},
			},
		},
	}

	admitted, err := (DeterministicShortPlanPolicy{}).Admit(context.Background(), request, proposal)
	if err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if admitted.Digest == "" || len(admitted.Steps) != 2 {
		t.Fatalf("admitted plan = %+v", admitted)
	}
	if admitted.Steps[0].ID != "search" || admitted.Steps[0].ToolCategory != ToolCategoryRead ||
		admitted.Steps[0].ApprovalRequired {
		t.Fatalf("tool step = %+v", admitted.Steps[0])
	}

	proposal.Steps[0].CriterionIDs[0] = "mutated"
	admitted.Steps[0].CriterionIDs[0] = "mutated"
	readmitted, err := (DeterministicShortPlanPolicy{}).Admit(context.Background(), request, ShortPlanProposal{
		Version: ShortPlanVersionV1,
		Steps: []ShortPlanStep{
			{ID: "search", Kind: ShortPlanStepTool, Objective: "find sources", ToolName: "web_search", CriterionIDs: []string{"source-found"}},
			{ID: "answer", Kind: ShortPlanStepRespond, Objective: "answer", CriterionIDs: []string{"answer-written"}},
		},
	})
	if err != nil || readmitted.Steps[0].CriterionIDs[0] != "source-found" {
		t.Fatalf("readmitted plan/error = %+v/%v", readmitted, err)
	}
}

func TestDeterministicShortPlanPolicyInfersWriteApprovalFromCatalog(t *testing.T) {
	request := shortPlanTestRequest()
	request.Task.AllowedTools = []string{"create_tweet"}
	request.AvailableTools = []ToolDefinition{{Name: "create_tweet", Category: ToolCategoryWrite}}
	request.TargetCriterionIDs = []string{"answer-written"}
	proposal := ShortPlanProposal{
		Version: ShortPlanVersionV1,
		Steps: []ShortPlanStep{{
			ID: "publish", Kind: ShortPlanStepTool, Objective: "publish the approved draft",
			ToolName: "create_tweet", CriterionIDs: []string{"answer-written"},
		}},
	}

	admitted, err := (DeterministicShortPlanPolicy{}).Admit(context.Background(), request, proposal)
	if err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if !admitted.Steps[0].ApprovalRequired || admitted.Steps[0].ToolCategory != ToolCategoryWrite {
		t.Fatalf("write admission = %+v", admitted.Steps[0])
	}
}

func TestDeterministicShortPlanPolicyRejectsToolOutsideCurrentAuthorization(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*ShortPlanRequest)
	}{
		{
			name: "not allowed by task",
			configure: func(request *ShortPlanRequest) {
				request.Task.AllowedTools = nil
			},
		},
		{
			name: "not available in environment",
			configure: func(request *ShortPlanRequest) {
				request.AvailableTools = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := shortPlanTestRequest()
			test.configure(&request)
			_, err := (DeterministicShortPlanPolicy{}).Admit(context.Background(), request, ShortPlanProposal{
				Version: ShortPlanVersionV1,
				Steps: []ShortPlanStep{{
					ID: "search", Kind: ShortPlanStepTool, Objective: "search",
					ToolName: "web_search", CriterionIDs: []string{"source-found", "answer-written"},
				}},
			})
			if !HasErrorCode(err, ErrorUnknownTool) {
				t.Fatalf("Admit() error = %v", err)
			}
		})
	}
}

func TestDeterministicShortPlanPolicyRejectsBudgetAndUncoveredCriteria(t *testing.T) {
	request := shortPlanTestRequest()
	request.CompletedSteps = 3
	request.Budget.MaxSteps = 4
	proposal := ShortPlanProposal{
		Version: ShortPlanVersionV1,
		Steps: []ShortPlanStep{
			{ID: "search", Kind: ShortPlanStepTool, Objective: "search", ToolName: "web_search", CriterionIDs: []string{"source-found"}},
			{ID: "answer", Kind: ShortPlanStepRespond, Objective: "answer", CriterionIDs: []string{"answer-written"}},
		},
	}
	if _, err := (DeterministicShortPlanPolicy{}).Admit(context.Background(), request, proposal); !HasErrorCode(err, ErrorBudgetExceeded) {
		t.Fatalf("budget Admit() error = %v", err)
	}

	request.CompletedSteps = 0
	request.Budget.MaxSteps = 4
	proposal.Steps = proposal.Steps[:1]
	if _, err := (DeterministicShortPlanPolicy{}).Admit(context.Background(), request, proposal); !HasErrorCode(err, ErrorInvalidAction) {
		t.Fatalf("criteria Admit() error = %v", err)
	}
}

func TestDeterministicShortPlanPolicyRequiresTerminalStepsLast(t *testing.T) {
	request := shortPlanTestRequest()
	_, err := (DeterministicShortPlanPolicy{}).Admit(context.Background(), request, ShortPlanProposal{
		Version: ShortPlanVersionV1,
		Steps: []ShortPlanStep{
			{ID: "clarify", Kind: ShortPlanStepAskHuman, Objective: "clarify source scope", CriterionIDs: []string{"source-found"}},
			{ID: "answer", Kind: ShortPlanStepRespond, Objective: "answer", CriterionIDs: []string{"answer-written"}},
		},
	})
	if !HasErrorCode(err, ErrorInvalidAction) {
		t.Fatalf("Admit() error = %v", err)
	}
}

func shortPlanTestRequest() ShortPlanRequest {
	return ShortPlanRequest{
		Task: TaskSpec{
			Goal: "return a grounded answer",
			CompletionCriteria: []CompletionCriterion{
				{ID: "source-found", Description: "a source was observed", Required: true},
				{ID: "answer-written", Description: "an answer was produced", Required: true},
			},
			AllowedTools: []string{"web_search"},
		},
		AvailableTools: []ToolDefinition{{Name: "web_search", Category: ToolCategoryRead}},
		Budget:         Budget{MaxSteps: 5},
	}
}
