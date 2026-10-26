package admission

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSharedConfigDefaultDisabledAndStrictParsing(t *testing.T) {
	lookup := func(values map[string]string) func(string) (string, bool) {
		return func(name string) (string, bool) { value, ok := values[name]; return value, ok }
	}
	cfg, enabled, err := FromEnv(lookup(nil))
	require.NoError(t, err)
	require.False(t, enabled)
	require.Equal(t, DefaultConfig(), cfg)
	for _, values := range []map[string]string{{"AGENT_SHARED_ADMISSION_ENABLED": "maybe"}, {"AGENT_SHARED_MAX_RUNS": "-1"}, {"AGENT_SHARED_ADMISSION_LEASE": "1s"}, {"AGENT_SHARED_TOKEN_BUDGET_PER_MINUTE": "no"}, {"AGENT_SHARED_COST_BUDGET_PER_MINUTE": "100"}} {
		_, _, err := FromEnv(lookup(values))
		require.Error(t, err)
	}
	cfg, enabled, err = FromEnv(lookup(map[string]string{"AGENT_SHARED_ADMISSION_ENABLED": "true", "AGENT_SHARED_COST_BUDGET_PER_MINUTE": "100", "AGENT_SHARED_COST_MICROS_PER_TOKEN": "2"}))
	require.NoError(t, err)
	require.True(t, enabled)
	require.Equal(t, int64(2), cfg.CostMicrosPerToken)
}
