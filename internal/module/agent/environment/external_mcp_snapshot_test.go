package environment

import (
	"context"
	"encoding/json"
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestDecodeExternalMCPSnapshotBindsActorAndConnectionRevision(t *testing.T) {
	binding := externalMCPBinding("crm.lookup", "lookup", agentRuntime.ToolCategoryRead, false)
	environment, err := NewExternalMCPEnvironment(
		&staticExternalMCPToolCatalog{bindings: []ExternalMCPToolBinding{binding}},
		42,
	)
	if err != nil {
		t.Fatal(err)
	}
	task := externalMCPTask(binding.Tool.Name)
	snapshot, err := environment.Snapshot(context.Background(), agentRuntime.SnapshotRequest{
		Task: task, Phase: agentRuntime.SnapshotPhaseBefore,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := DecodeExternalMCPSnapshot(&snapshot, agentRuntime.SnapshotPhaseBefore, 42)
	if err != nil {
		t.Fatalf("DecodeExternalMCPSnapshot() error = %v", err)
	}
	if len(view.Tools) != 1 || view.Tools[0].Name != binding.Tool.Name ||
		view.Tools[0].ConnectionRevision != binding.ConnectionRevision ||
		view.Tools[0].BindingDigest == "" {
		t.Fatalf("decoded view = %+v", view)
	}
	if _, err := DecodeExternalMCPSnapshot(&snapshot, agentRuntime.SnapshotPhaseBefore, 43); err == nil {
		t.Fatal("cross-actor snapshot decode error = nil")
	}

	tampered := snapshot
	var metadata map[string]interface{}
	if err := json.Unmarshal(tampered.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	tools := metadata["tools"].([]interface{})
	tools[0].(map[string]interface{})["connection_revision"] = float64(binding.ConnectionRevision + 1)
	tampered.Metadata, _ = json.Marshal(metadata)
	if _, err := DecodeExternalMCPSnapshot(&tampered, agentRuntime.SnapshotPhaseBefore, 42); err == nil {
		t.Fatal("tampered revision snapshot decode error = nil")
	}
}
