package profile

import agentRuntime "twitter-clone/internal/module/agent/runtime"

// PromptProfile identifies a versioned system prompt independently from the
// execution strategy that consumes it.
type PromptProfile struct {
	ID           string
	Version      string
	SystemPrompt string
}

// AgentProfile is versioned execution metadata owned by the application
// layer. Callers treat it as immutable; Runtime stays unaware of business
// roles and prompt wording.
type AgentProfile struct {
	ID           string
	Version      string
	Prompt       PromptProfile
	Budget       agentRuntime.Budget
	AllowedTools []string
}

func (p AgentProfile) AllowsTool(name string) bool {
	for _, allowed := range p.AllowedTools {
		if name == allowed {
			return true
		}
	}
	return false
}

// FilterTools applies a fail-closed allowlist while preserving provider order.
func (p AgentProfile) FilterTools(tools []agentRuntime.ToolDefinition) []agentRuntime.ToolDefinition {
	filtered := make([]agentRuntime.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		if p.AllowsTool(tool.Name) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func (p AgentProfile) PrimaryTool() string {
	if len(p.AllowedTools) == 0 {
		return ""
	}
	return p.AllowedTools[0]
}
