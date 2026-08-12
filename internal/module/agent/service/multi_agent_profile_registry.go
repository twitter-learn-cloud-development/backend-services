package service

import (
	"fmt"

	agentMultiRole "twitter-clone/internal/module/agent/multirole"
	"twitter-clone/internal/module/agent/repository"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

// multiAgentProfileSpec is the single built-in mapping from a capability
// execution profile to its governed strategy and role-execution settings.
// Registry functions return value copies so callers cannot mutate shared
// process state.
type multiAgentProfileSpec struct {
	executionProfile     string
	templateID           string
	researchCapabilityID string
	draftCapabilityID    string
	researchTools        []string
	parentProfileID      string
	researcherProfileID  string
	requiredTool         string
	label                string
	dialogueMode         repository.DialogueMode
	runtimeMode          agentRuntime.Mode
}

func builtInMultiAgentProfileSpecs() []multiAgentProfileSpec {
	return []multiAgentProfileSpec{
		{
			executionProfile:     ExecutionProfileRuntimeResearchDraft,
			templateID:           "platform.research_draft.v1",
			researchCapabilityID: CapabilityPlatformSearch,
			draftCapabilityID:    CapabilityContentDraft,
			researchTools:        []string{"hybrid_search_tweets"},
			parentProfileID:      profileUnifiedResearchDraft,
			researcherProfileID:  profileMultiPlatformResearcher,
			requiredTool:         "hybrid_search_tweets",
			label:                "platform research draft",
			dialogueMode:         repository.ModeAssist,
			runtimeMode:          agentRuntime.ModeAssist,
		},
		{
			executionProfile:     ExecutionProfileRuntimeWebDraft,
			templateID:           "web.research_draft.v1",
			researchCapabilityID: CapabilityWebSearch,
			draftCapabilityID:    CapabilityContentDraft,
			researchTools:        []string{"web_search", "page_read"},
			parentProfileID:      profileUnifiedWebDraft,
			researcherProfileID:  profileMultiWebResearcher,
			requiredTool:         "web_search",
			label:                "web research draft",
			dialogueMode:         repository.ModeAssist,
			runtimeMode:          agentRuntime.ModeAssist,
		},
	}
}

func multiAgentProfileSpecFor(executionProfile string) (multiAgentProfileSpec, bool) {
	for _, spec := range builtInMultiAgentProfileSpecs() {
		if spec.executionProfile == executionProfile {
			spec.researchTools = append([]string(nil), spec.researchTools...)
			return spec, true
		}
	}
	return multiAgentProfileSpec{}, false
}

func builtInMultiAgentStrategyTemplates() []agentStrategy.Template {
	specs := builtInMultiAgentProfileSpecs()
	templates := make([]agentStrategy.Template, 0, len(specs))
	for _, spec := range specs {
		templates = append(templates, agentMultiRole.ResearchDraftTemplate(
			spec.templateID,
			spec.executionProfile,
			spec.researchCapabilityID,
			spec.draftCapabilityID,
			spec.researchTools,
		))
	}
	return templates
}

func multiAgentConfig(executionProfile, templateID string) (multiAgentExecutionConfig, error) {
	spec, ok := multiAgentProfileSpecFor(executionProfile)
	if !ok || spec.templateID != templateID {
		return multiAgentExecutionConfig{}, fmt.Errorf(
			"%w: profile %q template %q",
			ErrMultiAgentPlanUnsupported,
			executionProfile,
			templateID,
		)
	}
	return multiAgentExecutionConfig{
		templateID:          spec.templateID,
		parentProfileID:     spec.parentProfileID,
		researcherProfileID: spec.researcherProfileID,
		requiredTool:        spec.requiredTool,
		label:               spec.label,
		dialogueMode:        spec.dialogueMode,
		runtimeMode:         spec.runtimeMode,
	}, nil
}
