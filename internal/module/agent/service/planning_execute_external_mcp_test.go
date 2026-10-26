package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentEvidence "twitter-clone/internal/module/agent/evidence"
	externalmcp "twitter-clone/internal/module/agent/mcp/remote"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"
)

type externalMCPTestCostEstimator struct{}

func (externalMCPTestCostEstimator) EstimateCost(
	string,
	agentRuntime.TokenUsage,
) (agentRuntime.CostEstimate, error) {
	return agentRuntime.CostEstimate{Micros: 1, PricingVersion: "test-v1"}, nil
}

func TestPlanningExecuteExternalMCPReadFlagIsIndependent(t *testing.T) {
	service := &AgentService{
		planningExecute: PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100,
		},
		planningCoordinator: new(agentRuntime.PlanningCoordinator),
	}
	if service.planningExecuteEnabled(41, ExecutionProfileRuntimeExternalMCP) {
		t.Fatal("external MCP read entered planning without its dedicated flag")
	}
	service.planningExecute.ExternalMCPReadEnabled = true
	if !service.planningExecuteEnabled(41, ExecutionProfileRuntimeExternalMCP) {
		t.Fatal("external MCP read did not enter the fully enabled canary")
	}
	if service.planningExecuteEnabled(41, ExecutionProfileRuntimeWorkflow) {
		t.Fatal("workflow entered the external MCP read canary")
	}
}

func TestPlanningExecuteExternalMCPWriteRequiresApprovalRecovery(t *testing.T) {
	service := &AgentService{
		planningExecute: PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, ExternalMCPWriteEnabled: true,
		},
		planningCoordinator: new(agentRuntime.PlanningCoordinator),
	}
	if service.planningExecuteExternalMCPWriteEnabled(41) {
		t.Fatal("external MCP write planning bypassed approval recovery")
	}
	service.unifiedAgentApprovalRecovery = true
	if !service.planningExecuteExternalMCPWriteEnabled(41) {
		t.Fatal("external MCP write did not enter the fully governed canary")
	}
}

func TestRunAgentPlannedExternalMCPExposesOnlyReadToolsAndVerifiesEvidence(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	store := mixedExternalMCPPlanningStore(qualifiedName)
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(&externalMCPRuntimeCaller{}),
	)
	planner, coordinator := newExternalMCPReadPlanningCoordinator(t, qualifiedName)
	runner := &capturingRuntimeRunner{result: externalMCPPlanningRun(qualifiedName, true)}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithExternalMCPManager(manager),
		WithExternalMCPEnabled(true),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, ExternalMCPReadEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 41, Content: "read the approved documentation connector",
		PreferredCapabilityIDs: []string{CapabilityExternalMCP},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Response != "verified external MCP answer" ||
		len(planner.requests) != 1 || runner.calls != 1 {
		t.Fatalf("result/planner/runtime = %+v/%d/%d", result, len(planner.requests), runner.calls)
	}
	if runner.request.InitialToolChoice != agentRuntime.ToolChoiceRequired ||
		runner.request.Context.StrategyPlanDigest == "" ||
		len(runner.request.Tools) != 1 ||
		runner.request.Tools[0].Name != qualifiedName ||
		runner.request.Tools[0].ApprovalRequired() {
		t.Fatalf("planned external MCP runtime request = %+v", runner.request)
	}
}

func TestRunAgentPlannedExternalMCPBlocksMissingObservation(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	store := mixedExternalMCPPlanningStore(qualifiedName)
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(&externalMCPRuntimeCaller{}),
	)
	_, coordinator := newExternalMCPReadPlanningCoordinator(t, qualifiedName)
	runner := &capturingRuntimeRunner{result: externalMCPPlanningRun(qualifiedName, false)}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithExternalMCPManager(manager),
		WithExternalMCPEnabled(true),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, ExternalMCPReadEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 41, Content: "read the approved documentation connector",
		PreferredCapabilityIDs: []string{CapabilityExternalMCP},
	})
	if err == nil || !strings.Contains(
		err.Error(), "admitted tool step did not produce a successful observation",
	) {
		t.Fatalf("RunAgent() missing observation error = %v", err)
	}
}

func TestRunAgentPlannedExternalMCPGovernedExposesApprovalBoundWrite(t *testing.T) {
	const qualifiedName = "mcp_server.create_record"
	store := mixedExternalMCPPlanningStore("mcp_server.lookup")
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(&externalMCPRuntimeCaller{}),
	)
	planner, coordinator := newExternalMCPWritePlanningCoordinator(t, qualifiedName)
	runner := &capturingRuntimeRunner{result: externalMCPPlanningRun(qualifiedName, true)}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithExternalMCPManager(manager),
		WithExternalMCPEnabled(true),
		WithUnifiedAgentApprovalRecovery(true),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, ExternalMCPWriteEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 41, Content: "create one approved connector record",
		PreferredCapabilityIDs: []string{CapabilityExternalMCP},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if result.Response != "verified external MCP answer" || len(planner.requests) != 1 ||
		runner.calls != 1 || runner.request.Context.AgentProfileID != profileUnifiedExternalMCPGoverned {
		t.Fatalf("result/planner/runtime = %+v/%d/%+v", result, len(planner.requests), runner.request)
	}
	var writeTool *agentRuntime.ToolDefinition
	for index := range runner.request.Tools {
		if runner.request.Tools[index].Name == qualifiedName {
			writeTool = &runner.request.Tools[index]
			break
		}
	}
	if writeTool == nil || writeTool.Category != agentRuntime.ToolCategoryWrite ||
		!writeTool.ApprovalRequired() || runner.request.Context.StrategyPlanDigest == "" {
		t.Fatalf("planned governed tools/request = %+v/%+v", runner.request.Tools, runner.request.Context)
	}
}

func TestPlannedExternalMCPWritePersistsVerifiedApprovalAndResumesOnce(t *testing.T) {
	const (
		userID        = uint64(41)
		qualifiedName = "mcp_server.create_record"
	)
	store := mixedExternalMCPPlanningStore("mcp_server.lookup")
	caller := &externalMCPRuntimeCaller{}
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true), externalmcp.WithCaller(caller),
	)
	approvalRepo := newApprovalWorkflowRepositoryFake(nil)
	dialogueRepo := &assistRuntimeRepository{}
	repo := &agentApprovalRepositoryFake{
		AgentRepository: dialogueRepo, ToolApprovalRepository: approvalRepo,
	}
	runStore := &memoryAgentExecutionRunStore{}
	model := &controlledModel{responses: []agentRuntime.ModelResponse{
		{Actions: []agentRuntime.Action{{
			ID: "write-1", Type: agentRuntime.ActionToolCall, Name: qualifiedName,
			Arguments: json.RawMessage(`{"value":"created"}`),
		}}},
		{Message: agentRuntime.Message{Content: "The approved record was created."}},
	}}
	_, coordinator := newExternalMCPWritePlanningCoordinator(t, qualifiedName)
	governedExecutor := workflowTool.NewExecutor(
		workflowTool.NewRegistry(),
		workflowTool.WithApprovalGate(NewPersistentApprovalGate(approvalRepo, time.Hour)),
		workflowTool.WithIdempotencyStore(NewPersistentToolResultStore(approvalRepo)),
	)
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		repo, nil, nil,
		WithAgentRunner(&plannedSuspendingRuntimeRunner{}),
		WithWorkflowToolExecutor(governedExecutor),
		WithExternalMCPManager(manager), WithExternalMCPEnabled(true),
		WithAgentExecutionRunStore(runStore), WithRecoverableAgentRuns(true),
		WithUnifiedAgentApprovalRecovery(true),
		WithAgentRunRecovery(testAgentCheckpointCipher(t), 64*1024, time.Minute),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, ExternalMCPWriteEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()
	service.runtimeRunner = agentRuntime.NewReActRunner(
		model, &mcpRuntimeToolExecutor{service: service}, nil,
		agentRuntime.WithTokenCounter(service.runtimeTokens),
		agentRuntime.WithCostEstimator(externalMCPTestCostEstimator{}),
	)

	suspended, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: userID, Content: "create one approved connector record",
		PreferredCapabilityIDs: []string{CapabilityExternalMCP},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if suspended.RunStatus != string(agentRuntime.RunStatusApprovalRequired) ||
		!suspended.ApprovalState.ResumeSupported ||
		runStore.run == nil || runStore.run.CheckpointVersion != agentRuntime.VerifiedCheckpointVersion ||
		runStore.run.VerifiedCheckpointVersion != agentRuntime.VerifiedCheckpointVersion ||
		caller.calls != 0 {
		t.Fatalf("suspended result/run/calls = %+v/%+v/%d", suspended, runStore.run, caller.calls)
	}
	approvals, total, err := service.ListToolApprovals(
		context.Background(), userID, repository.ToolApprovalStatusPending, 1, 10,
	)
	if err != nil || total != 1 || len(approvals) != 1 {
		t.Fatalf("ListToolApprovals() approvals/total/error = %+v/%d/%v", approvals, total, err)
	}
	approved, err := service.DecideToolApproval(
		context.Background(), userID, approvals[0].ID,
		repository.ToolApprovalStatusApproved, "", approvals[0].Revision,
	)
	if err != nil {
		t.Fatalf("DecideToolApproval() error = %v", err)
	}
	grant, err := service.IssueAgentResumeGrant(
		context.Background(), userID, approved.ID, runStore.run.Revision,
	)
	if err != nil {
		t.Fatalf("IssueAgentResumeGrant() error = %v", err)
	}
	resumed, err := service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: userID, RunID: suspended.RunID, ExpectedRevision: grant.Run.Revision,
		ApprovalID: approved.ID, ResumeToken: grant.ResumeToken,
	})
	if err != nil {
		t.Fatalf("ResumeAgentExecutionRun() error = %v", err)
	}
	if resumed.RunStatus != UnifiedAgentRunStatusCompleted ||
		resumed.Response != "The approved record was created." || caller.calls != 1 ||
		runStore.run.Status != repository.AgentExecutionRunCompleted ||
		runStore.run.CheckpointCiphertext != "" {
		t.Fatalf("resumed result/run/calls = %+v/%+v/%d", resumed, runStore.run, caller.calls)
	}
	if _, err := service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: userID, RunID: suspended.RunID, ExpectedRevision: grant.Run.Revision,
		ApprovalID: approved.ID, ResumeToken: grant.ResumeToken,
	}); err == nil || caller.calls != 1 {
		t.Fatalf("replayed resume error/calls = %v/%d", err, caller.calls)
	}
}

func TestRunAgentExternalMCPPlanningFlagOffPreservesLegacyPath(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	store := mixedExternalMCPPlanningStore(qualifiedName)
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(&externalMCPRuntimeCaller{}),
	)
	planner, coordinator := newExternalMCPReadPlanningCoordinator(t, qualifiedName)
	runner := &capturingRuntimeRunner{result: externalMCPPlanningRun(qualifiedName, true)}
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithExternalMCPManager(manager),
		WithExternalMCPEnabled(true),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, ExternalMCPReadEnabled: false,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 41, Content: "read the approved documentation connector",
		PreferredCapabilityIDs: []string{CapabilityExternalMCP},
	})
	if err != nil {
		t.Fatalf("RunAgent() legacy fallback error = %v", err)
	}
	if result.Response != "verified external MCP answer" || len(planner.requests) != 0 ||
		runner.calls != 1 || runner.request.Context.StrategyPlanDigest != "" {
		t.Fatalf("legacy result/planner/runtime = %+v/%d/%+v", result, len(planner.requests), runner.request)
	}
}

func TestPlannedExternalMCPResumeRejectsRevokedReadToolBeforeRuntime(t *testing.T) {
	const qualifiedName = "mcp_server.lookup"
	store := mixedExternalMCPPlanningStore(qualifiedName)
	manager := externalmcp.NewManager(
		store, nil, nil, nil,
		externalmcp.WithEnabled(true),
		externalmcp.WithCaller(&externalMCPRuntimeCaller{}),
	)
	planner := &serviceShortPlanPlanner{result: agentRuntime.ShortPlanResult{
		Proposal: agentRuntime.ShortPlanProposal{
			Version: agentRuntime.ShortPlanVersionV1,
			Steps: []agentRuntime.ShortPlanStep{{
				ID: "clarify", Kind: agentRuntime.ShortPlanStepAskHuman,
				Objective:    "clarify the connector lookup",
				CriterionIDs: []string{agentEvidence.ExternalMCPReadObservedCriterion},
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
	service := NewAgentService(
		"http://127.0.0.1:1/v1", "test", "default-model", "127.0.0.1:1",
		&assistRuntimeRepository{}, nil, nil,
		WithAgentRunner(runner),
		WithExternalMCPManager(manager),
		WithExternalMCPEnabled(true),
		WithAgentExecutionRunStore(runStore),
		WithRecoverableAgentRuns(true),
		WithAgentRunRecovery(testAgentCheckpointCipher(t), 64*1024, time.Minute),
		WithPlanningExecute(PlanningExecuteConfig{
			Enabled: true, CanaryPercent: 100, ExternalMCPReadEnabled: true,
			MaxOutputTokens: DefaultPlanningMaxOutputTokens,
			ParseRepairs:    1, AdmissionRepairs: 1,
		}, coordinator),
	)
	defer service.Close()

	suspended, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 41, Content: "read the approved documentation connector",
		PreferredCapabilityIDs: []string{CapabilityExternalMCP},
	})
	if err != nil {
		t.Fatalf("RunAgent() error = %v", err)
	}
	if suspended.RunStatus != UnifiedAgentRunStatusAwaitingHuman {
		t.Fatalf("suspended result = %+v", suspended)
	}

	store.connection.ToolPolicies[0].Enabled = false
	_, err = service.ResumeAgentExecutionRun(context.Background(), ResumeAgentExecutionRequest{
		UserID: 41, RunID: suspended.RunID,
		ExpectedRevision: suspended.ApprovalState.Revision,
		HumanResponse:    "Golang",
	})
	if err == nil {
		t.Fatal("ResumeAgentExecutionRun() accepted a revoked external MCP read tool")
	}
	if runner.resumeCalls != 0 {
		t.Fatalf("runtime resume calls after revocation = %d", runner.resumeCalls)
	}
}

func newExternalMCPReadPlanningCoordinator(
	t *testing.T,
	qualifiedName string,
) (*serviceShortPlanPlanner, *agentRuntime.PlanningCoordinator) {
	t.Helper()
	planner := &serviceShortPlanPlanner{result: agentRuntime.ShortPlanResult{
		Proposal: agentRuntime.ShortPlanProposal{
			Version: agentRuntime.ShortPlanVersionV1,
			Steps: []agentRuntime.ShortPlanStep{
				{
					ID: "read", Kind: agentRuntime.ShortPlanStepTool,
					Objective:    "read the authorized external connector",
					ToolName:     qualifiedName,
					CriterionIDs: []string{agentEvidence.ExternalMCPReadObservedCriterion},
				},
				{
					ID: "respond", Kind: agentRuntime.ShortPlanStepRespond,
					Objective:    "answer only from the observed connector result",
					CriterionIDs: []string{agentEvidence.ExternalMCPReadObservedCriterion},
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

func newExternalMCPWritePlanningCoordinator(
	t *testing.T,
	qualifiedName string,
) (*serviceShortPlanPlanner, *agentRuntime.PlanningCoordinator) {
	t.Helper()
	planner := &serviceShortPlanPlanner{result: agentRuntime.ShortPlanResult{
		Proposal: agentRuntime.ShortPlanProposal{
			Version: agentRuntime.ShortPlanVersionV1,
			Steps: []agentRuntime.ShortPlanStep{
				{
					ID: "write", Kind: agentRuntime.ShortPlanStepTool,
					Objective:    "create the authorized external connector record",
					ToolName:     qualifiedName,
					CriterionIDs: []string{agentEvidence.ExternalMCPGovernedObservedCriterion},
				},
				{
					ID: "respond", Kind: agentRuntime.ShortPlanStepRespond,
					Objective:    "report only the governed connector outcome",
					CriterionIDs: []string{agentEvidence.ExternalMCPGovernedObservedCriterion},
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

func externalMCPPlanningRun(qualifiedName string, observed bool) agentRuntime.RunResult {
	action := agentRuntime.Action{
		ID: "external-read-1", Type: agentRuntime.ActionToolCall, Name: qualifiedName,
	}
	step := agentRuntime.Step{Index: 1, Actions: []agentRuntime.Action{action}}
	if observed {
		step.Observations = []agentRuntime.Observation{{
			ActionID: action.ID, Name: qualifiedName, Content: "verified connector result",
		}}
	}
	return agentRuntime.RunResult{
		Status: agentRuntime.RunStatusCompleted, FinalAnswer: "verified external MCP answer",
		Steps: []agentRuntime.Step{step},
		Usage: agentRuntime.TokenUsage{
			InputTokens: 4, OutputTokens: 3, TotalTokens: 7,
			EstimatedCostMicros: 11, CostEstimated: true,
		},
	}
}

func mixedExternalMCPPlanningStore(readQualifiedName string) *externalMCPRuntimeStore {
	store := controlledExternalMCPStore(readQualifiedName, externalmcp.ToolCategoryRead)
	store.snapshot.Tools[0].DeclaredReadOnly = true
	const writeQualifiedName = "mcp_server.create_record"
	store.connection.ToolPolicies = append(
		store.connection.ToolPolicies,
		externalmcp.ToolPolicy{
			SnapshotID: store.snapshot.ID, ToolName: "create_record",
			QualifiedName: writeQualifiedName, Category: externalmcp.ToolCategoryWrite,
			Enabled: true,
		},
	)
	store.snapshot.Tools = append(store.snapshot.Tools, externalmcp.ToolSchema{
		Name: "create_record", QualifiedName: writeQualifiedName,
		InputSchemaJSON:  `{"type":"object","additionalProperties":false,"properties":{"value":{"type":"string"},"request_id":{"type":"string"}},"required":["value","request_id"]}`,
		DeclaredReadOnly: false, DeclaredIdempotent: true,
		IdempotencyKeyArgument: "request_id",
	})
	return store
}
