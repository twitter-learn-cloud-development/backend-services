package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type workflowToolGoalModel struct {
	toolName string
	calls    int
}

func (model *workflowToolGoalModel) Complete(
	context.Context,
	agentRuntime.ModelRequest,
) (agentRuntime.ModelResponse, error) {
	model.calls++
	if model.calls == 1 {
		return agentRuntime.ModelResponse{Actions: []agentRuntime.Action{{
			ID: "workflow-goal-action", Type: agentRuntime.ActionToolCall,
			Name: model.toolName, Arguments: json.RawMessage(`{"user_input":"run exact revision"}`),
		}}}, nil
	}
	return agentRuntime.ModelResponse{
		Message: agentRuntime.Message{Content: "The published workflow output is ready."},
	}, nil
}

func TestWorkflowToolGoalExecutesExactRevisionAndVerifiesChildOutput(t *testing.T) {
	service, repo, _ := newWorkflowAsToolTestService(
		t,
		42,
		dsl.WorkflowDSL{
			Name:  "Verified child output",
			Nodes: []dsl.NodeDSL{{ID: "start", Type: "start"}, {ID: "end", Type: "end"}},
			Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
		},
		workflowTool.NewRegistry(),
	)
	publication, err := service.PublishWorkflowTool(
		context.Background(), 42, repo.workflow.ID.Hex(),
		PublishWorkflowToolInput{Description: "Return the governed child workflow output."},
	)
	if err != nil {
		t.Fatal(err)
	}
	publishedRevisionID := publication.WorkflowRevisionID
	publishedDSLHash := publication.WorkflowDSLHash

	// Advance the mutable draft after publication. Runtime must still execute
	// the immutable revision selected by the publication.
	repo.workflow.DSLJSON = `{"name":"new draft","nodes":[],"edges":[]}`

	environment, err := service.newWorkflowToolEnvironment(42)
	if err != nil {
		t.Fatal(err)
	}
	task := agentRuntime.TaskSpec{
		ID: "e2e-17", Goal: "execute the exact published workflow revision",
		AllowedTools: []string{publication.ToolName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          agentEvidence.WorkflowToolOutputVerifiedCriterion,
			Description: "the authoritative child run and declared output are verified", Required: true,
		}},
	}
	tools, err := environment.Tools(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	resolver := workflowToolRunEvidenceResolver{store: repo}
	model := &workflowToolGoalModel{toolName: publication.ToolName}
	runner := agentRuntime.NewVerifiedRunner(
		agentRuntime.NewReActRunner(model, &mcpRuntimeToolExecutor{service: service}, nil),
		agentEvidence.WorkflowToolGoalVerifier{Resolver: resolver},
		agentEvidence.WorkflowToolGoalCollector{Resolver: resolver},
	)
	result, err := runner.Run(context.Background(), agentRuntime.VerifiedRunRequest{
		Task: task,
		Run: agentRuntime.RunRequest{
			Context: agentRuntime.RunContext{
				RunID: "parent-workflow-goal", UserID: 42,
				Budget: agentRuntime.Budget{MaxSteps: 3, Timeout: 5 * time.Second},
			},
			Model: "controlled-model", Tools: tools,
			Messages:          []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "run workflow"}},
			InitialToolChoice: agentRuntime.ToolChoiceRequired,
		},
		Environment: environment,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != agentRuntime.GoalRunVerified || !result.Verification.Passed() ||
		len(result.Evidence.Items) != 1 {
		t.Fatalf("verified result = %+v", result)
	}
	if model.calls != 2 {
		t.Fatalf("model calls = %d, want 2", model.calls)
	}

	repo.mu.Lock()
	if len(repo.runs) != 1 {
		repo.mu.Unlock()
		t.Fatalf("child runs = %d, want 1", len(repo.runs))
	}
	for _, child := range repo.runs {
		if child.WorkflowRevisionID != publishedRevisionID ||
			child.ParentRunID != "parent-workflow-goal" ||
			child.ParentActionID != "workflow-goal-action" || child.Status != WorkflowRunStatusSuccess {
			repo.mu.Unlock()
			t.Fatalf("child run lineage = %+v", child)
		}
	}
	repo.mu.Unlock()
	if publication.WorkflowDSLHash != publishedDSLHash {
		t.Fatal("published DSL hash drifted after draft edit")
	}
}

func TestWorkflowToolGoalPropagatesChildFailureWithoutCompletionEvidence(t *testing.T) {
	registry := workflowTool.NewRegistry()
	innerCalls := 0
	err := registry.RegisterHandler(
		workflowTool.ToolSpec{
			Name: "FailRead", Description: "return a controlled read failure",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Category:    workflowTool.CategoryRead, Permission: workflowTool.PermissionAuthenticated,
			Timeout: time.Second, Retry: workflowTool.RetryPolicy{MaxAttempts: 1},
			Approval: workflowTool.ApprovalNever,
		},
		workflowTool.HandlerFunc(func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			innerCalls++
			return nil, errors.New("controlled child failure")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	service, repo, _ := newWorkflowAsToolTestService(
		t,
		42,
		dsl.WorkflowDSL{
			Name: "Failing child",
			Nodes: []dsl.NodeDSL{
				{ID: "start", Type: "start"},
				{ID: "fail", Type: "tool", Properties: json.RawMessage(`{"tool_name":"FailRead"}`)},
				{ID: "end", Type: "end"},
			},
			Edges: []dsl.EdgeDSL{
				{ID: "start-fail", Source: "start", Target: "fail"},
				{ID: "fail-end", Source: "fail", Target: "end"},
			},
		},
		registry,
	)
	publication, err := service.PublishWorkflowTool(
		context.Background(), 42, repo.workflow.ID.Hex(), PublishWorkflowToolInput{},
	)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := service.newWorkflowToolEnvironment(42)
	if err != nil {
		t.Fatal(err)
	}
	task := agentRuntime.TaskSpec{
		ID: "e2e-17-failure", Goal: "propagate the child workflow failure",
		AllowedTools: []string{publication.ToolName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          agentEvidence.WorkflowToolOutputVerifiedCriterion,
			Description: "the authoritative child output is verified", Required: true,
		}},
	}
	tools, err := environment.Tools(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	resolver := workflowToolRunEvidenceResolver{store: repo}
	runner := agentRuntime.NewVerifiedRunner(
		agentRuntime.NewReActRunner(
			&workflowToolGoalModel{toolName: publication.ToolName},
			&mcpRuntimeToolExecutor{service: service}, nil,
		),
		agentEvidence.WorkflowToolGoalVerifier{Resolver: resolver},
		agentEvidence.WorkflowToolGoalCollector{Resolver: resolver},
	)
	_, err = runner.Run(context.Background(), agentRuntime.VerifiedRunRequest{
		Task: task,
		Run: agentRuntime.RunRequest{
			Context: agentRuntime.RunContext{
				RunID: "parent-workflow-failure", UserID: 42,
				Budget: agentRuntime.Budget{MaxSteps: 2, Timeout: 5 * time.Second},
			},
			Model: "controlled-model", Tools: tools,
			Messages:          []agentRuntime.Message{{Role: agentRuntime.RoleUser, Content: "run failing workflow"}},
			InitialToolChoice: agentRuntime.ToolChoiceRequired,
		},
		Environment: environment,
	})
	if err == nil || !strings.Contains(err.Error(), "published workflow did not complete successfully") {
		t.Fatalf("Run() failure = %v", err)
	}
	if innerCalls != 1 {
		t.Fatalf("inner tool calls = %d, want 1", innerCalls)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.runs) != 1 {
		t.Fatalf("child runs = %d, want 1", len(repo.runs))
	}
	for _, child := range repo.runs {
		if child.Status != WorkflowRunStatusFailed ||
			child.ParentRunID != "parent-workflow-failure" ||
			child.ParentActionID != "workflow-goal-action" ||
			!strings.Contains(child.ErrorMessage, "controlled child failure") {
			t.Fatalf("failed child run = %+v", child)
		}
	}
}
