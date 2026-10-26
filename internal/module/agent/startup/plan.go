package startup

import (
	"fmt"
	"strings"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

const (
	ProcessRoleEnv           = "AGENT_PROCESS_ROLE"
	TrendingReporterOwnerEnv = "AGENT_TRENDING_REPORTER_OWNER"
)

type ProcessRole string

const (
	ProcessRoleAPI    ProcessRole = "api"
	ProcessRoleWorker ProcessRole = "worker"
	ProcessRoleAll    ProcessRole = "all"
)

type TrendingReporterOwner string

const (
	TrendingReporterOwnerDisabled TrendingReporterOwner = "disabled"
	TrendingReporterOwnerTemporal TrendingReporterOwner = "temporal"
)

type Plan struct {
	role                  ProcessRole
	trendingReporterOwner TrendingReporterOwner
}

func Parse(rawRole, rawReporterOwner string) (Plan, error) {
	role := ProcessRole(strings.ToLower(strings.TrimSpace(rawRole)))
	if role == "" {
		role = ProcessRoleAll
	}
	switch role {
	case ProcessRoleAPI, ProcessRoleWorker, ProcessRoleAll:
	default:
		return Plan{}, fmt.Errorf(
			"invalid %s=%q: expected api, worker, or all",
			ProcessRoleEnv,
			rawRole,
		)
	}

	reporterOwner := TrendingReporterOwner(strings.ToLower(strings.TrimSpace(rawReporterOwner)))
	if reporterOwner == "" {
		reporterOwner = TrendingReporterOwnerTemporal
	}
	switch reporterOwner {
	case TrendingReporterOwnerDisabled, TrendingReporterOwnerTemporal:
	default:
		return Plan{}, fmt.Errorf(
			"invalid %s=%q: expected temporal or disabled",
			TrendingReporterOwnerEnv,
			rawReporterOwner,
		)
	}

	return Plan{
		role:                  role,
		trendingReporterOwner: reporterOwner,
	}, nil
}

func (p Plan) Role() ProcessRole {
	return p.role
}

func (p Plan) TrendingReporterOwner() TrendingReporterOwner {
	return p.trendingReporterOwner
}

func (p Plan) StartsAPI() bool {
	return p.role == ProcessRoleAPI || p.role == ProcessRoleAll
}

func (p Plan) StartsWorkers() bool {
	return p.role == ProcessRoleWorker || p.role == ProcessRoleAll
}

func (p Plan) NeedsPersona() bool { return p.StartsAPI() }

// Shared run admission only covers the Runtime runner and Workflow service.
// An API process must not silently keep legacy entry points outside that gate.
func (p Plan) ValidateSharedAdmission(enabled bool, rollout agentRuntime.Rollout) error {
	if !enabled || !p.StartsAPI() {
		return nil
	}
	for _, mode := range []agentRuntime.Mode{agentRuntime.ModeChat, agentRuntime.ModeConsult, agentRuntime.ModeAssist, agentRuntime.ModeMulti, agentRuntime.ModeWorkflow} {
		if !rollout.Enabled(mode) {
			return fmt.Errorf("shared API admission requires Runtime v2 for every mode")
		}
	}
	return nil
}

// ActiveTrendingReporterOwner returns the sole reporter implementation that
// this process may activate. A missing Temporal dependency degrades to no
// reporter instead of silently starting a second scheduling path.
func (p Plan) ActiveTrendingReporterOwner(temporalAvailable bool) TrendingReporterOwner {
	if !p.StartsWorkers() || !temporalAvailable {
		return TrendingReporterOwnerDisabled
	}
	return p.trendingReporterOwner
}
