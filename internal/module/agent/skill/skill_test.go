package skill

import (
	"encoding/json"
	"testing"
	"time"

	agentRuntime "twitter-clone/internal/module/agent/runtime"

	"github.com/stretchr/testify/require"
)

func TestValidateVersionAndOutputContract(t *testing.T) {
	t.Parallel()

	version := validVersion()
	require.NoError(t, ValidateVersion(version))
	require.NoError(t, ValidateOutput(
		version.Output,
		json.RawMessage(`{"schema":"workflow.run.v1","status":"success","response":"done"}`),
	))
	require.ErrorContains(t, ValidateOutput(
		version.Output,
		json.RawMessage(`{"schema":"workflow.run.v1","status":"failed","response":"done"}`),
	), "contract validation failed")
}

func TestValidateVersionRejectsAuthorityAndBudgetDrift(t *testing.T) {
	t.Parallel()

	version := validVersion()
	version.AllowedTools = []string{"workflow_aaaaaaaaaaaaaaaaaaaaaaaa", "other"}
	require.ErrorContains(t, ValidateVersion(version), "exactly its bound workflow tool")

	version = validVersion()
	version.Budget.Deadline = time.Now()
	require.ErrorContains(t, ValidateVersion(version), "request-specific deadline")
}

func TestCloneVersionDoesNotShareMutableSlices(t *testing.T) {
	t.Parallel()

	original := validVersion()
	cloned := CloneVersion(original)
	cloned.AllowedTools[0] = "changed"
	cloned.Output.SchemaJSON[0] = '['
	cloned.Workflow.InputSchemaJSON[0] = '['

	require.Equal(t, "workflow_aaaaaaaaaaaaaaaaaaaaaaaa", original.AllowedTools[0])
	require.Equal(t, byte('{'), original.Output.SchemaJSON[0])
	require.Equal(t, byte('{'), original.Workflow.InputSchemaJSON[0])
}

func validVersion() Version {
	return Version{
		ContractVersion: ContractVersionV1,
		ID:              "workflow_aaaaaaaaaaaaaaaaaaaaaaaa",
		Version:         "v1-0123456789abcdef",
		DisplayName:     "Digest",
		Description:     "Build a digest.",
		Instructions:    "Run the bound workflow and answer from its result.",
		Source:          SourceWorkflow,
		AllowedTools:    []string{"workflow_aaaaaaaaaaaaaaaaaaaaaaaa"},
		Profile: ProfileBinding{
			ID: "unified.workflow", Version: "v1",
			PromptID: "unified.workflow.system", PromptVersion: "v1",
		},
		Budget: agentRuntime.Budget{
			MaxSteps: 5, MaxInputTokens: 12_000, MaxOutputTokens: 2_048,
			MaxTotalTokens: 30_000, MaxEstimatedCostMicros: 120_000,
			Timeout: 75 * time.Second,
		},
		Output: OutputContract{
			SchemaID: "workflow.run.v1", ContentType: "application/json",
			SchemaJSON: json.RawMessage(`{
				"type":"object",
				"properties":{
					"schema":{"const":"workflow.run.v1"},
					"status":{"const":"success"},
					"response":{"type":"string"}
				},
				"required":["schema","status","response"]
			}`),
		},
		Workflow: &WorkflowBinding{
			PublicationID: "bbbbbbbbbbbbbbbbbbbbbbbb", PublicationRevision: 1,
			WorkflowID:             "aaaaaaaaaaaaaaaaaaaaaaaa",
			WorkflowRevisionID:     "cccccccccccccccccccccccc",
			WorkflowRevisionNumber: 1, WorkflowDSLHash: "deadbeef",
			ToolName:        "workflow_aaaaaaaaaaaaaaaaaaaaaaaa",
			InputSchemaJSON: json.RawMessage(`{"type":"object"}`),
		},
	}
}
