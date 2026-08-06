package service

import (
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestAgentServiceRuntimeRolloutIsDependencyInjected(t *testing.T) {
	rollout, err := agentRuntime.ParseRollout("consult,assist,workflow")
	if err != nil {
		t.Fatalf("ParseRollout() error = %v", err)
	}

	service := NewAgentService(
		"http://127.0.0.1:1/v1",
		"test-key",
		"test-model",
		"127.0.0.1:1",
		nil,
		nil,
		nil,
		WithRuntimeRollout(rollout),
	)
	defer service.Close()

	if !service.RuntimeV2Enabled(agentRuntime.ModeConsult) {
		t.Fatal("consult mode should use Runtime v2")
	}
	if !service.RuntimeV2Enabled(agentRuntime.ModeWorkflow) {
		t.Fatal("workflow mode should use Runtime v2")
	}
	if !service.RuntimeV2Enabled(agentRuntime.ModeAssist) {
		t.Fatal("assist mode should use Runtime v2")
	}
	if service.RuntimeV2Enabled(agentRuntime.ModeChat) {
		t.Fatal("chat mode should remain on the legacy path")
	}
}

func TestAgentServiceRuntimeRolloutDefaultsToLegacyWithoutInfrastructure(t *testing.T) {
	service := NewAgentService(
		"http://127.0.0.1:1/v1",
		"test-key",
		"test-model",
		"127.0.0.1:1",
		nil,
		nil,
		nil,
	)
	defer service.Close()

	modes := []agentRuntime.Mode{
		agentRuntime.ModeChat,
		agentRuntime.ModeConsult,
		agentRuntime.ModeAssist,
		agentRuntime.ModeMulti,
		agentRuntime.ModeWorkflow,
	}
	for _, mode := range modes {
		if service.RuntimeV2Enabled(mode) {
			t.Fatalf("mode %q unexpectedly uses Runtime v2", mode)
		}
	}
}
