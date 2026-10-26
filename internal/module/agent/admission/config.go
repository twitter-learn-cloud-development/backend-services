package admission

import (
	"errors"
	"strconv"
	"time"
)

func FromEnv(lookup func(string) (string, bool)) (Config, bool, error) {
	cfg := DefaultConfig()
	enabled := false
	if raw, ok := lookup("AGENT_SHARED_ADMISSION_ENABLED"); ok && raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return cfg, false, errors.New("invalid shared admission enable flag")
		}
		enabled = parsed
	}
	if raw, ok := lookup("AGENT_SHARED_ADMISSION_NAMESPACE"); ok && raw != "" {
		cfg.Namespace = raw
	}
	if raw, ok := lookup("AGENT_SHARED_ADMISSION_LEASE"); ok && raw != "" {
		lease, err := time.ParseDuration(raw)
		if err != nil {
			return cfg, enabled, errors.New("invalid admission lease")
		}
		cfg.Lease = lease
	}
	for name, target := range map[string]*int{"AGENT_SHARED_MAX_RUNS": &cfg.MaxRuns, "AGENT_SHARED_MAX_USER_RUNS": &cfg.MaxPerUser, "AGENT_SHARED_MAX_WORKFLOW_RUNS": &cfg.MaxPerWorkflow, "AGENT_SHARED_MAX_PROVIDER_CALLS": &cfg.MaxProviderCalls, "AGENT_SHARED_MAX_PER_PROVIDER": &cfg.MaxPerProvider, "AGENT_SHARED_OUTPUT_TOKEN_LIMIT": &cfg.OutputTokenLimit} {
		if raw, ok := lookup(name); ok && raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return cfg, enabled, errors.New("invalid shared admission integer")
			}
			*target = value
		}
	}
	for name, target := range map[string]*int64{"AGENT_SHARED_TOKEN_BUDGET_PER_MINUTE": &cfg.MaxTokensPerMinute, "AGENT_SHARED_COST_BUDGET_PER_MINUTE": &cfg.MaxCostMicrosPerMinute, "AGENT_SHARED_COST_MICROS_PER_TOKEN": &cfg.CostMicrosPerToken} {
		if raw, ok := lookup(name); ok && raw != "" {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return cfg, enabled, errors.New("invalid shared admission budget")
			}
			*target = value
		}
	}
	return cfg, enabled, cfg.Validate()
}
