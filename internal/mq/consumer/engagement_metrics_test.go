package consumer

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"testing"
	"time"
)

func TestEngagementMetricsBoundLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := NewEngagementMetrics(reg)
	require.NoError(t, err)
	m.Observe(RoutingKeyTweetLiked, "duplicate", time.Millisecond)
	m.Observe("private-user-id", "raw Redis error", time.Millisecond)
	families, err := reg.Gather()
	require.NoError(t, err)
	require.Len(t, families, 2)
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				switch label.GetName() {
				case "kind":
					require.Contains(t, []string{RoutingKeyTweetLiked, "unknown"}, label.GetValue())
				case "result":
					require.Contains(t, []string{"duplicate", "unknown"}, label.GetValue())
				default:
					t.Fatalf("unbounded label: %s", label.GetName())
				}
			}
		}
	}
}
func TestEngagementAlertRulesLoad(t *testing.T) {
	body, err := os.ReadFile("../../../deploy/prometheus/rules/twitter-clone-engagement.yml")
	require.NoError(t, err)
	var config struct {
		Groups []struct {
			Name  string                         `yaml:"name"`
			Rules []struct{ Alert, Expr string } `yaml:"rules"`
		} `yaml:"groups"`
	}
	require.NoError(t, yaml.Unmarshal(body, &config))
	require.Len(t, config.Groups, 1)
	require.Len(t, config.Groups[0].Rules, 5)
	for _, rule := range config.Groups[0].Rules {
		require.NotEmpty(t, rule.Alert)
		require.NotEmpty(t, rule.Expr)
	}
}
