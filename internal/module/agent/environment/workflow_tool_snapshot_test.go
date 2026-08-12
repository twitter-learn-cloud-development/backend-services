package environment

import (
	"context"
	"encoding/json"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestDecodeWorkflowToolSnapshotBindsActorAndImmutableCatalog(t *testing.T) {
	binding := workflowBinding("workflow_64b64c9f7f0c2f11b9f0a010", workflowPublicationID)
	environment, err := NewWorkflowToolEnvironment(
		&staticWorkflowToolCatalog{bindings: []WorkflowToolBinding{binding}},
		42,
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: workflowTask(binding.Tool.Name), Phase: agentRuntime.SnapshotPhaseBefore,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := DecodeWorkflowToolSnapshot(&snapshot, agentRuntime.SnapshotPhaseBefore, 42)
	if err != nil {
		t.Fatalf("DecodeWorkflowToolSnapshot() error = %v", err)
	}
	if len(view.Tools) != 1 || view.Tools[0].Name != binding.Tool.Name ||
		view.Tools[0].BindingDigest != workflowBindingDigest(binding) {
		t.Fatalf("decoded view = %+v", view)
	}
	if _, err = DecodeWorkflowToolSnapshot(&snapshot, agentRuntime.SnapshotPhaseBefore, 43); err == nil {
		t.Fatal("cross-actor workflow snapshot decoded successfully")
	}

	var metadata workflowToolSnapshotMetadata
	if err = json.Unmarshal(snapshot.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.Tools[0].BindingDigest = "sha256:" + workflowDSLHash
	snapshot.Metadata, err = json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeWorkflowToolSnapshot(&snapshot, agentRuntime.SnapshotPhaseBefore, 42); err == nil {
		t.Fatal("tampered workflow snapshot decoded successfully")
	}
}
