package service

import (
	"errors"
	"testing"

	agentMultiRole "twitter-clone/internal/module/agent/multirole"
)

func TestBuiltInMultiAgentProfileSpecsDriveStrategyAndExecution(t *testing.T) {
	specs := builtInMultiAgentProfileSpecs()
	if len(specs) != 2 {
		t.Fatalf("builtInMultiAgentProfileSpecs() count = %d, want 2", len(specs))
	}
	templates := builtInMultiAgentStrategyTemplates()
	if len(templates) != len(specs) {
		t.Fatalf("strategy template count = %d, want %d", len(templates), len(specs))
	}
	for index, spec := range specs {
		template := templates[index]
		if template.ID != spec.templateID || template.ExecutionProfile != spec.executionProfile ||
			len(template.Roles) != 3 || template.Roles[0].RoleID != agentMultiRole.RoleResearcher {
			t.Fatalf("strategy template %d = %+v for spec %+v", index, template, spec)
		}
		config, err := multiAgentConfig(spec.executionProfile, spec.templateID)
		if err != nil {
			t.Fatalf("multiAgentConfig(%q, %q) error = %v", spec.executionProfile, spec.templateID, err)
		}
		if config.parentProfileID != spec.parentProfileID ||
			config.researcherProfileID != spec.researcherProfileID ||
			config.requiredTool != spec.requiredTool || config.dialogueMode != spec.dialogueMode ||
			config.runtimeMode != spec.runtimeMode {
			t.Fatalf("execution config = %+v for spec %+v", config, spec)
		}
	}
}

func TestMultiAgentProfileRegistryReturnsIndependentToolSlices(t *testing.T) {
	first, ok := multiAgentProfileSpecFor(ExecutionProfileRuntimeWebDraft)
	if !ok {
		t.Fatal("web draft profile is missing")
	}
	first.researchTools[0] = "mutated"
	second, ok := multiAgentProfileSpecFor(ExecutionProfileRuntimeWebDraft)
	if !ok || len(second.researchTools) != 2 || second.researchTools[0] != "web_search" {
		t.Fatalf("registry shared mutable tools: %+v", second.researchTools)
	}
}

func TestMultiAgentConfigRejectsUnknownProfileAndMismatchedTemplate(t *testing.T) {
	for _, testCase := range []struct {
		profile  string
		template string
	}{
		{profile: "runtime.unknown", template: "platform.research_draft.v1"},
		{profile: ExecutionProfileRuntimeResearchDraft, template: "web.research_draft.v1"},
	} {
		_, err := multiAgentConfig(testCase.profile, testCase.template)
		if !errors.Is(err, ErrMultiAgentPlanUnsupported) {
			t.Fatalf("multiAgentConfig(%q, %q) error = %v", testCase.profile, testCase.template, err)
		}
	}
}
