package service

import (
	"context"
	"errors"
	"testing"
)

func TestExplicitCapabilityPlannerDoesNotInferKeywords(t *testing.T) {
	planner := NewExplicitCapabilityPlanner(nil)

	plan, err := planner.Plan(context.Background(), AgentCapabilityPlanRequest{
		Query: "请先搜索云原生资料，再写一篇推文",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.ExecutionProfile != ExecutionProfileRuntimeChat ||
		len(plan.CapabilityIDs) != 1 || plan.CapabilityIDs[0] != CapabilityConversationReply {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestExplicitCapabilityPlannerResolvesSelectedCapabilities(t *testing.T) {
	planner := NewExplicitCapabilityPlanner(nil)

	plan, err := planner.Plan(context.Background(), AgentCapabilityPlanRequest{
		Query:                  "任意正文",
		PreferredCapabilityIDs: []string{CapabilityPlatformSearch, CapabilityContentDraft},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.ExecutionProfile != ExecutionProfileRuntimeResearchDraft ||
		!sameCapabilityIDs(plan.CapabilityIDs, []string{CapabilityPlatformSearch, CapabilityContentDraft}) {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestExplicitCapabilityPlannerRejectsUnknownCapability(t *testing.T) {
	planner := NewExplicitCapabilityPlanner(nil)

	_, err := planner.Plan(context.Background(), AgentCapabilityPlanRequest{
		Query:                  "read my calendar",
		PreferredCapabilityIDs: []string{"calendar.read"},
	})
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("Plan() error = %v, want ErrUnsupportedCapability", err)
	}
}

func TestExplicitCapabilityPlannerRejectsUnavailableCapability(t *testing.T) {
	catalog, err := NewAgentCapabilityCatalog(
		[]AgentCapabilityDefinition{{
			ID: "web.search", Version: "v1", Status: AgentCapabilityPlanned,
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("NewAgentCapabilityCatalog() error = %v", err)
	}
	planner := NewExplicitCapabilityPlanner(catalog)

	_, err = planner.Plan(context.Background(), AgentCapabilityPlanRequest{
		Query:                  "search the web",
		PreferredCapabilityIDs: []string{"web.search"},
	})
	if !errors.Is(err, ErrCapabilityUnavailable) {
		t.Fatalf("Plan() error = %v, want ErrCapabilityUnavailable", err)
	}
}

func TestExplicitCapabilityPlannerHonorsCancellation(t *testing.T) {
	planner := NewExplicitCapabilityPlanner(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := planner.Plan(ctx, AgentCapabilityPlanRequest{Query: "hello"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Plan() error = %v, want context.Canceled", err)
	}
}
