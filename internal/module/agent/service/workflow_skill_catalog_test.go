package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/skill"
	"twitter-clone/internal/module/agent/workflow/dsl"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestWorkflowSkillCatalogProjectsAndResolvesExactVersion(t *testing.T) {
	t.Parallel()

	service, repo, _ := newWorkflowSkillTestService(t)
	publication, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		PublishWorkflowToolInput{Description: "Build a read-only digest."},
	)
	require.NoError(t, err)

	versions, err := service.ListAgentSkills(context.Background(), 42, 10)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	projected := versions[0]
	require.Equal(t, publication.ToolName, projected.ID)
	require.Equal(t, []string{publication.ToolName}, projected.AllowedTools)
	require.Equal(t, profileUnifiedWorkflow, projected.Profile.ID)
	require.Equal(t, publication.WorkflowRevisionID.Hex(), projected.Workflow.WorkflowRevisionID)
	require.NotEmpty(t, projected.Version)

	resolved, err := service.GetAgentSkill(
		context.Background(),
		42,
		projected.ID,
		projected.Version,
	)
	require.NoError(t, err)
	require.Equal(t, projected, resolved)

	_, err = service.GetAgentSkill(
		context.Background(),
		99,
		projected.ID,
		projected.Version,
	)
	require.ErrorIs(t, err, skill.ErrSkillNotFound)
}

func TestWorkflowSkillCatalogInvalidatesPriorVersionOnPublicationUpdate(t *testing.T) {
	t.Parallel()

	service, repo, _ := newWorkflowSkillTestService(t)
	publication, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		PublishWorkflowToolInput{Description: "First contract."},
	)
	require.NoError(t, err)
	first, err := service.GetAgentSkill(
		context.Background(),
		42,
		publication.ToolName,
		mustWorkflowSkillVersion(t, service, 42),
	)
	require.NoError(t, err)

	updated, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		PublishWorkflowToolInput{
			Description:      "Second contract.",
			ExpectedRevision: publication.Revision,
		},
	)
	require.NoError(t, err)
	secondVersion := mustWorkflowSkillVersion(t, service, 42)
	require.NotEqual(t, first.Version, secondVersion)
	boundContext := withWorkflowSkillExecution(context.Background(), 42, first)
	require.ErrorIs(
		t,
		validateWorkflowSkillExecutionBinding(boundContext, 42, updated),
		skill.ErrVersionNotFound,
	)

	_, err = service.GetAgentSkill(
		context.Background(),
		42,
		updated.ToolName,
		first.Version,
	)
	require.ErrorIs(t, err, skill.ErrVersionNotFound)
}

func TestWorkflowSkillCatalogFailsClosedWhenDisabled(t *testing.T) {
	t.Parallel()

	service, _, _ := newWorkflowSkillTestService(t)
	service.skillCatalogEnabled = false

	_, err := service.ListAgentSkills(context.Background(), 42, 10)
	require.ErrorIs(t, err, skill.ErrCatalogDisabled)
}

func TestRunAgentExecutesOnlyExplicitExactSkillVersion(t *testing.T) {
	t.Parallel()

	definition := dsl.WorkflowDSL{
		Name: "Skill digest",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
	}
	rawDSL, err := json.Marshal(definition)
	require.NoError(t, err)
	workflow := &repository.WorkflowDefinition{
		ID: primitive.NewObjectID(), UserID: 42, Name: definition.Name,
		DSLJSON: string(rawDSL),
	}
	workflowRepo := newApprovalWorkflowRepositoryFake(workflow)
	workflowRepo.currentRevision.DSLHash = workflowAsToolDSLHash(string(rawDSL))
	repo := &workflowAsToolAgentRepository{
		approvalWorkflowRepositoryFake: workflowRepo,
		dialogues:                      &assistRuntimeRepository{},
	}
	runner := &capturingRuntimeRunner{}
	catalog, err := NewBuiltInAgentCapabilityCatalog(
		WithAvailableWorkflowCapability(),
		WithAvailableSkillCapability(),
	)
	require.NoError(t, err)
	store := newMemoryWorkflowToolPublicationStore()
	service := NewAgentService(
		"http://127.0.0.1:1/v1",
		"test",
		"default-model",
		"127.0.0.1:1",
		repo,
		nil,
		nil,
		WithAgentRunner(runner),
		WithAgentCapabilityCatalog(catalog),
		WithWorkflowToolPublications(store, true, 20, time.Second),
		WithWorkflowSkillCatalog(true, 20),
	)
	defer service.Close()
	publication, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		workflow.ID.Hex(),
		PublishWorkflowToolInput{Description: "Build the selected digest."},
	)
	require.NoError(t, err)
	versions, err := service.ListAgentSkills(context.Background(), 42, 20)
	require.NoError(t, err)
	require.Len(t, versions, 1)

	workflowRunID := primitive.NewObjectID().Hex()
	runner.result = agentRuntime.RunResult{
		Status:      agentRuntime.RunStatusCompleted,
		FinalAnswer: "The selected Skill completed.",
		Steps: []agentRuntime.Step{{
			Index: 1,
			Actions: []agentRuntime.Action{{
				ID: "skill-action", Type: agentRuntime.ActionToolCall,
				Name: publication.ToolName,
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "skill-action", Name: publication.ToolName, Content: "completed",
				StructuredContent: json.RawMessage(`{
					"schema":"workflow.run.v1",
					"workflow_id":"` + workflow.ID.Hex() + `",
					"workflow_revision_id":"` + publication.WorkflowRevisionID.Hex() + `",
					"workflow_run_id":"` + workflowRunID + `",
					"status":"success",
					"response":"The selected Skill completed."
				}`),
			}},
		}},
	}
	result, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "create the digest",
		SkillID: versions[0].ID, SkillVersion: versions[0].Version,
	})
	require.NoError(t, err)
	require.Equal(t, ExecutionProfileRuntimeSkill, result.ExecutionProfile)
	require.Equal(t, []string{CapabilitySkillRun}, result.CapabilityIDs)
	require.Equal(t, versions[0].ID, result.SelectedSkillID)
	require.Equal(t, versions[0].Version, result.SelectedSkillVersion)
	require.Len(t, runner.request.Tools, 1)
	require.Equal(t, publication.ToolName, runner.request.Tools[0].Name)
	require.Equal(t, agentRuntime.ToolChoiceRequired, runner.request.InitialToolChoice)
	require.Equal(t, versions[0].Budget, runner.request.Context.Budget)
	require.Len(t, repo.dialogues.saved, 2)
}

func TestRunAgentRejectsPartialSkillSelectionBeforeRuntime(t *testing.T) {
	t.Parallel()

	service, _, _ := newWorkflowSkillTestService(t)
	service.capabilityPlanner = NewExplicitCapabilityPlanner(nil)

	runner := &capturingRuntimeRunner{}
	service.runtimeRunner = runner

	_, err := service.RunAgent(context.Background(), UnifiedAgentRequest{
		UserID: 42, Content: "run it", SkillID: "workflow_aaaaaaaaaaaaaaaaaaaaaaaa",
	})
	require.ErrorIs(t, err, ErrInvalidUnifiedAgentRequest)
	require.Zero(t, runner.calls)
}

func newWorkflowSkillTestService(
	t *testing.T,
) (*AgentService, *approvalWorkflowRepositoryFake, *memoryWorkflowToolPublicationStore) {
	t.Helper()
	definition := dsl.WorkflowDSL{
		Name: "Read-only digest",
		Nodes: []dsl.NodeDSL{
			{ID: "start", Type: "start"},
			{ID: "end", Type: "end"},
		},
		Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
	}
	service, repo, store := newWorkflowAsToolTestService(
		t,
		42,
		definition,
		workflowTool.NewRegistry(),
	)
	resolver, err := NewBuiltInProfileResolver(nil)
	require.NoError(t, err)
	service.profileResolver = resolver
	service.skillCatalogEnabled = true
	service.skillCatalogLimit = defaultAgentSkillCatalogLimit
	return service, repo, store
}

func mustWorkflowSkillVersion(t *testing.T, service *AgentService, userID uint64) string {
	t.Helper()
	versions, err := service.ListAgentSkills(context.Background(), userID, 10)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	return versions[0].Version
}
