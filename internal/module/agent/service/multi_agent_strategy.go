package service

import (
	"context"
	"errors"
	"fmt"

	agentStrategy "twitter-clone/internal/module/agent/strategy"
)

var ErrMultiAgentExecutionUnavailable = errors.New("multi-agent execution is unavailable")

// NewBuiltInAgentExecutionStrategyPlanner creates a deterministic admission
// snapshot. ExecutorAvailable must reflect the independently controlled
// aggregate executor rollout; admission alone never changes execution.
func NewBuiltInAgentExecutionStrategyPlanner(policy agentStrategy.Policy) (agentStrategy.Planner, error) {
	return agentStrategy.NewDeterministicPlanner(policy, builtInMultiAgentStrategyTemplates())
}

func (s *AgentService) planUnifiedAgentExecutionStrategy(
	ctx context.Context,
	request UnifiedAgentRequest,
	capabilityPlan AgentCapabilityPlan,
) (agentStrategy.Plan, error) {
	if s == nil || s.executionStrategyPlanner == nil {
		return agentStrategy.Plan{}, errors.New("agent execution strategy planner is not configured")
	}

	strategyRequest := agentStrategy.Request{
		Query: request.Content, ExecutionProfile: capabilityPlan.ExecutionProfile,
		CapabilityIDs: append([]string(nil), capabilityPlan.CapabilityIDs...),
	}
	var admissionProfileID string
	var admissionProfileVersion string
	profileSpec, supportsMultiAgent := multiAgentProfileSpecFor(capabilityPlan.ExecutionProfile)
	if supportsMultiAgent {
		selected, err := s.resolveAgentProfile(ctx, profileSpec.parentProfileID, request.UserID)
		if err != nil {
			return agentStrategy.Plan{}, fmt.Errorf("resolve multi-agent admission profile: %w", err)
		}
		strategyRequest.Budget = selected.Budget
		strategyRequest.AllowedTools = append([]string(nil), selected.AllowedTools...)
		admissionProfileID = selected.ID
		admissionProfileVersion = selected.Version
	}

	plan, err := s.executionStrategyPlanner.Plan(ctx, strategyRequest)
	if err != nil {
		return agentStrategy.Plan{}, fmt.Errorf("plan agent execution strategy: %w", err)
	}
	if plan.SelectedStrategy == agentStrategy.KindMultiAgent {
		if !s.multiAgentExecutionEnabled || !supportsMultiAgent {
			return agentStrategy.Plan{}, fmt.Errorf(
				"%w: aggregate lifecycle executor is disabled for plan %s",
				ErrMultiAgentExecutionUnavailable,
				plan.PlanDigest,
			)
		}
		plan, err = agentStrategy.BindProfileSet(plan, admissionProfileID, admissionProfileVersion)
		if err != nil {
			return agentStrategy.Plan{}, fmt.Errorf("pin multi-agent profile set: %w", err)
		}
	}
	return plan, nil
}
