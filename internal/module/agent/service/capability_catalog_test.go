package service

import (
	"context"
	"errors"
	"testing"
)

func TestAgentCapabilityCatalogReturnsImmutableSnapshots(t *testing.T) {
	t.Parallel()

	catalog, err := NewBuiltInAgentCapabilityCatalog()
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	definitions, err := catalog.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(definitions) != 7 {
		t.Fatalf("definitions = %d, want 7", len(definitions))
	}
	assertCapabilityDefinitionStatus(t, definitions, CapabilityExternalMCP, AgentCapabilityPlanned)
	assertCapabilityDefinitionStatus(t, definitions, CapabilityWorkflowRun, AgentCapabilityPlanned)
	assertCapabilityDefinitionStatus(t, definitions, CapabilitySkillRun, AgentCapabilityPlanned)
	definitions[0].Dependencies = append(definitions[0].Dependencies, "mutated")

	plan, err := catalog.ResolvePlan(context.Background(), []string{
		CapabilityContentDraft,
		CapabilityPlatformSearch,
	})
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	plan.CapabilityIDs[0] = "mutated"

	secondPlan, err := catalog.ResolvePlan(context.Background(), []string{
		CapabilityContentDraft,
		CapabilityPlatformSearch,
	})
	if err != nil {
		t.Fatalf("ResolvePlan() second error = %v", err)
	}
	assertCapabilityIDs(
		t,
		secondPlan.CapabilityIDs,
		[]string{CapabilityPlatformSearch, CapabilityContentDraft},
	)
}

func TestBuiltInCapabilityCatalogEnablesExternalMCPOnlyByOption(t *testing.T) {
	t.Parallel()

	defaultCatalog, err := NewBuiltInAgentCapabilityCatalog()
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	if _, err := defaultCatalog.ResolvePlan(
		context.Background(),
		[]string{CapabilityExternalMCP},
	); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("default external MCP error = %v", err)
	}

	enabledCatalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableExternalMCPCapability())
	if err != nil {
		t.Fatalf("enabled catalog error = %v", err)
	}
	plan, err := enabledCatalog.ResolvePlan(
		context.Background(),
		[]string{CapabilityExternalMCP},
	)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if plan.ExecutionProfile != ExecutionProfileRuntimeExternalMCP {
		t.Fatalf("ExecutionProfile = %q", plan.ExecutionProfile)
	}
	assertCapabilityIDs(t, plan.CapabilityIDs, []string{CapabilityExternalMCP})
}

func TestBuiltInCapabilityCatalogEnablesWebSearchOnlyByOption(t *testing.T) {
	t.Parallel()

	defaultCatalog, err := NewBuiltInAgentCapabilityCatalog()
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	if _, err := defaultCatalog.ResolvePlan(
		context.Background(),
		[]string{CapabilityWebSearch},
	); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("default web search error = %v", err)
	}

	enabledCatalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWebSearchCapability())
	if err != nil {
		t.Fatalf("enabled catalog error = %v", err)
	}
	plan, err := enabledCatalog.ResolvePlan(
		context.Background(),
		[]string{CapabilityWebSearch, CapabilityContentDraft},
	)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if plan.ExecutionProfile != ExecutionProfileRuntimeWebDraft {
		t.Fatalf("ExecutionProfile = %q", plan.ExecutionProfile)
	}
	assertCapabilityIDs(
		t,
		plan.CapabilityIDs,
		[]string{CapabilityWebSearch, CapabilityContentDraft},
	)
}

func TestBuiltInCapabilityCatalogEnablesWorkflowOnlyByOption(t *testing.T) {
	t.Parallel()

	defaultCatalog, err := NewBuiltInAgentCapabilityCatalog()
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	if _, err := defaultCatalog.ResolvePlan(
		context.Background(),
		[]string{CapabilityWorkflowRun},
	); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("default workflow error = %v", err)
	}

	enabledCatalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableWorkflowCapability())
	if err != nil {
		t.Fatalf("enabled catalog error = %v", err)
	}
	plan, err := enabledCatalog.ResolvePlan(
		context.Background(),
		[]string{CapabilityWorkflowRun},
	)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if plan.ExecutionProfile != ExecutionProfileRuntimeWorkflow {
		t.Fatalf("ExecutionProfile = %q", plan.ExecutionProfile)
	}
	assertCapabilityIDs(t, plan.CapabilityIDs, []string{CapabilityWorkflowRun})
}

func TestBuiltInCapabilityCatalogEnablesSkillOnlyByOption(t *testing.T) {
	t.Parallel()

	defaultCatalog, err := NewBuiltInAgentCapabilityCatalog()
	if err != nil {
		t.Fatalf("NewBuiltInAgentCapabilityCatalog() error = %v", err)
	}
	if _, err := defaultCatalog.ResolvePlan(
		context.Background(),
		[]string{CapabilitySkillRun},
	); !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("default Skill error = %v", err)
	}

	enabledCatalog, err := NewBuiltInAgentCapabilityCatalog(WithAvailableSkillCapability())
	if err != nil {
		t.Fatalf("enabled catalog error = %v", err)
	}
	plan, err := enabledCatalog.ResolvePlan(
		context.Background(),
		[]string{CapabilitySkillRun},
	)
	if err != nil {
		t.Fatalf("ResolvePlan() error = %v", err)
	}
	if plan.ExecutionProfile != ExecutionProfileRuntimeSkill {
		t.Fatalf("ExecutionProfile = %q", plan.ExecutionProfile)
	}
	assertCapabilityIDs(t, plan.CapabilityIDs, []string{CapabilitySkillRun})
}

func TestAgentCapabilityCatalogRejectsDependencyCycles(t *testing.T) {
	t.Parallel()

	_, err := NewAgentCapabilityCatalog(
		[]AgentCapabilityDefinition{
			{
				ID: "a", Version: "v1", Status: AgentCapabilityAvailable,
				Dependencies: []string{"b"},
			},
			{
				ID: "b", Version: "v1", Status: AgentCapabilityAvailable,
				Dependencies: []string{"a"},
			},
		},
		nil,
	)
	if err == nil {
		t.Fatal("NewAgentCapabilityCatalog() error = nil, want dependency cycle error")
	}
}

func TestAgentCapabilityCatalogRejectsRouteThatViolatesDependencyOrder(t *testing.T) {
	t.Parallel()

	_, err := NewAgentCapabilityCatalog(
		[]AgentCapabilityDefinition{
			{ID: "search", Version: "v1", Status: AgentCapabilityAvailable},
			{
				ID: "draft", Version: "v1", Status: AgentCapabilityAvailable,
				Dependencies: []string{"search"},
			},
		},
		[]AgentCapabilityRoute{{
			CapabilityIDs:        []string{"search", "draft"},
			OrderedCapabilityIDs: []string{"draft", "search"},
			ExecutionProfile:     "invalid",
		}},
	)
	if err == nil {
		t.Fatal("NewAgentCapabilityCatalog() error = nil, want dependency order error")
	}
}

func TestAgentCapabilityCatalogFailsClosedForPlannedCapability(t *testing.T) {
	t.Parallel()

	catalog, err := NewAgentCapabilityCatalog(
		[]AgentCapabilityDefinition{{
			ID: "web.search", Version: "v1", Status: AgentCapabilityPlanned,
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("NewAgentCapabilityCatalog() error = %v", err)
	}
	_, err = catalog.ResolvePlan(context.Background(), []string{"web.search"})
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("ResolvePlan() error = %v, want ErrCapabilityUnavailable", err)
	}
}

func assertCapabilityDefinitionStatus(
	t *testing.T,
	definitions []AgentCapabilityDefinition,
	capabilityID string,
	want AgentCapabilityStatus,
) {
	t.Helper()
	for _, definition := range definitions {
		if definition.ID == capabilityID {
			if definition.Status != want {
				t.Fatalf("capability %q status = %q, want %q", capabilityID, definition.Status, want)
			}
			return
		}
	}
	t.Fatalf("capability %q not found", capabilityID)
}
