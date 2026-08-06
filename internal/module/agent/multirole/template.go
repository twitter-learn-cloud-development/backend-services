package multirole

import (
	"time"

	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

// ResearchDraftTemplate centralizes the production topology and role budgets.
// Business capability identifiers remain supplied by the application layer.
func ResearchDraftTemplate(
	templateID string,
	executionProfile string,
	researchCapabilityID string,
	draftCapabilityID string,
	researchTools []string,
) agentStrategy.Template {
	return agentStrategy.Template{
		ID: templateID, ExecutionProfile: executionProfile,
		RequiredCapabilityIDs: []string{researchCapabilityID, draftCapabilityID},
		MaxParallelRoles:      1,
		Roles: []agentStrategy.RoleTemplate{
			{
				RoleID: RoleResearcher, CapabilityIDs: []string{researchCapabilityID},
				AllowedTools: append([]string(nil), researchTools...), MaxSteps: 3,
				RequiredTotalTokens: 10_000, RequiredCostMicros: 45_000,
				EstimatedLatency: 25 * time.Second,
			},
			{
				RoleID: RoleDrafter, CapabilityIDs: []string{draftCapabilityID}, MaxSteps: 1,
				RequiredTotalTokens: 9_000, RequiredCostMicros: 35_000,
				EstimatedLatency: 17 * time.Second,
			},
			{
				RoleID: RoleReviewer, CapabilityIDs: []string{draftCapabilityID}, MaxSteps: 1,
				RequiredTotalTokens: 5_000, RequiredCostMicros: 20_000,
				EstimatedLatency: 8 * time.Second,
			},
		},
	}
}
