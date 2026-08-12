package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	agentEnvironment "twitter-clone/internal/module/agent/environment"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type workflowGoalCatalog struct {
	bindings []agentEnvironment.WorkflowToolBinding
}

func (catalog workflowGoalCatalog) ListWorkflowTools(
	context.Context,
	uint64,
	int,
) ([]agentEnvironment.WorkflowToolBinding, error) {
	return append([]agentEnvironment.WorkflowToolBinding(nil), catalog.bindings...), nil
}

type workflowGoalRunResolver struct {
	run WorkflowToolRunEvidence
}

func (resolver workflowGoalRunResolver) ResolveWorkflowToolRunEvidence(
	context.Context,
	uint64,
	string,
) (WorkflowToolRunEvidence, error) {
	return resolver.run, nil
}

func TestWorkflowToolGoalVerifierBindsPublishedRevisionChildRunAndOutput(t *testing.T) {
	const (
		publicationID = "64b64c9f7f0c2f11b9f0a001"
		workflowID    = "64b64c9f7f0c2f11b9f0a002"
		revisionID    = "64b64c9f7f0c2f11b9f0a003"
		childRunID    = "64b64c9f7f0c2f11b9f0a004"
		dslHash       = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	)
	toolName := "workflow_" + workflowID
	identity := agentEnvironment.WorkflowToolBindingIdentity{
		PublicationID: publicationID, PublicationRevision: 7,
		WorkflowID: workflowID, WorkflowRevisionID: revisionID,
		WorkflowRevisionNumber: 3, WorkflowDSLHash: dslHash,
	}
	bindingDigest, err := agentEnvironment.WorkflowToolBindingDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	binding := agentEnvironment.WorkflowToolBinding{
		Tool: agentRuntime.ToolDefinition{
			Name: toolName, Description: "published workflow",
			InputSchema: json.RawMessage(`{"type":"object"}`),
			Category:    agentRuntime.ToolCategoryRead,
		},
		PublicationID: publicationID, PublicationRevision: 7,
		WorkflowID: workflowID, WorkflowRevisionID: revisionID,
		WorkflowRevisionNumber: 3, WorkflowDSLHash: dslHash,
	}
	environment, err := agentEnvironment.NewWorkflowToolEnvironment(
		workflowGoalCatalog{bindings: []agentEnvironment.WorkflowToolBinding{binding}}, 42,
	)
	if err != nil {
		t.Fatal(err)
	}
	task := agentRuntime.TaskSpec{
		ID: "workflow-goal", Goal: "run the exact published workflow",
		AllowedTools: []string{toolName},
		CompletionCriteria: []agentRuntime.CompletionCriterion{{
			ID:          WorkflowToolOutputVerifiedCriterion,
			Description: "the authoritative child workflow output is verified", Required: true,
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
	response := "verified workflow response"
	responseHash := sha256.Sum256([]byte(response))
	runOutputHash := sha256.Sum256([]byte(`{"blackboard":{"end":{"response":"verified workflow response"}}}`))
	structured, err := json.Marshal(WorkflowToolResult{
		Schema:        WorkflowToolResultSchema,
		PublicationID: publicationID, PublicationRevision: 7,
		WorkflowID: workflowID, WorkflowRevisionID: revisionID,
		WorkflowRevisionNumber: 3, WorkflowDSLHash: dslHash,
		BindingDigest: bindingDigest, WorkflowRunID: childRunID,
		ParentRunID: "parent-run", ParentActionID: "workflow-action",
		Status: "success", Response: response,
		ResponseDigest:  "sha256:" + hex.EncodeToString(responseHash[:]),
		RunOutputDigest: "sha256:" + hex.EncodeToString(runOutputHash[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	finishedAt := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	run := agentRuntime.RunResult{
		Context: agentRuntime.RunContext{RunID: "parent-run", UserID: 42},
		Status:  agentRuntime.RunStatusCompleted,
		Steps: []agentRuntime.Step{{
			Index: 1, FinishedAt: finishedAt,
			Actions: []agentRuntime.Action{{
				ID: "workflow-action", Type: agentRuntime.ActionToolCall, Name: toolName,
			}},
			Observations: []agentRuntime.Observation{{
				ActionID: "workflow-action", Name: toolName, StructuredContent: structured,
			}},
		}},
	}
	resolver := workflowGoalRunResolver{run: WorkflowToolRunEvidence{
		WorkflowRunID: childRunID, WorkflowID: workflowID,
		WorkflowRevisionID: revisionID, WorkflowRevisionNumber: 3,
		InvocationSource: "runtime", ParentRunID: "parent-run",
		ParentActionID: "workflow-action", Status: "success",
		RunOutputDigest: "sha256:" + hex.EncodeToString(runOutputHash[:]),
		FinishedAt:      finishedAt,
	}}
	collector := WorkflowToolGoalCollector{Resolver: resolver}
	items, err := collector.Collect(context.Background(), agentRuntime.EvidenceCollectionRequest{
		Task: task, Run: run, Before: &before, After: &after,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("evidence = %+v", items)
	}
	ledger, err := (agentRuntime.EvidenceLedger{}).With(items[0])
	if err != nil {
		t.Fatal(err)
	}
	verifier := WorkflowToolGoalVerifier{Resolver: resolver}
	verified, err := verifier.Verify(context.Background(), agentRuntime.VerificationRequest{
		Task: task, Run: run, Before: &before, After: &after, Evidence: ledger,
	})
	if err != nil || !verified.Passed() {
		t.Fatalf("Verify() = %+v, %v", verified, err)
	}

	forged := ledger
	forged.Items = append([]agentRuntime.Evidence(nil), ledger.Items...)
	forged.Items[0].Digest = "sha256:" + dslHash
	verified, err = verifier.Verify(context.Background(), agentRuntime.VerificationRequest{
		Task: task, Run: run, Before: &before, After: &after, Evidence: forged,
	})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Passed() {
		t.Fatal("forged workflow evidence passed verification")
	}
}
