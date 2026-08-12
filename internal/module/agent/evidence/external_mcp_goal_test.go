package evidence

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type externalMCPGoalCatalog struct {
	bindings []agentEnvironment.ExternalMCPToolBinding
}

func (catalog externalMCPGoalCatalog) ListExternalMCPTools(
	context.Context,
	uint64,
) ([]agentEnvironment.ExternalMCPToolBinding, error) {
	return append([]agentEnvironment.ExternalMCPToolBinding(nil), catalog.bindings...), nil
}

func TestExternalMCPReadGoalVerifierBindsObservationToAuthorizationSnapshot(t *testing.T) {
	const qualifiedName = "mcp_crm.lookup"
	binding := agentEnvironment.ExternalMCPToolBinding{
		Tool: agentRuntime.ToolDefinition{
			Name: qualifiedName, Category: agentRuntime.ToolCategoryRead,
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
		ConnectionID:      "mcpconn_0123456789abcdef0123456789abcdef",
		ConnectionOwnerID: 42, ConnectionScope: "user", ConnectionRevision: 7,
		ServerID: "mcp_crm", SnapshotID: "mcpsnap_0123456789abcdef0123456789abcdef",
		SnapshotVersion:  3,
		SchemaHash:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		PolicySnapshotID: "mcpsnap_0123456789abcdef0123456789abcdef",
		PolicyToolName:   "lookup", PolicyCategory: "read",
		PolicyQualifiedName: qualifiedName, PolicyEnabled: true,
	}
	environment, err := agentEnvironment.NewExternalMCPEnvironment(externalMCPGoalCatalog{
		bindings: []agentEnvironment.ExternalMCPToolBinding{binding},
	}, 42)
	if err != nil {
		t.Fatal(err)
	}
	task := agentRuntime.TaskSpec{
		ID: "external-mcp-read", Goal: "read from the approved connector",
		AllowedTools: []string{qualifiedName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          ExternalMCPReadObservedCriterion,
			Description: "a tenant-bound MCP read observation is verified", Required: true,
		}},
	}
	before, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseAfter,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "run-mcp-read", UserID: 42},
		Status:  agentRuntime.RunStatusCompleted,
		Steps: []agentRuntime.Step{{
			Index: 1, FinishedAt: time.Date(2026, 8, 11, 14, 0, 0, 0, time.UTC),
			Actions: []agentRuntime.Action{{
				ID: "read-1", Type: agentRuntime.ActionToolCall, Name: qualifiedName,
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "read-1", Name: qualifiedName,
				StructuredContent: json.RawMessage(`{"count":1}`),
			}},
		}},
	}
	items, err := (ExternalMCPReadGoalCollector{}).Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: run, Before: &before, After: &after,
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("Collect() items/error = %+v/%v", items, err)
	}
	ledger, err := (agentRuntime.EvidenceLedger{}).With(items[0])
	if err != nil {
		t.Fatal(err)
	}
	verification, err := (ExternalMCPReadGoalVerifier{}).Verify(context.Background(), agentRuntime.VerificationRequest{
		Task: task, Run: run, Before: &before, After: &after, Evidence: ledger,
	})
	if err != nil || !verification.Passed() {
		t.Fatalf("Verify() result/error = %+v/%v", verification, err)
	}

	forged := ledger
	forged.Items[0].Digest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	verification, err = (ExternalMCPReadGoalVerifier{}).Verify(context.Background(), agentRuntime.VerificationRequest{
		Task: task, Run: run, Before: &before, After: &after, Evidence: forged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if verification.Passed() {
		t.Fatalf("forged evidence passed: %+v", verification)
	}
}
