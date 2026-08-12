package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ExplicitCapabilityPlanner resolves caller-supplied capability IDs and uses
// conversation.reply when no capability was selected. It intentionally does
// not infer product routes from user wording.
type ExplicitCapabilityPlanner struct {
	catalog AgentCapabilityCatalog
}

func NewExplicitCapabilityPlanner(catalog AgentCapabilityCatalog) AgentCapabilityPlanner {
	if catalog == nil {
		builtIn, err := NewBuiltInAgentCapabilityCatalog()
		if err != nil {
			panic(fmt.Sprintf("invalid built-in agent capability catalog: %v", err))
		}
		catalog = builtIn
	}
	return ExplicitCapabilityPlanner{catalog: catalog}
}

func (p ExplicitCapabilityPlanner) Plan(
	ctx context.Context,
	request AgentCapabilityPlanRequest,
) (AgentCapabilityPlan, error) {
	if err := ctx.Err(); err != nil {
		return AgentCapabilityPlan{}, err
	}
	if p.catalog == nil {
		return AgentCapabilityPlan{}, errors.New("agent capability catalog is not configured")
	}

	preferred := uniqueCapabilityIDs(request.PreferredCapabilityIDs)
	if len(preferred) > 0 {
		return p.catalog.ResolvePlan(ctx, preferred)
	}
	if strings.TrimSpace(request.Query) == "" {
		return AgentCapabilityPlan{}, fmt.Errorf("%w: content is required", ErrInvalidUnifiedAgentRequest)
	}

	return p.catalog.ResolvePlan(ctx, []string{CapabilityConversationReply})
}
