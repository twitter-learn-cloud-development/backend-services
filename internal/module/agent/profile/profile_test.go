package profile

import (
	"testing"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

func TestAgentProfileFiltersToolsFailClosed(t *testing.T) {
	profile := AgentProfile{AllowedTools: []string{"search"}}
	filtered := profile.FilterTools([]agentRuntime.ToolDefinition{
		{Name: "search", Category: agentRuntime.ToolCategoryRead},
		{Name: "publish", Category: agentRuntime.ToolCategoryWrite},
	})

	if len(filtered) != 1 || filtered[0].Name != "search" {
		t.Fatalf("FilterTools() = %+v", filtered)
	}
	if profile.PrimaryTool() != "search" || profile.AllowsTool("publish") {
		t.Fatalf("profile allowlist behavior is inconsistent")
	}
}
