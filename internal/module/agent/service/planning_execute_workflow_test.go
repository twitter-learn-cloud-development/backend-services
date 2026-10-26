package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type planningWorkflowReActRunner struct {
	service *AgentService
	model   *workflowToolGoalModel
	request agentRuntime.RunRequest
	calls   int
}

type planningWorkflowCostEstimator struct{}

func (planningWorkflowCostEstimator) EstimateCost(
	_ string,
	_ agentRuntime.TokenUsage,
) (agentRuntime.CostEstimate, error) {
	return agentRuntime.CostEstimate{PricingVersion: "workflow-planning-test-v1"}, nil
}

func (runner *planningWorkflowReActRunner) Run(
	ctx context.Context,
	request agentRuntime.RunRequest,
) (agentRuntime.RunResult, error) {
	runner.calls++
	runner.request = request
	return agentRuntime.NewReActRunner(
		runner.model,
		&mcpRuntimeToolExecutor{service: runner.service},
		nil,
		agentRuntime.WithCostEstimator(planningWorkflowCostEstimator{}),
	).Run(ctx, request)
}

func TestPlanningExecuteWorkflowReadFlagIsIndependent(t *testing.T) {
	service := &AgentService{
		planningExecute: PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100,
		},
		planningCoordinator: new(agentRuntime.PlanningCoordinator),
	}
	if service.planningExecuteEnabled(42, ExecutionProfileRuntimeWorkflow) {
		t.Fatal("workflow entered planning without its dedicated flag")
	}
	service.planningExecute.WorkflowReadEnabled = true
	if !service.planningExecuteEnabled(42, ExecutionProfileRuntimeWorkflow) {
		t.Fatal("workflow did not enter the fully enabled canary")
	}
	if service.planningExecuteEnabled(42, ExecutionProfileRuntimeExternalMCP) {
		t.Fatal("external MCP entered the workflow planning canary")
	}
}

func TestRunAgentPlannedWorkflowVerifiesAuthoritativeChildOutput(t *testing.T) {
	workflowID := primitive.NewObjectID()
	toolName := workflowRuntimeToolName(workflowID)
	planner, coordinator := newWorkflowReadPlanningCoordinator(t, toolName)
	runner := &planningWorkflowReActRunner{
		model: &workflowToolGoalModel{toolName: toolName},
	}
	service, repo, _, publication := newPlanningWorkflowAgentService(
		t, workflowID, runner,
		PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, WorkflowReadEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		},
		coordinator,
	)
	runner.service = service
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "run the published knowledge workflow",
		PreferredCapabilityIDs: []string{CapabilityWorkflowRun},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Response != "The published workflow output is ready." ||
		len(planner.requests) != 1 || runner.calls != 1 {
		t.Fatalf("result/planner/runtime = %+v/%d/%d", result, len(planner.requests), runner.calls)
	}
	if runner.request.InitialToolChoice != agentRuntime.ToolChoiceRequired ||
		runner.request.Context.StrategyPlanDigest == "" ||
		len(runner.request.Tools) != 1 ||
		runner.request.Tools[0].Name != publication.ToolName {
		t.Fatalf("planned workflow runtime request = %+v", runner.request)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.runs) != 1 {
		t.Fatalf("authoritative child runs = %d, want 1", len(repo.runs))
	}
	for _, child := range repo.runs {
		if child.ParentRunID != result.RunID ||
			child.ParentActionID != "workflow-goal-action" ||
			child.WorkflowRevisionID != publication.WorkflowRevisionID ||
			child.Status != WorkflowRunStatusSuccess {
			t.Fatalf("authoritative child run = %+v", child)
		}
	}
}

func TestRunAgentPlannedWorkflowBlocksForgedCompletion(t *testing.T) {
	workflowID := primitive.NewObjectID()
	toolName := workflowRuntimeToolName(workflowID)
	_, coordinator := newWorkflowReadPlanningCoordinator(t, toolName)
	runner := &capturingRuntimeRunner{result: workflowPlanningFixture(toolName)}
	service, _, _, _ := newPlanningWorkflowAgentService(
		t, workflowID, runner,
		PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, WorkflowReadEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		},
		coordinator,
	)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "run the published knowledge workflow",
		PreferredCapabilityIDs: []string{CapabilityWorkflowRun},
	})
	if err == nil || !strings.Contains(err.Error(), "planned workflow completion was not verified") {
		t.Fatalf("RunAgent() forged completion error = %v", err)
	}
}

func TestRunAgentWorkflowPlanningFlagOffPreservesLegacyPath(t *testing.T) {
	workflowID := primitive.NewObjectID()
	toolName := workflowRuntimeToolName(workflowID)
	planner, coordinator := newWorkflowReadPlanningCoordinator(t, toolName)
	runner := &capturingRuntimeRunner{result: workflowPlanningFixture(toolName)}
	service, _, _, _ := newPlanningWorkflowAgentService(
		t, workflowID, runner,
		PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, WorkflowReadEnabled: false,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		},
		coordinator,
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "run the published knowledge workflow",
		PreferredCapabilityIDs: []string{CapabilityWorkflowRun},
	})
	if err != nil {
		t.Fatalf("RunAgent() legacy fallback error = %v", err)
	}
	if result.Response != "legacy workflow completion" || len(planner.requests) != 0 ||
		runner.calls != 1 || runner.request.Context.StrategyPlanDigest != "" {
		t.Fatalf("legacy result/planner/runtime = %+v/%d/%+v", result, len(planner.requests), runner.request)
	}
}

func TestPlannedWorkflowResumeRejectsDisabledPublicationBeforeRuntime(t *testing.T) {
	workflowID := primitive.NewObjectID()
	planner := &serviceShortPlanPlanner{result: agentRuntime.ShortPlanResult{
		Proposal: agentRuntime.ShortPlanProposal{
			Version: agentRuntime.ShortPlanVersionV1,
			Steps: []agentRuntime.ShortPlanStep{{
				ID: "clarify", Kind: agentRuntime.ShortPlanStepAskHuman,
				Objective:    "clarify the workflow input",
				CriterionIDs: []string{agentEvidence.WorkflowToolOutputVerifiedCriterion},
			}},
		},
		Usage:    agentRuntime.TokenUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
		Attempts: 1, Model: "default-model", Provider: "test-provider",
	}}
	coordinator, err := agentRuntime.NewPlanningCoordinator(
		planner, agentRuntime.DeterministicShortPlanPolicy{},
	)
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	runner := &plannedSuspendingRuntimeRunner{}
	runStore := &memoryAgentExecutionRunStore{}
	service, _, _, publication := newPlanningWorkflowAgentService(
		t, workflowID, runner,
		PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, WorkflowReadEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		},
		coordinator,
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithAgentRunRecovery(testAgentCheckpointCipher(t), 64*1024, time.Minute),
	)
	defer service.Close()

	suspended, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "run the published knowledge workflow",
		PreferredCapabilityIDs: []string{CapabilityWorkflowRun},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if suspended.RunStatus != UnifiedAgentRunStatusAwaitingHuman {
		t.Fatalf("suspended result = %+v", suspended)
	}
	_, err = service.UnpublishWorkflowTool(
		context.Background(), 42, workflowID.Hex(), publication.Revision,
	)
	if err != nil {
		t.Fatalf("UnpublishWorkflowTool() error = %v", err)
	}
	_, err = service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: 42, RunID: suspended.RunID,
		ExpectedRevision: suspended.ApprovalState.Revision,
		HumanResponse:    "use the existing input",
	})
	if err == nil {
		t.Fatal("ResumeAgentExecutionRun() accepted a disabled workflow publication")
	}
	if runner.resumeCalls != 0 {
		t.Fatalf("runtime resume calls after publication disable = %d", runner.resumeCalls)
	}
}

func newWorkflowReadPlanningCoordinator(
	t *testing.T,
	toolName string,
) (*serviceShortPlanPlanner, *agentRuntime.PlanningCoordinator) {
	t.Helper()
	planner := &serviceShortPlanPlanner{result: agentRuntime.ShortPlanResult{
		Proposal: agentRuntime.ShortPlanProposal{
			Version: agentRuntime.ShortPlanVersionV1,
			Steps: []agentRuntime.ShortPlanStep{
				{
					ID: "run", Kind: agentRuntime.ShortPlanStepTool,
					Objective:    "run the immutable published workflow",
					ToolName:     toolName,
					CriterionIDs: []string{agentEvidence.WorkflowToolOutputVerifiedCriterion},
				},
				{
					ID: "respond", Kind: agentRuntime.ShortPlanStepRespond,
					Objective:    "answer only from the verified child workflow output",
					CriterionIDs: []string{agentEvidence.WorkflowToolOutputVerifiedCriterion},
				},
			},
		},
		Usage: agentRuntime.TokenUsage{
			InputTokens: 4, OutputTokens: 3, TotalTokens: 7,
			EstimatedCostMicros: 9, CostEstimated: true,
		},
		Attempts: 1, Model: "default-model", Provider: "test-provider",
	}}
	coordinator, err := agentRuntime.NewPlanningCoordinator(
		planner, agentRuntime.DeterministicShortPlanPolicy{},
	)
	if err != nil {
		t.Fatalf("NewPlanningCoordinator() error = %v", err)
	}
	return planner, coordinator
}

func newPlanningWorkflowAgentService(
	t *testing.T,
	workflowID primitive.ObjectID,
	runner agentRuntime.AgentRunner,
	planning PlanningExecuteConfig,
	coordinator *agentRuntime.PlanningCoordinator,
	extraOptions ...Option,
) (*AgentService, *approvalWorkflowRepositoryFake, *memoryWorkflowToolPublicationStore, *repository.WorkflowToolPublication) {
	t.Helper()
	definition := dsl.WorkflowDSL{
		Name:  "Planning knowledge workflow",
		Nodes: []dsl.NodeDSL{{ID: "start", Type: "start"}, {ID: "end", Type: "end"}},
		Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
	}
	rawDSL, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	workflow := &repository.WorkflowDefinition{
		ID: workflowID, UserID: 42, Name: definition.Name, DSLJSON: string(rawDSL),
	}
	workflowRepo := newApprovalWorkflowRepositoryFake(workflow)
	workflowRepo.currentRevision.DSLHash = workflowAsToolDSLHash(string(rawDSL))
	repo := &workflowAsToolAgentRepository{
		approvalWorkflowRepositoryFake: workflowRepo,
		dialogues:                      &assistRuntimeRepository{},
	}
	store := newMemoryWorkflowToolPublicationStore()
	catalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWorkflowCapability())
	if err != nil {
		t.Fatal(err)
	}
	options := []Option{
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithWorkflowToolPublications(store, true, 20, time.Second),
		WithPlanningExecute(planning, coordinator),
	}
	options = append(options, extraOptions...)
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil, options...,
	)
	service.workflowToolExecutor = workflowTool.NewExecutor(workflowTool.NewRegistry())
	publication, err := service.PublishWorkflowTool(
		context.Background(), 42, workflowID.Hex(),
		PublishWorkflowToolInput{Description: "Run a read-only governed workflow."},
	)
	if err != nil {
		service.Close()
		t.Fatal(err)
	}
	return service, workflowRepo, store, publication
}

func workflowPlanningFixture(toolName string) agentRuntime.RunResult {
	return agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "legacy workflow completion",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "workflow-action", Type: agentRuntime.ActionToolCall, Name: toolName,
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "workflow-action", Name: toolName, Content: "unverified completion",
				StructuredContent: json.RawMessage(`{"schema":"workflow.run.v1","status":"success"}`),
			}},
		}},
	}
}
