package service

import (
	"context"
	"strings"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
	"twitter-clone/internal/module/agent/workflow/dsl"
	workflowTool "twitter-clone/internal/module/agent/workflow/tool"

	"github.com/stretchr/testify/require"
)

func TestWorkflowToolEnvironmentUsesActiveImmutableTenantPublication(t *testing.T) {
	service, repo, _ := newWorkflowAsToolTestService(
		t,
		42,
		dsl.WorkflowDSL{
			Name:  "Environment workflow",
			Nodes: []dsl.NodeDSL{{ID: "start", Type: "start"}, {ID: "end", Type: "end"}},
			Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
		},
		workflowTool.NewRegistry(),
	)
	publication, err := service.PublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		PublishWorkflowToolInput{Description: "Run the immutable environment workflow."},
	)
	require.NoError(t, err)

	environment, err := service.newWorkflowToolEnvironment(42)
	require.NoError(t, err)
	task := agentRuntime.TaskSpec{
		ID: "workflow-environment-task", Goal: "Run the published workflow.",
		AllowedTools: []string{publication.ToolName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: "workflow-complete", Description: "The workflow completed.", Required: true,
		}},
	}
	tools, err := environment.Tools(context.Background(), task)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Equal(t, publication.ToolName, tools[0].Name)

	snapshot, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	})
	require.NoError(t, err)
	require.Equal(t, "workflow.tool.v1", snapshot.Environment)
	require.Contains(t, string(snapshot.Metadata), `"binding_digest":"sha256:`)
	require.NotContains(t, string(snapshot.Metadata), publication.WorkflowRevisionID.Hex())
	require.NotContains(t, string(snapshot.Metadata), publication.WorkflowDSLHash)

	_, err = service.UnpublishWorkflowTool(
		context.Background(),
		42,
		repo.workflow.ID.Hex(),
		publication.Revision,
	)
	require.NoError(t, err)
	tools, err = environment.Tools(context.Background(), task)
	require.NoError(t, err)
	require.Empty(t, tools)

	otherTenant, err := service.newWorkflowToolEnvironment(99)
	require.NoError(t, err)
	tools, err = otherTenant.Tools(context.Background(), task)
	require.NoError(t, err)
	require.Empty(t, tools)
}

func TestWorkflowEnvironmentCatalogExcludesDriftedRevisionBinding(t *testing.T) {
	service, repo, store := newWorkflowAsToolTestService(
		t,
		42,
		dsl.WorkflowDSL{
			Name:  "Drifted workflow",
			Nodes: []dsl.NodeDSL{{ID: "start", Type: "start"}, {ID: "end", Type: "end"}},
			Edges: []dsl.EdgeDSL{{ID: "start-end", Source: "start", Target: "end"}},
		},
		workflowTool.NewRegistry(),
	)
	publication, err := service.PublishWorkflowTool(
		context.Background(), 42, repo.workflow.ID.Hex(), PublishWorkflowToolInput{},
	)
	require.NoError(t, err)

	store.mu.Lock()
	stored := store.byWorkflowID[workflowToolPublicationStoreKey(42, repo.workflow.ID)]
	stored.WorkflowDSLHash = strings.Repeat("0", 64)
	store.mu.Unlock()

	environment, err := service.newWorkflowToolEnvironment(42)
	require.NoError(t, err)
	tools, err := environment.Tools(context.Background(), agentRuntime.TaskSpec{
		Goal: "Run the selected workflow.", AllowedTools: []string{publication.ToolName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID: "done", Description: "The workflow completed.", Required: true,
		}},
	})
	require.NoError(t, err)
	require.Empty(t, tools)
}
