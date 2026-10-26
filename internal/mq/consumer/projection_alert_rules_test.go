package consumer

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestProjectionAlertRulesParseAndIncludeTelemetryFailure(t *testing.T) {
	body, err := os.ReadFile("../../../deploy/prometheus/rules/twitter-clone-production.yml")
	require.NoError(t, err)
	var config struct {
		Groups []struct {
			Name  string `yaml:"name"`
			Rules []struct {
				Alert string `yaml:"alert"`
				Expr  string `yaml:"expr"`
				For   string `yaml:"for"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	require.NoError(t, yaml.Unmarshal(body, &config))
	found := map[string]bool{}
	for _, group := range config.Groups {
		if group.Name != "twitter-clone-projection-lifecycle" {
			continue
		}
		for _, rule := range group.Rules {
			require.NotEmpty(t, rule.Expr)
			require.Equal(t, "5m", rule.For)
			found[rule.Alert] = true
		}
	}
	for _, name := range []string{"TweetProjectionTasksExhausted", "TweetProjectionBacklogOld", "TweetProjectionBacklogTelemetryUnavailable"} {
		require.True(t, found[name], name)
	}
}
